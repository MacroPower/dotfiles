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

var (
	// stashAllowed lists git stash subcommands that are
	// safe to allow through. Any stash invocation whose
	// third argument is not in this set is denied, which
	// blocks save/push forms used to shelve changes.
	stashAllowed = map[string]bool{
		"pop":    true,
		"apply":  true,
		"list":   true,
		"show":   true,
		"branch": true,
		"drop":   true,
		"clear":  true,
	}

	// k8sBlockedCmds lists CLI commands that should be routed
	// through the mcp-kubernetes MCP server instead of being
	// invoked directly.
	k8sBlockedCmds = map[string]string{
		"kubectl": "mcp__kubernetes__call_kubectl",
	}
)

func handleBash(input []byte, stdout io.Writer, cfg config, logger *slog.Logger) error {
	var hook map[string]any

	err := json.Unmarshal(input, &hook)
	if err != nil {
		logger.Info("invalid JSON, delegating", slog.Any("error", err))
		return delegate(input, cfg.rtkRewrite, logger)
	}

	toolInput, ok := hook["tool_input"].(map[string]any)
	if !ok {
		return delegate(input, cfg.rtkRewrite, logger)
	}

	command, ok := toolInput["command"].(string)
	if !ok || command == "" {
		return delegate(input, cfg.rtkRewrite, logger)
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

		return delegate(input, cfg.rtkRewrite, logger)
	}

	if reason, denied := checkGitStashDenied(prog); denied {
		logger.Info(
			"denied",
			slog.String("rule", "git-stash"),
			slog.String("command", command),
			slog.String("reason", reason),
		)

		return encodeJSON(stdout, denyResponse(reason))
	}

	if reason, denied := checkK8sCliDenied(prog); denied {
		logger.Info(
			"denied",
			slog.String("rule", "k8s-cli"),
			slog.String("command", command),
			slog.String("reason", reason),
		)

		return encodeJSON(stdout, denyResponse(reason))
	}

	return delegate(input, cfg.rtkRewrite, logger)
}

// checkGitStashDenied walks the AST looking for git stash invocations that
// save/push changes. It allows read and consume subcommands (pop, apply, list,
// show, branch, drop, clear) and denies everything else.
func checkGitStashDenied(prog *syntax.File) (string, bool) {
	found := false

	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}

		parts0 := call.Args[0].Parts
		parts1 := call.Args[1].Parts
		if len(parts0) != 1 || len(parts1) != 1 {
			return true
		}

		lit0, ok0 := parts0[0].(*syntax.Lit)
		lit1, ok1 := parts1[0].(*syntax.Lit)
		if !ok0 || !ok1 || lit0.Value != "git" || lit1.Value != "stash" {
			return true
		}

		// Bare "git stash" (implicit push) or unknown subcommand/flag.
		if len(call.Args) == 2 {
			found = true
			return true
		}

		parts2 := call.Args[2].Parts
		if len(parts2) != 1 {
			found = true
			return true
		}

		lit2, ok2 := parts2[0].(*syntax.Lit)
		if !ok2 || !stashAllowed[lit2.Value] {
			found = true
		}

		return true
	})

	if !found {
		return "", false
	}

	return "Do not use git stash to shelve changes. All issues in the working tree are your responsibility to fix, regardless of origin.", true
}

// checkK8sCliDenied walks the AST looking for direct invocations of kubectl.
// These should use the mcp-kubernetes MCP server.
func checkK8sCliDenied(prog *syntax.File) (string, bool) {
	var tool string

	syntax.Walk(prog, func(node syntax.Node) bool {
		if tool != "" {
			return false
		}

		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 1 {
			return true
		}

		parts := call.Args[0].Parts
		if len(parts) != 1 {
			return true
		}

		lit, ok := parts[0].(*syntax.Lit)
		if !ok {
			return true
		}

		if _, blocked := k8sBlockedCmds[lit.Value]; blocked {
			tool = lit.Value
		}

		return true
	})

	if tool == "" {
		return "", false
	}

	return fmt.Sprintf(
		"Direct %s usage is blocked. Use %s instead.",
		tool, k8sBlockedCmds[tool],
	), true
}

func delegate(input []byte, rtkRewrite string, logger *slog.Logger) error {
	if rtkRewrite == "" {
		return nil
	}

	logger.Info("delegating", slog.String("target", rtkRewrite))

	cmd := exec.CommandContext(context.Background(), rtkRewrite)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("delegating to %s: %w", rtkRewrite, err)
	}

	return nil
}
