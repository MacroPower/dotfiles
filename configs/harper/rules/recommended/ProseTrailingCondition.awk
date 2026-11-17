# Overview
#
# A trailing condition is a condition that follows the step it governs
# (`Use the flag if the port is taken`). The rule reports one in
# markdown prose. The reader pictures the step before the condition says
# whether the step applies, so the technical-writing skill puts the
# condition first. The fix moves the condition to the start of the
# sentence (`If the port is taken, use the flag`). The rule sits at
# Recommended, because an `if` after a command does not always state the
# condition of that command.
#
# Expression
#
# The check includes lib/sentences.awk, which finds the sentence
# boundaries. It reads each sentence that opens with an imperative from
# the `imperative` list (`use`, `set`, `run`). An imperative is the verb
# form that gives a command, such as `run` in `run make`. The check
# reports the first `if` after the verb that sits outside parentheses.
#
# The check drops the punctuation around each word, a straight `'`
# included, so `'Run`, `"Run`, and `‘Run` each count as the imperative
# `run`. It reads a curly apostrophe inside a word as a straight one, so
# `Don’t` counts as an imperative, the same as `Don't`. prose-lint
# straightens the apostrophe in the copy that Harper reads, but this
# check reads the original markdown file or commit message.
#
# An attribution can split a quotation that opens the sentence (`"Copy
# the files," the guide says, "then restart the job if it hangs."`). The
# second part goes on with the quoted speech, so the check reads the
# words of both parts, each up to its closing quote. The check skips the
# attribution, so the last word of the first part counts as the word
# before the second part. The first word of the second part counts as
# the word after the first part. An `if` right after the opening quote
# of the second part counts too (`"if they changed."`), and so does an
# `if` after emphasis and that quote (`**"if they changed."**`).
#
# split_quotation() in lib/sentences.awk finds the second part. It opens
# at the first later word that opens with a quote of the same kind,
# single or double, as the first part. split_quotation() needs a comma
# at the end of the first part and at the end of the attribution. No
# other word of the attribution can end in a comma, a period, or another
# mark that closes a clause. The attribution also cannot hold a word
# from a list in split_quotation() of words that can join a second
# clause to it, such as `and`, `but`, `then`, `if`, `when`, or
# `because`.
#
# An `if` in the second part passes when the quotation fails one of
# these tests (`"Copy the files" the guide says "then restart the job if
# it hangs."`, `"Copy the files," the guide and the wiki say, "then
# restart the job if it hangs."`). It also passes when a word of the
# attribution opens with a quote of the same kind as the first part
# (`"Copy the files," the "Ops" guide says, "then restart the job if it
# hangs."`). split_quotation() then reads that word as the start of the
# second part, and the word before it does not end in a comma.
#
# quotation_end() in lib/sentences.awk finds the word that closes the
# second part. A second part that no word closes runs to the end of the
# sentence. In single quotes, a word with a dropped letter or a plural
# possessive can end the second part early, under the same tests that
# end the first part early. An `if` after that word passes (`‘Copy the
# files’, the guide says, ‘then keep goin’ if the build fails’.`).
#
# False Positives
#
# The check skips `if any` and `if so`, because both read worse at the
# front. It skips `if` after `as` and `even`, where `if` does not open a
# condition. It skips `if it exists`, `if they exist`, and `if there is
# one`, because each reads the same at either end. A period, a colon, or
# a comma must follow `exists` or `exist`, because later words can make
# the condition longer (`if it exists in the cache`). A closing quote,
# paren, or bracket can follow that mark, and so can emphasis (`"if they
# exist."`).
#
# An `if` after `only` passes, because the front position drops the
# restriction (`Use this option only if the file sits in a
# subdirectory`). So does an `if` after `;`, `and`, `or`, or `but`,
# because that `if` opens the next clause (`Keep the file short; if it
# grows, split it`). A clause is a subject and its verb, such as
# `the tests pass`.
#
# A sentence can open with a quotation, such as the label of a button,
# and go on after the closing quote (`"Save" appears in the menu if the
# file changed`). A period, a question mark, or an exclamation mark can
# sit before that quote when a lowercase word follows it (`"Save
# changes?" appears if you close the editor`). The imperative then sits
# inside the quotation, and the `if` after it states the condition of
# the verb after the quotation. So the check reads only the words up to
# the closing quote, which quoted_opening() in lib/sentences.awk finds.
#
# Two quoted strings read as a split quotation when a comma follows the
# first string and the words between them. Those words must also pass
# the tests of split_quotation(). An `if` in the second string then
# fires, although the words between the strings form a main clause and
# not an attribution (`"Run the build," appears in red, "Retry if busy"
# in gray`). The list in split_quotation() leaves out `that`, `which`,
# `who`, and `as`, because each of them also opens a phrase inside an
# attribution. A main clause that one of them joins therefore still
# fires (`"Run the build," appears above a button that says, "Retry if
# busy."`).
#
# In single quotes, the closing quote is the same mark as an apostrophe.
# quoted_opening() can therefore end a quotation early at a word inside
# it that ends in an apostrophe, and an `if` after that word passes. A
# word with a dropped letter ends the quotation (`‘Keep goin’ if the
# build fails’`). So does a plural possessive with a punctuation mark
# after it (`‘Copy the users’, the admins’, and the guests’ files if the
# disk fills’`). A plural possessive with no mark after it ends the
# quotation when the quotation closes at another word that ends in `s`
# and the quote (`‘Copy the users’ files if the job runs’ is the rule`).
# A quoted imperative also passes (`"Run" the build if the tests pass`).
#
# In single quotes, the quotation can also run past its closing quote.
# A word that ends in `s` and the quote can be a plural possessive, so
# quoted_opening() passes over it. The quotation ends instead at the
# first later word that ends in the quote without an `s` before it, or
# with a mark after the quote. The closing quote of each later
# quotation in single quotes ends only that quotation, so `'Save logs'
# appears in the menu if the run prints 'Done'` passes.
#
# The word that ends the quotation can be a foot mark (`12'`), a word
# with a dropped letter, a quoted value (`mode='full'`), or a plural
# possessive in a list (`users',`). An `if` before that word fires
# (`'Save logs' appears in the menu if the 12' cable fails`). The second
# part of a split quotation runs past its closing quote at the same
# words (`‘Copy the files’, the guide says, ‘then save logs’ if the 12'
# cable fails`).
#
# `unless` passes. After an imperative, it names the exception to a
# default (`Use the standard edition unless you need extra features`),
# and the technical-writing skill puts the exception after the common
# case.
#
# The check still reports an `if` that states the condition of a later
# verb (`Set the timeout so that the job fails if the server hangs`).
#
# Related Rules
#
# The Recommended ProseVagueCriterion reports `if needed`,
# `if necessary`, `if applicable`, `if appropriate`, and `if possible`,
# so the check skips them. ProseVagueCriterion matches such a phrase
# only when nothing but emphasis, a footnote reference, or the markup of
# a markdown link sits between its two words. The check skips the phrase
# in that case, so it still skips `if **needed**`, `if[^1] needed`, and
# `if [needed](https://example.com)`.
#
# The check also skips the phrase after some marks that Harper reads as
# text, so neither rule reports the `if`. A lone `*` or `_` counts as
# emphasis when no later mark closes it (`if* needed`). The check drops
# a footnote reference before it tests the marks, so it skips
# `if**[^1]** needed`, as it skips `if**** needed`. lib/markdown.awk
# blanks a bare URL, so the check reads
# `if [needed](https://example.com/a b)` as a link to `b`. The check
# also reads a title that no mark closes as a title that runs to the end
# of the sentence (`if [needed](see "Setup) now.`).
#
# The check does not read link definitions, so it skips the phrase in a
# reference link with no definition, which Harper reads as text
# (`if [needed now][ref]`). It reads a shortcut reference link, which
# holds only the text in brackets, as a plain bracket. An `if` before
# one therefore draws a finding from both rules when the file defines
# its label (`if [needed]` and `[needed]: https://example.com`).
#
# The check cannot tell a wikilink before a `(url)`
# (`if [[needed]](https://example.com)`) from a link whose text opens
# with a bracket. So neither rule reports an `if` before such a link
# (`if [[needed] now](https://example.com)`) or before the broken
# `if [[needed] now.`.
#
# An attribution between the two words stops ProseVagueCriterion, so an
# `if` at the end of the first part of a split quotation fires
# (`"Restart the agent if," the guide says, "needed."`). So does an `if`
# before a quote, a paren, a plain bracket, or a comma (`"Restart the
# agent if" needed.`, `Restart the agent if "needed".`, `Restart the
# agent if (needed).`, `Restart the agent if [needed].`, `Restart the
# agent if, needed.`). An `if` before a quote can also open a condition
# on a quoted string (`Restart the agent if "needed" appears in the
# log`).
#
# ProseIfWhether reports `if` after a verb of checking (`check if`,
# `notice if`), so the check skips that `if` too. ProseIfWhether does
# not match across an attribution either. The check still skips an `if`
# after a verb of checking at the end of the first part of a split
# quotation (`"Run the tests and see," the guide says, "if they
# pass."`). That `if` means `whether`, so it does not open a condition.
#
# ProseDoubleNegative reports `Do not ... unless`.
#
# Measurement
#
# The rule hit 21 lines in the measure.py corpus, and 19 of 21 sampled
# hits put a condition after its step.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseTrailingCondition: <message>

