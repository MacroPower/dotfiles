package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

// HookInput is the JSON payload Claude Code sends to hooks.
//
// Cwd, HookEventName, and TranscriptPath are sent on every event.
// Source is set on SessionStart only (one of "startup", "resume",
// "clear", "compact"). ToolResponse is set on PostToolUse only.
type HookInput struct {
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

// askResponse returns a PreToolUse decision that forces a permission
// prompt. Hook decisions are evaluated after settings ask rules and
// before settings allow rules, so an "ask" here prompts even when a
// settings allow rule or sandbox auto-allow would otherwise let the
// command run.
func askResponse(reason string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "ask",
			"permissionDecisionReason": reason,
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

func blockResponse(reason string) map[string]any {
	return map[string]any{
		"decision": "block",
		"reason":   reason,
	}
}

// updatedOutputResponse returns a PostToolUse decision that replaces the
// tool's surfaced output with updated. Claude Code requires updated to
// match the tool's output shape, so callers re-emit the whole
// tool_response map with only stdout/stderr overwritten. See
// [handlePostBashCompact].
func updatedOutputResponse(updated map[string]any) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "PostToolUse",
			"updatedToolOutput": updated,
		},
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
