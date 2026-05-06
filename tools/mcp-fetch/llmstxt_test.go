package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const llmsNoticeMarker = "<llms.txt available at "

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

			h := newTestHandler(t, srv.Client(), withCheckLLMs(true))

			result, _, err := h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/page"})
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

	store := newTestStore(t)
	h := newTestHandler(t, srv.Client(), withCheckLLMs(true), withStore(store))

	result, _, err := h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/page"})
	require.NoError(t, err)

	text := resultText(t, result)
	require.Contains(t, text, llmsNoticeMarker)

	noticeStart := strings.Index(text, "\n\n"+llmsNoticeMarker)
	require.GreaterOrEqual(t, noticeStart, 0, "notice marker not found in result text")

	noticeLen := len(text) - noticeStart

	rows, err := store.RecentFetches(t.Context(), 10)
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

	h := newTestHandler(t, srv.Client(), withCheckLLMs(true))

	r1, _, err := h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/page-a"})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, r1), llmsNoticeMarker)

	r2, _, err := h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/page-b"})
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

	h := newTestHandler(t, srv.Client(), withCheckLLMs(true))
	target := srv.URL + "/page"

	r1, _, err := h.handle(t.Context(), nil, FetchInput{URL: target})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, r1), llmsNoticeMarker,
		"first fetch (content cache miss) must include the notice")

	r2, _, err := h.handle(t.Context(), nil, FetchInput{URL: target})
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

	h := newTestHandler(t, srv.Client(), withCheckLLMs(true))

	_, _, err := h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/page-a"})
	require.NoError(t, err)

	_, _, err = h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/page-b"})
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

	h := newTestHandler(t, srv.Client(), withCheckLLMs(true))

	result, _, err := h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/llms.txt"})
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

	h := newTestHandler(t, srv.Client(), withCheckLLMs(true))

	result, _, err := h.handle(t.Context(), nil, FetchInput{
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

	h := newTestHandler(t, srv.Client(), withCheckLLMs(false))

	result, _, err := h.handle(t.Context(), nil, FetchInput{URL: srv.URL + "/page"})
	require.NoError(t, err)

	assert.NotContains(t, resultText(t, result), llmsNoticeMarker)
	assert.Equal(t, int32(0), probeCount.Load(),
		"discovery disabled must not trigger any probe")
}
