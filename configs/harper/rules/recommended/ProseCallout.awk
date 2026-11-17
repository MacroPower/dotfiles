# Overview
#
# Reports two misuses of a callout in markdown prose. A callout is a
# boxed aside such as a note, a tip, or a warning. Readers skip
# callouts, and a page that stacks them trains readers to skip all of
# them.
#
# The first misuse is an instruction inside a note (`Note: Run the
# migration first`). The technical-writing skill says a reader who skips
# every note still completes the procedure, so a note gives information
# and never a step. The fix moves the instruction into the text, or into
# a warning when skipping it causes harm. The second misuse is a callout
# that follows another with no prose between them. The fix merges the
# two callouts or moves one into the text.
#
# The rule sits at Recommended, because the check tells an instruction
# by its first word or by a phrase such as `you must`. That test can
# misread a sentence.
#
# Expression
#
# The check includes lib/markdown.awk. It knows these callout forms: a
# GitHub alert (`> [!NOTE]`), a Hugo shortcode (`{{< note >}}`), a MyST
# directive (`:::{note}`), and a label that opens a paragraph
# (`**Note:**`, `Note:`). lib/markdown.awk skips the opening line of a
# MyST directive, so the block before the `@include` line reads that
# line.
#
# A label callout runs to the end of its paragraph. A blank line, a line
# with no letter, a heading, or a list item ends it. A heading opens
# with one to six `#` marks and a space, as CommonMark reads it. A line
# that opens with seven or more marks (`####### Run the migration
# first`) or with an issue number (`#1234 first`) therefore continues
# the note.
#
# For the first misuse, the check reads the sentences of each note,
# info, or important callout. It reports a sentence that opens with a
# command verb (`Run`, `Ensure`, `Do not`) or holds `you must`, `you
# should`, `must not`, `should not`, `make sure`, or `be sure`.
#
# A sentence ends at a `.`, `?`, or `!` before a space, after any
# closing quote, paren, bracket, or emphasis. A closing quote is
# straight or curly (`"`, `'`, `”`, or `’`), and the pattern names each
# curly quote by its UTF-8 bytes, as ends_sentence() in lib/markdown.awk
# does. A quote after the mark can close a quotation inside the
# sentence, so a lowercase word after the quote continues the sentence
# (`prints "Done!" always`). continues_sentence() in lib/markdown.awk
# holds that test, and lib/sentences.awk calls it too.
#
# `e.g.`, `i.e.`, `vs.`, `cf.`, and a three-period ellipsis do not end a
# sentence, so the word after one of them does not open a sentence (`a
# directory, e.g. never the root`). ends_sentence() in lib/markdown.awk
# holds that test, and lib/sentences.awk calls it too. A note sentence
# that ends in a three-period ellipsis, quoted or not, therefore joins
# the next sentence. Command verbs that open the next sentence then pass
# (`prints Waiting... Run the cleanup`). Phrases such as `you must` in
# the next sentence still draw the finding.
#
# The check drops the punctuation around the first word, a straight `'`
# included, so `'Run`, `"Run`, and `‘Run` each count as the command verb
# `run`. The `keep in mind` guard in Related Rules skips the same
# leading punctuation. The check reads a curly apostrophe between two
# letters of the first word as a straight one. A note that opens with
# `Don’t` therefore draws the finding that `Don't` draws. prose-lint
# straightens the apostrophe in the copy that Harper reads, but this
# check reads the original markdown file or commit message.
#
# The check also skips seven or more `#` marks before the first word, so
# a continuation line such as `####### Run the migration first` holds a
# sentence that opens with `run`. A heading inside an alert, a
# shortcode, or a directive keeps its one to six marks (`> ## Run the
# Migration`). Its first word then does not hold a letter, so a command
# verb that opens the heading does not draw the finding.
#
# For the second misuse, the check reports a callout when no prose line
# sits between it and the callout before it. A line that
# lib/markdown.awk skips, such as code, also separates two callouts.
# Each finding names the line where its callout starts.
#
# False Positives
#
# Warnings and cautions can hold an instruction, because they open with
# the step they guard. `to make sure` and `to be sure` pass, because
# each states a purpose and not a step.
#
# The check tells a command verb by its spelling, so a note that opens
# with the same word as a noun still draws a finding (`Note: Check boxes
# appear only in the admin view`).
#
# Related Rules
#
# A sentence that opens with `keep in mind` passes, because the Required
# ProseFiller reports the phrase. A label matches only with a leading
# capital, so an all-caps label (`IMPORTANT:`) passes on purpose. Prompt
# files for an agent use it for emphasis, and ProseCapsEmphasis reports
# it in other files.
#
# Measurement
#
# The rule hit 155 lines in the measure.py docs corpus, and 20 of 20
# sampled hits put an instruction in a note or stacked two callouts.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseCallout: <message>

