package bungie

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/httpfake"
)

func TestCappedBody(t *testing.T) {
	t.Parallel()

	raw := &httpfake.EndlessBody{}

	b := New(WithContentURL("http://content.invalid"))
	b.http = &http.Client{Transport: httpfake.BodyTransport{Body: raw}}

	body, err := b.Content(t.Context(), "/table.json")
	require.NoError(t, err)

	n, err := io.CopyN(io.Discard, body, maxContentBytes+1)
	assert.Equal(t, int64(maxContentBytes), n, "the reader stops at the cap")
	require.ErrorIs(t, err, io.EOF, "the reader ends at the cap")

	require.EqualError(t, body.Close(), httpfake.ErrBodyClose.Error(),
		"the reader returns the close error of the body unchanged")
	assert.True(t, raw.Closed)
}
