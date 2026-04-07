package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"
)

func mustParse(t *testing.T, command string) *syntax.File {
	t.Helper()

	prog, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	require.NoError(t, err)

	return prog
}

func TestCheckGitStashDenied(t *testing.T) {
	t.Parallel()

	const stashDenied = "Do not use git stash to shelve changes. All issues in the working tree are your responsibility to fix, regardless of origin."

	tests := map[string]struct {
		input string
		want  string
	}{
		"bare git stash": {
			input: "git stash",
			want:  stashDenied,
		},
		"git stash push": {
			input: "git stash push",
			want:  stashDenied,
		},
		"git stash push with path": {
			input: "git stash push -- file.go",
			want:  stashDenied,
		},
		"git stash save": {
			input: `git stash save "wip"`,
			want:  stashDenied,
		},
		"git stash -k": {
			input: "git stash -k",
			want:  stashDenied,
		},
		"git stash --keep-index": {
			input: "git stash --keep-index",
			want:  stashDenied,
		},
		"git stash in pipeline": {
			input: "git stash || echo fail",
			want:  stashDenied,
		},
		"git stash in subshell": {
			input: "(git stash)",
			want:  stashDenied,
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

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			got, denied := checkGitStashDenied(prog)
			assert.Equal(t, tt.want != "", denied)

			if denied {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestCheckGitSubcmdDenied(t *testing.T) {
	t.Parallel()

	const cloneDenied = "Direct git clone usage is blocked. Use mcp__git__git_clone instead."

	tests := map[string]struct {
		input string
		want  string
	}{
		"git clone url": {
			input: "git clone https://example.com/repo.git",
			want:  cloneDenied,
		},
		"git clone with flags and dest": {
			input: "git clone --depth 1 https://example.com/repo.git /tmp/dst",
			want:  cloneDenied,
		},
		"git -C dir clone": {
			input: "git -C /tmp clone https://example.com/repo.git",
			want:  cloneDenied,
		},
		"git --git-dir long flag clone": {
			input: "git --git-dir=/x clone https://example.com/repo.git",
			want:  cloneDenied,
		},
		"git -c key=val clone": {
			input: "git -c user.name=x clone https://example.com/repo.git",
			want:  cloneDenied,
		},
		"git stacked value flags clone": {
			input: "git -C /tmp -c foo=bar clone https://example.com/repo.git",
			want:  cloneDenied,
		},
		"git clone in pipeline": {
			input: "git clone https://example.com/repo.git | tee log",
			want:  cloneDenied,
		},
		"git clone in compound": {
			input: "cd /tmp && git clone https://example.com/repo.git",
			want:  cloneDenied,
		},
		"git clone in subshell": {
			input: "(git clone https://example.com/repo.git)",
			want:  cloneDenied,
		},
		"git clone with env prefix": {
			input: "GIT_TERMINAL_PROMPT=0 git clone https://example.com/repo.git",
			want:  cloneDenied,
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

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			_, got, denied := checkGitSubcmdDenied(prog)
			assert.Equal(t, tt.want != "", denied)

			if denied {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestCheckKubectxDenied(t *testing.T) {
	t.Parallel()

	const kubectxDeniedMsg = "Do not use kubectx or kubens directly. Use mcp__kubectx__list to list contexts and mcp__kubectx__select to switch contexts."

	tests := map[string]struct {
		input string
		want  string
	}{
		"bare kubectx": {
			input: "kubectx",
			want:  kubectxDeniedMsg,
		},
		"kubectx with context": {
			input: "kubectx my-context",
			want:  kubectxDeniedMsg,
		},
		"kubectx list flag": {
			input: "kubectx -l",
			want:  kubectxDeniedMsg,
		},
		"bare kubens": {
			input: "kubens",
			want:  kubectxDeniedMsg,
		},
		"kubens with namespace": {
			input: "kubens kube-system",
			want:  kubectxDeniedMsg,
		},
		"kubectx in pipeline": {
			input: "kubectx | grep prod",
			want:  kubectxDeniedMsg,
		},
		"kubectx in subshell": {
			input: "(kubectx my-context)",
			want:  kubectxDeniedMsg,
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

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			got, denied := checkKubectxDenied(prog)
			assert.Equal(t, tt.want != "", denied)

			if denied {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestHasKubectl(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  bool
	}{
		"simple command": {
			input: "kubectl get pods",
			want:  true,
		},
		"bare kubectl": {
			input: "kubectl",
			want:  true,
		},
		"pipeline": {
			input: "kubectl get pods | grep foo",
			want:  true,
		},
		"subshell": {
			input: "(kubectl get ns)",
			want:  true,
		},
		"chained": {
			input: "kubectl get pods && kubectl get svc",
			want:  true,
		},
		"no match: already wrapped": {
			input: "kubectl-claude get pods",
		},
		"no match: echo": {
			input: "echo kubectl",
		},
		"no match: sh -c": {
			input: `sh -c "kubectl get pods"`,
		},
		"no match: unrelated": {
			input: "git status",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			assert.Equal(t, tt.want, hasKubectl(prog))
		})
	}
}