{
  if (FNR == 1) {
    nonblank = 0
    myst_line = 0
  }
  if ($0 !~ /^[ \t]*$/) {
    nonblank++
  }
  # lib/markdown.awk skips the opening line of a MyST directive, so this
  # block reads it. The line does not count inside a fence or an HTML
  # comment that an earlier line opened.
  if (fence_char == "" && !in_comment && match(tolower($0), /^[ \t>]*:::+[ \t]*\{(note|tip|important|warning|caution|attention|danger|hint|seealso)\}/)) {
    myst_kind = tolower($0)
    sub(/^[^{]*\{/, "", myst_kind)
    sub(/\}.*$/, "", myst_kind)
    myst_line = FNR
  }
}

@include "markdown.awk"

function BEGIN_CHECK() {
  imper = "^(run|use|set|add|make|ensure|do|don't|avoid|remember|check|install|create|delete|remove|configure|enable|disable|click|select|open|update|restart|verify|always|never|keep|include|specify|define|call|pass|change|replace|edit|upgrade|be)$"
  modal = "(^|[^a-z])(you must|you should|must not|should not|make sure|be sure)([^a-z]|$)"
  purpose = "(^|[^a-z])to (make|be) sure([^a-z]|$)"
  last_file = ""
}

function new_file() {
  if (kind != "") {
    end_callout()
  }
  last_file = FILENAME
  since_prose = 1
  kind = ""
  myst_seen = 0
}

function start_callout(k, line) {
  if (kind != "") {
    end_callout()
  }
  if (!since_prose) {
    printf "%s:%d:1: Style::ProseCallout: Callout. Two callouts in a row. Merge them, or move one into the text.\n", FILENAME, line
  }
  since_prose = 0
  kind = k
  body = ""
  body_file = FILENAME
  body_line = line
}

# Reports the first instruction in a note.
function end_callout(   m, pieces, seps, n, sents, i, s, t, e, w, parts, lead) {
  if (kind ~ /^(note|info|important)$/ && body ~ /[A-Za-z]/) {
    m = split(body, pieces, /[.!?](["')*_\]]|\342\200\235|\342\200\231)*[ \t]+/, seps)
    # ends_sentence() in lib/markdown.awk reads the token that ends at
    # each split, so `e.g.` and `reading...` do not end the sentence. A
    # quote after the mark can close a quotation inside the sentence
    # (`prints "Done!" always`). The piece after such a quote then
    # continues the sentence when continues_sentence() in
    # lib/markdown.awk accepts its first token.
    n = 0
    for (i = 1; i <= m; i++) {
      e = 1
      if (i > 1) {
        t = pieces[i - 1]
        sub(/^.*[ \t]/, "", t)
        s = seps[i - 1]
        sub(/[ \t]+$/, "", s)
        e = ends_sentence(t s)
        if (e == 2) {
          t = pieces[i]
          sub(/[ \t].*$/, "", t)
          e = !continues_sentence(t)
        }
      }
      if (e) {
        sents[++n] = pieces[i]
      } else {
        sents[n] = sents[n] seps[i - 1] pieces[i]
      }
    }
    for (i = 1; i <= n; i++) {
      s = sents[i]
      sub(/^([ \t*_]|#{7,}[ \t])+/, "", s)
      split(tolower(s), parts, /[ \t]+/)
      # A curly apostrophe between two letters becomes a straight one, so
      # `don’t` matches `don't` in the `imper` list. The pattern names
      # the curly apostrophe by its UTF-8 bytes, so it matches under the
      # C locale and under a UTF-8 locale. The strip then drops a
      # straight `'` at either end of the word, as it drops a double
      # quote, so `'Run` reads as `run`. No word in the `imper` list
      # opens or ends with an apostrophe.
      w = gensub(/([a-z])\342\200\231([a-z])/, "\\1'\\2", "g", parts[1])
      gsub(/^[^a-z]+|[^a-z]+$/, "", w)
      # The `keep in mind` guard skips the same leading punctuation, so
      # a note that opens with `'Keep in mind` passes as `Keep in mind`
      # does.
      lead = tolower(s)
      sub(/^[^a-z]+/, "", lead)
      if ((w ~ imper && lead !~ /^keep in mind/) || (tolower(s) ~ modal && tolower(s) !~ purpose)) {
        printf "%s:%d:1: Style::ProseCallout: Callout. A note holds an instruction, and readers skip notes. Move the instruction into the text, or into a warning when skipping it causes harm.\n", body_file, body_line
        break
      }
    }
  }
  kind = ""
  body = ""
}

function prose(text,   lower, k) {
  if (FILENAME != last_file) {
    new_file()
    last_nb = 0
  }
  # A line that lib/markdown.awk skipped, such as code, separates two
  # callouts.
  if (nonblank != last_nb + 1 && in_block !~ /^(short|myst)$/) {
    if (kind != "") {
      end_callout()
    }
    in_block = ""
    since_prose = 1
  }
  last_nb = nonblank
  if (myst_line && myst_line < FNR && !myst_seen) {
    start_callout(myst_kind, myst_line)
    myst_seen = 1
    in_block = "myst"
  }
  lower = tolower(text)
  if (match(lower, /\{\{[<%][ \t]*(note|caution|warning|tip|info|important)[ \t]*[>%]\}\}/)) {
    k = substr(lower, RSTART, RLENGTH)
    gsub(/[^a-z]/, "", k)
    start_callout(k, FNR)
    in_block = "short"
    return
  }
  if (lower ~ /\{\{[<%][ \t]*\/[ \t]*(note|caution|warning|tip|info|important)[ \t]*[>%]\}\}/) {
    end_callout()
    in_block = ""
    return
  }
  if (match(text, /\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]/)) {
    start_callout(tolower(substr(text, RSTART + 2, RLENGTH - 3)), FNR)
    in_block = "quote"
    return
  }
  if (in_block == "quote") {
    if ($0 ~ /^[ \t]*>/) {
      body = body " " text
      return
    }
    end_callout()
    in_block = ""
  }
  if (in_block == "short") {
    body = body " " text
    return
  }
  if (in_block == "myst") {
    if (text ~ /^[ \t]*:::/) {
      end_callout()
      in_block = ""
      myst_line = 0
      myst_seen = 0
      return
    }
    body = body " " text
    return
  }
  if (in_block == "label") {
    if (FNR == label_last + 1 && text ~ /[A-Za-z]/ && text !~ /^[ \t]*(#{1,6}([ \t]|$)|([-*+]|[0-9]+[.)])[ \t])/) {
      body = body " " text
      label_last = FNR
      return
    }
    end_callout()
    in_block = ""
  }
  if (match(text, /^[ \t]*(\*\*|__)?(Note|Tip|Warning|Caution|Important|Info)(:(\*\*|__)|(\*\*|__):|(\*\*|__)[ \t]|:)/)) {
    k = substr(text, RSTART, RLENGTH)
    gsub(/[^A-Za-z]/, "", k)
    start_callout(tolower(k), FNR)
    body = substr(text, RSTART + RLENGTH)
    in_block = "label"
    label_last = FNR
    return
  }
  if (text ~ /[A-Za-z]/ && text !~ /^[ \t]*(\{\{|<)/) {
    since_prose = 1
  }
}

END {
  if (kind != "") {
    end_callout()
  }
}
