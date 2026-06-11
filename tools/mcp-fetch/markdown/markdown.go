package markdown

import (
	"bytes"
	"fmt"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
)

// Convert renders an HTML document to Markdown. It first tries
// readability extraction to isolate the main article, falling back to
// converting the whole document when extraction yields nothing usable.
func Convert(body []byte) (string, error) {
	article, err := readability.FromReader(bytes.NewReader(body), nil)
	if err == nil && article.Node != nil {
		var buf bytes.Buffer

		renderErr := article.RenderHTML(&buf)
		if renderErr == nil {
			md, mdErr := htmltomarkdown.ConvertString(buf.String())
			if mdErr == nil && strings.TrimSpace(md) != "" {
				return md, nil
			}
		}
	}

	md, err := htmltomarkdown.ConvertString(string(body))
	if err != nil {
		return "", fmt.Errorf("converting HTML to Markdown: %w", err)
	}

	return md, nil
}
