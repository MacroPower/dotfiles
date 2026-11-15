package render_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime/pprof"
	"strings"
	"sync"
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

// logBuffer is a log sink that a test can read while the goroutines of
// a render still write to it.
type logBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p) //nolint:wrapcheck // A bytes.Buffer write returns a nil error.
}

// entries decodes the JSON log lines whose message is msg.
func (b *logBuffer) entries(t *testing.T, msg string) []map[string]any {
	t.Helper()

	b.mu.Lock()
	defer b.mu.Unlock()

	var out []map[string]any

	for line := range strings.Lines(b.buf.String()) {
		var entry map[string]any

		require.NoError(t, json.Unmarshal([]byte(line), &entry), "log line: %s", line)

		if entry["msg"] == msg {
			out = append(out, entry)
		}
	}

	return out
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
			assert.Contains(t, string(out.HTML), tc.want)
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
	assert.Contains(t, string(out.HTML), "FromScript")
	assert.Equal(t, "test-ua", gotUserAgent.Load(),
		"subresource requests carry the configured User-Agent")
}

func TestRenderSourceMapComment(t *testing.T) {
	t.Parallel()

	// Each case fills the root element from a script that ends in a
	// source map comment. No script text contains the filled element,
	// so only a script that ran produces it.
	const (
		fill   = `document.getElementById("root").textContent = "Filled";`
		filled = `<div id="root">Filled</div>`
		mapRef = "\n//# sourceMappingURL=app.js.map"
	)

	tests := map[string]struct {
		// scripts maps a path to the JavaScript that the server
		// returns for it.
		scripts map[string]string
		body    string
	}{
		"external script": {
			body:    `<script src="/static/app.js"></script>`,
			scripts: map[string]string{"/static/app.js": fill + mapRef},
		},
		"external script with a final newline": {
			body:    `<script src="/static/app.js"></script>`,
			scripts: map[string]string{"/static/app.js": fill + mapRef + "\n"},
		},
		"external script with an absolute map URL": {
			body: `<script src="/static/app.js"></script>`,
			scripts: map[string]string{
				"/static/app.js": fill + "\n//# sourceMappingURL=https://cdn.example.com/app.js.map",
			},
		},
		"inline script": {
			body: `<script>` + fill + mapRef + `</script>`,
		},
		"inline script with a file map URL": {
			body: `<script>` + fill + "\n//# sourceMappingURL=file:///nonexistent/app.js.map</script>",
		},
		"inline script with a malformed data URL": {
			body: `<script>` + fill + "\n//# sourceMappingURL=data:application/json;base64,!!!</script>",
		},
		"eval": {
			body: `<script>eval("document.getElementById('root').textContent = 'Filled';` +
				`\n//# sourceMappingURL=app.js.map");</script>`,
		},
		"Function constructor": {
			body: `<script>new Function("document.getElementById('root').textContent = 'Filled';` +
				`\n//# sourceMappingURL=app.js.map")();</script>`,
		},
		"module script": {
			body:    `<script type="module" src="/static/app.js"></script>`,
			scripts: map[string]string{"/static/app.js": fill + mapRef},
		},
		"imported module": {
			body: `<script type="module" src="/static/app.js"></script>`,
			scripts: map[string]string{
				"/static/app.js": `import { text } from "./dep.js";` +
					`document.getElementById("root").textContent = text;`,
				"/static/dep.js": `export const text = "Filled";` + mapRef,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var mapRequests atomic.Int64

			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".map") {
					mapRequests.Add(1)
				}

				http.NotFound(w, r)
			})

			for path, src := range tc.scripts {
				mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "text/javascript")
					mustWrite(w, []byte(src))
				})
			}

			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			seed := page(`<div id="root"></div>` + tc.body)

			out, err := newRenderer(t).Render(t.Context(), srv.URL+pagePath, seed)
			require.NoError(t, err)
			assert.Contains(t, string(out.HTML), filled)
			assert.Zero(t, mapRequests.Load(), "a render must not request a source map")
		})
	}
}

func TestRenderFetchReturnsScriptSource(t *testing.T) {
	t.Parallel()

	// The renderer changes only the copy of a script that the engine
	// downloads to run. A page that reads the same script with fetch
	// receives the bytes that the server sent.
	src := `window.appLoaded = true;` + "\n//# sourceMappingURL=app.js.map"

	mux := http.NewServeMux()
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		mustWrite(w, []byte(src))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	seed := page(`<div id="root"></div><script src="/app.js"></script>` +
		`<script>fetch("/app.js").then(r => r.text()).then(t => {` +
		`document.getElementById("root").textContent = window.appLoaded + ":" + t.length; });</script>`)

	out, err := newRenderer(t).Render(t.Context(), srv.URL+pagePath, seed)
	require.NoError(t, err)
	assert.Contains(t, string(out.HTML), fmt.Sprintf(`<div id="root">true:%d</div>`, len(src)))
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
	assert.Contains(t, string(out.HTML), "FetchedData")
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

	denied, err := rules.Compile(rules.File{
		Reason: "test",
		Deny: []rules.DenyRule{{
			URLMatch: rules.URLMatch{Path: `^/blocked\.js$`},
			Reason:   "blocked by test",
		}},
	})
	require.NoError(t, err)

	seed := page(`<div id="root"></div>` +
		`<script src="/blocked.js"></script>` +
		`<script>document.getElementById("root").textContent = "StillRendered";</script>`)

	out, err := newRenderer(t, render.WithRules(denied)).
		Render(t.Context(), srv.URL+pagePath, seed)
	require.NoError(t, err)
	assert.Contains(t, string(out.HTML), "StillRendered")
	assert.NotContains(t, string(out.HTML), "ShouldNotAppear")
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
	assert.Contains(t, string(out.HTML), "StillRendered")
	assert.NotContains(t, string(out.HTML), "ShouldNotAppear")
}

