// Package hook implements the JSON protocol Claude Code uses to talk
// to hook processes: parsing the event payload delivered on stdin and
// building the decision documents (deny, ask, allow, block, updated
// tool output) a hook emits on stdout.
//
// The package is transport-only. It knows the shapes Claude Code
// sends and accepts, not the policy that decides between them; policy
// lives with the caller.
package hook
