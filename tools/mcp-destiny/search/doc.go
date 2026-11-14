// Package search implements the search language of the web client of
// Agg's Destiny Vault Manager. The web client runs the search code of
// Destiny Item Manager (DIM). The package holds a port of the lexer and the
// parser of DIM. It also ports a closed set of the filters of the web
// client. Those filters test the query of an autotag rule against the items
// of an account.
//
// # Parse Tree
//
// [ParseQuery] parses a query into a parse tree. Each node of the tree is an
// and, an or, a not, or a filter with a name and a value. The parser does
// not check filter names or filter values.
//
// A query that the parser rejects gives a [QueryError] in place of a tree.
// The web client skips most such queries. It still runs a few of them, but
// it reads each one differently from its text. Such a query has a ")" that
// closes no "(", for example, or an unquoted "or" at its end. The error
// reports whether the web client runs the query. A caller can therefore
// tell a query that the web client skips from a query that it misreads.
//
// # Filters
//
// [CompileQuery] turns a parse tree into a test of an item. It evaluates a
// closed set of filters. The set holds filters such as the is: words, tag:,
// count:, and name:. It also holds the rating filters of the roll sheets,
// such as rate: and verdict:. [SupportedKeywords] names each filter of the
// set.
//
// A term outside the set voids the whole query, and the compiled query then
// has no test. The web client voids a whole query when one of its terms is
// invalid. The package cannot tell whether the web client accepts such a
// term. A test of the other terms can therefore match items that the web
// client does not match. A value that the web client rejects, such as
// count:=<2, also voids the query. The web client skips an autotag rule
// whose query holds such a value, whatever its other terms mean.
//
// The rating filters read the ratings of one game mode, and a compiled query
// names the game modes that it reads. A caller therefore loads only the
// ratings that a query reads.
//
// # Outcomes
//
// A test gives each item one of these outcomes:
//
//   - Match: the query matches the item.
//   - No match: the query does not match the item.
//   - Undecided: the package cannot decide the item. The profile lacks what
//     a filter reads, such as the perks of a copy that a rating filter
//     rates. The outcome carries each reason.
//
// The and, or, and not nodes combine the outcomes of their operands in
// Kleene logic. An and does not match when one of its terms does not match,
// and an or matches when one of its terms matches. Otherwise an undecided
// term makes the and or the or undecided. A not keeps an undecided term
// undecided.
//
// # Items
//
// A test runs in a [Context], which [NewContext] builds from a snapshot of
// the inventory. The context holds the items that the web client builds, in
// every location. DIM builds no item in a bucket that holds dummy items. The
// web client also builds no item whose item set the EquipableItemSet
// definitions lack. It builds no item with an item set at all when the
// service publishes no such definitions. The filters that compare an item
// with other items, such as is:dupe and count:, therefore never count such
// an item.
//
// The rule editor of the web client previews a rule against the built items
// with an instance id that DIM marks as equipment, in every location.
// [OwnedItems] gives those items, and [UnbuiltLine] counts the owned items
// that the web client does not build.
package search
