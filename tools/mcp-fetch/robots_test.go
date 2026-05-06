package main

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/temoto/robotstxt"
)

func TestCheckRobots(t *testing.T) {
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
			err:       ErrRobotsDisallowed,
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

			h := newTestHandler(t, srv.Client())
			h.checkRobots = true

			u, parseErr := url.Parse(srv.URL + tt.path)
			require.NoError(t, parseErr)

			err := h.checkRobotsURL(t.Context(), u)

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

	// Serve a robots.txt larger than maxRobotsBytes.
	body := "User-agent: *\nDisallow: /secret/\n" + strings.Repeat("# padding\n", maxRobotsBytes/10)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain")
			mustWrite(w, []byte(body))

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client())
	h.checkRobots = true

	u, err := url.Parse(srv.URL + "/secret/page")
	require.NoError(t, err)

	// Should still parse correctly and enforce the disallow rule.
	err = h.checkRobotsURL(t.Context(), u)
	require.ErrorIs(t, err, ErrRobotsDisallowed)
}

func TestCrossOriginRedirectRobotsCheck(t *testing.T) {
	t.Parallel()

	// Target server: has robots.txt disallowing /secret/.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			mustWrite(w, []byte("User-agent: *\nDisallow: /secret/\n"))

		default:
			w.WriteHeader(http.StatusOK)
			mustWrite(w, []byte("target content"))
		}
	}))
	t.Cleanup(target.Close)

	// Redirector: sends a cross-origin redirect to the target's disallowed path.
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.WriteHeader(http.StatusNotFound)
		default:
			http.Redirect(w, r, target.URL+"/secret/page", http.StatusFound)
		}
	}))
	t.Cleanup(redirector.Close)

	h := newTestHandler(t, &http.Client{})
	h.checkRobots = true

	result, _, err := h.handle(t.Context(), nil, FetchInput{URL: redirector.URL + "/go"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError, "expected tool error for cross-origin redirect to disallowed path")
	assert.Contains(t, resultText(t, result), "robots.txt")
}

func newTestHandler(t *testing.T, client *http.Client, opts ...fetchOption) *fetchHandler {
	t.Helper()

	defaults := []fetchOption{
		withUserAgent("test-agent"),
		withLogger(slog.Default()),
		withRobotsCache(expirable.NewLRU[string, *robotstxt.RobotsData](10, nil, time.Hour)),
		withContentCache(expirable.NewLRU[string, string](10, nil, time.Hour)),
		withLLMsCache(expirable.NewLRU[string, string](10, nil, time.Hour)),
		withCheckRobots(false),
		withCheckLLMs(false),
	}

	h := newFetchHandler(append(defaults, opts...)...)

	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}

		err := h.validateURL(req.URL)
		if err != nil {
			return err
		}

		if h.checkRobots && req.URL.Host != via[len(via)-1].URL.Host {
			return h.checkRobotsURL(req.Context(), req.URL)
		}

		return nil
	}

	h.client = client

	return h
}
