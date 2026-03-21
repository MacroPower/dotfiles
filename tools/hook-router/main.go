package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"sort"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// config holds runtime settings resolved from the environment.
type config struct {
	gitIdempotent string
	rtkRewrite    string
}

func configFromEnv() config {
	c := config{
		gitIdempotent: os.Getenv("GIT_IDEMPOTENT"),
		rtkRewrite:    os.Getenv("RTK_REWRITE"),
	}

	if c.gitIdempotent == "" {
		c.gitIdempotent = "git-idempotent"
	}

	return c
}

func main() {
	err := run(os.Stdin, os.Stdout, configFromEnv())
	if err != nil {
		fmt.Fprintf(os.Stderr, "hook-router: %v\n", err)
		os.Exit(1)
	}
}

func run(stdin io.Reader, stdout io.Writer, cfg config) error {
	input, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	var hook map[string]any

	err = json.Unmarshal(input, &hook)
	if err != nil {
		return delegate(input, cfg.rtkRewrite)
	}

	toolInput, ok := hook["tool_input"].(map[string]any)
	if !ok {
		return delegate(input, cfg.rtkRewrite)
	}

	command, ok := toolInput["command"].(string)
	if !ok || command == "" {
		return delegate(input, cfg.rtkRewrite)
	}

	parser := syntax.NewParser()

	prog, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "hook-router: parse error, delegating: %v\n", err)
		return delegate(input, cfg.rtkRewrite)
	}

	// Deny takes priority over rewriting.
	if reason, denied := checkDenied(prog); denied {
		return encodeJSON(stdout, denyResponse(reason))
	}

	if reason, denied := checkGitStashDenied(prog); denied {
		return encodeJSON(stdout, denyResponse(reason))
	}

	rewritten, rewrote, err := rewriteClones(prog, cfg.gitIdempotent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hook-router: rewrite error, delegating: %v\n", err)
		return delegate(input, cfg.rtkRewrite)
	}

	if !rewrote {
		return delegate(input, cfg.rtkRewrite)
	}

	// Build updatedInput preserving all original fields.
	updatedInput := make(map[string]any, len(toolInput))
	maps.Copy(updatedInput, toolInput)

	updatedInput["command"] = rewritten

	return encodeJSON(stdout, map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":      "PreToolUse",
			"permissionDecision": "allow",
			"updatedInput":       updatedInput,
		},
	})
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

func encodeJSON(w io.Writer, v any) error {
	err := json.NewEncoder(w).Encode(v)
	if err != nil {
		return fmt.Errorf("encoding output: %w", err)
	}

	return nil
}

// replacements maps commands that should be denied to their suggested alternatives.
var replacements = map[string]string{
	"grep": "rg",
	"find": "fd",
}

// stashAllowed lists git stash subcommands that are safe to allow through.
// Any stash invocation whose third argument is not in this set is denied,
// which blocks save/push forms used to shelve changes.
var stashAllowed = map[string]bool{
	"pop":    true,
	"apply":  true,
	"list":   true,
	"show":   true,
	"branch": true,
	"drop":   true,
	"clear":  true,
}

// checkDenied walks the AST looking for commands that should be replaced
// with modern alternatives. It returns a denial reason and true if any
// denied command is found.
func checkDenied(prog *syntax.File) (string, bool) {
	found := make(map[string]string) // command -> replacement

	syntax.Walk(prog, func(node syntax.Node) bool {
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

		if alt, match := replacements[lit.Value]; match {
			found[lit.Value] = alt
		}

		return true
	})

	if len(found) == 0 {
		return "", false
	}

	var hints []string
	for cmd, alt := range found {
		hints = append(hints, fmt.Sprintf("Use %s instead of %s.", alt, cmd))
	}

	// Sort for deterministic output.
	sort.Strings(hints)

	return strings.Join(hints, " "), true
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

func rewriteClones(prog *syntax.File, gitIdempotent string) (string, bool, error) {
	rewrote := false
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
		if !ok0 || !ok1 || lit0.Value != "git" || lit1.Value != "clone" {
			return true
		}

		// Replace [git, clone, ...rest] with [git-idempotent, clone, ...rest]
		lit0.Value = gitIdempotent
		rewrote = true

		return true
	})

	if !rewrote {
		return "", false, nil
	}

	var buf bytes.Buffer

	printer := syntax.NewPrinter()

	err := printer.Print(&buf, prog)
	if err != nil {
		return "", false, fmt.Errorf("printing rewritten command: %w", err)
	}

	// Trim trailing newline added by printer.
	return strings.TrimRight(buf.String(), "\n"), true, nil
}

func delegate(input []byte, rtkRewrite string) error {
	if rtkRewrite == "" {
		return nil
	}

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
