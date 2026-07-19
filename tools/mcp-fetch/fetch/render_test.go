package fetch_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/fetch"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/render"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/store"
)

const (
	renderFallbackNotice = "<javascript rendering failed; returning non-rendered content>"
	renderHintNotice     = "<page appears to require JavaScript; retry with render_js=true to execute it>"

	// spaInjectedText is long enough that the rendered page no longer
	// counts as a JS shell. It stays on one line because the fixtures
	// embed it in a JavaScript string literal.
	spaInjectedText = `Rendered client side: the quick brown fox jumps over the lazy dog while thirty plus words of meaningful article content appear in the document body only after the script has run to completion in the browser.`
)

// newSPAServer serves a client-rendered shell page whose content is
// injected by an inline script, plus supporting fixtures.
func newSPAServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var requests atomic.Int64

	shell := `<!DOCTYPE html><html><head><title>SPA</title></head><body>` +
		`<div id="root"></div>` +
		`<script>document.getElementById("root").innerHTML = "<p>` + spaInjectedText + `</p>";</script>` +
		`</body></html>`

	mux := http.NewServeMux()
	mux.HandleFunc("/spa", func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		mustWrite(w, []byte(shell))
	})
	mux.HandleFunc("/hang", func(w http.ResponseWriter, _ *http.Request) {
		// Budget expiry interrupts the script VM, so each render of
		// this page costs its render budget and the while(true) loop
		// then stops with the goroutine.
		requests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		mustWrite(w, []byte(`<html><body><p>Static copy</p><script>while (true) {}</script></body></html>`))
	})
	mux.HandleFunc("/text", func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		mustWrite(w, []byte("just text"))
	})
	mux.HandleFunc("/self-destruct", func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		mustWrite(w, []byte(`<html><body><article><h1>Server copy</h1><p>`+
			spaInjectedText+`</p></article>`+
			`<script>document.body.innerHTML = "";</script></body></html>`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, &requests
}

func TestHandleRenderJS(t *testing.T) {
	t.Parallel()

	srv, requests := newSPAServer(t)
	st := newTestStore(t)
	h := newTestHandler(t, srv.Client(), fetch.WithStore(st))

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/spa", RenderJS: true})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := resultText(t, result)
	assert.Contains(t, text, "Rendered client side")
	assert.NotContains(t, text, renderHintNotice)
	assert.NotContains(t, text, renderFallbackNotice)

	// A repeat request must hit the rendered cache slot, not the network.
	result, _, err = h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/spa", RenderJS: true})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, result), "Rendered client side")
	assert.Equal(t, int64(1), requests.Load(), "second render_js fetch must be served from cache")

	rows, err := st.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// Rows are newest first: the cache hit, then the rendered fetch.
	assert.Equal(t, 1, rows[0].RenderJS)
	assert.Equal(t, 1, rows[0].RenderOK)
	assert.Equal(t, 1, rows[0].CacheHit)
	assert.Equal(t, 1, rows[1].RenderJS)
	assert.Equal(t, 1, rows[1].RenderOK)
	assert.Equal(t, 0, rows[1].CacheHit)
}

func TestHandleRenderJSFallback(t *testing.T) {
	t.Parallel()

	srv, requests := newSPAServer(t)
	st := newTestStore(t)
	h := newTestHandler(t, srv.Client(),
		fetch.WithStore(st),
		fetch.WithRenderOptions(render.WithBudget(500*time.Millisecond)),
	)

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/hang", RenderJS: true})
	require.NoError(t, err)
	require.False(t, result.IsError, "a render failure must degrade, not error")

	text := resultText(t, result)
	assert.Contains(t, text, "Static copy")
	assert.Contains(t, text, renderFallbackNotice)
	assert.NotContains(t, text, renderHintNotice,
		"no retry hint when render_js was already requested")

	rows, err := st.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].RenderJS)
	assert.Equal(t, 0, rows[0].RenderOK)

	// The failure must not populate the rendered cache slot: a repeat
	// render_js call retries the render — from the cached fetch, with
	// no second network request. The fallback notice proves the retry
	// happened rather than the plain slot being served.
	result, _, err = h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/hang", RenderJS: true})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, result), renderFallbackNotice,
		"repeat render_js call must retry the render")
	assert.Equal(t, int64(1), requests.Load(), "render retry must reuse the cached fetch")

	// A render_js continuation pages over the degraded content from
	// the plain slot instead of burning another render budget; a
	// retry would take at least the full 500ms budget on this page.
	start := time.Now()
	result, _, err = h.Handle(t.Context(), nil, fetch.Input{
		URL: srv.URL + "/hang", RenderJS: true, StartIndex: 3,
	})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, result), "copy",
		"continuation must serve the degraded content")
	assert.Less(t, time.Since(start), 400*time.Millisecond,
		"continuation must not retry the render")

	// The plain conversion was cached under the base key, so a plain
	// fetch of the same page needs no network.
	result, _, err = h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/hang"})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, result), "Static copy")
	assert.Equal(t, int64(1), requests.Load(), "plain fetch must reuse the base cache entry")
}

