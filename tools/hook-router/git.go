package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// GitRunner executes git commands in a working directory.
type GitRunner struct {
	Dir string
}

// HasChanges reports whether there are code changes since baseSHA.
// Falls back to checking working tree status if baseSHA is empty.
// Returns false (not an error) if Dir is not a git repository.
func (g *GitRunner) HasChanges(ctx context.Context, baseSHA string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-dir")
	cmd.Dir = g.Dir

	if err := cmd.Run(); err != nil {
		return false, nil // not a git repo
	}

	if baseSHA != "" {
		cmd = exec.CommandContext(ctx, "git", "diff", "--quiet", baseSHA, "HEAD")
		cmd.Dir = g.Dir

		if err := cmd.Run(); err != nil {
			// exit code 1 means there are differences
			return true, nil
		}
	}

	// Also check working tree for uncommitted changes.
	cmd = exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = g.Dir

	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}

	return len(strings.TrimSpace(string(out))) > 0, nil
}

// HeadSHA returns the current HEAD commit SHA.
func (g *GitRunner) HeadSHA(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = g.Dir

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}
