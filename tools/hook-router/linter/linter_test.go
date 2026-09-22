package linter_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/linter"
)

func TestParse(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		err  bool
		want []linter.Rule
	}{
		"empty string yields empty rules": {
			in:   "",
			want: nil,
		},
		"single rule round-trips": {
			in: `[{"pathGlob":"/**/*.md","command":["prose-lint"]}]`,
			want: []linter.Rule{
				{PathGlob: "/**/*.md", Command: []string{"prose-lint"}},
			},
		},
		"timeout field round-trips": {
			in: `[{"pathGlob":"/**/*.md","command":["prose-lint"],"timeout":"3s"}]`,
			want: []linter.Rule{
				{PathGlob: "/**/*.md", Command: []string{"prose-lint"}, Timeout: "3s"},
			},
		},
		"unknown fields are silently dropped": {
			in: `[{"pathGlob":"/**/*.md","command":["prose-lint"],"foo":1}]`,
			want: []linter.Rule{
				{PathGlob: "/**/*.md", Command: []string{"prose-lint"}},
			},
		},
		"malformed JSON returns error": {
			in:  `[{"pathGlob":`,
			err: true,
		},
		"empty pathGlob rejected": {
			in:  `[{"pathGlob":"","command":["prose-lint"]}]`,
			err: true,
		},
		"empty command rejected": {
			in:  `[{"pathGlob":"/**/*.md","command":[]}]`,
			err: true,
		},
		"invalid pathGlob rejected": {
			in:  `[{"pathGlob":"/tmp/[bad","command":["prose-lint"]}]`,
			err: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rules, err := linter.Parse(tc.in)
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

// TestParseNixJSON pins the wire shape produced by builtins.toJSON on
// home/claude.nix's linterRuleType submodule, including the brace
// alternation the default prose-lint rule relies on.
func TestParseNixJSON(t *testing.T) {
	t.Parallel()

	in := `[{"command":["/nix/store/abc/bin/prose-lint"],"pathGlob":"/**/*.{md,go,nix}","timeout":"15s"}]`

	rules, err := linter.Parse(in)
	require.NoError(t, err)
	require.Len(t, rules.Rules(), 1)
	assert.Equal(t, "/**/*.{md,go,nix}", rules.Rules()[0].PathGlob)
	assert.Equal(t, []string{"/nix/store/abc/bin/prose-lint"}, rules.Rules()[0].Command)
	assert.Equal(t, "15s", rules.Rules()[0].Timeout)

	rule, ok := rules.Match("/home/x/repo/README.md")
	require.True(t, ok)
	assert.Equal(t, []string{"/nix/store/abc/bin/prose-lint"}, rule.Command)
}

func TestMatchBraceAlternation(t *testing.T) {
	t.Parallel()

	rules := linter.New([]linter.Rule{
		{PathGlob: "/**/*.{md,go,nix}", Command: []string{"prose-lint"}},
	})

	cases := map[string]struct {
		path    string
		wantHit bool
	}{
		"markdown at depth":      {path: "/home/x/repo/docs/guide.md", wantHit: true},
		"go at root":             {path: "/main.go", wantHit: true},
		"nix":                    {path: "/home/x/repo/home/claude.nix", wantHit: true},
		"yaml is not in the set": {path: "/home/x/repo/Taskfile.yaml", wantHit: false},
		"extension is a prefix":  {path: "/home/x/repo/a.mdx", wantHit: false},
		"no extension":           {path: "/home/x/repo/README", wantHit: false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, ok := rules.Match(tc.path)
			assert.Equal(t, tc.wantHit, ok)
		})
	}
}

func TestMatchFirstWins(t *testing.T) {
	t.Parallel()

	rules := linter.New([]linter.Rule{
		{PathGlob: "/tmp/skip/*.md", Command: []string{"true"}},
		{PathGlob: "/tmp/**/*.md", Command: []string{"prose-lint"}},
	})

	rule, ok := rules.Match("/tmp/skip/x.md")
	require.True(t, ok)
	assert.Equal(t, []string{"true"}, rule.Command)

	rule, ok = rules.Match("/tmp/other/x.md")
	require.True(t, ok)
	assert.Equal(t, []string{"prose-lint"}, rule.Command)
}

func TestNilAndEmpty(t *testing.T) {
	t.Parallel()

	t.Run("nil engine is empty and does not match", func(t *testing.T) {
		t.Parallel()

		var rules *linter.Engine
		assert.True(t, rules.Empty())
		assert.Nil(t, rules.Rules())

		_, ok := rules.Match("/tmp/x.md")
		assert.False(t, ok)
	})

	t.Run("empty engine is empty and does not match", func(t *testing.T) {
		t.Parallel()

		rules := linter.New(nil)
		assert.True(t, rules.Empty())

		_, ok := rules.Match("/tmp/x.md")
		assert.False(t, ok)
	})
}

func TestParseFindings(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		out  string
		want []linter.Finding
	}{
		"empty output": {
			out:  "",
			want: nil,
		},
		"blank lines are skipped": {
			out:  "\n\n",
			want: nil,
		},
		"one finding per line": {
			out: "/a/b.md:3:1: Style::X: bad\n/a/b.md:10:4: Style::Y: worse\n",
			want: []linter.Finding{
				{Line: 3, Text: "/a/b.md:3:1: Style::X: bad"},
				{Line: 10, Text: "/a/b.md:10:4: Style::Y: worse"},
			},
		},
		"path with a colon still parses the first digits pair": {
			out: "/a/c:d.md:7:1: msg\n",
			want: []linter.Finding{
				{Line: 7, Text: "/a/c:d.md:7:1: msg"},
			},
		},
		"unparseable line keeps Line 0": {
			out: "harper panicked\n",
			want: []linter.Finding{
				{Line: 0, Text: "harper panicked"},
			},
		},
		"stdin marker": {
			out: "<stdin>:2:5: msg\n",
			want: []linter.Finding{
				{Line: 2, Text: "<stdin>:2:5: msg"},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, linter.ParseFindings(tc.out))
		})
	}
}

