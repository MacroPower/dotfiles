package render_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/render"
)

func TestLooksLikeJSShell(t *testing.T) {
	t.Parallel()

	longText := `This page has plenty of server-rendered words in it, more than
enough to clear the shell threshold: the quick brown fox jumps over the lazy
dog again and again and again until the count is well past thirty words.`

	tests := map[string]struct {
		body string
		md   string
		want bool
	}{
		"empty root div": {
			body: `<html><body><div id="root"></div><script src="/app.js"></script></body></html>`,
			md:   longText,
			want: true,
		},
		"empty app div": {
			body: `<html><body><div id="app">  </div><script src="/app.js"></script></body></html>`,
			md:   longText,
			want: true,
		},
		"react root marker": {
			body: `<html><body><div data-reactroot></div><script>init()</script></body></html>`,
			md:   longText,
			want: true,
		},
		"noscript enable javascript": {
			body: `<html><body><noscript>You need to enable JavaScript to run this app.</noscript><script src="/main.js"></script></body></html>`,
			md:   longText,
			want: true,
		},
		"tiny text with script": {
			body: `<html><body><div>Loading...</div><script src="/bundle.js"></script></body></html>`,
			md:   "Loading...",
			want: true,
		},
		"article page": {
			body: `<html><body><article>` + longText + `</article><script src="/analytics.js"></script></body></html>`,
			md:   longText,
			want: false,
		},
		"no scripts at all": {
			body: `<html><body><p>Tiny.</p></body></html>`,
			md:   "Tiny.",
			want: false,
		},
		"populated root div": {
			body: `<html><body><div id="root"><p>` + longText + `</p></div><script src="/app.js"></script></body></html>`,
			md:   longText,
			want: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := render.LooksLikeJSShell([]byte(tc.body), tc.md)
			assert.Equal(t, tc.want, got)
		})
	}
}
