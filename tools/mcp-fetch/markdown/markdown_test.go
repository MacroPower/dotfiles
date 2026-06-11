package markdown_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/markdown"
)

func TestConvert(t *testing.T) {
	t.Parallel()

	html := `<!DOCTYPE html>
<html><head><title>Test</title></head>
<body><article><h1>Hello World</h1><p>This is a test paragraph.</p></article></body>
</html>`

	got, err := markdown.Convert([]byte(html))
	require.NoError(t, err)
	assert.Contains(t, got, "Hello World")
	assert.Contains(t, got, "test paragraph")
}

func TestConvertFragment(t *testing.T) {
	t.Parallel()

	// A bare fragment that readability cannot turn into an article still
	// converts via the whole-document fallback.
	got, err := markdown.Convert([]byte("<p>just <strong>some</strong> text</p>"))
	require.NoError(t, err)
	assert.Contains(t, got, "some")
}
