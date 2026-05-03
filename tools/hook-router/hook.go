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
