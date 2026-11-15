# Overview
#
# Reports an emoji in markdown prose and in source comments
# (`:rocket: Deployed the fix`). An emoji decorates the text and states
# nothing, so the reader must guess the point that it stands for. The
# fix deletes the emoji or says the point in words (`Deployed the fix`).
# When the text names an emoji, the fix sets the emoji in a code span.
# The rule sits at Required, because the check matches the characters
# themselves and each fix is mechanical.
#
# An emoji alone in a table cell reports as well (`| Linux | ✅ |`). It
# stands in for a word such as `yes`, and the word reads the same in
# the source and in every renderer. A `:shortcode:` shows as its name
# wherever the renderer does not expand it. The fix writes the word or
# a plain `✓`.
#
# Expression
#
# The check includes lib/markdown.awk and reads each prose line. Emoji
# include the pictographs, flags, and skin tones in U+1F000 to U+1FAFF,
# and the symbols whose default presentation is emoji. Those symbols
# include a check mark button, a sparkle, and a star. Any symbol
# followed by the U+FE0F emoji selector counts too, and so do keycaps
# and `:shortcode:` names. Every prose line counts, because emoji turn
# up in link text, table cells, and summaries as often as in headings
# and list items.
#
# One emoji can hold several code points, such as a skin tone, a
# selector, a keycap mark, or a symbol that the U+200D joiner ties on.
# `joined` matches the code points after the first one, so the check
# reads them as one emoji. A flag is a pair of regional indicator
# letters, and `emoji` matches the pair as one. A region flag such as
# Scotland's is a black flag and tag letters, and `joined` reads the
# tag letters into the flag. Four symbols, such as `✌`, are text by
# default but take a skin tone, and `emoji` matches each of them with
# its tone as one emoji too. Two emoji with no space between them read
# as two (`🎉🚀`), so the check judges each one.
#
# `named` reads the text on each side of the emoji and passes an emoji
# that the text names. The check reports the first emoji on the line
# that `named` does not pass. When no emoji character reports, the
# check reports the first `:shortcode:` name that `named` does not
# pass. `named` also passes an emoji that decorates the text directly
# before or after the word `emoji` (`We love emoji 🎉`,
# `## ✨ Emoji support`).
#
# A range needs a UTF-8 locale. BEGIN_CHECK reads `utf8`, which
# lib/markdown.awk sets when gawk reads a two-byte character as one.
# Under the C locale the check spells the same block as its UTF-8 bytes,
# so the output matches in both.
#
# configs/harper/comment-checks.txt lists the check, so prose-lint also
# runs it on the comments of a source file. A `!<kind>` after a name in
# the list skips one comment kind, such as `!go` for Go comments, and
# the list puts none after this check.
#
# False Positives
#
# The U+2600 to U+27BF block stays out as a range, because a plain check
# mark in a table cell or a prompt glyph is text, not decoration.
# lib/markdown.awk skips the `:name: value` option lines of a MyST
# directive, so an option such as `:class: tip` does not count as a
# shortcode.
#
# An emoji that the text names is the subject of the sentence and not
# decoration, so it passes. `named` takes an emoji as named when the
# word `emoji` or `emojis` sits directly before it or directly after it
# (`the emoji 🎉 has a code point`, `the 🎉 emoji`). A colon can follow
# the word before the emoji, as in a glossary entry (`Emoji: 🎉`). A
# parenthesis or a straight quote can sit between the word and the
# emoji (`the heart emoji (❤️)`, `the "🎉" emoji`). An emoji in a code
# span passes too, because lib/markdown.awk blanks code spans.
#
# A named emoji reports when a curly quote sits between it and the word
# `emoji` (`the emoji “🎉”`). `named` passes only the emoji next to the
# word, so the other emoji in a list of named emoji report
# (`the emojis 🎉, 🚀, and 🔥`). Each row of a table that lists emoji
# by name reports too (`| 🎉 | party popper |`), and the code span fits
# each one. `named` reads one line at a time, so a named emoji also
# reports when a line break falls between it and the word `emoji`. For
# example, one line ends in `the emoji`, and the next line opens with
# `🎉 has a code point`.
#
# Measurement
#
# The rule hit 14 lines in the measure.py corpus, and 12 of 14 sampled
# hits decorated the text or stood in for a word. Eight of the 12 were
# the `:heavy_check_mark:` in the cells of one table, where the fix
# writes `yes`. The two misfires named an emoji without the word
# `emoji`. One quoted the label of a link on another site
# (`the link to 📦 Source`), and the code span fits it. The other was
# the sample output of an emoji function (`I :heart: Hugo!`), which the
# page renders on purpose, so no fix fits it.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseEmoji: <message>

@include "markdown.awk"

