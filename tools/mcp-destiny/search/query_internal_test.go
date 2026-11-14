package search

import (
	"encoding/json"
	"errors"
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

// withoutSource returns tokens without their Text and Start, so a test can
// compare the other fields.
func withoutSource(tokens []queryToken) []queryToken {
	out := make([]queryToken, 0, len(tokens))

	for _, token := range tokens {
		token.Text = ""
		token.Start = 0
		out = append(out, token)
	}

	return out
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

// lexQuery returns the tokens of query, or the [*QueryError] of the first
// token that does not lex.
func lexQuery(query string) ([]queryToken, error) {
	lexer := newQueryLexer(query)

	var tokens []queryToken

	for {
		token, ok, err := lexer.next()
		if err != nil {
			return nil, err
		}

		if !ok {
			return tokens, nil
		}

		tokens = append(tokens, token)
	}
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
			query:    "name:\"a\\\u00e9\"",
			kind:     queryBadEscape,
			fragment: "\u00e9",
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
			query: "\u00e9:",
			want:  []queryToken{filterToken(keywordFilter, "\u00e9:")},
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
				assert.Equal(t, tc.want, withoutSource(tokens))

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
			assert.Equal(t, tc.want, withoutSource(tokens))
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
			assert.Equal(t, tc.want, withoutSource(tokens))
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
			assert.Equal(t, tc.want, withoutSource(tokens))
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
			assert.Equal(t, tc.want, withoutSource(tokens))
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
			assert.Equal(t, tc.want, withoutSource(tokens))
		})
	}
}

func TestLexQuerySource(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query      string
		wantTexts  []string
		wantStarts []int
	}{
		"each kind of token": {
			query: `is:weapon or (name:"Hard Light" -x) /* c */ not:y`,
			wantTexts: []string{
				"is:weapon", "or", "(", `name:"hard light"`, "", "-", "x", ")", "", "/* c */", "not:y",
			},
			wantStarts: []int{0, 9, 13, 14, 31, 32, 33, 34, 35, 36, 44},
		},
		"whitespace inside parens": {
			query:      `( a )`,
			wantTexts:  []string{"(", "a", ")"},
			wantStarts: []int{0, 2, 3},
		},
		"offsets of the normalized query": {
			query:      "X \u201cA B\u201d",
			wantTexts:  []string{"x", "", `"a b"`},
			wantStarts: []int{0, 1, 2},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tokens, err := lexQuery(tc.query)
			require.NoError(t, err)

			texts := make([]string, 0, len(tokens))
			starts := make([]int, 0, len(tokens))

			for _, token := range tokens {
				texts = append(texts, token.Text)
				starts = append(starts, token.Start)
			}

			assert.Equal(t, tc.wantTexts, texts)
			assert.Equal(t, tc.wantStarts, starts)
		})
	}
}

func TestLexQueryEndsAfterError(t *testing.T) {
	t.Parallel()

	lexer := newQueryLexer(`a "b c`)

	token, ok, err := lexer.next()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "a", token.Text)

	token, ok, err = lexer.next()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, tokenImplicitAnd, token.Kind)

	_, ok, err = lexer.next()
	require.EqualError(t, err, `the quote in "\"b c" never closes`)
	assert.False(t, ok)

	for range 2 {
		_, ok, err = lexer.next()
		require.NoError(t, err)
		assert.False(t, ok)
	}
}

// snapshotNode returns node in the form of the AST snapshot of DIM without
// the positions. It leaves out Quoted and Text, which DIM does not have.
func snapshotNode(node *QueryNode) map[string]any {
	out := map[string]any{"op": string(node.Op)}
	if node.Comment != "" {
		out["comment"] = node.Comment
	}

	switch node.Op {
	case OpFilter:
		out["type"] = node.Type
		out["args"] = node.Args

	case OpNot:
		out["operand"] = snapshotNode(node.Operand)
	case OpAnd, OpOr:
		operands := make([]any, 0, len(node.Operands))
		for _, operand := range node.Operands {
			operands = append(operands, snapshotNode(operand))
		}

		out["operands"] = operands

	default:
	}

	return out
}

// filterSnapshot returns the snapshot of a filter node.
func filterSnapshot(typ, args string) map[string]any {
	return map[string]any{"op": "filter", "type": typ, "args": args}
}

// opSnapshot returns the snapshot of an and node or an or node.
func opSnapshot(op string, operands ...map[string]any) map[string]any {
	list := make([]any, 0, len(operands))
	for _, operand := range operands {
		list = append(list, operand)
	}

	return map[string]any{"op": op, "operands": list}
}

