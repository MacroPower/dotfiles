package robots

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/temoto/robotstxt"
)

const (
	maxRobotsBytes = 512 * 1024 // 512 KiB

	cacheSize = 128
	cacheTTL  = time.Hour
)

// ErrDisallowed is returned when a URL is disallowed by robots.txt.
var ErrDisallowed = errors.New("robots.txt disallows this URL")

// Checker fetches and evaluates robots.txt for a single user agent,
// caching each origin's parsed rules.
type Checker struct {
	client    *http.Client
	cache     *expirable.LRU[string, *robotstxt.RobotsData]
	log       *slog.Logger
	userAgent string
}

// Option configures a [Checker] via [New].
//
// Functions of this type:
//   - [WithLogger]
type Option func(*Checker)

// WithLogger sets the structured logger used for body-close warnings.
// See [Option].
func WithLogger(l *slog.Logger) Option {
	return func(c *Checker) { c.log = l }
}

// New constructs a [*Checker] that fetches robots.txt through client and
// evaluates rules for userAgent. The client should be the same one used
// for content fetches so robots requests share its redirect, proxy, and
// timeout policy.
func New(client *http.Client, userAgent string, opts ...Option) *Checker {
	c := &Checker{
		client:    client,
		userAgent: userAgent,
		log:       slog.New(slog.DiscardHandler),
		cache:     expirable.NewLRU[string, *robotstxt.RobotsData](cacheSize, nil, cacheTTL),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Check reports whether u's path is allowed for the checker's user
// agent, returning [ErrDisallowed] when it is not. Results are cached
// per origin. A nil receiver allows everything.
func (c *Checker) Check(ctx context.Context, u *url.URL) error {
	if c == nil {
		return nil
	}

	origin := u.Scheme + "://" + u.Host

	robots := c.load(ctx, origin)

	group := robots.FindGroup(c.userAgent)
	if !group.Test(u.Path) {
		return fmt.Errorf("%w: %s", ErrDisallowed, u.Path)
	}

	return nil
}

func (c *Checker) load(ctx context.Context, origin string) *robotstxt.RobotsData {
	if cached, ok := c.cache.Get(origin); ok {
		return cached
	}

	robots := c.fetch(ctx, origin)

	c.cache.Add(origin, robots)

	return robots
}

func (c *Checker) fetch(ctx context.Context, origin string) *robotstxt.RobotsData {
	robotsURL := origin + "/robots.txt"

	//nolint:gosec // origin is derived from a validated URL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, http.NoBody)
	if err != nil {
		return &robotstxt.RobotsData{}
	}

	resp, err := c.client.Do(req) //nolint:gosec // tainted from above
	if err != nil {
		return &robotstxt.RobotsData{}
	}
	defer c.closeBody(ctx, resp.Body, robotsURL)

	if resp.StatusCode != http.StatusOK {
		return &robotstxt.RobotsData{}
	}

	resp.Body = io.NopCloser(io.LimitReader(resp.Body, maxRobotsBytes))

	robots, err := robotstxt.FromResponse(resp)
	if err != nil {
		return &robotstxt.RobotsData{}
	}

	return robots
}

func (c *Checker) closeBody(ctx context.Context, body io.Closer, label string) {
	err := body.Close()
	if err != nil {
		c.log.WarnContext(ctx, "closing response body",
			slog.String("url", label),
			slog.Any("error", err),
		)
	}
}
