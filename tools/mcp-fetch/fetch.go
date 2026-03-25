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
	userAgent    string
	checkRobots  bool
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

	u, err := url.ParseRequestURI(input.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid URL: %w", err)
	}

	err = h.validateURL(u)
	if err != nil {
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
	if !ok {
		body, contentType, err := h.doFetch(ctx, input.URL)
		if err != nil {
			if isToolError(err) {
				return toolError(err), nil, nil
			}

			return nil, nil, fmt.Errorf("fetching URL: %w", err)
		}

		content, err = h.processBody(body, contentType, input.Raw)
		if err != nil {
			return nil, nil, fmt.Errorf("processing response: %w", err)
		}

		h.contentCache.Add(cacheKey, content)
	}

	result := truncate(content, startIndex, maxLength)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil, nil
}

func (h *fetchHandler) doFetch(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, "", fmt.Errorf("building request: %w", err)
	}

	req.Header.Set("User-Agent", h.userAgent)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("performing request: %w", err)
	}
	defer h.closeBody(ctx, resp.Body, rawURL)

	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("%w: status %d", ErrHTTPStatus, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, "", fmt.Errorf("reading response: %w", err)
	}

	return body, resp.Header.Get("Content-Type"), nil
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

func truncate(content string, startIndex, maxLength int) string {
	runes := []rune(content)
	total := len(runes)

	if startIndex >= total {
		return fmt.Sprintf("<content empty: start_index %d exceeds content length %d>", startIndex, total)
	}

	end := min(startIndex+maxLength, total)
	result := string(runes[startIndex:end])

	if end < total {
		result += fmt.Sprintf(
			"\n\n<content truncated: %d/%d characters shown. Use start_index=%d to continue reading>",
			end-startIndex, total, end,
		)
	}

	return result
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
