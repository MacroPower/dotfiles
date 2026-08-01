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
// The duration ceiling alone is not enough: an agent that has
// internalized "sleep over Ns is denied" waits with `sleep 6` filler
// calls instead, burning foreground turns until a background task's
// notification arrives. So beyond the ceiling, a command whose only
// content is sleeps and no-op filler (`echo`, `printf`, `true`, `:`,
// `jobs`, `wait`) is denied at any duration as a filler wait. The
// ceiling then only governs sleeps embedded in real work -- the settle
// idiom `kill "$pid"; sleep 1; pgrep -f server` stays allowed, a bare
// `sleep 6` does not.
//
// Deliberate non-goals:
//
//   - The ceiling is per sleep call, not per command: `build; sleep 9;
//     probe; sleep 9` stays allowed. Summing across a whole program
//     would misread branches (an if/else runs only one arm).
//   - No loop-context tracking. A foreground `until <check>; do sleep
//     1; done` with a substantive check is allowed; the Bash tool's own
//     120s default timeout bounds it, and the timeout error it produces
//     is itself instructive. (A busy-wait like `while true; do sleep 5;
//     done` still falls to the filler rule -- its only commands are
//     `true` and `sleep`.)
//   - Wrapper forms (`timeout 30 sleep 300`, `sh -c 'sleep 300'`, `env
//     sleep 300`) are out of scope. This is a nudge, not a sandbox.
//   - `sleep 300 &` is denied even though shell backgrounding means it
//     does not block the tool call. Detecting stmt.Background needs
//     parent tracking the walk does not have, and the deny reason
//     points at run_in_background, which is the better answer anyway.
package sleepguard
