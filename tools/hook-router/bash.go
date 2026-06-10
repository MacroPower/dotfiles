package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/kubectx"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/state"
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
	h, err := hook.ParseInput(input)
	if err != nil {
		logger.Info("invalid JSON, falling through", slog.Any("error", err))
		return nil
	}

	command, _ := h.ToolInput["command"].(string)
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
		decision, ruleKind, response := "denied", "command-deny", hook.Deny(reason)
		if rule.Ask() {
			decision, ruleKind, response = "ask", "command-ask", hook.Ask(reason)
		}

		logger.Info(
			decision,
			slog.String("rule", ruleKind),
			slog.String("command", rule.Command),
			slog.String("args", strings.Join(rule.Args, " ")),
			slog.String("command_input", command),
			slog.String("reason", reason),
		)

		return writeDecision(stdout, response)
	}

	if kubectx.HasKubectl(prog) {
		if cfg.kubeconfigPath == "" {
			reason := "No kubeconfig selected. Use mcp__kubectx__select to choose a context first."

			logger.Info(
				"denied",
				slog.String("rule", "kubectl-no-kubeconfig"),
				slog.String("command", command),
				slog.String("reason", reason),
			)

			return writeDecision(stdout, hook.Deny(reason))
		}

		if reason, overridden := kubectx.KubeconfigOverride(prog); overridden {
			logger.Info(
				"denied",
				slog.String("rule", "kubectl-kubeconfig-override"),
				slog.String("command", command),
				slog.String("reason", reason),
			)

			return writeDecision(stdout, hook.Deny(reason))
		}

		logger.Info(
			"allow",
			slog.String("rule", "kubectl"),
			slog.String("command", command),
		)

		if cfg.autoAllow {
			return writeDecision(stdout, hook.Allow("sandbox auto-allow (kubectl)"))
		}

		return nil
	}

	if cfg.autoAllow {
		return writeDecision(stdout, hook.Allow("sandbox auto-allow"))
	}

	return nil
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
	store *state.Store,
	logger *slog.Logger,
) error {
	h, err := hook.ParseInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	command, _ := h.ToolInput["command"].(string)
	if command == "" {
		return nil
	}

	if h.ToolResponse == nil {
		return nil
	}

	isError, _ := h.ToolResponse["is_error"].(bool)
	interrupted, _ := h.ToolResponse["interrupted"].(bool)

	var exitCode *int
	if v, ok := h.ToolResponse["exit_code"].(float64); ok {
		ec := int(v)
		exitCode = &ec
	}

	failure := isError || interrupted || (exitCode != nil && *exitCode != 0)
	if !failure {
		logger.Debug("bash command succeeded", slog.String("command", command))
		return nil
	}

	stdout, _ := h.ToolResponse["stdout"].(string)
	stderr, _ := h.ToolResponse["stderr"].(string)

	err = store.RecordBashFailure(ctx, state.BashFailure{
		SessionID:      h.SessionID,
		TranscriptPath: h.TranscriptPath,
		HookEventName:  h.HookEventName,
		Cwd:            h.Cwd,
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
// then re-emits the whole tool_response via [hook.UpdatedOutput] so
// the shortened output is what Claude reads. The streams it rewrites
// (stdout, stderr, or both) come from [compact.Compactor.Streams]. Stateless:
// takes no store.
//
// When cfg.outputArchive is enabled, compaction is lossless-by-retrieval:
// before a stream is shortened, its uncompacted content is written to a
// per-stream file via [archive.Archive.Annotate] and a one-line pointer
// naming that file is appended, so the model can read back the exact
// ANSI and repeated lines compaction dropped. The fallback is
// per-stream and conservative: if the archive write fails or the pointer
// would not net-shorten the output, that stream keeps its full original
// content (already in the shallow copy) -- never a lossy rewrite with no
// recovery path, never a dangling pointer. With archiving disabled
// (nil-safe [archive.Archive.Empty]), the stream takes the plain lossy
// compaction with no file and no pointer.
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
// on [compact.Compactor.Empty] (nil-safe) runs first so a nil cfg.compactor is
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

	h, err := hook.ParseInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if h.ToolResponse == nil {
		return nil
	}

	updated := make(map[string]any, len(h.ToolResponse))
	for k, v := range h.ToolResponse {
		updated[k] = v
	}

	changed := false

	for _, stream := range cfg.compactor.Streams() {
		raw, ok := h.ToolResponse[stream].(string)
		if !ok {
			continue
		}

		out, did := cfg.compactor.Compact(raw)
		if !did {
			continue
		}

		if cfg.outputArchive.Empty() {
			// Archiving off: plain lossy compaction, no recoverable original.
			updated[stream] = out
			changed = true

			continue
		}

		annotated, ok := cfg.outputArchive.Annotate(h.SessionID, stream, raw, out, logger)
		if !ok {
			// Save failed or the pointer would not net-shorten: keep the
			// full original stream (already in the shallow copy) so the
			// rewrite stays recoverable -- no lossy compaction without a
			// pointer, no pointer without a file.
			continue
		}

		updated[stream] = annotated
		changed = true
	}

	if !changed {
		return nil
	}

	command, _ := h.ToolInput["command"].(string)

	err = writeDecision(stdout, hook.UpdatedOutput(updated))
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
