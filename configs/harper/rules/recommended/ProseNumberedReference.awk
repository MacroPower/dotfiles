# Reports a reference to a numbered line of a file in markdown prose
# and source comments. It reports a file name with a line number after
# a colon, the word `line` or `lines` before a number, and a GitHub link
# to a line on a branch (`blob/main/cli.go#L264`). The numbers move when
# someone edits the file, and nothing points the editor at the
# reference. The fix names the function or the section, or links a
# commit permalink, whose lines stay put.
#
# The check reads the raw line in `$0`, which keeps the code spans and
# link destinations that the reader blanks, because most of these
# references sit inside one. That also reaches this comment, so its
# examples describe the forms instead of spelling them out. A file
# reference needs an extension from a list of source and config
# formats, so a host and port (`example.com:443`) passes. A link to a
# commit hash of six or more hex digits or to a version tag (`v1.2.3`)
# passes too. `step N` stays out, because the skills cite their own
# steps on purpose. Commit messages and changelogs skip the check,
# because a commit pins the lines it names.
#
# It hit 12 lines in the measure.py corpus, and 10 of 12 named a line of
# a file that can change. The misses numbered the lines of a backtrace
# and the lines that a Hugo option highlights.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseNumberedReference: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  ext = "(go|py|pyi|rs|ts|tsx|js|jsx|mjs|cjs|nix|c|h|cc|cpp|hpp|java|kt|rb|sh|bash|fish|md|yaml|yml|json|toml|awk|weir|lua|swift|zig|ex|exs|hs|cs|php|scala|sql|tf|proto|vue|css|html)"
  file_line = "[A-Za-z0-9][A-Za-z0-9_./-]*\\." ext ":[0-9]+(-[0-9]+)?"
  line_ref = "(^|[^A-Za-z])[Ll]ines?[ \t]+[0-9]+(-[0-9]+)?"
  anchor = "/(blob|tree)/[^/ \t]+/[^ \t)]*#L[0-9]+"
}

function report(at, what) {
  printf "%s:%d:%d: Style::ProseNumberedReference: Numbered reference `%s`. Name the function or the section, or link a commit permalink.\n", FILENAME, FNR, column($0, at), what
}

function prose(text,   at, hit, ref) {
  if (match($0, file_line)) {
    report(RSTART, substr($0, RSTART, RLENGTH))
    return
  }
  if (match($0, anchor)) {
    hit = substr($0, RSTART, RLENGTH)
    at = RSTART + index(hit, "#L") - 1
    ref = hit
    sub(/^\/(blob|tree)\//, "", ref)
    sub(/\/.*/, "", ref)
    if (ref !~ /^[0-9a-f]+$/ || length(ref) < 6) {
      if (ref !~ /^v?[0-9]+\./) {
        report(at, substr(hit, index(hit, "#L")))
        return
      }
    }
  }
  if (match($0, line_ref)) {
    at = RSTART
    if (substr($0, at, 1) !~ /[Ll]/) {
      at++
    }
    report(at, substr($0, at, RSTART + RLENGTH - at))
  }
}
