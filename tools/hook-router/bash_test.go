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
