package cmdrules_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/cmdrules"
)

// mustParse parses command into a shell AST, failing the test on a
// parse error.
func mustParse(t *testing.T, command string) *syntax.File {
	t.Helper()

	prog, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	require.NoError(t, err)

	return prog
}

const (
	stashDeniedReason        = "Do not use git stash to shelve changes. All issues in the working tree are your responsibility to fix, regardless of origin."
	cloneDeniedReason        = "Direct git clone of a remote URL is blocked. Use mcp__git__git_clone instead. A clone from a local path runs in Bash."
	fetchDeniedReason        = "Direct git fetch usage is blocked. Use mcp__git__git_fetch instead. That tool refuses a remote on the local file system. Fetch from one in Bash by a literal path that starts with `/`, `./`, `../`, or `file://`, as in `git fetch ../other main`."
	pullDeniedReason         = "Direct git pull usage is blocked. Use mcp__git__git_pull instead. That tool refuses a remote on the local file system. Pull from one in Bash by a literal path that starts with `/`, `./`, `../`, or `file://`, as in `git pull ../other main`."
	pushDeniedReason         = "Direct git push usage is blocked. Use mcp__git__git_push instead."
	remoteUpdateDeniedReason = "Direct git remote update usage is blocked. Use mcp__git__git_fetch instead."
	kubectxReason            = "Do not use kubectx or kubens directly. Use mcp__kubectx__list to list contexts and mcp__kubectx__select to switch contexts."
	ghGroupAskReason         = "This gh subcommand can mutate GitHub state. Confirm before running."
	ghFallbackAskReason      = "This gh subcommand is not on the read-only allowlist. Confirm before running; prefer mcp__github__* tools for reads."
	ghAPIDeniedReason        = "`gh api` reaches api.github.com, which is denied. Clone with mcp__git__git_clone to read repository files locally, or use the mcp__github__* tools for issues, PRs, releases, and search."
	ghRepoViewTool           = "mcp__git__git_clone (the README) or mcp__github__search_repositories (repository metadata)"
)

// canonicalRules mirrors the rules wired into home/claude.nix for the
// git and kubectx bundles. The hook-router handler tests carry a copy
// of the same fixture so integration coverage stays in sync with
// matcher coverage. Update both when home/claude.nix gains or drops
// rules.
func canonicalRules() *cmdrules.Engine {
	return cmdrules.New([]cmdrules.Rule{
		{
			Command:       "git",
			Args:          []string{"clone"},
			OnlyRemoteURL: true,
			Reason:        cloneDeniedReason,
		},
		{
			Command:         "git",
			Args:            []string{"fetch"},
			ExceptLocalPath: true,
			Reason:          fetchDeniedReason,
		},
		{
			Command:         "git",
			Args:            []string{"pull"},
			ExceptLocalPath: true,
			Reason:          pullDeniedReason,
		},
		{
			Command: "git",
			Args:    []string{"push"},
			Reason:  pushDeniedReason,
		},
		{
			Command: "git",
			Args:    []string{"remote", "update"},
			Reason:  remoteUpdateDeniedReason,
		},
		{
			Command: "git",
			Args:    []string{"stash"},
			Except:  []string{"pop", "apply", "list", "show", "branch", "drop", "clear"},
			Reason:  stashDeniedReason,
		},
		{
			Command: "kubectx",
			Reason:  kubectxReason,
		},
		{
			Command: "kubens",
			Reason:  kubectxReason,
		},
	})
}

func TestParseCommandRules(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		err  bool
		want []cmdrules.Rule
	}{
		"empty string yields empty rules": {
			in:   "",
			want: nil,
		},
		"single rule round-trips": {
			in: `[{"command":"git","args":["clone"],"reason":"r"}]`,
			want: []cmdrules.Rule{
				{Command: "git", Args: []string{"clone"}, Reason: "r"},
			},
		},
		"rule with except round-trips": {
			in: `[{"command":"git","args":["stash"],"except":["pop","apply"],"reason":"r"}]`,
			want: []cmdrules.Rule{
				{
					Command: "git",
					Args:    []string{"stash"},
					Except:  []string{"pop", "apply"},
					Reason:  "r",
				},
			},
		},
		"unknown fields are silently dropped": {
			in: `[{"command":"git","args":["clone"],"reason":"r","foo":1,"bar":["x"]}]`,
			want: []cmdrules.Rule{
				{Command: "git", Args: []string{"clone"}, Reason: "r"},
			},
		},
		"explicit deny action round-trips": {
			in: `[{"command":"git","args":["clone"],"action":"deny","reason":"r"}]`,
			want: []cmdrules.Rule{
				{Command: "git", Args: []string{"clone"}, Action: "deny", Reason: "r"},
			},
		},
		"ask action round-trips": {
			in: `[{"command":"gh","args":["pr"],"except":["view"],"action":"ask","reason":"r"}]`,
			want: []cmdrules.Rule{
				{
					Command: "gh",
					Args:    []string{"pr"},
					Except:  []string{"view"},
					Action:  "ask",
					Reason:  "r",
				},
			},
		},
		"exceptBare round-trips": {
			in: `[{"command":"git","args":["remote"],"except":["-v"],"exceptBare":true,"action":"ask","reason":"r"}]`,
			want: []cmdrules.Rule{
				{
					Command:    "git",
					Args:       []string{"remote"},
					Except:     []string{"-v"},
					ExceptBare: true,
					Action:     "ask",
					Reason:     "r",
				},
			},
		},
		"onlyRemoteURL round-trips": {
			in: `[{"command":"git","args":["clone"],"onlyRemoteURL":true,"reason":"r"}]`,
			want: []cmdrules.Rule{
				{Command: "git", Args: []string{"clone"}, OnlyRemoteURL: true, Reason: "r"},
			},
		},
		"exceptLocalPath round-trips": {
			in: `[{"command":"git","args":["fetch"],"exceptLocalPath":true,"reason":"r"}]`,
			want: []cmdrules.Rule{
				{Command: "git", Args: []string{"fetch"}, ExceptLocalPath: true, Reason: "r"},
			},
		},
		"unknown action returns error": {
			in:  `[{"command":"gh","action":"prompt","reason":"r"}]`,
			err: true,
		},
		"malformed JSON returns error": {
			in:  `[{"command":`,
			err: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rules, err := cmdrules.Parse(tc.in)
			if tc.err {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, rules)
			assert.Equal(t, tc.want, rules.Rules())
		})
	}
}

