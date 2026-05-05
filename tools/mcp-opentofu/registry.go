package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"golang.org/x/mod/semver"
)

const (
	defaultBaseURL   = "https://api.opentofu.org"
	defaultUserAgent = "MCP-OpenTofu/" + version
	defaultTimeout   = 30 * time.Second
	maxResponseBytes = 10 * 1024 * 1024 // 10 MiB
	defaultCacheSize = 256
	defaultCacheTTL  = time.Hour
)

// ErrRegistry is returned for user-facing failures originating from the
// OpenTofu Registry: non-2xx HTTP responses and JSON decode errors. The
// handler surfaces these as tool-level errors with [*mcp.CallToolResult.IsError]
// set to true; transport-layer failures bubble up as internal errors instead.
var ErrRegistry = errors.New("registry")

// Addr is the structured registry address.
//
// Provider addr.display includes the "terraform-provider-" prefix
// (e.g. "hashicorp/terraform-provider-aws"), which conflicts with the tool's
// "Do NOT include prefix" guidance, so renderers build headings from
// {Namespace}/{Name} instead. Module addr also carries Target; provider addr
// does not, so the json tag is omitempty for completeness even though we
// never marshal these.
type Addr struct {
	Display   string `json:"display"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Target    string `json:"target,omitempty"`
}

// Version is a registry version entry.
type Version struct {
	ID string `json:"id"`
}

// DocItem is a single resource or data source entry inside a provider
// version's docs index.
type DocItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ProviderVersionDocs is the docs section of a provider version index.
type ProviderVersionDocs struct {
	Resources   []DocItem `json:"resources"`
	Datasources []DocItem `json:"datasources"`
}

// ProviderVersion is the response shape of
// /providers/{ns}/{name}/{ver}/index.json.
type ProviderVersion struct {
	ID   string              `json:"id"`
	Docs ProviderVersionDocs `json:"docs"`
}

// Provider is the response shape of /providers/{ns}/{name}/index.json.
type Provider struct {
	Addr        Addr      `json:"addr"`
	Description string    `json:"description"`
	Link        string    `json:"link"`
	Versions    []Version `json:"versions"`
	Popularity  int       `json:"popularity"`
}

// Module is the response shape of /modules/{ns}/{name}/{target}/index.json.
//
// ForkOf is always present; non-fork modules carry the sentinel
// {Display:"//", Namespace:"", Name:"", Target:""}, so the renderer compares
// ForkOf.Namespace to "" instead of testing for absence.
type Module struct {
	Addr        Addr      `json:"addr"`
	ForkOf      Addr      `json:"fork_of"`
	Description string    `json:"description"`
	Versions    []Version `json:"versions"`
	Popularity  int       `json:"popularity"`
	ForkCount   int       `json:"fork_count"`
}

// SearchLinkVars holds the structured identifier fields embedded in each
// search hit.
//
// The module discriminator uses target_system here, not target, so this is
// kept distinct from [Addr] rather than reused.
type SearchLinkVars struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Target    string `json:"target_system"`
	ID        string `json:"id"`
}

// SearchResultItem is one entry from /docs/search.
//
// There is no Addr field: the search response returns addr as a plain string
// ("ns/name"), incompatible with [Addr]. Identifiers come from LinkVariables
// instead.
type SearchResultItem struct {
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	Type          string         `json:"type"`
	Version       string         `json:"version"`
	LinkVariables SearchLinkVars `json:"link_variables"`
}

// Option configures a [*Client]. See [WithBaseURL], [WithHTTPClient], and
// [WithUserAgent].
type Option func(*Client)

// WithBaseURL overrides the default https://api.opentofu.org base URL.
// Tests pass an [net/http/httptest.NewServer] URL through this option.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient overrides the underlying [*http.Client]. The default has a
// 30s timeout and uses [http.DefaultTransport].
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.http = hc }
}

// WithUserAgent overrides the User-Agent header sent on every request.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// WithCache overrides the default response-body LRU. The default constructed
// by [NewClient] holds 256 entries with a one-hour TTL; tests pass shorter
// TTLs to exercise eviction. A nil cache is ignored so the constructor
// invariant (responseCache always non-nil) holds.
func WithCache(cache *expirable.LRU[string, []byte]) Option {
	return func(c *Client) {
		if cache != nil {
			c.responseCache = cache
		}
	}
}

// Client is the OpenTofu Registry HTTP client.
//
// Successful (2xx) response bodies are cached in responseCache keyed by the
// path and normalized query string. Cache values are raw bytes; JSON or
// Markdown decoding happens after the lookup so a single entry serves every
// caller of the same upstream URL regardless of the destination type.
type Client struct {
	http          *http.Client
	responseCache *expirable.LRU[string, []byte]
	baseURL       string
	userAgent     string
}

// NewClient returns a [*Client] configured with the given options.
func NewClient(opts ...Option) *Client {
	c := &Client{
		http:          &http.Client{Timeout: defaultTimeout},
		baseURL:       defaultBaseURL,
		userAgent:     defaultUserAgent,
		responseCache: expirable.NewLRU[string, []byte](defaultCacheSize, nil, defaultCacheTTL),
	}
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Search calls /registry/docs/search?q={q}.
func (c *Client) Search(ctx context.Context, query string) ([]SearchResultItem, error) {
	u := c.baseURL + "/registry/docs/search?q=" + url.QueryEscape(query)

	var out []SearchResultItem

	err := c.getJSON(ctx, u, &out)
	if err != nil {
		return nil, err
	}

	return out, nil
}

// Provider calls /registry/docs/providers/{ns}/{name}/index.json.
func (c *Client) Provider(ctx context.Context, ns, name string) (Provider, error) {
	u := fmt.Sprintf(
		"%s/registry/docs/providers/%s/%s/index.json",
		c.baseURL, url.PathEscape(ns), url.PathEscape(name),
	)

	var out Provider

	err := c.getJSON(ctx, u, &out)
	if err != nil {
		return Provider{}, err
	}

	return out, nil
}

// ProviderVersion calls /registry/docs/providers/{ns}/{name}/{version}/index.json.
func (c *Client) ProviderVersion(ctx context.Context, ns, name, ver string) (ProviderVersion, error) {
	u := fmt.Sprintf(
		"%s/registry/docs/providers/%s/%s/%s/index.json",
		c.baseURL, url.PathEscape(ns), url.PathEscape(name), url.PathEscape(ver),
	)

	var out ProviderVersion

	err := c.getJSON(ctx, u, &out)
	if err != nil {
		return ProviderVersion{}, err
	}

	return out, nil
}

// Module calls /registry/docs/modules/{ns}/{name}/{target}/index.json.
func (c *Client) Module(ctx context.Context, ns, name, target string) (Module, error) {
	u := fmt.Sprintf(
		"%s/registry/docs/modules/%s/%s/%s/index.json",
		c.baseURL, url.PathEscape(ns), url.PathEscape(name), url.PathEscape(target),
	)

	var out Module

	err := c.getJSON(ctx, u, &out)
	if err != nil {
		return Module{}, err
	}

	return out, nil
}

// ResourceDocs calls
// /registry/docs/providers/{ns}/{name}/{version}/resources/{resource}.md
// and returns the Markdown body verbatim.
func (c *Client) ResourceDocs(ctx context.Context, ns, name, ver, resource string) (string, error) {
	u := fmt.Sprintf(
		"%s/registry/docs/providers/%s/%s/%s/resources/%s.md",
		c.baseURL, url.PathEscape(ns), url.PathEscape(name), url.PathEscape(ver), url.PathEscape(resource),
	)

	return c.getText(ctx, u)
}

// DatasourceDocs calls
// /registry/docs/providers/{ns}/{name}/{version}/datasources/{ds}.md
// and returns the Markdown body verbatim.
func (c *Client) DatasourceDocs(ctx context.Context, ns, name, ver, ds string) (string, error) {
	u := fmt.Sprintf(
		"%s/registry/docs/providers/%s/%s/%s/datasources/%s.md",
		c.baseURL, url.PathEscape(ns), url.PathEscape(name), url.PathEscape(ver), url.PathEscape(ds),
	)

	return c.getText(ctx, u)
}

func (c *Client) getJSON(ctx context.Context, u string, dst any) error {
	body, err := c.fetch(ctx, u)
	if err != nil {
		return err
	}

	err = json.Unmarshal(body, dst)
	if err != nil {
		return fmt.Errorf("%w: decoding %s: %w", ErrRegistry, u, err)
	}

	return nil
}

func (c *Client) getText(ctx context.Context, u string) (string, error) {
	body, err := c.fetch(ctx, u)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func (c *Client) fetch(ctx context.Context, u string) ([]byte, error) {
	key, err := cacheKey(u)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", u, err)
	}

	cached, ok := c.responseCache.Get(key)
	if ok {
		return cached, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json, text/markdown, text/plain;q=0.9, */*;q=0.5")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}

	defer func() {
		closeErr := resp.Body.Close()
		if closeErr != nil {
			slog.DebugContext(ctx, "closing response body",
				slog.String("url", u),
				slog.Any("error", closeErr),
			)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: %s returned status %d", ErrRegistry, u, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", u, err)
	}

	c.responseCache.Add(key, body)

	return body, nil
}

// cacheKey returns the path plus normalized query string for u. Two URLs
// that differ only in query encoding (`%20` vs `+`, parameter order) yield
// identical keys; type filters applied client-side share one entry because
// they never reach the upstream URL.
func cacheKey(u string) (string, error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", u, err)
	}

	if parsed.RawQuery == "" {
		return parsed.Path, nil
	}

	return parsed.Path + "?" + parsed.Query().Encode(), nil
}

// latestVersion returns the highest semver-ranked entry in vs, preserving
// the original ID string. Entries are coerced to v-prefixed form before
// parsing; unparseable entries are skipped. bestCanonical holds the
// v-prefixed form for [semver.Compare] while bestRaw holds the original
// ID returned to the caller. Falls back to vs[0].ID when nothing parses,
// and returns "" when vs is empty.
func latestVersion(vs []Version) string {
	if len(vs) == 0 {
		return ""
	}

	var bestCanonical, bestRaw string
	for _, v := range vs {
		canonical := v.ID
		if !strings.HasPrefix(canonical, "v") {
			canonical = "v" + canonical
		}

		if !semver.IsValid(canonical) {
			continue
		}

		if bestCanonical == "" || semver.Compare(canonical, bestCanonical) > 0 {
			bestCanonical, bestRaw = canonical, v.ID
		}
	}

	if bestRaw == "" {
		return vs[0].ID
	}

	return bestRaw
}
