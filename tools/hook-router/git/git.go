package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

// ErrNoBranch reports a HEAD that names no branch, such as a detached
// HEAD.
var ErrNoBranch = errors.New("HEAD names no branch")

// Runner answers questions about the repository of the directory Dir.
//
// Each git command of a Runner runs in Dir, and git finds the
// repository from that directory. A variable of the environment can
// point git at another repository. With GIT_DIR set, a git command
// acts on the repository that GIT_DIR names, not on the repository of
// its working directory. A Runner therefore starts each command
// without the variables that are local to one repository, such as
// GIT_DIR, GIT_WORK_TREE, GIT_INDEX_FILE, and GIT_COMMON_DIR.
//
// Each method first reads the names of those variables from git
// rev-parse --local-env-vars, so the list follows the installed git.
// The method returns an error when that command fails. A Runner also
// starts each command without GIT_REFERENCE_BACKEND. That variable
// points git at the HEAD and the refs of another repository, and git
// 2.56 leaves it out of the list.
//
// git also lists GIT_CONFIG_COUNT and GIT_CONFIG_PARAMETERS. A Runner
// keeps both of them and every variable that git does not list, except
// GIT_REFERENCE_BACKEND. A parent process can therefore pass config to
// the commands of a Runner, such as a safe.directory entry for a
// repository that another user owns.
type Runner struct {
	Dir string
}

// HeadSHA returns the current HEAD commit SHA.
func (g *Runner) HeadSHA(ctx context.Context) (string, error) {
	names, err := repositoryEnv(ctx)
	if err != nil {
		return "", err
	}

	out, err := g.command(ctx, names, "rev-parse", "HEAD").Output()
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
	names, err := repositoryEnv(ctx)
	if err != nil {
		return "", err
	}

	// The full ref name avoids the "heads/" prefix that --short adds
	// when a tag shares the name of the branch.
	out, err := g.command(ctx, names, "symbolic-ref", "--quiet", "HEAD").Output()
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

// command returns the git command that runs args in Dir. The command
// starts in the environment of the process, without the variables that
// names lists.
func (g *Runner) command(ctx context.Context, names []string, args ...string) *exec.Cmd {
	//nolint:gosec // G204: the program is git, and the methods of Runner pass args.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.Dir

	// Environ also sets PWD to Dir, as a command with a nil Env does.
	cmd.Env = slices.DeleteFunc(cmd.Environ(), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")

		return slices.Contains(names, name)
	})

	return cmd
}

// repositoryEnv returns the name of each variable of the environment
// that a [Runner] removes from its git commands.
//
// git rev-parse --local-env-vars prints the names of the variables that
// are local to one repository, so the list follows the installed git.
// The command does not need a repository, and it prints the names
// whatever values the variables hold. Its output also holds
// GIT_CONFIG_COUNT and GIT_CONFIG_PARAMETERS. These two variables pass
// config to each git command of a process, such as the options of git
// -c. They do not name a repository. repositoryEnv leaves both out, and
// git keeps them too when it runs a command in another repository.
//
// repositoryEnv adds referenceBackendEnv when the output does not hold
// it.
func repositoryEnv(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		return nil, fmt.Errorf("listing the repository variables of git: %w", err)
	}

	names := slices.DeleteFunc(strings.Fields(string(out)), func(name string) bool {
		return name == "GIT_CONFIG_COUNT" || name == "GIT_CONFIG_PARAMETERS"
	})

	if !slices.Contains(names, referenceBackendEnv) {
		names = append(names, referenceBackendEnv)
	}

	return names, nil
}

// referenceBackendEnv names the variable that holds the URI of a ref
// store, such as files:///path/to/repo/.git. It takes precedence over
// the extensions.refStorage key of the repository. git reads HEAD and
// every ref from the store that the variable names. git 2.56 leaves the
// variable out of the output of git rev-parse --local-env-vars.
const referenceBackendEnv = "GIT_REFERENCE_BACKEND"