// withComment returns snapshot with the comment.
func withComment(snapshot map[string]any, comment string) map[string]any {
	snapshot["comment"] = comment

	return snapshot
}

func TestParseQuerySnapshot(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/query_parser_cases.json")
	require.NoError(t, err)

	var fixture struct {
		Cases []struct {
			AST   map[string]any `json:"ast"`
			Query string         `json:"query"`
		} `json:"cases"`
	}

	require.NoError(t, json.Unmarshal(raw, &fixture))
	require.Len(t, fixture.Cases, 47)

	for i, tc := range fixture.Cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()

			node, err := ParseQuery(tc.Query)

			// DIM parses a query without a token to a noop node.
			if tc.AST["op"] == string(opNoop) {
				var queryErr *QueryError

				require.ErrorAs(t, err, &queryErr)
				assert.Equal(t, queryNoTerm, queryErr.Kind)
				assert.Nil(t, node)

				return
			}

			require.NoError(t, err, tc.Query)
			assert.Equal(t, tc.AST, snapshotNode(node), tc.Query)
		})
	}
}

func TestParseQueryEquivalent(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query      string
		equivalent string
	}{
		"implicit and binds looser than or": {
			query:      `is:blue is:weapon or is:armor not:maxpower`,
			equivalent: `is:blue and (is:weapon or is:armor) and -is:maxpower`,
		},
		"not before a bare word": {
			query:      `not forgotten`,
			equivalent: `-"forgotten"`,
		},
		"bare words": {
			query:      `cluster tracking`,
			equivalent: `"cluster" and "tracking"`,
		},
		"and binds tighter than or": {
			query:      `is:weapon and is:sniperrifle or not is:armor and modslot:arrival`,
			equivalent: `(is:weapon and is:sniperrifle) or (-is:armor and modslot:arrival)`,
		},
		"or between implicit ands": {
			query:      `is:weapon is:sniperrifle or not is:armor modslot:arrival`,
			equivalent: `is:weapon and (is:sniperrifle or -is:armor) and modslot:arrival`,
		},
		"and after or after an implicit and": {
			query:      `is:weapon is:sniperrifle or is:armor and modslot:arrival`,
			equivalent: `is:weapon and (is:sniperrifle or (is:armor and modslot:arrival))`,
		},
		"or of quoted values": {
			query:      `is:rocketlauncher perk:"cluster" or perk:"tracking module"`,
			equivalent: `is:rocketlauncher (perk:"cluster" or perk:"tracking module")`,
		},
		"group that never closes": {
			query:      `is:blue (is:rocketlauncher`,
			equivalent: `is:blue is:rocketlauncher`,
		},
		"whitespace at the ends": {
			query:      `  is:blue  `,
			equivalent: `is:blue`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			node, err := ParseQuery(tc.query)
			require.NoError(t, err)

			equivalent, err := ParseQuery(tc.equivalent)
			require.NoError(t, err)

			assert.Equal(t, snapshotNode(equivalent), snapshotNode(node))
		})
	}
}

func TestParseQueryComments(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want  map[string]any
		query string
	}{
		"comments before groups": {
			query: `/* My cool search */ (/* armor */ is:armor and is:blue) or ` +
				`(/*weapons*/ is:weapon and perkname:"Kill Clip")`,
			want: withComment(opSnapshot("or",
				opSnapshot("and", filterSnapshot("is", "armor"), filterSnapshot("is", "blue")),
				withComment(opSnapshot("and",
					filterSnapshot("is", "weapon"), filterSnapshot("perkname", "kill clip"),
				), "weapons"),
			), "my cool search"),
		},
		"spread drops the comment of the right side": {
			query: `x /* c */(b c)`,
			want: opSnapshot("and",
				filterSnapshot(keywordFilter, "x"),
				filterSnapshot(keywordFilter, "b"),
				filterSnapshot(keywordFilter, "c"),
			),
		},
		"comment of the left side moves up": {
			query: `/* a */ x /* b */ y`,
			want: withComment(opSnapshot("and",
				filterSnapshot(keywordFilter, "x"),
				withComment(filterSnapshot(keywordFilter, "y"), "b"),
			), "a"),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			node, err := ParseQuery(tc.query)
			require.NoError(t, err)
			assert.Equal(t, tc.want, snapshotNode(node))
		})
	}
}

func TestParseQueryErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query    string
		fragment string
		err      string
		kind     QueryErrorKind
		// dropped is the kind of the lexer error that a queryDroppedError
		// wraps.
		dropped QueryErrorKind
		runs    bool
	}{
		"end after a negation": {
			query:    `is:weapon -`,
			kind:     queryMissingTerm,
			fragment: `-`,
			err:      `the query ends after "-", which needs a search term after it`,
		},
		"end after an open paren": {
			query:    `is:weapon (`,
			kind:     queryMissingTerm,
			fragment: `(`,
		},
		"end after not": {
			query:    `not (`,
			kind:     queryMissingTerm,
			fragment: `(`,
		},
		"comment without a term": {
			query:    `/* note */`,
			kind:     queryMissingTerm,
			fragment: `/* note */`,
		},
		"quoted strings without a space": {
			query:    `"a""b"`,
			kind:     queryMissingOperator,
			fragment: `"b"`,
			err:      `the query has no space or operator before "\"b\""`,
		},
		"open paren right after a filter": {
			query:    `is:weapon(`,
			kind:     queryMissingOperator,
			fragment: `(`,
			err:      `the query has no space or operator before "("`,
		},
		"negation right after a quoted string": {
			query:    `"a"-b`,
			kind:     queryMissingOperator,
			fragment: `-`,
		},
		"empty group": {
			query:    `()`,
			kind:     queryUnexpectedToken,
			fragment: `)`,
			err:      `the query has ")" where a search term belongs`,
		},
		"close paren after or": {
			query:    `is:weapon or )`,
			kind:     queryUnexpectedToken,
			fragment: `)`,
		},
		"or after a byte order mark": {
			query:    "\ufeff or x",
			kind:     queryUnexpectedToken,
			fragment: `or`,
		},
		"quote that never closes": {
			query:    `name:"abc`,
			kind:     queryUnclosedQuote,
			fragment: `name:"abc`,
		},
		"lexer error after a group that is the whole query": {
			query:    `(is:arc is:void)"abc`,
			kind:     queryUnclosedQuote,
			fragment: `"abc`,
			err:      `the quote in "\"abc" never closes`,
		},
		"lexer error after a group under an and": {
			query:    `is:weapon is:blue (is:arc is:void)"abc`,
			kind:     queryUnclosedQuote,
			fragment: `"abc`,
		},
		"lexer error after a group of another operator": {
			query:    `is:weapon (is:arc or is:void)"abc`,
			kind:     queryUnclosedQuote,
			fragment: `"abc`,
		},
		"lexer error after a negated group": {
			query:    `is:weapon -(is:arc is:void)"abc`,
			kind:     queryUnclosedQuote,
			fragment: `"abc`,
		},
		"filter name without a value": {
			query:    `is:weapon name: x`,
			kind:     queryNoValue,
			fragment: `name:`,
		},
		"byte order mark only": {
			query: "\ufeff",
			kind:  queryNoTerm,
			err:   "the query has no search term",
		},
		"close paren after a filter": {
			query:    `is:weapon )`,
			kind:     queryStrayParen,
			fragment: `)`,
			runs:     true,
		},
		"terms after a close paren": {
			query:    `is:weapon ) is:armor`,
			kind:     queryStrayParen,
			fragment: `) is:armor`,
			err:      `the query has a ")" that closes no "(", and the web client ignores ") is:armor"`,
			runs:     true,
		},
		"quote that never closes after a close paren": {
			query:    `is:weapon ) "abc`,
			kind:     queryStrayParen,
			fragment: `) "abc`,
			runs:     true,
		},
		"not right before a paren": {
			query:    `not(is:weapon)`,
			kind:     queryStrayParen,
			fragment: `)`,
			runs:     true,
		},
		"lexer error after a group that spreads": {
			query:    `is:weapon (is:arc is:void)"abc`,
			kind:     queryDroppedError,
			dropped:  queryUnclosedQuote,
			fragment: `"abc`,
			err:      `the quote in "\"abc" never closes. The web client drops this error and ignores "\"abc"`,
			runs:     true,
		},
		"filter name without a value after an or group that spreads": {
			query:    `is:weapon or (is:arc or is:void)is:`,
			kind:     queryDroppedError,
			dropped:  queryNoValue,
			fragment: `is:`,
			runs:     true,
		},
		"lexer error after a group with a comment": {
			query:    `x /* c */(b c)"d`,
			kind:     queryDroppedError,
			dropped:  queryUnclosedQuote,
			fragment: `"d`,
			runs:     true,
		},
		"unknown escape after a group that spreads": {
			query:    `x (a b)"q\z"`,
			kind:     queryDroppedError,
			dropped:  queryBadEscape,
			fragment: `"q\z"`,
			runs:     true,
		},
		"or at the end": {
			query:    `is:weapon or`,
			kind:     queryBareOperator,
			fragment: `or`,
			err: `the web client reads the unquoted word "or" as a search for that word, ` +
				`not as an operator. Quote the word to search for it`,
			runs: true,
		},
		"and at the end": {
			query:    `cluster and`,
			kind:     queryBareOperator,
			fragment: `and`,
			runs:     true,
		},
		"not alone": {
			query:    `not`,
			kind:     queryBareOperator,
			fragment: `not`,
			runs:     true,
		},
		"negated or": {
			query:    `-or`,
			kind:     queryBareOperator,
			fragment: `or`,
			runs:     true,
		},
		"or at the start": {
			query:    `OR is:weapon`,
			kind:     queryBareOperator,
			fragment: `or`,
			runs:     true,
		},
		"or in a group": {
			query:    `(or)`,
			kind:     queryBareOperator,
			fragment: `or`,
			runs:     true,
		},
		"node error comes before a bare operator": {
			query:    `or -`,
			kind:     queryMissingTerm,
			fragment: `-`,
		},
		"dropped error comes before a bare operator": {
			query:    `or (a b)"c`,
			kind:     queryDroppedError,
			dropped:  queryUnclosedQuote,
			fragment: `"c`,
			runs:     true,
		},
		"close paren comes before a bare operator": {
			query:    `or ) x`,
			kind:     queryStrayParen,
			fragment: `) x`,
			runs:     true,
		},
		"error of a node comes before the errors of its operands": {
			query:    `a - ) "b"c`,
			kind:     queryMissingOperator,
			fragment: `c`,
		},
		"errors of sibling nodes come in query order": {
			query:    `a - ) b -`,
			kind:     queryUnexpectedToken,
			fragment: `)`,
		},
		"bare operators come in query order": {
			query:    `or and`,
			kind:     queryBareOperator,
			fragment: `or`,
			runs:     true,
		},
		"lexer error right after an empty group": {
			query:    `()"abc`,
			kind:     queryUnclosedQuote,
			fragment: `"abc`,
		},
		"unexpected token after a negation": {
			query:    `-)x`,
			kind:     queryUnexpectedToken,
			fragment: `)`,
		},
		"unexpected token after a comment": {
			query:    `/* c */)x`,
			kind:     queryUnexpectedToken,
			fragment: `)`,
		},
		"query ends after an operator": {
			query:    "is:weapon or\ufeff",
			kind:     queryMissingTerm,
			fragment: `or`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			node, err := ParseQuery(tc.query)
			assert.Nil(t, node)

			var queryErr *QueryError

			require.ErrorAs(t, err, &queryErr)
			assert.Equal(t, tc.kind, queryErr.Kind)
			assert.Equal(t, tc.fragment, queryErr.Fragment)
			assert.Equal(t, tc.runs, queryErr.WebClientRuns())

			if tc.err != "" {
				require.EqualError(t, err, tc.err)
			}

			if tc.dropped == 0 {
				assert.NoError(t, queryErr.Err)

				return
			}

			var lexErr *QueryError

			require.ErrorAs(t, errors.Unwrap(err), &lexErr)
			assert.Equal(t, tc.dropped, lexErr.Kind)
			assert.False(t, lexErr.WebClientRuns())
		})
	}
}

