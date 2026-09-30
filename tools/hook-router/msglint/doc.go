// Package msglint lints the message a shell command is about to hand
// to git or gh. It backs hook-router's PreToolUse:Bash handling: when
// a command carries a commit message (`git commit -m`, `-F`, or a
// `$(cat <<EOF)` substitution) or a pull request title and body
// (`gh pr create --title --body`), the text goes to an external linter
// on stdin, and the findings come back as a deny reason so Claude
// rewrites the message before the command runs.
//
// The finding's [linter.Tier] sets how hard the deny pushes. A
// Required finding denies the command every time. A Recommended
// finding, with no Required one beside it, denies once. [Check] hands
// the caller a waiver key for the message and the caller records it.
// The identical command passes on its next run, which is how Claude
// keeps a finding it judges a false positive. Optional findings never
// deny on their own and only appear in a reason another tier raised.
//
// The linter sees literal text only. A message built from a parameter
// expansion or an arbitrary command substitution has no text to read
// before the command runs, so the hook lets it through rather than
// lint a fragment. The linter follows the same exit-code contract as the
// file linter: 0 means clean, 1 means findings on stdout, and any
// other code is a crash, reported as [ErrLintFailed] for the caller to
// log and swallow.
package msglint
