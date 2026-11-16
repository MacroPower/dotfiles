// Package git shells out to the git CLI for the small set of
// repository questions hook-router asks. It reports the current HEAD
// commit, the current branch, and whether anything changed since a
// baseline.
//
// Each answer of a [Runner] is for the repository that git finds from
// the directory of the Runner. Variables of the environment, such as
// GIT_DIR and GIT_REFERENCE_BACKEND, name one repository for every git
// command of a process. A Runner starts its git commands without those
// variables.
//
// A directory that is not a git repository is reported as "no
// changes", not an error, so callers can run unconditionally.
package git
