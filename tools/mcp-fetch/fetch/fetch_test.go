package fetch_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/fetch"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/rules"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/store"
)

const llmsNoticeMarker = "<llms.txt available at "

func TestHandle(t *testing.T) {
	t.Parallel()

	htmlPage := `<!DOCTYPE html>
<html><head><title>Test</title></head>
<body><article><h1>Hello World</h1><p>This is a test paragraph.</p></article></body>
</html>`

	plainText := "Hello, plain text!"
	jsonBody := `{"key": "value"}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			mustWrite(w, []byte(htmlPage))

		case "/text":
			w.Header().Set("Content-Type", "text/plain")
			mustWrite(w, []byte(plainText))

		case "/json":
			w.Header().Set("Content-Type", "application/json")
			mustWrite(w, []byte(jsonBody))

		case "/redirect":
			http.Redirect(w, r, "/text", http.StatusFound)
		case "/not-found":
			w.WriteHeader(http.StatusNotFound)
			mustWrite(w, []byte("not found"))

		case "/server-error":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client())

	tests := map[string]struct {
		input fetch.Input
		want  string
		err   string
	}{
		"html to markdown": {
			input: fetch.Input{URL: srv.URL + "/html"},
			want:  "Hello World",
		},
		"plain text with content-type prefix": {
			input: fetch.Input{URL: srv.URL + "/text"},
			want:  "Content-Type: text/plain\n\n" + plainText,
		},
		"json raw content": {
			input: fetch.Input{URL: srv.URL + "/json"},
			want:  "Content-Type: application/json\n\n" + jsonBody,
		},
		"html raw mode": {
			input: fetch.Input{URL: srv.URL + "/html", Raw: true},
			want:  "Content-Type: text/html; charset=utf-8\n\n" + htmlPage,
		},
		"redirect followed": {
			input: fetch.Input{URL: srv.URL + "/redirect"},
			want:  "Content-Type: text/plain\n\n" + plainText,
		},
		"404 returns tool error": {
			input: fetch.Input{URL: srv.URL + "/not-found"},
			err:   "HTTP error: status 404",
		},
		"500 returns tool error": {
			input: fetch.Input{URL: srv.URL + "/server-error"},
			err:   "HTTP error: status 500",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, _, err := h.Handle(t.Context(), nil, tt.input)
			require.NoError(t, err)
			require.NotNil(t, result)

			text := resultText(t, result)
			if tt.err != "" {
				assert.True(t, result.IsError, "expected tool error")
				assert.Contains(t, text, tt.err)
			} else {
				assert.False(t, result.IsError, "unexpected tool error: %s", text)
				assert.Contains(t, text, tt.want)
			}
		})
	}
}

func TestHandleGrep(t *testing.T) {
	t.Parallel()

	htmlPage := `<!DOCTYPE html>
<html><head><title>Test</title></head>
<body><article><h1>Hello World</h1><p>This is a test paragraph.</p></article></body>
</html>`

	var manyLines strings.Builder
	for i := range 50 {
		fmt.Fprintf(&manyLines, "match line %d\n", i)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			mustWrite(w, []byte(htmlPage))

		case "/text":
			w.Header().Set("Content-Type", "text/plain")
			mustWrite(w, []byte("first line\nsecond line\nthird line"))

		case "/lines":
			w.Header().Set("Content-Type", "text/plain")
			mustWrite(w, []byte(manyLines.String()))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	tests := map[string]struct {
		input       fetch.Input
		wantHas     []string
		wantMissing []string
		wantErr     bool
		errHas      string
	}{
		"filters markdown to matching lines": {
			input:       fetch.Input{URL: srv.URL + "/html", Pattern: "paragraph"},
			wantHas:     []string{"paragraph"},
			wantMissing: []string{"Hello World"},
		},
		"no match returns notice not error": {
			input:   fetch.Input{URL: srv.URL + "/html", Pattern: "zzzznomatch"},
			wantHas: []string{"<no lines matched pattern", "zzzznomatch"},
		},
		"invalid pattern returns tool error": {
			input:   fetch.Input{URL: srv.URL + "/html", Pattern: "("},
			wantErr: true,
			errHas:  "invalid grep pattern",
		},
		"content-type prefix line participates in matching": {
			input:   fetch.Input{URL: srv.URL + "/text", Pattern: "Content-Type"},
			wantHas: []string{"1:Content-Type: text/plain"},
		},
		"grep composes with pagination": {
			input:   fetch.Input{URL: srv.URL + "/lines", Pattern: "match line", MaxLength: 20},
			wantHas: []string{"content truncated"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newTestHandler(t, srv.Client())

			result, _, err := h.Handle(t.Context(), nil, tt.input)
			require.NoError(t, err)
			require.NotNil(t, result)

			text := resultText(t, result)

			if tt.wantErr {
				assert.True(t, result.IsError, "expected tool error")
				assert.Contains(t, text, tt.errHas)

				return
			}

			assert.False(t, result.IsError, "unexpected tool error: %s", text)

			for _, want := range tt.wantHas {
				assert.Contains(t, text, want)
			}

			for _, missing := range tt.wantMissing {
				assert.NotContains(t, text, missing)
			}
		})
	}
}

func TestHandleGrepNoMatchRecordsOK(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("hello world"))
	}))
	t.Cleanup(srv.Close)

	st := newTestStore(t)
	h := newTestHandler(t, srv.Client(), fetch.WithStore(st))

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/x", Pattern: "nomatch"})
	require.NoError(t, err)
	assert.False(t, result.IsError)

	rows, err := st.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, store.OutcomeOK, rows[0].Outcome)
	assert.Equal(t, 0, rows[0].Truncated)
	assert.Positive(t, rows[0].OutputBytes)
}

func TestDefaultMaxLength(t *testing.T) {
	t.Parallel()

	// The default cap is 5000 characters; serve more than that.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")

		for range 6000 {
			mustWrite(w, []byte("x"))
		}
	}))
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client())

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/"})
	require.NoError(t, err)

	text := resultText(t, result)
	assert.Contains(t, text, "content truncated")
}

func TestContentCacheHit(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		mustWrite(w, []byte(`<html><body><article><p>hello</p></article></body></html>`))
	}))
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client())
	target := srv.URL + "/page"

	// First call: cache miss, hits the server.
	r1, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: target})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, r1), "hello")
	assert.Equal(t, int32(1), calls.Load())

	// Second call: cache hit, server not called again.
	r2, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: target})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, r2), "hello")
	assert.Equal(t, int32(1), calls.Load())

	// Raw mode: separate cache entry, hits the server again.
	r3, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: target, Raw: true})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, r3), "hello")
	assert.Equal(t, int32(2), calls.Load())
}

func TestAddTool(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	h := newTestHandler(t, &http.Client{})

	require.NotPanics(t, func() {
		mcp.AddTool(srv, &mcp.Tool{
			Name:        "fetch",
			Description: "Fetch a URL.",
		}, h.Handle)
	})
}

func TestRejectedSchemes(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"file scheme rejected":   "file:///etc/passwd",
		"ftp scheme rejected":    "ftp://example.com/file",
		"gopher scheme rejected": "gopher://example.com/",
	}

	for name, target := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newTestHandler(t, &http.Client{})

			result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: target})
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.True(t, result.IsError, "expected tool error")
			assert.Contains(t, resultText(t, result), "unsupported URL scheme")
		})
	}
}

func TestDeniedByRules(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &http.Client{}, fetch.WithRules(mustRules(t,
		rules.DenyRule{URLMatch: rules.URLMatch{Host: `evil\.com`}, Reason: "blocked host"},
	)))

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: "https://evil.com/page"})
	require.NoError(t, err)
	assert.True(t, result.IsError, "expected tool error")
	assert.Contains(t, resultText(t, result), "blocked host")
}

func TestRedirectToDeniedURL(t *testing.T) {
	t.Parallel()

	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		mustWrite(w, []byte("should not reach here"))
	}))
	t.Cleanup(denied.Close)

	// Redirector sends to the denied server.
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, denied.URL+"/secret", http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	deniedURL, err := url.Parse(denied.URL)
	require.NoError(t, err)

	h := newTestHandler(t, redirector.Client(), fetch.WithRules(mustRules(t,
		rules.DenyRule{
			URLMatch: rules.URLMatch{Host: regexp.QuoteMeta(deniedURL.Host)},
			Reason:   "denied redirect target",
		},
	)))

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: redirector.URL + "/go"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError, "expected tool error for denied redirect")
	assert.Contains(t, resultText(t, result), "denied redirect target")
}

func TestRedirectToFileScheme(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Redirect to a file:// URL.
		w.Header().Set("Location", "file:///etc/passwd")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client())

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/evil"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError, "expected tool error for file:// redirect")
	assert.Contains(t, resultText(t, result), "unsupported URL scheme")
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

	h := newTestHandler(t, &http.Client{}, fetch.WithCheckRobots(true))

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: redirector.URL + "/go"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError, "expected tool error for cross-origin redirect to disallowed path")
	assert.Contains(t, resultText(t, result), "robots.txt")
}

func TestHandleLogging(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("ok"))
	}))
	t.Cleanup(srv.Close)

	tests := map[string]struct {
		input     fetch.Input
		wantMsg   string
		wantLevel string
		wantHost  string
	}{
		"denied URL logged at WARN": {
			input:     fetch.Input{URL: "https://evil.com/page"},
			wantMsg:   "denied",
			wantLevel: "WARN",
			wantHost:  "evil.com",
		},
		"allowed URL logged at INFO": {
			input:     fetch.Input{URL: srv.URL + "/ok"},
			wantMsg:   "allowed",
			wantLevel: "INFO",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			h := newTestHandler(t, srv.Client(),
				fetch.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
				fetch.WithRules(mustRules(t,
					rules.DenyRule{URLMatch: rules.URLMatch{Host: `evil\.com`}, Reason: "blocked"},
				)),
			)

			_, _, err := h.Handle(t.Context(), nil, tt.input)
			require.NoError(t, err)

			var entry map[string]any

			err = json.Unmarshal(buf.Bytes(), &entry)
			require.NoError(t, err, "log output: %s", buf.String())

			assert.Equal(t, tt.wantMsg, entry["msg"])
			assert.Equal(t, tt.wantLevel, entry["level"])
			assert.Contains(t, entry["url"], tt.input.URL)

			if tt.wantHost != "" {
				assert.Equal(t, tt.wantHost, entry["host"])
			}
		})
	}
}

func TestHandleRecording_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("hello world"))
	}))
	t.Cleanup(srv.Close)

	st := newTestStore(t)
	h := newTestHandler(t, srv.Client(), fetch.WithStore(st))

	_, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/x"})
	require.NoError(t, err)

	rows, err := st.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	got := rows[0]
	assert.Equal(t, store.OutcomeOK, got.Outcome)
	assert.Equal(t, 200, got.StatusCode)
	assert.Contains(t, got.ContentType, "text/plain")
	assert.Positive(t, got.ResponseBytes)
	assert.Positive(t, got.OutputBytes)
	assert.Equal(t, 0, got.CacheHit)
	assert.Empty(t, got.Error)
}

func TestHandleRecording_AllOutcomes(t *testing.T) {
	t.Parallel()

	plainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("Content-Type", "text/plain")
			mustWrite(w, []byte("ok"))

		case "/404":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(plainSrv.Close)

	deniedURL := "https://blocked.invalid/x"

	tests := map[string]struct {
		input        fetch.Input
		wantOutcome  string
		wantStatus   int
		wantHostHas  string
		wantErrorHas string
		denied       bool
	}{
		"ok": {
			input:       fetch.Input{URL: plainSrv.URL + "/ok"},
			wantOutcome: store.OutcomeOK, wantStatus: 200,
		},
		"http_error": {
			input:       fetch.Input{URL: plainSrv.URL + "/404"},
			wantOutcome: store.OutcomeHTTPError, wantStatus: 404,
			wantErrorHas: "404",
		},
		"denied": {
			input:        fetch.Input{URL: deniedURL},
			denied:       true,
			wantOutcome:  store.OutcomeDenied,
			wantHostHas:  "blocked.invalid",
			wantErrorHas: "test deny",
		},
		"fetch_error transport": {
			input:        fetch.Input{URL: "http://127.0.0.1:1/never-listens"},
			wantOutcome:  store.OutcomeFetchError,
			wantErrorHas: "performing request",
		},
		"bad_pattern": {
			input:        fetch.Input{URL: plainSrv.URL + "/ok", Pattern: "("},
			wantOutcome:  store.OutcomeBadPattern,
			wantHostHas:  "127.0.0.1",
			wantErrorHas: "error parsing regexp",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			st := newTestStore(t)

			opts := []fetch.Option{fetch.WithStore(st)}
			if tt.denied {
				opts = append(opts, fetch.WithRules(mustRules(t,
					rules.DenyRule{URLMatch: rules.URLMatch{Host: `blocked\.invalid`}, Reason: "test deny"},
				)))
			}

			h := newTestHandler(t, plainSrv.Client(), opts...)

			_, _, _ = h.Handle(t.Context(), nil, tt.input)

			rows, err := st.RecentFetches(t.Context(), 10)
			require.NoError(t, err)
			require.Len(t, rows, 1)

			got := rows[0]
			assert.Equal(t, tt.wantOutcome, got.Outcome)

			if tt.wantStatus != 0 {
				assert.Equal(t, tt.wantStatus, got.StatusCode)
			}

			if tt.wantHostHas != "" {
				assert.Contains(t, got.Host, tt.wantHostHas)
			}

			if tt.wantErrorHas != "" {
				assert.Contains(t, got.Error, tt.wantErrorHas)
			}
		})
	}
}

func TestHandleRecording_InvalidURL(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	h := newTestHandler(t, &http.Client{}, fetch.WithStore(st))

	_, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: "::not-a-url"})
	require.Error(t, err)

	rows, err := st.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, store.OutcomeInvalidURL, rows[0].Outcome)
	assert.Empty(t, rows[0].Host)
}

func TestHandleRecording_CacheHit(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("hello"))
	}))
	t.Cleanup(srv.Close)

	st := newTestStore(t)
	h := newTestHandler(t, srv.Client(), fetch.WithStore(st))

	_, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/cached"})
	require.NoError(t, err)

	_, _, err = h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/cached"})
	require.NoError(t, err)

	rows, err := st.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// Newest first: second call was a cache hit.
	assert.Equal(t, 1, rows[0].CacheHit)
	assert.Equal(t, 0, rows[0].StatusCode, "cache hit rows have status_code=0 by convention")
	assert.Empty(t, rows[0].ContentType)
	assert.Equal(t, 0, rows[0].ResponseBytes)
	// First call was a cache miss.
	assert.Equal(t, 0, rows[1].CacheHit)
	assert.Equal(t, 200, rows[1].StatusCode)
}

func TestHandleRecording_RawMode(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		mustWrite(w, []byte("<html><body>hi</body></html>"))
	}))
	t.Cleanup(srv.Close)

	st := newTestStore(t)
	h := newTestHandler(t, srv.Client(), fetch.WithStore(st))

	_, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/raw", Raw: true})
	require.NoError(t, err)

	rows, err := st.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].RawMode)
}

func TestHandleRecording_FailureLogsWarn(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("ok"))
	}))
	t.Cleanup(srv.Close)

	st := newTestStore(t)
	// Close the store so Record() fails.
	require.NoError(t, st.Close())

	var buf bytes.Buffer

	h := newTestHandler(t, srv.Client(),
		fetch.WithStore(st),
		fetch.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
	)

	_, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/x"})
	require.NoError(t, err)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 2,
		"expected exactly two log lines (allowed Info + recording fetch Warn), got: %s", buf.String())

	var first, second map[string]any

	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &second))

	assert.Equal(t, "allowed", first["msg"])
	assert.Equal(t, "INFO", first["level"])
	assert.Equal(t, "recording fetch", second["msg"])
	assert.Equal(t, "WARN", second["level"])
}

func TestLLMsTxtDiscovery(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status      int
		contentType string
		body        string
		wantNotice  bool
	}{
		"present with text/markdown": {
			status:      http.StatusOK,
			contentType: "text/markdown",
			body:        "# llms.txt\n",
			wantNotice:  true,
		},
		"present with text/plain": {
			status:      http.StatusOK,
			contentType: "text/plain",
			body:        "# llms.txt\n",
			wantNotice:  true,
		},
		"missing returns 404": {
			status:     http.StatusNotFound,
			wantNotice: false,
		},
		"html content-type rejected": {
			status:      http.StatusOK,
			contentType: "text/html; charset=utf-8",
			body:        "<html><body>SPA fallback</body></html>",
			wantNotice:  false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/llms.txt" {
					if tt.contentType != "" {
						w.Header().Set("Content-Type", tt.contentType)
					}

					w.WriteHeader(tt.status)

					if tt.body != "" {
						mustWrite(w, []byte(tt.body))
					}

					return
				}

				w.Header().Set("Content-Type", "text/plain")
				mustWrite(w, []byte("page body"))
			}))
			t.Cleanup(srv.Close)

			h := newTestHandler(t, srv.Client(), fetch.WithCheckLLMs(true))

			result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/page"})
			require.NoError(t, err)
			require.NotNil(t, result)

			text := resultText(t, result)

			if tt.wantNotice {
				assert.Contains(t, text, llmsNoticeMarker)
				assert.Contains(t, text, srv.URL+"/llms.txt")
			} else {
				assert.NotContains(t, text, llmsNoticeMarker)
			}
		})
	}
}

func TestLLMsTxtOutputBytesExcludesNotice(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/llms.txt" {
			w.Header().Set("Content-Type", "text/markdown")
			mustWrite(w, []byte("# llms.txt\n"))

			return
		}

		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("page body"))
	}))
	t.Cleanup(srv.Close)

	st := newTestStore(t)
	h := newTestHandler(t, srv.Client(), fetch.WithCheckLLMs(true), fetch.WithStore(st))

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/page"})
	require.NoError(t, err)

	text := resultText(t, result)
	require.Contains(t, text, llmsNoticeMarker)

	noticeStart := strings.Index(text, "\n\n"+llmsNoticeMarker)
	require.GreaterOrEqual(t, noticeStart, 0, "notice marker not found in result text")

	noticeLen := len(text) - noticeStart

	rows, err := st.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	assert.Equal(t, len(text)-noticeLen, rows[0].OutputBytes,
		"OutputBytes must reflect content bytes only, not the appended notice")
}

func TestLLMsTxtPositiveCacheHit(t *testing.T) {
	t.Parallel()

	var probeCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/llms.txt" {
			probeCount.Add(1)

			w.Header().Set("Content-Type", "text/markdown")
			mustWrite(w, []byte("# llms.txt\n"))

			return
		}

		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("page body"))
	}))
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client(), fetch.WithCheckLLMs(true))

	r1, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/page-a"})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, r1), llmsNoticeMarker)

	r2, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/page-b"})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, r2), llmsNoticeMarker)

	assert.Equal(t, int32(1), probeCount.Load(),
		"second fetch on same origin must reuse positive cache, not re-probe")
}

func TestLLMsTxtNoticeOnContentCacheHit(t *testing.T) {
	t.Parallel()

	var probeCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/llms.txt" {
			probeCount.Add(1)

			w.Header().Set("Content-Type", "text/markdown")
			mustWrite(w, []byte("# llms.txt\n"))

			return
		}

		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("page body"))
	}))
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client(), fetch.WithCheckLLMs(true))
	target := srv.URL + "/page"

	r1, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: target})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, r1), llmsNoticeMarker,
		"first fetch (content cache miss) must include the notice")

	r2, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: target})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, r2), llmsNoticeMarker,
		"second fetch (content cache hit) must still include the notice")

	assert.Equal(t, int32(1), probeCount.Load(),
		"llms cache must short-circuit the second probe even when content cache hit")
}

func TestLLMsTxtNegativeCacheHit(t *testing.T) {
	t.Parallel()

	var probeCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/llms.txt" {
			probeCount.Add(1)
			w.WriteHeader(http.StatusNotFound)

			return
		}

		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("page body"))
	}))
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client(), fetch.WithCheckLLMs(true))

	_, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/page-a"})
	require.NoError(t, err)

	_, _, err = h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/page-b"})
	require.NoError(t, err)

	assert.Equal(t, int32(1), probeCount.Load(),
		"second fetch must reuse negative cache, not re-probe")
}

func TestLLMsTxtSkipWhenFetchingItself(t *testing.T) {
	t.Parallel()

	var probeCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/llms.txt" {
			probeCount.Add(1)

			w.Header().Set("Content-Type", "text/markdown")
			mustWrite(w, []byte("# llms.txt\n"))

			return
		}

		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("page body"))
	}))
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client(), fetch.WithCheckLLMs(true))

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/llms.txt"})
	require.NoError(t, err)

	text := resultText(t, result)
	assert.NotContains(t, text, llmsNoticeMarker,
		"fetching llms.txt directly must not append a self-referential notice")

	assert.Equal(t, int32(1), probeCount.Load(),
		"only the user's GET should hit /llms.txt; no separate probe")
}

func TestLLMsTxtSkipOnPagedFetch(t *testing.T) {
	t.Parallel()

	var probeCount atomic.Int32

	body := strings.Repeat("x", 100)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/llms.txt" {
			probeCount.Add(1)

			w.Header().Set("Content-Type", "text/markdown")
			mustWrite(w, []byte("# llms.txt\n"))

			return
		}

		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte(body))
	}))
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client(), fetch.WithCheckLLMs(true))

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{
		URL:        srv.URL + "/page",
		StartIndex: 10,
	})
	require.NoError(t, err)

	assert.NotContains(t, resultText(t, result), llmsNoticeMarker,
		"paged fetch must not include the notice")
	assert.Equal(t, int32(0), probeCount.Load(),
		"paged fetch must not trigger an llms.txt probe")
}

func TestLLMsTxtDisabledViaOption(t *testing.T) {
	t.Parallel()

	var probeCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/llms.txt" {
			probeCount.Add(1)

			w.Header().Set("Content-Type", "text/markdown")
			mustWrite(w, []byte("# llms.txt\n"))

			return
		}

		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("page body"))
	}))
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client(), fetch.WithCheckLLMs(false))

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/page"})
	require.NoError(t, err)

	assert.NotContains(t, resultText(t, result), llmsNoticeMarker)
	assert.Equal(t, int32(0), probeCount.Load(),
		"discovery disabled must not trigger any probe")
}

// newTestHandler builds a handler over the given client with robots and
// llms discovery off by default; pass options to override.
//
// Each handler gets its own client cloned from the caller's, sharing the
// (concurrency-safe) Transport but owning its CheckRedirect, which
// [fetch.New] installs. httptest servers hand back one shared *http.Client,
// so cloning keeps parallel subtests from racing on that field.
func newTestHandler(t *testing.T, client *http.Client, opts ...fetch.Option) *fetch.Handler {
	t.Helper()

	own := &http.Client{Transport: client.Transport, Timeout: client.Timeout}

	defaults := []fetch.Option{
		fetch.WithUserAgent("test-agent"),
		fetch.WithLogger(slog.Default()),
		fetch.WithClient(own),
		fetch.WithCheckRobots(false),
		fetch.WithCheckLLMs(false),
	}

	return fetch.New(append(defaults, opts...)...)
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")

	st, err := store.Open(t.Context(), dbPath)
	require.NoError(t, err)

	// Close is idempotent, so this is safe even when a test closes the
	// store early to exercise a Record failure.
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	return st
}

func mustRules(t *testing.T, deny ...rules.DenyRule) *rules.Rules {
	t.Helper()

	r, err := rules.Compile("", deny, nil)
	require.NoError(t, err)

	return r
}

func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content)

	tc, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected TextContent, got %T", result.Content[0])

	return tc.Text
}

func mustWrite(w http.ResponseWriter, data []byte) {
	_, err := w.Write(data)
	if err != nil {
		panic(fmt.Sprintf("writing response: %v", err))
	}
}
