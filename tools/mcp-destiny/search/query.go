package search

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

// The operators of a [QueryNode]. Each value is the op name that the
// parser of DIM gives the node.
const (
	// OpAnd matches an item that every operand matches.
	OpAnd QueryOp = "and"
	// OpOr matches an item that any operand matches.
	OpOr QueryOp = "or"
	// OpNot matches an item that the operand does not match.
	OpNot QueryOp = "not"
	// OpFilter matches an item that the filter accepts.
	OpFilter QueryOp = "filter"

	// opNoop stands in for an atom that does not parse. Only a parse
	// with an error gives it.
	opNoop QueryOp = "noop"
)

// The rules that a [QueryError] reports. The lexer rules of the web client
// come first, then the rule that a query has a search term, then the
// parser rules. The last three kinds name a query that the web client runs
// differently from its text.
const (
	queryUnclosedQuote QueryErrorKind = iota + 1
	queryBadEscape
	queryNoValue
	queryNoTerm
	queryMissingTerm
	queryMissingOperator
	queryUnexpectedToken
	queryStrayParen
	queryDroppedError
	queryBareOperator
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
	// Text is the source of the token in the query that the lexer reads,
	// without the characters of jsSpace at its ends.
	Text string
	// Start is the byte offset of the token in the query that the lexer
	// reads.
	Start int
	// Quoted is true for a filter token whose value the query quotes.
	Quoted bool
}

// queryLexer reads a search query with the rules of the lexer of DIM,
// which the web client uses. It returns one token per call of next, so a
// parser can stop at any token.
type queryLexer struct {
	// err is the first error that next returned.
	err   error
	query string
	pos   int
	// errStart is the position of the token that next read when it
	// returned err.
	errStart int
}

// queryTokens wraps a [queryLexer] with a buffer of one token, as the
// PeekableGenerator of DIM does. The parser can then look at the next
// token before it takes the token.
type queryTokens struct {
	lexer *queryLexer
	// next is the token that peek read and that the parser has not taken.
	next *queryToken
	// last is the token that the parser took last.
	last queryToken
}

// QueryOp is the operator of a [QueryNode].
type QueryOp string

// QueryNode is one node of the parse tree of a search query. The tree
// follows the abstract syntax tree of DIM without the positions.
type QueryNode struct {
	// err is the error that the parse stored on the node.
	err error
	// Operand is the operand of a not node.
	Operand *QueryNode
	// Op is the operator of the node.
	Op QueryOp
	// Type is the filter name of a filter node, such as "is" in
	// is:weapon. A quoted string and a bare word have the type "keyword".
	Type string
	// Args is the value of a filter node, without quotes and escapes.
	Args string
	// Text is the Text of the token of a filter node, so the filter that
	// not:x negates has the Text "not:x".
	Text string
	// Comment is the content of the comment that labels the node.
	Comment string
	// Operands holds the operands of an and node or an or node, in the
	// order of the query.
	Operands []*QueryNode
	// Quoted is true for a filter node whose value the query quotes.
	Quoted bool
}

// QueryErrorKind names the rule that a query breaks.
type QueryErrorKind int

// QueryError reports a query that the search parser of the web client
// rejects, a query without a search term, or a query that the web client
// runs differently from its text.
type QueryError struct {
	// Err is the lexer error that the parser of DIM drops. Only the kind
	// queryDroppedError sets it.
	Err error
	// Fragment is the part of the query that the error names. It comes
	// from the lowercase query with ASCII quotes that the lexer reads. For
	// a ")" that closes no "(" and for a lexer error that the parser of
	// DIM drops, Fragment is the rest of the query that the web client
	// ignores.
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
	case queryMissingTerm:
		return fmt.Sprintf("the query ends after %q, which needs a search term after it", e.Fragment)
	case queryMissingOperator:
		return fmt.Sprintf("the query has no space or operator before %q", e.Fragment)
	case queryUnexpectedToken:
		return fmt.Sprintf("the query has %q where a search term belongs", e.Fragment)
	case queryStrayParen:
		return fmt.Sprintf(`the query has a ")" that closes no "(", and the web client ignores %q`, e.Fragment)
	case queryDroppedError:
		return fmt.Sprintf("%v. The web client drops this error and ignores %q", e.Err, e.Fragment)
	case queryBareOperator:
		return fmt.Sprintf("the web client reads the unquoted word %q as a search for that word, "+
			"not as an operator. Quote the word to search for it", e.Fragment)

	default:
		return "the query has no search term"
	}
}

// Unwrap returns the lexer error that the parser of DIM drops, or nil.
func (e *QueryError) Unwrap() error {
	return e.Err
}

// WebClientRuns reports whether the web client runs a query with the
// error. The web client runs a query with a ")" that closes no "(", with a
// lexer error that the parser of DIM drops, or with an unquoted or, and,
// or not. It reads such a query differently from its text. It skips a
// query with any other error.
func (e *QueryError) WebClientRuns() bool {
	switch e.Kind {
	case queryStrayParen, queryDroppedError, queryBareOperator:
		return true
	default:
		return false
	}
}

