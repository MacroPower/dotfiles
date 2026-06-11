package robots_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/robots"
)

func TestCheck(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		robotsTxt string
		status    int
		path      string
		err       error
	}{
		"allowed path": {
			robotsTxt: "User-agent: *\nDisallow: /private/\n",
			status:    http.StatusOK,
			path:      "/public/page",
		},
		"disallowed path": {
			robotsTxt: "User-agent: *\nDisallow: /private/\n",
			status:    http.StatusOK,
			path:      "/private/secret",
			err:       robots.ErrDisallowed,
		},
		"missing robots.txt": {
			status: http.StatusNotFound,
			path:   "/anything",
		},
		"malformed robots.txt allows": {
			robotsTxt: "this is not valid robots.txt content @@##$$",
			status:    http.StatusOK,
			path:      "/anything",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/robots.txt" {
					w.WriteHeader(tt.status)

					if tt.robotsTxt != "" {
						mustWrite(w, []byte(tt.robotsTxt))
					}

					return
				}

				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)

			c := robots.New(srv.Client(), "test-agent")

			u, parseErr := url.Parse(srv.URL + tt.path)
			require.NoError(t, parseErr)

			err := c.Check(t.Context(), u)

			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOversizedRobotsTxt(t *testing.T) {
	t.Parallel()

	// Serve a robots.txt well past the internal read cap (512 KiB).
	body := "User-agent: *\nDisallow: /secret/\n" + strings.Repeat("# padding\n", 60_000)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain")
			mustWrite(w, []byte(body))

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := robots.New(srv.Client(), "test-agent")

	u, err := url.Parse(srv.URL + "/secret/page")
	require.NoError(t, err)

	// The disallow rule sits at the top, so it is enforced even though
	// the trailing padding is truncated.
	err = c.Check(t.Context(), u)
	require.ErrorIs(t, err, robots.ErrDisallowed)
}

func TestNilCheckerAllows(t *testing.T) {
	t.Parallel()

	var c *robots.Checker

	u, err := url.Parse("https://example.com/anything")
	require.NoError(t, err)

	assert.NoError(t, c.Check(t.Context(), u))
}

func mustWrite(w http.ResponseWriter, data []byte) {
	_, err := w.Write(data)
	if err != nil {
		panic(fmt.Sprintf("writing response: %v", err))
	}
}
