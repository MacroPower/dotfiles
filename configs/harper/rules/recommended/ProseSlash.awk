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
# Names pass as well. A quote, a `[`, or an `=` before the pair marks a
# quoted value, a link, or an attribute (`"text/plain"`,
# `[database/sql]`, `targetDir="images/qr"`). A pair before `package`
# names an import path (`the net/http package`), and a pair that opens
# with `golang/` or `kubernetes/` names a repository. A pair that opens
# with a top-level media type such as `text` or `image` passes
# (`text/html`). Pairs that end in another such type or in `binary`
# still report (`audio/video`, `text/binary`), because they join two
# words.
#
# It hit 162 lines in the measure.py corpus, and 17 of 20 sampled hits
# joined two words that `and` or `or` can join. The misses named a
# thing: a Hugo edition (`extended/deploy`), the shorthand `k/k` for
# the kubernetes repository, and a namespace and name in a table.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseSlash: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  pair = "(^|[^A-Za-z0-9_./~:@\"'=[-])[a-z]+/[a-z]+([^A-Za-z0-9_/.-]|\\.([^A-Za-z0-9]|$)|$)"
  owner = "^(golang|kubernetes)/"
  media = "^(application|text|image|audio|video|multipart|font)/"
  kinds = "/(application|text|image|audio|video|multipart|font|binary)$"
}

function prose(text,   rest, base, at, hit, after) {
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
    after = substr(rest, at + RLENGTH)
    if (hit != "and/or" && hit !~ owner && !(hit ~ media && hit !~ kinds) && after !~ /^[ \t]+packages?([^a-z]|$)/) {
      printf "%s:%d:%d: Style::ProseSlash: Slash in `%s`. Join the words with `and` or `or`, or set a path or name in a code span.\n", FILENAME, FNR, column(text, base + at), hit
      return
    }
    base += at + RLENGTH - 1
    rest = substr(rest, at + RLENGTH)
  }
}
