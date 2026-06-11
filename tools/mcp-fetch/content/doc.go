// Package content shapes fetched text for return to the caller: it
// filters lines with a grep-style RE2 matcher and slices the result to a
// rune-window page.
//
// [Grep] renders matches GNU-grep style with 1-based line numbers over
// the original text, so numbers stay meaningful after pagination.
// [Paginate] returns a rune window plus a flag and a continuation hint
// when content was elided. Patterns compile through [CompilePattern] so a
// caller can validate a pattern before doing any work.
package content
