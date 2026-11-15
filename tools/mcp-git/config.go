package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var (
	// ErrDeniedConfig is returned when the git config of a repository
	// names a command that the server does not run.
	ErrDeniedConfig = errors.New("git config names a command")

	// ErrConfigList is returned when git prints a config listing that
	// does not pair each scope with a key.
	ErrConfigList = errors.New("git config listing is malformed")

	// hookKeyPattern matches the config keys that define a hook, and
	// it captures the name of the hook.
	hookKeyPattern = regexp.MustCompile(`^hook\.(.*)\.(?:command|event)$`)

	// remoteCommandPattern matches the config keys that name a command
	// that git runs when it contacts a remote. The command is a
	// credential helper, or the program that serves a fetch or a push.
	remoteCommandPattern = regexp.MustCompile(
		`^(?:credential\.(?:.*\.)?helper|remote\..*\.(?:uploadpack|receivepack))$`,
	)

	// worktreeCommandPattern matches the config keys that name a
	// command that git runs when it updates the working tree or writes
	// a commit. The command is a filter, a merge driver, or a program
	// that signs commits and verifies their signatures.
	worktreeCommandPattern = regexp.MustCompile(
		`^(?:filter\..*\.(?:clean|smudge|process)|merge\..*\.driver|gpg\.(?:.*\.)?program|gpg\.ssh\.defaultkeycommand)$`,
	)
)

// repoConfig holds what the git config of a repository asks git to
// run. The caller can write the config files of the repository, which
// git reports with the scopes local and worktree. It cannot write the
// system config, the global config, or the environment of the server.
type repoConfig struct {
	// hooks holds the name of each hook that a config file of any
	// scope defines. A hook of the global config still runs commands
	// from the working tree.
	hooks []string

	// remoteCommands holds each key that matches remoteCommandPattern
	// and that a config file of the repository sets.
	remoteCommands []string

	// worktreeCommands holds each key that matches
	// worktreeCommandPattern and that a config file of the repository
	// sets.
	worktreeCommands []string
}

// readRepoConfig lists the git config that applies in the repository
// at repo, and returns the hooks and the commands that it names.
func readRepoConfig(ctx context.Context, repo string) (repoConfig, error) {
	list, err := gitValue(ctx, "-C", repo, "config", "--list", "--show-scope", "-z")
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return repoConfig{}, err
		}

		return repoConfig{}, fmt.Errorf("reading git config: %w", err)
	}

	return parseConfigList(list)
}

// readUserHooks returns the name of each hook that the git config
// defines outside every repository. That config holds the system
// config, the global config, and the config of the command line. The
// listing runs in the root directory. A repository at the root adds
// only the names of its own hooks to the result.
func readUserHooks(ctx context.Context) ([]string, error) {
	cfg, err := readRepoConfig(ctx, string(filepath.Separator))
	if err != nil {
		return nil, err
	}

	return cfg.hooks, nil
}

// parseConfigList reads the output of git config --list --show-scope
// -z. Each entry holds a scope, a NUL, a key, a newline, a value, and
// a NUL. An entry for a key without a value holds no newline. A key
// that is set to an empty value names no command.
func parseConfigList(list string) (repoConfig, error) {
	var cfg repoConfig

	if list == "" {
		return cfg, nil
	}

	fields := strings.Split(strings.TrimSuffix(list, "\x00"), "\x00")
	if len(fields)%2 != 0 {
		return cfg, ErrConfigList
	}

	for i := 0; i < len(fields); i += 2 {
		scope := fields[i]
		key, value, _ := strings.Cut(fields[i+1], "\n")

		if m := hookKeyPattern.FindStringSubmatch(key); m != nil {
			cfg.hooks = appendUnique(cfg.hooks, m[1])

			continue
		}

		if value == "" || isTrustedScope(scope) {
			continue
		}

		switch {
		case remoteCommandPattern.MatchString(key):
			cfg.remoteCommands = appendUnique(cfg.remoteCommands, key)
		case worktreeCommandPattern.MatchString(key):
			cfg.worktreeCommands = appendUnique(cfg.worktreeCommands, key)
		}
	}

	return cfg, nil
}

// isTrustedScope reports whether scope names a part of the git config
// that the caller cannot write. Every scope that git adds later counts
// as a scope that the caller can write.
func isTrustedScope(scope string) bool {
	return scope == "system" || scope == "global" || scope == "command"
}

// appendUnique appends value to list unless list already holds it.
func appendUnique(list []string, value string) []string {
	if slices.Contains(list, value) {
		return list
	}

	return append(list, value)
}

// check returns an error that wraps [ErrDeniedConfig] when the config
// files of the repository name a command that git options cannot turn
// off. The name of such a key holds a part that the config chooses,
// such as the name of a filter, or the key holds a list, as
// credential.helper does. Every command reads the keys of
// remoteCommandPattern. Only a command that updates the working tree
// reads the keys of worktreeCommandPattern, and worktree is set for
// such a command.
func (c repoConfig) check(worktree bool) error {
	keys := c.remoteCommands
	if worktree {
		keys = slices.Concat(keys, c.worktreeCommands)
	}

	if len(keys) == 0 {
		return nil
	}

	advice := "Remove each key, or move it to the global git config"

	// A fetch reads no key of worktreeCommandPattern, and git in Bash
	// runs the commands inside the sandbox.
	if len(c.remoteCommands) == 0 {
		advice += ". To update the working tree with these commands, run git_fetch, and then merge or rebase in Bash"
	}

	return fmt.Errorf(
		"%w: the repository sets %s. This server runs outside the sandbox, "+
			"so it does not run a command that the config of a repository names. %s",
		ErrDeniedConfig, strings.Join(keys, ", "), advice,
	)
}
