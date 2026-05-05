package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		input FetchInput
		want  string
		err   string
	}{
		"html to markdown": {
			input: FetchInput{URL: srv.URL + "/html"},
			want:  "Hello World",
		},
		"plain text with content-type prefix": {
			input: FetchInput{URL: srv.URL + "/text"},
			want:  "Content-Type: text/plain\n\n" + plainText,
		},
		"json raw content": {
			input: FetchInput{URL: srv.URL + "/json"},
			want:  "Content-Type: application/json\n\n" + jsonBody,
		},
		"html raw mode": {
			input: FetchInput{URL: srv.URL + "/html", Raw: true},
			want:  "Content-Type: text/html; charset=utf-8\n\n" + htmlPage,
		},
		"redirect followed": {
			input: FetchInput{URL: srv.URL + "/redirect"},
			want:  "Content-Type: text/plain\n\n" + plainText,
		},
		"404 returns tool error": {
			input: FetchInput{URL: srv.URL + "/not-found"},
			err:   "HTTP error: status 404",
		},
		"500 returns tool error": {
			input: FetchInput{URL: srv.URL + "/server-error"},
			err:   "HTTP error: status 500",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, _, err := h.handle(t.Context(), nil, tt.input)
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

func TestTruncation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		content    string
		startIndex int
		maxLength  int
		want       string
	}{
		"no truncation needed": {
			content:   "short",
			maxLength: 100,
			want:      "short",
		},
		"truncated with hint": {
			content:   "abcdefghij",
			maxLength: 5,
			want:      "abcde",
		},
		"start index offset": {
			content:    "abcdefghij",
			startIndex: 3,
			maxLength:  100,
			want:       "defghij",
		},
		"start index beyond content": {
			content:    "short",
			startIndex: 100,
			maxLength:  100,
			want:       "<content empty",
		},
		"truncation hint": {
			content:   "abcdefghij",
			maxLength: 5,
			want:      "start_index=5",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, _ := truncate(tt.content, tt.startIndex, tt.maxLength)
			assert.Contains(t, result, tt.want)
		})
	}
}

func TestDefaultMaxLength(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")

		for range defaultMaxLength + 1000 {
			mustWrite(w, []byte("x"))
		}
	}))
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client())

	result, _, err := h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/"})
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
	r1, _, err := h.handle(t.Context(), nil, FetchInput{URL: target})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, r1), "hello")
	assert.Equal(t, int32(1), calls.Load())

	// Second call: cache hit, server not called again.
	r2, _, err := h.handle(t.Context(), nil, FetchInput{URL: target})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, r2), "hello")
	assert.Equal(t, int32(1), calls.Load())

	// Raw mode: separate cache entry, hits the server again.
	r3, _, err := h.handle(t.Context(), nil, FetchInput{URL: target, Raw: true})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, r3), "hello")
	assert.Equal(t, int32(2), calls.Load())
}

func TestAddTool(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	h := newTestHandler(t, http.DefaultClient)

	require.NotPanics(t, func() {
		mcp.AddTool(srv, &mcp.Tool{
			Name:        "fetch",
			Description: "Fetch a URL.",
		}, h.handle)
	})
}

