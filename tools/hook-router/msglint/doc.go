// Package msglint lints the message a shell command is about to hand
// to git or gh. It backs hook-router's PreToolUse:Bash handling: when
// a command carries a commit message (`git commit -m`, `-F`, or a
// `$(cat <<EOF)` substitution) or a pull request title and body
// (`gh pr create --title --body`), the text goes to an external linter
// on stdin, and any findings come back as a deny reason so Claude
// rewrites the message before the command runs.
//
// The linter sees literal text only. A message built from a parameter
// expansion or an arbitrary command substitution has no text to read
// before the command runs, so the hook lets it through rather than
// lint a fragment. The linter follows the same exit-code contract as the
// file linter: 0 means clean, 1 means findings on stdout, and any
// other code is a crash, reported as [ErrLintFailed] for the caller to
// log and swallow.
package msglint
