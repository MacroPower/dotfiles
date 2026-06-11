package llmstxt_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/llmstxt"
)

func TestFind(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status      int
		contentType string
		body        string
		wantFound   bool
	}{
		"present with text/markdown": {
			status:      http.StatusOK,
			contentType: "text/markdown",
			body:        "# llms.txt\n",
			wantFound:   true,
		},
		"present with text/plain": {
			status:      http.StatusOK,
			contentType: "text/plain",
			body:        "# llms.txt\n",
			wantFound:   true,
		},
		"present with no content-type": {
			status:    http.StatusOK,
			body:      "# llms.txt\n",
			wantFound: true,
		},
		"missing returns 404": {
			status:    http.StatusNotFound,
			wantFound: false,
		},
		"html content-type rejected": {
			status:      http.StatusOK,
			contentType: "text/html; charset=utf-8",
			body:        "<html><body>SPA fallback</body></html>",
			wantFound:   false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.contentType != "" {
					w.Header().Set("Content-Type", tt.contentType)
				}

				w.WriteHeader(tt.status)

				if tt.body != "" {
					mustWrite(w, []byte(tt.body))
				}
			}))
			t.Cleanup(srv.Close)

			f := llmstxt.New(srv.Client(), "test-agent")

			got := f.Find(t.Context(), srv.URL)

			if tt.wantFound {
				assert.Equal(t, srv.URL+"/llms.txt", got)
			} else {
				assert.Empty(t, got)
			}
		})
	}
}

func TestFindCachesResult(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status int
		want   func(origin string) string
	}{
		"positive result cached": {
			status: http.StatusOK,
			want:   func(origin string) string { return origin + "/llms.txt" },
		},
		"negative result cached": {
			status: http.StatusNotFound,
			want:   func(string) string { return "" },
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var probes atomic.Int32

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				probes.Add(1)
				w.Header().Set("Content-Type", "text/markdown")
				w.WriteHeader(tt.status)
				mustWrite(w, []byte("# llms.txt\n"))
			}))
			t.Cleanup(srv.Close)

			f := llmstxt.New(srv.Client(), "test-agent")

			first := f.Find(t.Context(), srv.URL)
			second := f.Find(t.Context(), srv.URL)

			assert.Equal(t, tt.want(srv.URL), first)
			assert.Equal(t, first, second)
			assert.Equal(t, int32(1), probes.Load(), "second lookup must reuse the cache")
		})
	}
}

func mustWrite(w http.ResponseWriter, data []byte) {
	_, err := w.Write(data)
	if err != nil {
		panic(fmt.Sprintf("writing response: %v", err))
	}
}
