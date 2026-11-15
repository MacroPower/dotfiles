package wireinput_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/wireinput"
)

// The sent texts in this file spell each escape with a doubled
// backslash in an interpreted string. The ran texts spell each
// character with an eight-digit escape. Claude Code decodes neither
// spelling in a tool input, so an agent can edit this file with Write
// and Edit.

func TestRestore(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		sent string
		ran  string
		// want is the restored text. An empty want stands for sent.
		want        string
		wantEscapes []wireinput.Escape
		err         error
	}{
		"escape in a rune literal": {
			sent:        "var r = '\\u2018'\n",
			ran:         "var r = '\U00002018'\n",
			wantEscapes: []wireinput.Escape{{Text: "\\u2018", Rune: 0x2018}},
		},
		"escapes beside the same character typed as itself": {
			sent: "a\\u00e9b\U000000e9c\\uFEFFd",
			ran:  "a\U000000e9b\U000000e9c\U0000feffd",
			wantEscapes: []wireinput.Escape{
				{Text: "\\u00e9", Rune: 0xe9},
				{Text: "\\uFEFF", Rune: 0xfeff},
			},
		},
		"surrogate pair": {
			sent:        "x\\ud83d\\ude00y",
			ran:         "x\U0001f600y",
			wantEscapes: []wireinput.Escape{{Text: "\\ud83d\\ude00", Rune: 0x1f600}},
		},
		"escape of an ASCII letter": {
			sent:        "k\\u0041: 1",
			ran:         "kA: 1",
			wantEscapes: []wireinput.Escape{{Text: "\\u0041", Rune: 'A'}},
		},
		"escape of a backslash": {
			sent:        "a\\u005cb",
			ran:         "a\\b",
			wantEscapes: []wireinput.Escape{{Text: "\\u005c", Rune: '\\'}},
		},
		"escape after two backslashes": {
			sent:        "\\\\\\u2018",
			ran:         "\\\\\U00002018",
			wantEscapes: []wireinput.Escape{{Text: "\\u2018", Rune: 0x2018}},
		},
		"escape after one backslash stays text": {
			sent: "\\\\u2018",
			ran:  "\\\\u2018",
		},
		"escape of a control character stays text": {
			sent: "\\u001b[31m and \\u0085",
			ran:  "\\u001b[31m and \\u0085",
		},
		"half of a surrogate pair stays text": {
			sent: "\\ud800x \\ude00y",
			ran:  "\\ud800x \\ude00y",
		},
		"escapes in a string that Claude Code skipped stay text": {
			sent: "C:\\dir \\u2018 \\u005c",
			ran:  "C:\\dir \\u2018 \\u005c",
		},
		"stripped whitespace stays stripped": {
			sent: "a  \n\tb\t\r\nc ",
			ran:  "a\n\tb\r\nc",
			want: "a\n\tb\r\nc",
		},
		"escape on a line that lost its whitespace": {
			sent:        "x := '\\u2018'  \ny",
			ran:         "x := '\U00002018'\ny",
			want:        "x := '\\u2018'\ny",
			wantEscapes: []wireinput.Escape{{Text: "\\u2018", Rune: 0x2018}},
		},
		"kept escape on a line that lost its whitespace": {
			sent: "C:\\dir \\u2018 \nb",
			ran:  "C:\\dir \\u2018\nb",
			want: "C:\\dir \\u2018\nb",
		},
		"whitespace escape that the strip removed": {
			sent:        "a \\u2028 \nb",
			ran:         "a\nb",
			want:        "a \\u2028\nb",
			wantEscapes: []wireinput.Escape{{Text: "\\u2028", Rune: 0x2028}},
		},
		"whitespace escapes at the end of the text": {
			sent: "a\\u3000\\ufeff",
			ran:  "a",
			wantEscapes: []wireinput.Escape{
				{Text: "\\u3000", Rune: 0x3000},
				{Text: "\\ufeff", Rune: 0xfeff},
			},
		},
		"whitespace escape inside a line": {
			sent:        "a \\u00a0b",
			ran:         "a \U000000a0b",
			wantEscapes: []wireinput.Escape{{Text: "\\u00a0", Rune: 0xa0}},
		},
		"straight quotes that the Edit tool curled": {
			sent:        "say \"it's\" '\\u2014'",
			ran:         "say \U0000201cit\U00002019s\U0000201d \U00002018\U00002014\U00002019",
			want:        "say \U0000201cit\U00002019s\U0000201d \U00002018\\u2014\U00002019",
			wantEscapes: []wireinput.Escape{{Text: "\\u2014", Rune: 0x2014}},
		},
		"same text": {
			sent: "plain\n",
			ran:  "plain\n",
		},
		"empty texts": {},
		"other character": {
			sent: "abc",
			ran:  "abd",
			err:  wireinput.ErrMisaligned,
		},
		"straight quote against a curly quote of the other kind": {
			sent: "'a'",
			ran:  "\U0000201ca\U0000201d",
			err:  wireinput.ErrMisaligned,
		},
		"curly quote against a straight quote": {
			sent: "\U00002018a\U00002019",
			ran:  "'a'",
			err:  wireinput.ErrMisaligned,
		},
		"ran text goes on": {
			sent: "a\\u2018",
			ran:  "a\U00002018b",
			err:  wireinput.ErrMisaligned,
		},
		"ran text stops short": {
			sent: "a\\u2018b",
			ran:  "a\U00002018",
			err:  wireinput.ErrMisaligned,
		},
		"whitespace missing inside a line": {
			sent: "a  b",
			ran:  "a b",
			err:  wireinput.ErrMisaligned,
		},
		"character after missing whitespace": {
			sent: "a \\u2018",
			ran:  "a\U00002018",
			err:  wireinput.ErrMisaligned,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := wireinput.Restore(tc.sent, tc.ran)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				return
			}

			require.NoError(t, err)

			want := tc.want
			if want == "" {
				want = tc.sent
			}

			assert.Equal(t, want, got.Text)
			assert.Equal(t, tc.wantEscapes, got.Escapes)
		})
	}
}
