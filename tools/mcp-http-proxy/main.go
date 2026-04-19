package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	version        = "0.1.0"
	connectTimeout = 30 * time.Second
)

// headerFlag collects repeatable --header K=V flags.
type headerFlag []string

func (h *headerFlag) String() string { return strings.Join(*h, ",") }

func (h *headerFlag) Set(v string) error {
	*h = append(*h, v)
	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

// run parses args and runs the proxy until ctx is cancelled or the local
// transport closes. localTransport is the stdio (or in-memory, in tests)
// transport used for the proxy's MCP server side.
func run(ctx context.Context, args []string, localTransport mcp.Transport) error {
	fs := flag.NewFlagSet("mcp-http-proxy", flag.ContinueOnError)
	url := fs.String("url", "", "upstream Streamable HTTP MCP endpoint")
	logFile := fs.String("log-file", "", "path to JSON log file (append)")

	var headers headerFlag
	fs.Var(&headers, "header", "HTTP header K=V (repeatable; values are expanded with os.ExpandEnv)")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}

	if *url == "" {
		return errors.New("--url is required")
	}

	parsedHeaders, err := parseHeaders(headers)
	if err != nil {
		return fmt.Errorf("parsing headers: %w", err)
	}

	logger, closeLog, err := openLogger(*logFile)
	if err != nil {
		return err
	}
	defer closeLog()

	httpClient := &http.Client{
		Transport: &headerTransport{base: http.DefaultTransport, headers: parsedHeaders},
	}

	upstreamTransport := &mcp.StreamableClientTransport{
		Endpoint:   *url,
		HTTPClient: httpClient,
	}

	client := mcp.NewClient(
		&mcp.Implementation{Name: "mcp-http-proxy", Version: version},
		&mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}},
	)

	connectCtx, cancelConnect := context.WithTimeout(ctx, connectTimeout)
	cs, err := client.Connect(connectCtx, upstreamTransport, nil)
	cancelConnect()
	if err != nil {
		return fmt.Errorf("connecting upstream: %w", err)
	}
	defer func() { _ = cs.Close() }()

	srv := newProxyServer(cs, cs.InitializeResult(), logger)

	if err := srv.Run(ctx, localTransport); err != nil {
		return fmt.Errorf("running stdio server: %w", err)
	}
	return nil
}

// parseHeaders parses "K=V" items, applying os.ExpandEnv to each value.
func parseHeaders(raw []string) (http.Header, error) {
	h := http.Header{}
	for _, item := range raw {
		k, v, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("%w: %q", errBadHeader, item)
		}
		k = strings.TrimSpace(k)
		if k == "" {
			return nil, fmt.Errorf("%w: %q", errBadHeader, item)
		}
		h.Add(k, os.ExpandEnv(v))
	}
	return h, nil
}

var errBadHeader = errors.New("header must be K=V")

// headerTransport wraps an [http.RoundTripper], adding fixed headers to every
// request. Used to thread auth tokens through to the upstream endpoint.
type headerTransport struct {
	base    http.RoundTripper
	headers http.Header
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Set (not Add) so user-supplied headers reliably override any baseline
	// values the SDK's transport may have written.
	for k, vs := range t.headers {
		req.Header[k] = append([]string(nil), vs...)
	}
	return t.base.RoundTrip(req)
}

