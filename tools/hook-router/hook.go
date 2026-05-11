package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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

// handlePostFileWrite runs the matching formatter rule against the
// file Claude Code just wrote. Stateless: takes no store. Reads
// file_path from tool_input (shared by Write, Edit, and MultiEdit),
// looks up a rule via [*FormatterRules.Match], and runs it. Non-zero
// formatter exits log at warn and are otherwise swallowed, so a
// wedged or missing formatter never reaches the hook JSON channel.
//
// Emits no hookSpecificOutput; the only side effect of a successful
// run is the formatted file on disk.
func handlePostFileWrite(
	ctx context.Context,
	input []byte,
	cfg config,
	logger *slog.Logger,
) error {
	if cfg.formatterRules.Empty() {
		return nil
	}

	hook, err := parseHookInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	filePath, ok := hook.ToolInput["file_path"].(string)
	if !ok || filePath == "" {
		return nil
	}

	rule, ok := cfg.formatterRules.Match(filePath)
	if !ok {
		return nil
	}

	err = rule.Run(ctx, filePath)
	if err != nil {
		logger.Warn("formatter run failed",
			slog.String("file_path", filePath),
			slog.String("formatter", rule.Command[0]),
			slog.Any("error", err),
		)

		return nil
	}

	logger.Info("formatted file",
		slog.String("file_path", filePath),
		slog.String("formatter", rule.Command[0]),
	)

	return nil
}