func TestParseQueryAccepts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
	}{
		"group that never closes":         {query: `is:blue (is:rocketlauncher`},
		"not before a bare word":          {query: `not forgotten`},
		"quoted operator word":            {query: `"or"`},
		"operator word as a filter value": {query: `name:or`},
		"operator word after keyword":     {query: `keyword:or`},
		"negation with a space":           {query: `is:weapon - is:armor`},
		"or between filters":              {query: `is:weapon or is:armor`},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			node, err := ParseQuery(tc.query)
			require.NoError(t, err)
			assert.NotNil(t, node)
		})
	}
}

func TestParseQueryFilterSource(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query  string
		typ    string
		text   string
		quoted bool
	}{
		"quoted operator word": {
			query:  `"or"`,
			typ:    keywordFilter,
			text:   `"or"`,
			quoted: true,
		},
		"operator word as a filter value": {
			query: `name:or`,
			typ:   "name",
			text:  `name:or`,
		},
		"quoted filter value": {
			query:  `name:"A b"`,
			typ:    "name",
			text:   `name:"a b"`,
			quoted: true,
		},
		"quoted value of a not filter": {
			query:  `not:"x"`,
			typ:    "is",
			text:   `not:"x"`,
			quoted: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			node, err := ParseQuery(tc.query)
			require.NoError(t, err)

			// A not filter reads as the negation of an is filter.
			if node.Op == OpNot {
				node = node.Operand
			}

			require.Equal(t, OpFilter, node.Op)
			assert.Equal(t, tc.typ, node.Type)
			assert.Equal(t, tc.text, node.Text)
			assert.Equal(t, tc.quoted, node.Quoted)
		})
	}
}
