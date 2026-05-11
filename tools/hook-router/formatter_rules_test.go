package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFormatterRules(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		err  bool
		want []FormatterRule
	}{
		"empty string yields empty rules": {
			in:   "",
			want: nil,
		},
		"single rule round-trips": {
			in: `[{"pathGlob":"/tmp/plans/*.md","command":["mdformat"]}]`,
			want: []FormatterRule{
				{PathGlob: "/tmp/plans/*.md", Command: []string{"mdformat"}},
			},
		},
		"timeout field round-trips": {
			in: `[{"pathGlob":"/tmp/*.md","command":["mdformat"],"timeout":"3s"}]`,
			want: []FormatterRule{
				{PathGlob: "/tmp/*.md", Command: []string{"mdformat"}, Timeout: "3s"},
			},
		},
		"unknown fields are silently dropped": {
			in: `[{"pathGlob":"/tmp/*.md","command":["mdformat"],"foo":1}]`,
			want: []FormatterRule{
				{PathGlob: "/tmp/*.md", Command: []string{"mdformat"}},
			},
		},
		"malformed JSON returns error": {
			in:  `[{"pathGlob":`,
			err: true,
		},
		"empty pathGlob rejected": {
			in:  `[{"pathGlob":"","command":["mdformat"]}]`,
			err: true,
		},
		"empty command rejected": {
			in:  `[{"pathGlob":"/tmp/*.md","command":[]}]`,
			err: true,
		},
		"invalid pathGlob rejected": {
			in:  `[{"pathGlob":"/tmp/[bad","command":["mdformat"]}]`,
			err: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rules, err := parseFormatterRules(tc.in)
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

// TestParseFormatterRulesNixJSON pins the wire shape produced by
// builtins.toJSON on home/claude.nix's formatterRuleType submodule.
// The Nix attribute names are emitted verbatim, so a future rename
// on either side that breaks this round-trip would fail every
// PostToolUse invocation at runtime.
func TestParseFormatterRulesNixJSON(t *testing.T) {
	t.Parallel()

	// Exact shape of `builtins.toJSON [{ pathGlob = "/x/plans/*.md"; command = ["/nix/store/abc/bin/mdformat"]; timeout = "5s"; }]`.
	in := `[{"command":["/nix/store/abc/bin/mdformat"],"pathGlob":"/x/plans/*.md","timeout":"5s"}]`

	rules, err := parseFormatterRules(in)
	require.NoError(t, err)
	require.Len(t, rules.rules, 1)
	assert.Equal(t, "/x/plans/*.md", rules.rules[0].PathGlob)
	assert.Equal(t, []string{"/nix/store/abc/bin/mdformat"}, rules.rules[0].Command)
	assert.Equal(t, "5s", rules.rules[0].Timeout)

	rule, ok := rules.Match("/x/plans/today.md")
	require.True(t, ok)
	assert.Equal(t, []string{"/nix/store/abc/bin/mdformat"}, rule.Command)
}

func TestFormatterRulesMatch(t *testing.T) {
	t.Parallel()

	rules := NewFormatterRules([]FormatterRule{
		{
			PathGlob: "/home/x/.claude/plans/*.md",
			Command:  []string{"mdformat"},
		},
		{
			PathGlob: "/srv/research/*.md",
			Command:  []string{"mdformat"},
		},
	})

	cases := map[string]struct {
		path     string
		wantHit  bool
		wantGlob string
	}{
		"positive match in plans dir": {
			path:     "/home/x/.claude/plans/2025-01-12.md",
			wantHit:  true,
			wantGlob: "/home/x/.claude/plans/*.md",
		},
		"positive match in research dir": {
			path:     "/srv/research/foo.md",
			wantHit:  true,
			wantGlob: "/srv/research/*.md",
		},
		"prefix collision: plans-archive must not match plans": {
			path:    "/home/x/.claude/plans-archive/old.md",
			wantHit: false,
		},
		"wrong extension under plans": {
			path:    "/home/x/.claude/plans/notes.txt",
			wantHit: false,
		},
		"unrelated path": {
			path:    "/tmp/random.md",
			wantHit: false,
		},
		"subdirectory below plans does not match (no recursive **)": {
			path:    "/home/x/.claude/plans/sub/nested.md",
			wantHit: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rule, ok := rules.Match(tc.path)
			assert.Equal(t, tc.wantHit, ok)

			if tc.wantHit {
				assert.Equal(t, tc.wantGlob, rule.PathGlob)
			}
		})
	}
}

func TestFormatterRulesMatchFirstWins(t *testing.T) {
	t.Parallel()

	rules := NewFormatterRules([]FormatterRule{
		{PathGlob: "/tmp/*.md", Command: []string{"first"}},
		{PathGlob: "/tmp/*.md", Command: []string{"second"}},
	})

	rule, ok := rules.Match("/tmp/x.md")
	require.True(t, ok)
	assert.Equal(t, []string{"first"}, rule.Command)
}

func TestFormatterRulesNilAndEmpty(t *testing.T) {
	t.Parallel()

	t.Run("nil engine is empty and does not match", func(t *testing.T) {
		t.Parallel()

		var rules *FormatterRules
		assert.True(t, rules.Empty())

		_, ok := rules.Match("/tmp/x.md")
		assert.False(t, ok)
	})

	t.Run("empty engine is empty and does not match", func(t *testing.T) {
		t.Parallel()

		rules := NewFormatterRules(nil)
		assert.True(t, rules.Empty())

		_, ok := rules.Match("/tmp/x.md")
		assert.False(t, ok)
	})
}

func TestFormatterRuleRunTimeout(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("sleep semantics differ on windows")
	}

	tmp := t.TempDir()
	target := filepath.Join(tmp, "input.md")
	require.NoError(t, os.WriteFile(target, []byte("# t"), 0o644))

	rule := FormatterRule{
		PathGlob: filepath.Join(tmp, "*.md"),
		Command:  []string{"sleep", "5"},
		Timeout:  "150ms",
	}

	start := time.Now()
	err := rule.Run(t.Context(), target)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 2*time.Second, "expected hard kill near timeout, not 5s sleep")
}

