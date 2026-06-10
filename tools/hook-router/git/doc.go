// Package git shells out to the git CLI for the small set of
// repository questions hook-router asks: the current HEAD commit and
// whether anything changed since a baseline.
//
// A directory that is not a git repository is reported as "no
// changes", not an error, so callers can run unconditionally.
package git
