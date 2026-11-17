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
# so the check skips them. ProseVagueCriterion does not match such a
# phrase across an attribution or a closing quote. The check therefore
# skips the phrase only when neither sits between the two words. An
# `if` that ends a quotation, or the first part of a split quotation,
# then fires (`"Restart the agent if," the guide says, "needed."`,
# `"Restart the agent if" needed.`).
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
      # ProseVagueCriterion does not match across an attribution or a
      # closing quote. The check skips its words only when the next token
      # that the check reads is the token right after the `if`.
      if (word(at[k + 1]) ~ /^(any|so)$/ || (at[k + 1] == i + 1 && word(i + 1) ~ vague) || word(at[k - 1]) ~ checking || word(at[k - 1]) ~ /^(as|even|only)$/) {
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
