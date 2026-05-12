package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"
)

const (
	// bashStderrTailBytes keeps the last 16 KiB of stderr. Errors
	// usually print at the end, so the tail is where the signal lives.
	bashStderrTailBytes = 16 * 1024
	// bashStdoutHeadBytes and bashStdoutTailBytes split stdout into a
	// head+tail capture. Long `go test -v` or `npm install` logs need
	// context at both ends to be readable.
	bashStdoutHeadBytes = 2 * 1024
	bashStdoutTailBytes = 2 * 1024
	bashTruncSentinel   = "\n...truncated...\n"
)

// truncateTail returns the last n bytes of s, aligned forward to a
// UTF-8 rune boundary so a multi-byte char is never split. Strings
// shorter than n pass through unchanged.
func truncateTail(s string, n int) string {
	if len(s) <= n {
		return s
	}

	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}

	return s[start:]
}

// truncateHeadTail keeps the first head bytes and the last tail bytes
// of s, joined by [bashTruncSentinel]. Returns s unchanged when
// len(s) <= head+tail+len(sentinel), so a string barely over the limit
// is not expanded by the sentinel insertion. Both indices are aligned
// to UTF-8 rune boundaries (head rounds down, tail rounds up).
//
// Indices are clamped to len(s) to stay safe if a caller passes head
// or tail values larger than the input.
func truncateHeadTail(s string, head, tail int) string {
	if len(s) <= head+tail+len(bashTruncSentinel) {
		return s
	}

	headEnd := head
	if headEnd > len(s) {
		headEnd = len(s)
	}

	for headEnd > 0 && !utf8.RuneStart(s[headEnd]) {
		headEnd--
	}

	tailStart := len(s) - tail
	if tailStart < 0 {
		tailStart = 0
	}

	for tailStart < len(s) && !utf8.RuneStart(s[tailStart]) {
		tailStart++
	}

	return s[:headEnd] + bashTruncSentinel + s[tailStart:]
}

func handleBash(ctx context.Context, input []byte, stdout io.Writer, cfg config, logger *slog.Logger) error {
	var hook map[string]any

	err := json.Unmarshal(input, &hook)
	if err != nil {
		logger.Info("invalid JSON, delegating", slog.Any("error", err))
		return delegate(ctx, input, cfg.rtkRewrite, logger)
	}

	toolInput, ok := hook["tool_input"].(map[string]any)
	if !ok {
		return delegate(ctx, input, cfg.rtkRewrite, logger)
	}

	command, ok := toolInput["command"].(string)
	if !ok || command == "" {
		return delegate(ctx, input, cfg.rtkRewrite, logger)
	}

	logger.Info("checking command", slog.String("command", command))

	parser := syntax.NewParser()

	prog, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		logger.Warn(
			"parse error, delegating",
			slog.String("command", command),
			slog.Any("error", err),
		)

		return delegate(ctx, input, cfg.rtkRewrite, logger)
	}

	if rule, reason, denied := cfg.commandRules.Check(prog); denied {
		logger.Info(
			"denied",
			slog.String("rule", "command-deny"),
			slog.String("command", rule.Command),
			slog.String("args", strings.Join(rule.Args, " ")),
			slog.String("command_input", command),
			slog.String("reason", reason),
		)

		return encodeJSON(stdout, denyResponse(reason))
	}

	if hasKubectl(prog) {
		if cfg.kubeconfigPath == "" {
			reason := "No kubeconfig found. Use mcp__kubectx__select to choose a context first."

			logger.Info(
				"denied",
				slog.String("rule", "kubectl-no-kubeconfig"),
				slog.String("command", command),
				slog.String("reason", reason),
			)

			return encodeJSON(stdout, denyResponse(reason))
		}

		rewritten := "KUBECONFIG=" + cfg.kubeconfigPath + " " + command

		logger.Info(
			"rewrite",
			slog.String("rule", "kubectl"),
			slog.String("command", command),
			slog.String("rewritten", rewritten),
		)

		resp := rewriteResponse(rewritten)
		if cfg.autoAllow {
			mergeAllow(resp, "sandbox auto-allow (kubectl rewrite)")
		}

		return encodeJSON(stdout, resp)
	}

	return delegateOrAutoAllow(ctx, input, stdout, cfg, logger)
}

// delegateOrAutoAllow runs RTK and, when [config.autoAllow] is set,
// emits a PreToolUse "allow" decision if RTK produced no rewrite of
// its own. Without auto-allow, falls through to [delegate] so streaming
// behavior on Linux/non-sandbox hosts is unchanged.
func delegateOrAutoAllow(ctx context.Context, input []byte, stdout io.Writer, cfg config, logger *slog.Logger) error {
	if !cfg.autoAllow {
		return delegate(ctx, input, cfg.rtkRewrite, logger)
	}

	captured, err := delegateCapture(ctx, input, cfg.rtkRewrite, logger)
	if err != nil {
		// Forward whatever RTK already wrote before erroring; a complete
		// JSON object may have landed on stdout before a downstream step
		// failed. Propagate the error so the wrapper exits non-zero
		// instead of silently swallowing RTK failures under auto-allow.
		if len(captured) > 0 {
			_, _ = stdout.Write(captured)
		}

		return err
	}

	if len(captured) > 0 {
		out, err := mergeAllowIntoJSON(captured, "sandbox auto-allow (rtk rewrite)")
		if err != nil {
			logger.Warn(
				"RTK output not mergeable, forwarding verbatim",
				slog.Any("error", err),
			)

			out = captured
		}

		_, err = stdout.Write(out)
		if err != nil {
			return fmt.Errorf("forwarding RTK output: %w", err)
		}

		return nil
	}

	return encodeJSON(stdout, allowResponse("sandbox auto-allow"))
}

