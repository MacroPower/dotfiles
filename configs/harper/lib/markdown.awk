# Reads markdown for a prose check. A check file pulls lib/markdown.awk
# in with `@include "markdown.awk"`, and its caller puts this directory
# on AWKPATH. The check defines two functions:
#
#   BEGIN_CHECK()  runs once at startup, after `utf8` is set
#   prose(text)    runs on each prose line
#
# lib/markdown.awk passes each prose line to prose() after blanking code
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
# lib/markdown.awk drops the `\r` from each line that ends in `\r\n`. A
# file with CRLF line endings therefore draws the same findings as its
# copy with LF endings. lib/markdown.awk sets RS in BEGIN, and gawk runs
# BEGIN before it reads the first line. A rule that a check puts ahead
# of the include therefore reads the same lines.
#
# A bare URL never ends in a mark that closes a sentence, a clause, a
# quote, a bracket, or emphasis. A straight or curly closing quote
# counts as such a mark. The period after the URL in `Read
# https://example.com.` therefore still ends the sentence, and so does
# the period in `See “https://example.com.”`.
#
# Before each call to prose(), lib/markdown.awk sets `bare_line` to 1
# when the line holds only `>` markers, HTML comments, `pre` elements,
# and whitespace, and to 0 otherwise. A check that splits paragraphs
# ends one at a bare line, such as a bare `>` between two quoted
# paragraphs. CommonMark reads a line that opens with an HTML comment or
# with `<pre` as an HTML block, and that block ends the open paragraph.
# A line that holds only a code span, a URL, or an inline `code`, `kbd`,
# or `samp` element also reaches prose() as spaces. That line still
# holds text, so it continues the sentence of the line above it. A check
# therefore tests `bare_line` and never the blank text.
#
# gawk counts an offset in characters under a UTF-8 locale and in bytes
# under the C locale that the nix build sandbox uses. Blanking writes
# one space for each character or each byte that it removes. An offset
# in the text that prose() gets is therefore the same offset in the
# line. column() converts the offset to the character column that a
# person sees in an editor.
#
# With RAW=1 in the environment, lib/markdown.awk passes every line to
# prose() whole, string literals included, for a source file with no
# markdown structure.
#
# With SHADOW=1 in the environment, lib/markdown.awk takes the comment
# shadow that prose-lint builds from a source file. Each comment line
# keeps its line and column there, and every other line reads `x`.
# lib/markdown.awk takes an `x` line as blank, so the comments on each
# side of a line of code stay apart. It blanks the comment marker (`#`,
# `//`, `///`, `//!`, or `--`) to spaces, so the columns stay put, and
# drops the ` x` that the shadow adds after a line that ends in `-`,
# `#`, `/`, `*`, or `!`. It also turns off the indented-code rule,
# because comment text is often indented under its marker.

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
  bare_line = $0 ~ /^[ \t]*$/
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