function BEGIN_CHECK(   list, n, i, bmp, flag, tone, tag, sym) {
  # Characters below U+1F000 whose default presentation is emoji.
  # A text-default symbol such as ✓, ★, ⚠, or ❯ passes on its own. It
  # counts only when the U+FE0F variation selector or a skin tone
  # follows it.
  n = split("⌚ ⌛ ⏩ ⏪ ⏫ ⏬ ⏰ ⏳ ◽ ◾ ☔ ☕ ♈ ♉ ♊ ♋ ♌ ♍ ♎ ♏ ♐ ♑ ♒ ♓ ♿ ⚓ ⚡ ⚪ ⚫ ⚽ ⚾ ⛄ ⛅ ⛎ ⛔ ⛪ ⛲ ⛳ ⛵ ⛺ ⛽ ✅ ✊ ✋ ✨ ❌ ❎ ❓ ❔ ❕ ❗ ➕ ➖ ➗ ➰ ➿ ⬛ ⬜ ⭐ ⭕", list, " ")
  bmp = ""
  for (i = 1; i <= n; i++) {
    bmp = bmp "|" list[i]
  }
  # U+FE0F asks for emoji presentation, U+20E3 builds a keycap, and
  # U+1F000 to U+1FAFF holds the pictographs, flags, and skin tones.
  # `flag` matches the two regional indicator letters of a flag, and
  # gawk takes the longest match, so `emoji` reads a flag whole. `tone`
  # matches a skin tone, U+1F3FB to U+1F3FF, inclusive. ☝, ⛹, ✌, and ✍
  # are text by default but take a skin tone. `emoji` matches each of
  # the four with a tone after it. gawk takes the leftmost match, so the
  # symbol and its tone read as one emoji. `tag` matches a tag letter,
  # U+E0020 to U+E007F, inclusive. A region flag such as Scotland's is a
  # black flag and a run of tag letters.
  # A range needs a UTF-8 locale. Under the C locale, the code spells
  # the same block as its UTF-8 bytes. A tag letter is invisible, so
  # `tag` spells its range as bytes under both locales. `sym` matches
  # one character other than a space or a tab, and the C locale reads
  # that character as one byte or more.
  if (utf8) {
    smp = "[🀀-🫿]"
    flag = "[🇦-🇿][🇦-🇿]"
    tone = "[🏻-🏿]"
    tag = "[\xF3\xA0\x80\xA0-\xF3\xA0\x81\xBF]"
    sym = "[^ \t]"
  } else {
    smp = "\xF0\x9F[\x80-\xAB][\x80-\xBF]"
    flag = "\xF0\x9F\x87[\xA6-\xBF]\xF0\x9F\x87[\xA6-\xBF]"
    tone = "\xF0\x9F\x8F[\xBB-\xBF]"
    tag = "\xF3\xA0(\x80[\xA0-\xBF]|\x81[\x80-\xBF])"
    sym = "([\x21-\x7F]|[\xC2-\xF4][\x80-\xBF]+)"
  }
  emoji = "(" flag "|(☝|⛹|✌|✍)" tone "|" substr(bmp, 2) "|️|⃣|" smp ")"
  # The code points that follow the first one in the same emoji. Each is
  # a selector, a keycap mark, a skin tone, a tag letter, or a symbol
  # after U+200D, and the bytes spell U+200D. A second emoji is not among
  # them, so each emoji in a run reads on its own.
  joined = "^(️|⃣|" tone "|" tag "|\xE2\x80\x8D" sym ")+"
  shortcode = "(^|[ \t(])(:[a-z0-9_+-]*[a-z][a-z0-9_+-]*:)([ \t.,;!?)]|$)"
}

# Tells whether the text names the emoji that sits between `before` and
# `after`. The word `emoji` or `emojis` then sits directly before the
# emoji or directly after it. A colon can follow the word before the
# emoji, and a `(` or a straight quote can sit between that word and the
# emoji. A `)` or a straight quote can sit between the emoji and the
# word after it.
function named(before, after) {
  return before ~ /\<[Ee]mojis?:?[ \t]+[("']?$/ || after ~ /^[)"']?[ \t]+[Ee]mojis?\>/
}

function report(text, start, end) {
  printf "%s:%d:%d: Style::ProseEmoji: Emoji `%s` decorates the text or stands in for a word. Delete it, or say the point in words. When the text names the emoji, set it in a code span.\n", FILENAME, FNR, column(text, start), substr(text, start, end - start)
}

function prose(text,   from, start, end) {
  from = 1
  while (match(substr(text, from), emoji)) {
    start = from + RSTART - 1
    end = start + RLENGTH
    # A selector or keycap mark names the symbol before it.
    if (start > 1 && substr(text, start, RLENGTH) ~ /^(️|⃣)$/) {
      start--
      while (!utf8 && start > 1 && substr(text, start, 1) ~ "^[\x80-\xBF]$") {
        start--
      }
    }
    if (match(substr(text, end), joined)) {
      end += RLENGTH
    }
    if (!named(substr(text, 1, start - 1), substr(text, end))) {
      report(text, start, end)
      return
    }
    from = end
  }
  from = 1
  while (match(substr(text, from), shortcode)) {
    start = from + RSTART - 1
    end = start + RLENGTH
    # The pattern also reads the character on each side of the name.
    start += (substr(text, start, 1) != ":")
    end -= (substr(text, end - 1, 1) != ":")
    if (!named(substr(text, 1, start - 1), substr(text, end))) {
      report(text, start, end)
      return
    }
    from = end
  }
}
