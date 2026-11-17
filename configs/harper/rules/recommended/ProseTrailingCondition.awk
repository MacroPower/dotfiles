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
# False Positives
#
# The check skips `if any` and `if so`, because both read worse at the
# front. It skips `if` after `as` and `even`, where `if` does not open a
# condition. It skips `if it exists`, `if they exist`, and `if there is
# one`, because each reads the same at either end.
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
# A quotation that an attribution splits passes when the `if` sits in
# its second part (`"Copy the files," the guide says, "then restart the
# job if it hangs."`).
#
# In single quotes, the quotation can also run past its closing quote.
# A word that ends in `s` and the quote can be a plural possessive, so
# quoted_opening() passes over it. The quotation ends instead at the
# first later word that ends in the quote without an `s` before it, or
# with a mark after the quote. The closing quote of each later
# quotation in single quotes ends only that quotation, so `'Save logs'
# appears in the menu if the run prints 'Done'` passes. The word that
# ends the quotation can be a foot mark (`12'`), a word with a dropped
# letter, a quoted value (`mode='full'`), or a plural possessive in a
# list (`users',`). An `if` before that word fires (`'Save logs'
# appears in the menu if the 12' cable fails`).
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
# so the check skips them. ProseIfWhether reports `if` after a verb of
# checking (`check if`, `notice if`), so the check skips that `if` too.
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
  vague = "^(needed|necessary|applicable|appropriate|possible|any|so)$"
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

function sentence(n,   i, depth, w, t, limited, last) {
  if (word(1) !~ imperative || tok[1] !~ /^[^A-Za-z]*[A-Z]/) {
    return
  }
  # A quotation that opens the sentence holds the imperative, so the
  # check reads only the words up to its closing quote.
  last = quoted_opening(n)
  if (!last) {
    last = n
  }
  depth = 0
  limited = 0
  for (i = 2; i <= last; i++) {
    t = tok[i]
    w = word(i)
    if (w == "if" && depth == 0 && t ~ /^[a-z]/) {
      if (word(i + 1) ~ vague || word(i - 1) ~ checking || word(i - 1) ~ /^(as|even|only)$/) {
        continue
      }
      if ((word(i + 1) ~ /^(it|they)$/ && word(i + 2) ~ /^exists?$/ && tok[i + 2] ~ /[.:,]$/) || (word(i + 1) == "there" && word(i + 2) == "is" && word(i + 3) == "one")) {
        continue
      }
      if (limited || tok[i - 1] ~ /;$/ || word(i - 1) ~ /^(and|or|but)$/) {
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
