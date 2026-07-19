package typography_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/typography"
)

func TestDisallowed(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		r    rune
		want bool
	}{
		"ascii hyphen":           {r: '-', want: false},
		"em dash":                {r: '—', want: true},
		"en dash":                {r: '–', want: true},
		"figure dash":            {r: '‒', want: true},
		"horizontal bar":         {r: '―', want: true},
		"non-breaking hyphen":    {r: '‑', want: true},
		"minus sign":             {r: '−', want: true},
		"left double quote":      {r: '“', want: true},
		"right double quote":     {r: '”', want: true},
		"left single quote":      {r: '‘', want: true},
		"right single quote":     {r: '’', want: true},
		"single low-9 quote":     {r: '‚', want: true},
		"double low-9 quote":     {r: '„', want: true},
		"ellipsis":               {r: '…', want: true},
		"left guillemet":         {r: '«', want: false},
		"right guillemet":        {r: '»', want: false},
		"ascii letter":           {r: 'a', want: false},
		"ascii apostrophe":       {r: '\'', want: false},
		"ascii double quote":     {r: '"', want: false},
		"ascii period":           {r: '.', want: false},
		"box drawing horizontal": {r: '─', want: false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, typography.Disallowed(tc.r))
		})
	}
}

func TestIntroduced(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		changes []typography.Change
		want    []rune
	}{
		"new file em dash": {
			changes: []typography.Change{{Before: "", After: "a — b"}},
			want:    []rune{'—'},
		},
		"preserved dash": {
			changes: []typography.Change{{Before: "x — y", After: "x — y z"}},
			want:    nil,
		},
		"moved same count": {
			changes: []typography.Change{{Before: "a — b", After: "b — a"}},
			want:    nil,
		},
		"added beyond existing": {
			changes: []typography.Change{{Before: "a — b", After: "a — b — c"}},
			want:    []rune{'—'},
		},
		"ascii double dash": {
			changes: []typography.Change{{Before: "", After: "a -- b"}},
			want:    nil,
		},
		"minus introduced": {
			changes: []typography.Change{{Before: "", After: "v: −5"}},
			want:    []rune{'−'},
		},
		"removed dash": {
			changes: []typography.Change{{Before: "a — b", After: "a - b"}},
			want:    nil,
		},
		"curly quotes introduced": {
			changes: []typography.Change{{Before: "", After: "don’t “x”"}},
			want:    []rune{'’', '“', '”'},
		},
		"ellipsis introduced": {
			changes: []typography.Change{{Before: "", After: "wait…"}},
			want:    []rune{'…'},
		},
		"mixed classes": {
			changes: []typography.Change{{Before: "", After: "en – “q”…"}},
			want:    []rune{'–', '“', '”', '…'},
		},
		"preserved quote new dash": {
			changes: []typography.Change{{Before: "“x”", After: "“x” — y"}},
			want:    []rune{'—'},
		},
		"cross-change move nets to zero": {
			changes: []typography.Change{
				{Before: "a — b", After: "a b"},
				{Before: "c d", After: "c — d"},
			},
			want: nil,
		},
		"cross-change net add": {
			changes: []typography.Change{
				{Before: "a — b", After: "a b"},
				{Before: "c d", After: "c — d — e"},
			},
			want: []rune{'—'},
		},
		"no changes": {
			changes: nil,
			want:    nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := typography.Introduced(tc.changes...)

			runes := make([]rune, 0, len(got))
			for _, f := range got {
				runes = append(runes, f.Rune)
			}

			if tc.want == nil {
				assert.Empty(t, runes)
			} else {
				assert.Equal(t, tc.want, runes, "findings must be sorted by rune")
			}
		})
	}
}

func TestIntroducedClasses(t *testing.T) {
	t.Parallel()

	got := typography.Introduced(typography.Change{Before: "", After: "a — “b”…"})
	require.Len(t, got, 4)

	byRune := map[rune]typography.Class{}
	for _, f := range got {
		byRune[f.Rune] = f.Class
	}

	assert.Equal(t, typography.ClassDash, byRune['—'])
	assert.Equal(t, typography.ClassQuote, byRune['“'])
	assert.Equal(t, typography.ClassQuote, byRune['”'])
	assert.Equal(t, typography.ClassEllipsis, byRune['…'])
}

func TestIntroducedSamples(t *testing.T) {
	t.Parallel()

	got := typography.Introduced(typography.Change{
		Before: "",
		After:  "first — line\nclean line\n  second — line  \n",
	})
	require.Len(t, got, 1)

	assert.Equal(t, []string{"first — line", "second — line"}, got[0].Samples,
		"samples must be trimmed and only contain lines with the rune")
}

func TestReason(t *testing.T) {
	t.Parallel()

	t.Run("empty findings", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, typography.Reason("", nil))
	})

	t.Run("dash finding", func(t *testing.T) {
		t.Parallel()

		findings := typography.Introduced(typography.Change{Before: "", After: "the plan — discussed"})
		got := typography.Reason("/tmp/notes.md", findings)

		assert.Contains(t, got, "/tmp/notes.md")
		assert.Contains(t, got, "U+2014 EM DASH")
		assert.Contains(t, got, `"--"`)
		assert.Contains(t, got, "restructure the sentence")
		assert.Contains(t, got, "the plan — discussed")
		assert.Contains(t, got, "Only newly introduced characters are blocked")
	})

	t.Run("quote finding", func(t *testing.T) {
		t.Parallel()

		findings := typography.Introduced(typography.Change{Before: "", After: "don’t"})
		got := typography.Reason("", findings)

		assert.Contains(t, got, "U+2019 RIGHT SINGLE QUOTATION MARK")
		assert.Contains(t, got, "ASCII straight quotes")
	})

	t.Run("ellipsis finding", func(t *testing.T) {
		t.Parallel()

		findings := typography.Introduced(typography.Change{Before: "", After: "wait…"})
		got := typography.Reason("", findings)

		assert.Contains(t, got, "U+2026 HORIZONTAL ELLIPSIS")
		assert.Contains(t, got, `"..."`)
	})

	t.Run("all classes grouped", func(t *testing.T) {
		t.Parallel()

		findings := typography.Introduced(typography.Change{Before: "", After: "a — “b”…"})
		got := typography.Reason("", findings)

		assert.Contains(t, got, "Dashes (")
		assert.Contains(t, got, "Curly quotes (")
		assert.Contains(t, got, "Ellipsis (")
	})
}
