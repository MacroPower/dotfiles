// Package search implements the search language of the web client of
// Agg's Destiny Vault Manager. The web client runs the search code of
// Destiny Item Manager (DIM). The package holds a port of the lexer and the
// parser of DIM.
//
// A caller parses a query into a parse tree and walks the tree to build its
// own test of an item. Each node of the tree is an and, an or, a not, or a
// filter with a name and a value. The parser does not check filter names or
// filter values, so the caller decides which filters it knows.
//
// A query that the parser rejects gives a [QueryError] in place of a tree.
// The web client skips most such queries. It still runs a few of them, but
// it reads each one differently from its text. Such a query has a ")" that
// closes no "(", for example, or an unquoted "or" at its end. The error
// reports whether the web client runs the query. A caller can therefore
// tell a query that the web client skips from a query that it misreads.
package search
