// Package linter routes file paths to external linter commands via
// doublestar path globs and turns their output into findings Claude
// reads. It backs hook-router's PostToolUse:Write/Edit/MultiEdit
// handling: after Claude Code writes a file and the formatter has run,
// the first matching rule's linter runs against it, and any findings
// come back through exit code 2, which Claude Code shows to the agent
// that wrote the file.
//
// Rules arrive as JSON (pathGlob, command, timeout) and the engine
// evaluates them in declaration order with first match winning. A
// linter follows one exit-code contract: 0 means clean, 1 means
// findings on stdout (one `path:line:col: message` line each), and any
// other code is a crash, reported as [ErrLinterFailed] for the caller
// to log and swallow. [Format] renders findings for the hook channel.
//
// An edit reports the findings on the lines it wrote, plus every
// finding it created elsewhere in the file. A finding counts as created
// when the file before the edit has no finding with the same message
// on the same line text. A finding that the edit moved stays quiet,
// and so does an older finding on a line the edit did not change.
//
// A finding can carry a `[Tier]` tag after `path:line:col:`, which
// prose-lint prints from the tier of the rule that reported it. The
// [Tier] sets what the finding asks of Claude. A Required finding gets
// fixed, and a Recommended finding gets fixed unless the rule misread
// the sentence. An Optional finding gets fixed where the rewrite reads
// better. [Format] groups findings under one header per tier so the
// instruction sits next to the lines it covers. An untagged line is
// Required so a linter without tiers keeps its full weight.
package linter