func TestFormatterRuleRunMissingFile(t *testing.T) {
	t.Parallel()

	rule := FormatterRule{
		PathGlob: "/tmp/*.md",
		Command:  []string{"sh", "-c", "echo should-not-run"},
	}

	err := rule.Run(t.Context(), "/tmp/definitely-does-not-exist-xyz.md")
	assert.NoError(t, err, "missing file should be a silent no-op")
}

func TestFormatterRuleRunEmptyCommand(t *testing.T) {
	t.Parallel()

	rule := FormatterRule{PathGlob: "/tmp/*.md"}
	err := rule.Run(t.Context(), "/tmp/x.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty command")
}

func TestFormatterRuleResolveTimeout(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want time.Duration
	}{
		"empty defaults": {in: "", want: defaultFormatterTimeout},
		"valid override": {in: "250ms", want: 250 * time.Millisecond},
		"malformed defaults": {in: "not-a-duration", want: defaultFormatterTimeout},
		"non-positive defaults": {in: "-1s", want: defaultFormatterTimeout},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := FormatterRule{Timeout: tc.in}
			assert.Equal(t, tc.want, r.ResolveTimeout())
		})
	}
}

func TestHandlePostFileWriteTools(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	const before = "# t\n\n\n\nbar\n"

	rule := FormatterRule{
		PathGlob: filepath.Join(tmp, "*.md"),
		// `tr -s '\n'` collapses runs of newlines, proving the formatter
		// actually ran without relying on mdformat being on PATH.
		Command: []string{"sh", "-c", `tr -s '\n' < "$1" > "$1.tmp" && mv "$1.tmp" "$1"`, "sh"},
	}

	cfg := config{formatterRules: NewFormatterRules([]FormatterRule{rule})}
	logger := slog.New(slog.DiscardHandler)

	for _, toolName := range []string{"Write", "Edit", "MultiEdit"} {
		t.Run(toolName, func(t *testing.T) {
			require.NoError(t, os.WriteFile(target, []byte(before), 0o644))

			input, err := json.Marshal(map[string]any{
				"tool_name":  toolName,
				"tool_input": map[string]any{"file_path": target},
			})
			require.NoError(t, err)

			err = handlePostFileWrite(t.Context(), input, cfg, logger)
			require.NoError(t, err)

			got, err := os.ReadFile(target)
			require.NoError(t, err)
			assert.NotContains(t, string(got), "\n\n\n", "blank-line run should be collapsed for "+toolName)
		})
	}
}

