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

	// gitBlockedSubcmds lists git subcommands that should be
	// routed through an MCP tool instead of being invoked
	// directly.
	gitBlockedSubcmds = map[string]string{
		"clone": "mcp__git__git_clone",
	}

	// gitFlagsTakingValue lists top-level git flags that consume
	// the following argument as their value (e.g. `git -C dir
	// clone url`). The walker skips both the flag and its value
	// when locating the subcommand.
	gitFlagsTakingValue = map[string]bool{
		"-C":             true,
		"-c":             true,
		"--exec-path":    true,
		"--git-dir":      true,
		"--work-tree":    true,
		"--namespace":    true,
		"--super-prefix": true,
		"--config-env":   true,
	}
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

	if reason, denied := checkGitStashDenied(prog); denied {
		logger.Info(
			"denied",
			slog.String("rule", "git-stash"),
			slog.String("command", command),
			slog.String("reason", reason),
		)

		return encodeJSON(stdout, denyResponse(reason))
	}

	if subcmd, reason, denied := checkGitSubcmdDenied(prog); denied {
		logger.Info(
			"denied",
			slog.String("rule", "git-subcmd"),
			slog.String("subcmd", subcmd),
			slog.String("command", command),
			slog.String("reason", reason),
		)

		return encodeJSON(stdout, denyResponse(reason))
	}

	if reason, denied := checkKubectxDenied(prog); denied {
		logger.Info(
			"denied",
			slog.String("rule", "kubectx"),
			slog.String("command", command),
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

// checkGitSubcmdDenied walks the AST looking for direct invocations of
// git subcommands listed in [gitBlockedSubcmds]. These should use the
// corresponding MCP tool instead. Leading flags between `git` and the
// subcommand (e.g. `git -C dir clone url`) are skipped.
func checkGitSubcmdDenied(prog *syntax.File) (string, string, bool) {
	var subcmd string

	syntax.Walk(prog, func(node syntax.Node) bool {
		if subcmd != "" {
			return false
		}

		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}

		parts0 := call.Args[0].Parts
		if len(parts0) != 1 {
			return true
		}

		lit0, ok0 := parts0[0].(*syntax.Lit)
		if !ok0 || lit0.Value != "git" {
			return true
		}

		skipNext := false

		for _, arg := range call.Args[1:] {
			if len(arg.Parts) != 1 {
				return true
			}

			lit, ok := arg.Parts[0].(*syntax.Lit)
			if !ok {
				return true
			}

			if skipNext {
				skipNext = false
				continue
			}

			if strings.HasPrefix(lit.Value, "-") {
				// Long flags written as `--flag=value` carry the
				// value inline; only consume the next arg when the
				// flag is the bare form.
				if gitFlagsTakingValue[lit.Value] {
					skipNext = true
				}

				continue
			}

			if _, blocked := gitBlockedSubcmds[lit.Value]; blocked {
				subcmd = lit.Value
			}

			return true
		}

		return true
	})

	if subcmd == "" {
		return "", "", false
	}

	return subcmd, fmt.Sprintf(
		"Direct git %s usage is blocked. Use %s instead.",
		subcmd, gitBlockedSubcmds[subcmd],
	), true
}

// kubectxDenied lists command names that are blocked in favor of the
// mcp-kubectx MCP tools.
var kubectxDenied = map[string]bool{
	"kubectx": true,
	"kubens":  true,
}

// checkKubectxDenied walks the AST looking for kubectx or kubens
// invocations. These are denied because the MCP kubectx tools
// (mcp__kubectx__list, mcp__kubectx__select) should be used instead.
func checkKubectxDenied(prog *syntax.File) (string, bool) {
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
		if !ok || !kubectxDenied[lit.Value] {
			return true
		}

		found = true

		return true
	})

	if !found {
		return "", false
	}

	return "Do not use kubectx or kubens directly. Use mcp__kubectx__list to list contexts and mcp__kubectx__select to switch contexts.", true
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
