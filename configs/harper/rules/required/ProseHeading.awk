# Reports markdown headings below the title that narrate instead of
# naming a topic. prose-lint and check.py both run this file, so the
# hook and the build-time fixture check agree on every heading.
#
# Only level-two headings are examined. A level-one heading is the
# document title, and a deeper heading often labels a single case, so
# neither is reported. A heading that is one code span is exempt too,
# since a command reference names the exact command.
#
# A heading is reported when it starts with a question word or an
# auxiliary, contains a pronoun, an auxiliary, or a subordinating
# conjunction anywhere, ends with a question mark, or runs past six
# words. These are closed word classes, so a noun phrase such as
# `Printing YAML with Lipgloss Styles` passes while `How matching
# works` and `Configuring the server before you start` are reported.
# Harper's part-of-speech tagger is not used, because a heading has no
# sentence around it and the tagger reads `works` and `ships` as nouns.
# The rule misses a short sentence built only from open-class words,
# such as `The scheduler retries jobs`.
#
# YAML front matter, fenced code blocks, and indented code blocks are
# skipped. Setext headings (text underlined with = or -) are not
# examined, because the formatter rewrites them to # headings.
#
# Output shape, one line per finding:
#   <file>:<line>:1: Style::ProseHeading: <message>

BEGIN {
  front_matter = 0
  fence_char = ""
  fence_len = 0
  # Words that report a heading when it starts with one of them.
  split("how why what what's when where which who do", list, " ")
  for (i in list) {
    opener[list[i]] = 1
  }
  # Words that report a heading wherever they appear.
  split("you your we we're our us i my let's it's is are was were can will should must does don't if before after because until while once so", list, " ")
  for (i in list) {
    anywhere[list[i]] = 1
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

/^ {0,3}##[ \t]/ {
  text = $0
  sub(/^ {0,3}#+[ \t]+/, "", text)
  sub(/[ \t]+#+[ \t]*$/, "", text)
  sub(/[ \t]+$/, "", text)
  if (text ~ /^`[^`]+`$/) {
    next
  }
  words = split(tolower(text), parts, /[ \t]+/)
  narrates = 0
  if (words > 6) {
    narrates = 1
  }
  if (text ~ /\?$/) {
    narrates = 1
  }
  for (i = 1; i <= words; i++) {
    word = parts[i]
    gsub(/[^a-z']/, "", word)
    if ((i == 1 && word in opener) || word in anywhere) {
      narrates = 1
    }
  }
  if (narrates) {
    printf "%s:%d:1: Style::ProseHeading: Heading \"%s\" narrates. Name the topic as a short noun phrase.\n", FILENAME, NR, text
  }
}
