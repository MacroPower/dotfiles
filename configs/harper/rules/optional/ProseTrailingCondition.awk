# Reports a step whose condition trails it in markdown prose (`Use the
# flag if the port is taken`). The reader pictures the step before the
# condition says whether it applies, so the technical-writing skill
# puts the condition first (`If the port is taken, use the flag`).
#
# The check reads each sentence that opens with an imperative, the verb
# form that gives a command, from a list (`use`, `set`, `run`). It
# reports the first `if` or `unless` after the verb that sits outside
# parentheses. It skips `if needed`, `if necessary`, `if applicable`,
# `if appropriate`, and `if possible`, which the vague-criterion rule
# reports, and `if any` and `if so`, which read worse at the front. It
# skips `if` after a verb of checking (`check if`, `determine if`), which
# the if-whether rule reports, and after `as` and `even`, where `if`
# opens no condition. `Avoid ... unless` passes, because there the
# condition names the exception to a ban. lib/sentences.awk finds the
# sentence boundaries.
#
# It hit 31 lines in the measure.py corpus, and 20 of 20 sampled hits
# put a condition after its step.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseTrailingCondition: <message>

@include "sentences.awk"

function BEGIN_CHECK() {
  imperative = "^(add|apply|avoid|build|call|change|choose|click|configure|copy|create|define|delete|deploy|disable|do|don't|download|edit|enable|follow|install|keep|leave|mark|move|open|pass|pick|place|provide|push|put|read|remove|rename|replace|restart|return|run|save|see|select|send|set|skip|specify|start|stop|store|try|turn|update|upgrade|use|wait|write)$"
  vague = "^(needed|necessary|applicable|appropriate|possible|any|so)$"
  checking = "^(check|checks|checked|checking|test|tests|tested|testing|determine|determines|determined|determining|verify|verifies|verified|verifying|see|sees|seeing|decide|decides|decided|deciding|ask|asks|asked|asking|detect|detects|detected|detecting|tell|tells|telling)$"
}

# Returns token i in lowercase without the punctuation around it.
function word(i,   w) {
  w = tolower(tok[i])
  gsub(/^[^a-z']+|[^a-z']+$/, "", w)
  return w
}

function sentence(n,   first, i, depth, w, t) {
  first = word(1)
  if (first !~ imperative || tok[1] !~ /^[^A-Za-z]*[A-Z]/) {
    return
  }
  depth = 0
  for (i = 2; i <= n; i++) {
    t = tok[i]
    w = word(i)
    if ((w == "if" || w == "unless") && depth == 0 && t ~ /^[a-z]/) {
      if (w == "if" && (word(i + 1) ~ vague || word(i - 1) ~ checking || word(i - 1) == "as" || word(i - 1) == "even")) {
        continue
      }
      if (w == "unless" && first == "avoid") {
        continue
      }
      printf "%s:%d:%d: Style::ProseTrailingCondition: Trailing condition. Move the `%s` clause to the start of the sentence.\n", sentence_file, tok_line[i], tok_col[i], w
      return
    }
    depth += gsub(/\(/, "(", t) - gsub(/\)/, ")", t)
  }
}
