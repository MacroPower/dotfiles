package main

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
	"github.com/temoto/robotstxt"

	readability "codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
)

const (
	defaultMaxLength = 5000
	maxMaxLength     = 1_000_000
	maxResponseBytes = 10 * 1024 * 1024 // 10 MiB

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

	// ErrDenied is returned when a URL is blocked by the configured [Rules].
	ErrDenied = errors.New("URL denied by rules")

	// toolErrors are sentinel errors surfaced to the caller as tool-level
	// failures rather than internal errors.
	toolErrors = []error{
		ErrHTTPStatus,
		ErrBadScheme,
		ErrDenied,
		ErrRobotsDisallowed,
	}
)

// FetchInput is the input schema for the fetch tool.
type FetchInput struct {
	URL        string `json:"url"                  jsonschema:"URL to fetch"`
	MaxLength  int    `json:"max_length,omitzero"  jsonschema:"Maximum number of characters to return (default 5000)"`
	StartIndex int    `json:"start_index,omitzero" jsonschema:"Character offset to start reading from (default 0)"`
	Raw        bool   `json:"raw,omitzero"         jsonschema:"Return raw content without HTML-to-Markdown conversion"`
}

// fetchHandler holds the shared state for the fetch tool handler.
type fetchHandler struct {
	client       *http.Client
	rules        *Rules
	log          *slog.Logger
	robotsCache  *expirable.LRU[string, *robotstxt.RobotsData]
	contentCache *expirable.LRU[string, string]
	store        *Store
	userAgent    string
	checkRobots  bool
}

// fetchOption configures a [fetchHandler] via [newFetchHandler].
//
// Functions of this type:
//   - [withUserAgent]
//   - [withCheckRobots]
//   - [withRules]
//   - [withLogger]
//   - [withRobotsCache]
//   - [withContentCache]
//   - [withStore]
//
// The HTTP client is wired separately after construction because the
// client's CheckRedirect closure captures the handler, creating a
// cyclic dependency that doesn't fit the option pattern.
type fetchOption func(*fetchHandler)

// withUserAgent sets the User-Agent header. See [fetchOption].
func withUserAgent(ua string) fetchOption {
	return func(h *fetchHandler) { h.userAgent = ua }
}

// withCheckRobots toggles robots.txt enforcement. See [fetchOption].
func withCheckRobots(check bool) fetchOption {
	return func(h *fetchHandler) { h.checkRobots = check }
}

// withRules sets the URL allow/deny rules. See [fetchOption].
func withRules(r *Rules) fetchOption {
	return func(h *fetchHandler) { h.rules = r }
}

// withLogger sets the structured logger. See [fetchOption].
func withLogger(l *slog.Logger) fetchOption {
	return func(h *fetchHandler) { h.log = l }
}

// withRobotsCache replaces the per-origin robots.txt cache. See [fetchOption].
func withRobotsCache(c *expirable.LRU[string, *robotstxt.RobotsData]) fetchOption {
	return func(h *fetchHandler) { h.robotsCache = c }
}

// withContentCache replaces the per-URL content cache. See [fetchOption].
func withContentCache(c *expirable.LRU[string, string]) fetchOption {
	return func(h *fetchHandler) { h.contentCache = c }
}

// withStore enables SQLite recording of fetch results. A nil store
// disables recording. See [fetchOption].
func withStore(s *Store) fetchOption {
	return func(h *fetchHandler) { h.store = s }
}

// newFetchHandler constructs a [*fetchHandler] from the given options.
// Caches default to in-memory expirable LRUs and the logger defaults
// to discard, so a zero-option call returns a usable handler.
func newFetchHandler(opts ...fetchOption) *fetchHandler {
	h := &fetchHandler{
		log:          slog.New(slog.DiscardHandler),
		robotsCache:  expirable.NewLRU[string, *robotstxt.RobotsData](128, nil, time.Hour),
		contentCache: expirable.NewLRU[string, string](64, nil, time.Hour),
		checkRobots:  true,
	}

	for _, opt := range opts {
		opt(h)
	}

	return h
}

// validateURL checks that the URL uses an allowed scheme and passes URL rules.
func (h *fetchHandler) validateURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: %s", ErrBadScheme, u.Scheme)
	}

	if reason := h.rules.Check(u); reason != "" {
		return fmt.Errorf("%w: %s", ErrDenied, reason)
	}

	return nil
}