func TestCommandRulesCheck_GitStash(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		"bare git stash": {
			input: "git stash",
			want:  stashDeniedReason,
		},
		"git stash push": {
			input: "git stash push",
			want:  stashDeniedReason,
		},
		"git stash push with path": {
			input: "git stash push -- file.go",
			want:  stashDeniedReason,
		},
		"git stash save": {
			input: `git stash save "wip"`,
			want:  stashDeniedReason,
		},
		"git stash -k": {
			input: "git stash -k",
			want:  stashDeniedReason,
		},
		"git stash --keep-index": {
			input: "git stash --keep-index",
			want:  stashDeniedReason,
		},
		"git stash in pipeline": {
			input: "git stash || echo fail",
			want:  stashDeniedReason,
		},
		"git stash in subshell": {
			input: "(git stash)",
			want:  stashDeniedReason,
		},
		"git -C dir stash (tightened)": {
			input: "git -C /tmp stash",
			want:  stashDeniedReason,
		},
		"git -c key=val stash (tightened)": {
			input: "git -c user.name=x stash",
			want:  stashDeniedReason,
		},
		"git --git-dir flag stash (tightened)": {
			input: "git --git-dir=/x stash",
			want:  stashDeniedReason,
		},
		"git -C quoted dir stash": {
			input: `git -C "$dir" stash`,
			want:  stashDeniedReason,
		},
		// `--keep-index` is the candidate slot (no flag-skipping
		// between args and except) and isn't in except, so deny.
		"git stash --keep-index pop denies": {
			input: "git stash --keep-index pop",
			want:  stashDeniedReason,
		},
		// `pop` is the candidate slot here, so allow.
		"git stash pop --keep-index allows": {
			input: "git stash pop --keep-index",
		},
		// Except still applies after a quoted flag value is skipped.
		"no match: git -C quoted dir stash pop": {
			input: `git -C "$dir" stash pop`,
		},
		"no match: git stash pop": {
			input: "git stash pop",
		},
		"no match: git stash apply": {
			input: "git stash apply stash@{0}",
		},
		"no match: git stash list": {
			input: "git stash list",
		},
		"no match: git stash show": {
			input: "git stash show -p",
		},
		"no match: git stash drop": {
			input: "git stash drop stash@{1}",
		},
		"no match: git stash branch": {
			input: "git stash branch newbranch",
		},
		"no match: git stash clear": {
			input: "git stash clear",
		},
		"no match: git status": {
			input: "git status",
		},
		"no match: echo git stash": {
			input: "echo git stash",
		},
		"no match: sh -c git stash": {
			input: `sh -c "git stash"`,
		},
		"no match: git help stash": {
			input: "git help stash",
		},
	}

	rules := canonicalRules()

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			_, got, denied := rules.Check(prog)
			assert.Equal(t, tt.want != "", denied)

			if denied {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestCommandRulesCheck_GitClone(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		"git clone url": {
			input: "git clone https://example.com/repo.git",
			want:  cloneDeniedReason,
		},
		"git clone with flags and dest": {
			input: "git clone --depth 1 https://example.com/repo.git /tmp/dst",
			want:  cloneDeniedReason,
		},
		"git -C dir clone": {
			input: "git -C /tmp clone https://example.com/repo.git",
			want:  cloneDeniedReason,
		},
		"git --git-dir long flag clone": {
			input: "git --git-dir=/x clone https://example.com/repo.git",
			want:  cloneDeniedReason,
		},
		"git -c key=val clone": {
			input: "git -c user.name=x clone https://example.com/repo.git",
			want:  cloneDeniedReason,
		},
		"git stacked value flags clone": {
			input: "git -C /tmp -c foo=bar clone https://example.com/repo.git",
			want:  cloneDeniedReason,
		},
		"git clone in pipeline": {
			input: "git clone https://example.com/repo.git | tee log",
			want:  cloneDeniedReason,
		},
		"git clone in compound": {
			input: "cd /tmp && git clone https://example.com/repo.git",
			want:  cloneDeniedReason,
		},
		"git clone in subshell": {
			input: "(git clone https://example.com/repo.git)",
			want:  cloneDeniedReason,
		},
		"git clone with env prefix": {
			input: "GIT_TERMINAL_PROMPT=0 git clone https://example.com/repo.git",
			want:  cloneDeniedReason,
		},
		"git -C quoted dir clone": {
			input: `git -C "$dir" clone https://example.com/repo.git`,
			want:  cloneDeniedReason,
		},
		"git -c quoted value clone": {
			input: `git -c "k=v" clone https://example.com/repo.git`,
			want:  cloneDeniedReason,
		},
		"git clone ssh url": {
			input: "git clone ssh://git@example.com/org/repo.git",
			want:  cloneDeniedReason,
		},
		"git clone scp-style url": {
			input: "git clone git@example.com:org/repo.git",
			want:  cloneDeniedReason,
		},
		"git clone host alias": {
			input: "git clone gh:org/repo dst",
			want:  cloneDeniedReason,
		},
		"git clone double-quoted url": {
			input: `git clone "https://example.com/repo.git" dst`,
			want:  cloneDeniedReason,
		},
		"git clone single-quoted url": {
			input: "git clone 'https://example.com/repo.git' dst",
			want:  cloneDeniedReason,
		},
		"git clone url with expansions after the host": {
			input: `git clone "https://example.com/$org/$repo.git"`,
			want:  cloneDeniedReason,
		},
		"git clone url with a token expansion": {
			input: `git clone "https://x-access-token:${TOKEN}@example.com/org/repo.git"`,
			want:  cloneDeniedReason,
		},
		"git clone url after option values": {
			input: "git clone --branch main --depth 1 https://example.com/repo.git dst",
			want:  cloneDeniedReason,
		},
		"git clone url with a local reference": {
			input: "git clone --reference /tmp/ref https://example.com/repo.git",
			want:  cloneDeniedReason,
		},
		"no match: git status": {
			input: "git status",
		},
		"no match: git cloner": {
			input: "git cloner",
		},
		"no match: echo git clone": {
			input: "echo git clone https://example.com/repo.git",
		},
		"no match: sh -c git clone": {
			input: `sh -c "git clone https://example.com/repo.git"`,
		},
		"no match: git help clone": {
			input: "git help clone",
		},
		"no match: git -C dir status": {
			input: "git -C /tmp status",
		},
		// A clone that names no remote URL reads a local repository.
		"no match: clone between relative names": {
			input: "git clone -q bare.git a",
		},
		"no match: clone between absolute paths": {
			input: "git clone /tmp/scratch/bare.git /tmp/scratch/work",
		},
		"no match: clone from a dot path": {
			input: "git clone ./bare.git work",
		},
		"no match: clone from a parent path": {
			input: "git clone ../bare.git",
		},
		"no match: clone from a home path": {
			input: "git clone ~/src/repo /tmp/scratch/work",
		},
		"no match: clone from a file url": {
			input: "git clone file:///tmp/scratch/bare.git work",
		},
		"no match: clone from a file url with an expansion": {
			input: `git clone "file://$S/bare.git" work`,
		},
		"no match: clone from a path with a colon after a slash": {
			input: "git clone ./a:b work",
		},
		"no match: clone between quoted variable paths": {
			input: `git clone -q "$S/bare.git" "$S/work"`,
		},
		"no match: clone from a variable": {
			input: `git clone "$src" "$dst"`,
		},
		"no match: init then clone": {
			input: `git init -q --bare "$S/bare.git" && git clone -q "$S/bare.git" "$S/work"`,
		},
		"no match: git -C dir clone between relative names": {
			input: "git -C /tmp/scratch clone bare.git work",
		},
		"no match: local clone with option values": {
			input: "git clone --bare --branch main --depth 1 file:///tmp/scratch/a b",
		},
		"no match: local clone with attached option values": {
			input: "git clone --filter=blob:none --config=core.sshCommand=ssh:x ./a b",
		},
		"no match: git clone without a repository": {
			input: "git clone",
		},
	}

	rules := canonicalRules()

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			_, got, denied := rules.Check(prog)
			assert.Equal(t, tt.want != "", denied)

			if denied {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestCommandRulesCheck_GitRemoteOps(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		"git fetch": {
			input: "git fetch",
			want:  fetchDeniedReason,
		},
		"git fetch with flags and remote": {
			input: "git fetch --tags origin",
			want:  fetchDeniedReason,
		},
		"git -C dir fetch": {
			input: "git -C /tmp/git/x fetch",
			want:  fetchDeniedReason,
		},
		"git -C quoted dir fetch": {
			input: `git -C "$dir" fetch`,
			want:  fetchDeniedReason,
		},
		"git --git-dir quoted value fetch": {
			input: `git --git-dir="$x" fetch`,
			want:  fetchDeniedReason,
		},
		"git fetch in compound pipeline": {
			input: "cd x && git fetch -q | tail -2",
			want:  fetchDeniedReason,
		},
		"git fetch from a named remote with a branch": {
			input: "git fetch upstream main",
			want:  fetchDeniedReason,
		},
		// git reads a plain name as the name of a remote. It reads the
		// name as a path only when the repository has no such remote.
		"git fetch from a plain name": {
			input: "git fetch bare.git",
			want:  fetchDeniedReason,
		},
		"git fetch a branch with a slash": {
			input: "git fetch origin feature/topic",
			want:  fetchDeniedReason,
		},
		"git fetch with a refspec": {
			input: "git fetch origin refs/heads/main:refs/remotes/origin/main",
			want:  fetchDeniedReason,
		},
		"git fetch from a url": {
			input: "git fetch https://example.com/repo.git main",
			want:  fetchDeniedReason,
		},
		"git fetch from a variable": {
			input: `git fetch "$remote" main`,
			want:  fetchDeniedReason,
		},
		// Only the text before the first expansion counts, and the
		// variable can hold a URL.
		"git fetch from a path that starts with a variable": {
			input: `git fetch "$S/bare.git" main`,
			want:  fetchDeniedReason,
		},
		"git fetch a branch that starts with a variable": {
			input: `git fetch origin "$USER/topic"`,
			want:  fetchDeniedReason,
		},
		"git fetch all": {
			input: "git fetch --all --prune",
			want:  fetchDeniedReason,
		},
		// The path of -C names the repository that fetches, not the
		// repository that it fetches from.
		"git -C relative dir fetch": {
			input: "git -C ../other fetch",
			want:  fetchDeniedReason,
		},
		"git pull": {
			input: "git pull --rebase",
			want:  pullDeniedReason,
		},
		"git pull without arguments": {
			input: "git pull",
			want:  pullDeniedReason,
		},
		"git pull from a named remote": {
			input: "git pull --rebase origin main",
			want:  pullDeniedReason,
		},
		"git push": {
			input: "git push origin main",
			want:  pushDeniedReason,
		},
		"git push force with lease": {
			input: "git push --force-with-lease",
			want:  pushDeniedReason,
		},
		// The push rule sets no ExceptLocalPath.
		"git push to a local path": {
			input: "git push ../bare.git main",
			want:  pushDeniedReason,
		},
		"git remote update": {
			input: "git remote update",
			want:  remoteUpdateDeniedReason,
		},
		// A fetch or a pull that names a local path reads a local
		// repository.
		"no match: fetch from a parent path": {
			input: "git fetch ../bare.git main",
		},
		"no match: fetch from an absolute path": {
			input: "git fetch /tmp/scratch/bare.git",
		},
		"no match: fetch from a dot path with a refspec": {
			input: "git fetch -q ./bare.git main:main",
		},
		"no match: fetch from a file url": {
			input: "git fetch file:///tmp/scratch/bare.git main",
		},
		"no match: fetch from the repository itself": {
			input: "git fetch . main:topic",
		},
		"no match: fetch from a home path": {
			input: "git fetch ~/src/repo main",
		},
		"no match: fetch from a path with an expansion": {
			input: `git fetch "/tmp/scratch/$name" main`,
		},
		"no match: fetch from a path after an option value": {
			input: "git fetch --depth 1 ../bare.git",
		},
		"no match: pull from a parent path": {
			input: "git pull ../bare.git main",
		},
		"no match: pull from an absolute path with flags": {
			input: "git pull -q --ff-only /tmp/scratch/bare.git main",
		},
		"no match: pull a branch of the repository itself": {
			input: "git pull . topic",
		},
		"no match: git -C dir pull from a parent path": {
			input: "git -C /tmp/scratch/work pull ../bare.git main",
		},
		// The remote deny carries two positionals, so the local-only
		// git remote leaves fall through.
		"no match: git remote -v": {
			input: "git remote -v",
		},
		"no match: git remote add": {
			input: "git remote add origin git@github.com:o/r.git",
		},
		"no overlap: git stash push": {
			input: "git stash push",
			want:  stashDeniedReason,
		},
		"no match: git ls-remote": {
			input: "git ls-remote origin",
		},
		"no match: git fetch-pack": {
			input: "git fetch-pack",
		},
		"no match: git help push": {
			input: "git help push",
		},
		"no match: echo git push": {
			input: "echo git push",
		},
		// A non-literal positional ends collection.
		"no match: quoted subcommand variable": {
			input: `git "$sub" origin`,
		},
		"no match: expanded args before subcommand": {
			input: "git $opts fetch",
		},
	}

	rules := canonicalRules()

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			_, got, matched := rules.Check(prog)
			assert.Equal(t, tt.want != "", matched)

			if matched {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// TestCommandRulesCheck_ExceptBare pins the ExceptBare matcher
// semantics. No production rule sets ExceptBare, so the test builds its
// own engine around a git remote ask rule.
func TestCommandRulesCheck_ExceptBare(t *testing.T) {
	t.Parallel()

	const reason = "git remote mutation"

	rules := cmdrules.New([]cmdrules.Rule{
		{
			Command:    "git",
			Args:       []string{"remote"},
			Except:     []string{"-v", "--verbose", "show", "get-url", "-h", "--help"},
			ExceptBare: true,
			Action:     "ask",
			Reason:     reason,
		},
	})

	tests := map[string]struct {
		input string
		want  string
	}{
		"git remote add asks": {
			input: "git remote add origin https://example.com/repo.git",
			want:  reason,
		},
		"git remote set-url asks": {
			input: "git remote set-url origin https://example.com/repo.git",
			want:  reason,
		},
		"git remote remove asks": {
			input: "git remote remove origin",
			want:  reason,
		},
		"git remote rename asks": {
			input: "git remote rename origin upstream",
			want:  reason,
		},
		"git remote prune asks": {
			input: "git remote prune origin",
			want:  reason,
		},
		"git -C dir remote set-url asks (flag skipping)": {
			input: "git -C /tmp remote set-url origin https://example.com/repo.git",
			want:  reason,
		},
		"git remote add in compound asks": {
			input: "git status && git remote add o u",
			want:  reason,
		},
		// Bare `git remote` is a listing, exempted via exceptBare.
		"no match: bare git remote": {
			input: "git remote",
		},
		"no match: git remote -v": {
			input: "git remote -v",
		},
		"no match: git remote --verbose": {
			input: "git remote --verbose",
		},
		"no match: git remote show": {
			input: "git remote show origin",
		},
		"no match: git remote get-url": {
			input: "git remote get-url origin",
		},
		// Bare exemption still applies after phase-1 git flag skipping.
		"no match: git -C dir remote (bare after flag skip)": {
			input: "git -C /tmp remote",
		},
		"no match: git -C dir remote -v": {
			input: "git -C /tmp remote -v",
		},
		"no match: git status": {
			input: "git status",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			rule, got, matched := rules.Check(prog)
			assert.Equal(t, tt.want != "", matched)

			if matched {
				assert.Equal(t, tt.want, got)
				assert.True(t, rule.Ask())
			}
		})
	}
}

// TestCommandRulesCheck_RemoteURLAndLocalPath pins how OnlyRemoteURL
// and ExceptLocalPath read the words after the args of a rule. The
// test builds its own engine, so each case names the word that
// decides it and no git option stands in the way.
func TestCommandRulesCheck_RemoteURLAndLocalPath(t *testing.T) {
	t.Parallel()

	const (
		remoteReason = "the call names a remote URL"
		pathReason   = "the call names no local path"
		bothReason   = "the call names a remote URL and no local path"
		exceptReason = "the call names a remote URL after no exempt word"
	)

	rules := cmdrules.New([]cmdrules.Rule{
		{Command: "sync", Args: []string{"copy"}, OnlyRemoteURL: true, Reason: remoteReason},
		{Command: "sync", Args: []string{"read"}, ExceptLocalPath: true, Reason: pathReason},
		{
			Command:         "sync",
			Args:            []string{"both"},
			OnlyRemoteURL:   true,
			ExceptLocalPath: true,
			Reason:          bothReason,
		},
		{
			Command:       "sync",
			Args:          []string{"group"},
			Except:        []string{"list"},
			OnlyRemoteURL: true,
			Reason:        exceptReason,
		},
	})

	tests := map[string]struct {
		input string
		want  string
	}{
		"remote url: scheme": {
			input: "sync copy https://example.com/x",
			want:  remoteReason,
		},
		"remote url: host and path": {
			input: "sync copy example.com:x/y",
			want:  remoteReason,
		},
		"remote url: user, host, and path": {
			input: "sync copy user@example.com:x",
			want:  remoteReason,
		},
		"remote url: colon without a slash": {
			input: "sync copy host:x",
			want:  remoteReason,
		},
		"remote url: after other words": {
			input: "sync copy --fast name ./dir https://example.com/x",
			want:  remoteReason,
		},
		"remote url: single quotes": {
			input: "sync copy 'https://example.com/x'",
			want:  remoteReason,
		},
		"remote url: double quotes": {
			input: `sync copy "https://example.com/x"`,
			want:  remoteReason,
		},
		"remote url: quotes around a part": {
			input: `sync copy "https:"//example.com/x`,
			want:  remoteReason,
		},
		"remote url: expansion after the colon": {
			input: `sync copy "host:$dir"`,
			want:  remoteReason,
		},
		"remote url: command substitution after the scheme": {
			input: "sync copy https://$(hostname)/x",
			want:  remoteReason,
		},
		"no remote url: no words": {
			input: "sync copy",
		},
		"no remote url: plain name": {
			input: "sync copy name",
		},
		"no remote url: name with a slash": {
			input: "sync copy dir/name",
		},
		"no remote url: file url": {
			input: "sync copy file:///tmp/x",
		},
		"no remote url: colon after a slash": {
			input: "sync copy dir/a:b",
		},
		"no remote url: absolute path with a colon": {
			input: "sync copy /tmp/a:b",
		},
		"no remote url: option with a url": {
			input: "sync copy --from=https://example.com/x ./dir",
		},
		"no remote url: expansion before the colon": {
			input: `sync copy "$host:x"`,
		},
		"no remote url: expansion alone": {
			input: "sync copy $src",
		},
		"no remote url: command substitution alone": {
			input: `sync copy "$(pwd)"`,
		},
		"no remote url: empty word": {
			input: `sync copy ""`,
		},
		"no local path: no words": {
			input: "sync read",
			want:  pathReason,
		},
		"no local path: plain name": {
			input: "sync read name",
			want:  pathReason,
		},
		"no local path: name with a slash": {
			input: "sync read dir/name",
			want:  pathReason,
		},
		"no local path: remote url": {
			input: "sync read https://example.com/x",
			want:  pathReason,
		},
		"no local path: option with a path": {
			input: "sync read --from=/tmp/x name",
			want:  pathReason,
		},
		"no local path: expansion alone": {
			input: `sync read "$dir"`,
			want:  pathReason,
		},
		"no local path: expansion before a slash": {
			input: `sync read "$dir/x"`,
			want:  pathReason,
		},
		"no local path: unquoted expansion before a slash": {
			input: "sync read $dir/x",
			want:  pathReason,
		},
		"no local path: quoted expansion before an unquoted slash": {
			input: `sync read "$dir"/x`,
			want:  pathReason,
		},
		"no local path: command substitution before a slash": {
			input: `sync read "$(pwd)/x"`,
			want:  pathReason,
		},
		"no local path: dot before an expansion": {
			input: `sync read ."$x"`,
			want:  pathReason,
		},
		"no local path: name that starts with a dot": {
			input: "sync read .hidden",
			want:  pathReason,
		},
		"local path: absolute": {
			input: "sync read /tmp/x",
		},
		"local path: dot slash": {
			input: "sync read ./x",
		},
		"local path: dot dot slash": {
			input: "sync read ../x",
		},
		"local path: dot": {
			input: "sync read .",
		},
		"local path: dot dot": {
			input: "sync read ..",
		},
		"local path: home": {
			input: "sync read ~/x",
		},
		"local path: file url": {
			input: "sync read file:///tmp/x",
		},
		"local path: single quotes": {
			input: "sync read '/tmp/a b'",
		},
		"local path: double quotes": {
			input: `sync read "/tmp/a b"`,
		},
		"local path: expansion after the start": {
			input: `sync read "/tmp/$name"`,
		},
		"local path: quotes around a part": {
			input: `sync read ."/"x`,
		},
		"local path: empty quotes before the start": {
			input: `sync read ""/tmp/x`,
		},
		"local path: after other words": {
			input: "sync read --fast name host:x ./dir",
		},
		"both: remote url alone": {
			input: "sync both host:x",
			want:  bothReason,
		},
		"both: remote url and local path": {
			input: "sync both host:x ./dir",
		},
		"both: local path alone": {
			input: "sync both ./dir",
		},
		"both: plain name": {
			input: "sync both name",
		},
		"except: remote url after another word": {
			input: "sync group add host:x",
			want:  exceptReason,
		},
		"except: exempt word before a remote url": {
			input: "sync group list host:x",
		},
		"except: no remote url": {
			input: "sync group add name",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			_, got, matched := rules.Check(prog)
			assert.Equal(t, tt.want != "", matched)

			if matched {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestCommandRulesCheck_Kubectx(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		"bare kubectx": {
			input: "kubectx",
			want:  kubectxReason,
		},
		"kubectx with context": {
			input: "kubectx my-context",
			want:  kubectxReason,
		},
		"kubectx list flag": {
			input: "kubectx -l",
			want:  kubectxReason,
		},
		"bare kubens": {
			input: "kubens",
			want:  kubectxReason,
		},
		"kubens with namespace": {
			input: "kubens kube-system",
			want:  kubectxReason,
		},
		"kubectx in pipeline": {
			input: "kubectx | grep prod",
			want:  kubectxReason,
		},
		"kubectx in subshell": {
			input: "(kubectx my-context)",
			want:  kubectxReason,
		},
		"no match: kubectl": {
			input: "kubectl get pods",
		},
		"no match: echo kubectx": {
			input: "echo kubectx",
		},
		"no match: sh -c kubectx": {
			input: `sh -c "kubectx"`,
		},
		"no match: unrelated": {
			input: "git status",
		},
	}

	rules := canonicalRules()

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			_, got, denied := rules.Check(prog)
			assert.Equal(t, tt.want != "", denied)

			if denied {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// ghAskRules mirrors the gh ask-rule bundle in home/claude.nix:
// subcommand-scoped rules first, top-level fallback last. Each group's
// except set is the union of its allowed read-only or local-only leaves
// and its redirected leaves, so every other subcommand asks; redirected
// leaves are caught by ghRedirectRules, which production evaluates
// first. The hook-router handler tests carry a copy of the same
// fixture. Update all when home/claude.nix gains or drops rules.
func ghAskRules() *cmdrules.Engine {
	group := func(name string, except ...string) cmdrules.Rule {
		return cmdrules.Rule{
			Command: "gh",
			Args:    []string{name},
			Except:  except,
			Action:  "ask",
			Reason:  ghGroupAskReason,
		}
	}

	return cmdrules.New([]cmdrules.Rule{
		group("alias", "list"),
		group("attestation", "download", "trusted-root", "verify"),
		group("cache", "list"),
		group("codespace", "list", "logs", "view"),
		group("config", "clear-cache", "get", "list"),
		group("discussion", "list", "view"),
		group("extension", "list", "search"),
		group("gist", "list", "view"),
		group("gpg-key", "list"),
		group("issue", "status", "list", "view"),
		group("label", "list"),
		group("org", "list"),
		group("pr", "checkout", "checks", "status", "diff", "list", "view"),
		group("project", "field-list", "item-list", "list", "view"),
		group("release", "download", "verify", "verify-asset", "list", "view"),
		group("repo", "gitignore", "license", "list", "set-default"),
		group("ruleset", "check", "list", "view"),
		group("run", "download", "watch", "view", "list"),
		group("secret", "list"),
		group("ssh-key", "list"),
		group("variable", "get", "list"),
		group("workflow", "view", "list"),
		{
			Command: "gh",
			Except: []string{
				"alias", "attestation", "cache", "codespace", "config",
				"discussion", "extension", "gist", "gpg-key", "issue",
				"label", "org",
				"pr", "project", "release", "repo", "ruleset", "run",
				"secret", "ssh-key", "variable", "workflow",
				"browse", "completion", "licenses", "status",
				"help", "version", "--version",
			},
			Action: "ask",
			Reason: ghFallbackAskReason,
		},
	})
}

// ghRedirectReason mirrors the redirect deny reason produced in
// home/claude.nix for a gh read subcommand that has a github MCP
// equivalent.
func ghRedirectReason(tool string) string {
	return "Read via " + tool + " instead of the gh CLI."
}

// ghRedirectRules mirrors the gh redirect deny-rule bundle in
// home/claude.nix: the blanket `gh api` deny, then read-only gh
// subcommands with a github MCP equivalent, denied and pointed at the
// MCP tool. The hook-router handler tests carry a copy of the same
// fixture.
func ghRedirectRules() *cmdrules.Engine {
	redirect := func(tool string, args ...string) cmdrules.Rule {
		return cmdrules.Rule{
			Command: "gh",
			Args:    args,
			Reason:  ghRedirectReason(tool),
		}
	}

	return cmdrules.New([]cmdrules.Rule{
		{
			Command: "gh",
			Args:    []string{"api"},
			Reason:  ghAPIDeniedReason,
		},
		redirect("mcp__github__get_discussion (+ mcp__github__get_discussion_comments for the thread)", "discussion", "view"),
		redirect("mcp__github__list_discussions", "discussion", "list"),
		redirect("mcp__github__get_gist", "gist", "view"),
		redirect("mcp__github__list_gists", "gist", "list"),
		redirect("mcp__github__issue_read", "issue", "view"),
		redirect("mcp__github__list_issues", "issue", "list"),
		redirect("mcp__github__list_label", "label", "list"),
		redirect("mcp__github__pull_request_read", "pr", "view"),
		redirect("mcp__github__list_pull_requests", "pr", "list"),
		redirect("mcp__github__pull_request_read (diff method)", "pr", "diff"),
		redirect("mcp__github__projects_get (get_project)", "project", "view"),
		redirect("mcp__github__projects_list (list_projects)", "project", "list"),
		redirect("mcp__github__projects_list (list_project_fields)", "project", "field-list"),
		redirect("mcp__github__projects_list (list_project_items)", "project", "item-list"),
		redirect("mcp__github__get_release_by_tag / mcp__github__get_latest_release", "release", "view"),
		redirect("mcp__github__list_releases", "release", "list"),
		redirect("mcp__github__repository_ruleset_read (get)", "ruleset", "view"),
		redirect("mcp__github__repository_ruleset_read (list)", "ruleset", "list"),
		redirect("mcp__github__repository_ruleset_read (get_rules_for_branch)", "ruleset", "check"),
		redirect("mcp__github__actions_get (get_workflow_run) / mcp__github__get_job_logs for logs", "run", "view"),
		redirect("mcp__github__actions_list (list_workflow_runs)", "run", "list"),
		redirect("mcp__github__actions_get (get_workflow)", "workflow", "view"),
		redirect("mcp__github__actions_list (list_workflows)", "workflow", "list"),
		redirect("mcp__github__search_code / search_issues / search_pull_requests / search_repositories", "search"),
	})
}

// ghWriteRedirectReason mirrors the redirect deny reason produced in
// home/claude.nix for a gh write subcommand that has a github MCP
// equivalent.
func ghWriteRedirectReason(tool string) string {
	return "Write via " + tool + " instead of the gh CLI."
}

// ghWriteRedirectRules mirrors the deny rules that home/claude.nix
// builds from ghWriteRedirectGroups. Each rule denies a mutating gh
// subcommand whose mutation a github MCP write tool covers, and its
// reason names that tool. The hook-router handler tests carry a copy
// of the same fixture.
func ghWriteRedirectRules() *cmdrules.Engine {
	redirect := func(tool string, args ...string) cmdrules.Rule {
		return cmdrules.Rule{
			Command: "gh",
			Args:    args,
			Reason:  ghWriteRedirectReason(tool),
		}
	}

	return cmdrules.New([]cmdrules.Rule{
		redirect("mcp__github__create_pull_request", "pr", "create"),
		redirect("mcp__github__update_pull_request", "pr", "edit"),
		redirect("mcp__github__update_pull_request (state: closed)", "pr", "close"),
		redirect("mcp__github__update_pull_request (state: open)", "pr", "reopen"),
		redirect("mcp__github__update_pull_request (draft: false)", "pr", "ready"),
		redirect("mcp__github__merge_pull_request", "pr", "merge"),
		redirect("mcp__github__pull_request_review_write (+ add_comment_to_pending_review for inline comments)", "pr", "review"),
		redirect("mcp__github__update_pull_request_branch", "pr", "update-branch"),
		redirect("mcp__github__actions_run_trigger (rerun_workflow_run / rerun_failed_jobs)", "run", "rerun"),
		redirect("mcp__github__actions_run_trigger (cancel_workflow_run)", "run", "cancel"),
		redirect("mcp__github__actions_run_trigger (run_workflow)", "workflow", "run"),
	})
}

// ghRules mirrors the full production gh engine: read and write
// redirect deny rules before ask rules, matching the serialization
// order in the hook-router wrapper (all deny rules precede any ask
// rule).
// ghCloneRedirectRules mirrors the gh clone-redirect deny-rule bundle
// in home/claude.nix: gh subcommands that clone a repository or read
// files out of one over the API, denied and pointed at
// mcp__git__git_clone.
func ghCloneRedirectRules() *cmdrules.Engine {
	redirect := func(tool string, args ...string) cmdrules.Rule {
		return cmdrules.Rule{
			Command: "gh",
			Args:    args,
			Reason:  ghRedirectReason(tool),
		}
	}

	return cmdrules.New([]cmdrules.Rule{
		redirect("mcp__git__git_clone", "gist", "clone"),
		redirect("mcp__git__git_clone", "repo", "clone"),
		redirect("mcp__git__git_clone", "repo", "read-dir"),
		redirect("mcp__git__git_clone", "repo", "read-file"),
		redirect(ghRepoViewTool, "repo", "view"),
	})
}

func ghRules() *cmdrules.Engine {
	rules := append(ghRedirectRules().Rules(), ghWriteRedirectRules().Rules()...)
	rules = append(rules, ghCloneRedirectRules().Rules()...)
	return cmdrules.New(append(rules, ghAskRules().Rules()...))
}

func TestCommandRulesCheck_Ask(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		"gh pr merge asks via group rule (write redirect denies in prod)": {
			input: "gh pr merge 1",
			want:  ghGroupAskReason,
		},
		"gh pr view not asked (redirect deny handles it in prod)": {
			input: "gh pr view 1",
		},
		"gh pr view with flags not asked": {
			input: "gh pr view 1 --json title",
		},
		"gh pr checks is exempt": {
			input: "gh pr checks 1",
		},
		"gh pr checkout is exempt": {
			input: "gh pr checkout 1",
		},
		"gh run download is exempt": {
			input: "gh run download 123",
		},
		"gh config get is exempt": {
			input: "gh config get editor",
		},
		"gh config set asks": {
			input: "gh config set editor vim",
			want:  ghGroupAskReason,
		},
		"gh secret set asks": {
			input: "gh secret set TOKEN",
			want:  ghGroupAskReason,
		},
		"gh repo clone asks": {
			input: "gh repo clone owner/repo",
			want:  ghGroupAskReason,
		},
		"gh repo read-file asks": {
			input: "gh repo read-file README.md",
			want:  ghGroupAskReason,
		},
		"gh codespace ssh asks": {
			input: "gh codespace ssh",
			want:  ghGroupAskReason,
		},
		"gh auth status hits the fallback": {
			input: "gh auth status",
			want:  ghFallbackAskReason,
		},
		"gh browse is exempt at the fallback": {
			input: "gh browse",
		},
		"bare gh pr asks (bare ignores except)": {
			input: "gh pr",
			want:  ghGroupAskReason,
		},
		"gh issue close asks": {
			input: "gh issue close 2",
			want:  ghGroupAskReason,
		},
		"gh issue list is exempt": {
			input: "gh issue list",
		},
		"gh run watch is exempt": {
			input: "gh run watch 123",
		},
		"gh workflow run asks (write redirect denies in prod)": {
			input: "gh workflow run deploy.yml",
			want:  ghGroupAskReason,
		},
		"gh api hits the fallback ask (redirect deny wins in prod)": {
			input: "gh api /user",
			want:  ghFallbackAskReason,
		},
		"gh auth token hits the fallback": {
			input: "gh auth token",
			want:  ghFallbackAskReason,
		},
		"bare gh asks (bare ignores except)": {
			input: "gh",
			want:  ghFallbackAskReason,
		},
		"gh search hits the fallback ask (redirect deny wins in prod)": {
			input: "gh search repos foo",
			want:  ghFallbackAskReason,
		},
		"gh --version is exempt at the fallback": {
			input: "gh --version",
		},
		"flag before subcommand fails closed": {
			input: "gh -R owner/repo pr view 1",
			want:  ghFallbackAskReason,
		},
		"gh pr merge in compound asks": {
			input: "gh pr view 1 && gh pr merge 1",
			want:  ghGroupAskReason,
		},
		"no match: unrelated command": {
			input: "git status",
		},
	}

	rules := ghAskRules()

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			rule, got, matched := rules.Check(prog)
			assert.Equal(t, tt.want != "", matched)

			if matched {
				assert.Equal(t, tt.want, got)
				assert.True(t, rule.Ask())
			}
		})
	}
}

// TestCommandRulesCheck_Redirect exercises the full production gh engine
// (redirect deny rules before ask rules): gh subcommands with a github
// MCP equivalent are denied and redirected, reads without one stay on
// gh, and mutating subcommands without one still ask.
func TestCommandRulesCheck_Redirect(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input   string
		want    string
		wantAsk bool
	}{
		"gh issue view redirects to MCP": {
			input: "gh issue view 1",
			want:  ghRedirectReason("mcp__github__issue_read"),
		},
		"gh issue list redirects to MCP": {
			input: "gh issue list",
			want:  ghRedirectReason("mcp__github__list_issues"),
		},
		"gh pr view redirects to MCP (deny beats ask exempt)": {
			input: "gh pr view 1 --json title",
			want:  ghRedirectReason("mcp__github__pull_request_read"),
		},
		"gh pr list redirects to MCP": {
			input: "gh pr list",
			want:  ghRedirectReason("mcp__github__list_pull_requests"),
		},
		"gh pr diff redirects to MCP": {
			input: "gh pr diff 2",
			want:  ghRedirectReason("mcp__github__pull_request_read (diff method)"),
		},
		"gh release view redirects to MCP": {
			input: "gh release view v1.2.3",
			want:  ghRedirectReason("mcp__github__get_release_by_tag / mcp__github__get_latest_release"),
		},
		"gh release list redirects to MCP": {
			input: "gh release list",
			want:  ghRedirectReason("mcp__github__list_releases"),
		},
		"gh search redirects to MCP": {
			input: "gh search repos foo",
			want:  ghRedirectReason("mcp__github__search_code / search_issues / search_pull_requests / search_repositories"),
		},
		"gh pr checks stays on gh (no MCP equivalent)": {
			input: "gh pr checks 1",
		},
		"gh pr status stays on gh": {
			input: "gh pr status",
		},
		"gh run view redirects to MCP": {
			input: "gh run view 123",
			want:  ghRedirectReason("mcp__github__actions_get (get_workflow_run) / mcp__github__get_job_logs for logs"),
		},
		"gh run view --log redirects to MCP": {
			input: "gh run view 123 --log",
			want:  ghRedirectReason("mcp__github__actions_get (get_workflow_run) / mcp__github__get_job_logs for logs"),
		},
		"gh run list redirects to MCP": {
			input: "gh run list --workflow ci.yaml",
			want:  ghRedirectReason("mcp__github__actions_list (list_workflow_runs)"),
		},
		"gh run watch stays on gh (streams, no MCP equivalent)": {
			input: "gh run watch 123",
		},
		"gh label list redirects to MCP": {
			input: "gh label list",
			want:  ghRedirectReason("mcp__github__list_label"),
		},
		"gh cache list stays on gh": {
			input: "gh cache list",
		},
		"gh repo view redirects (README is a file read)": {
			input: "gh repo view owner/repo",
			want:  ghRedirectReason(ghRepoViewTool),
		},
		"gh workflow view redirects to MCP": {
			input: "gh workflow view ci.yaml",
			want:  ghRedirectReason("mcp__github__actions_get (get_workflow)"),
		},
		"gh workflow list redirects to MCP": {
			input: "gh workflow list",
			want:  ghRedirectReason("mcp__github__actions_list (list_workflows)"),
		},
		"gh status stays on gh": {
			input: "gh status",
		},
		"gh pr merge redirects to MCP": {
			input: "gh pr merge 1",
			want:  ghWriteRedirectReason("mcp__github__merge_pull_request"),
		},
		"gh pr create redirects to MCP": {
			input: "gh pr create --title x",
			want:  ghWriteRedirectReason("mcp__github__create_pull_request"),
		},
		"gh pr close redirects to MCP": {
			input: "gh pr close 1",
			want:  ghWriteRedirectReason("mcp__github__update_pull_request (state: closed)"),
		},
		"gh pr review redirects to MCP": {
			input: "gh pr review 1 --approve",
			want:  ghWriteRedirectReason("mcp__github__pull_request_review_write (+ add_comment_to_pending_review for inline comments)"),
		},
		"gh pr update-branch redirects to MCP": {
			input: "gh pr update-branch 1",
			want:  ghWriteRedirectReason("mcp__github__update_pull_request_branch"),
		},
		"gh run rerun redirects to MCP": {
			input: "gh run rerun 123",
			want:  ghWriteRedirectReason("mcp__github__actions_run_trigger (rerun_workflow_run / rerun_failed_jobs)"),
		},
		"gh run cancel redirects to MCP": {
			input: "gh run cancel 123",
			want:  ghWriteRedirectReason("mcp__github__actions_run_trigger (cancel_workflow_run)"),
		},
		"gh workflow run redirects to MCP": {
			input: "gh workflow run deploy.yml",
			want:  ghWriteRedirectReason("mcp__github__actions_run_trigger (run_workflow)"),
		},
		"gh pr comment still asks (no MCP equivalent)": {
			input:   "gh pr comment 1 --body hi",
			want:    ghGroupAskReason,
			wantAsk: true,
		},
		"gh run delete still asks (no MCP equivalent)": {
			input:   "gh run delete 123",
			want:    ghGroupAskReason,
			wantAsk: true,
		},
		"gh issue create still asks": {
			input:   "gh issue create --title x",
			want:    ghGroupAskReason,
			wantAsk: true,
		},
		"gh discussion list redirects to MCP": {
			input: "gh discussion list",
			want:  ghRedirectReason("mcp__github__list_discussions"),
		},
		"gh discussion create still asks": {
			input:   "gh discussion create",
			want:    ghGroupAskReason,
			wantAsk: true,
		},
		"gh gist view redirects to MCP": {
			input: "gh gist view abc123",
			want:  ghRedirectReason("mcp__github__get_gist"),
		},
		"gh project item-list redirects to MCP": {
			input: "gh project item-list 7 --owner me",
			want:  ghRedirectReason("mcp__github__projects_list (list_project_items)"),
		},
		"gh project item-edit still asks": {
			input:   "gh project item-edit --id x",
			want:    ghGroupAskReason,
			wantAsk: true,
		},
		"gh ruleset check redirects to MCP": {
			input: "gh ruleset check main",
			want:  ghRedirectReason("mcp__github__repository_ruleset_read (get_rules_for_branch)"),
		},
		"gh repo clone redirects to git clone": {
			input: "gh repo clone owner/repo",
			want:  ghRedirectReason("mcp__git__git_clone"),
		},
		"gh repo read-file redirects to git clone": {
			input: "gh repo read-file README.md",
			want:  ghRedirectReason("mcp__git__git_clone"),
		},
		"gh gist clone redirects to git clone": {
			input: "gh gist clone abc123",
			want:  ghRedirectReason("mcp__git__git_clone"),
		},
		"gh api is denied outright": {
			input: "gh api /user",
			want:  ghAPIDeniedReason,
		},
		"gh api repo contents is denied (file browsing)": {
			input: "gh api repos/owner/repo/contents/README.md",
			want:  ghAPIDeniedReason,
		},
		"gh api graphql is denied": {
			input: "gh api graphql -f query='{viewer{login}}'",
			want:  ghAPIDeniedReason,
		},
		"gh api in compound is denied": {
			input: "gh cache list && gh api /user",
			want:  ghAPIDeniedReason,
		},
	}

	rules := ghRules()

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			rule, got, matched := rules.Check(prog)
			assert.Equal(t, tt.want != "", matched)

			if tt.want != "" {
				assert.Equal(t, tt.want, got)
				assert.Equal(t, tt.wantAsk, rule.Ask())
			}
		})
	}
}