func TestHandlePostFileWriteNoMatch(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	const before = "# t\n\n\n\nbar\n"
	require.NoError(t, os.WriteFile(target, []byte(before), 0o644))

	// Glob in an unrelated directory — rule must not fire.
	rule := FormatterRule{
		PathGlob: "/var/empty/should-never-match/*.md",
		Command:  []string{"sh", "-c", `echo bad > "$1"`, "sh"},
	}

	cfg := config{formatterRules: NewFormatterRules([]FormatterRule{rule})}
	logger := slog.New(slog.DiscardHandler)

	input, err := json.Marshal(map[string]any{
		"tool_name":  "Write",
		"tool_input": map[string]any{"file_path": target},
	})
	require.NoError(t, err)

	err = handlePostFileWrite(t.Context(), input, cfg, logger)
	require.NoError(t, err)

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, before, string(got))
}

func TestHandlePostFileWriteEmptyEngine(t *testing.T) {
	t.Parallel()

	cfg := config{formatterRules: NewFormatterRules(nil)}
	logger := slog.New(slog.DiscardHandler)

	input := []byte(`{"tool_name":"Write","tool_input":{"file_path":"/tmp/x.md"}}`)

	err := handlePostFileWrite(t.Context(), input, cfg, logger)
	assert.NoError(t, err)
}

func TestHandlePostFileWriteMissingFilePath(t *testing.T) {
	t.Parallel()

	rule := FormatterRule{
		PathGlob: "/tmp/*.md",
		Command:  []string{"true"},
	}

	cfg := config{formatterRules: NewFormatterRules([]FormatterRule{rule})}
	logger := slog.New(slog.DiscardHandler)

	cases := map[string]string{
		"no tool_input":         `{"tool_name":"Write"}`,
		"tool_input has no key": `{"tool_name":"Write","tool_input":{}}`,
		"file_path empty":       `{"tool_name":"Write","tool_input":{"file_path":""}}`,
		"file_path wrong type":  `{"tool_name":"Write","tool_input":{"file_path":42}}`,
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := handlePostFileWrite(t.Context(), []byte(in), cfg, logger)
			assert.NoError(t, err)
		})
	}
}

func TestHandlePostFileWriteFormatterFailureSwallowed(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	require.NoError(t, os.WriteFile(target, []byte("# t\n"), 0o644))

	rule := FormatterRule{
		PathGlob: filepath.Join(tmp, "*.md"),
		Command:  []string{"sh", "-c", "exit 7", "sh"},
	}

	cfg := config{formatterRules: NewFormatterRules([]FormatterRule{rule})}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	input, err := json.Marshal(map[string]any{
		"tool_name":  "Write",
		"tool_input": map[string]any{"file_path": target},
	})
	require.NoError(t, err)

	err = handlePostFileWrite(t.Context(), input, cfg, logger)
	require.NoError(t, err, "formatter exit codes must not propagate")
	assert.Contains(t, strings.ToLower(buf.String()), "formatter run failed",
		"non-zero exit should be logged at warn")
}
