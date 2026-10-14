# Reports a markdown heading that skips a level below the heading
# before it, such as a `####` heading right after a `##` heading.
# Readers who scan the headings read the outline from their levels.
# When the outline skips a level, they cannot tell whether the deeper
# heading belongs to the one above it or to a missing section.
# Screen readers list headings by level, so the skip also breaks their
# outline. The fix moves the heading up to the next level, or adds the
# missing heading above it, which ProseHeading then checks.
#
# A `title:` line in YAML front matter counts as the level-one heading,
# and so does a `#` heading. A file with neither takes its first
# heading's level as the top, so a PR description or an included
# fragment that starts at `###` passes. Headings inside fenced and
# indented code pass, because lib/markdown.awk skips code. Each finding
# names the line of the heading that skips.
#
# It hit 18 lines in the measure.py docs, and 16 of 18 skipped a level.
# In one miss, YAML comment lines inside an unfenced Hugo shortcode
# opened with `#`, and the check took them for a level-one heading. The
# other was a sidebar heading inside a blockquote, which skips a level
# on purpose.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseHeadingLevel: <message>

{
  if (FNR == 1) {
    in_fm = ($0 ~ /^---[ \t]*$/)
    titled = 0
  } else if (in_fm) {
    if ($0 ~ /^(---|\.\.\.)[ \t]*$/) {
      in_fm = 0
    } else if ($0 ~ /^title[ \t]*:/) {
      titled = 1
    }
  }
}

@include "markdown.awk"

function BEGIN_CHECK() {
  last_file = ""
}

function prose(text,   t, level, want) {
  if (FILENAME != last_file) {
    last_file = FILENAME
    prev = 0
  }
  if (!match(text, /^ {0,3}#{1,6}([ \t]|$)/)) {
    return
  }
  if (!prev && titled) {
    prev = 1
  }
  t = text
  sub(/^ */, "", t)
  match(t, /^#+/)
  level = RLENGTH
  if (prev && level > prev + 1) {
    want = substr("######", 1, prev + 1)
    printf "%s:%d:1: Style::ProseHeadingLevel: Heading level. A level-%d heading follows a level-%d heading and skips a level. Make it `%s`, or add the missing heading above it.\n", FILENAME, FNR, level, prev, want
  }
  prev = level
}
