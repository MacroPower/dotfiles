package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listEntry returns one entry of the listing that git config --list
// --show-scope -z prints.
func listEntry(scope, key, value string) string {
	return scope + "\x00" + key + "\n" + value + "\x00"
}

func TestParseConfigList(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		list string
		want repoConfig
		err  error
	}{
		"empty listing": {},
		"keys that name no command": {
			list: listEntry("local", "remote.origin.url", "https://example.invalid/r.git") +
				listEntry("local", "core.hookspath", ".husky") +
				listEntry("local", "credential.username", "me") +
				listEntry("local", "credential.helperselector.selected", "manager") +
				listEntry("local", "filter.lfs.required", "true") +
				listEntry("local", "merge.conflictstyle", "diff3") +
				listEntry("local", "merge.x.name", "a driver") +
				listEntry("local", "gpg.format", "ssh") +
				listEntry("local", "hook.jobs", "2") +
				listEntry("local", "hook.check.enabled", "true") +
				listEntry("local", "hook.check.parallel", "true"),
		},
		// A hook of the global config still runs commands from the
		// working tree, so the scope of a hook does not matter.
		"hooks of every scope": {
			list: listEntry("system", "hook.audit.command", "audit") +
				listEntry("global", "hook.lint.event", "pre-push") +
				listEntry("local", "hook.check.command", "check") +
				listEntry("worktree", "hook.tree.command", "tree") +
				listEntry("command", "hook.env.command", "env"),
			want: repoConfig{hooks: []string{"audit", "lint", "check", "tree", "env"}},
		},
		"hook with a command and events": {
			list: listEntry("local", "hook.check.command", "check") +
				listEntry("local", "hook.check.event", "pre-push") +
				listEntry("local", "hook.check.event", "reference-transaction"),
			want: repoConfig{hooks: []string{"check"}},
		},
		"hook names keep their case and their punctuation": {
			list: listEntry("local", "hook.a.b c.command", "x") +
				listEntry("local", "hook.x=y.event", "pre-push") +
				listEntry("local", "hook..command", "x") +
				listEntry("local", "hook.UPPER.command", "x") +
				listEntry("local", "hook.pre-push.command", "x"),
			want: repoConfig{hooks: []string{"a.b c", "x=y", "", "UPPER", "pre-push"}},
		},
		// The server disables a hook whatever its command is.
		"hook with an empty command": {
			list: listEntry("local", "hook.check.command", ""),
			want: repoConfig{hooks: []string{"check"}},
		},
		"commands for a remote": {
			list: listEntry("local", "credential.helper", "store") +
				listEntry("local", "credential.https://example.invalid.helper", "!f() { :; }; f") +
				listEntry("local", "remote.origin.uploadpack", "/x/upload-pack") +
				listEntry("local", "remote.a.b.receivepack", "/x/receive-pack"),
			want: repoConfig{remoteKeys: []string{
				"credential.helper",
				"credential.https://example.invalid.helper",
				"remote.origin.uploadpack",
				"remote.a.b.receivepack",
			}},
		},
		"commands for the working tree": {
			list: listEntry("local", "filter.lfs.clean", "git-lfs clean -- %f") +
				listEntry("local", "filter.lfs.smudge", "git-lfs smudge -- %f") +
				listEntry("local", "filter.lfs.process", "git-lfs filter-process") +
				listEntry("local", "merge.x.driver", "driver %O %A %B") +
				listEntry("local", "gpg.program", "/x/gpg") +
				listEntry("local", "gpg.openpgp.program", "/x/gpg") +
				listEntry("local", "gpg.x509.program", "/x/gpgsm") +
				listEntry("local", "gpg.ssh.program", "/x/ssh-keygen") +
				listEntry("local", "gpg.ssh.defaultkeycommand", "/x/keys"),
			want: repoConfig{worktreeKeys: []string{
				"filter.lfs.clean",
				"filter.lfs.smudge",
				"filter.lfs.process",
				"merge.x.driver",
				"gpg.program",
				"gpg.openpgp.program",
				"gpg.x509.program",
				"gpg.ssh.program",
				"gpg.ssh.defaultkeycommand",
			}},
		},
		// git-lfs reads these keys when the global config defines it
		// as a filter. lfs.storage names the directory where git-lfs
		// writes each object that it downloads.
		"keys of git-lfs": {
			list: listEntry("local", "lfs.extension.x.clean", "clean %f") +
				listEntry("local", "lfs.extension.x.smudge", "smudge %f") +
				listEntry("local", "lfs.extension.x.priority", "0") +
				listEntry("local", "lfs.customtransfer.agent.path", "/x/agent") +
				listEntry("local", "lfs.customtransfer.agent.args", "; command") +
				listEntry("local", "lfs.customTransfer.Agent.path", "/x/agent") +
				listEntry("local", "lfs.Extension.X.smudge", "smudge %f") +
				listEntry("local", "lfs.storage", "/x/lfs") +
				listEntry("worktree", "lfs.storage", "../../x") +
				listEntry("local", "lfs.customtransfer.agent.concurrent", "false") +
				listEntry("local", "lfs.standalonetransferagent", "agent") +
				listEntry("local", "lfs.repositoryformatversion", "0") +
				listEntry("local", "lfs.https://example.invalid/r.git/info/lfs.access", "basic"),
			want: repoConfig{worktreeKeys: []string{
				"lfs.extension.x.clean",
				"lfs.extension.x.smudge",
				"lfs.customtransfer.agent.path",
				"lfs.customtransfer.agent.args",
				"lfs.customTransfer.Agent.path",
				"lfs.Extension.X.smudge",
				"lfs.storage",
			}},
		},
		"keys that change the connection": {
			list: listEntry("local", "http.proxy", "http://proxy.invalid:8080") +
				listEntry("local", "http.https://github.com/.proxy", "socks5://proxy.invalid") +
				listEntry("local", "http.sslverify", "false") +
				listEntry("local", "http.https://github.com/.sslverify", "false") +
				listEntry("local", "http.sslcainfo", "/x/ca.pem") +
				listEntry("local", "http.sslcapath", "/x/ca") +
				listEntry("local", "http.curloptresolve", "github.com:443:192.0.2.1") +
				listEntry("local", "http.cookiefile", "/x/cookies") +
				listEntry("local", "http.postbuffer", "1048576") +
				listEntry("worktree", "http.extraheader", "X: y") +
				listEntry("local", "remote.origin.proxy", "http://proxy.invalid:8080") +
				listEntry("local", "remote.a.b.proxyauthmethod", "basic"),
			want: repoConfig{remoteKeys: []string{
				"http.proxy",
				"http.https://github.com/.proxy",
				"http.sslverify",
				"http.https://github.com/.sslverify",
				"http.sslcainfo",
				"http.sslcapath",
				"http.curloptresolve",
				"http.cookiefile",
				"http.postbuffer",
				"http.extraheader",
				"remote.origin.proxy",
				"remote.a.b.proxyauthmethod",
			}},
		},
		// An empty http.sslVerify is false, and an empty proxy turns
		// off the proxy of the global config.
		"connection keys with an empty value and without a value": {
			list: listEntry("local", "http.sslverify", "") +
				listEntry("local", "http.proxy", "") +
				"local\x00http.sslverify\x00" +
				"local\x00http.emptyauth\x00" +
				listEntry("local", "remote.origin.proxy", ""),
			want: repoConfig{remoteKeys: []string{
				"http.sslverify",
				"http.proxy",
				"http.emptyauth",
				"remote.origin.proxy",
			}},
		},
		"keys of a remote that change no connection": {
			list: listEntry("local", "remote.origin.url", "https://example.invalid/r.git") +
				listEntry("local", "remote.origin.push", "refs/heads/a:refs/heads/b") +
				listEntry("local", "remote.proxy.url", "https://example.invalid/r.git") +
				listEntry("local", "httpx.proxy", "http://proxy.invalid") +
				listEntry("local", "core.worktree", "/x"),
		},
		// The caller cannot write these parts of the config.
		"keys of the system, the global config, and the environment": {
			list: listEntry("system", "credential.helper", "store") +
				listEntry("global", "credential.https://github.com.helper", "/x/gh auth git-credential") +
				listEntry("global", "filter.lfs.process", "git-lfs filter-process") +
				listEntry("global", "lfs.customtransfer.agent.path", "/x/agent") +
				listEntry("global", "lfs.storage", "/x/lfs") +
				listEntry("system", "http.proxy", "http://proxy.invalid:8080") +
				listEntry("global", "http.sslcainfo", "/x/ca.pem") +
				listEntry("command", "http.sslverify", "false") +
				listEntry("command", "gpg.program", "/x/gpg"),
		},
		"commands of the worktree config and of a scope without a name": {
			list: listEntry("worktree", "credential.helper", "store") +
				listEntry("unknown", "filter.x.clean", "clean") +
				listEntry("unknown", "http.proxy", "http://proxy.invalid:8080"),
			want: repoConfig{
				remoteKeys:   []string{"credential.helper", "http.proxy"},
				worktreeKeys: []string{"filter.x.clean"},
			},
		},
		// An empty value names no command. For credential.helper, it
		// clears the helpers that come before it. git-lfs uses its
		// default directory for an empty lfs.storage.
		"keys with an empty value": {
			list: listEntry("local", "credential.helper", "") +
				listEntry("local", "filter.x.smudge", "") +
				listEntry("local", "lfs.customtransfer.agent.args", "") +
				listEntry("local", "lfs.storage", ""),
		},
		"key without a value": {
			list: "local\x00submodule.recurse\x00" +
				listEntry("local", "credential.helper", "store"),
			want: repoConfig{remoteKeys: []string{"credential.helper"}},
		},
		"value with a newline": {
			list: listEntry("local", "credential.helper", "!f() {\n:\n}; f"),
			want: repoConfig{remoteKeys: []string{"credential.helper"}},
		},
		"key that the config sets more than one time": {
			list: listEntry("local", "credential.helper", "cache") +
				listEntry("local", "credential.helper", "store"),
			want: repoConfig{remoteKeys: []string{"credential.helper"}},
		},
		"scope without a key": {
			list: listEntry("local", "credential.helper", "store") + "local\x00",
			err:  ErrConfigList,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := parseConfigList(tt.list)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRepoConfigCheck(t *testing.T) {
	t.Parallel()

	// fetchAdvice is the part of the message that tells the caller
	// how to update the working tree without the server.
	const fetchAdvice = "run git_fetch"

	tests := map[string]struct {
		cfg      repoConfig
		worktree bool

		// want holds the keys that the message names, or nil when the
		// config passes.
		want []string

		// advice is set when the message tells the caller to fetch
		// and then merge in Bash.
		advice bool
	}{
		"no command": {},
		"hooks alone": {
			cfg:      repoConfig{hooks: []string{"check"}},
			worktree: true,
		},
		"command for a remote": {
			cfg:  repoConfig{remoteKeys: []string{"credential.helper"}},
			want: []string{"credential.helper"},
		},
		"command for a remote and a working tree update": {
			cfg:      repoConfig{remoteKeys: []string{"credential.helper"}},
			worktree: true,
			want:     []string{"credential.helper"},
		},
		"command for the working tree": {
			cfg: repoConfig{worktreeKeys: []string{"filter.x.smudge"}},
		},
		"command for the working tree and a working tree update": {
			cfg:      repoConfig{worktreeKeys: []string{"filter.x.smudge", "merge.x.driver"}},
			worktree: true,
			want:     []string{"filter.x.smudge", "merge.x.driver"},
			advice:   true,
		},
		// A fetch refuses the repository too, so the message must not
		// tell the caller to fetch.
		"commands of both kinds and a working tree update": {
			cfg: repoConfig{
				remoteKeys:   []string{"remote.origin.uploadpack"},
				worktreeKeys: []string{"gpg.program"},
			},
			worktree: true,
			want:     []string{"remote.origin.uploadpack", "gpg.program"},
		},
		"commands of both kinds": {
			cfg: repoConfig{
				remoteKeys:   []string{"remote.origin.uploadpack"},
				worktreeKeys: []string{"gpg.program"},
			},
			want: []string{"remote.origin.uploadpack"},
		},
		"key that changes the connection": {
			cfg:  repoConfig{remoteKeys: []string{"http.proxy"}},
			want: []string{"http.proxy"},
		},
		// A fetch refuses the repository too, so the message must not
		// tell the caller to fetch.
		"key that changes the connection and a working tree update": {
			cfg: repoConfig{
				remoteKeys:   []string{"http.sslverify"},
				worktreeKeys: []string{"lfs.extension.x.clean"},
			},
			worktree: true,
			want:     []string{"http.sslverify", "lfs.extension.x.clean"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.check(tt.worktree)
			if tt.want == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, ErrDeniedConfig)
			assert.Contains(t, err.Error(), "the repository sets "+strings.Join(tt.want, ", ")+".")
			assert.Equal(t, tt.advice, strings.Contains(err.Error(), fetchAdvice))
		})
	}
}

func TestReadRepoConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// setup changes the config of the repository at repo.
		setup func(t *testing.T, repo string)
		want  repoConfig
		err   bool
	}{
		"new repository": {
			setup: func(*testing.T, string) {},
		},
		"hook and commands": {
			setup: func(t *testing.T, repo string) {
				t.Helper()

				runGitCmds(t, [][]string{
					{"git", "-C", repo, "config", "hook.check.command", "check"},
					{"git", "-C", repo, "config", "hook.check.event", "pre-push"},
					{"git", "-C", repo, "config", "credential.helper", "store"},
					{"git", "-C", repo, "config", "filter.x.smudge", "smudge"},
					{"git", "-C", repo, "config", "http.https://example.invalid/.sslVerify", "false"},
					{"git", "-C", repo, "config", "lfs.customTransfer.Agent.path", "agent"},
				})
			},
			want: repoConfig{
				hooks:        []string{"check"},
				remoteKeys:   []string{"credential.helper", "http.https://example.invalid/.sslverify"},
				worktreeKeys: []string{"filter.x.smudge", "lfs.customTransfer.Agent.path"},
			},
		},
		// git reports the keys of an included file with the scope of
		// the file that includes it.
		"file that the config includes": {
			setup: func(t *testing.T, repo string) {
				t.Helper()

				included := filepath.Join(t.TempDir(), "included")
				require.NoError(t, os.WriteFile(
					included,
					[]byte("[hook \"check\"]\n\tcommand = check\n[merge \"x\"]\n\tdriver = driver\n"),
					0o644,
				))
				runGitCmds(t, [][]string{
					{"git", "-C", repo, "config", "include.path", included},
				})
			},
			want: repoConfig{
				hooks:        []string{"check"},
				worktreeKeys: []string{"merge.x.driver"},
			},
		},
		"worktree config": {
			setup: func(t *testing.T, repo string) {
				t.Helper()

				runGitCmds(t, [][]string{
					{"git", "-C", repo, "config", "extensions.worktreeConfig", "true"},
					{"git", "-C", repo, "config", "--worktree", "gpg.program", "gpg"},
				})
			},
			want: repoConfig{worktreeKeys: []string{"gpg.program"}},
		},
		"key in another case": {
			setup: func(t *testing.T, repo string) {
				t.Helper()

				appendGitConfig(t, repo, "[Credential]\n\tHelper = store\n[Hook \"Check\"]\n\tCommand = check\n")
			},
			want: repoConfig{
				hooks:      []string{"Check"},
				remoteKeys: []string{"credential.helper"},
			},
		},
		"broken config": {
			setup: func(t *testing.T, repo string) {
				t.Helper()

				appendGitConfig(t, repo, "[credential\n")
			},
			err: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := filepath.Join(t.TempDir(), "repo")
			runGitCmds(t, [][]string{{"git", "init", "-q", repo}})
			tt.setup(t, repo)

			got, err := readRepoConfig(t.Context(), repo)
			if tt.err {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// git lists the global config and the config of the command line
// outside every repository.
func TestReadUserHooks(t *testing.T) { //nolint:paralleltest // t.Setenv
	global := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(global, []byte("[hook \"global\"]\n\tcommand = check\n"), 0o644))

	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "hook.command line.event")
	t.Setenv("GIT_CONFIG_VALUE_0", "post-checkout")

	got, err := readUserHooks(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"global", "command line"}, got)
}