func TestCommandRulesCheck_Generality(t *testing.T) {
	t.Parallel()

	t.Run("user-defined rule for arbitrary command fires on bare invocation", func(t *testing.T) {
		t.Parallel()

		rules := cmdrules.New([]cmdrules.Rule{
			{Command: "danger", Reason: "danger is blocked"},
		})

		prog := mustParse(t, "danger --really")
		rule, reason, denied := rules.Check(prog)
		require.True(t, denied)
		assert.Equal(t, "danger is blocked", reason)
		assert.Equal(t, "danger", rule.Command)
	})

	t.Run("git rule with empty args denies every git invocation (footgun)", func(t *testing.T) {
		t.Parallel()

		rules := cmdrules.New([]cmdrules.Rule{
			{Command: "git", Reason: "no git"},
		})

		for _, input := range []string{
			"git",
			"git status",
			"git -C /tmp log",
			"git stash pop",
		} {
			prog := mustParse(t, input)
			_, reason, denied := rules.Check(prog)
			assert.Truef(t, denied, "expected deny for %q", input)
			assert.Equal(t, "no git", reason)
		}
	})

	t.Run("non-git rules do not skip leading flags during args match", func(t *testing.T) {
		t.Parallel()

		// `kubectx --foo select`: without flag-skipping, position 1
		// is "--foo" rather than "select", so the rule does not
		// match. A git rule would skip the flag and match at
		// position 2.
		rules := cmdrules.New([]cmdrules.Rule{
			{Command: "kubectx", Args: []string{"select"}, Reason: "blocked"},
		})

		matchInput := "kubectx select my-context"
		prog := mustParse(t, matchInput)
		_, _, denied := rules.Check(prog)
		assert.True(t, denied, "expected deny for %q", matchInput)

		nonMatchInput := "kubectx --foo select"
		prog = mustParse(t, nonMatchInput)
		_, _, denied = rules.Check(prog)
		assert.False(t, denied, "expected allow for %q (non-git rules do not skip flags)", nonMatchInput)
	})

	t.Run("except with bare command+args still denies", func(t *testing.T) {
		t.Parallel()

		rules := cmdrules.New([]cmdrules.Rule{
			{
				Command: "git",
				Args:    []string{"stash"},
				Except:  []string{"pop"},
				Reason:  "blocked",
			},
		})

		prog := mustParse(t, "git stash")
		_, reason, denied := rules.Check(prog)
		require.True(t, denied)
		assert.Equal(t, "blocked", reason)
	})

	t.Run("except matches first declared candidate without flag-skipping", func(t *testing.T) {
		t.Parallel()

		// `--keep-index` is the next positional after `stash` and
		// is not in `except`, so the rule fires.
		rules := cmdrules.New([]cmdrules.Rule{
			{
				Command: "git",
				Args:    []string{"stash"},
				Except:  []string{"pop"},
				Reason:  "blocked",
			},
		})

		prog := mustParse(t, "git stash --keep-index")
		_, _, denied := rules.Check(prog)
		assert.True(t, denied)
	})

	t.Run("aggregation order: first declared rule wins", func(t *testing.T) {
		t.Parallel()

		bundleRule := cmdrules.Rule{Command: "danger", Reason: "from bundle"}
		extraRule := cmdrules.Rule{Command: "danger", Reason: "from extra"}

		rules := cmdrules.New([]cmdrules.Rule{bundleRule, extraRule})
		prog := mustParse(t, "danger now")
		_, reason, denied := rules.Check(prog)
		require.True(t, denied)
		assert.Equal(t, "from bundle", reason)
	})

	t.Run("deny declared before ask wins for the same command", func(t *testing.T) {
		t.Parallel()

		// The Nix wrapper serializes deny rules before ask rules, so
		// a command covered by both resolves to deny.
		rules := cmdrules.New([]cmdrules.Rule{
			{Command: "gh", Args: []string{"repo"}, Reason: "denied"},
			{Command: "gh", Action: "ask", Reason: "asked"},
		})

		prog := mustParse(t, "gh repo delete foo")
		rule, reason, matched := rules.Check(prog)
		require.True(t, matched)
		assert.Equal(t, "denied", reason)
		assert.False(t, rule.Ask())
	})

	t.Run("walker single-pass: first match wins across rules", func(t *testing.T) {
		t.Parallel()

		// Both rules could match `git stash` on the AST; the engine
		// returns the first declared rule's reason.
		rules := cmdrules.New([]cmdrules.Rule{
			{Command: "git", Args: []string{"stash"}, Reason: "first"},
			{Command: "git", Args: []string{"stash"}, Reason: "second"},
		})

		prog := mustParse(t, "git stash")
		_, reason, denied := rules.Check(prog)
		require.True(t, denied)
		assert.Equal(t, "first", reason)
	})

	t.Run("nil engine is a no-op match", func(t *testing.T) {
		t.Parallel()

		var rules *cmdrules.Engine
		prog := mustParse(t, "git stash")
		_, _, denied := rules.Check(prog)
		assert.False(t, denied)
	})

	t.Run("empty engine is a no-op match", func(t *testing.T) {
		t.Parallel()

		rules := cmdrules.New(nil)
		prog := mustParse(t, "git stash")
		_, _, denied := rules.Check(prog)
		assert.False(t, denied)
		assert.True(t, rules.Empty())
	})

	t.Run("nil engine reports empty without panic", func(t *testing.T) {
		t.Parallel()

		var rules *cmdrules.Engine
		assert.True(t, rules.Empty())
	})
}

