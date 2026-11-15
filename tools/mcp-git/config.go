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
	// sets a key that the server refuses.
	ErrDeniedConfig = errors.New("git config of the repository is denied")

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

	// connectionPattern matches the config keys that change how git
	// connects to the host of an http or https remote. Such a key can
	// name a proxy, an address for the host, or the certificates that
	// git trusts. git then sends the credential of the host to another
	// server. The pattern matches every key under http, because a key
	// of the form http.<url>.<name> wins over each value that a git
	// option sets for http.<name>. It also matches the proxy keys of a
	// remote, which win over http.proxy.
	connectionPattern = regexp.MustCompile(`^(?:http\..*|remote\..*\.proxy(?:authmethod)?)$`)

	// worktreePattern matches the config keys that name a command that
	// git runs when it updates the working tree or writes a commit.
	// The command is a filter, a merge driver, or a program that signs
	// commits and verifies their signatures.
	//
	// The pattern also matches keys of git-lfs. The global config can
	// define git-lfs as a filter, and git-lfs then reads these keys
	// from the config of the repository. It runs the clean and smudge
	// commands of an extension. It also runs the path of a custom
	// transfer agent in a shell, with the args of the agent.
	// lfs.storage names the directory where git-lfs writes each object
	// that it downloads, and that directory can lie outside every
	// allowed directory. git keeps the case of the words extension and
	// customtransfer, because they belong to the subsection of the key.
	worktreePattern = regexp.MustCompile(
		`^(?:filter\..*\.(?:clean|smudge|process)|merge\..*\.driver` +
			`|gpg\.(?:.*\.)?program|gpg\.ssh\.defaultkeycommand` +
			`|lfs\.(?i:extension)\..*\.(?:clean|smudge)` +
			`|lfs\.(?i:customtransfer)\..*\.(?:path|args)|lfs\.storage)$`,
	)
)

// repoConfig holds what the git config of a repository asks git to
// run, and how it asks git to connect to a remote. The caller can
// write the config files of the repository, which git reports with the
// scopes local and worktree. It cannot write the system config, the
// global config, or the environment of the server.
type repoConfig struct {
	// hooks holds the name of each hook that a config file of any
	// scope defines. A hook of the global config still runs commands
	// from the working tree.
	hooks []string

	// remoteKeys holds each key that matches remoteCommandPattern or
	// connectionPattern and that a config file of the repository sets.
	remoteKeys []string

	// worktreeKeys holds each key that matches worktreePattern
	// and that a config file of the repository sets.
	worktreeKeys []string
}

// readRepoConfig lists the git config that applies in the repository
// at repo. It returns the hooks that the config defines, and the keys
// that the server refuses.
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
// a NUL. An entry for a key without a value holds no newline.
//
// A key of remoteCommandPattern or worktreePattern counts only when
// its value is not empty. An empty command key names no command, and
// git-lfs uses its default directory for an empty lfs.storage. A key
// of connectionPattern counts whatever its value is, because an empty
// value still changes the connection. An empty http.sslVerify turns
// off the certificate check, and an empty http.proxy turns off the
// proxy of the global config.
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

		if isTrustedScope(scope) {
			continue
		}

		if connectionPattern.MatchString(key) {
			cfg.remoteKeys = appendUnique(cfg.remoteKeys, key)

			continue
		}

		if value == "" {
			continue
		}

		switch {
		case remoteCommandPattern.MatchString(key):
			cfg.remoteKeys = appendUnique(cfg.remoteKeys, key)
		case worktreePattern.MatchString(key):
			cfg.worktreeKeys = appendUnique(cfg.worktreeKeys, key)
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
// files of the repository set a key that the server refuses. git
// options cannot override most of these keys. The name of such a key
// holds a part that the config chooses, such as the name of a filter
// or the URL of an http key. Other keys hold a list, as
// credential.helper does. A git option for lfs.storage also replaces
// the directory that the global config names.
//
// Every command reads the keys of remoteCommandPattern and
// connectionPattern. Only a command that updates the working tree
// reads the keys of worktreePattern, and worktree is set for such a
// command.
func (c repoConfig) check(worktree bool) error {
	keys := c.remoteKeys
	if worktree {
		keys = slices.Concat(keys, c.worktreeKeys)
	}

	if len(keys) == 0 {
		return nil
	}

	advice := "Remove each key, or move it to the global git config"

	// A fetch reads no key of worktreePattern, and git in Bash
	// applies these keys inside the sandbox.
	if len(c.remoteKeys) == 0 {
		advice += ". To update the working tree with these keys, run git_fetch, and then merge or rebase in Bash"
	}

	return fmt.Errorf(
		"%w: the repository sets %s. This server runs outside the sandbox. "+
			"It does not run a command that the config of a repository names. "+
			"It does not let that config change how git connects to a remote, or where git-lfs writes. %s",
		ErrDeniedConfig, strings.Join(keys, ", "), advice,
	)
}