func TestRenderSubresourceByteCap(t *testing.T) {
	t.Parallel()

	// A script over the byte cap fails to load outright (truncating it
	// mid-token would execute corrupted JavaScript), so nothing is
	// injected; the page itself still renders.
	const byteCap = 40

	mux := http.NewServeMux()
	mux.HandleFunc("/big.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		mustWrite(w, []byte(`document.body.innerHTML += "<p>ShouldNotAppear</p>";`))
	})
	mux.HandleFunc("/small.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		mustWrite(w, []byte(`window.small = "SmallLoaded";`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// The page requests the oversized script twice.
	seed := page(`<div id="root"></div>` +
		`<script src="/big.js"></script>` +
		`<script src="/small.js"></script>` +
		`<script src="/big.js"></script>` +
		`<script>document.getElementById("root").textContent = "StillRendered:" + window.small;</script>`)

	var logs logBuffer

	out, err := newRenderer(t,
		render.WithMaxSubresourceBytes(byteCap),
		render.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))),
	).Render(t.Context(), srv.URL+pagePath, seed)
	require.NoError(t, err)
	assert.Contains(t, string(out.HTML), "StillRendered:SmallLoaded")
	assert.NotContains(t, string(out.HTML), "ShouldNotAppear")
	assert.Equal(t, []string{srv.URL + "/big.js"}, out.Oversized,
		"the result names each skipped subresource one time")

	// The browser logs the skipped script only as a 403 status. The
	// renderer names the cap at a level that the default handler keeps.
	entries := logs.entries(t, "render: subresource exceeds byte cap")
	require.Len(t, entries, 2, "one log line for each refused request")

	for _, entry := range entries {
		assert.Equal(t, "WARN", entry["level"])
		assert.Equal(t, srv.URL+"/big.js", entry["url"])
		assert.Equal(t, srv.URL+pagePath, entry["page"])
		assert.InDelta(t, byteCap, entry["limit_bytes"], 0)
	}
}

func TestRenderDefaultSubresourceByteCap(t *testing.T) {
	t.Parallel()

	// Each script fills the root element, and a comment pads the script
	// to the size of the case.
	const (
		fill   = `document.getElementById("root").textContent = "Filled";`
		filled = `<div id="root">Filled</div>`
	)

	limit := render.New(&http.Client{}).MaxSubresourceBytes()

	tests := map[string]struct {
		size int64
		want bool
	}{
		"bundle of 3 MiB loads": {
			size: 3 << 20,
			want: true,
		},
		"bundle of the cap size loads": {
			size: limit,
			want: true,
		},
		"bundle one byte larger than the cap is skipped": {
			size: limit + 1,
			want: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			padding := int(tc.size) - len(fill) - len("/**/")
			script := fill + "/*" + strings.Repeat("x", padding) + "*/"
			require.Len(t, script, int(tc.size))

			mux := http.NewServeMux()
			mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/javascript")
				mustWrite(w, []byte(script))
			})

			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			seed := page(`<div id="root"></div><script src="/bundle.js"></script>`)

			// The budget leaves room for a large download on a busy host.
			out, err := newRenderer(t, render.WithBudget(time.Minute)).
				Render(t.Context(), srv.URL+pagePath, seed)
			require.NoError(t, err)

			if tc.want {
				assert.Contains(t, string(out.HTML), filled)
				assert.Empty(t, out.Oversized)
			} else {
				assert.NotContains(t, string(out.HTML), filled)
				assert.Equal(t, []string{srv.URL + "/bundle.js"}, out.Oversized)
			}
		})
	}
}

func TestRenderFailureReportsOversized(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/big.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		mustWrite(w, []byte(`document.body.innerHTML += "<p>ShouldNotAppear</p>";`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// The render skips the script and then runs out of budget in the
	// loop. The result of the failed render still names the script.
	seed := page(`<script src="/big.js"></script><script>while (true) {}</script>`)

	out, err := newRenderer(t,
		render.WithBudget(time.Second),
		render.WithMaxSubresourceBytes(10),
	).Render(t.Context(), srv.URL+pagePath, seed)
	require.ErrorIs(t, err, render.ErrRenderFailed)
	assert.Nil(t, out.HTML)
	assert.Equal(t, []string{srv.URL + "/big.js"}, out.Oversized)
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
	assert.Contains(t, string(out.HTML), "StillRendered")
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
	assert.Contains(t, string(out.HTML), "seed copy")
	assert.Zero(t, pageRequests.Load(), "top-level document must come from the seed")
}
