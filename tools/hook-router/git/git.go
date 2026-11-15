package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNoBranch reports a HEAD that names no branch, such as a detached
// HEAD.
var ErrNoBranch = errors.New("HEAD names no branch")

// Runner executes git commands in a working directory.
type Runner struct {
	Dir string
}

// HasChanges reports whether there are code changes since baseSHA.
// Falls back to checking working tree status if baseSHA is empty.
// Returns false (not an error) if Dir is not a git repository.
func (g *Runner) HasChanges(ctx context.Context, baseSHA string) (bool, error) {
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
func (g *Runner) HeadSHA(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = g.Dir

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// CurrentBranch returns the short name of the branch that HEAD names,
// such as "feat/x". A branch with no commits still has a name. It
// returns [ErrNoBranch] when HEAD is detached or names a ref outside
// refs/heads/, and another error when Dir is not a git repository.
func (g *Runner) CurrentBranch(ctx context.Context) (string, error) {
	// The full ref name avoids the "heads/" prefix that --short adds
	// when a tag shares the name of the branch.
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "--quiet", "HEAD")
	cmd.Dir = g.Dir

	out, err := cmd.Output()
	if err != nil {
		// With --quiet, exit code 1 is the answer for a detached HEAD.
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", ErrNoBranch
		}

		return "", fmt.Errorf("git symbolic-ref HEAD: %w", err)
	}

	branch, ok := strings.CutPrefix(strings.TrimSuffix(string(out), "\n"), "refs/heads/")
	if !ok || branch == "" {
		return "", ErrNoBranch
	}

	return branch, nil
}
