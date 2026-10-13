# Reports two lowercase words joined by a slash in markdown prose
# (`read/write`, `client/server`). The slash can mean `and`, `or`, or
# `per`, so the reader picks one, and the technical-writing skill bans
# it. The Required ProseAndOr rule reports `and/or`, so this check
# skips it.
#
# Paths and URLs pass: a slash before or after the pair (`/etc/hosts`,
# `a/b/c`), a `.`, `~`, `:`, or `@` before it, or a file extension
# after it. A capitalized pair passes too (`I/O`, `CI/CD`, `TCP/IP`).
# The reader blanks code spans first, so a match inside one passes.
#
# It hit 126 lines in the kubernetes and nix.dev docs, and 9 of 10
# sampled hits joined two words that `and` or `or` can join. The miss
# was the shorthand `k/k` for the kubernetes repository.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseSlash: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  pair = "(^|[^A-Za-z0-9_./~:@-])[a-z]+/[a-z]+([^A-Za-z0-9_/.-]|\\.([^A-Za-z0-9]|$)|$)"
}

function prose(text,   rest, base, at, hit) {
  rest = text
  base = 0
  while (match(rest, pair)) {
    at = RSTART
    if (substr(rest, at, 1) !~ /[a-z]/) {
      at++
    }
    hit = substr(rest, at)
    match(hit, /^[a-z]+\/[a-z]+/)
    hit = substr(hit, 1, RLENGTH)
    if (hit != "and/or") {
      printf "%s:%d:%d: Style::ProseSlash: Slash in `%s`. Join the words with `and` or `or`, or set a path or name in a code span.\n", FILENAME, FNR, column(text, base + at), hit
      return
    }
    base += at + RLENGTH - 1
    rest = substr(rest, at + RLENGTH)
  }
}
