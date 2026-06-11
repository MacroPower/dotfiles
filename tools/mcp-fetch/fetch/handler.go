package fetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/content"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/llmstxt"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/markdown"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/robots"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/rules"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/store"
)

const (
	defaultUserAgent = "MCP-Fetch"

	defaultMaxLength = 5000
	maxMaxLength     = 1_000_000
	maxResponseBytes = 10 * 1024 * 1024 // 10 MiB

	requestTimeout   = 30 * time.Second
	maxRedirects     = 10
	contentCacheSize = 64
	contentCacheTTL  = time.Hour

	// recordTimeout caps how long the deferred Record call will wait
	// when the request context has already been canceled. Detached
	// from the request via [context.WithoutCancel] so cancellation
	// does not lose the row.
	recordTimeout = 5 * time.Second
)

var (
	// ErrHTTPStatus is returned when the HTTP response has a status >= 400.
	ErrHTTPStatus = errors.New("HTTP error")

	// ErrBadScheme is returned when a URL uses an unsupported scheme.
	ErrBadScheme = errors.New("unsupported URL scheme")

	// ErrDenied is returned when a URL is blocked by the configured rules.
	ErrDenied = errors.New("URL denied by rules")

	// toolErrors are sentinel errors surfaced to the caller as tool-level
	// failures rather than internal errors.
	toolErrors = []error{
		ErrHTTPStatus,
		ErrBadScheme,
		ErrDenied,
		robots.ErrDisallowed,
	}
)

// Input is the input schema for the fetch tool.
type Input struct {
	URL        string `json:"url"                  jsonschema:"URL to fetch"`
	MaxLength  int    `json:"max_length,omitzero"  jsonschema:"Maximum number of characters to return (default 5000)"`
	StartIndex int    `json:"start_index,omitzero" jsonschema:"Character offset to start reading from (default 0)"`
	Raw        bool   `json:"raw,omitzero"         jsonschema:"Return raw content without HTML-to-Markdown conversion"`
	Pattern    string `json:"pattern,omitzero"     jsonschema:"RE2 regex; return only matching lines (grep-style), applied before start_index/max_length"`
	IgnoreCase bool   `json:"ignore_case,omitzero" jsonschema:"Case-insensitive pattern (grep -i)"`
	Context    int    `json:"context,omitzero"     jsonschema:"Lines of context around each match (grep -C, default 0)"`
	Invert     bool   `json:"invert,omitzero"      jsonschema:"Return non-matching lines (grep -v)"`
}

// Handler holds the shared state for the fetch tool handler.
type Handler struct {
	client       *http.Client
	transport    *http.Transport
	rules        *rules.Rules
	robots       *robots.Checker
	llms         *llmstxt.Finder
	store        *store.Store
	log          *slog.Logger
	contentCache *expirable.LRU[string, string]
	userAgent    string
	checkRobots  bool
	checkLLMs    bool
}

// Option configures a [Handler] via [New].
//
// Functions of this type:
//   - [WithUserAgent]
//   - [WithCheckRobots]
//   - [WithCheckLLMs]
//   - [WithRules]
//   - [WithStore]
//   - [WithLogger]
//   - [WithTransport]
//   - [WithClient]
type Option func(*Handler)

// WithUserAgent sets the User-Agent header. See [Option].
func WithUserAgent(ua string) Option {
	return func(h *Handler) { h.userAgent = ua }
}

// WithCheckRobots toggles robots.txt enforcement. See [Option].
func WithCheckRobots(check bool) Option {
	return func(h *Handler) { h.checkRobots = check }
}

// WithCheckLLMs toggles llms.txt discovery and notice. See [Option].
func WithCheckLLMs(check bool) Option {
	return func(h *Handler) { h.checkLLMs = check }
}

// WithRules sets the URL allow/deny rules. See [Option].
func WithRules(r *rules.Rules) Option {
	return func(h *Handler) { h.rules = r }
}

// WithStore enables SQLite recording of fetch results. A nil store
// disables recording. See [Option].
func WithStore(s *store.Store) Option {
	return func(h *Handler) { h.store = s }
}

// WithLogger sets the structured logger. See [Option].
func WithLogger(l *slog.Logger) Option {
	return func(h *Handler) { h.log = l }
}

// WithTransport sets the HTTP transport used to build the handler's
// client (e.g. to configure a proxy). Ignored when [WithClient] supplies
// a client. See [Option].
func WithTransport(t *http.Transport) Option {
	return func(h *Handler) { h.transport = t }
}

// WithClient injects a pre-built HTTP client instead of constructing one
// from a transport. [New] still installs the redirect policy on it. See
// [Option].
func WithClient(c *http.Client) Option {
	return func(h *Handler) { h.client = c }
}

