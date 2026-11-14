package dvm

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

	s := New(WithURL("http://service.invalid"))
	s.http = &http.Client{Transport: httpfake.BodyTransport{Body: raw}}

	body, err := s.Artifact(t.Context(), "core-0.json")
	require.NoError(t, err)

	n, err := io.CopyN(io.Discard, body, maxArtifactBytes+1)
	assert.Equal(t, int64(maxArtifactBytes), n, "the reader stops at the cap")
	require.ErrorIs(t, err, io.EOF, "the reader ends at the cap")

	require.EqualError(t, body.Close(), httpfake.ErrBodyClose.Error(),
		"the reader returns the close error of the body unchanged")
	assert.True(t, raw.Closed)
}
