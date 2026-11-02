// Package msglint lints prose before it leaves through a shell command
// or an MCP tool call. It backs two hook-router PreToolUse handlers.
// On Bash, [Messages] reads the commit message a command hands to git
// (`git commit -m`, `-F`, or a `$(cat <<EOF)` substitution) and the
// pull request title and body it hands to gh (`gh pr create --title
// --body`). On an MCP tool, [Config.ToolMessages] reads the string
// arguments that [Config.ToolFields] names, such as the `text` of a
// chat message or the `body` of a pull request. Each text goes to an
// external linter on stdin. The findings come back as a deny reason so
// Claude rewrites the text before the call runs.
//
// The finding's [linter.Tier] sets how hard the deny pushes. A
// Required finding denies the call every time. A Recommended finding,
// with no Required one beside it, denies once. [Check] hands the
// caller a waiver key for the text and the caller records it. The
// identical call passes on its next run, so Claude keeps a finding it
// judges a false positive by running the call again unchanged.
// Optional findings never deny on their own and only appear in a
// reason another tier raised.
//
// The linter sees literal text only. A shell message built from a
// parameter expansion or an arbitrary command substitution has no text
// to read before the command runs. The hook lets it through rather
// than lint a fragment. The linter follows the same exit-code contract
// as the file linter. Exit 0 means clean and exit 1 means findings on
// stdout. Any other code is a crash, reported as [ErrLintFailed] for
// the caller to log and swallow.
package msglint