func TestValidateURL(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, http.DefaultClient)

	tests := map[string]struct {
		url string
		err error
	}{
		"http allowed": {
			url: "http://example.com/page",
		},
		"https allowed": {
			url: "https://example.com/page",
		},
		"file scheme rejected": {
			url: "file:///etc/passwd",
			err: ErrBadScheme,
		},
		"ftp scheme rejected": {
			url: "ftp://example.com/file",
			err: ErrBadScheme,
		},
		"gopher scheme rejected": {
			url: "gopher://example.com/",
			err: ErrBadScheme,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, parseErr := url.ParseRequestURI(tt.url)
			require.NoError(t, parseErr)

			err := h.validateURL(u)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateURLWithRules(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, http.DefaultClient)
	h.rules = &Rules{
		deny: mustDeny(t, DenyRule{
			URLMatch: URLMatch{Host: `evil\.com`},
			Reason:   "blocked host",
		}),
	}

	u, err := url.ParseRequestURI("https://evil.com/page")
	require.NoError(t, err)

	got := h.validateURL(u)
	require.ErrorIs(t, got, ErrDenied)
	assert.Contains(t, got.Error(), "blocked host")
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

	h := newTestHandler(t, redirector.Client())

	// Parse the denied server's host to build a deny rule.
	deniedURL, err := url.Parse(denied.URL)
	require.NoError(t, err)

	h.rules = &Rules{
		deny: mustDeny(t, DenyRule{
			URLMatch: URLMatch{Host: regexp.QuoteMeta(deniedURL.Host)},
			Reason:   "denied redirect target",
		}),
	}

	result, _, err := h.handle(t.Context(), nil, FetchInput{URL: redirector.URL + "/go"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError, "expected tool error for denied redirect")
	assert.Contains(t, resultText(t, result), "denied redirect target")
}

func TestRedirectToFileScheme(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Redirect to a file:// URL.
		w.Header().Set("Location", "file:///etc/passwd")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client())

	result, _, err := h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/evil"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError, "expected tool error for file:// redirect")
	assert.Contains(t, resultText(t, result), "unsupported URL scheme")
}

func TestHandleLogging(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("ok"))
	}))
	t.Cleanup(srv.Close)

	denyRules := mustDeny(t, DenyRule{
		URLMatch: URLMatch{Host: `evil\.com`},
		Reason:   "blocked",
	})

	tests := map[string]struct {
		input     FetchInput
		wantMsg   string
		wantLevel string
		wantHost  string
	}{
		"denied URL logged at WARN": {
			input:     FetchInput{URL: "https://evil.com/page"},
			wantMsg:   "denied",
			wantLevel: "WARN",
			wantHost:  "evil.com",
		},
		"allowed URL logged at INFO": {
			input:     FetchInput{URL: srv.URL + "/ok"},
			wantMsg:   "allowed",
			wantLevel: "INFO",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			h := newTestHandler(t, srv.Client())
			h.log = slog.New(slog.NewJSONHandler(&buf, nil))
			h.rules = &Rules{deny: denyRules}

			_, _, err := h.handle(t.Context(), nil, tt.input)
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

	store := newTestStore(t)
	h := newTestHandler(t, srv.Client(), withStore(store))

	_, _, err := h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/x"})
	require.NoError(t, err)

	rows, err := store.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	got := rows[0]
	assert.Equal(t, OutcomeOK, got.Outcome)
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
	denyRules := mustDeny(t, DenyRule{
		URLMatch: URLMatch{Host: `blocked\.invalid`},
		Reason:   "test deny",
	})

	tests := map[string]struct {
		input        FetchInput
		wantOutcome  string
		wantStatus   int
		wantHostHas  string
		wantErrorHas string
		rules        *Rules
	}{
		"ok": {
			input:       FetchInput{URL: plainSrv.URL + "/ok"},
			wantOutcome: OutcomeOK, wantStatus: 200,
		},
		"http_error": {
			input:       FetchInput{URL: plainSrv.URL + "/404"},
			wantOutcome: OutcomeHTTPError, wantStatus: 404,
			wantErrorHas: "404",
		},
		"denied": {
			input:        FetchInput{URL: deniedURL},
			rules:        &Rules{deny: denyRules},
			wantOutcome:  OutcomeDenied,
			wantHostHas:  "blocked.invalid",
			wantErrorHas: "test deny",
		},
		"fetch_error transport": {
			input:        FetchInput{URL: "http://127.0.0.1:1/never-listens"},
			wantOutcome:  OutcomeFetchError,
			wantErrorHas: "performing request",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newTestStore(t)
			h := newTestHandler(t, plainSrv.Client(), withStore(store))

			if tt.rules != nil {
				h.rules = tt.rules
			}

			_, _, err := h.handle(t.Context(), nil, tt.input)
			_ = err

			rows, err := store.RecentFetches(t.Context(), 10)
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

	store := newTestStore(t)
	h := newTestHandler(t, http.DefaultClient, withStore(store))

	_, _, err := h.handle(t.Context(), nil, FetchInput{URL: "::not-a-url"})
	require.Error(t, err)

	rows, err := store.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, OutcomeInvalidURL, rows[0].Outcome)
	assert.Empty(t, rows[0].Host)
}

func TestHandleRecording_CacheHit(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("hello"))
	}))
	t.Cleanup(srv.Close)

	store := newTestStore(t)
	h := newTestHandler(t, srv.Client(), withStore(store))

	_, _, err := h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/cached"})
	require.NoError(t, err)

	_, _, err = h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/cached"})
	require.NoError(t, err)

	rows, err := store.RecentFetches(t.Context(), 10)
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

	store := newTestStore(t)
	h := newTestHandler(t, srv.Client(), withStore(store))

	_, _, err := h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/raw", Raw: true})
	require.NoError(t, err)

	rows, err := store.RecentFetches(t.Context(), 10)
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

	store := newTestStore(t)
	// Close the underlying DB so Record() fails.
	require.NoError(t, store.db.Close())

	var buf bytes.Buffer

	h := newTestHandler(t, srv.Client(),
		withStore(store),
		withLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
	)

	_, _, err := h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/x"})
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
