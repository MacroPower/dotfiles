package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The token kinds of the search lexer. Each value is the type name that
// the lexer of Destiny Item Manager (DIM) gives the token.
const (
	tokenOpen        tokenKind = "("
	tokenClose       tokenKind = ")"
	tokenNot         tokenKind = "not"
	tokenOr          tokenKind = "or"
	tokenAnd         tokenKind = "and"
	tokenImplicitAnd tokenKind = "implicit_and"
	tokenFilter      tokenKind = "filter"
	tokenComment     tokenKind = "comment"
)

// The rules that a [QueryError] reports: the lexer rules of the web
// client, and the rule that a query has a search term.
const (
	queryUnclosedQuote QueryErrorKind = iota + 1
	queryBadEscape
	queryNoValue
	queryNoTerm
)

const (
	// jsSpace is the body of a character class that holds the characters
	// that \s matches in a JavaScript regular expression.
	// String.prototype.trim removes the same characters. The set differs
	// from the \s of Go, which matches ASCII only, and from
	// [unicode.IsSpace], which matches U+0085 and not U+FEFF.
	jsSpace = `\t-\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

	// keywordFilter is the keyword of a filter token that holds a quoted
	// string or a bare word. The web client matches such a token against
	// the text of an item.
	keywordFilter = "keyword"
)

var (
	// jsSpaceChar matches one character of jsSpace.
	jsSpaceChar = regexp.MustCompile(`^[` + jsSpace + `]$`)

	// The patterns of the lexer of DIM, in its order. The lexer runs each
	// pattern at its position in the query. DIM writes the body of a
	// comment as a dot, which in JavaScript matches every character except
	// the line terminators \n, \r, U+2028, and U+2029. The comment pattern
	// spells out that class, because the dot of Go matches all of them
	// except \n.
	queryParens     = jsPattern(`(\(\s*|\s*\))`)
	queryNegation   = jsPattern(`-\s*`)
	queryBoolean    = jsPattern(`(not|\s+or|\s+and)\s+`)
	queryComment    = jsPattern(`\/\*([^\n\r\x{2028}\x{2029}]*?)\*\/\s*`)
	queryFilterName = jsPattern(`[a-z]+:`)
	queryFilterArgs = jsPattern(`[^\s()]+`)
	queryBareWord   = jsPattern(`[^\s)]+`)
	querySpace      = jsPattern(`\s+`)
)

// tokenKind is the type of a [queryToken].
type tokenKind string

// queryToken is one token of a search query.
type queryToken struct {
	// Kind is the type of the token.
	Kind tokenKind
	// Keyword is the filter name of a filter token, such as "is" in
	// is:weapon. A quoted string and a bare word have the keyword
	// "keyword".
	Keyword string
	// Args is the value of a filter token, without quotes and escapes.
	Args string
	// Content is the text of a comment token without its delimiters and
	// the spaces at its ends.
	Content string
	// Quoted is true for a filter token whose value the query quotes.
	Quoted bool
}

// queryLexer reads a search query with the rules of the lexer of DIM,
// which the web client uses. It returns one token per call of next, so a
// parser can stop at any token.
type queryLexer struct {
	query string
	pos   int
}

// QueryErrorKind names the rule that a query breaks.
type QueryErrorKind int

// QueryError reports a query that the search lexer of the web client
// rejects, or a query without a search term.
type QueryError struct {
	// Fragment is the failing part of the query. It comes from the
	// lowercase query with ASCII quotes that the lexer reads.
	Fragment string
	// Kind names the rule that the query breaks.
	Kind QueryErrorKind
}

// Error describes the failing part of the query.
func (e *QueryError) Error() string {
	switch e.Kind {
	case queryUnclosedQuote:
		return fmt.Sprintf("the quote in %q never closes", e.Fragment)
	case queryBadEscape:
		return fmt.Sprintf("the query puts a backslash before %q inside quotes, "+
			"and inside quotes a backslash escapes only a double quote, a single quote, or a backslash", e.Fragment)

	case queryNoValue:
		return fmt.Sprintf("the filter %q has no value", e.Fragment)
	default:
		return "the query has no search term"
	}
}

// checkQuery returns a [*QueryError] for a query that the search lexer of
// the web client rejects or that yields no token. The web client skips an
// autotag rule with such a query. checkQuery does not check filter names
// or filter values.
func checkQuery(query string) error {
	tokens, err := lexQuery(query)
	if err != nil {
		return err
	}

	if len(tokens) == 0 {
		return &QueryError{Kind: queryNoTerm}
	}

	return nil
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

// newQueryLexer returns a lexer that reads query after [normalizeQuery].
func newQueryLexer(query string) *queryLexer {
	return &queryLexer{query: normalizeQuery(query)}
}

// next returns the next token of the query. Its bool is false at the end
// of the query. It returns a [*QueryError] for a quote that never closes
// and for a filter name without a value. Inside quotes, a backslash
// escapes only a quote or a backslash, and next returns a [*QueryError]
// for any other escape.
//
// A run of whitespace gives an implicit_and token, except a run at the
// start or at the end of the query.
func (l *queryLexer) next() (queryToken, bool, error) {
	for l.pos < len(l.query) {
		start := l.pos

		if m, ok := l.match(queryParens); ok {
			return queryToken{Kind: tokenKind(jsTrim(m))}, true, nil
		}

		if isQuote(l.query[start]) {
			args, err := l.consumeString(start)
			if err != nil {
				return queryToken{}, false, err
			}

			return queryToken{Kind: tokenFilter, Keyword: keywordFilter, Args: args, Quoted: true}, true, nil
		}

		if _, ok := l.match(queryNegation); ok {
			return queryToken{Kind: tokenNot}, true, nil
		}

		if m, ok := l.match(queryBoolean); ok {
			return queryToken{Kind: tokenKind(jsTrim(m))}, true, nil
		}

		if m, ok := l.match(queryComment); ok {
			return queryToken{Kind: tokenComment, Content: jsTrim(m)}, true, nil
		}

		if m, ok := l.match(queryFilterName); ok {
			return l.filter(start, m)
		}

		if m, ok := l.match(queryBareWord); ok {
			return queryToken{Kind: tokenFilter, Keyword: keywordFilter, Args: m}, true, nil
		}

		// The bare-word pattern takes every character outside jsSpace
		// except ")", and the parens pattern takes ")", so the query
		// continues with whitespace here.
		l.match(querySpace)

		if start > 0 && l.pos < len(l.query) {
			return queryToken{Kind: tokenImplicitAnd}, true, nil
		}
	}

	return queryToken{}, false, nil
}

// filter returns the filter token whose name the lexer consumed. start is
// the position of the name, and name is the name with its colon.
func (l *queryLexer) filter(start int, name string) (queryToken, bool, error) {
	token := queryToken{Kind: tokenFilter, Keyword: strings.TrimSuffix(name, ":")}

	if l.pos < len(l.query) && isQuote(l.query[l.pos]) {
		args, err := l.consumeString(start)
		if err != nil {
			return queryToken{}, false, err
		}

		token.Args = args
		token.Quoted = true

		return token, true, nil
	}

	args, ok := l.match(queryFilterArgs)
	if !ok {
		return queryToken{}, false, &QueryError{Kind: queryNoValue, Fragment: name}
	}

	token.Args = args

	return token, true, nil
}

// consumeString consumes the quoted string at the position of the lexer
// and returns its text. Inside the quotes, a backslash escapes a double
// quote, a single quote, or a backslash. A backslash at the end of the
// query stands for itself. The fragment of a quote that never closes runs
// from the position from to the end of the query.
func (l *queryLexer) consumeString(from int) (string, error) {
	quote := l.query[l.pos]
	l.pos++

	var b strings.Builder

	for l.pos < len(l.query) {
		c := l.query[l.pos]
		l.pos++

		switch {
		case c == quote:
			return b.String(), nil

		case c == '\\' && l.pos < len(l.query):
			escaped := l.query[l.pos]
			if !isQuote(escaped) && escaped != '\\' {
				r, _ := utf8.DecodeRuneInString(l.query[l.pos:])

				return "", &QueryError{Kind: queryBadEscape, Fragment: string(r)}
			}

			b.WriteByte(escaped)

			l.pos++

		default:
			b.WriteByte(c)
		}
	}

	return "", &QueryError{Kind: queryUnclosedQuote, Fragment: jsTrim(l.query[from:])}
}

// match consumes the text that re matches at the position of the lexer.
// It returns the first group of re when re has one, or else the matched
// text. Its bool is false when re does not match.
func (l *queryLexer) match(re *regexp.Regexp) (string, bool) {
	m := re.FindStringSubmatch(l.query[l.pos:])
	if m == nil {
		return "", false
	}

	l.pos += len(m[0])

	return m[len(m)-1], true
}

// normalizeQuery returns query in the form that the lexer of DIM reads. It
// lowercases the query and turns the typographic quotes U+2018 through
// U+201A and U+201C through U+201E into ASCII quotes. It lowercases U+0130
// to "i" and U+0307, as String.prototype.toLowerCase does.
// [strings.ToLower] lowercases U+0130 to "i" alone.
func normalizeQuery(query string) string {
	query = strings.ToLower(strings.ReplaceAll(query, "\u0130", "i\u0307"))

	return strings.Map(func(r rune) rune {
		switch {
		case r >= '\u2018' && r <= '\u201a':
			return '\''
		case r >= '\u201c' && r <= '\u201e':
			return '"'
		default:
			return r
		}
	}, query)
}

// jsPattern compiles a pattern of the lexer of DIM for a match at the start
// of a text. In the pattern, \s stands for the characters of jsSpace. The
// pattern puts \s either outside a character class or at the start of a
// negated class.
func jsPattern(pattern string) *regexp.Regexp {
	pattern = strings.NewReplacer(`[^\s`, `[^`+jsSpace, `\s`, `[`+jsSpace+`]`).Replace(pattern)

	return regexp.MustCompile(`^` + pattern)
}

// isJSSpace reports whether r is a character of jsSpace.
func isJSSpace(r rune) bool {
	return jsSpaceChar.MatchString(string(r))
}

// jsTrim removes the characters of jsSpace from both ends of s, as
// String.prototype.trim does.
func jsTrim(s string) string {
	return strings.TrimFunc(s, isJSSpace)
}

// isQuote reports whether c opens a quoted string.
func isQuote(c byte) bool {
	return c == '"' || c == '\''
}
