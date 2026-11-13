package main

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// implicitAnd is the token of a run of whitespace between two terms.
var implicitAnd = queryToken{Kind: tokenImplicitAnd}

// filterToken returns an unquoted filter token.
func filterToken(keyword, args string) queryToken {
	return queryToken{Kind: tokenFilter, Keyword: keyword, Args: args}
}

// quotedToken returns a filter token whose value the query quotes.
func quotedToken(keyword, args string) queryToken {
	return queryToken{Kind: tokenFilter, Keyword: keyword, Args: args, Quoted: true}
}

// snapshotTokens returns tokens in the form of the lexer snapshot of DIM.
// Each entry holds the type of the token. A filter entry adds the keyword
// and the args, and a comment entry adds the content.
func snapshotTokens(tokens []queryToken) [][]string {
	out := make([][]string, 0, len(tokens))

	for _, token := range tokens {
		switch token.Kind {
		case tokenFilter:
			out = append(out, []string{string(token.Kind), token.Keyword, token.Args})
		case tokenComment:
			out = append(out, []string{string(token.Kind), token.Content})
		default:
			out = append(out, []string{string(token.Kind)})
		}
	}

	return out
}

func TestLexQuerySnapshot(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/query_parser_cases.json")
	require.NoError(t, err)

	var fixture struct {
		Source  string `json:"source"`
		License string `json:"license"`
		Cases   []struct {
			Query string     `json:"query"`
			Lexer [][]string `json:"lexer"`
		} `json:"cases"`
	}

	require.NoError(t, json.Unmarshal(raw, &fixture))
	assert.Contains(t, fixture.Source, "dd9fc5d01041a218fcebd536f4de11678057d8ae")
	assert.Contains(t, fixture.License, "Copyright (c) 2018 Destiny Item Manager")
	require.Len(t, fixture.Cases, 47)

	for i, tc := range fixture.Cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()

			tokens, err := lexQuery(tc.Query)
			require.NoError(t, err)
			assert.Equal(t, tc.Lexer, snapshotTokens(tokens), tc.Query)
		})
	}
}

func TestLexQueryQuoteRoundTrip(t *testing.T) {
	t.Parallel()

	// Each query is "name:" and the output of quoteFilterString of DIM for
	// the original string.
	tests := map[string]struct {
		query    string
		original string
	}{
		"plain": {
			query:    `name:foobar`,
			original: `foobar`,
		},
		"backslash without quotes": {
			query:    `name:Foo\bar`,
			original: `Foo\bar`,
		},
		"spaces": {
			query:    `name:"My cool loadout"`,
			original: `My cool loadout`,
		},
		"double quotes inside single quotes": {
			query:    `name:'My "cool" loadout'`,
			original: `My "cool" loadout`,
		},
		"escaped double quotes": {
			query:    `name:"My \"cool\" loadout's little brother"`,
			original: `My "cool" loadout's little brother`,
		},
		"escaped backslash": {
			query:    `name:"My \"cool\" load\\out's little brother"`,
			original: `My "cool" load\out's little brother`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tokens, err := lexQuery(tc.query)
			require.NoError(t, err)
			require.Len(t, tokens, 1)
			assert.Equal(t, tokenFilter, tokens[0].Kind)
			assert.Equal(t, "name", tokens[0].Keyword)
			assert.Equal(t, strings.ToLower(tc.original), tokens[0].Args)
		})
	}
}

func TestLexQueryErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query    string
		fragment string
		err      string
		want     []queryToken
		kind     QueryErrorKind
	}{
		"quote after a filter name never closes": {
			query:    `name:"abc`,
			kind:     queryUnclosedQuote,
			fragment: `name:"abc`,
			err:      `the quote in "name:\"abc" never closes`,
		},
		"quote without a filter name never closes": {
			query:    `"a b`,
			kind:     queryUnclosedQuote,
			fragment: `"a b`,
		},
		"backslash at the end is literal": {
			query:    `name:"a\`,
			kind:     queryUnclosedQuote,
			fragment: `name:"a\`,
		},
		"fragment drops a trailing byte order mark": {
			query:    "name:\"abc\ufeff",
			kind:     queryUnclosedQuote,
			fragment: `name:"abc`,
		},
		"quote after an unmatched paren never closes": {
			query:    `is:weapon ) "abc`,
			kind:     queryUnclosedQuote,
			fragment: `"abc`,
			err:      `the quote in "\"abc" never closes`,
		},
		"unknown escape": {
			query:    `name:"a\b"`,
			kind:     queryBadEscape,
			fragment: `b`,
			err: `the query puts a backslash before "b" inside quotes, ` +
				`and inside quotes a backslash escapes only a double quote, a single quote, or a backslash`,
		},
		"unknown escape of a multibyte character": {
			query:    `name:"a\é"`,
			kind:     queryBadEscape,
			fragment: `é`,
		},
		"filter name at the end": {
			query:    `is:`,
			kind:     queryNoValue,
			fragment: `is:`,
			err:      `the filter "is:" has no value`,
		},
		"filter name before a paren": {
			query:    `is:(`,
			kind:     queryNoValue,
			fragment: `is:`,
		},
		"filter name before a space": {
			query:    `is:weapon name: x`,
			kind:     queryNoValue,
			fragment: `name:`,
			err:      `the filter "name:" has no value`,
		},
		"quote inside a bare word": {
			query: `ab"c`,
			want:  []queryToken{filterToken(keywordFilter, `ab"c`)},
		},
		"escaped double quote inside single quotes": {
			query: `name:'a\"b'`,
			want:  []queryToken{quotedToken("name", `a"b`)},
		},
		"escaped single quote inside double quotes": {
			query: `name:"a\'b"`,
			want:  []queryToken{quotedToken("name", `a'b`)},
		},
		"digit before a colon": {
			query: `x2:`,
			want:  []queryToken{filterToken(keywordFilter, `x2:`)},
		},
		"hyphen before a colon": {
			query: `a-b:`,
			want:  []queryToken{filterToken(keywordFilter, `a-b:`)},
		},
		"underscore before a colon": {
			query: `is_x:`,
			want:  []queryToken{filterToken(keywordFilter, `is_x:`)},
		},
		"accented letter before a colon": {
			query: `é:`,
			want:  []queryToken{filterToken(keywordFilter, `é:`)},
		},
		"open paren inside a bare word": {
			query: `a(is:`,
			want:  []queryToken{filterToken(keywordFilter, `a(is:`)},
		},
		"not after whitespace": {
			query: `a not b`,
			want: []queryToken{
				filterToken(keywordFilter, "a"),
				implicitAnd,
				{Kind: tokenNot},
				filterToken(keywordFilter, "b"),
			},
		},
		"double negation": {
			query: `--x`,
			want: []queryToken{
				{Kind: tokenNot},
				{Kind: tokenNot},
				filterToken(keywordFilter, "x"),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tokens, err := lexQuery(tc.query)
			if tc.kind == 0 {
				require.NoError(t, err)
				assert.Equal(t, tc.want, tokens)

				return
			}

			var queryErr *QueryError

			require.ErrorAs(t, err, &queryErr)
			assert.Equal(t, tc.kind, queryErr.Kind)
			assert.Equal(t, tc.fragment, queryErr.Fragment)
			assert.Nil(t, tokens)

			if tc.err != "" {
				require.EqualError(t, err, tc.err)
			}
		})
	}
}

func TestLexQueryWhitespace(t *testing.T) {
	t.Parallel()

	split := []queryToken{filterToken("is", "weapon"), implicitAnd, filterToken("is", "armor")}

	// The cases name every character that \s matches in JavaScript, and
	// three characters outside that set.
	tests := map[string]struct {
		space string
		want  []queryToken
	}{
		"character tabulation":      {space: "\t", want: split},
		"line feed":                 {space: "\n", want: split},
		"line tabulation":           {space: "\v", want: split},
		"form feed":                 {space: "\f", want: split},
		"carriage return":           {space: "\r", want: split},
		"space":                     {space: " ", want: split},
		"no-break space":            {space: "\u00a0", want: split},
		"ogham space mark":          {space: "\u1680", want: split},
		"en quad":                   {space: "\u2000", want: split},
		"em quad":                   {space: "\u2001", want: split},
		"en space":                  {space: "\u2002", want: split},
		"em space":                  {space: "\u2003", want: split},
		"three-per-em space":        {space: "\u2004", want: split},
		"four-per-em space":         {space: "\u2005", want: split},
		"six-per-em space":          {space: "\u2006", want: split},
		"figure space":              {space: "\u2007", want: split},
		"punctuation space":         {space: "\u2008", want: split},
		"thin space":                {space: "\u2009", want: split},
		"hair space":                {space: "\u200a", want: split},
		"line separator":            {space: "\u2028", want: split},
		"paragraph separator":       {space: "\u2029", want: split},
		"narrow no-break space":     {space: "\u202f", want: split},
		"medium mathematical space": {space: "\u205f", want: split},
		"ideographic space":         {space: "\u3000", want: split},
		"byte order mark":           {space: "\ufeff", want: split},
		"next line": {
			space: "\u0085",
			want:  []queryToken{filterToken("is", "weapon\u0085is:armor")},
		},
		"mongolian vowel separator": {
			space: "\u180e",
			want:  []queryToken{filterToken("is", "weapon\u180eis:armor")},
		},
		"zero width space": {
			space: "\u200b",
			want:  []queryToken{filterToken("is", "weapon\u200bis:armor")},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tokens, err := lexQuery("is:weapon" + tc.space + "is:armor")
			require.NoError(t, err)
			assert.Equal(t, tc.want, tokens)
		})
	}
}

