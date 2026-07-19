// Package typography detects non-ASCII typographic characters that a
// text change newly introduces, so a PreToolUse hook can deny the
// write and steer toward ASCII equivalents.
//
// Three classes are flagged: dash punctuation other than the ASCII
// hyphen-minus (Unicode category Pd, plus the U+2212 minus sign),
// curly single and double quotation marks (U+2018 through U+201F),
// and the horizontal ellipsis (U+2026). Guillemets (U+00AB and
// U+00BB) are deliberately not flagged: they are legitimate quotation
// marks in many languages and have no unambiguous ASCII replacement.
//
// Detection is count-based rather than presence-based. The input is
// one or more before/after text pairs; per rune, the counts across
// all befores are subtracted from the counts across all afters, and
// only a positive net delta is reported. Pre-existing characters that an
// edit preserves, moves, or deletes therefore never trip the check,
// which keeps the deny loop convergent: an agent keeping text that
// already contains an em dash is not blocked from doing so.
//
// The package is pure string computation with no filesystem or
// process access; callers supply the before text (an Edit's
// old_string, a Write's current on-disk content).
package typography
