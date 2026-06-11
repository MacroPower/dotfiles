package content_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/content"
)

func TestGrep(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		text        string
		pattern     string
		opts        content.GrepOptions
		want        string
		wantMatched bool
		err         error
	}{
		"single match": {
			text:        "foo\nbar\nbaz",
			pattern:     "bar",
			want:        "2:bar",
			wantMatched: true,
		},
		"no match": {
			text:        "foo\nbar\nbaz",
			pattern:     "qux",
			want:        "",
			wantMatched: false,
		},
		"ignore case": {
			text:        "foo\nBAR\nbaz",
			pattern:     "bar",
			opts:        content.GrepOptions{IgnoreCase: true},
			want:        "2:BAR",
			wantMatched: true,
		},
		"invert with group separator": {
			text:        "foo\nbar\nbaz",
			pattern:     "bar",
			opts:        content.GrepOptions{Invert: true},
			want:        "1:foo\n--\n3:baz",
			wantMatched: true,
		},
		"context lines": {
			text:        "alpha\nbeta\ngamma\ndelta\nepsilon",
			pattern:     "gamma",
			opts:        content.GrepOptions{Context: 1},
			want:        "2-beta\n3:gamma\n4-delta",
			wantMatched: true,
		},
		"non-adjacent groups separated": {
			text:        "alpha\nbeta\ngamma\ndelta\nepsilon",
			pattern:     "alpha|epsilon",
			want:        "1:alpha\n--\n5:epsilon",
			wantMatched: true,
		},
		"context groups separated": {
			text:        "a\nb\nMATCH1\nd\ne\nf\ng\nMATCH2\ni",
			pattern:     "MATCH1|MATCH2",
			opts:        content.GrepOptions{Context: 1},
			want:        "2-b\n3:MATCH1\n4-d\n--\n7-g\n8:MATCH2\n9-i",
			wantMatched: true,
		},
		"preserved line numbers": {
			text:        "a\nb\nc\nd\ne\nf\ng\nh\ni\nj",
			pattern:     "h",
			want:        "8:h",
			wantMatched: true,
		},
		"invalid regex": {
			text:    "foo\nbar",
			pattern: "(",
			err:     content.ErrBadPattern,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, matched, err := content.Grep(tt.text, tt.pattern, tt.opts)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantMatched, matched)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCompilePattern(t *testing.T) {
	t.Parallel()

	re, err := content.CompilePattern("foo", true)
	require.NoError(t, err)
	assert.True(t, re.MatchString("FOO"), "ignore-case pattern must match upper case")

	_, err = content.CompilePattern("(", false)
	require.ErrorIs(t, err, content.ErrBadPattern)
}

func TestPaginate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		text          string
		startIndex    int
		maxLength     int
		want          string
		wantTruncated bool
	}{
		"no truncation needed": {
			text:      "short",
			maxLength: 100,
			want:      "short",
		},
		"truncated with hint": {
			text:          "abcdefghij",
			maxLength:     5,
			want:          "abcde",
			wantTruncated: true,
		},
		"start index offset": {
			text:       "abcdefghij",
			startIndex: 3,
			maxLength:  100,
			want:       "defghij",
		},
		"start index beyond content": {
			text:       "short",
			startIndex: 100,
			maxLength:  100,
			want:       "<content empty",
		},
		"truncation hint names next index": {
			text:          "abcdefghij",
			maxLength:     5,
			want:          "start_index=5",
			wantTruncated: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, truncated := content.Paginate(tt.text, tt.startIndex, tt.maxLength)
			assert.Contains(t, result, tt.want)
			assert.Equal(t, tt.wantTruncated, truncated)
		})
	}
}