@include "sentences.awk"

function BEGIN_CHECK() {
  imperative = "^(add|apply|avoid|build|call|change|choose|click|combine|configure|copy|create|define|delete|deploy|disable|do|don't|download|edit|enable|follow|include|increase|install|keep|limit|leave|mark|move|open|pass|pick|place|provide|push|put|read|remove|rename|replace|restart|restrict|retry|return|run|save|see|select|send|set|skip|specify|start|stop|store|try|turn|update|upgrade|use|wait|write)$"
  vague = "^(needed|necessary|applicable|appropriate|possible)$"
  checking = "^(check|checks|checked|checking|test|tests|tested|testing|determine|determines|determined|determining|verify|verifies|verified|verifying|see|sees|seeing|decide|decides|decided|deciding|ask|asks|asked|asking|detect|detects|detected|detecting|tell|tells|telling|know|knows|notice|notices|wonder|matter|matters)$"
}

# Returns token i in lowercase without the punctuation around it. The
# strip drops a straight `'` at either end of the word, as it drops a
# double quote, so `'Run` reads as `run` and `needed'` as `needed`. No
# word that the check compares opens or ends with an apostrophe. A
# curly apostrophe inside the word then becomes a straight one, so
# `don’t` reads as `don't`. The pattern names the curly apostrophe by
# its UTF-8 bytes, so it matches under the C locale and under a UTF-8
# locale.
function word(i,   w) {
  w = tolower(tok[i])
  gsub(/^[^a-z]+|[^a-z]+$/, "", w)
  gsub(/\342\200\231/, "'", w)
  return w
}

