package clarity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/httpbody"
)

// DefaultURL is the base URL of the site that publishes the Clarity
// database. A [*Client] reads that site when no option sets another URL.
const DefaultURL = "https://database-clarity.github.io/Live-Clarity-Database"

const (
	defaultUserAgent = "MCP-Destiny"

	// defaultTimeout bounds one request. Clarity is an optional source,
	// so a site that does not answer must not hold a tool call for long.
	defaultTimeout = 15 * time.Second

	// pathVersions is the path of the versions file, which names the
	// version of the descriptions file.
	pathVersions = "/versions.json"

	// pathDescriptions is the path of the descriptions file.
	pathDescriptions = "/descriptions/clarity.json"

	// maxVersionsBytes caps the versions file.
	maxVersionsBytes = 1 << 20

	// maxDescriptionsBytes caps the descriptions file.
	maxDescriptionsBytes = 32 << 20
)

var (
	// errNoVersion reports a versions file without the version of the
	// descriptions file.
	errNoVersion = errors.New("the versions file names no descriptions version")

	// errTooLarge reports a file that is larger than its cap.
	errTooLarge = errors.New("the file is larger than the server accepts")
)

// ClientOption configures a [*Client] via [NewClient].
//
// Functions of this type:
//   - [WithURL]
//   - [WithUserAgent]
type ClientOption func(*Client)

// WithURL overrides the base URL of the Clarity site. It is a
// [ClientOption].
func WithURL(u string) ClientOption {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithUserAgent sets the User-Agent header of every request. The header
// defaults to MCP-Destiny. It is a [ClientOption].
func WithUserAgent(ua string) ClientOption {
	return func(c *Client) { c.userAgent = ua }
}

// Client is the HTTP client for the site that publishes the Clarity
// database. The site serves two files. The versions file names the
// version of the descriptions file, and the descriptions file holds the
// description of every perk that Clarity covers.
type Client struct {
	http      *http.Client
	baseURL   string
	userAgent string
}

// NewClient returns a [*Client] configured with the given options.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		http:      &http.Client{Timeout: defaultTimeout},
		baseURL:   DefaultURL,
		userAgent: defaultUserAgent,
	}
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Version returns the version of the descriptions file that the site
// publishes at the time of the call. Clarity writes the version as a
// number, and Version returns the digits of that number unchanged.
func (c *Client) Version(ctx context.Context) (string, error) {
	u := c.baseURL + pathVersions

	raw, err := c.get(ctx, u, maxVersionsBytes)
	if err != nil {
		return "", err
	}

	var versions struct {
		Descriptions json.Number `json:"descriptions"`
	}

	err = json.Unmarshal(raw, &versions)
	if err != nil {
		return "", fmt.Errorf("decoding %s: %w", u, err)
	}

	if versions.Descriptions == "" {
		return "", fmt.Errorf("%s: %w", u, errNoVersion)
	}

	return versions.Descriptions.String(), nil
}

// Descriptions returns the content of the descriptions file. [Decode]
// reads it.
func (c *Client) Descriptions(ctx context.Context) ([]byte, error) {
	return c.get(ctx, c.baseURL+pathDescriptions, maxDescriptionsBytes)
}

// get returns the body that the site serves at the URL u. It rejects a
// body of more than limit bytes.
func (c *Client) get(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	//nolint:bodyclose // The deferred httpbody.Close closes the body.
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}

	defer httpbody.Close(ctx, resp.Body, u)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned status %d", u, resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", u, err)
	}

	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%s: %w", u, errTooLarge)
	}

	return raw, nil
}