func TestRanges(t *testing.T) {
	t.Parallel()

	const content = "one\ntwo\nthree\ntwo\nfive\n"

	cases := map[string]struct {
		needle string
		want   []linter.LineRange
	}{
		"single occurrence": {
			needle: "three",
			want:   []linter.LineRange{{Start: 3, End: 3}},
		},
		"repeated occurrence reports every hit": {
			needle: "two",
			want:   []linter.LineRange{{Start: 2, End: 2}, {Start: 4, End: 4}},
		},
		"multi-line needle spans its lines": {
			needle: "two\nthree",
			want:   []linter.LineRange{{Start: 2, End: 3}},
		},
		"needle with trailing newline does not extend past its last line": {
			needle: "three\n",
			want:   []linter.LineRange{{Start: 3, End: 4}},
		},
		"missing needle": {
			needle: "seven",
			want:   nil,
		},
		"empty needle": {
			needle: "",
			want:   nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, linter.Ranges(content, tc.needle))
		})
	}
}

func TestFilter(t *testing.T) {
	t.Parallel()

	findings := []linter.Finding{
		{Line: 0, Text: "unplaced"},
		{Line: 2, Text: "two"},
		{Line: 5, Text: "five"},
		{Line: 9, Text: "nine"},
	}

	cases := map[string]struct {
		ranges []linter.LineRange
		want   []linter.Finding
	}{
		"keeps findings inside a range and drops Line 0": {
			ranges: []linter.LineRange{{Start: 1, End: 5}},
			want:   []linter.Finding{{Line: 2, Text: "two"}, {Line: 5, Text: "five"}},
		},
		"union of ranges": {
			ranges: []linter.LineRange{{Start: 2, End: 2}, {Start: 9, End: 12}},
			want:   []linter.Finding{{Line: 2, Text: "two"}, {Line: 9, Text: "nine"}},
		},
		"no ranges keeps nothing": {
			ranges: nil,
			want:   nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, linter.Filter(findings, tc.ranges))
		})
	}
}

