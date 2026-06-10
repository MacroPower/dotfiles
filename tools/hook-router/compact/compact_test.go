package compact_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/compact"
)

func TestParseCompactConfig(t *testing.T) {
	t.Parallel()

	type check func(t *testing.T, c *compact.Compactor)

	cases := map[string]struct {
		in    string
		err   bool
		check check
	}{
		"empty string yields disabled compactor": {
			in: "",
			check: func(t *testing.T, c *compact.Compactor) {
				t.Helper()
				assert.True(t, c.Empty())
			},
		},
		"enable=false is disabled": {
			in: `{"enable":false}`,
			check: func(t *testing.T, c *compact.Compactor) {
				t.Helper()
				assert.True(t, c.Empty())
			},
		},
		"full config round-trips": {
			in: `{"enable":true,"stripAnsi":true,"minRunLength":4,"minBytes":1024,"streams":["stdout","stderr"]}`,
			check: func(t *testing.T, c *compact.Compactor) {
				t.Helper()
				assert.False(t, c.Empty())
				assert.Equal(t, []string{"stdout", "stderr"}, c.Streams())
				assert.True(t, c.Config().StripAnsi)
				assert.Equal(t, 4, c.Config().MinRunLength)
				assert.Equal(t, 1024, c.Config().MinBytes)
			},
		},
		"single-stream config compacts only that stream": {
			in: `{"enable":true,"streams":["stdout"]}`,
			check: func(t *testing.T, c *compact.Compactor) {
				t.Helper()
				assert.False(t, c.Empty())
				assert.Equal(t, []string{"stdout"}, c.Streams())
				assert.False(t, c.Config().StripAnsi, "unset bool stays false")
				assert.Equal(t, compact.DefaultMinRunLength, c.Config().MinRunLength, "numeric default applied")
				assert.Equal(t, compact.DefaultMinBytes, c.Config().MinBytes, "numeric default applied")
			},
		},
		"enabled but no streams is empty": {
			in: `{"enable":true,"streams":[]}`,
			check: func(t *testing.T, c *compact.Compactor) {
				t.Helper()
				assert.True(t, c.Empty(), "selecting no streams compacts nothing")
				assert.Empty(t, c.Streams())
			},
		},
		"enabled with streams omitted is empty": {
			in: `{"enable":true}`,
			check: func(t *testing.T, c *compact.Compactor) {
				t.Helper()
				assert.True(t, c.Empty(), "an unset streams list compacts nothing")
			},
		},
		"non-positive numeric knobs get defaults": {
			in: `{"enable":true,"streams":["stdout"],"minRunLength":0,"minBytes":-5}`,
			check: func(t *testing.T, c *compact.Compactor) {
				t.Helper()
				assert.Equal(t, compact.DefaultMinRunLength, c.Config().MinRunLength)
				assert.Equal(t, compact.DefaultMinBytes, c.Config().MinBytes)
			},
		},
		"unknown stream rejected": {
			in:  `{"enable":true,"streams":["stdout","stdin"]}`,
			err: true,
		},
		"malformed JSON returns error": {
			in:  `{"enable":`,
			err: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c, err := compact.Parse(tc.in)
			if tc.err {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, c)
			tc.check(t, c)
		})
	}
}

// TestParseCompactConfigNixJSON pins the wire shape produced by
// builtins.toJSON on home/claude.nix's outputCompaction submodule. Nix
// emits attribute names verbatim in lexicographic order, so a rename on
// either side that broke this round-trip would mis-parse every
// PostToolUse:Bash invocation at runtime.
func TestParseCompactConfigNixJSON(t *testing.T) {
	t.Parallel()

	// Exact shape of `builtins.toJSON { enable = true; stripAnsi = true;
	// minRunLength = 3; minBytes = 2048; streams = ["stdout" "stderr"]; }`.
	in := `{"enable":true,"minBytes":2048,"minRunLength":3,"streams":["stdout","stderr"],"stripAnsi":true}`

	c, err := compact.Parse(in)
	require.NoError(t, err)
	require.False(t, c.Empty())
	assert.Equal(t, []string{"stdout", "stderr"}, c.Streams())
	assert.True(t, c.Config().StripAnsi)
	assert.Equal(t, 3, c.Config().MinRunLength)
	assert.Equal(t, 2048, c.Config().MinBytes)
}

func TestCompactorNilAndEmpty(t *testing.T) {
	t.Parallel()

	var c *compact.Compactor

	assert.True(t, c.Empty(), "nil receiver is empty")
	assert.Nil(t, c.Streams(), "nil receiver selects no streams")

	out, changed := c.Compact(strings.Repeat("x\n", 100))
	assert.False(t, changed, "nil receiver never changes output")
	assert.Equal(t, strings.Repeat("x\n", 100), out)
}

func TestCompactMarker(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "    [hook-router: +2 identical lines]", compact.Marker(2))
	assert.Equal(t, "    [hook-router: +49 identical lines]", compact.Marker(49))
}

func TestStripANSI(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want string
	}{
		"escape-free fast path":  {in: "hello world", want: "hello world"},
		"empty":                  {in: "", want: ""},
		"sgr green":              {in: "\x1b[32mok\x1b[0m", want: "ok"},
		"sgr bold red":           {in: "\x1b[1;31mX\x1b[0m", want: "X"},
		"osc title with bel":     {in: "\x1b]0;title\x07rest", want: "rest"},
		"sgr across a full line": {in: "\x1b[32mok\x1b[0m\nplain\n", want: "ok\nplain\n"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, compact.StripANSI(tc.in))
		})
	}
}

