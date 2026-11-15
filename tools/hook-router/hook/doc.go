// Package hook implements the JSON protocol Claude Code uses to talk
// to hook processes. It parses the event payload delivered on stdin.
// It builds the decision documents a hook emits on stdout: deny, ask,
// allow, block, updated tool output, and the answer to a permission
// prompt.
//
// The package is transport-only. It knows the JSON payloads Claude Code
// sends and accepts, not the policy that decides between them. Policy
// lives with the caller.
package hook
