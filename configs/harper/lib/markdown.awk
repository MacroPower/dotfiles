# Reads markdown for a prose check. A check file pulls this reader in
# with `@include "markdown.awk"`, and its caller puts this directory on
# AWKPATH. The check defines two functions:
#
#   BEGIN_CHECK()  runs once at startup, after `utf8` is set
#   prose(text)    runs on each prose line
#
# The reader passes each prose line to prose() after blanking code
# spans, HTML code, kbd, samp, and pre elements, autolinks, link
# destinations, and bare URLs to spaces, so columns stay put. It skips
# YAML front matter, fenced code at any indentation or inside a
# blockquote, HTML comments, and link reference definitions
# (`[guide]: https://example.com`). It also skips indented code (an
# indented line after a blank line outside a list, so a nested list
# item still counts). It skips a MyST directive's opening fence and the
# `:name: value` option lines under it, because ProseEmoji's shortcode
# pattern matches an option such as `:class: tip`.
#
# The reader drops the `\r` from each line that ends in `\r\n`. A file
# with CRLF line endings therefore draws the same findings as its copy
# with LF endings. The reader sets RS in BEGIN, and gawk runs BEGIN
# before it reads the first line. A rule that a check puts ahead of the
# include therefore reads the same lines.
#
# A bare URL never ends in an ASCII mark that closes a sentence, a
# clause, a quote, a bracket, or emphasis. The period after the URL in
# `Read https://example.com.` therefore still ends the sentence.
#
# gawk counts an offset in characters under a UTF-8 locale and in bytes
# under the C locale that the nix build sandbox uses. Blanking writes
# one space for each character or each byte that it removes. An offset
# in the text that prose() gets is therefore the same offset in the
# line. column() converts the offset to the character column that a
# person sees in an editor.
#
# With RAW=1 in the environment, the reader passes every line to
# prose() whole, string literals included, for a source file with no
# markdown structure.
#
# With SHADOW=1 in the environment, the reader takes the comment shadow
# that prose-lint builds from a source file. Each comment line keeps its
# line and column there, and every other line reads `x`. The reader
# takes an `x` line as blank, so the comments on each side of a line of
# code stay apart. It blanks the comment marker (`#`, `//`, `///`,
# `//!`, or `--`) to spaces, so the columns stay put, and drops the ` x`
# that the shadow adds after a line that ends in `-`, `#`, `/`, `*`, or
# `!`. It also turns off the indented-code rule, because comment text
# is often indented under its marker.

BEGIN {
  RS = "\r?\n"
  front_matter = 0
  fence_char = ""
  fence_len = 0
  prev_blank = 1
  in_list = 0
  in_indent = 0
  in_comment = 0
  myst_options = 0
  span_open = 0
  raw_mode = ENVIRON["RAW"] != ""
  shadow_mode = ENVIRON["SHADOW"] != ""
  # A UTF-8 locale reads é as one character, and the C locale that the
  # nix build sandbox runs under reads it as two bytes.
  utf8 = length("é") == 1
  BEGIN_CHECK()
}

raw_mode {
  prose($0)
  next
}

