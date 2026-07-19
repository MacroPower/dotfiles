package render_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/render"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/rules"
)

const pagePath = "/page"

// newRenderer builds a renderer with a short default budget so
// failure-path tests stay fast.
func newRenderer(t *testing.T, opts ...render.Option) *render.Renderer {
	t.Helper()

	client := &http.Client{Timeout: 30 * time.Second}
	opts = append([]render.Option{render.WithBudget(5 * time.Second)}, opts...)

	return render.New(client, opts...)
}

func page(body string) render.Seed {
	return render.Seed{
		Body:        []byte("<html><body>" + body + "</body></html>"),
		ContentType: "text/html",
	}
}

func mustWrite(w http.ResponseWriter, data []byte) {
	_, err := w.Write(data)
	if err != nil {
		panic(fmt.Sprintf("writing response: %v", err))
	}
}

func TestRenderInlineScripts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		script string
		want   string
	}{
		"dom injection": {
			script: `document.getElementById("root").innerHTML = "<h1>Injected</h1>";`,
			want:   "Injected",
		},
		"setTimeout content": {
			script: `setTimeout(() => { document.getElementById("root").textContent = "Delayed"; }, 250);`,
			want:   "Delayed",
		},
		"throwing script keeps DOM": {
			script: `document.getElementById("root").textContent = "BeforeThrow"; throw new Error("boom");`,
			want:   "BeforeThrow",
		},
		"setInterval page": {
			script: `document.getElementById("root").textContent = "IntervalPage"; setInterval(() => {}, 10);`,
			want:   "IntervalPage",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.NotFoundHandler())
			t.Cleanup(srv.Close)

			seed := page(`<div id="root"></div><script>` + tc.script + `</script>`)

			out, err := newRenderer(t).Render(t.Context(), srv.URL+pagePath, seed)
			require.NoError(t, err)
			assert.Contains(t, string(out), tc.want)
		})
	}
}

