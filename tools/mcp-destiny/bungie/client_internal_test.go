package bungie

import (
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// endlessBody is a response body that never ends and reads as zero-valued
// bytes. Its Close records the call and returns errBodyClose.
type endlessBody struct {
	closed bool
}

var errBodyClose = errors.New("body close refused")

func (b *endlessBody) Read(p []byte) (int, error) {
	clear(p)

	return len(p), nil
}

func (b *endlessBody) Close() error {
	b.closed = true

	return errBodyClose
}

// bodyTransport answers every request with status 200 and one body.
type bodyTransport struct {
	body io.ReadCloser
}

func (rt bodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       rt.body,
		Request:    req,
	}, nil
}

func TestCappedBody(t *testing.T) {
	t.Parallel()

	raw := &endlessBody{}

	b := New(WithContentURL("http://content.invalid"))
	b.http = &http.Client{Transport: bodyTransport{body: raw}}

	body, err := b.Content(t.Context(), "/table.json")
	require.NoError(t, err)

	n, err := io.CopyN(io.Discard, body, maxContentBytes+1)
	assert.Equal(t, int64(maxContentBytes), n, "the reader stops at the cap")
	require.ErrorIs(t, err, io.EOF, "the reader ends at the cap")

	require.EqualError(t, body.Close(), errBodyClose.Error(),
		"the reader returns the close error of the body unchanged")
	assert.True(t, raw.closed)
}
