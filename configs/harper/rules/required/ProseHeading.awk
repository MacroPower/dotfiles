# Reports markdown headings below the title that name no topic: a
# wrap-up (`Conclusion`, `Final Thoughts`, `Key Takeaways`, `TL;DR`), or
# a heading that ends in a colon (`Setup:`). prose-lint and check.py
# both run this file, so the hook and the build-time fixture check agree
# on every heading.
#
# A wrap-up announces where the document ends, not what the section
# holds. `Summary`, `Overview`, and `Next Steps` pass, because
# human-written docs and PR descriptions use them as section names. A
# heading already sets its section apart from the text, so a colon at
# its end adds nothing, and the fix deletes it.
#
# The Recommended rule of the same name reports headings that narrate
# (`How matching works`) or run long. Those triggers misread a heading
# more often, so a commit that draws them passes on its second try.
#
# The check skips a level-one heading, because it is the document
# title. It also skips a heading that is one code span, because a
# command reference names the exact command. A trailing attribute list
# (`{#linux-security}`) is markup, so the check drops it first. A heading
# that holds a code span prints in a double-backtick span, so its own
# backticks stay inside the quote.
#
# The check skips YAML front matter, fenced code blocks, and indented
# code blocks. It does not examine Setext headings (text underlined
# with = or -), because the formatter rewrites them to # headings.
#
# It hit 15 lines in the measure.py docs, and 15 of 15 were wrap-ups or
# trailing colons (`Conclusion`, `Linux:`).
#
# Output format, one line per finding:
#   <file>:<line>:1: Style::ProseHeading: <message>

BEGIN {
  front_matter = 0
  fence_char = ""
  fence_len = 0
  # Whole headings that announce a wrap-up.
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
  sub(/^ {0,3}#+[ \t]+/, "", text)
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
    printf "%s:%d:1: Style::ProseHeading: Heading %s names no topic. Name the topic as a short noun phrase.\n", FILENAME, NR, quote(text)
  } else if (text ~ /:$/) {
    printf "%s:%d:1: Style::ProseHeading: Heading %s ends in a colon. Delete the colon.\n", FILENAME, NR, quote(text)
  }
}
