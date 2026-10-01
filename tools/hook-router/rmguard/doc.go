// Package rmguard denies `rm` and `rmdir` calls that Claude Code would
// stop on a permission prompt, and tells the agent how to rewrite the
// target.
//
// Claude Code has built-in checks for removal targets it cannot prove
// safe before the command runs. Each one forces a permission prompt
// that no permission mode or allow rule skips, and an unattended
// session waits out the prompt's timeout before the agent can retry. A
// PreToolUse deny arrives ahead of that prompt, so the agent rewrites
// the command in the same turn.
//
// # Variable Paths
//
// An operand matches when it starts with a parameter expansion that
// cannot fail on empty (`$X`, `${X}`, `$1`, `${X:-}`) followed by a
// `/`. The text after that slash must also leave the path at the root
// or one level below it. That text is nothing, a glob, another slash,
// another expansion, or a top-level directory name. `rm -rf
// "$TMPDIR/build"` does not match, and neither does a target guarded
// with `${X:?}`.
//
// The deny reason carries the rewrite. When every unguarded variable
// in the operand is a plain name, the reason shows the operand with
// each one changed to `${NAME:?}` and every other byte kept, so
// quoting and globs keep their meaning. Otherwise it gives the generic
// advice.
//
// # Unresolvable Targets
//
// Four more shapes match, each with its own advice:
//
//   - A target made only of command substitutions, such as `rm -rf
//     $(find . -name x)`.
//   - A relative target with a trailing `/*` after the command changes
//     directory, such as `cd build && rm -rf ./*`.
//   - A trailing `/*` in a directory that is not a literal path: one
//     under `~`, a variable, a substitution, or `..`, a `*/` target,
//     or an `rmdir -p` target.
//   - A glob that spans more than one directory level, such as `rm -rf
//     build/*/*`.
//
// A quoted or escaped glob character is literal and never matches.
//
// Deliberate non-goals:
//
//   - Parity with Claude Code. The rules follow the patterns in its
//     binary but do not reproduce them. Claude Code resolves targets
//     against the working directory and this package reads only the
//     command text. A miss falls back to the built-in prompt, and an
//     extra deny costs one rewrite.
//   - Claude Code's checks for a variable assigned in the same command
//     to a dangerous directory, for a critical system path, and for
//     the working directory or its ancestors.
//   - Wrapper forms (`sudo rm`, `xargs rm`, `sh -c 'rm ...'`).
//   - A run_in_background exemption. The built-in prompt fires for a
//     background call too.
package rmguard
