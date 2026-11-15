package render

import (
	"io"
	"net/http"
	"strings"

	"github.com/grafana/sobek"
	"github.com/grafana/sobek/parser"
)

// sourceMapGuard is the line that [sourceMapTransport] appends to a
// script. A script runs the same with an empty comment after it.
const sourceMapGuard = "\n//"

// disableSourceMaps stops vm from loading the source map that a script
// names in a `//# sourceMappingURL=` comment. sobek loads the map when
// it compiles the script, and a map that does not load fails the script
// with a SyntaxError. sobek loads a map from a data URL or from a local
// file, but not over HTTP. The map of a bundle from the web therefore
// never loads. A render does not use source maps, and a page must not
// make the renderer open local files.
//
// The setting covers the code that vm compiles itself: inline scripts,
// external classic scripts, and the strings that eval and the Function
// constructor receive. [sourceMapTransport] covers modules.
func disableSourceMaps(vm *sobek.Runtime) {
	vm.SetParserOptions(parser.WithDisableSourceMaps)
}

// sourceMapTransport appends [sourceMapGuard] to every response body,
// so sobek finds no source map comment in a script that the engine
// downloads. sobek reads the comment only from the last line of a
// source that is neither blank nor `})`, and the guard becomes that
// line.
//
// The guard exists for modules. gost-dom parses a module with
// sobek.ParseModule and passes no parser options, so
// [disableSourceMaps] does not reach it. The guard adds a line instead
// of removing the comment, so every byte that the server sent stays in
// place.
type sourceMapTransport struct {
	inner http.RoundTripper
}

func (t sourceMapTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	inner := t.inner
	if inner == nil {
		inner = http.DefaultTransport
	}

	resp, err := inner.RoundTrip(req)
	if err != nil {
		return nil, err //nolint:wrapcheck // The transport error reaches the client unchanged.
	}

	resp.Body = guardedBody{
		Reader: io.MultiReader(resp.Body, strings.NewReader(sourceMapGuard)),
		Closer: resp.Body,
	}

	// The guard makes the body longer than a length that the server
	// declared. The Content-Length header stays as it is. The transport
	// that gost-dom builds around a handler shares the header map with
	// the goroutine that runs the handler.
	resp.ContentLength = -1

	return resp, nil
}

// guardedBody reads a response body followed by [sourceMapGuard] and
// closes the response body.
type guardedBody struct {
	io.Reader
	io.Closer
}