# Blanks each bare URL in s. A URL runs from `http://` or `https://` to
# the next space, `<`, `>`, or `)`. The function leaves out the marks at
# its end that close a sentence, a clause, a quote, a bracket, or
# emphasis. Each such mark stays in the text, and a curly closing quote
# counts as one. The URL keeps the marks inside its path
# (`https://example.com/v1.2/guide`).
#
# The pattern of these marks names each curly closing quote by its
# UTF-8 bytes, as ends_sentence() does. It matches the whole character
# under the C locale and under a UTF-8 locale. A URL that ends in
# another character, such as `é`, keeps every byte of it.
function blank_urls(s,   out, n) {
  out = ""
  while (match(s, /https?:\/\/[^ \t<>)]*/)) {
    out = out substr(s, 1, RSTART - 1)
    s = substr(s, RSTART)
    n = RLENGTH
    if (match(substr(s, 1, n), /([.,:;?!*_~"'\]]|\342\200\235|\342\200\231)+$/)) {
      n = RSTART - 1
    }
    out = out spaces(n)
    s = substr(s, n + 1)
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
  t = blank_urls(s)
  # Each `*` or `:` that the URL leaves at the start of the line, as on a
  # wrapped line that opens `https://example.com/* covers`, becomes a
  # space too. An emphasized URL leaves two `*` marks there
  # (`*https://example.com* covers`). Otherwise the checks read the line
  # as a list item or as a definition (`: text`).
  if (s !~ /^[ \t]*[*:]([ \t]|$)/) {
    while (match(t, /^[ \t]*[*:]([ \t]|$)/)) {
      t = spaces(RLENGTH) substr(t, RLENGTH + 1)
    }
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

# Reports whether token t ends a sentence. Such a token ends in `.`,
# `?`, or `!`, after any closing quote, paren, bracket, or emphasis. A
# closing quote is straight or curly (`"`, `'`, `”`, or `’`). `e.g.`,
# `i.e.`, `vs.`, `cf.`, and a three-period ellipsis do not end a
# sentence.
#
# The function returns 1 for a token that ends a sentence, and 0 for any
# other token. It returns 2 for a token whose closing marks hold a
# quote, because such a quote can also close a quotation inside a
# sentence (`prints "Lucky penny!" every time`). The token after it
# then decides. The sentence continues when continues_sentence()
# accepts that token, and it ends at the quote when another token
# follows or the paragraph ends.
#
# Each check that reads a sentence one token at a time calls this
# function, so those checks end a sentence at the same token.
# ProseCallout splits a note into sentences with a pattern of its own.
# That pattern ends a sentence at the same closing marks, curly quotes
# included. When those marks hold a quote, ProseCallout passes the first
# token after them to continues_sentence(). A quotation inside a
# sentence of a note then does not end the sentence either.
#
# The patterns write each curly closing quote as its UTF-8 bytes in
# octal, `\342\200\235` for `”` and `\342\200\231` for `’`. They match
# the character under a UTF-8 locale and its three bytes under the C
# locale. Each quote is an alternative of its own, because a bracket
# expression of the bytes of both quotes misses `’` under a UTF-8
# locale. Under the C locale, such a bracket expression matches any one
# of the bytes. The sentence-ends test of prose-lint runs the patterns
# under both locales.
function ends_sentence(t) {
  if (t ~ /\.\.\.(["')*_\]]|\342\200\235|\342\200\231)*$/ || tolower(t) ~ /^[("'*_]*(e\.g|i\.e|vs|cf)\.$/) {
    return 0
  }
  if (t ~ /[.?!][)*_\]]*(["']|\342\200\235|\342\200\231)(["')*_\]]|\342\200\235|\342\200\231)*$/) {
    return 2
  }
  return t ~ /[.?!][)*_\]]*$/
}

# Reports whether token t continues the sentence after a token for which
# ends_sentence() returned 2. Such a token opens with a lowercase letter,
# after any opening paren, bracket, or emphasis (`every`, `*every*`). A
# new sentence opens with a capital, a digit, or another mark.
#
# An HTML attribute (`src="img/table.svg"`) also opens with a lowercase
# letter, but it does not continue a sentence. Such an attribute
# follows the `alt` text of an image tag that spans lines, after the
# quote that closes the text.
#
# lib/markdown.awk blanks a code span, so the token after a span that
# follows the quote decides. A sentence that opens with a span and then
# a lowercase word (`"Done." `make` runs next`) therefore continues the
# sentence before it.
function continues_sentence(t) {
  return t ~ /^[(\[*_]*[a-z]/ && t !~ /^[a-z][a-z0-9-]*=/
}

# Reports whether token t opens with a straight or a curly opening
# quote. The quote can follow any opening paren, bracket, or emphasis,
# as in continues_sentence() (`"Save`, `(“Save`, `**“Stop!”**`). A
# sentence that opens with such a token and continues past a quoted
# mark opens with a quotation (`"Save changes?" appears`). Its first
# words then belong to the quotation, not to the clause after it. The
# pattern writes `“` and `‘` as their UTF-8 bytes, `\342\200\234` and
# `\342\200\230`, as ends_sentence() writes the closing quotes.
function opens_quote(t) {
  return t ~ /^[(\[*_]*(["']|\342\200\234|\342\200\230)/
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
  # lib/runs.awk reads `raw` to find a horizontal rule, because clean()
  # can leave a list item with only marks.
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
  bare_line = blank(raw, "<!--([^-]|-[^-])*-->|<pre([ \t][^>]*)?>([^<]|<[^/])*</pre>") ~ /^[ \t]*$/
  prose(clean(raw))
}