// hasKubectl walks the AST looking for commands where the first word is
// exactly "kubectl".
func hasKubectl(prog *syntax.File) bool {
	found := false

	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 1 {
			return true
		}

		parts0 := call.Args[0].Parts
		if len(parts0) != 1 {
			return true
		}

		lit, ok := parts0[0].(*syntax.Lit)
		if !ok || lit.Value != "kubectl" {
			return true
		}

		found = true

		return true
	})

	return found
}

// runRTK execs rtkRewrite with input on stdin, sending RTK's stdout to
// the given writer. Stderr goes to os.Stderr so RTK's missing-jq,
// missing-rtk, and version-too-old warnings reach the user. Empty
// rtkRewrite is a no-op so callers don't special-case unset RTK.
func runRTK(ctx context.Context, input []byte, rtkRewrite string, stdout io.Writer, logger *slog.Logger) error {
	if rtkRewrite == "" {
		return nil
	}

	logger.Info("delegating", slog.String("target", rtkRewrite))

	cmd := exec.CommandContext(ctx, rtkRewrite)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("delegating to %s: %w", rtkRewrite, err)
	}

	return nil
}

func delegate(ctx context.Context, input []byte, rtkRewrite string, logger *slog.Logger) error {
	return runRTK(ctx, input, rtkRewrite, os.Stdout, logger)
}

// handlePostBash records bash command failures for later analysis.
// Successful runs are dropped silently. Every error path returns nil:
// parse failures, missing tool_response, and DB write errors all log
// at warn and swallow. PostToolUse hook errors get fed back to Claude
// as "error" feedback, and surfacing DB-locked errors there is just
// noise.
//
// A row is written when any of is_error, interrupted, or a non-zero
// exit_code is set on tool_response. All three are persisted to their
// own columns regardless of which one tripped the gate, so analysis
// can disambiguate. Stderr is never a failure signal on its own:
// kubectl, git, pre-commit, npm, and cargo all chatter to stderr on
// success.
func handlePostBash(
	ctx context.Context,
	input []byte,
	store *Store,
	logger *slog.Logger,
) error {
	hook, err := parseHookInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	command, _ := hook.ToolInput["command"].(string)
	if command == "" {
		return nil
	}

	if hook.ToolResponse == nil {
		return nil
	}

	isError, _ := hook.ToolResponse["is_error"].(bool)
	interrupted, _ := hook.ToolResponse["interrupted"].(bool)

	var exitCode *int
	if v, ok := hook.ToolResponse["exit_code"].(float64); ok {
		ec := int(v)
		exitCode = &ec
	}

	failure := isError || interrupted || (exitCode != nil && *exitCode != 0)
	if !failure {
		logger.Debug("bash command succeeded", slog.String("command", command))
		return nil
	}

	stdout, _ := hook.ToolResponse["stdout"].(string)
	stderr, _ := hook.ToolResponse["stderr"].(string)

	err = store.RecordBashFailure(ctx, BashFailure{
		SessionID:      hook.SessionID,
		TranscriptPath: hook.TranscriptPath,
		HookEventName:  hook.HookEventName,
		Cwd:            hook.Cwd,
		Command:        command,
		Stdout:         truncateHeadTail(stdout, bashStdoutHeadBytes, bashStdoutTailBytes),
		Stderr:         truncateTail(stderr, bashStderrTailBytes),
		IsError:        isError,
		Interrupted:    interrupted,
		ExitCode:       exitCode,
	})
	if err != nil {
		logger.Warn("recording bash failure",
			slog.String("command", command),
			slog.Any("error", err),
		)

		return nil
	}

	logger.Info("recorded bash failure",
		slog.String("command", command),
		slog.Bool("is_error", isError),
		slog.Bool("interrupted", interrupted),
	)

	return nil
}

// delegateCapture runs RTK with a buffered stdout so the caller can
// decide whether to forward RTK's output or substitute a different
// response.
//
// Trade-off: rtk-rewrite.sh translates `rtk rewrite` exit 2 (RTK's own
// deny) into "exit 0 + empty stdout", relying on Claude Code's native
// deny rules to catch the same command. Under auto-allow, "empty
// stdout" means "emit allow", so an RTK-only deny rule (one not also
// covered by [DenyCommandRule] or settings.permissions.deny) would
// silently run. The bundled deny set covers the cases we care about,
// and reproducing RTK's deny intent would require execing `rtk rewrite`
// directly and re-implementing its exit-code protocol.
func delegateCapture(ctx context.Context, input []byte, rtkRewrite string, logger *slog.Logger) ([]byte, error) {
	var buf bytes.Buffer

	err := runRTK(ctx, input, rtkRewrite, &buf, logger)

	return buf.Bytes(), err
}
