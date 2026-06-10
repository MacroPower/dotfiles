// Package compact shrinks verbose-but-successful command output by
// stripping ANSI/VT escape sequences and collapsing consecutive runs
// of byte-identical lines (uniq -c semantics) into a single line plus
// a count marker. It backs hook-router's PostToolUse:Bash output
// compaction.
//
// Compaction is conservative by construction: output below a size
// floor is left alone, and a rewrite is only surfaced when it is
// strictly shorter than the original, so the transform can only ever
// shrink what the model reads.
package compact
