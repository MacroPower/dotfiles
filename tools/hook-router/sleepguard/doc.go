// Package sleepguard denies foreground Bash `sleep` calls that would
// block the session and steers the agent toward the harness's real
// waiting primitives: background Bash calls with completion
// notifications, poll loops inside those background calls, and the
// Monitor tool.
//
// A Bash call with run_in_background set is never checked. That
// exemption is the point, not a loophole: a poll loop like `until
// <check>; do sleep 1; done` inside a background call is the wait
// idiom this guard steers toward, so `sleep` there is correct rather
// than a smell.
//
// Durations follow GNU sleep semantics: operands sum (`sleep 5 10` is
// 15s), a trailing `s`/`m`/`h`/`d` suffix scales, fractional values are
// accepted, and `inf`/`infinity` parse to an infinite duration. A call
// whose duration cannot be read -- a parameter expansion, arithmetic,
// command substitution, or an unparseable operand -- is denied rather
// than allowed: a guard that let `sleep $DELAY` through would be
// trivially routed around. Negative operands clamp to zero so they
// cannot arithmetic a call under the ceiling.
//
// Deliberate non-goals:
//
//   - The ceiling is per sleep call, not per command: `sleep 9; sleep
//     9; sleep 9` stays allowed. Summing across a whole program would
//     misread branches (an if/else runs only one arm).
//   - No loop-context tracking. A foreground `while true; do sleep 5;
//     done` with per-iteration sleeps under the ceiling is allowed; the
//     Bash tool's own 120s default timeout bounds it, and the timeout
//     error it produces is itself instructive.
//   - Wrapper forms (`timeout 30 sleep 300`, `sh -c 'sleep 300'`, `env
//     sleep 300`) are out of scope. This is a nudge, not a sandbox.
//   - `sleep 300 &` is denied even though shell backgrounding means it
//     does not block the tool call. Detecting stmt.Background needs
//     parent tracking the walk does not have, and the deny reason
//     points at run_in_background, which is the better answer anyway.
package sleepguard
