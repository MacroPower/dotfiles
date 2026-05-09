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

	"mvdan.cc/sh/v3/syntax"
)

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
		_, err = stdout.Write(captured)
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