func TestCollapseRuns(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in     string
		minRun int
		want   string
	}{
		"empty": {
			in:     "",
			minRun: 3,
			want:   "",
		},
		"single line no newline": {
			in:     "abc",
			minRun: 3,
			want:   "abc",
		},
		"single line trailing newline round-trips": {
			in:     "abc\n",
			minRun: 3,
			want:   "abc\n",
		},
		"run below minRun passes through": {
			in:     "a\na\nb",
			minRun: 3,
			want:   "a\na\nb",
		},
		"run at minRun collapses": {
			in:     "a\na\na\nb",
			minRun: 3,
			want:   "a\n" + compact.Marker(2) + "\nb",
		},
		"run at start": {
			in:     "a\na\na\nb\nc",
			minRun: 3,
			want:   "a\n" + compact.Marker(2) + "\nb\nc",
		},
		"run at end": {
			in:     "x\nb\nb\nb",
			minRun: 3,
			want:   "x\nb\n" + compact.Marker(2),
		},
		"two independent runs with kept line between": {
			in:     "A\nA\nA\nB\nA\nA\nA",
			minRun: 3,
			want:   "A\n" + compact.Marker(2) + "\nB\nA\n" + compact.Marker(2),
		},
		"blank-line run collapses": {
			in:     "a\n\n\n\n",
			minRun: 3,
			want:   "a\n\n" + compact.Marker(3),
		},
		"crlf identical lines collapse (cr is content)": {
			in:     "a\r\na\r\na\r\nb",
			minRun: 3,
			want:   "a\r\n" + compact.Marker(2) + "\nb",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, compact.CollapseRuns(tc.in, tc.minRun))
		})
	}
}

func TestCompact(t *testing.T) {
	t.Parallel()

	const wide = "a-sufficiently-wide-repeated-line"

	// enabled is the common config: low MinBytes so small fixtures clear
	// the size gate, leaving the per-case behavior under test.
	enabled := compact.Config{
		Enable:       true,
		StripAnsi:    true,
		MinRunLength: 3,
		MinBytes:     1,
		Streams:      []string{"stdout", "stderr"},
	}

	cases := map[string]struct {
		cfg     compact.Config
		in      string
		want    string
		changed bool
	}{
		"disabled passes through": {
			cfg:     compact.Config{Enable: false},
			in:      strings.Repeat(wide+"\n", 50),
			want:    strings.Repeat(wide+"\n", 50),
			changed: false,
		},
		"empty string below minBytes": {
			cfg:     enabled,
			in:      "",
			want:    "",
			changed: false,
		},
		"single line no newline unchanged": {
			cfg:     enabled,
			in:      "just one line",
			want:    "just one line",
			changed: false,
		},
		"single line trailing newline unchanged": {
			cfg:     enabled,
			in:      "just one line\n",
			want:    "just one line\n",
			changed: false,
		},
		"run below minRunLength unchanged": {
			cfg:     enabled,
			in:      strings.Repeat(wide+"\n", 2),
			want:    strings.Repeat(wide+"\n", 2),
			changed: false,
		},
		"wide run collapses": {
			cfg:     enabled,
			in:      strings.Repeat(wide+"\n", 50),
			want:    wide + "\n" + compact.Marker(49) + "\n",
			changed: true,
		},
		"collapse works with stripAnsi off": {
			cfg: compact.Config{
				Enable: true, StripAnsi: false, MinRunLength: 3, MinBytes: 1,
				Streams: []string{"stdout", "stderr"},
			},
			in:      strings.Repeat(wide+"\n", 50),
			want:    wide + "\n" + compact.Marker(49) + "\n",
			changed: true,
		},
		"blank-line run collapses end-to-end": {
			cfg: compact.Config{
				Enable: true, StripAnsi: false, MinRunLength: 3, MinBytes: 1,
				Streams: []string{"stdout", "stderr"},
			},
			in:      "x" + strings.Repeat("\n", 61),
			want:    "x\n\n" + compact.Marker(60),
			changed: true,
		},
		"ansi-only stripped": {
			cfg:     enabled,
			in:      "\x1b[32mline one\x1b[0m\nline two\n",
			want:    "line one\nline two\n",
			changed: true,
		},
		"ansi plus repeats does both": {
			cfg:     enabled,
			in:      strings.Repeat("\x1b[32mok\x1b[0m\n", 5),
			want:    "ok\n" + compact.Marker(4) + "\n",
			changed: true,
		},
		"ansi-free no-repeat unchanged (fast path)": {
			cfg:     enabled,
			in:      "line one\nline two\nline three\n",
			want:    "line one\nline two\nline three\n",
			changed: false,
		},
		"below minBytes unchanged despite repeats": {
			cfg:     compact.Config{Enable: true, StripAnsi: true, Streams: []string{"stdout", "stderr"}},
			in:      strings.Repeat("xy\n", 10),
			want:    strings.Repeat("xy\n", 10),
			changed: false,
		},
		"collapse that would not shorten is suppressed (final gate)": {
			cfg: compact.Config{
				Enable: true, StripAnsi: false, MinRunLength: 3, MinBytes: 1,
				Streams: []string{"stdout", "stderr"},
			},
			in:      "ab\nab\nab\n",
			want:    "ab\nab\nab\n",
			changed: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := compact.New(tc.cfg)
			got, changed := c.Compact(tc.in)
			assert.Equal(t, tc.changed, changed)
			assert.Equal(t, tc.want, got)

			if changed {
				assert.Less(t, len(got), len(tc.in),
					"a changed result must be strictly shorter than the input")
			}
		})
	}
}
