package hook

import (
	"encoding/json"
	"fmt"
	"io"
)

// Input is the JSON payload Claude Code sends to hooks.
//
// Cwd, HookEventName, and TranscriptPath are sent on every event.
// Source is set on SessionStart only (one of "startup", "resume",
// "clear", "compact"). ToolResponse is set on PostToolUse only.
type Input struct {
	SessionID      string         `json:"session_id"`
	HookEventName  string         `json:"hook_event_name"`
	ToolName       string         `json:"tool_name"`
	ToolInput      map[string]any `json:"tool_input"`
	ToolResponse   map[string]any `json:"tool_response"`
	TranscriptPath string         `json:"transcript_path"`
	Prompt         string         `json:"prompt"`
	StopHookActive bool           `json:"stop_hook_active"`
	Cwd            string         `json:"cwd"`
	Source         string         `json:"source"`
}

// ParseInput decodes the JSON payload Claude Code wrote to the hook's
// stdin into an [Input].
func ParseInput(data []byte) (Input, error) {
	var h Input
	if err := json.Unmarshal(data, &h); err != nil {
		return Input{}, fmt.Errorf("parsing hook input: %w", err)
	}

	return h, nil
}

// Deny returns a PreToolUse decision that blocks the tool call.
func Deny(reason string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	}
}

// Ask returns a PreToolUse decision that forces a permission prompt.
// Hook decisions are evaluated after settings ask rules and before
// settings allow rules, so an "ask" here prompts even when a settings
// allow rule or sandbox auto-allow would otherwise let the command run.
func Ask(reason string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "ask",
			"permissionDecisionReason": reason,
		},
	}
}

// Allow returns a PreToolUse decision that skips the analyzer's
// permission prompt. Per Claude Code's hook docs, ask and deny rules
// in settings still fire even when a hook returns "allow".
func Allow(reason string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "allow",
			"permissionDecisionReason": reason,
		},
	}
}

// AllowWithInput returns a PreToolUse decision that skips the analyzer's
// permission prompt and replaces the tool's entire tool_input with
// updated before the call runs. updatedInput is the only documented way
// to carry a rewritten input, and it is paired with an "allow" decision
// because a decision-less updatedInput is unreliable. Claude Code
// re-evaluates the modified input against settings deny and ask rules, so
// this never bypasses the user's permission rules. The replacement is
// whole-object: updated entirely supplants the prior tool_input, so it
// must carry every field that should survive (description, timeout, etc.).
func AllowWithInput(reason string, updated map[string]any) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "allow",
			"permissionDecisionReason": reason,
			"updatedInput":             updated,
		},
	}
}

// Block returns a Stop decision that blocks the stop with the given
// reason.
func Block(reason string) map[string]any {
	return map[string]any{
		"decision": "block",
		"reason":   reason,
	}
}

// UpdatedOutput returns a PostToolUse decision that replaces the
// tool's surfaced output with updated. Claude Code requires updated to
// match the tool's output shape, so callers re-emit the whole
// tool_response map with only the rewritten fields overwritten.
func UpdatedOutput(updated map[string]any) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "PostToolUse",
			"updatedToolOutput": updated,
		},
	}
}

// Encode writes v to w as a single JSON document, the shape Claude
// Code reads hook decisions from on stdout.
func Encode(w io.Writer, v any) error {
	err := json.NewEncoder(w).Encode(v)
	if err != nil {
		return fmt.Errorf("encoding output: %w", err)
	}

	return nil
}
