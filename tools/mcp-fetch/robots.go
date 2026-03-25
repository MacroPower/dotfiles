package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/temoto/robotstxt"
)

const maxRobotsBytes = 512 * 1024 // 512 KiB

// ErrRobotsDisallowed is returned when a URL is disallowed by robots.txt.
var ErrRobotsDisallowed = errors.New("robots.txt disallows this URL")

// checkRobotsURL fetches and checks robots.txt for the given URL's origin.
// It returns an error if the path is disallowed for the handler's user agent.
// Results are cached per origin in the handler's robotsCache.
func (h *fetchHandler) checkRobotsURL(ctx context.Context, u *url.URL) error {
	origin := u.Scheme + "://" + u.Host

	robots := h.loadRobots(ctx, origin)

	group := robots.FindGroup(h.userAgent)
	if !group.Test(u.Path) {
		return fmt.Errorf("%w: %s", ErrRobotsDisallowed, u.Path)
	}

	return nil
}

func (h *fetchHandler) loadRobots(ctx context.Context, origin string) *robotstxt.RobotsData {
	if cached, ok := h.robotsCache.Get(origin); ok {
		return cached
	}

	robots := h.fetchRobots(ctx, origin)

	h.robotsCache.Add(origin, robots)

	return robots
}

func (h *fetchHandler) fetchRobots(ctx context.Context, origin string) *robotstxt.RobotsData {
	robotsURL := origin + "/robots.txt"

	//nolint:gosec // origin is derived from a validated URL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, http.NoBody)
	if err != nil {
		return &robotstxt.RobotsData{}
	}

	resp, err := h.client.Do(req) //nolint:gosec // tainted from above
	if err != nil {
		return &robotstxt.RobotsData{}
	}
	defer h.closeBody(ctx, resp.Body, robotsURL)

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
