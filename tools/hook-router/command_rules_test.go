package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	stashDeniedReason   = "Do not use git stash to shelve changes. All issues in the working tree are your responsibility to fix, regardless of origin."
	cloneDeniedReason   = "Direct git clone usage is blocked. Use mcp__git__git_clone instead."
	kubectxReason       = "Do not use kubectx or kubens directly. Use mcp__kubectx__list to list contexts and mcp__kubectx__select to switch contexts."
	ghGroupAskReason    = "This gh subcommand can mutate GitHub state. Confirm before running."
	ghFallbackAskReason = "This gh subcommand is not on the read-only allowlist. Confirm before running; prefer mcp__github__* tools for reads."
)

// canonicalRules mirrors the rules wired into home/claude.nix for the
// git and kubectx bundles. TestRun in main_test.go reuses it so
// integration coverage stays in sync with matcher coverage. Update
// here when home/claude.nix gains or drops rules.
func canonicalRules() *CommandRules {
	return NewCommandRules([]CommandRule{
		{
			Command: "git",
			Args:    []string{"clone"},
			Reason:  cloneDeniedReason,
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
		want []CommandRule
	}{
		"empty string yields empty rules": {
			in:   "",
			want: nil,
		},
		"single rule round-trips": {
			in: `[{"command":"git","args":["clone"],"reason":"r"}]`,
			want: []CommandRule{
				{Command: "git", Args: []string{"clone"}, Reason: "r"},
			},
		},
		"rule with except round-trips": {
			in: `[{"command":"git","args":["stash"],"except":["pop","apply"],"reason":"r"}]`,
			want: []CommandRule{
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
			want: []CommandRule{
				{Command: "git", Args: []string{"clone"}, Reason: "r"},
			},
		},
		"explicit deny action round-trips": {
			in: `[{"command":"git","args":["clone"],"action":"deny","reason":"r"}]`,
			want: []CommandRule{
				{Command: "git", Args: []string{"clone"}, Action: "deny", Reason: "r"},
			},
		},
		"ask action round-trips": {
			in: `[{"command":"gh","args":["pr"],"except":["view"],"action":"ask","reason":"r"}]`,
			want: []CommandRule{
				{
					Command: "gh",
					Args:    []string{"pr"},
					Except:  []string{"view"},
					Action:  "ask",
					Reason:  "r",
				},
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

			rules, err := parseCommandRules(tc.in)
			if tc.err {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, rules)
			assert.Equal(t, tc.want, rules.rules)
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

// ghAskRules mirrors the gh ask rules wired into the github bundle in
// home/claude.nix: subcommand-scoped rules first, top-level fallback
// last. Update here when home/claude.nix gains or drops rules.
func ghAskRules() *CommandRules {
	group := func(name string, except ...string) CommandRule {
		return CommandRule{
			Command: "gh",
			Args:    []string{name},
			Except:  except,
			Action:  "ask",
			Reason:  ghGroupAskReason,
		}
	}

	return NewCommandRules([]CommandRule{
		group("pr", "view", "list", "diff", "checks", "status"),
		group("issue", "view", "list"),
		group("run", "view", "list", "watch"),
		group("repo", "view", "list"),
		group("release", "view", "list"),
		group("workflow", "view", "list"),
		{
			Command: "gh",
			Except: []string{
				"pr", "issue", "run", "repo", "release", "workflow",
				"search", "status", "help", "version", "--version",
			},
			Action: "ask",
			Reason: ghFallbackAskReason,
		},
	})
}

func TestCommandRulesCheck_Ask(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		"gh pr merge asks via group rule": {
			input: "gh pr merge 1",
			want:  ghGroupAskReason,
		},
		"gh pr view is exempt": {
			input: "gh pr view 1",
		},
		"gh pr view with flags is exempt": {
			input: "gh pr view 1 --json title",
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
		"gh workflow run asks": {
			input: "gh workflow run deploy.yml",
			want:  ghGroupAskReason,
		},
		"gh api hits the fallback": {
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
		"gh search is exempt at the fallback": {
			input: "gh search repos foo",
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

func TestCommandRulesCheck_Generality(t *testing.T) {
	t.Parallel()

	t.Run("user-defined rule for arbitrary command fires on bare invocation", func(t *testing.T) {
		t.Parallel()

		rules := NewCommandRules([]CommandRule{
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

		rules := NewCommandRules([]CommandRule{
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
		rules := NewCommandRules([]CommandRule{
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

		rules := NewCommandRules([]CommandRule{
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
		rules := NewCommandRules([]CommandRule{
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

		bundleRule := CommandRule{Command: "danger", Reason: "from bundle"}
		extraRule := CommandRule{Command: "danger", Reason: "from extra"}

		rules := NewCommandRules([]CommandRule{bundleRule, extraRule})
		prog := mustParse(t, "danger now")
		_, reason, denied := rules.Check(prog)
		require.True(t, denied)
		assert.Equal(t, "from bundle", reason)
	})

	t.Run("deny declared before ask wins for the same command", func(t *testing.T) {
		t.Parallel()

		// The Nix wrapper serializes deny rules before ask rules, so
		// a command covered by both resolves to deny.
		rules := NewCommandRules([]CommandRule{
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
		rules := NewCommandRules([]CommandRule{
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

		var rules *CommandRules
		prog := mustParse(t, "git stash")
		_, _, denied := rules.Check(prog)
		assert.False(t, denied)
	})

	t.Run("empty engine is a no-op match", func(t *testing.T) {
		t.Parallel()

		rules := NewCommandRules(nil)
		prog := mustParse(t, "git stash")
		_, _, denied := rules.Check(prog)
		assert.False(t, denied)
		assert.True(t, rules.Empty())
	})

	t.Run("nil engine reports empty without panic", func(t *testing.T) {
		t.Parallel()

		var rules *CommandRules
		assert.True(t, rules.Empty())
	})
}