func TestLexQueryOperatorTrim(t *testing.T) {
	t.Parallel()

	// Each query puts U+FEFF inside the text that an operator token
	// matches. strings.TrimSpace keeps U+FEFF, and String.prototype.trim
	// removes it.
	tests := map[string]struct {
		query string
		want  []queryToken
	}{
		"parens": {
			query: "(\ufeffis:weapon\ufeff)",
			want: []queryToken{
				{Kind: tokenOpen},
				filterToken("is", "weapon"),
				{Kind: tokenClose},
			},
		},
		"or": {
			query: "is:weapon\ufeffor\ufeffis:armor",
			want: []queryToken{
				filterToken("is", "weapon"),
				{Kind: tokenOr},
				filterToken("is", "armor"),
			},
		},
		"and": {
			query: "is:weapon\ufeffand\ufeffis:armor",
			want: []queryToken{
				filterToken("is", "weapon"),
				{Kind: tokenAnd},
				filterToken("is", "armor"),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tokens, err := lexQuery(tc.query)
			require.NoError(t, err)
			assert.Equal(t, tc.want, tokens)
		})
	}
}

func TestLexQueryComments(t *testing.T) {
	t.Parallel()

	split := []queryToken{
		filterToken(keywordFilter, "/*"), implicitAnd,
		filterToken(keywordFilter, "a"), implicitAnd,
		filterToken(keywordFilter, "b"), implicitAnd,
		filterToken(keywordFilter, "*/"), implicitAnd,
		filterToken("is", "armor"),
	}

	tests := map[string]struct {
		query string
		want  []queryToken
	}{
		"line feed ends the comment": {
			query: "/* a\nb */ is:armor",
			want:  split,
		},
		"carriage return ends the comment": {
			query: "/* a\rb */ is:armor",
			want:  split,
		},
		"line separator ends the comment": {
			query: "/* a\u2028b */ is:armor",
			want:  split,
		},
		"paragraph separator ends the comment": {
			query: "/* a\u2029b */ is:armor",
			want:  split,
		},
		"trim keeps next line": {
			query: "/* x\u0085*/ is:armor",
			want: []queryToken{
				{Kind: tokenComment, Content: "x\u0085"},
				filterToken("is", "armor"),
			},
		},
		"trim removes no-break spaces": {
			query: "/*\u00a0x\u00a0*/ is:armor",
			want: []queryToken{
				{Kind: tokenComment, Content: "x"},
				filterToken("is", "armor"),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tokens, err := lexQuery(tc.query)
			require.NoError(t, err)
			assert.Equal(t, tc.want, tokens)
		})
	}
}

func TestLexQueryLowercase(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
		want  []queryToken
	}{
		"dotted capital I is not a filter name": {
			query: "\u0130S:weapon",
			want:  []queryToken{filterToken(keywordFilter, "i\u0307s:weapon")},
		},
		"capital letters": {
			query: "IS:Weapon",
			want:  []queryToken{filterToken("is", "weapon")},
		},
		"final sigma": {
			query: "\u0391\u03a3",
			want:  []queryToken{filterToken(keywordFilter, "\u03b1\u03c3")},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tokens, err := lexQuery(tc.query)
			require.NoError(t, err)
			assert.Equal(t, tc.want, tokens)
		})
	}
}

func TestLexQueryQuoted(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
		want  []queryToken
	}{
		"quoted word": {
			query: `"or"`,
			want:  []queryToken{quotedToken(keywordFilter, "or")},
		},
		"bare word": {
			query: `or`,
			want:  []queryToken{filterToken(keywordFilter, "or")},
		},
		"typographic double quotes": {
			query: "\u201ehard light\u201c",
			want:  []queryToken{quotedToken(keywordFilter, "hard light")},
		},
		"typographic single quotes": {
			query: "\u201ahard light\u2018",
			want:  []queryToken{quotedToken(keywordFilter, "hard light")},
		},
		"reversed single quote stays": {
			query: "name:\u201ba",
			want:  []queryToken{filterToken("name", "\u201ba")},
		},
		"reversed double quote stays": {
			query: "name:\u201fa",
			want:  []queryToken{filterToken("name", "\u201fa")},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tokens, err := lexQuery(tc.query)
			require.NoError(t, err)
			assert.Equal(t, tc.want, tokens)
		})
	}
}

func TestCheckQuery(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
		kind  QueryErrorKind
	}{
		"byte order mark only": {
			query: "\ufeff",
			kind:  queryNoTerm,
		},
		"filter": {
			query: "is:weapon",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := checkQuery(tc.query)
			if tc.kind == 0 {
				require.NoError(t, err)

				return
			}

			var queryErr *QueryError

			require.ErrorAs(t, err, &queryErr)
			assert.Equal(t, tc.kind, queryErr.Kind)
			require.EqualError(t, err, "the query has no search term")
		})
	}
}