func TestFormat(t *testing.T) {
	t.Parallel()

	t.Run("empty findings render nothing", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, linter.Format("/a.md", nil, linter.MaxFindings))
	})

	t.Run("singular header and every line", func(t *testing.T) {
		t.Parallel()

		got := linter.Format("/a.md", []linter.Finding{{Line: 1, Text: "/a.md:1:1: msg"}}, linter.MaxFindings)
		assert.Equal(t, "prose-lint: 1 finding in /a.md. Fix each before continuing.\n/a.md:1:1: msg", got)
	})

	t.Run("count limit truncates with a trailer", func(t *testing.T) {
		t.Parallel()

		findings := make([]linter.Finding, 5)
		for i := range findings {
			findings[i] = linter.Finding{Line: i + 1, Text: "line"}
		}

		got := linter.Format("/a.md", findings, 2)
		assert.Equal(t, "prose-lint: 5 findings in /a.md. Fix each before continuing.\nline\nline\n... and 3 more", got)
	})

	t.Run("byte limit truncates with a trailer", func(t *testing.T) {
		t.Parallel()

		big := strings.Repeat("x", linter.MaxFormatBytes)
		findings := []linter.Finding{{Line: 1, Text: "short"}, {Line: 2, Text: big}}

		got := linter.Format("/a.md", findings, linter.MaxFindings)
		assert.Less(t, len(got), linter.MaxFormatBytes)
		assert.True(t, strings.HasSuffix(got, "short\n... and 1 more"), got)
	})
}

func TestRun(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("sh semantics differ on windows")
	}

	cases := map[string]struct {
		command []string
		timeout string
		want    []linter.Finding
		err     bool
	}{
		"exit 0 is clean": {
			command: []string{"sh", "-c", `exit 0`, "sh"},
			want:    nil,
		},
		"exit 1 with output parses findings": {
			command: []string{"sh", "-c", `printf '%s:2:1: Style::X: bad\n' "$1"; exit 1`, "sh"},
			want:    []linter.Finding{{Line: 2, Text: "PATH:2:1: Style::X: bad"}},
		},
		"exit 1 without output is clean": {
			command: []string{"sh", "-c", `exit 1`, "sh"},
			want:    nil,
		},
		"exit 2 fails": {
			command: []string{"sh", "-c", `echo boom >&2; exit 2`, "sh"},
			err:     true,
		},
		"missing binary fails": {
			command: []string{"/nonexistent/hook-router-test/linter"},
			err:     true,
		},
		"timeout fails": {
			command: []string{"sh", "-c", `sleep 5`, "sh"},
			timeout: "150ms",
			err:     true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tmp := t.TempDir()
			target := filepath.Join(tmp, "input.md")
			require.NoError(t, os.WriteFile(target, []byte("# t\n"), 0o644))

			rule := linter.Rule{PathGlob: "/**/*.md", Command: tc.command, Timeout: tc.timeout}

			start := time.Now()
			got, err := rule.Run(t.Context(), target)
			elapsed := time.Since(start)

			assert.Less(t, elapsed, 2*time.Second, "a timeout must kill the process near its budget")

			if tc.err {
				require.ErrorIs(t, err, linter.ErrLinterFailed)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)

			for i := range tc.want {
				tc.want[i].Text = strings.ReplaceAll(tc.want[i].Text, "PATH", target)
			}

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRunStderrInError(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "input.md")
	require.NoError(t, os.WriteFile(target, []byte("# t\n"), 0o644))

	rule := linter.Rule{
		PathGlob: "/**/*.md",
		Command:  []string{"sh", "-c", `echo dictionary missing >&2; exit 3`, "sh"},
	}

	_, err := rule.Run(t.Context(), target)
	require.ErrorIs(t, err, linter.ErrLinterFailed)
	assert.Contains(t, err.Error(), "dictionary missing")
}

func TestRunMissingFile(t *testing.T) {
	t.Parallel()

	rule := linter.Rule{
		PathGlob: "/**/*.md",
		Command:  []string{"sh", "-c", `echo should-not-run; exit 1`, "sh"},
	}

	got, err := rule.Run(t.Context(), "/nonexistent/hook-router-test/missing.md")
	require.NoError(t, err, "missing file should be a silent no-op")
	assert.Nil(t, got)
}

func TestRunEmptyCommand(t *testing.T) {
	t.Parallel()

	rule := linter.Rule{PathGlob: "/**/*.md"}

	_, err := rule.Run(t.Context(), "/tmp/x.md")
	require.ErrorIs(t, err, linter.ErrLinterFailed)
	assert.Contains(t, err.Error(), "empty command")
}

func TestResolveTimeout(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want time.Duration
	}{
		"empty defaults":        {in: "", want: linter.DefaultTimeout},
		"valid override":        {in: "250ms", want: 250 * time.Millisecond},
		"malformed defaults":    {in: "not-a-duration", want: linter.DefaultTimeout},
		"non-positive defaults": {in: "-1s", want: linter.DefaultTimeout},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := linter.Rule{Timeout: tc.in}
			assert.Equal(t, tc.want, r.ResolveTimeout())
		})
	}
}
