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

	rewritten, rewrote, err := rewriteClones(command, cfg.gitIdempotent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hook-router: parse error, delegating: %v\n", err)
		return delegate(input, cfg.rtkRewrite)
	}

	if !rewrote {
		return delegate(input, cfg.rtkRewrite)
	}

	// Build updatedInput preserving all original fields.
	updatedInput := make(map[string]any, len(toolInput))
	maps.Copy(updatedInput, toolInput)

	updatedInput["command"] = rewritten

	output := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":      "PreToolUse",
			"permissionDecision": "allow",
			"updatedInput":       updatedInput,
		},
	}

	enc := json.NewEncoder(stdout)

	err = enc.Encode(output)
	if err != nil {
		return fmt.Errorf("encoding output: %w", err)
	}

	return nil
}

func rewriteClones(command, gitIdempotent string) (string, bool, error) {
	parser := syntax.NewParser()

	prog, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		return "", false, fmt.Errorf("parsing command: %w", err)
	}

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

	err = printer.Print(&buf, prog)
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
