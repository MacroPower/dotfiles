# Measures runs of prose for a check. A run is the prose between two
# breaks, such as a list, a table, a code block, or a heading. A check
# file pulls this tracker in with `@include "runs.awk"`, which also
# pulls in markdown.awk, and defines two functions:
#
#   BEGIN_CHECK()  runs once at startup, after `utf8` is set
#   run(words)     runs on each run that holds at least one word
#
# run() reads the file that holds the run from `run_file`, and the line
# and character column of its first word from `run_line` and `run_col`.
#
# Blank lines between paragraphs continue a run. So do lines that the
# reader blanks to whitespace. A line that holds only a code span
# becomes one, and so does a bare `>` between two quoted paragraphs.
#
# These lines end a run:
#
# - Every non-blank line that the reader skips. Fenced code, Mermaid
#   included, indented code, a multi-line HTML comment, and a MyST
#   directive opener each leave one. A rule ahead of the include counts
#   the non-blank lines, so prose() can tell a skipped blank line from
#   skipped code.
# - A heading, written with `#` marks or underlined with `=` or `-`.
# - A list of two or more items, and a table of two or more body rows.
# - A `:::` closer, a definition-list line that opens with `: `, and a
#   Hugo shortcode line that opens with `{{`.
#
# A list or a table holds the run open until it ends, because only its
# end shows how many items or rows it has. One item or one body row
# adds structure without content, so its words join the run. HTML lines
# and horizontal rules neither end a run nor add words, for the same
# reason. A single list item, a `<br>`, or a rule therefore never
# clears a finding.
#
# Words are tokens that hold a letter. The reader blanks code spans, so
# a span counts as none. The title above a `=` or `-` underline counts
# toward the run that the underline ends.

{
  if (FNR == 1) {
    nonblank = 0
  }
  if ($0 !~ /^[ \t]*$/ && !(ENVIRON["SHADOW"] != "" && $0 == "x")) {
    nonblank++
  }
}

@include "markdown.awk"

# Counts the words in s and records the column of the first one in
# `first_col`, or 0 when s holds no word.
function count_words(s,   rest, pos, t, n) {
  rest = s
  pos = 0
  n = 0
  first_col = 0
  while (match(rest, /[^ \t]+/)) {
    t = substr(rest, RSTART, RLENGTH)
    if (t ~ /[A-Za-z]/) {
      n++
      if (!first_col) {
        first_col = pos + RSTART
      }
    }
    pos += RSTART + RLENGTH - 1
    rest = substr(rest, RSTART + RLENGTH)
  }
  return n
}

# Adds n words that start at line and col to the run, and opens the run
# at that position when it holds no word yet.
function add_words(n, file, line, col) {
  if (n == 0) {
    return
  }
  if (run_words == 0) {
    run_file = file
    run_line = line
    run_col = col
  }
  run_words += n
}

function close_run() {
  if (run_words > 0) {
    run(run_words)
  }
  run_words = 0
}

# Ends the pending list or table. A block of two or more items or body
# rows ends the run, and a smaller one adds its words to the run.
function settle() {
  if (pend_kind == "") {
    return
  }
  if (pend_count >= 2) {
    close_run()
  } else {
    add_words(pend_words, pend_file, pend_line, pend_col)
  }
  pend_kind = ""
}

# Opens a pending list or table at the current line.
function pend(kind) {
  pend_kind = kind
  pend_count = 0
  pend_words = 0
  pend_line = 0
}

# Adds the words of the current line to the pending block.
function pend_add(text,   n) {
  n = count_words(text)
  if (n > 0 && pend_words == 0) {
    pend_file = FILENAME
    pend_line = FNR
    pend_col = column(text, first_col)
  }
  pend_words += n
}

function prose(text,   n) {
  if (FILENAME != last_file) {
    settle()
    close_run()
    last_file = FILENAME
  } else if (nonblank != last_nonblank + 1) {
    # Code inside a list item makes the list a break, whatever its
    # count.
    if (pend_kind == "list" && in_list) {
      pend_count += 2
    } else {
      settle()
      close_run()
    }
  }
  last_nonblank = nonblank
  after_text = (FNR == last_line + 1) && last_was_text
  last_line = FNR
  last_was_text = 0

  # A list item, and the lines that continue it, extend a pending list.
  if (in_list && text !~ /^[ \t]*([-*_][ \t]*){3,}$/) {
    if (pend_kind != "list") {
      settle()
      pend("list")
    }
    if (text ~ /^[ \t]*([-*+]|[0-9]+[.)])([ \t]|$)/) {
      pend_count++
    }
    pend_add(text)
    return
  }

  # A table row extends a pending table. Rows count only after the
  # delimiter row under the header. Lines that open with `|` but hold no
  # delimiter row therefore count no rows.
  if (text ~ /^[ \t]*\|/) {
    if (pend_kind != "table") {
      settle()
      pend("table")
      table_delim = 0
    }
    if (!table_delim && text ~ /^[ \t]*\|?([ \t]*:?-+:?[ \t]*\|)+[ \t]*(:?-+:?[ \t]*)?$/) {
      table_delim = 1
      return
    }
    if (table_delim) {
      pend_count++
    }
    pend_add(text)
    return
  }

  settle()

  if (text ~ /^[ \t]*(#+([ \t]|$)|:::|:[ \t]|\{\{)/) {
    close_run()
    return
  }
  if (text ~ /^[ \t]*(=+|-+)[ \t]*$/ && after_text) {
    close_run()
    return
  }
  if (text ~ /^[ \t]*</ || text ~ /^[ \t]*([-*_][ \t]*){3,}$/) {
    return
  }

  n = count_words(text)
  if (n > 0) {
    add_words(n, FILENAME, FNR, column(text, first_col))
    last_was_text = 1
  }
}

END {
  settle()
  close_run()
}
