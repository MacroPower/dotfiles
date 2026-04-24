package main

import (
	"context"
	"crypto/sha256"
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

// Fingerprint returns a snapshot of the current repository state as a
// HEAD commit SHA and a SHA-256 hash of the working tree content. Two
// calls return the same pair only when committed and uncommitted state
// are identical.
//
// The working-tree hash is derived from the full diff against HEAD plus
// the list of untracked non-ignored paths, so edits to an already-dirty
// file produce a different hash. Content of untracked-never-added files
// is not hashed.
func (g *GitRunner) Fingerprint(ctx context.Context) (headSHA, wtHash string, err error) {
	headSHA, err = g.HeadSHA(ctx)
	if err != nil {
		return "", "", err
	}

	diffCmd := exec.CommandContext(ctx, "git", "diff", "--no-ext-diff", "--binary", "HEAD")
	diffCmd.Dir = g.Dir

	diff, err := diffCmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("git diff HEAD: %w", err)
	}

	untrackedCmd := exec.CommandContext(ctx, "git", "ls-files", "--others", "--exclude-standard", "-z")
	untrackedCmd.Dir = g.Dir

	untracked, err := untrackedCmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("git ls-files --others: %w", err)
	}

	h := sha256.New()
	h.Write(diff)
	h.Write([]byte{0})
	h.Write(untracked)

	return headSHA, fmt.Sprintf("%x", h.Sum(nil)), nil
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