// ParseQuery parses query with a port of the search parser of DIM, which
// the web client runs. It returns the parse tree, or nil and a
// [*QueryError] for a query that the web client skips or reads differently
// from its text. It does not check filter names or filter values.
//
// The web client skips a query that has no search term or whose parse
// tree holds an error, such as a lexer error that the parser of DIM keeps.
// It runs a query with a lexer error that the parser of DIM drops or with
// a ")" that closes no "(". It then ignores the rest of the query from
// that point. It also runs a query with an unquoted or, and, or not that
// the lexer reads as a bare word. It then searches for the word. The error
// names the first problem in that order.
func ParseQuery(query string) (*QueryNode, error) {
	lexer := newQueryLexer(query)
	tokens := &queryTokens{lexer: lexer}

	first, err := tokens.peek()
	if err != nil {
		return nil, err
	}

	if first == nil {
		return nil, &QueryError{Kind: queryNoTerm}
	}

	root := parse(tokens, 1)

	failed := root.find(func(node *QueryNode) bool { return node.err != nil })
	if failed != nil {
		return nil, failed.err
	}

	if lexer.err != nil {
		return nil, &QueryError{Kind: queryDroppedError, Fragment: lexer.rest(lexer.errStart), Err: lexer.err}
	}

	if tokens.next != nil && tokens.next.Kind == tokenClose {
		return nil, &QueryError{Kind: queryStrayParen, Fragment: lexer.rest(tokens.next.Start)}
	}

	bare := root.find((*QueryNode).isBareOperator)
	if bare != nil {
		return nil, &QueryError{Kind: queryBareOperator, Fragment: bare.Text}
	}

	return root, nil
}

// parse reads an expression whose binary operators have at least the
// precedence minPrecedence, as the parse function of DIM does. It returns
// a node for every input. An error ends the parse call and lands on the
// node that the call returns, and the caller goes on.
//
// When the right side of an operator has the same operator as the new
// node, the new node takes the operands of the right side and drops its
// error and its comment. An operator error comes back, because its token
// stays in the buffer for the caller to read again. The caller loses a
// lexer error, because the lexer reports the end of the query after it.
func parse(tokens *queryTokens, minPrecedence int) *QueryNode {
	node, err := parseAtom(tokens)
	if err != nil {
		return &QueryNode{Op: opNoop, err: err}
	}

	for {
		token, err := tokens.peek()
		if err != nil {
			node.err = err

			return node
		}

		if token == nil || token.Kind == tokenClose {
			return node
		}

		op, precedence, ok := queryOperator(token.Kind)
		if !ok {
			node.err = &QueryError{Kind: queryMissingOperator, Fragment: token.Text}

			return node
		}

		if precedence < minPrecedence {
			return node
		}

		tokens.skip()

		// Every operator is left associative.
		rhs := parse(tokens, precedence+1)

		if node.Op == op {
			node.Operands = append(node.Operands, rhs)

			continue
		}

		operands := []*QueryNode{node, rhs}
		if rhs.Op == op {
			operands = append([]*QueryNode{node}, rhs.Operands...)
		}

		// The comment of the left side moves up to the new node.
		left := node
		node = &QueryNode{Op: op, Operands: operands, Comment: left.Comment}
		left.Comment = ""
	}
}

// parseAtom reads the next atom, as the parseAtom function of DIM does. An
// atom is a filter, a group in parentheses, or a not or a comment before
// an atom. A filter named not, such as not:x, reads as the negation of the
// filter is:x. It returns an error for a missing atom, for a token that
// cannot start an atom, and for a token that does not lex.
func parseAtom(tokens *queryTokens) (*QueryNode, error) {
	token, err := tokens.pop()
	if err != nil {
		return nil, err
	}

	if token == nil {
		return nil, &QueryError{Kind: queryMissingTerm, Fragment: tokens.last.Text}
	}

	switch token.Kind {
	case tokenFilter:
		filter := &QueryNode{
			Op:     OpFilter,
			Type:   token.Keyword,
			Args:   token.Args,
			Text:   token.Text,
			Quoted: token.Quoted,
		}
		if token.Keyword != "not" {
			return filter, nil
		}

		filter.Type = filterIs

		return &QueryNode{Op: OpNot, Operand: filter}, nil

	case tokenNot:
		operand, err := parseAtom(tokens)
		if err != nil {
			return nil, err
		}

		return &QueryNode{Op: OpNot, Operand: operand}, nil

	case tokenOpen:
		group := parse(tokens, 1)

		// A group without its ")" ends at the end of the query.
		next, err := tokens.peek()
		if err != nil {
			return nil, err
		}

		if next != nil && next.Kind == tokenClose {
			tokens.skip()
		}

		return group, nil

	case tokenComment:
		node, err := parseAtom(tokens)
		if err != nil {
			return nil, err
		}

		node.Comment = token.Content

		return node, nil

	default:
		return nil, &QueryError{Kind: queryUnexpectedToken, Fragment: token.Text}
	}
}