# Returns token i up to its first `]`, in lowercase and without the
# punctuation around it. A `]` closes the text of a link, so the vague
# test in sentence() reads the text of a reference link apart from its
# label. `[needed][ref].` then reads as `needed`, and word() reads the
# same token as `needed][ref`.
function word_before_bracket(i,   w) {
  w = tok[i]
  sub(/\].*$/, "", w)
  w = tolower(w)
  gsub(/^[^a-z]+|[^a-z]+$/, "", w)
  return w
}

# Reports whether Harper reads the last word of token i and the first
# word of token i + 1 side by side. Only emphasis, a footnote reference,
# and the markup of a markdown link can sit between the two words.
# Emphasis is a run of `*`, `_`, and `~~` marks, which can close token i
# (`**restart if** needed`) or open token i + 1 (`if **needed**`).
# Harper reads a lone `~` as text, so `if ~needed~` does not pass. Every
# other mark between the words counts as text, such as a quote, a paren,
# a bracket, or a comma (`if "needed"`, `if (needed)`, `if, needed`).
#
# Harper skips a footnote reference, such as `[^1]`, so one can close
# token i next to the emphasis marks (`if[^1] needed`,
# `**restart if**[^1] needed`). The check drops each footnote reference
# before it tests the marks.
#
# The function counts the brackets after a `[` before the second word to
# find the `]` that closes it. That `[` opens a link when no `]` closes
# it, because lib/markdown.awk blanks the `](url)` of most links
# (`if [needed](https://example.com)`). It also opens a link when a `[`
# follows the `]` directly, as the label of a reference link does
# (`if [needed][ref]`, `if [needed now][ref]`, `if [needed][]`). A
# footnote reference after the `]` is not such a label, so
# `if [needed][^1]` fires. Every other `]` closes a plain bracket, which
# Harper reads as text (`if [needed now]`).
#
# A `(` right after the `]` can open a link destination that
# lib/markdown.awk leaves in place, such as a URL in angle brackets
# (`if [needed](<https://example.com/a b>)`) or a URL before a title in
# single quotes. The check reads the `(` as the start of a link only
# when the text after it follows the CommonMark grammar of an inline
# link. Every other `(` opens a plain paren, and the `]` before it
# closes a plain bracket (`if [needed](see below)`,
# `if [needed]("on" or "off")`, `if [needed](a (b) c)`).
#
# The text of an inline link holds an optional destination, an optional
# title, and the `)`, and spaces can sit between the parts. A space must
# sit between a destination and a title. A destination sits in angle
# brackets, or it does not hold a space and nests parens at most one
# level deep (`a(b)c`). A title sits in double quotes, single quotes, or
# parens.
# The check reads only the text up to the end of the sentence, which
# can fall inside a title. So it also accepts a title that no mark
# closes before that end (`if [needed](#anchor 'Read this. Then go.')`).
#
# Harper reads a `[[` as a wikilink when `]]` closes it and no `[` or
# `]` sits between (`if [[needed]]`, `if [[needed now]]`). Harper shows
# only the text after a `|`, so the check passes a wikilink only when no
# `|` sits inside it, and `if [[needed now|label]]` fires. Harper also
# reads emphasis inside a wikilink as text, so `if [[**needed**]]`
# fires. The first `[` of a `[[` also opens a link when no `]` closes it
# (`if [[needed]](https://example.com)`). lib/markdown.awk blanks the
# second `]` of such a wikilink with the `(url)` after it. Every other
# `[[` fires (`if [[needed] now]`).
function side_by_side(n, i,   e, dest, title, open_title, tail, head, wiki, rest, j, d, p, c) {
  e = "([*_]|~~)*"
  # The `(` of an inline link and its destination, a title that a mark
  # closes, and a title that runs to the end of the sentence.
  dest = "^\\([ \t]*(<[^<>]*>|([^ \t<()]|\\([^ \t()]*\\))([^ \t()]|\\([^ \t()]*\\))*)?"
  title = "[ \t]+(\"[^\"]*\"|'[^']*'|\\([^()]*\\))"
  open_title = "[ \t]+(\"[^\"]*|'[^']*|\\([^()]*)$"
  tail = tok[i]
  sub(/^.*[A-Za-z]/, "", tail)
  gsub(/\[\^[^][]*\]/, "", tail)
  head = tok[i + 1]
  sub(/[A-Za-z].*$/, "", head)
  if (tail !~ ("^" e "$")) {
    return 0
  }
  if (head ~ ("^" e "$")) {
    return 1
  }
  wiki = head ~ ("^" e "\\[\\[$")
  if (!wiki && head !~ ("^" e "\\[" e "$")) {
    return 0
  }
  rest = substr(tok[i + 1], index(tok[i + 1], "[") + 1)
  for (j = i + 2; j <= n; j++) {
    rest = rest " " tok[j]
  }
  if (wiki && rest ~ /^\[[^][|]*\]\]/) {
    return 1
  }
  d = 1
  for (p = 1; p <= length(rest); p++) {
    c = substr(rest, p, 1)
    if (c == "[") {
      d++
    } else if (c == "]" && --d == 0) {
      c = substr(rest, p + 1)
      return !wiki && ((c ~ /^\[/ && c !~ /^\[\^/) || c ~ (dest "(" title ")?[ \t]*\\)") || c ~ (dest open_title))
    }
  }
  return 1
}

function sentence(n,   i, k, m, at, depth, w, t, limited, quoted, resume, resume_end) {
  if (word(1) !~ imperative || tok[1] !~ /^[^A-Za-z]*[A-Z]/) {
    return
  }
  # A quotation that opens the sentence holds the imperative, so the
  # check reads only the words of the quotation. Its first part closes at
  # token `quoted`. When an attribution splits the quotation, its second
  # part runs from token `resume` to token `resume_end` (`"Copy the
  # files," the guide says, "then restart the job if it hangs."`). A
  # second part that no token closes runs to the end of the sentence.
  quoted = quoted_opening(n)
  resume = split_quotation(n, quoted)
  resume_end = resume ? quotation_end(n, resume) : 0
  if (resume && !resume_end) {
    resume_end = n
  }
  # at[k] holds the index of the kth token that the check reads. The
  # last word of the first part and the first word of the second part
  # then sit next to each other.
  m = 0
  for (i = 1; i <= (quoted ? quoted : n); i++) {
    at[++m] = i
  }
  for (i = resume; resume && i <= resume_end; i++) {
    at[++m] = i
  }
  # The skips read up to three tokens past the last token that the check
  # reads. gawk 5.3 aborts when the check passes word() an element of
  # `at` that does not exist and then compares that element
  # (`at[k + 1] == i + 1`). So each of those three elements points at
  # token 0.
  # lib/sentences.awk never sets token 0, so word() reads it as empty.
  at[m + 1] = at[m + 2] = at[m + 3] = 0
  depth = 0
  limited = 0
  for (k = 2; k <= m; k++) {
    i = at[k]
    t = tok[i]
    w = word(i)
    # The opening quote of the second part sits before its first word
    # (`"if`), so the test for a lowercase word reads the word after it.
    if (i == resume) {
      sub(/^[*_]*(["']|\342\200\234|\342\200\230)/, "", t)
    }
    if (w == "if" && depth == 0 && t ~ /^[a-z]/) {
      # ProseVagueCriterion matches its words only when Harper reads them
      # side by side. An attribution between the words stops the match.
      # The check therefore skips the words only when the next token that
      # it reads is the token right after the `if`. The test reads that
      # token up to its first `]`, and side_by_side() then tests the marks
      # between the two words.
      if (word(at[k + 1]) ~ /^(any|so)$/ || (at[k + 1] == i + 1 && word_before_bracket(i + 1) ~ vague && side_by_side(n, i)) || word(at[k - 1]) ~ checking || word(at[k - 1]) ~ /^(as|even|only)$/) {
        continue
      }
      if ((word(at[k + 1]) ~ /^(it|they)$/ && word(at[k + 2]) ~ /^exists?$/ && tok[at[k + 2]] ~ /[.:,]([")*_\]']|\342\200\235|\342\200\231)*$/) || (word(at[k + 1]) == "there" && word(at[k + 2]) == "is" && word(at[k + 3]) == "one")) {
        continue
      }
      if (limited || tok[at[k - 1]] ~ /;$/ || word(at[k - 1]) ~ /^(and|or|but)$/) {
        return
      }
      printf "%s:%d:%d: Style::ProseTrailingCondition: Trailing condition. Move the `%s` clause to the start of the sentence.\n", sentence_file, tok_line[i], tok_col[i], w
      return
    }
    if (w == "only") {
      limited = 1
    }
    depth += gsub(/\(/, "(", t) - gsub(/\)/, ")", t)
  }
}
