// Package linter routes file paths to external linter commands via
// doublestar path globs and turns their output into findings Claude
// reads. It backs hook-router's PostToolUse:Write/Edit/MultiEdit
// handling: after Claude Code writes a file and the formatter has run,
// the first matching rule's linter runs against it, and any findings
// come back through exit code 2 so asyncRewake wakes Claude with them.
//
// Rules arrive as JSON (pathGlob, command, timeout) and the engine
// evaluates them in declaration order with first match winning. A
// linter follows one exit-code contract: 0 means clean, 1 means
// findings on stdout (one `path:line:col: message` line each), and any
// other code is a crash, reported as [ErrLinterFailed] for the caller
// to log and swallow. [Ranges] and [Filter] narrow findings to the
// lines an edit touched, and [Format] renders them for the hook
// channel.
package linter
