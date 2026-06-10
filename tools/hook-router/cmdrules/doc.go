// Package cmdrules implements a deny/ask rule engine for shell
// commands, evaluated against a parsed shell AST (mvdan.cc/sh) so
// rules match the commands a script actually runs rather than its raw
// text. It backs hook-router's PreToolUse:Bash gate.
//
// Rules are declared as JSON (command, positional args, exceptions,
// action, reason) and evaluated in declaration order with first match
// winning, so the caller building the list controls precedence.
package cmdrules
