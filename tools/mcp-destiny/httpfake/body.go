package httpfake

import (
	"errors"
	"io"
	"net/http"
)

// ErrBodyClose is the error that [*EndlessBody.Close] returns.
var ErrBodyClose = errors.New("body close refused")

// EndlessBody is a response body that never ends and reads as zero-valued
// bytes. Its Close records the call and returns [ErrBodyClose].
type EndlessBody struct {
	// Closed reports whether a caller closed the body.
	Closed bool
}

// Read fills p with zero-valued bytes.
func (b *EndlessBody) Read(p []byte) (int, error) {
	clear(p)

	return len(p), nil
}

// Close records the call and returns [ErrBodyClose].
func (b *EndlessBody) Close() error {
	b.Closed = true

	return ErrBodyClose
}

// BodyTransport is an [http.RoundTripper] that answers every request with
// status 200 and one body.
type BodyTransport struct {
	// Body is the body of every answer.
	Body io.ReadCloser
}

// RoundTrip answers req with status 200 and the body of the transport.
func (rt BodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       rt.Body,
		Request:    req,
	}, nil
}