func (h *fetchHandler) handle(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input FetchInput,
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
	rec := FetchRecord{
		URL:        input.URL,
		MaxLength:  maxLength,
		StartIndex: startIndex,
		RawMode:    boolToInt(input.Raw),
		Outcome:    OutcomeInternalError,
	}

	defer h.recordFetch(ctx, &rec, start)

	u, err := url.ParseRequestURI(input.URL)
	if err != nil {
		rec.Outcome = OutcomeInvalidURL
		rec.Error = err.Error()

		return nil, nil, fmt.Errorf("invalid URL: %w", err)
	}

	rec.Host = u.Host

	err = h.validateURL(u)
	if err != nil {
		rec.Outcome = OutcomeDenied
		rec.Error = err.Error()

		h.log.WarnContext(ctx, "denied",
			slog.String("host", u.Host),
			slog.String("url", input.URL),
			slog.Any("error", err),
		)

		return toolError(err), nil, nil
	}

	if h.checkRobots {
		err = h.checkRobotsURL(ctx, u)
		if err != nil {
			rec.Outcome = OutcomeRobotsDenied
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

	content, ok := h.contentCache.Get(cacheKey)
	if ok {
		rec.CacheHit = 1
	} else {
		body, contentType, status, err := h.doFetch(ctx, input.URL)
		if err != nil {
			rec.Error = err.Error()

			switch {
			case errors.Is(err, ErrHTTPStatus):
				rec.Outcome = OutcomeHTTPError
				rec.StatusCode = status

				return toolError(err), nil, nil

			case isToolError(err):
				rec.Outcome = OutcomeFetchError

				return toolError(err), nil, nil

			default:
				rec.Outcome = OutcomeFetchError

				return nil, nil, fmt.Errorf("fetching URL: %w", err)
			}
		}

		rec.StatusCode = status
		rec.ContentType = contentType
		rec.ResponseBytes = len(body)

		content, err = h.processBody(body, contentType, input.Raw)
		if err != nil {
			rec.Outcome = OutcomeInternalError
			rec.Error = err.Error()

			return nil, nil, fmt.Errorf("processing response: %w", err)
		}

		h.contentCache.Add(cacheKey, content)
	}

	result, truncated := truncate(content, startIndex, maxLength)

	rec.Outcome = OutcomeOK
	rec.OutputBytes = len(result)
	rec.Truncated = boolToInt(truncated)

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
func (h *fetchHandler) recordFetch(ctx context.Context, rec *FetchRecord, start time.Time) {
	panicVal := recover()
	if panicVal != nil {
		// rec.Outcome is already OutcomeInternalError (the sentinel set
		// at the top of handle); only the error string needs filling in.
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

func (h *fetchHandler) doFetch(ctx context.Context, rawURL string) ([]byte, string, int, error) {
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

func (h *fetchHandler) processBody(body []byte, contentType string, raw bool) (string, error) {
	isHTML := strings.Contains(contentType, "text/html") ||
		bytes.HasPrefix(bytes.TrimSpace(body), []byte("<html"))

	if !isHTML || raw {
		var prefix string
		if contentType != "" {
			prefix = "Content-Type: " + contentType + "\n\n"
		}

		return prefix + string(body), nil
	}

	return convertHTML(body)
}

func convertHTML(body []byte) (string, error) {
	article, err := readability.FromReader(bytes.NewReader(body), nil)
	if err == nil && article.Node != nil {
		var buf bytes.Buffer

		renderErr := article.RenderHTML(&buf)
		if renderErr == nil {
			md, mdErr := htmltomarkdown.ConvertString(buf.String())
			if mdErr == nil && strings.TrimSpace(md) != "" {
				return md, nil
			}
		}
	}

	md, err := htmltomarkdown.ConvertString(string(body))
	if err != nil {
		return "", fmt.Errorf("converting HTML to Markdown: %w", err)
	}

	return md, nil
}

// truncate slices the content to the [startIndex, startIndex+maxLength)
// rune window and returns the result plus a flag indicating whether
// content was elided beyond the window.
func truncate(content string, startIndex, maxLength int) (string, bool) {
	runes := []rune(content)
	total := len(runes)

	if startIndex >= total {
		return fmt.Sprintf("<content empty: start_index %d exceeds content length %d>", startIndex, total), false
	}

	end := min(startIndex+maxLength, total)
	result := string(runes[startIndex:end])

	if end < total {
		result += fmt.Sprintf(
			"\n\n<content truncated: %d/%d characters shown. Use start_index=%d to continue reading>",
			end-startIndex, total, end,
		)

		return result, true
	}

	return result, false
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

func (h *fetchHandler) closeBody(ctx context.Context, body io.Closer, label string) {
	err := body.Close()
	if err != nil {
		h.log.WarnContext(
			ctx, "closing response body",
			slog.String("url", label),
			slog.Any("error", err),
		)
	}
}
