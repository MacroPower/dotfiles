# Reports markdown headings below the title that narrate instead of
# naming a topic. prose-lint and check.py both run this file, so the
# hook and the build-time fixture check agree on every heading.
#
# A heading is reported when it has more than three words, starts
# with how, why, what, or when, or ends with a question mark. YAML
# front matter, fenced code blocks, and indented code blocks are
# skipped. Level-one headings are the document title and are exempt.
#
# Output shape, one line per finding:
#   <file>:<line>:1: Style::ProseHeading: <message>

BEGIN {
  front_matter = 0
  fence_char = ""
  fence_len = 0
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

/^#{2,6}[ \t]/ {
  text = $0
  sub(/^#+[ \t]+/, "", text)
  sub(/[ \t]+#+[ \t]*$/, "", text)
  sub(/[ \t]+$/, "", text)
  words = split(text, parts, /[ \t]+/)
  lower = tolower(text)
  narrates = 0
  if (words > 3) {
    narrates = 1
  }
  if (lower ~ /^(how|why|what|when)([ \t]|$)/) {
    narrates = 1
  }
  if (text ~ /\?$/) {
    narrates = 1
  }
  if (narrates) {
    printf "%s:%d:1: Style::ProseHeading: Heading \"%s\" narrates. Name the topic in one to three words.\n", FILENAME, NR, text
  }
}
