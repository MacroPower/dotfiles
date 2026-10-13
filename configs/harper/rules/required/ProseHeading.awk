# Reports markdown headings below the title that narrate instead of
# naming a topic. prose-lint and check.py both run this file, so the
# hook and the build-time fixture check agree on every heading.
#
# The check skips a level-one heading, because it is the document
# title, and a heading that is one code span, because a command
# reference names the exact command.
#
# The check reports a heading at any level from two down when it
# starts with a question word or an auxiliary, or ends with a question
# mark or a colon. At level two it also reports a heading that contains a
# pronoun, an auxiliary, or a subordinating conjunction anywhere, or
# that runs past six words. Deeper headings skip those two checks,
# because they often label a single case (`Retries after a lease
# error`) and can run longer than a section name. A possessive (`your`,
# `our`) that opens a level-two heading passes, because it starts a
# noun phrase (`Your Task`), but one after a verb still counts
# (`Setting up your editor`). The word lists hold closed word classes,
# so a noun phrase such as `Printing YAML with Lipgloss Styles` passes,
# and the check reports `How matching works` and `Configuring the
# server before you start`.
#
# The check does not use Harper's part-of-speech tagger, because a
# heading has no sentence around it and the tagger reads `works` and
# `ships` as nouns. The rule misses a short sentence built only from
# open-class words, such as `The scheduler retries jobs`.
#
# A heading at any level from two down that announces only a wrap-up
# (`Conclusion`, `Final Thoughts`, `Key Takeaways`, `TL;DR`) names no
# topic, so the check reports it too. `Summary`, `Overview`, and
# `Next Steps` pass, because human-written docs and PR descriptions use
# them as section names.
#
# The check skips YAML front matter, fenced code blocks, and indented
# code blocks. It does not examine Setext headings (text underlined
# with = or -), because the formatter rewrites them to # headings.
#
# Output shape, one line per finding:
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
  # Words that report a heading wherever they appear.
  split("you your we we're our us i my let's it it's they they're that is are was were has have can will should must does don't if before after because until while once so", list, " ")
  for (i in list) {
    anywhere[list[i]] = 1
  }
  # Words from the list above that pass as the heading's first word.
  possessive["your"] = 1
  possessive["our"] = 1
  # Whole headings that announce a wrap-up.
  n = split("conclusion|conclusions|final thoughts|closing thoughts|concluding thoughts|key takeaways|takeaways|wrapping up|wrap-up|in summary|in closing|tl;dr|tldr", list, "|")
  for (i = 1; i <= n; i++) {
    closer[list[i]] = 1
  }
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
  words = split(tolower(text), parts, /[ \t]+/)
  narrates = 0
  if (level == 2 && words > 6) {
    narrates = 1
  }
  if (text ~ /[?:]$/) {
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
  name = tolower(text)
  sub(/[.!:]+$/, "", name)
  gsub(/[ \t]+/, " ", name)
  if (name in closer) {
    printf "%s:%d:1: Style::ProseHeading: Heading \"%s\" names no topic. Name the topic as a short noun phrase.\n", FILENAME, NR, text
  } else if (narrates) {
    printf "%s:%d:1: Style::ProseHeading: Heading \"%s\" narrates. Name the topic as a short noun phrase.\n", FILENAME, NR, text
  }
}
