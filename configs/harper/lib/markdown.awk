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
# (`[guide]: https://example.com`). It also skips indented code, an
# indented line outside a list after a blank line or a thematic break.
# A nested list item therefore still counts. It skips a MyST directive's
# opening fence and the `:name: value` option lines under it, because
# ProseEmoji's shortcode pattern matches an option such as `:class: tip`.
#
# In a blockquote, indented code is a line indented four or more
# columns past the space after its last `>`, such as
# `>     make build`. lib/markdown.awk skips that line too. A line that
# continues a paragraph or a list of the blockquote stays prose,
# because CommonMark reads it as more text of that block.
# Some indented code in a blockquote still reaches prose().
# lib/markdown.awk reads a setext underline of `=` marks, or of two `-`
# marks, as more text of the open paragraph. Code after one therefore
# stays prose. lib/markdown.awk also does not track the text column of a
# list item in a blockquote, so code in such an item stays prose. It
# opens a list at each list marker in a blockquote, even where
# CommonMark reads the marker as more text of the open paragraph
# (`> text` / `> 2. more`, `> text` / `> *`) or as a setext underline
# (`> text` / `> -`). Code after such a line stays prose until the list
# ends.
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
# under the C locale. prose-lint runs the checks under the locale of the
# user, and check.py runs each check on its fixtures under both locales.
# Blanking writes one space for each character or each byte that it
# removes. An offset in the text that prose() gets is therefore the
# same offset in the line. column() converts the offset to the character
# column that a person sees in an editor.
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
# `#`, `/`, `*`, or `!`. It also reads indented code as prose, in a
# blockquote too, because comment text is often indented under its
# marker.

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
  quote_para = 0
  quote_closed = 0
  quote_code = 0
  item_depth = 0
  item_empty = 0
  html_open = 0
  raw_mode = ENVIRON["RAW"] != ""
  shadow_mode = ENVIRON["SHADOW"] != ""
  # A UTF-8 locale reads é as one character, and the C locale reads it
  # as two bytes.
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
# ProseCallout splits a note with a pattern of its own at each mark
# before a space, after the same closing marks, curly quotes included.
# It passes the token that ends at each split to this function. When
# this function returns 2, ProseCallout passes the first token after the
# split to continues_sentence(). It then joins the pieces of each
# sentence that continues past a split.
#
# The patterns write each curly closing quote as its UTF-8 bytes in
# octal, `\342\200\235` for `”` and `\342\200\231` for `’`. They match
# the character under a UTF-8 locale and its three bytes under the C
# locale. Each quote is an alternative of its own, because a bracket
# expression of the bytes of both quotes misses `’` under a UTF-8
# locale. Under the C locale, such a bracket expression matches any one
# of the bytes. check.py and the sentence-ends test of prose-lint run
# the patterns under both locales.
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
# sentence that opens with such a token and goes on after the closing
# quote opens with a quotation (`"Save" appears`, `"Save changes?"
# appears`). Its first words then belong to the quotation, not to the
# clause after it. The pattern writes `“` and `‘` as their UTF-8 bytes,
# `\342\200\234` and `\342\200\230`, as ends_sentence() writes the
# closing quotes.
function opens_quote(t) {
  return t ~ /^[(\[*_]*(["']|\342\200\234|\342\200\230)/
}

# Returns s with each tab expanded to spaces up to the next tab stop.
# A tab stop sits every four columns.
function expand_tabs(s,   e, i, c) {
  e = ""
  for (i = 1; i <= length(s); i++) {
    c = substr(s, i, 1)
    e = e (c == "\t" ? spaces(4 - length(e) % 4) : c)
  }
  return e
}

# Returns the column of the text of the list item that opens s, with
# tabs expanded. s opens with whitespace and a list marker. One to four
# columns of whitespace follow the marker. After five or more, the text
# is indented code, and its column sits one column past the marker. An
# item with no text on its first line (`-` or `-   `) also puts its
# column one column past the marker.
function item_column(s,   m, w) {
  match(s, /^[ \t]*([-*+]|[0-9]{1,9}[.)])/)
  m = length(expand_tabs(substr(s, 1, RLENGTH)))
  if (!match(s, /^[ \t]*([-*+]|[0-9]{1,9}[.)])[ \t]+/)) {
    return m + 1
  }
  w = length(expand_tabs(substr(s, 1, RLENGTH))) - m
  return m + ((w > 4 || RLENGTH == length(s)) ? 1 : w)
}

# Counts the blockquote markers that open the line. A marker is a `>`
# after at most three spaces. A list item indents its blockquote.
# Inside a list, the first marker can therefore also sit zero to three
# columns past the text of an open list item. `item_cols[1]` to
# `item_cols[item_depth]` hold the text columns of the open list items
# outside any blockquote, from the outermost item in. One space after
# each marker belongs to the marker, and so does one column of a tab
# there. The function sets `quote_level` to the number of markers and
# `quote_first` to the column of the first marker, or to -1. It sets
# `quote_indent` to the number of columns of whitespace before the
# text after the markers, with tabs expanded, or to -1 when no text
# follows them. The function skips a `>` after a list marker
# (`- > text`), because the line opens with the list marker. The
# list-item branch of the main rule counts that `>`.
function read_quote(   p, e, first, i) {
  match($0, /^[ \t>]*/)
  p = substr($0, 1, RLENGTH)
  e = expand_tabs(p)
  first = "^( {0,3}"
  for (i = 1; i <= item_depth; i++) {
    first = first "| {" item_cols[i] "," item_cols[i] + 3 "}"
  }
  first = first ")>"
  quote_level = 0
  quote_first = -1
  while (match(e, quote_level == 0 ? first : "^ {0,3}>")) {
    if (quote_level == 0) {
      quote_first = RLENGTH - 1
    }
    quote_level++
    e = substr(e, RLENGTH + 1)
    if (substr(e, 1, 1) == " ") {
      e = substr(e, 2)
    }
  }
  match(e, /^ */)
  quote_indent = (RLENGTH == length(e) && length(p) == length($0)) ? -1 : RLENGTH
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
  # A blank line ends every blockquote and the HTML block that
  # `html_open` marks.
  quote_para = 0
  delete quote_lists
  html_open = 0
  # As in CommonMark, a blank line right after the first line of a list
  # item closes the item when that first line holds only the list
  # marker. A blank line that holds whitespace up to the text column of
  # the item keeps the item open.
  if (item_empty && length(expand_tabs($0)) < item_cols[item_depth]) {
    item_depth--
    item_empty = 0
  }
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
  # `quote_para` holds the number of `>` markers of the open paragraph,
  # or 0 when no blockquote holds one. `quote_lists[n]` holds 1 when the
  # blockquote with n markers holds an open list. `quote_closed` holds 1
  # after a bare `>` line or a line of indented code, because neither
  # leaves a paragraph open. A line indented four or more columns past
  # its markers is indented code, unless it continues the open
  # paragraph or an open list of its blockquote.
  read_quote()
  # A line whose first marker or text sits left of the text of a list
  # item ends the item.
  last_item_col = item_depth ? item_cols[item_depth] : -1
  while (item_depth && (quote_first >= 0 ? quote_first : quote_indent) < item_cols[item_depth]) {
    item_depth--
  }
  # `item_empty` holds 1 after the first line of an item with no text,
  # until the next line.
  item_empty = 0
  if (!shadow_mode && quote_level > quote_para && !(quote_level in quote_lists) && quote_indent >= 4) {
    quote_para = 0
    quote_closed = 1
    quote_code = quote_code || raw ~ /^[ \t]*(```|~~~)/
    prev_blank = 0
    span_open = 0
    myst_options = 0
    next
  }
  # A list item or a block counts only within three columns of the
  # markers, because a list marker or a `#` further right can be text of
  # the open paragraph. Outside any blockquote, `item_nested` holds 1 for
  # a line within three columns of the text of the innermost open list
  # item. A list item or a block there opens inside that item, so it
  # counts too.
  item_nested = quote_level == 0 && item_depth && quote_indent < item_cols[item_depth] + 4
  # `quote_break` holds 1 for a thematic break, a line of three or more
  # `*`, `-`, or `_` marks of one kind with optional spaces or tabs
  # between them (`* * *`). CommonMark reads such a line as a break and
  # not as a list item. `quote_block` holds 1 for an ATX heading of one
  # to six `#`, a fence, an HTML comment, or a thematic break. Each of
  # these opens a block.
  quote_break = raw ~ /^[ \t]*((\*[ \t]*){3,}|(-[ \t]*){3,}|(_[ \t]*){3,})$/
  quote_block = (quote_indent < 4 || item_nested) && (quote_break || raw ~ /^[ \t]*(#{1,6}([ \t]|$)|```|~~~|<!--)/)
  # `quote_lazy` holds 1 for a line of text with fewer markers than the
  # open paragraph that is not a `quote_block` line. CommonMark reads
  # such a line as more text of that paragraph, so each blockquote of
  # the paragraph stays open. A list item there opens a block, and the
  # list-item branch below ends each deeper blockquote for it.
  quote_lazy = quote_level < quote_para && quote_indent >= 0 && !quote_block
  # A list in a blockquote ends at a line with fewer markers that is
  # not lazy. It also ends at a line of the blockquote without an
  # indent, when that line follows a `quote_closed` line or is a
  # `quote_block` line.
  for (quote_n in quote_lists) {
    if ((quote_level < quote_n + 0 && !quote_lazy) || (quote_level == quote_n + 0 && quote_indent == 0 && (quote_closed || quote_block))) {
      delete quote_lists[quote_n]
    }
  }
  # A line that opens with `<` can start an HTML block, such as
  # `<div>`. Such a block runs to the next blank line, and a list marker
  # inside it is text. `html_open` therefore holds 1 from each line
  # outside any blockquote that opens with `<` to the next blank line,
  # and no list item opens in that span. A lone HTML comment ends on its
  # own line, so it does not set `html_open`. Inline HTML and an HTML
  # block that ends before a blank line set it too, so a list item after
  # one goes untracked.
  if (quote_level == 0 && raw ~ /^[ \t]*</ && raw !~ /^[ \t]*<!--/) {
    html_open = 1
  }
  # A bare `>` line or a `quote_block` line ends the paragraph. A lazy
  # line continues it. CommonMark caps an ordered list marker at nine
  # digits, so `1234567890. x` is text. CommonMark also does not let an
  # empty list item interrupt a paragraph, and an ordered list must
  # start at 1 to interrupt one. `item_opens` holds 1 for a bullet or a
  # `1.` with text after it, which can interrupt a paragraph. Outside a
  # blockquote, a line such as `2026. It runs` or a lone `*` after a
  # line of text therefore continues the paragraph and pushes no item.
  # In a blockquote, every list marker opens a list. Such a line opens
  # the next item when the open paragraph sits in an item of a list of
  # that blockquote. It opens a new blockquote when its first `>` sits
  # left of the text of the item that holds the paragraph.
  # lib/markdown.awk tracks neither case, and a list that it opens by
  # mistake keeps the lines of the blockquote prose.
  item_opens = raw ~ /^[ \t]*([-*+]|0*1[.)])[ \t]+[^ \t]/
  if (quote_indent < 0 || quote_block) {
    quote_para = 0
  } else if ((quote_indent < 4 || item_nested) && raw ~ /^[ \t]*([-*+]|[0-9]{1,9}[.)])([ \t]|$)/) {
    # The item ends each blockquote deeper than its markers.
    quote_para = quote_level
    for (quote_n in quote_lists) {
      if (quote_n + 0 > quote_level) {
        delete quote_lists[quote_n]
      }
    }
    quote_lists[quote_level] = 1
    # Outside a blockquote, an item opens when `item_opens` holds 1,
    # after a blank line, or left of the text of the last item. No item
    # opens inside an HTML block.
    if (quote_level == 0 && !html_open && (item_opens || prev_blank || quote_indent < last_item_col)) {
      item_cols[++item_depth] = item_column($0)
      item_empty = raw ~ /^[ \t]*([-*+]|[0-9]{1,9}[.)])[ \t]*$/
    }
    # Each `>` after the list marker opens a blockquote in the item
    # (`- > text`, `- - > text`), and the paragraph sits inside the last
    # one. A list marker after a `>` opens a list in that blockquote
    # (`- > - sub`), and the lines of its item continue that list. The
    # loop reads the line with tabs expanded, as read_quote() does. A tab
    # between two `>` marks (`1. > <tab>> text`) then counts as the
    # spaces up to its tab stop.
    quote_item = blank(expand_tabs($0), "^([ \t]*>)+")
    sub(/^[ \t]*([-*+]|[0-9]{1,9}[.)])([ \t]+|$)/, "", quote_item)
    while (match(quote_item, /^ {0,3}(>[ \t]?|([-*+]|[0-9]{1,9}[.)])([ \t]+|$))/)) {
      if (index(substr(quote_item, 1, RLENGTH), ">")) {
        quote_para++
      } else {
        quote_lists[quote_para] = 1
      }
      quote_item = substr(quote_item, RLENGTH + 1)
    }
  } else if (quote_level > quote_para) {
    quote_para = quote_level
  }
  # read_quote() places a `>` only within three columns of the margin
  # or of the text of an open list item. A `>` further right can still
  # open a blockquote, such as in an item that lib/markdown.awk did not
  # track, and its paragraph can stay open. Each later line with that
  # many markers or fewer therefore stays prose until the paragraph
  # ends. A list marker after those markers opens a list in that
  # blockquote, so the lines of its item stay prose too.
  if (quote_level == 0 && match($0, /^([ \t]*>)+/)) {
    quote_item = substr($0, 1, RLENGTH)
    quote_rest = substr($0, RLENGTH + 1)
    quote_n = gsub(/>/, "", quote_item)
    if (quote_n > quote_para) {
      quote_para = quote_n
    }
    if (quote_rest ~ /^[ \t]*([-*+]|[0-9]{1,9}[.)])([ \t]|$)/) {
      quote_lists[quote_n] = 1
    }
  }
  quote_closed = quote_indent < 0
  # A skipped line of indented code in a blockquote sets `quote_code`
  # when it opens with a fence marker (`>     ~~~sh`). CommonMark ends
  # the blockquote at the next line without `>`, because code does not
  # take a lazy line. Each such line four or more columns past the
  # margin and past the text of each open list item is then indented
  # code too (`    ~~~`). `quote_code` stays 1 across those lines, and
  # across the blank lines and bare `>` lines among them. A fence marker
  # on one of those lines does not open a fence. Without the guard,
  # that marker opens a fence, and the lines after it stay skipped until
  # the next fence marker. The guard needs a fence marker in the skipped
  # code. After other code, the line can open a real fence in a list
  # item that lib/markdown.awk does not track, such as an item after a
  # line of inline HTML.
  quote_code = quote_code && (quote_indent < 0 || (quote_level == 0 && quote_indent >= 4 && !item_nested))
  if (raw ~ /^[ \t]*(```|~~~)/ && !quote_code) {
    line = raw
    sub(/^[ \t]*/, "", line)
    span_open = 0
    fence_char = substr(line, 1, 1)
    fence_len = 0
    while (substr(line, fence_len + 1, 1) == fence_char) {
      fence_len++
    }
    # A fence without an indent ends the list, as the `in_list` block
    # below explains.
    if (raw !~ /^[ \t]/) {
      in_list = 0
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
  # A list runs from its first item to the next line without an indent
  # that follows a blank line. A heading or a table row also ends it. A
  # heading opens with one to six `#` marks. CommonMark reads a line
  # with seven or more marks as text, so that line continues the list
  # item or the code span above it.
  #
  # A thematic break or a fence without an indent also ends the list,
  # with or without a blank line above it (`- An item.` above `***`).
  # CommonMark lets a line left of the text of a list item continue the
  # item only when the line holds more text of its paragraph. A break or
  # a fence opens a block instead. The fence branch above ends the list
  # at a fence. A thematic break is not an item, even with spaces
  # between its marks (`* * *`, `- - -`).
  if (raw ~ /^[ \t]*([-*+]|[0-9]+[.)])[ \t]/ && !quote_break) {
    in_list = 1
  } else if ((prev_blank || quote_break) && raw !~ /^[ \t]/) {
    in_list = 0
  } else if (raw ~ /^ {0,3}(#{1,6}([ \t]|$)|\|)/) {
    in_list = 0
  }
  # A thematic break closes the open paragraph, as a blank line does. So
  # `prev_blank` holds 1 after a `quote_block` break, and the next line
  # takes the tests of a line after a blank line. Outside a list, an
  # indented line right under the break is indented code (`***` above
  # `    make build`). In an HTML block, a line of rule marks is text of
  # the block, so it does not count. In a list item, a line under the
  # break that reaches four columns past the margin can still be
  # paragraph text of the item. So a break that `item_depth` places in
  # an item does not count either. A break without an indent has
  # already ended every item in the `item_depth` loop above, so it
  # still counts.
  prev_blank = quote_break && quote_block && !html_open && !item_depth
  # A list item, heading, table row, or HTML line starts a new block,
  # so a code span open on the line before ends there.
  if (raw ~ /^[ \t]*(([-*+]|[0-9]+[.)])[ \t]|#{1,6}([ \t]|$)|[|<])/) {
    span_open = 0
  }
  bare_line = blank(raw, "<!--([^-]|-[^-])*-->|<pre([ \t][^>]*)?>([^<]|<[^/])*</pre>") ~ /^[ \t]*$/
  prose(clean(raw))
}