// New constructs a [*Handler] from the given options. The content cache
// defaults to an in-memory expirable LRU and the logger defaults to
// discard, so a zero-option call returns a usable handler.
//
// New wires the HTTP client last: the redirect policy closes over the
// handler so every redirect hop re-runs URL validation and, on a
// cross-origin hop, the robots.txt check. The robots [*robots.Checker]
// and llms.txt [*llmstxt.Finder] share that same client so their probes
// obey the same redirect, proxy, and timeout policy.
func New(opts ...Option) *Handler {
	h := &Handler{
		userAgent:    defaultUserAgent,
		log:          slog.New(slog.DiscardHandler),
		contentCache: expirable.NewLRU[string, string](contentCacheSize, nil, contentCacheTTL),
		checkRobots:  true,
		checkLLMs:    true,
	}

	for _, opt := range opts {
		opt(h)
	}

	if h.client == nil {
		transport := h.transport
		if transport == nil {
			transport = &http.Transport{}
		}

		h.client = &http.Client{Transport: transport, Timeout: requestTimeout}
	}

	h.robots = robots.New(h.client, h.userAgent, robots.WithLogger(h.log))
	h.llms = llmstxt.New(h.client, h.userAgent, llmstxt.WithLogger(h.log))

	h.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errors.New("stopped after 10 redirects")
		}

		err := h.validateURL(req.URL)
		if err != nil {
			return err
		}

		if h.checkRobots && req.URL.Host != via[len(via)-1].URL.Host {
			return h.robots.Check(req.Context(), req.URL)
		}

		return nil
	}

	return h
}

// validateURL checks that the URL uses an allowed scheme and passes URL rules.
func (h *Handler) validateURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: %s", ErrBadScheme, u.Scheme)
	}

	if reason := h.rules.Check(u); reason != "" {
		return fmt.Errorf("%w: %s", ErrDenied, reason)
	}

	return nil
}

