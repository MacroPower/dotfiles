package httpfake

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Request is one request that a fake server received. Body holds the
// decoded JSON body, and it is nil for an empty body or a JSON null. Query is
// the raw query string of the URL, and Auth is the Authorization header.
type Request struct {
	Body   map[string]any
	Method string
	Path   string
	Query  string
	Auth   string
}

// Recorder collects the requests of a fake server. Its zero value is ready
// to use, and the handlers of a server can share one Recorder.
type Recorder struct {
	requests []Request
	mu       sync.Mutex
}

// Record reads req into a [Request], appends it to the recorder, and
// returns it. A body that does not read, or that holds JSON other than an
// object or null, fails t.
func (r *Recorder) Record(t *testing.T, req *http.Request) Request {
	t.Helper()

	raw, err := io.ReadAll(req.Body)
	require.NoError(t, err)

	out := Request{
		Method: req.Method,
		Path:   req.URL.Path,
		Query:  req.URL.RawQuery,
		Auth:   req.Header.Get("Authorization"),
	}

	if len(raw) > 0 {
		require.NoError(t, json.Unmarshal(raw, &out.Body))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.requests = append(r.requests, out)

	return out
}

// All returns a copy of every recorded request, in the order of arrival.
func (r *Recorder) All() []Request {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.requests)
}

// Calls returns the recorded requests for a method and path.
func (r *Recorder) Calls(method, path string) []Request {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []Request

	for _, req := range r.requests {
		if req.Method == method && req.Path == path {
			out = append(out, req)
		}
	}

	return out
}

// WriteJSON answers with status and body under the JSON content type. A
// failed write fails t.
func WriteJSON(t *testing.T, w http.ResponseWriter, status int, body string) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_, err := io.WriteString(w, body)
	require.NoError(t, err)
}

// Serve starts a fake server that answers every request with one status
// and body, and that records the requests. Each request must carry
// userAgent in its User-Agent header, or the test fails. Serve returns the
// URL of the server and its [Recorder], and it closes the server when the
// test ends.
func Serve(t *testing.T, userAgent string, status int, body string) (string, *Recorder) {
	t.Helper()

	rec := &Recorder{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.Record(t, r)
		assert.Equal(t, userAgent, r.Header.Get("User-Agent"))
		WriteJSON(t, w, status, body)
	}))
	t.Cleanup(srv.Close)

	return srv.URL, rec
}
