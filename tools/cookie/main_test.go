package main

import (
	"math/rand/v2"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDropsTrailingEmpty(t *testing.T) {
	t.Parallel()

	got := parse("hello\n%\nworld\n%\n")

	require.Len(t, got, 2)
	assert.Equal(t, "hello", got[0])
	assert.Equal(t, "world", got[1])
}

func TestFilterShortRespectsBound(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", shortLen+1)
	mid := strings.Repeat("y", shortLen)
	short := "z"

	got := filterShort([]string{long, mid, short, ""}, shortLen)

	require.Len(t, got, 2)

	for _, e := range got {
		assert.LessOrEqual(t, len(e), shortLen)
		assert.NotEmpty(t, e)
	}
}

func TestEmbeddedFilesShortMode(t *testing.T) {
	t.Parallel()

	entries, err := fortunes.ReadDir("fortunes")
	require.NoError(t, err)
	require.NotEmpty(t, entries, "fortunes/ embed yielded no files")

	for _, entry := range entries {
		t.Run(entry.Name(), func(t *testing.T) {
			t.Parallel()

			data, err := fortunes.ReadFile("fortunes/" + entry.Name())
			require.NoError(t, err)

			all := parse(string(data))
			short := filterShort(all, shortLen)

			// Catch drift where one byte change pushes entries
			// over the SHORT bound. Clamp to the file size so
			// tiny corpora (pratchett has only 3 quotes) still
			// pass when every entry fits.
			want := min(len(all), 5)

			assert.GreaterOrEqualf(t, len(short), want,
				"file %q kept %d/%d entries <= %d bytes; SHORT bound may be too tight",
				entry.Name(), len(short), len(all), shortLen,
			)
		})
	}
}

func TestPickRandomNeverEmits_LeadingInteger(t *testing.T) {
	t.Parallel()

	leading := regexp.MustCompile(`^\d+$`)
	rng := rand.New(rand.NewPCG(1, 2))

	all, err := loadAll()
	require.NoError(t, err)
	require.NotEmpty(t, all)

	for range 200 {
		got := pickRandom(rng, all)
		first, _, _ := strings.Cut(got, "\n")
		assert.Falsef(t, leading.MatchString(first),
			"first line %q matches ^[0-9]+$", first,
		)
	}
}

func TestPickRandomDeterministic(t *testing.T) {
	t.Parallel()

	entries := []string{"a", "b", "c", "d", "e"}

	rng1 := rand.New(rand.NewPCG(42, 99))
	rng2 := rand.New(rand.NewPCG(42, 99))

	for range 50 {
		assert.Equal(t, pickRandom(rng1, entries), pickRandom(rng2, entries))
	}
}

func TestWrap(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input string
		width int
		want  []string
	}{
		"empty input yields one blank line": {
			input: "",
			width: 10,
			want:  []string{""},
		},
		"single short word": {
			input: "hi",
			width: 40,
			want:  []string{"hi"},
		},
		"multiple words fit on one line": {
			input: "foo bar baz",
			width: 40,
			want:  []string{"foo bar baz"},
		},
		"split on whitespace when over width": {
			input: "aaa bbb ccc",
			width: 5,
			want:  []string{"aaa", "bbb", "ccc"},
		},
		"oversize token gets hard-broken": {
			input: "aaaaaa",
			width: 4,
			want:  []string{"aaaa", "aa"},
		},
		"degenerate width 1 puts every char on its own line": {
			input: "ab cd",
			width: 1,
			want:  []string{"a", "b", "c", "d"},
		},
		"greedy packing fills lines": {
			input: "a b c d e f",
			width: 5,
			want:  []string{"a b c", "d e f"},
		},
		"single newline joins as space": {
			input: "foo\nbar",
			width: 40,
			want:  []string{"foo bar"},
		},
		"double newline breaks paragraph": {
			input: "foo\n\nbar",
			width: 40,
			want:  []string{"foo", "bar"},
		},
		"triple newline collapses to one break": {
			input: "foo\n\n\nbar",
			width: 40,
			want:  []string{"foo", "bar"},
		},
		"newline plus tab breaks and strips tab": {
			input: "Less is more or less more\n\t-- Y_Plentyn on #LinuxGER",
			width: 40,
			want:  []string{"Less is more or less more", "-- Y_Plentyn on #LinuxGER"},
		},
		"newline plus space breaks": {
			input: "foo\n bar",
			width: 40,
			want:  []string{"foo", "bar"},
		},
		"newline plus multiple spaces strips all": {
			input: "foo\n   bar",
			width: 40,
			want:  []string{"foo", "bar"},
		},
		"newline plus multiple tabs strips all": {
			input: "foo\n\t\tbar",
			width: 40,
			want:  []string{"foo", "bar"},
		},
		"newline space newline soft-joins second newline": {
			input: "foo\n \nbar",
			width: 40,
			want:  []string{"foo", "bar"},
		},
		"leading single newline dropped": {
			input: "\nfoo",
			width: 40,
			want:  []string{"foo"},
		},
		"leading double newline dropped": {
			input: "\n\nfoo",
			width: 40,
			want:  []string{"foo"},
		},
		"trailing single newline dropped": {
			input: "foo\n",
			width: 40,
			want:  []string{"foo"},
		},
		"crlf cr is whitespace lf soft-joins": {
			input: "foo\r\nbar",
			width: 40,
			want:  []string{"foo bar"},
		},
		"reflow forces hard break of new oversize token": {
			input: "abcdef\nghij",
			width: 5,
			want:  []string{"abcde", "f", "ghij"},
		},
		"source-wrapped paragraph reflows then attribution": {
			input: "Feel free to contact me (flames about my english and the useless of this\n" +
				"driver will be redirected to /dev/null, oh no, it's full...).\n" +
				"\t-- Michael Beck",
			width: 40,
			want: []string{
				"Feel free to contact me (flames about my",
				"english and the useless of this driver",
				"will be redirected to /dev/null, oh no,",
				"it's full...).",
				"-- Michael Beck",
			},
		},
		"mixed double newline and indent": {
			input: "a\n\n\tb",
			width: 40,
			want:  []string{"a", "b"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := wrap(tc.input, tc.width)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestBubble(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		lines []string
		width int
		want  string
	}{
		"single line sized to text": {
			lines: []string{"hi"},
			width: 40,
			want: " ╭────╮\n" +
				" │ hi │\n" +
				" ╰────╯",
		},
		"multi-line pads to width": {
			lines: []string{"foo", "bar"},
			width: 5,
			want: " ╭───────╮\n" +
				" │ foo   │\n" +
				" │ bar   │\n" +
				" ╰───────╯",
		},
		"three-line pads every row": {
			lines: []string{"aa", "bb", "cc"},
			width: 4,
			want: " ╭──────╮\n" +
				" │ aa   │\n" +
				" │ bb   │\n" +
				" │ cc   │\n" +
				" ╰──────╯",
		},
		"empty single line collapses to body 0": {
			lines: []string{""},
			width: 10,
			want: " ╭──╮\n" +
				" │  │\n" +
				" ╰──╯",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := bubble(tc.lines, tc.width)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRenderContainsCowAndFortune(t *testing.T) {
	t.Parallel()

	entries, err := cows.ReadDir("cows")
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	const fortune = "the quick brown fox jumps over the lazy dog"

	words := strings.Fields(fortune)

	for _, entry := range entries {
		t.Run(entry.Name(), func(t *testing.T) {
			t.Parallel()

			body, err := cows.ReadFile("cows/" + entry.Name())
			require.NoError(t, err)

			name := strings.TrimSuffix(entry.Name(), ".cow")
			rng := rand.New(rand.NewPCG(7, 11))

			got, err := render(rng, fortune, name, 40)
			require.NoError(t, err)

			assert.Truef(t, strings.HasSuffix(got, string(body)),
				"render output for %q should end with the cow body verbatim", name,
			)

			for _, w := range words {
				assert.Containsf(t, got, w, "fortune word %q missing from render output", w)
			}
		})
	}
}

func TestFormatEmptyCowReturnsBareFortune(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(1, 2))

	got, err := format(rng, "hello world", "", defaultWidth)
	require.NoError(t, err)
	assert.Equal(t, "hello world", got)
}

func TestPickCowUnknownName(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(1, 2))

	_, err := pickCow(rng, "doesnotexist")
	require.ErrorIs(t, err, ErrUnknownCow)
}

func TestPickCowRandomDeterministic(t *testing.T) {
	t.Parallel()

	rng1 := rand.New(rand.NewPCG(123, 456))
	rng2 := rand.New(rand.NewPCG(123, 456))

	for range 20 {
		a, err := pickCow(rng1, randomCow)
		require.NoError(t, err)

		b, err := pickCow(rng2, randomCow)
		require.NoError(t, err)

		assert.Equal(t, a, b)
	}
}

func TestRandomSentinelReserved(t *testing.T) {
	t.Parallel()

	_, err := cows.ReadFile("cows/" + randomCow + ".cow")
	require.Error(t, err, "cows/random.cow must not exist; the name is reserved as the random-pick sentinel")
}

func TestEmbeddedCowFilesNonEmpty(t *testing.T) {
	t.Parallel()

	entries, err := cows.ReadDir("cows")
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, entry := range entries {
		t.Run(entry.Name(), func(t *testing.T) {
			t.Parallel()

			data, err := cows.ReadFile("cows/" + entry.Name())
			require.NoError(t, err)
			require.NotEmpty(t, data, "cow file %q is empty", entry.Name())

			// At least one line's first non-space character must be the
			// bubble connector '\'. This catches a regression where a
			// $thoughts substitution was missed during extraction (a
			// leftover literal "$thoughts" would render visibly).
			var sawConnector bool

			for line := range strings.SplitSeq(string(data), "\n") {
				trimmed := strings.TrimLeft(line, " \t")
				if strings.HasPrefix(trimmed, "\\") {
					sawConnector = true
					break
				}
			}

			assert.Truef(t, sawConnector,
				"cow %q has no line whose first non-space char is '\\'; $thoughts substitution may have been missed",
				entry.Name(),
			)

			assert.NotContainsf(t, string(data), "$thoughts",
				"cow %q still contains literal $thoughts placeholder", entry.Name())
			assert.NotContainsf(t, string(data), "$eyes",
				"cow %q still contains literal $eyes placeholder", entry.Name())
			assert.NotContainsf(t, string(data), "$tongue",
				"cow %q still contains literal $tongue placeholder", entry.Name())
		})
	}
}
