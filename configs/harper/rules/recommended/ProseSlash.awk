# Overview
#
# Reports two lowercase words joined by a slash in markdown prose and in
# source comments (`read/write`, `client/server`). The slash can mean
# `and`, `or`, or `per`, so the reader must pick one, and the
# technical-writing skill bans it. The fix joins the words with `and` or
# `or`, or sets a path or a name in a code span. The rule sits at
# Recommended, because a pair that names one thing also matches.
#
# Expression
#
# The check includes lib/markdown.awk and reads one prose line at a
# time. lib/markdown.awk blanks code spans first, so a match inside one
# passes. `pair` matches two runs of lowercase letters joined by a
# slash. It also reads the characters on each side of the pair, so the
# check can skip a path, a URL, or a name.
#
# `media` holds every top-level media type that the Internet Assigned
# Numbers Authority (IANA) registers, and `kinds` holds the endings that
# mark two joined words. The media guard skips each pair that `media`
# matches and `kinds` does not. Two joined words therefore pass when the
# first is a top-level type and `kinds` lacks the second (`model/view`).
#
# configs/harper/comment-checks.txt lists the check, so prose-lint also
# runs it on the comments of a source file. The list puts `!go` after
# the name, so prose-lint skips the check on Go comments. A pair that
# joins two words in a Go comment therefore passes (`read/write`).
#
# False Positives
#
# Paths and URLs pass. A slash before or after the pair marks one
# (`/etc/hosts`, `a/b/c`), and so does a `.`, `~`, `:`, or `@` before
# the pair or a file extension after it. A capitalized pair passes too
# (`I/O`, `CI/CD`, `TCP/IP`).
#
# Names pass as well. A quote, a `[`, or an `=` before the pair marks a
# quoted value, a link, or an attribute (`"text/plain"`,
# `[database/sql]`, `targetDir="images/qr"`). A pair before `package`
# names an import path (`the net/http package`), and a pair that opens
# with `golang/` or `kubernetes/` names a repository.
#
# A pair that opens with a top-level media type passes as a media type
# (`text/html`, `message/http`). When such a pair ends in another
# top-level type or in `binary`, it joins two words and still reports
# (`audio/video`, `text/binary`). The type `example` is the exception.
# IANA registers it as a subtype of other types, so `text/example`
# passes.
#
# Go comments name import paths with no `package` after them (`the
# client in net/http`). `pair` cannot tell such a path from two joined
# words, so the check skips every Go comment.
#
# Related Rules
#
# ProseAndOr reports `and/or`, so this check skips the phrase.
#
# Measurement
#
# The rule hit 162 lines in the measure.py corpus, and 17 of 20 sampled
# hits joined two words that `and` or `or` can join. Each of the three
# misfires named one thing. They were a Hugo edition
# (`extended/deploy`), the shorthand `k/k` for the kubernetes
# repository, and a namespace and name in a table.
#
# configs/harper/comment-checks.txt skips the Go comments of the
# corpus, so every hit is in a markdown file.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseSlash: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  pair = "(^|[^A-Za-z0-9_./~:@\"'=[-])[a-z]+/[a-z]+([^A-Za-z0-9_/.-]|\\.([^A-Za-z0-9]|$)|$)"
  owner = "^(golang|kubernetes)/"
  media = "^(application|audio|example|font|haptics|image|message|model|multipart|text|video)/"
  kinds = "/(application|audio|font|haptics|image|message|model|multipart|text|video|binary)$"
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
      printf "%s:%d:%d: Style::ProseSlash: Slash in `%s`. Join the words with `and` or `or`, or set a path or name in a code span.\n", FILENAME, FNR, column(base + at), hit
      return
    }
    base += at + RLENGTH - 1
    rest = substr(rest, at + RLENGTH)
  }
}