shadow_mode {
  if ($0 == "x") {
    $0 = ""
  } else if (match($0, /^[ \t]*(\/\/[\/!]?|#|--)/)) {
    $0 = spaces(RLENGTH) substr($0, RLENGTH + 1)
    if ($0 ~ /[-#\/*!][ \t]* x$/) {
      $0 = substr($0, 1, length($0) - 2)
    }
  }
}

function spaces(n,   s) {
  s = sprintf("%*s", n, "")
  return s
}

# Replaces every match of re in s with spaces of the same length.
function blank(s, re,   out) {
  out = ""
  while (match(s, re)) {
    out = out substr(s, 1, RSTART - 1) spaces(RLENGTH)
    s = substr(s, RSTART + RLENGTH)
  }
  return out s
}

# Blanks each inline code span. Spans open with a run of backticks and
# close at the next run of the same length. An unclosed run blanks the
# rest of its line, because a span can wrap onto the next line of its
# paragraph. When a run of the same length appears on the next line,
# the span closes there. Otherwise the run was literal, and the next
# line reads as prose.
function strip_code(s,   out, i, L, c, n, j, m, found) {
  out = ""
  i = 1
  L = length(s)
  if (span_open) {
    j = 1
    found = 0
    while (j <= L) {
      if (substr(s, j, 1) == "`") {
        m = 0
        while (substr(s, j + m, 1) == "`") {
          m++
        }
        if (m == span_open) {
          found = 1
          break
        }
        j += m
      } else {
        j++
      }
    }
    # A span closes on the next line or not at all. With no closer
    # here, the run was literal, and this line reads as prose.
    if (found) {
      out = spaces(j + span_open - 1)
      i = j + span_open
    }
    span_open = 0
  }
  while (i <= L) {
    c = substr(s, i, 1)
    if (c == "\\") {
      out = out substr(s, i, 2)
      i += 2
      continue
    }
    if (c != "`") {
      out = out c
      i++
      continue
    }
    n = 0
    while (substr(s, i + n, 1) == "`") {
      n++
    }
    j = i + n
    found = 0
    while (j <= L) {
      if (substr(s, j, 1) == "`") {
        m = 0
        while (substr(s, j + m, 1) == "`") {
          m++
        }
        if (m == n) {
          found = 1
          break
        }
        j += m
      } else {
        j++
      }
    }
    if (found) {
      out = out spaces(j + n - i)
      i = j + n
    } else {
      out = out spaces(L - i + 1)
      span_open = n
      break
    }
  }
  return out
}

function clean(s,   t) {
  s = strip_code(s)
  s = blank(s, "<!--([^-]|-[^-])*-->")
  s = blank(s, "<(code|kbd|samp|pre)[^>]*>([^<]|<[^/])*</(code|kbd|samp|pre)>")
  s = blank(s, "<[A-Za-z][A-Za-z0-9+.-]*:[^ \t<>]*>")
  s = blank(s, "\\]\\([^) \t]*([ \t]+\"[^\"]*\")?\\)")
  # A bare URL never ends in an ASCII mark that closes a sentence, a
  # clause, a quote, a bracket, or emphasis. Each such mark after the
  # URL stays in the text. A curly closing quote still goes with the
  # URL, and ProseTypography reports a curly opening quote in front of
  # the URL. awk picks the longest match, so the URL keeps the marks
  # inside its path (`https://example.com/v1.2/guide`).
  t = blank(s, "https?://([^ \t<>)]*[^ \t<>).,:;?!*_~\"'\\]])?")
  # A `*` or a `:` that the URL leaves at the start of the line, as on a
  # wrapped line that opens `https://example.com/* covers`, becomes a
  # space too. Otherwise the checks read the line as a list item or as a
  # definition (`: text`).
  if (s !~ /^[ \t]*[*:]([ \t]|$)/ && match(t, /^[ \t]*[*:]([ \t]|$)/)) {
    t = spaces(RLENGTH) substr(t, RLENGTH + 1)
  }
  # The trimmed URL can leave a line with only `-`, `*`, and `_` marks,
  # such as `- *https://example.com*`. lib/runs.awk reads that line as a
  # horizontal rule and not as a list item. On such a line, the blank
  # takes the marks at the end of the URL with it. The line holds no
  # word, so it loses no sentence end.
  if (t ~ /^[ \t]*([-*_][ \t]*){3,}$/) {
    t = blank(s, "https?://[^ \t<>)]*")
  }
  return t
}

# Returns the number of characters in s. Under the C locale, length()
# counts bytes, so the function leaves out each continuation byte of a
# UTF-8 character.
function chars(s) {
  if (utf8) {
    return length(s)
  }
  return length(s) - gsub("[\x80-\xBF]", "", s)
}

# Converts a 1-based offset in the line that gawk is on to a character
# column. A check takes the offset from the text that prose() gets, or
# from a copy of that text that the check blanked further. Under a
# UTF-8 locale the offset already counts characters, so the function
# returns it. Under the C locale the offset counts bytes, so the
# function counts the characters before it. It counts them in `$0`,
# because blanking leaves one space for each byte of a character. The
# blanked text then holds more characters than the line.
function column(off) {
  if (utf8) {
    return off
  }
  return chars(substr($0, 1, off - 1)) + 1
}

FNR == 1 && $0 ~ /^---[ \t]*$/ {
  front_matter = 1
  next
}

front_matter {
  if ($0 ~ /^(---|\.\.\.)[ \t]*$/) {
    front_matter = 0
  }
  next
}

fence_char != "" {
  line = $0
  sub(/^[ \t>]*/, "", line)
  if (substr(line, 1, 1) == fence_char) {
    n = 0
    while (substr(line, n + 1, 1) == fence_char) {
      n++
    }
    rest = substr(line, n + 1)
    if (n >= fence_len && rest ~ /^[ \t]*$/) {
      fence_char = ""
      fence_len = 0
    }
  }
  next
}

in_comment {
  if ($0 ~ /-->/) {
    in_comment = 0
  }
  next
}

/^[ \t]*$/ {
  prev_blank = 1
  span_open = 0
  myst_options = 0
  next
}

!shadow_mode && /^(    |\t)/ && (in_indent || (prev_blank && !in_list)) {
  in_indent = 1
  prev_blank = 0
  next
}

{
  in_indent = 0
  raw = blank($0, "^([ \t]*>)+")
  if (raw ~ /^[ \t]*(```|~~~)/) {
    line = raw
    sub(/^[ \t]*/, "", line)
    span_open = 0
    fence_char = substr(line, 1, 1)
    fence_len = 0
    while (substr(line, fence_len + 1, 1) == fence_char) {
      fence_len++
    }
    prev_blank = 0
    next
  }
  if (raw ~ /^[ \t]*:::+[ \t]*\{[^}]*\}/) {
    myst_options = 1
    prev_blank = 0
    next
  }
  if (myst_options && raw ~ /^[ \t]*:[A-Za-z][A-Za-z0-9_-]*:([ \t]|$)/) {
    prev_blank = 0
    next
  }
  myst_options = 0
  # A link reference definition (`[guide]: https://example.com`) holds
  # a label and a URL, not prose. A footnote (`[^1]: text`) stays.
  if (raw ~ /^ {0,3}\[[^]^][^]]*\]:[ \t]*[^ \t]+[ \t]*("[^"]*")?[ \t]*$/) {
    prev_blank = 0
    next
  }
  if (raw ~ /^[ \t]*<!--/ && raw !~ /-->/) {
    in_comment = 1
    next
  }
  # A list runs from its first item to the next line that follows a
  # blank line without an indent. A heading or a table row also ends it.
  if (raw ~ /^[ \t]*([-*+]|[0-9]+[.)])[ \t]/) {
    in_list = 1
  } else if (prev_blank && raw !~ /^[ \t]/) {
    in_list = 0
  } else if (raw ~ /^ {0,3}(#+([ \t]|$)|\|)/) {
    in_list = 0
  }
  prev_blank = 0
  # A list item, heading, table row, or HTML line starts a new block,
  # so a code span open on the line before ends there.
  if (raw ~ /^[ \t]*(([-*+]|[0-9]+[.)])[ \t]|#+([ \t]|$)|[|<])/) {
    span_open = 0
  }
  prose(clean(raw))
}