func TestRenderExternalScript(t *testing.T) {
	t.Parallel()

	var gotUserAgent atomic.Value

	mux := http.NewServeMux()
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent.Store(r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "text/javascript")
		mustWrite(w, []byte(`document.body.innerHTML += "<p>FromScript</p>";`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	seed := page(`<div id="root"></div><script src="/app.js"></script>`)

	out, err := newRenderer(t, render.WithUserAgent("test-ua")).
		Render(t.Context(), srv.URL+pagePath, seed)
	require.NoError(t, err)
	assert.Contains(t, string(out), "FromScript")
	assert.Equal(t, "test-ua", gotUserAgent.Load(),
		"subresource requests carry the configured User-Agent")
}

func TestRenderFetchDrivenContent(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/data", func(w http.ResponseWriter, _ *http.Request) {
		mustWrite(w, []byte("FetchedData"))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	seed := page(`<div id="root"></div>` +
		`<script>fetch("/data").then(r => r.text()).then(t => {` +
		`document.getElementById("root").textContent = t; });</script>`)

	out, err := newRenderer(t).Render(t.Context(), srv.URL+pagePath, seed)
	require.NoError(t, err)
	assert.Contains(t, string(out), "FetchedData")
}

func TestRenderInfiniteLoopTimesOut(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	seed := page(`<script>while (true) {}</script>`)

	start := time.Now()

	_, err := newRenderer(t, render.WithBudget(2*time.Second)).
		Render(t.Context(), srv.URL+pagePath, seed)
	require.ErrorIs(t, err, render.ErrRenderFailed)
	assert.Less(t, time.Since(start), 10*time.Second)

	// Budget expiry interrupts the script VM, so the render goroutine
	// must exit rather than spin in the loop for the life of the
	// process. Other parallel render tests may hold sobek goroutines
	// transiently; Eventually outlasts them.
	assert.Eventually(t, func() bool { return sobekGoroutines(t) == 0 },
		15*time.Second, 100*time.Millisecond,
		"interrupted render goroutine must exit, not keep executing the loop")
}

// sobekGoroutines counts live goroutines with a sobek frame anywhere in
// their stack, via the goroutine profile.
func sobekGoroutines(t *testing.T) int {
	t.Helper()

	var buf bytes.Buffer

	require.NoError(t, pprof.Lookup("goroutine").WriteTo(&buf, 2))

	count := 0

	for stack := range strings.SplitSeq(buf.String(), "\n\n") {
		if strings.Contains(stack, "github.com/grafana/sobek") {
			count++
		}
	}

	return count
}

func TestRenderRulesDeniedSubresource(t *testing.T) {
	t.Parallel()

	var blockedRequests atomic.Int64

	mux := http.NewServeMux()
	mux.HandleFunc("/blocked.js", func(w http.ResponseWriter, _ *http.Request) {
		blockedRequests.Add(1)
		w.Header().Set("Content-Type", "text/javascript")
		mustWrite(w, []byte(`document.body.innerHTML += "<p>ShouldNotAppear</p>";`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	denied, err := rules.Compile("test", []rules.DenyRule{{
		URLMatch: rules.URLMatch{Path: `^/blocked\.js$`},
		Reason:   "blocked by test",
	}}, nil)
	require.NoError(t, err)

	seed := page(`<div id="root"></div>` +
		`<script src="/blocked.js"></script>` +
		`<script>document.getElementById("root").textContent = "StillRendered";</script>`)

	out, err := newRenderer(t, render.WithRules(denied)).
		Render(t.Context(), srv.URL+pagePath, seed)
	require.NoError(t, err)
	assert.Contains(t, string(out), "StillRendered")
	assert.NotContains(t, string(out), "ShouldNotAppear")
	assert.Zero(t, blockedRequests.Load(), "denied subresource must not be requested")
}

func TestRenderSubresourceLimit(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		mustWrite(w, []byte(`document.body.innerHTML += "<p>ShouldNotAppear</p>";`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	seed := page(`<div id="root"></div>` +
		`<script src="/app.js"></script>` +
		`<script>document.getElementById("root").textContent = "StillRendered";</script>`)

	out, err := newRenderer(t, render.WithMaxSubresources(0)).
		Render(t.Context(), srv.URL+pagePath, seed)
	require.NoError(t, err)
	assert.Contains(t, string(out), "StillRendered")
	assert.NotContains(t, string(out), "ShouldNotAppear")
}

func TestRenderSubresourceByteCap(t *testing.T) {
	t.Parallel()

	// A script over the byte cap fails to load outright (truncating it
	// mid-token would execute corrupted JavaScript), so nothing is
	// injected; the page itself still renders.
	script := `document.body.innerHTML += "<p>ShouldNotAppear</p>";`

	mux := http.NewServeMux()
	mux.HandleFunc("/big.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		mustWrite(w, []byte(script))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	seed := page(`<div id="root"></div>` +
		`<script src="/big.js"></script>` +
		`<script>document.getElementById("root").textContent = "StillRendered";</script>`)

	out, err := newRenderer(t, render.WithMaxSubresourceBytes(10)).
		Render(t.Context(), srv.URL+pagePath, seed)
	require.NoError(t, err)
	assert.Contains(t, string(out), "StillRendered")
	assert.NotContains(t, string(out), "ShouldNotAppear")
}

func TestRenderSubresourcePOSTBlocked(t *testing.T) {
	t.Parallel()

	var posts atomic.Int64

	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}

		mustWrite(w, []byte("ok"))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	seed := page(`<div id="root"></div>` +
		`<script>fetch("/api", { method: "POST", body: "x" }).catch(function () {});` +
		`document.getElementById("root").textContent = "StillRendered";</script>`)

	out, err := newRenderer(t).Render(t.Context(), srv.URL+pagePath, seed)
	require.NoError(t, err)
	assert.Contains(t, string(out), "StillRendered")
	assert.Zero(t, posts.Load(), "state-changing subresource requests must not reach the network")
}

func TestRenderSeedServedWithoutRefetch(t *testing.T) {
	t.Parallel()

	var pageRequests atomic.Int64

	mux := http.NewServeMux()
	mux.HandleFunc(pagePath, func(w http.ResponseWriter, _ *http.Request) {
		pageRequests.Add(1)
		mustWrite(w, []byte("<html><body>network copy</body></html>"))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	seed := page(`<p>seed copy</p>`)

	out, err := newRenderer(t).Render(t.Context(), srv.URL+pagePath, seed)
	require.NoError(t, err)
	assert.Contains(t, string(out), "seed copy")
	assert.Zero(t, pageRequests.Load(), "top-level document must come from the seed")
}
