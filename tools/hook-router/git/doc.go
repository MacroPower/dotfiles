// Package git shells out to the git CLI for the small set of
// repository questions hook-router asks. It reports the current HEAD
// commit and the current branch.
//
// Each answer of a [Runner] is for the repository that git finds from
// the directory of the Runner. Variables of the environment, such as
// GIT_DIR and GIT_REFERENCE_BACKEND, name one repository for every git
// command of a process. A Runner starts its git commands without those
// variables.
//
// Each method of a Runner returns an error when git fails, for example
// in a directory that holds no repository.
package git
