// Package robots enforces robots.txt for a fetcher. A [Checker] fetches
// each origin's /robots.txt once, caches the parsed result per origin,
// and reports whether a given URL path is allowed for its configured
// user agent.
//
// The checker shares the caller's [*http.Client] so robots fetches obey
// the same redirect, proxy, and timeout policy as the content fetches
// they gate. Network and parse failures fail open: an unreachable or
// malformed robots.txt allows the request.
package robots
