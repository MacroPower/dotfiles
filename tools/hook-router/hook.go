package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// HookInput represents the JSON payload Claude Code sends to hooks.
//
// Cwd is included on every event. Source is only populated on
// SessionStart (one of "startup", "resume", "clear", "compact"); other
// events leave it empty.
type HookInput struct {
	SessionID      string         `json:"session_id"`
	ToolName       string         `json:"tool_name"`
	ToolInput      map[string]any `json:"tool_input"`
	Prompt         string         `json:"prompt"`
	StopHookActive bool           `json:"stop_hook_active"`
	Cwd            string         `json:"cwd"`
	Source         string         `json:"source"`
}

func parseHookInput(data []byte) (HookInput, error) {
	var h HookInput
	if err := json.Unmarshal(data, &h); err != nil {
		return HookInput{}, fmt.Errorf("parsing hook input: %w", err)
	}

	return h, nil
}

func denyResponse(reason string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	}
}

func rewriteResponse(command string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse",
			"updatedInput": map[string]any{
				"command": command,
			},
		},
	}
}

// allowResponse returns a PreToolUse decision that skips the analyzer's
// permission prompt. Per Claude Code's hook docs, ask and deny rules
// in settings still fire even when a hook returns "allow".
func allowResponse(reason string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "allow",
			"permissionDecisionReason": reason,
		},
	}
}

// mergeAllow attaches an "allow" permission decision to an existing
// PreToolUse response (typically from [rewriteResponse]), preserving
// hookEventName and updatedInput so one response both rewrites the
// command and skips the analyzer prompt.
func mergeAllow(resp map[string]any, reason string) {
	hso, ok := resp["hookSpecificOutput"].(map[string]any)
	if !ok {
		return
	}

	hso["permissionDecision"] = "allow"
	hso["permissionDecisionReason"] = reason
}

// mergeAllowIntoJSON merges a PreToolUse "allow" decision into the
// JSON response in data, leaving an existing permissionDecision
// (allow, deny, or ask) untouched. Returns the original bytes when
// no merge was needed, or freshly encoded bytes (with a trailing
// newline to match [encodeJSON] framing) when allow was merged in.
//
// Caller contract: data must be non-empty. The function does not
// distinguish "RTK had no opinion" from "RTK wanted to ask"; both
// surface as a missing permissionDecision and both get allow merged
// in. See the auto-allow trade-off note alongside [delegateOrAutoAllow].
func mergeAllowIntoJSON(data []byte, reason string) ([]byte, error) {
	var resp map[string]any

	err := json.Unmarshal(data, &resp)
	if err != nil {
		return nil, fmt.Errorf("parsing RTK response: %w", err)
	}

	hso, ok := resp["hookSpecificOutput"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("RTK response missing hookSpecificOutput object")
	}

	if _, has := hso["permissionDecision"]; has {
		return data, nil
	}

	mergeAllow(resp, reason)

	out, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("re-encoding merged response: %w", err)
	}

	return append(out, '\n'), nil
}

func blockResponse(reason string) map[string]any {
	return map[string]any{
		"decision": "block",
		"reason":   reason,
	}
}

func encodeJSON(w io.Writer, v any) error {
	err := json.NewEncoder(w).Encode(v)
	if err != nil {
		return fmt.Errorf("encoding output: %w", err)
	}

	return nil
}
