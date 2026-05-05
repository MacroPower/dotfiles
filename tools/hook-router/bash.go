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

		return encodeJSON(stdout, rewriteResponse(rewritten))
	}

	return delegate(ctx, input, cfg.rtkRewrite, logger)
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

func delegate(ctx context.Context, input []byte, rtkRewrite string, logger *slog.Logger) error {
	if rtkRewrite == "" {
		return nil
	}

	logger.Info("delegating", slog.String("target", rtkRewrite))

	cmd := exec.CommandContext(ctx, rtkRewrite)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("delegating to %s: %w", rtkRewrite, err)
	}

	return nil
}
