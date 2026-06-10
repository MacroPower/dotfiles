package kubectx_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/kubectx"
)

func mustParse(t *testing.T, command string) *syntax.File {
	t.Helper()

	prog, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	require.NoError(t, err)

	return prog
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
			assert.Equal(t, tt.want, kubectx.HasKubectl(prog))
		})
	}
}

func TestKubectlKubeconfigOverride(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  bool
	}{
		"inline KUBECONFIG assignment": {
			input: "KUBECONFIG=/x kubectl get pods",
			want:  true,
		},
		"flag separate value": {
			input: "kubectl --kubeconfig /x get pods",
			want:  true,
		},
		"flag inline value": {
			input: "kubectl --kubeconfig=/x get pods",
			want:  true,
		},
		"flag in later position": {
			input: "kubectl get pods --kubeconfig /x",
			want:  true,
		},
		"inline KUBECONFIG expansion": {
			input: "KUBECONFIG=$OTHER kubectl get pods",
			want:  true,
		},
		"flag inline expansion value": {
			input: "kubectl --kubeconfig=$VAR get pods",
			want:  true,
		},
		"flag separate expansion value": {
			input: "kubectl --kubeconfig $VAR get pods",
			want:  true,
		},
		"no match: plain kubectl": {
			input: "kubectl get pods",
		},
		"no match: KUBECONFIG on non-kubectl": {
			input: "KUBECONFIG=/x helm list",
		},
		"no match: env wrapper out of scope": {
			input: "env -i kubectl get pods",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			_, got := kubectx.KubeconfigOverride(prog)
			assert.Equal(t, tt.want, got)
		})
	}
}