// queryOperator returns the node operator and the precedence of a binary
// operator token, as the operator table of DIM gives them. An operator
// with a higher precedence binds tighter. The implicit and binds looser
// than or, so "a b or c" reads as "a and (b or c)". The bool is false for
// a token that is not a binary operator.
func queryOperator(kind tokenKind) (QueryOp, int, bool) {
	switch kind {
	case tokenImplicitAnd:
		return OpAnd, 1, true
	case tokenOr:
		return OpOr, 2, true
	case tokenAnd:
		return OpAnd, 3, true
	default:
		return "", 0, false
	}
}

// find returns the first node of the tree that match accepts, or nil. It
// visits each node before its operands and the operands in query order,
// as the validateQuery function of DIM does.
func (n *QueryNode) find(match func(*QueryNode) bool) *QueryNode {
	if match(n) {
		return n
	}

	if n.Operand != nil {
		return n.Operand.find(match)
	}

	for _, operand := range n.Operands {
		found := operand.find(match)
		if found != nil {
			return found
		}
	}

	return nil
}

// isBareOperator reports whether n is a bare word that spells or, and, or
// not. The lexer reads the words "or" and "and" as operators only with
// whitespace on both sides. It reads "not" as an operator only at the
// start of a term with whitespace after it. Elsewhere each word lexes as a
// bare word, which searches for the word. The Text of a bare word equals
// its Args, and the Text of the filter keyword:or does not.
func (n *QueryNode) isBareOperator() bool {
	if n.Op != OpFilter || n.Type != keywordFilter || n.Quoted || n.Text != n.Args {
		return false
	}

	switch n.Args {
	case "or", "and", "not":
		return true
	default:
		return false
	}
}

// peek returns the next token without taking it. It returns nil at the end
// of the query.
func (s *queryTokens) peek() (*queryToken, error) {
	if s.next == nil {
		token, ok, err := s.lexer.next()
		if err != nil {
			return nil, err
		}

		if ok {
			s.next = &token
		}
	}

	return s.next, nil
}

// pop takes the next token. It returns nil at the end of the query.
func (s *queryTokens) pop() (*queryToken, error) {
	token, err := s.peek()
	if err != nil || token == nil {
		return nil, err
	}

	s.skip()

	return token, nil
}

// skip takes the token that peek returned.
func (s *queryTokens) skip() {
	s.last = *s.next
	s.next = nil
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
// After an error, next reports the end of the query on every call. The
// lexer of DIM is a JavaScript generator, and a generator yields nothing
// after it throws.
func (l *queryLexer) next() (queryToken, bool, error) {
	for l.err == nil && l.pos < len(l.query) {
		start := l.pos

		token, ok, err := l.scan()
		if err != nil {
			l.err = err
			l.errStart = start

			return queryToken{}, false, err
		}

		if ok {
			token.Text = jsTrim(l.query[start:l.pos])
			token.Start = start

			return token, true, nil
		}
	}

	return queryToken{}, false, nil
}

// scan consumes the token at the position of the lexer. A run of
// whitespace gives an implicit_and token, except a run at the start or at
// the end of the query, for which the bool is false.
func (l *queryLexer) scan() (queryToken, bool, error) {
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

	// The bare-word pattern takes every character outside jsSpace except
	// ")", and the parens pattern takes ")", so the query continues with
	// whitespace here.
	l.match(querySpace)

	return queryToken{Kind: tokenImplicitAnd}, start > 0 && l.pos < len(l.query), nil
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

	return "", &QueryError{Kind: queryUnclosedQuote, Fragment: l.rest(from)}
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

// rest returns the query from the byte offset start to its end, without
// the characters of jsSpace at its ends.
func (l *queryLexer) rest(start int) string {
	return jsTrim(l.query[start:])
}

// normalizeQuery returns query in the form that the lexer of DIM reads. It
// lowercases the query and then applies [normalizeQuotes]. It lowercases
// U+0130 to "i" and U+0307, as String.prototype.toLowerCase does.
// [strings.ToLower] lowercases U+0130 to "i" alone.
func normalizeQuery(query string) string {
	return normalizeQuotes(strings.ToLower(strings.ReplaceAll(query, "\u0130", "i\u0307")))
}

// normalizeQuotes turns the typographic quotes U+2018 through U+201A into
// an ASCII apostrophe and U+201C through U+201E into an ASCII double
// quote, as normalizeQuotes of DIM does.
func normalizeQuotes(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '\u2018' && r <= '\u201a':
			return '\''
		case r >= '\u201c' && r <= '\u201e':
			return '"'
		default:
			return r
		}
	}, s)
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