// TestCommandRulesCheck_GrepFind covers the top-level
// extraCommandRules.deny rules wired in home/claude.nix that steer Bash
// toward rg/fd (and the Grep/Glob tools) over grep/find. Both are bare
// deny rules keyed on the command head literal.
func TestCommandRulesCheck_GrepFind(t *testing.T) {
	t.Parallel()

	const (
		grepReason = "use rg"
		findReason = "use fd"
	)

	rules := cmdrules.New([]cmdrules.Rule{
		{Command: "grep", Reason: grepReason},
		{Command: "find", Reason: findReason},
	})

	cases := map[string]struct {
		command    string
		want       bool
		wantReason string
	}{
		"bare grep": {
			command:    "grep foo bar.txt",
			want:       true,
			wantReason: grepReason,
		},
		"grep in pipeline": {
			command:    "cat x | grep foo",
			want:       true,
			wantReason: grepReason,
		},
		"bare find": {
			command:    "find . -name x",
			want:       true,
			wantReason: findReason,
		},
		// `git grep` head literal is `git`, not `grep`, so the
		// grep rule must not catch it.
		"git grep is not grep": {
			command: "git grep foo",
			want:    false,
		},
		// `grep` here is an argument of `xargs`, not a command
		// head, so the matcher does not see it.
		"grep as xargs argument": {
			command: "cat list | xargs grep foo",
			want:    false,
		},
		"rg is allowed": {
			command: "rg foo",
			want:    false,
		},
		"fd is allowed": {
			command: "fd x",
			want:    false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tc.command)
			_, reason, denied := rules.Check(prog)
			assert.Equalf(t, tc.want, denied, "deny mismatch for %q", tc.command)

			if tc.want {
				assert.Equal(t, tc.wantReason, reason)
			}
		})
	}
}
