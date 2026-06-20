package searchrewrite_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/searchrewrite"
)

// defaultExcludes mirrors the findExcludes default wired into
// home/claude.nix. Update both when the default changes.
var defaultExcludes = []string{".git", ".worktrees", ".claude/worktrees"}

// fullConfig enables both rewrites with the default excludes.
func fullConfig() searchrewrite.Config {
	return searchrewrite.Config{
		Grep:         true,
		Find:         true,
		FindExcludes: defaultExcludes,
	}
}

// mustParse parses command into a shell AST, failing the test on a parse
// error.
func mustParse(t *testing.T, command string) *syntax.File {
	t.Helper()

	prog, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	require.NoError(t, err)

	return prog
}

func TestRewrite(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		command string
		cfg     searchrewrite.Config
		want    string
		changed bool
	}{
		"find with name glob": {
			command: `find . -name '*.go'`,
			cfg:     fullConfig(),
			want:    `bfs -exclude \( -name .git -o -name .worktrees -o -path '*.claude/worktrees' \) . -name '*.go'`,
			changed: true,
		},
		"find double-quoted glob preserves quoting": {
			command: `find . -name "*.go"`,
			cfg:     fullConfig(),
			want:    `bfs -exclude \( -name .git -o -name .worktrees -o -path '*.claude/worktrees' \) . -name "*.go"`,
			changed: true,
		},
		"bare find": {
			command: `find`,
			cfg:     fullConfig(),
			want:    `bfs -exclude \( -name .git -o -name .worktrees -o -path '*.claude/worktrees' \)`,
			changed: true,
		},
		"find at worktree root matches path exclude": {
			command: `find .claude/worktrees`,
			cfg:     fullConfig(),
			want:    `bfs -exclude \( -name .git -o -name .worktrees -o -path '*.claude/worktrees' \) .claude/worktrees`,
			changed: true,
		},
		"grep recursive with line numbers": {
			command: `grep -rn foo .`,
			cfg:     fullConfig(),
			want:    `rg -n foo . -g '!.git' -g '!.worktrees' -g '!.claude/worktrees' -g '!**/.claude/worktrees'`,
			changed: true,
		},
		"grep long flags map": {
			command: `grep --ignore-case --recursive foo src`,
			cfg:     fullConfig(),
			want:    `rg -i foo src -g '!.git' -g '!.worktrees' -g '!.claude/worktrees' -g '!**/.claude/worktrees'`,
			changed: true,
		},
		"grep unknown short flag falls through": {
			command: `grep -rZ foo .`,
			cfg:     fullConfig(),
			want:    `grep -rZ foo .`,
			changed: false,
		},
		"grep unknown long flag falls through": {
			command: `grep --color=auto foo .`,
			cfg:     fullConfig(),
			want:    `grep --color=auto foo .`,
			changed: false,
		},
		"grep quoted multi-part flag word falls through": {
			// --include='*.go' is a Lit + SglQuoted word, not a single
			// literal: it must fall through, not be reread as the pattern.
			command: `grep --include='*.go' -rn foo .`,
			cfg:     fullConfig(),
			want:    `grep --include='*.go' -rn foo .`,
			changed: false,
		},
		"grep short flag with quoted attached value falls through": {
			command: `grep -i'x' foo .`,
			cfg:     fullConfig(),
			want:    `grep -i'x' foo .`,
			changed: false,
		},
		"grep BRE pattern falls through": {
			command: `grep '\(a\)' .`,
			cfg:     fullConfig(),
			want:    `grep '\(a\)' .`,
			changed: false,
		},
		"grep BRE pattern with -E rewrites": {
			command: `grep -E '\(a\)' .`,
			cfg:     fullConfig(),
			want:    `rg -E '\(a\)' . -g '!.git' -g '!.worktrees' -g '!.claude/worktrees' -g '!**/.claude/worktrees'`,
			changed: true,
		},
		"stdin grep falls through": {
			command: `cat x | grep foo`,
			cfg:     fullConfig(),
			want:    `cat x | grep foo`,
			changed: false,
		},
		"grep disabled leaves grep alone": {
			command: `grep -rn foo .`,
			cfg:     searchrewrite.Config{Find: true, FindExcludes: defaultExcludes},
			want:    `grep -rn foo .`,
			changed: false,
		},
		"find disabled leaves find alone": {
			command: `find . -name '*.go'`,
			cfg:     searchrewrite.Config{Grep: true, FindExcludes: defaultExcludes},
			want:    `find . -name '*.go'`,
			changed: false,
		},
		"empty excludes drops the prune clause": {
			command: `find . -type f`,
			cfg:     searchrewrite.Config{Find: true},
			want:    `bfs . -type f`,
			changed: true,
		},
		"find in a pipeline rewrites the find stage only": {
			command: `find . -name '*.go' | head`,
			cfg:     fullConfig(),
			want:    `bfs -exclude \( -name .git -o -name .worktrees -o -path '*.claude/worktrees' \) . -name '*.go' | head`,
			changed: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tc.command)

			got, _, changed := searchrewrite.Rewrite(prog, tc.command, tc.cfg)
			assert.Equal(t, tc.changed, changed)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRewriteReadOnly(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		command string
		want    bool
	}{
		"single search":              {command: `find . -name '*.go'`, want: true},
		"single grep":                {command: `grep -rn foo .`, want: true},
		"search piped to filter":     {command: `rg x | head`, want: true},
		"search piped to pager":      {command: `find . | sort | uniq -c`, want: true},
		"redirection is not":         {command: `rg x > out`, want: false},
		"append redirection is not":  {command: `rg x >> out`, want: false},
		"find delete is not":         {command: `find . -delete`, want: false},
		"find exec is not":           {command: `find . -exec rm {} \;`, want: false},
		"grep piped to xargs is not": {command: `grep -r x . | xargs rm`, want: false},
		"sequence is not":            {command: `rg x; rm y`, want: false},
		"and-list is not":            {command: `rg x && rm y`, want: false},
		"background is not":          {command: `rg x &`, want: false},
		"non-allowlisted stage":      {command: `rg x | grobble`, want: false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tc.command)

			_, readOnly, _ := searchrewrite.Rewrite(prog, tc.command, fullConfig())
			assert.Equal(t, tc.want, readOnly)
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	t.Run("empty string yields disabled config", func(t *testing.T) {
		t.Parallel()

		cfg, err := searchrewrite.Parse("")
		require.NoError(t, err)
		assert.False(t, cfg.Grep)
		assert.False(t, cfg.Find)
		assert.Nil(t, cfg.FindExcludes)
	})

	t.Run("full config round-trips", func(t *testing.T) {
		t.Parallel()

		cfg, err := searchrewrite.Parse(`{"grep":true,"find":true,"findExcludes":[".git",".worktrees"]}`)
		require.NoError(t, err)
		assert.True(t, cfg.Grep)
		assert.True(t, cfg.Find)
		assert.Equal(t, []string{".git", ".worktrees"}, cfg.FindExcludes)
	})

	t.Run("malformed JSON errors", func(t *testing.T) {
		t.Parallel()

		_, err := searchrewrite.Parse(`{not json`)
		require.Error(t, err)
	})
}
