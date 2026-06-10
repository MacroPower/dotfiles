// Package formatter routes file paths to external formatter commands
// via doublestar path globs. It backs hook-router's
// PostToolUse:Write/Edit/MultiEdit handling: after Claude Code writes
// a file, the first matching rule's formatter runs against it.
//
// Rules are declared as JSON (pathGlob, command, timeout) and
// evaluated in declaration order with first match winning. Formatter
// failures are designed to be swallowed by callers: a wedged or
// missing formatter should never break the hook that invoked it.
package formatter