// newProxyServer builds an [*mcp.Server] whose handlers forward incoming MCP
// calls to cs. Upstream capabilities are copied onto the local server so the
// stdio client sees the same feature set.
func newProxyServer(cs *mcp.ClientSession, init *mcp.InitializeResult, logger *slog.Logger) *mcp.Server {
	opts := &mcp.ServerOptions{}
	if init != nil {
		opts.Capabilities = init.Capabilities
	}

	if opts.Capabilities != nil && opts.Capabilities.Completions != nil {
		opts.CompletionHandler = func(ctx context.Context, req *mcp.CompleteRequest) (res *mcp.CompleteResult, err error) {
			defer logForward(logger, "completion/complete", time.Now(), &err)
			return cs.Complete(ctx, req.Params)
		}
	}

	if opts.Capabilities != nil && opts.Capabilities.Resources != nil && opts.Capabilities.Resources.Subscribe {
		opts.SubscribeHandler = func(ctx context.Context, req *mcp.SubscribeRequest) (err error) {
			defer logForward(logger, "resources/subscribe", time.Now(), &err)
			return cs.Subscribe(ctx, req.Params)
		}
		opts.UnsubscribeHandler = func(ctx context.Context, req *mcp.UnsubscribeRequest) (err error) {
			defer logForward(logger, "resources/unsubscribe", time.Now(), &err)
			return cs.Unsubscribe(ctx, req.Params)
		}
	}

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "mcp-http-proxy", Version: version},
		opts,
	)
	srv.AddReceivingMiddleware(forwardingMiddleware(cs, logger))
	return srv
}

// forwardingMiddleware replaces the server's default receiving handler for
// the MCP methods we proxy. Unknown methods (ping, initialize, notifications)
// fall through to next so the SDK's default handling runs.
func forwardingMiddleware(cs *mcp.ClientSession, logger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (res mcp.Result, err error) {
			switch method {
			case "tools/list":
				defer logForward(logger, method, time.Now(), &err)
				return cs.ListTools(ctx, req.GetParams().(*mcp.ListToolsParams))
			case "tools/call":
				defer logForward(logger, method, time.Now(), &err)
				// CallToolParamsRaw (incoming) carries Arguments as json.RawMessage;
				// CallToolParams (outbound) takes any. Assigning the RawMessage to
				// any preserves the exact wire bytes through re-marshal, since
				// json.RawMessage.MarshalJSON returns itself.
				raw := req.GetParams().(*mcp.CallToolParamsRaw)
				return cs.CallTool(ctx, &mcp.CallToolParams{
					Meta:      raw.Meta,
					Name:      raw.Name,
					Arguments: raw.Arguments,
				})
			case "resources/list":
				defer logForward(logger, method, time.Now(), &err)
				return cs.ListResources(ctx, req.GetParams().(*mcp.ListResourcesParams))
			case "resources/read":
				defer logForward(logger, method, time.Now(), &err)
				return cs.ReadResource(ctx, req.GetParams().(*mcp.ReadResourceParams))
			case "resources/templates/list":
				defer logForward(logger, method, time.Now(), &err)
				return cs.ListResourceTemplates(ctx, req.GetParams().(*mcp.ListResourceTemplatesParams))
			case "prompts/list":
				defer logForward(logger, method, time.Now(), &err)
				return cs.ListPrompts(ctx, req.GetParams().(*mcp.ListPromptsParams))
			case "prompts/get":
				defer logForward(logger, method, time.Now(), &err)
				return cs.GetPrompt(ctx, req.GetParams().(*mcp.GetPromptParams))
			default:
				return next(ctx, method, req)
			}
		}
	}
}

// logForward emits one JSON line per proxied call with its method, duration,
// and error (if any). Intended for `defer logForward(...)` at the top of each
// forward path.
func logForward(logger *slog.Logger, method string, start time.Time, errp *error) {
	attrs := []any{
		"method", method,
		"duration_ms", time.Since(start).Milliseconds(),
	}
	if errp != nil && *errp != nil {
		attrs = append(attrs, "error", (*errp).Error())
	}
	logger.Info("forward", attrs...)
}

// openLogger opens a JSON log file, or returns a discarding logger when path
// is empty. The second return value closes the file; callers should defer it.
func openLogger(path string) (*slog.Logger, func(), error) {
	if path == "" {
		return slog.New(slog.DiscardHandler), func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening %s: %w", path, err)
	}
	return slog.New(slog.NewJSONHandler(f, nil)), func() { _ = f.Close() }, nil
}
