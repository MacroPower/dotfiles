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
//
// A finding may carry a `[Tier]` tag after `path:line:col:`, which
// prose-lint prints from its rule manifest. The [Tier] sets what the
// finding asks of Claude. A Required finding gets fixed, a Recommended
// finding gets fixed unless the rule misread the sentence, and an
// Optional finding gets fixed where the rewrite reads better. [Format]
// groups findings under one header per tier so the instruction sits
// next to the lines it covers, and an untagged line is Required so a
// linter without tiers keeps its full weight.
package linter
