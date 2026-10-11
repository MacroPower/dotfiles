# Reports a Unicode arrow or its HTML entity standing in for a verb in
# markdown prose (`client → proxy`, `Settings → Integrations`). The
# Recommended check of the same name reports the spaced ASCII arrows.
# An arrow that names a key passes: after a modifier glyph or a `Ctrl+`
# style prefix, before the word `key`, or alone in a table cell. The
# up, down, and northeast arrows stay out, because docs use them as a
# link icon or a UI glyph.
#
# Output shape, one line per finding:
#   <file>:<line>:<col>: Style::ProseArrow: <message>
#
# The skeleton below the check reads markdown the way ProseHeading.awk
# does and also covers lines that check skips. It passes each prose line
# to prose() after blanking code spans, HTML code, kbd, samp, and pre
# elements, autolinks, link destinations, and bare URLs to spaces, so
# columns stay put. It skips YAML front matter, fenced code at any
# indentation or inside a blockquote, indented code (an indented line
# after a blank line outside a list, so a nested list item still
# counts), and HTML comments. Under the C locale the nix build sandbox
# uses, gawk reads bytes, so column() converts a byte offset to the
# character column a reader sees.

function BEGIN_CHECK(   list, n, i) {
  n = split("→ ← ↔ ⇒ ⇐ ⇔ ⟶ ⟵ ⟷ ⟹ ⟸ ⟺ ↦ ⟼ ➔ ➜ ➝ ➞ ➡ ⬅ &rarr; &larr; &harr; &rArr; &lArr; &hArr;", list, " ")
  arrow = ""
  for (i = 1; i <= n; i++) {
    arrow = arrow "|" list[i]
  }
  arrow = "(" substr(arrow, 2) ")"
  # An arrow that names a key: after a modifier glyph or a `Ctrl+`
  # style prefix, before the word `key`, or alone in a table cell.
  keyname = "(⌘|⌃|⌥|⇧|[A-Za-z]+[+-])+" arrow "|" arrow "[ \t]*(arrow[ \t]+)?keys?([^A-Za-z]|$)|\\|[ \t]*" arrow "[ \t]*(\\||$)"
}

function prose(text,   hit) {
  text = blank(text, keyname)
  if (match(text, arrow)) {
    hit = substr(text, RSTART, RLENGTH)
    printf "%s:%d:%d: Style::ProseArrow: Arrow %s stands in for a verb. Write the verb, or use > for a menu path.\n", FILENAME, FNR, column(text, RSTART), hit
  }
}

BEGIN {
  front_matter = 0
  fence_char = ""
  fence_len = 0
  prev_blank = 1
  in_list = 0
  in_indent = 0
  in_comment = 0
  # A UTF-8 locale reads é as one character; the C locale the nix
  # build sandbox runs under reads it as two bytes.
  utf8 = length("é") == 1
  BEGIN_CHECK()
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

# Blanks each inline code span: a backtick run closed by the next run
# of the same length. An unclosed run stays literal.
function strip_code(s,   out, i, L, c, n, j, m, found) {
  out = ""
  i = 1
  L = length(s)
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
      out = out substr(s, i, n)
      i += n
    }
  }
  return out
}

function clean(s) {
  s = strip_code(s)
  s = blank(s, "<!--([^-]|-[^-])*-->")
  s = blank(s, "<(code|kbd|samp|pre)[^>]*>([^<]|<[^/])*</(code|kbd|samp|pre)>")
  s = blank(s, "<[A-Za-z][A-Za-z0-9+.-]*:[^ \t<>]*>")
  s = blank(s, "\\]\\([^) \t]*([ \t]+\"[^\"]*\")?\\)")
  s = blank(s, "https?://[^ \t<>)]*")
  return s
}

# Converts a 1-based offset in s to a character column, so a byte
# offset under the C locale reports the column a UTF-8 reader sees.
function column(s, off,   pre) {
  if (utf8) {
    return off
  }
  pre = substr(s, 1, off - 1)
  return length(pre) - gsub("[\x80-\xBF]", "", pre) + 1
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
  next
}

/^(    |\t)/ && (in_indent || (prev_blank && !in_list)) {
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
    fence_char = substr(line, 1, 1)
    fence_len = 0
    while (substr(line, fence_len + 1, 1) == fence_char) {
      fence_len++
    }
    prev_blank = 0
    next
  }
  if (raw ~ /^[ \t]*<!--/ && raw !~ /-->/) {
    in_comment = 1
    next
  }
  if (raw ~ /^[ \t]*([-*+]|[0-9]+[.)])[ \t]/) {
    in_list = 1
  } else if (prev_blank && raw !~ /^[ \t]/) {
    in_list = 0
  }
  prev_blank = 0
  prose(clean(raw))
}
