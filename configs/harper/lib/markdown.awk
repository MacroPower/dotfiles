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
# A nested list item therefore still counts. In a list item, it skips
# indented code only after a heading or a table row in the item. It
# skips a MyST directive's opening fence and the `:name: value` option
# lines under it, because ProseEmoji's shortcode pattern matches an
# option such as `:class: tip`.
#
# lib/markdown.awk tracks the open list items outside a blockquote as
# GFM does. A line that sits left of the text of an item ends the item,
# unless the line is a lazy line, more text of the paragraph of the item
# (`- The pod runs,` above `and more.`). A list marker that cannot
# interrupt the open paragraph does not open an item (`The pod runs`
# above `2026. Then it stops.`). Before each call to prose(),
# lib/markdown.awk sets `in_list` to 1 when an item stays open or a
# blockquote holds an open list, and to 0 otherwise.
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
# side of a line of code stay apart. It blanks the comment marker to
# spaces, so the columns stay put. The marker is `//`, `///`, `//!`, a
# run of `#` marks, or a run of two or more `-` marks. The shadow mode
# drops the ` x` that the shadow adds after a line that ends in
# `-`, `#`, `/`, `*`, or `!`. It also reads indented code as prose, in
# a blockquote too, because comment text is often indented under its
# marker.
#
# The shadow mode also blanks each inline tag of Javadoc and JSDoc. A
# tag runs from `{@` and a letter to the next `}` on its line, such as
# `{@code a/b}` or `{@link Map}`. Harper reads such a tag as code in a
# Java, JavaScript, or TypeScript comment, so a check does not report
# the words in it either. A tag that wraps onto the next line stays
# prose there.
#
# Before each call to prose() in that mode, lib/markdown.awk sets
# `tag_line` to 1 when the comment text opens with a block tag, an `@`
# and a letter (`@param`, `@return`). It sets `tag_line` to 0 on every
# other line. Javadoc, JSDoc, and Doxygen end a description at each
# block tag. A check that splits paragraphs therefore ends one before a
# tag line, so a run of tags does not read as one long sentence. With
# HADDOCK=1 in the environment, the shadow comes from a Haskell file,
# and `tag_line` stays 0. prose-lint writes the inline code of a
# Haddock doc comment as a code span. It keeps the `@` marks of a plain
# comment, such as a type application (`read @Int`), and each `@` that
# no later `@` of its paragraph closes. Haskell comments do not use
# block tags such as `@param`, so a sentence can wrap onto a line that
# opens with such an `@`.
#
# In that mode, BLOCKS in the environment can name the blocks file that
# prose-lint writes for the shadow. Each line of the file opens with the
# number of a shadow line that belongs to a block comment, and its last
# field is 1 on the line that holds the `*/`. lib/markdown.awk then sets
# `edge_line` to 1 on the first line of each block comment, and on a
# comment line right under a line that holds a `*/`. It sets
# `edge_line` to 0 on every other line. Such a line opens a comment of
# its own in the source, so a check that splits paragraphs ends one
# before it.

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
  quote_loose = 0
  quote_closed = 0
  quote_code = 0
  item_depth = 0
  item_empty = 0
  item_block = 0
  block_para = 0
  list_code = 0
  table_open = 0
  cells_now = 0
  foot_open = 0
  html_open = 0
  tag_line = 0
  html_level = 0
  html_stop = ""
  html_shut = 0
  block_text = 0
  # `html_start` holds the starts of the HTML blocks of types 1, 2, 3, 5,
  # and 6, which html_block() reads in lowercase. `html_tags` holds the
  # tags of type 6. cmark-gfm, the parser that GitHub uses, lists these
  # tags. html_block() tests type 4 on its own, because type 4 needs a
  # capital letter after `<!`.
  html_tags = "address|article|aside|base|basefont|blockquote|body|caption|center|col|colgroup|dd|details|dialog|dir|div|dl|dt|fieldset|figcaption|figure|footer|form|frame|frameset|h[1-6]|head|header|hr|html|iframe|legend|li|link|main|menu|menuitem|nav|noframes|ol|optgroup|option|p|param|section|source|summary|table|tbody|td|tfoot|th|thead|title|tr|track|ul"
  html_start = "^[ \t]*<((script|pre|textarea|style)([ \t>]|$)|!--|[?]|!\\[cdata\\[|/?(" html_tags ")([ \t>]|/>|$))"
  # `html_lone` matches a line in lowercase that holds one complete open
  # or closing tag and only whitespace after it (`<br>`, `<a href="x">`,
  # `</span>`). Such a line can open an HTML block of type 7.
  html_lone = "^[ \t]*(<[a-z][a-z0-9-]*([ \t]+[a-z_:][a-z0-9_.:-]*([ \t]*=[ \t]*([^ \t\"'=<>`]+|'[^']*'|\"[^\"]*\"))?)*[ \t]*/?>|</[a-z][a-z0-9-]*[ \t]*>)[ \t]*$"
  raw_mode = ENVIRON["RAW"] != ""
  shadow_mode = ENVIRON["SHADOW"] != ""
  haddock = ENVIRON["HADDOCK"] != ""
  edge_line = 0
  # `comment_block` holds each shadow line of a block comment, and the
  # value 1 marks the line that holds the `*/`.
  if (shadow_mode && ENVIRON["BLOCKS"] != "") {
    while ((getline block_record < ENVIRON["BLOCKS"]) > 0) {
      split(block_record, block_fields, " ")
      comment_block[block_fields[1]] = block_fields[4]
    }
    close(ENVIRON["BLOCKS"])
  }
  # The destination of an inline link with the `]` before it, which
  # clean() blanks (`](https://example.com)`). A check that reads the
  # text that clean() blanked can find a link destination in it with
  # this pattern.
  link_dest = "\\]\\([^) \t]*([ \t]+\"[^\"]*\")?\\)"
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
  tag_line = 0
  edge_line = 0
  if ($0 == "x") {
    $0 = ""
  } else if (match($0, /^[ \t]*(\/\/[\/!]?|#+|-{2,})/)) {
    $0 = spaces(RLENGTH) substr($0, RLENGTH + 1)
    if ($0 ~ /[-#\/*!][ \t]* x$/) {
      $0 = substr($0, 1, length($0) - 2)
    }
    tag_line = !haddock && $0 ~ /^[ \t]*@[A-Za-z]/
    edge_line = (FNR in comment_block) != ((FNR - 1) in comment_block) || ((FNR - 1) in comment_block && comment_block[FNR - 1])
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
  if (shadow_mode) {
    s = blank(s, "[{]@[A-Za-z][^}]*[}]")
  }
  s = blank(s, "<!--([^-]|-[^-])*-->")
  s = blank(s, "<(code|kbd|samp|pre)[^>]*>([^<]|<[^/])*</(code|kbd|samp|pre)>")
  s = blank(s, "<[A-Za-z][A-Za-z0-9+.-]*:[^ \t<>]*>")
  s = blank(s, link_dest)
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

# Returns the column of the first character of the line that is not a
# space or a tab, with tabs expanded.
function text_column() {
  match($0, /^[ \t]*/)
  return length(expand_tabs(substr($0, 1, RLENGTH)))
}

# Returns the number of cells in the table row s, as GFM counts them. A
# `|` after a backslash is text of its cell. A `|` that opens the row
# does not end a cell. The text after the last `|` counts as a cell only
# when it is not blank.
function cells(s,   n) {
  gsub(/\\\|/, "", s)
  sub(/^[ \t]*\|/, "", s)
  n = gsub(/\|/, "|", s)
  sub(/^.*\|/, "", s)
  return n + (s ~ /[^ \t]/)
}

# Reports whether the line opens an HTML block of types 1 to 6 (`<div>`,
# `</td>`, `<pre class="x">`, `<!--`). Such a block can interrupt a
# paragraph. A line that opens with any other tag holds inline HTML or
# opens a block of type 7, and neither one interrupts a paragraph
# (`<span>x</span>`). The tag names ignore case. Type 4 needs a capital
# letter after `<!`, as in cmark-gfm (`<!DOCTYPE html>`).
function html_block(s) {
  return s ~ /^[ \t]*<![A-Z]/ || tolower(s) ~ html_start
}

# Returns a pattern for the end marker of the HTML block that the line
# opens, which matches a line in lowercase. GFM ends a block of type 1
# (`<pre>`, `<script>`, `<style>`, `<textarea>`) at the line that holds
# the closing tag of any of these four. It ends a block of type 2
# (`<!--`) at `-->`, a block of type 3 (`<?`) at `?>`, a block of type 4
# (`<!DOCTYPE`) at `>`, and a block of type 5 (`<![CDATA[`) at `]]>`.
# The opening line can hold the end marker too (`<pre>x</pre>`). The
# function returns an empty string for a block of type 6 or 7, which
# ends at a blank line. The main rule skips an HTML comment with a rule
# of its own, so it reads the pattern only for the other types.
function html_end_of(s,   t) {
  t = tolower(s)
  if (t ~ /^[ \t]*<(script|pre|textarea|style)([ \t>]|$)/) {
    return "</(script|pre|textarea|style)>"
  }
  if (t ~ /^[ \t]*<!--/) {
    return "-->"
  }
  if (t ~ /^[ \t]*<[?]/) {
    return "[?]>"
  }
  if (t ~ /^[ \t]*<!\[cdata\[/) {
    return "]]>"
  }
  if (s ~ /^[ \t]*<![A-Z]/) {
    return ">"
  }
  return ""
}

# Ends each open list item whose text sits right of the line. Fenced
# code and an HTML comment do not take a lazy line. So GFM ends an item
# at each line of either block that sits left of the text of the item.
# The rules for fenced code and for comments call this function, because
# they skip each line before the main rule reads it. A blank line keeps
# every item open. lib/markdown.awk keeps the fence or the comment open
# up to its closing marker.
#
# lib/markdown.awk opens a fence or a comment at any indent. Under an
# open paragraph, GFM reads some fence markers and comments as more text
# of that paragraph. Each of them sits four or more columns past the
# text of the innermost open item, or past the margin. `block_text`
# holds 1 for such a block.
# The paragraph takes lazy lines, so the function keeps every item open.
# A blank line in the block ends the paragraph, and the function then
# clears `block_text`. A blank line also ends the HTML block that
# `html_open` marks, as it does outside a fence or a comment. So does
# the end of the item that holds the block.
function leave_items(   c) {
  if ($0 ~ /^[ \t]*$/) {
    block_text = 0
    html_open = 0
    return
  }
  if (block_text) {
    return
  }
  c = text_column()
  while (item_depth && c < item_cols[item_depth]) {
    item_depth--
    html_open = 0
  }
}

fence_char != "" {
  leave_items()
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
  leave_items()
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
  # A blank line also ends the paragraph and the table that the main
  # rule below tracks.
  block_para = 0
  table_open = 0
  foot_open = 0
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

# After a blank line, a line left of the text of every open list item
# ends the list. No paragraph stays open across a blank line, so the
# line is not a lazy line. A line there four or more columns in is
# therefore indented code, and so is each line of that code.
!shadow_mode && /^(    |\t)/ && (in_indent || (prev_blank && (!item_depth || text_column() < item_cols[1]))) {
  item_depth = 0
  item_block = 0
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
  # item ends the item. `line_col` holds the column of that marker or
  # that text.
  line_col = quote_first >= 0 ? quote_first : quote_indent
  # `quote_break` holds 1 for a thematic break, a line of three or more
  # `*`, `-`, or `_` marks of one kind with optional spaces or tabs
  # between them (`* * *`). CommonMark reads such a line as a break and
  # not as a list item.
  quote_break = raw ~ /^[ \t]*((\*[ \t]*){3,}|(-[ \t]*){3,}|(_[ \t]*){3,})$/
  # `para_above` holds 1 when the line above leaves a paragraph open, and
  # `table_above` holds 1 when it leaves a table open. `block_para` and
  # `table_open` hold the same for the current line. Each branch below
  # that skips the rest of the block leaves neither open, except the
  # branches for a MyST directive and a link reference definition.
  para_above = block_para
  block_para = 0
  table_above = table_open
  table_open = 0
  code_above = list_code
  list_code = 0
  # `cells_above` holds the number of cells of the line above, which the
  # delimiter-row test below reads. The list-item branch counts the
  # cells of a line that opens an item from the text after its markers.
  cells_above = cells_now
  cells_now = cells(raw)
  # `foot_above` holds 1 when the line above leaves the paragraph of a
  # footnote definition open (`[^1]: text`). `foot_open` holds the same
  # for the current line.
  foot_above = foot_open
  foot_open = 0
  # `html_level` holds the number of `>` markers of the blockquote that
  # holds the open HTML block, or 0 outside a blockquote. A line with
  # fewer markers ends that blockquote, because an HTML block does not
  # take a lazy line. So does a line of only `>` marks, which is a blank
  # line of the blockquote. No paragraph stays open there, so
  # `quote_para` drops to 0.
  #
  # `html_stop` holds the pattern that html_end_of() returns for the open
  # block. `html_shut` holds 1 after the line that holds that end marker.
  # That line is the last line of the block, so the block ends at the
  # next line, and no paragraph stays open there either.
  if (html_open && (html_shut || (html_level && (quote_level < html_level || quote_indent < 0)))) {
    html_open = 0
    quote_para = 0
  }
  html_shut = html_open && html_stop != "" && tolower(raw) ~ html_stop
  # As in GFM, a line ends each open list item whose text sits right of
  # `line_col`. `item_match` counts the items that stay open, and
  # `item_rel` holds the columns between the text of the innermost of
  # them, or the margin, and `line_col`.
  #
  # A lazy line keeps every item open (`- The pod runs,` above `and
  # more.`). A lazy line continues the open paragraph. It does not open
  # a block within three columns of the text of the innermost item that
  # stays open, or of the margin. A `>` marker, a heading, a fence, a
  # list item, a thematic break, and a footnote definition each open a
  # block. So do an HTML block and a line that holds only one tag
  # (`<br>`), which opens an HTML block of type 7. A line of text, a
  # delimiter row, a link reference definition, and a line of inline
  # HTML continue the paragraph instead.
  #
  # `item_left` holds 1 for a line that ends an item. The HTML block or
  # the table that the item holds ends with it.
  item_match = item_depth
  while (item_match && line_col < item_cols[item_match]) {
    item_match--
  }
  item_rel = line_col - (item_match ? item_cols[item_match] : 0)
  item_lazy = para_above && item_match < item_depth && !(item_rel < 4 && (quote_level || quote_break || raw ~ /^[ \t]*(#{1,6}([ \t]|$)|```|~~~|([-*+]|[0-9]{1,9}[.)])([ \t]|$)|\[\^[^] \t]+\]:)/ || html_block(raw) || tolower(raw) ~ html_lone))
  item_left = !item_lazy && item_match < item_depth
  if (item_left) {
    item_depth = item_match
    html_open = 0
    table_above = 0
  }
  if (!item_depth) {
    item_block = 0
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
  # counts too. A lazy line does not open a block, so it never sets
  # `item_nested`.
  item_nested = quote_level == 0 && !item_lazy && item_depth && quote_indent < item_cols[item_depth] + 4
  # `quote_block` holds 1 for an ATX heading of one to six `#`, a fence,
  # an HTML comment, or a thematic break (`quote_break`). Each of these
  # opens a block.
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
  # `item_code` holds 1 for indented code, a line four or more columns
  # past the text of the innermost open list item, or past the margin
  # outside a list, that does not continue a paragraph. In an HTML block,
  # such a line is text of the block.
  #
  # After a heading or a table row in the item, `item_block` holds 1.
  # lib/markdown.awk then skips indented code in the item, as it skips
  # indented code outside a list. A fence marker or a list marker on such
  # a line therefore does not open a block. The code must open after a
  # blank line, and each line of it under that line is code too.
  # `code_above` holds 1 when the line above is such code.
  # lib/markdown.awk can misread a block above the code without a blank
  # line between, such as a fence that GFM reads as more text of a
  # paragraph. So it does not skip that code.
  item_code = !para_above && quote_level == 0 && !html_open && quote_indent >= (item_depth ? item_cols[item_depth] : 0) + 4
  if (!shadow_mode && item_block && item_code && (prev_blank || code_above)) {
    list_code = 1
    quote_para = 0
    quote_closed = 1
    prev_blank = 0
    span_open = 0
    myst_options = 0
    next
  }
  # `item_free` holds 1 for a line that interrupts no paragraph. No
  # paragraph is open above it, the line ends the list item that holds
  # the paragraph, or the paragraph sits in a blockquote that the line
  # leaves. The blockquote case does not count a `>` that GFM reads as
  # text of the paragraph, which `quote_loose` marks. A footnote
  # definition holds its paragraph as a list item does. So a line that
  # opens a block leaves the definition, and it interrupts no paragraph
  # (`[^1]: The note.` above `2. Then it stops.`). The line must sit
  # within three columns of the margin, or of the text of the list item
  # that holds the definition, as the tests that read `item_free` check.
  #
  # A line that opens an HTML block, such as `<div>`, sets `html_open`,
  # and so does the first line of a list item that opens one after its
  # markers (`- <div>`). A list marker inside the block is text, so no
  # list item opens there.
  # A line that holds only one tag opens a block of type 7 (`<br>`), but
  # only where `item_free` holds 1. A line of inline HTML does not open
  # a block (`<span>x</span>`). A lone HTML comment ends on its own
  # line, so it does not set `html_open`. A line inside an open block is
  # text of the block, so it does not open another one.
  #
  # A block of types 1 to 5 ends after the line that holds its end
  # marker, which can be the opening line (`<pre>x</pre>`).
  # lib/markdown.awk also ends every block at the next blank line. GFM
  # keeps a block of types 1 to 5 open across a blank line instead, up
  # to its end marker.
  item_free = !para_above || item_left || (quote_para > 0 && !quote_loose) || foot_above
  if (!html_open && quote_level == 0 && (quote_indent < 4 || item_nested) && raw !~ /^[ \t]*<!--/ && (html_block(raw) || (item_free && tolower(raw) ~ html_lone))) {
    html_open = 1
    html_level = 0
    html_stop = html_end_of(raw)
    html_shut = html_stop != "" && tolower(raw) ~ html_stop
  }
  # A bare `>` line or a `quote_block` line ends the paragraph. A lazy
  # line continues it. CommonMark caps an ordered list marker at nine
  # digits, so `1234567890. x` is text. CommonMark also does not let an
  # empty list item interrupt a paragraph, and an ordered list must
  # start at 1 to interrupt one. `item_opens` holds 1 for a bullet or a
  # `1.` with text after it, which can interrupt a paragraph. Outside a
  # blockquote, a line such as `2026. It runs` or a lone `*` after a
  # line of text therefore continues the paragraph and pushes no item.
  # Where `item_free` holds 1, every list marker opens an item, such as
  # `2.` right under a heading.
  # In a blockquote, every list marker opens a list. Such a line opens
  # the next item when the open paragraph sits in an item of a list of
  # that blockquote. It opens a new blockquote when its first `>` sits
  # left of the text of the item that holds the paragraph.
  # lib/markdown.awk tracks neither case, and a list that it opens by
  # mistake keeps the lines of the blockquote prose.
  item_opens = raw ~ /^[ \t]*([-*+]|0*1[.)])[ \t]+[^ \t]/
  item_pushed = 0
  item_head = 0
  if (quote_indent < 0 || quote_block) {
    quote_para = 0
  } else if ((quote_indent < 4 || item_nested) && raw ~ /^[ \t]*([-*+]|[0-9]{1,9}[.)])([ \t]|$)/) {
    # Outside a blockquote, an item opens when `item_opens` holds 1 or
    # when `item_free` holds 1. No item opens inside an HTML block.
    # `item_pushed` holds 1 for a line that opens an item.
    if (quote_level == 0 && !html_open && (item_opens || item_free)) {
      item_cols[++item_depth] = item_column($0)
      item_empty = raw ~ /^[ \t]*([-*+]|[0-9]{1,9}[.)])[ \t]*$/
      item_pushed = 1
    }
    # The item ends each blockquote deeper than its markers. When the
    # line does not open an item, a `>` after its markers is text of the
    # paragraph (`2. > make build` under a line of text). So the line
    # sets `quote_loose`.
    quote_para = quote_level
    quote_loose = !item_pushed
    for (quote_n in quote_lists) {
      if (quote_n + 0 > quote_level) {
        delete quote_lists[quote_n]
      }
    }
    quote_lists[quote_level] = 1
    # Each `>` after the list marker opens a blockquote in the item
    # (`- > text`, `- - > text`), and the paragraph sits inside the last
    # one. A list marker after a `>` opens a list in that blockquote
    # (`- > - sub`), and the lines of its item continue that list. The
    # loop reads the line with tabs expanded, as read_quote() does. A tab
    # between two `>` marks (`1. > <tab>> text`) then counts as the
    # spaces up to its tab stop.
    #
    # Outside a blockquote, a list marker right after the marker of an
    # item that the line opens, before any `>`, opens an item inside that
    # item (`- - The pod runs.`). The loop pushes that item too, unless
    # the item above it holds indented code. A thematic break after the
    # markers ends the loop (`- * * *`), because GFM reads the break as a
    # block of the item and not as more list markers.
    quote_item = blank(expand_tabs($0), "^([ \t]*>)+")
    item_width = length(quote_item)
    match(quote_item, /^ *([-*+]|[0-9]{1,9}[.)]) */)
    item_head = item_pushed && RLENGTH >= item_cols[item_depth] + 4
    sub(/^[ \t]*([-*+]|[0-9]{1,9}[.)])([ \t]+|$)/, "", quote_item)
    while (match(quote_item, /^ {0,3}(>[ \t]?|([-*+]|[0-9]{1,9}[.)])([ \t]+|$))/)) {
      item_skip = RLENGTH
      if (index(substr(quote_item, 1, item_skip), ">")) {
        quote_para++
      } else if (quote_item ~ /^ {0,3}((\*[ \t]*){3,}|(-[ \t]*){3,})$/) {
        break
      } else {
        quote_lists[quote_para] = 1
        if (item_pushed && !item_head && !quote_para) {
          item_at = item_width - length(quote_item)
          item_cols[++item_depth] = item_column(spaces(item_at) quote_item)
          item_empty = item_skip == length(quote_item)
          match(quote_item, /^ *([-*+]|[0-9]{1,9}[.)]) */)
          item_head = !item_empty && item_at + RLENGTH >= item_cols[item_depth] + 4
        }
      }
      quote_item = substr(quote_item, item_skip + 1)
    }
    # The text after the markers can be the header row of a table in the
    # item (`- | a | b |`). The markers do not count as a cell there.
    if (item_pushed) {
      cells_now = cells(quote_item)
    }
    # GFM does not open a paragraph in an item whose first line holds
    # indented code after its marker (`-     make build`) or after a `>`
    # of the item (`- >     make build`). Nor does it open one when the
    # line holds only markers, or when a heading, a thematic break, a
    # fence, an HTML block, or only one tag follows them (`- # Setup`,
    # `- ***`, `- ```sh`, `- <!-- note -->`, `- <br>`).
    #
    # `item_html` holds 1 for the HTML block or the tag. Unless the block
    # is a comment, it sets `html_open`, so the lines of the item under
    # it are text of the block, up to the end marker of the block
    # (`- <pre>x</pre>`). A block in a blockquote of the item
    # (`- > <div>`) also sets `html_level`, so the next line without a
    # `>` ends the block.
    item_html = !item_head && item_pushed && quote_item ~ /^ {0,3}</ && (html_block(quote_item) || tolower(quote_item) ~ html_lone)
    item_head = item_head || item_html || item_pushed && quote_item ~ /^[ \t]*$|^ {4}|^ {0,3}(#{1,6}([ \t]|$)|```|~~~|((\*[ \t]*){3,}|(-[ \t]*){3,}|(_[ \t]*){3,})$)/
    if (item_html && quote_item !~ /^ {0,3}<!--/) {
      html_open = 1
      html_level = quote_para
      html_stop = html_end_of(quote_item)
      html_shut = html_stop != "" && tolower(quote_item) ~ html_stop
    }
    # A line that `item_head` marks does not open a paragraph in a
    # blockquote of the item either (`- > # Setup`).
    if (item_head) {
      quote_para = 0
    }
  } else if (quote_level > quote_para) {
    quote_para = quote_level
  }
  # read_quote() places a `>` only within three columns of the margin
  # or of the text of an open list item. A `>` further right can still
  # open a blockquote in a list item that lib/markdown.awk does not
  # track, and its paragraph can stay open. Each later line with that
  # many markers or fewer therefore stays prose until the paragraph
  # ends. A list marker after those markers opens a list in that
  # blockquote, so the lines of its item stay prose too.
  #
  # Under a paragraph, GFM reads such a `>` as text of the paragraph
  # (`    > make build`). When no blockquote holds the paragraph, the
  # count that this block raises therefore sets `quote_loose`. Under a
  # paragraph in a blockquote, the line is a lazy line of that
  # paragraph, and the blockquote still holds it (`> A quote,` above
  # `    >> more`). The block then leaves the flag as it is. A line in a
  # blockquote clears the flag, because GFM reads the `>` at the start
  # of that line as a blockquote.
  if (quote_level > 0) {
    quote_loose = 0
  }
  if (quote_level == 0 && match($0, /^([ \t]*>)+/)) {
    quote_item = substr($0, 1, RLENGTH)
    quote_rest = substr($0, RLENGTH + 1)
    quote_n = gsub(/>/, "", quote_item)
    if (quote_n > quote_para) {
      if (!quote_para) {
        quote_loose = 1
      }
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
  # item that lib/markdown.awk does not track.
  quote_code = quote_code && (quote_indent < 0 || (quote_level == 0 && quote_indent >= 4 && !item_nested))
  if (raw ~ /^[ \t]*(```|~~~)/ && !quote_code) {
    line = raw
    sub(/^[ \t]*/, "", line)
    span_open = 0
    # leave_items() describes `block_text`.
    block_text = para_above && !(quote_indent < 4 || item_nested)
    fence_char = substr(line, 1, 1)
    fence_len = 0
    while (substr(line, fence_len + 1, 1) == fence_char) {
      fence_len++
    }
    prev_blank = 0
    next
  }
  # GFM reads a line of a MyST directive as text of a paragraph. In a
  # table, it reads the line as a row of the table, and in an HTML
  # block as text of the block. The next line reads `block_para`,
  # `table_open`, and `foot_open`.
  if (raw ~ /^[ \t]*:::+[ \t]*\{[^}]*\}/) {
    myst_options = 1
    prev_blank = 0
    block_para = !table_above && !html_open
    table_open = table_above
    foot_open = foot_above
    next
  }
  if (myst_options && raw ~ /^[ \t]*:[A-Za-z][A-Za-z0-9_-]*:([ \t]|$)/) {
    prev_blank = 0
    block_para = para_above
    table_open = table_above
    foot_open = foot_above
    next
  }
  myst_options = 0
  # A link reference definition (`[guide]: https://example.com`) holds
  # a label and a URL, not prose. A footnote (`[^1]: text`) stays.
  # CommonMark reads the definition as text of a paragraph until that
  # paragraph ends, so a line under it can continue the paragraph. In a
  # table, the line is a row of the table instead, and in an HTML block
  # it is text of the block.
  if (raw ~ /^ {0,3}\[[^]^][^]]*\]:[ \t]*[^ \t]+[ \t]*("[^"]*")?[ \t]*$/) {
    prev_blank = 0
    block_para = !table_above && !html_open
    table_open = table_above
    foot_open = foot_above
    next
  }
  if (raw ~ /^[ \t]*<!--/ && raw !~ /-->/) {
    block_text = para_above && !(quote_indent < 4 || item_nested)
    in_comment = 1
    next
  }
  # `item_block` holds 1 after a heading or a table row zero to three
  # columns from the margin in an open list item, up to the next list
  # item with text (`- An item.`, a blank line, `  ## Setup`). An empty
  # list item keeps it. The `item_code` test above then skips indented
  # code in the item. A later paragraph of the item stays prose when it
  # sits fewer than four columns past the text of the item
  # (`    It runs.`).
  #
  # A heading opens with one to six `#` marks. CommonMark reads a line
  # with seven or more marks as text, so that line continues the list
  # item or the code span above it.
  block_line = raw ~ /^ {0,3}(#{1,6}([ \t]|$)|\|)/
  if (item_pushed && !item_empty) {
    item_block = 0
  } else if (block_line && item_depth) {
    item_block = 1
  }
  # `table_open` holds 1 from the delimiter row of a table (`| - | - |`
  # under a line of text) up to the next block. A blank line, a heading,
  # a thematic break, a fence, an HTML block, and a list item each end
  # the table. So do indented code (`item_code`) and a line with other
  # blockquote markers. A line without a `|` in that span (`  more`) is a
  # row of the table, not a paragraph. A `|` line with no delimiter row
  # under it is text of a paragraph. A table does not open on a lazy
  # line. So a delimiter row left of the text of the list item that holds
  # the paragraph is more text of the paragraph (`  It runs | daily` over
  # `--|--`). So is a delimiter row with fewer blockquote markers than
  # the paragraph. GFM opens a table only when the delimiter row holds
  # as many cells as the line above it, its header row. So a row with
  # another number of cells is more text of the paragraph too
  # (`It runs | daily` over `| - | - | - |`). GFM reads a list item
  # before a table. So a line whose text opens with a `-` and whitespace
  # opens a list item and is not a delimiter row (`  - | - |` under
  # `- The job runs.`). `raw` holds spaces in place of the `>` markers,
  # so the test also covers a quoted line.
  if (para_above && !item_lazy && !quote_lazy && (quote_indent < 4 || item_nested) && raw ~ /\|/ && raw !~ /^[ \t]*-[ \t]/ && raw ~ /^[ \t]*\|?[ \t]*:?-+:?[ \t]*(\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$/ && cells_now == cells_above) {
    table_open = 1
    table_level = quote_level
  } else {
    table_open = table_above && !quote_block && !item_code && !item_pushed && !html_open && quote_level == table_level
  }
  # `para_end` holds 1 for an empty list item and for a setext underline
  # under a paragraph. GFM reads a lone `*` or `1.` at the text of the
  # item under a paragraph as more text of the paragraph. It also reads a
  # line of `=` or `-` marks as more text when the line is a lazy line or
  # sits four or more columns past the text of its block. The same holds
  # for a line that leaves the blockquote of the paragraph (`  > A
  # quote.` above `  ===`).
  para_end = (raw ~ /^[ \t]*([-*+]|[0-9]{1,9}[.)])[ \t]*$/ && (!para_above || item_left || quote_lazy)) || (para_above && !item_lazy && !quote_lazy && (quote_indent < 4 || item_nested) && raw ~ /^[ \t]*(=+|-+)[ \t]*$/)
  # `block_para` holds 1 for a line that leaves a paragraph open. A
  # heading, a table, a thematic break, and a setext underline leave none
  # open. A fence, an HTML block or comment, a line of only `>`, an empty
  # list item, and indented code leave none open either. Nor does a list
  # item that `item_head` marks.
  block_para = quote_indent >= 0 && !quote_block && !html_open && !table_open && !item_code && !para_end && !item_head
  # A footnote definition opens a paragraph, and it can interrupt an
  # open paragraph. The later lines of the new paragraph keep
  # `foot_open` at 1, lazy lines included. A line that opens a list item
  # leaves the definition.
  foot_open = block_para && !item_pushed && (((quote_indent < 4 || item_nested) && raw ~ /^[ \t]*\[\^[^] \t]+\]:/) || foot_above)
  # A thematic break closes the open paragraph, as a blank line does. So
  # `prev_blank` holds 1 after a `quote_block` break, and the next line
  # takes the tests of a line after a blank line. Outside a list, an
  # indented line right under the break is indented code (`***` above
  # `    make build`). In an HTML block, a line of rule marks is text of
  # the block, so it does not count. In a list item, a line under the
  # break that reaches four columns past the margin can still be
  # paragraph text of the item. So a break that `item_depth` places in
  # an item does not count either. A break left of the text of every
  # open item has already ended the list, so it still counts.
  prev_blank = quote_break && quote_block && !html_open && !item_depth
  # A list item, heading, table row, or HTML line starts a new block,
  # so a code span open on the line before ends there.
  if (raw ~ /^[ \t]*(([-*+]|[0-9]+[.)])[ \t]|#{1,6}([ \t]|$)|[|<])/) {
    span_open = 0
  }
  # `in_list` holds 1 when a list item outside a blockquote stays open,
  # or when a blockquote holds an open list.
  in_list = item_depth > 0
  for (quote_n in quote_lists) {
    if (quote_n + 0 > 0) {
      in_list = 1
    }
  }
  bare_line = blank(raw, "<!--([^-]|-[^-])*-->|<pre([ \t][^>]*)?>([^<]|<[^/])*</pre>") ~ /^[ \t]*$/
  prose(clean(raw))
}
