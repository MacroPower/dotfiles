# Reports markdown headings below the title that narrate instead of
# naming a topic (`How matching works`, `Is it safe?`), or that run past
# six words. The prose skill asks for a short noun phrase, a noun and
# the words attached to it (`Matching`). The Required rule of the same
# name reports wrap-ups (`Conclusion`) and trailing colons, so this
# check skips a wrap-up. prose-lint and check.py both run this file, so
# the hook and the build-time fixture check agree on every heading.
#
# The check skips a level-one heading, because it is the document
# title. It also skips a heading that is one code span, because a
# command reference names the exact command.
#
# At any level from two down, the check reports a heading that ends with
# a question mark or starts with a question word. It also reports one
# that starts with an auxiliary, a helping verb such as `is`, `has`, or
# `can`. At level two, it reports a heading that holds a pronoun, an
# auxiliary, or a joining word such as `because`, `until`, or `if`
# anywhere. It also reports a level-two heading that runs past six
# words. Deeper headings skip those two checks, because they often label
# a single case (`Retries after a lease error`) and can run longer than
# a section name.
#
# A possessive (`your`, `our`) that opens a level-two heading passes,
# because it starts a noun phrase (`Your Task`), but one after a verb
# still counts (`Setting up your editor`). The word lists hold only
# small fixed word classes, such as pronouns and helping verbs, so a
# noun phrase such as `Printing YAML with Lipgloss Styles` passes.
#
# The word count drops a trailing attribute list (`{#linux-security}`)
# and counts a code span as one word. It skips a token with no letter or
# digit, such as a lone `-`. A heading that holds a code span prints in
# a double-backtick span, so its own backticks stay inside the quote.
#
# The check does not use Harper's part-of-speech tagger, because a
# heading has no sentence around it and the tagger reads `works` and
# `ships` as nouns. The rule misses a short sentence whose words all
# fall outside the lists, such as `The scheduler retries jobs`.
#
# It hit 289 lines in the measure.py docs, and 12 of 20 sampled headings
# read better as a noun phrase (`What do you need?` becomes
# `Prerequisites`). Six more ran to seven or eight words, where a
# shorter name helps less. The other 2 were questions on FAQ pages,
# which readers search for by the question. Such a heading passes on
# the second try of a commit, so a writer can keep a FAQ question.
#
# The check skips YAML front matter, fenced code blocks, and indented
# code blocks. It does not examine Setext headings (text underlined
# with = or -), because the formatter rewrites them to # headings.
#
# Output format, one line per finding:
#   <file>:<line>:1: Style::ProseHeading: <message>

BEGIN {
  front_matter = 0
  fence_char = ""
  fence_len = 0
  # Words that report a heading when it starts with one of them.
  split("how why what what's when where which who do does did is are was were has have can could will would should must", list, " ")
  for (i in list) {
    opener[list[i]] = 1
  }
  # Words that report a level-two heading wherever they appear.
  split("you your we we're our us i my let's it it's they they're that is are was were has have can will should must does don't if before after because until while once so", list, " ")
  for (i in list) {
    anywhere[list[i]] = 1
  }
  # Words from the list above that pass as the heading's first word.
  possessive["your"] = 1
  possessive["our"] = 1
  # Whole headings that announce a wrap-up, which the Required rule
  # reports.
  n = split("conclusion|conclusions|final thoughts|closing thoughts|concluding thoughts|key takeaways|takeaways|wrapping up|wrap-up|in summary|in closing|tl;dr|tldr", list, "|")
  for (i = 1; i <= n; i++) {
    closer[list[i]] = 1
  }
}

# Returns the heading text as a code span. A heading that holds a code
# span gets a double-backtick span, so its own backticks stay inside.
function quote(s) {
  if (s ~ /`/) {
    return "`` " s " ``"
  }
  return "`" s "`"
}

NR == 1 && $0 ~ /^---[ \t]*$/ {
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
  sub(/^ {0,3}/, "", line)
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

/^ {0,3}(```|~~~)/ {
  line = $0
  sub(/^ {0,3}/, "", line)
  fence_char = substr(line, 1, 1)
  fence_len = 0
  while (substr(line, fence_len + 1, 1) == fence_char) {
    fence_len++
  }
  next
}

/^(    |\t)/ {
  next
}

/^ {0,3}#{2,6}[ \t]/ {
  text = $0
  sub(/^ {0,3}/, "", text)
  match(text, /^#+/)
  level = RLENGTH
  sub(/^#+[ \t]+/, "", text)
  sub(/[ \t]+#+[ \t]*$/, "", text)
  sub(/[ \t]+$/, "", text)
  if (text ~ /^`[^`]+`$/) {
    next
  }
  sub(/[ \t]*\{[#.][^}]*\}[ \t]*$/, "", text)
  name = tolower(text)
  sub(/[.!:]+$/, "", name)
  gsub(/[ \t]+/, " ", name)
  if (name in closer) {
    next
  }
  counted = text
  gsub(/`[^`]*`/, "code", counted)
  n = split(tolower(counted), raw, /[ \t]+/)
  words = 0
  for (i = 1; i <= n; i++) {
    if (raw[i] ~ /[a-z0-9]/) {
      parts[++words] = raw[i]
    }
  }
  narrates = 0
  if (text ~ /[?]$/) {
    narrates = 1
  }
  for (i = 1; i <= words; i++) {
    word = parts[i]
    gsub(/[^a-z']/, "", word)
    if (i == 1 && word in opener) {
      narrates = 1
    }
    if (level == 2 && word in anywhere && !(i == 1 && word in possessive)) {
      narrates = 1
    }
  }
  if (narrates) {
    printf "%s:%d:1: Style::ProseHeading: Heading %s narrates. Name the topic as a short noun phrase.\n", FILENAME, NR, quote(text)
  } else if (level == 2 && words > 6) {
    printf "%s:%d:1: Style::ProseHeading: Heading %s runs past six words. Name the topic in fewer words.\n", FILENAME, NR, quote(text)
  }
}
