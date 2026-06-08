package main

import (
	"context"
	"io"
	"log/slog"
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

func handleBash(input []byte, stdout io.Writer, cfg config, logger *slog.Logger) error {
	hook, err := parseHookInput(input)
	if err != nil {
		logger.Info("invalid JSON, falling through", slog.Any("error", err))
		return nil
	}

	command, _ := hook.ToolInput["command"].(string)
	if command == "" {
		return nil
	}

	logger.Info("checking command", slog.String("command", command))

	parser := syntax.NewParser()

	prog, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		logger.Warn(
			"parse error, falling through",
			slog.String("command", command),
			slog.Any("error", err),
		)

		return nil
	}

	if rule, reason, matched := cfg.commandRules.Check(prog); matched {
		decision, ruleKind, response := "denied", "command-deny", denyResponse(reason)
		if rule.Ask() {
			decision, ruleKind, response = "ask", "command-ask", askResponse(reason)
		}

		logger.Info(
			decision,
			slog.String("rule", ruleKind),
			slog.String("command", rule.Command),
			slog.String("args", strings.Join(rule.Args, " ")),
			slog.String("command_input", command),
			slog.String("reason", reason),
		)

		return encodeJSON(stdout, response)
	}

	if hasKubectl(prog) {
		if cfg.kubeconfigPath == "" {
			reason := "No kubeconfig selected. Use mcp__kubectx__select to choose a context first."

			logger.Info(
				"denied",
				slog.String("rule", "kubectl-no-kubeconfig"),
				slog.String("command", command),
				slog.String("reason", reason),
			)

			return encodeJSON(stdout, denyResponse(reason))
		}

		if reason, overridden := kubectlKubeconfigOverride(prog); overridden {
			logger.Info(
				"denied",
				slog.String("rule", "kubectl-kubeconfig-override"),
				slog.String("command", command),
				slog.String("reason", reason),
			)

			return encodeJSON(stdout, denyResponse(reason))
		}

		logger.Info(
			"allow",
			slog.String("rule", "kubectl"),
			slog.String("command", command),
		)

		if cfg.autoAllow {
			return encodeJSON(stdout, allowResponse("sandbox auto-allow (kubectl)"))
		}

		return nil
	}

	if cfg.autoAllow {
		return encodeJSON(stdout, allowResponse("sandbox auto-allow"))
	}

	return nil
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

// kubectlKubeconfigOverride walks the AST looking for a kubectl call
// that points itself at a kubeconfig other than the session-scoped one,
// either via an inline KUBECONFIG= assignment or a --kubeconfig flag.
// Returns an actionable reason and true on the first such call.
//
// The session kubeconfig is already scoped to the context chosen via
// mcp__kubectx__select, so an override is the documented escape hatch
// this check closes. Wrapper forms (env, sudo, sh -c, unset) are out of
// scope here and are contained by the sandbox read-deny on ~/.kube.
func kubectlKubeconfigOverride(prog *syntax.File) (string, bool) {
	const reason = "This kubectl command overrides the session kubeconfig (KUBECONFIG= or --kubeconfig). " +
		"The session is already scoped to the context chosen via mcp__kubectx__select; " +
		"use mcp__kubectx__select to switch contexts instead of pointing kubectl at another kubeconfig."

	overridden := false

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

		for _, assign := range call.Assigns {
			if assign.Name != nil && assign.Name.Value == "KUBECONFIG" {
				overridden = true
				return false
			}
		}

		for _, arg := range call.Args[1:] {
			if wordIsKubeconfigFlag(arg) {
				overridden = true
				return false
			}
		}

		return true
	})

	if overridden {
		return reason, true
	}

	return "", false
}

// wordIsKubeconfigFlag reports whether word is a --kubeconfig flag token,
// in either the separate-value form (--kubeconfig) or the inline-value
// form (--kubeconfig=...). Only the first literal part is inspected, so
// the inline form is caught even when its value is an expansion
// (--kubeconfig=$VAR parses as [Lit("--kubeconfig="), ParamExp]). The
// flag token itself must be a literal; its value may be any word, which
// also covers the separate-value expansion form (--kubeconfig $VAR).
func wordIsKubeconfigFlag(word *syntax.Word) bool {
	if len(word.Parts) == 0 {
		return false
	}

	lit, ok := word.Parts[0].(*syntax.Lit)
	if !ok {
		return false
	}

	return lit.Value == "--kubeconfig" || strings.HasPrefix(lit.Value, "--kubeconfig=")
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

// handlePostBashCompact rewrites a successful Bash command's surfaced
// output by stripping ANSI escapes and collapsing repeated line runs,
// then re-emits the whole tool_response via [updatedOutputResponse] so
// the shortened output is what Claude reads. The streams it rewrites
// (stdout, stderr, or both) come from [*Compactor.Streams]. Stateless:
// takes no store.
//
// The tool_response map is shallow-copied and only stdout/stderr are
// overwritten, preserving sibling fields (interrupted, isImage,
// exit_code, is_error, ...) regardless of which are present. Nothing is
// emitted unless a transform actually shortened something, so an output
// that does not compress passes through untouched.
//
// Every error path logs at warn and returns nil: PostToolUse hook errors
// are fed back to Claude as feedback, and surfacing parse/encode noise
// there would be worse than silently leaving the output as-is. The guard
// on [*Compactor.Empty] (nil-safe) runs first so a nil cfg.compactor is
// a no-op.
func handlePostBashCompact(
	input []byte,
	stdout io.Writer,
	cfg config,
	logger *slog.Logger,
) error {
	if cfg.compactor.Empty() {
		return nil
	}

	hook, err := parseHookInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if hook.ToolResponse == nil {
		return nil
	}

	updated := make(map[string]any, len(hook.ToolResponse))
	for k, v := range hook.ToolResponse {
		updated[k] = v
	}

	changed := false

	for _, stream := range cfg.compactor.Streams() {
		raw, ok := hook.ToolResponse[stream].(string)
		if !ok {
			continue
		}

		if out, did := cfg.compactor.Compact(raw); did {
			updated[stream] = out
			changed = true
		}
	}

	if !changed {
		return nil
	}

	command, _ := hook.ToolInput["command"].(string)

	err = encodeJSON(stdout, updatedOutputResponse(updated))
	if err != nil {
		logger.Warn("encoding compacted bash output",
			slog.String("command", command),
			slog.Any("error", err),
		)

		return nil
	}

	logger.Info("compacted bash output", slog.String("command", command))

	return nil
}
