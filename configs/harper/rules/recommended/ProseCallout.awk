# Reports two misuses of a callout in markdown prose. A callout is a
# boxed aside such as a note, a tip, or a warning. Readers skip
# callouts, and a page that stacks them trains readers to skip all of
# them.
#
# The first form is an instruction inside a note. The technical-writing
# skill says a reader who skips every note still completes the
# procedure, so a note gives information and never a step. The check
# reads the sentences of each note, info, or important callout. It
# reports a sentence that opens with a command verb (`Run`, `Ensure`,
# `Do not`) or holds `you must`, `you should`, `must not`, `should not`,
# `make sure`, or `be sure`. A purpose clause such as `to make sure`
# passes, and so does `keep in mind`, which ProseFiller reports.
# Warnings and cautions pass, because they open with the step they
# guard.
#
# The second form is a callout that follows another with no prose
# between them.
#
# The check knows four callout forms: a GitHub alert (`> [!NOTE]`), a
# Hugo shortcode (`{{< note >}}`), a MyST directive (`:::{note}`), and a
# label that opens a paragraph (`**Note:**`, `Note:`). A label matches
# only with a leading capital, so an all-caps label (`IMPORTANT:`)
# passes on purpose. Prompt files for an agent use it for emphasis, and
# ProseCapsEmphasis reports it in other files.
#
# It hit 155 lines in the measure.py docs corpus, and 20 of 20 sampled
# hits put an instruction in a note or stacked two callouts.
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
  # The reader skips a MyST opener line, so read it here. A fence or an
  # HTML comment open on the line before hides the line from the reader.
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
function end_callout(   n, sents, i, s, w, parts) {
  if (kind ~ /^(note|info|important)$/ && body ~ /[A-Za-z]/) {
    n = split(body, sents, /[.!?]["')*_\]]*[ \t]+/)
    for (i = 1; i <= n; i++) {
      s = sents[i]
      sub(/^[ \t*_]+/, "", s)
      split(tolower(s), parts, /[ \t]+/)
      w = parts[1]
      gsub(/[^a-z']/, "", w)
      if ((w ~ imper && tolower(s) !~ /^keep in mind/) || (tolower(s) ~ modal && tolower(s) !~ purpose)) {
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
  # A line the reader skipped, such as code, separates two callouts.
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
    if (FNR == label_last + 1 && text ~ /[A-Za-z]/ && text !~ /^[ \t]*(#|([-*+]|[0-9]+[.)])[ \t])/) {
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