func TestHandleRenderJSEmptyResultFallsBack(t *testing.T) {
	t.Parallel()

	srv, _ := newSPAServer(t)
	st := newTestStore(t)
	h := newTestHandler(t, srv.Client(), fetch.WithStore(st))

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{
		URL:      srv.URL + "/self-destruct",
		RenderJS: true,
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := resultText(t, result)
	assert.Contains(t, text, "Server copy",
		"empty rendered output must degrade to the plain conversion")
	assert.Contains(t, text, renderFallbackNotice)

	rows, err := st.RecentFetches(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].RenderJS)
	assert.Equal(t, 0, rows[0].RenderOK)
}

func TestHandleRenderJSNonHTML(t *testing.T) {
	t.Parallel()

	srv, requests := newSPAServer(t)
	st := newTestStore(t)
	h := newTestHandler(t, srv.Client(), fetch.WithStore(st))

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/text", RenderJS: true})
	require.NoError(t, err)

	text := resultText(t, result)
	assert.Contains(t, text, "just text")
	assert.NotContains(t, text, renderFallbackNotice)

	// A repeat request hits the cache and must still record that no
	// render produced the content.
	_, _, err = h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/text", RenderJS: true})
	require.NoError(t, err)

	rows, err := st.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, 1, rows[0].CacheHit)

	for _, row := range rows {
		assert.Equal(t, 1, row.RenderJS)
		assert.Equal(t, 0, row.RenderOK, "non-HTML content is never rendered")
	}

	// The render_js flag changes nothing about non-HTML content, so a
	// plain fetch of the same URL shares its cache entries.
	result, _, err = h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/text"})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, result), "just text")
	assert.Equal(t, int64(1), requests.Load(),
		"plain fetch after render_js of non-HTML must not refetch")
}

func TestHandleRenderJSGenericContentType(t *testing.T) {
	t.Parallel()

	// HTML served without a text/html Content-Type must still be
	// sniffed as HTML (by its doctype, case-insensitively) so
	// render_js runs instead of being silently skipped.
	shell := `<!DOCTYPE html><html><body><div id="root"></div>` +
		`<script>document.getElementById("root").innerHTML = "<p>` + spaInjectedText + `</p>";</script>` +
		`</body></html>`

	mux := http.NewServeMux()
	mux.HandleFunc("/spa", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		mustWrite(w, []byte(shell))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client())

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/spa", RenderJS: true})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, result), "Rendered client side")
	assert.NotContains(t, resultText(t, result), renderFallbackNotice)
}

func TestHandleRenderJSRawWins(t *testing.T) {
	t.Parallel()

	srv, _ := newSPAServer(t)
	st := newTestStore(t)
	h := newTestHandler(t, srv.Client(), fetch.WithStore(st))

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{
		URL:      srv.URL + "/spa",
		Raw:      true,
		RenderJS: true,
	})
	require.NoError(t, err)

	text := resultText(t, result)
	assert.Contains(t, text, "<script>", "raw mode must return the unrendered document")
	assert.NotContains(t, text, renderHintNotice)

	rows, err := st.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].RawMode)
	assert.Equal(t, 0, rows[0].RenderJS, "raw disables rendering, and the record shows the effective flag")
}

func TestHandleRenderHint(t *testing.T) {
	t.Parallel()

	srv, requests := newSPAServer(t)
	h := newTestHandler(t, srv.Client())

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/spa"})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, result), renderHintNotice)

	// The hint must survive a cache hit.
	result, _, err = h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/spa"})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, result), renderHintNotice)
	assert.Equal(t, int64(1), requests.Load())

	// Pagination continuations must not repeat the hint.
	result, _, err = h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/spa", StartIndex: 5})
	require.NoError(t, err)
	assert.NotContains(t, resultText(t, result), renderHintNotice)

	// A rendered fetch of the same page has real content and no hint.
	result, _, err = h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/spa", RenderJS: true})
	require.NoError(t, err)

	text := resultText(t, result)
	assert.Contains(t, text, "Rendered client side")
	assert.NotContains(t, text, renderHintNotice)
}

func TestHandleRenderJSRedirect(t *testing.T) {
	t.Parallel()

	// The page lives under /app/ and loads a relative script; rendering
	// only finds it when the renderer resolves against the final URL
	// after the redirect, not the original /old path.
	page := `<html><body><div id="root"></div>` +
		`<script src="app.js"></script></body></html>`

	mux := http.NewServeMux()
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})
	mux.HandleFunc("/app/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		mustWrite(w, []byte(page))
	})
	mux.HandleFunc("/app/app.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		mustWrite(w, []byte(`document.getElementById("root").innerHTML = "<p>`+spaInjectedText+`</p>";`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	h := newTestHandler(t, srv.Client())

	result, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/old", RenderJS: true})
	require.NoError(t, err)
	assert.Contains(t, resultText(t, result), "Rendered client side")
}

func TestHandleRenderRecordsOutcome(t *testing.T) {
	t.Parallel()

	srv, _ := newSPAServer(t)
	st := newTestStore(t)
	h := newTestHandler(t, srv.Client(), fetch.WithStore(st))

	_, _, err := h.Handle(t.Context(), nil, fetch.Input{URL: srv.URL + "/spa", RenderJS: true})
	require.NoError(t, err)

	rows, err := st.RecentFetches(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, store.OutcomeOK, rows[0].Outcome)
	assert.Equal(t, 1, rows[0].RenderJS)
	assert.Equal(t, 1, rows[0].RenderOK)
}