// Handle is the MCP tool handler for "fetch".
func (h *Handler) Handle(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input Input,
) (*mcp.CallToolResult, any, error) {
	maxLength := input.MaxLength
	if maxLength <= 0 {
		maxLength = defaultMaxLength
	}

	maxLength = min(maxLength, maxMaxLength)
	startIndex := max(input.StartIndex, 0)

	start := time.Now()

	// Sentinel outcome: every successful path overwrites this. If we
	// land in the deferred recorder without an outcome assignment
	// something has gone wrong and the row should reflect that.
	rec := store.FetchRecord{
		URL:        input.URL,
		MaxLength:  maxLength,
		StartIndex: startIndex,
		RawMode:    store.BoolToInt(input.Raw),
		Outcome:    store.OutcomeInternalError,
	}

	defer h.recordFetch(ctx, &rec, start)

	u, err := url.ParseRequestURI(input.URL)
	if err != nil {
		rec.Outcome = store.OutcomeInvalidURL
		rec.Error = err.Error()

		return nil, nil, fmt.Errorf("invalid URL: %w", err)
	}

	rec.Host = u.Host

	// Validate the grep pattern before any network request so a bad
	// pattern fails fast and never burns a fetch.
	if input.Pattern != "" {
		_, err = content.CompilePattern(input.Pattern, input.IgnoreCase)
		if err != nil {
			rec.Outcome = store.OutcomeBadPattern
			rec.Error = err.Error()

			return toolError(err), nil, nil
		}
	}

	err = h.validateURL(u)
	if err != nil {
		rec.Outcome = store.OutcomeDenied
		rec.Error = err.Error()

		h.log.WarnContext(ctx, "denied",
			slog.String("host", u.Host),
			slog.String("url", input.URL),
			slog.Any("error", err),
		)

		return toolError(err), nil, nil
	}

	if h.checkRobots {
		err = h.robots.Check(ctx, u)
		if err != nil {
			rec.Outcome = store.OutcomeRobotsDenied
			rec.Error = err.Error()

			h.log.WarnContext(ctx, "denied",
				slog.String("host", u.Host),
				slog.String("url", input.URL),
				slog.Any("error", err),
			)

			return toolError(err), nil, nil
		}
	}

	h.log.InfoContext(ctx, "allowed",
		slog.String("host", u.Host),
		slog.String("url", input.URL),
	)

	cacheKey := input.URL
	if input.Raw {
		cacheKey += "|raw"
	}

	processed, ok := h.contentCache.Get(cacheKey)
	if ok {
		rec.CacheHit = 1
	} else {
		body, contentType, status, err := h.doFetch(ctx, input.URL)
		if err != nil {
			rec.Error = err.Error()

			switch {
			case errors.Is(err, ErrHTTPStatus):
				rec.Outcome = store.OutcomeHTTPError
				rec.StatusCode = status

				return toolError(err), nil, nil

			case isToolError(err):
				rec.Outcome = store.OutcomeFetchError

				return toolError(err), nil, nil

			default:
				rec.Outcome = store.OutcomeFetchError

				return nil, nil, fmt.Errorf("fetching URL: %w", err)
			}
		}

		rec.StatusCode = status
		rec.ContentType = contentType
		rec.ResponseBytes = len(body)

		processed, err = h.processBody(body, contentType, input.Raw)
		if err != nil {
			rec.Outcome = store.OutcomeInternalError
			rec.Error = err.Error()

			return nil, nil, fmt.Errorf("processing response: %w", err)
		}

		h.contentCache.Add(cacheKey, processed)
	}

	filtered := processed

	if input.Pattern != "" {
		var matched bool

		filtered, matched, err = content.Grep(processed, input.Pattern, content.GrepOptions{
			IgnoreCase: input.IgnoreCase,
			Invert:     input.Invert,
			Context:    input.Context,
		})
		if err != nil {
			// The pattern compiled during pre-fetch validation, so a
			// failure here is internal rather than a bad-pattern case.
			rec.Outcome = store.OutcomeInternalError
			rec.Error = err.Error()

			return nil, nil, fmt.Errorf("filtering content: %w", err)
		}

		if !matched {
			notice := fmt.Sprintf("<no lines matched pattern %q>", input.Pattern)

			rec.Outcome = store.OutcomeOK
			rec.OutputBytes = len(notice)
			rec.Truncated = 0

			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: notice}},
			}, nil, nil
		}
	}

	result, truncated := content.Paginate(filtered, startIndex, maxLength)

	rec.Outcome = store.OutcomeOK
	rec.OutputBytes = len(result)
	rec.Truncated = store.BoolToInt(truncated)

	if h.checkLLMs && startIndex == 0 && u.Path != "/llms.txt" {
		origin := u.Scheme + "://" + u.Host

		llmsURL := h.llms.Find(ctx, origin)
		if llmsURL != "" {
			result += fmt.Sprintf(
				"\n\n<llms.txt available at %s: an LLM-optimized index of this site>",
				llmsURL,
			)
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil, nil
}

// recordFetch is the deferred recorder. It captures any panic so the
// row reflects the failure, persists the record on a context detached
// from the request (so client cancellation does not lose the row),
// then re-raises the panic so the runtime still surfaces the crash.
//
// Record errors are logged at slog Warn and never propagate to the
// MCP caller - the fetch already succeeded from their point of view.
func (h *Handler) recordFetch(ctx context.Context, rec *store.FetchRecord, start time.Time) {
	panicVal := recover()
	if panicVal != nil {
		// rec.Outcome is already OutcomeInternalError (the sentinel set
		// at the top of Handle); only the error string needs filling in.
		rec.Error = fmt.Sprintf("panic: %v", panicVal)

		h.log.ErrorContext(ctx, "fetch panic",
			slog.Any("panic", panicVal),
			slog.String("url", rec.URL),
		)
	}

	rec.DurationMs = time.Since(start).Milliseconds()

	if h.store != nil {
		recCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
		defer cancel()

		err := h.store.Record(recCtx, *rec)
		if err != nil {
			h.log.WarnContext(ctx, "recording fetch",
				slog.String("url", rec.URL),
				slog.Any("error", err),
			)
		}
	}

	if panicVal != nil {
		panic(panicVal)
	}
}

func (h *Handler) doFetch(ctx context.Context, rawURL string) ([]byte, string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, "", 0, fmt.Errorf("building request: %w", err)
	}

	req.Header.Set("User-Agent", h.userAgent)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, "", 0, fmt.Errorf("performing request: %w", err)
	}
	defer h.closeBody(ctx, resp.Body, rawURL)

	contentType := resp.Header.Get("Content-Type")

	if resp.StatusCode >= 400 {
		return nil, contentType, resp.StatusCode, fmt.Errorf("%w: status %d", ErrHTTPStatus, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, contentType, resp.StatusCode, fmt.Errorf("reading response: %w", err)
	}

	return body, contentType, resp.StatusCode, nil
}

func (h *Handler) processBody(body []byte, contentType string, raw bool) (string, error) {
	isHTML := strings.Contains(contentType, "text/html") ||
		bytes.HasPrefix(bytes.TrimSpace(body), []byte("<html"))

	if !isHTML || raw {
		var prefix string
		if contentType != "" {
			prefix = "Content-Type: " + contentType + "\n\n"
		}

		return prefix + string(body), nil
	}

	md, err := markdown.Convert(body)
	if err != nil {
		return "", fmt.Errorf("rendering markdown: %w", err)
	}

	return md, nil
}

func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		IsError: true,
	}
}

func isToolError(err error) bool {
	for _, target := range toolErrors {
		if errors.Is(err, target) {
			return true
		}
	}

	return false
}

func (h *Handler) closeBody(ctx context.Context, body io.Closer, label string) {
	err := body.Close()
	if err != nil {
		h.log.WarnContext(
			ctx, "closing response body",
			slog.String("url", label),
			slog.Any("error", err),
		)
	}
}
