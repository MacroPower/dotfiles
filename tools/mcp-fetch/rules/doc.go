// Package rules decides whether a URL may be fetched by matching its
// components against compiled allow/deny patterns.
//
// Rules come from a JSON file via [Load] or are built programmatically
// via [Compile]. A deny rule blocks matching URLs unless one of its
// exceptions applies; a non-empty allow list rejects any URL it does
// not cover. Every pattern is an anchored full-match regexp over a
// single URL component, so a host pattern never matches a substring of
// a longer host.
//
// The same matcher drives robots.txt exemptions. A rules file names the
// URLs whose robots.txt directives the fetcher ignores, which keeps the
// decision per-site and visible in configuration rather than a global
// switch that is easy to leave on and forget.
package rules
