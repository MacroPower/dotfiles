//! Overview
//!
//! Reports an emoji in markdown prose and in source comments
//! (`:rocket: Deployed the fix`). An emoji decorates the text and states
//! nothing, so the reader must guess the point that it stands for. The
//! fix deletes the emoji or says the point in words (`Deployed the fix`).
//! When the text names an emoji, the fix sets the emoji in a code span.
//! The rule sits at Required, because the check matches the characters
//! themselves and each fix is mechanical.
//!
//! An emoji alone in a table cell reports as well (`| Linux | ✅ |`). It
//! stands in for a word such as `yes`, and the word reads the same in
//! the source and in every renderer. A `:shortcode:` shows as its name
//! wherever the renderer does not expand it. The fix writes the word or
//! a plain `✓`.
//!
//! Expression
//!
//! The check reads the prose of each line of the markdown model, in
//! which code, HTML, and link destinations are blank. Emoji include the
//! pictographs, flags, and skin tones in U+1F000 to U+1FAFF, and the
//! symbols whose default presentation is emoji. Those symbols include a
//! check mark button, a sparkle, and a star. Any symbol followed by the
//! U+FE0F emoji selector counts too, and so do keycaps and `:shortcode:`
//! names. Every prose line counts, because emoji turn up in link text,
//! table cells, and summaries as often as in headings and list items.
//!
//! One emoji can hold several code points, such as a skin tone, a
//! selector, a keycap mark, or a symbol that the U+200D joiner ties on.
//! `JOINED` matches the code points after the first one, so the check
//! reads them as one emoji. A flag is a pair of regional indicator
//! letters, and `EMOJI` matches the pair as one. A region flag such as
//! Scotland's is a black flag and tag letters, and `JOINED` reads the
//! tag letters into the flag. Four symbols, such as `✌`, are text by
//! default but take a skin tone, and `EMOJI` matches each of them with
//! its tone as one emoji too. Two emoji with no space between them read
//! as two (`🎉🚀`), so the check judges each one.
//!
//! `named` reads the text on each side of the emoji and passes an emoji
//! that the text names. The check reports the first emoji on the line
//! that `named` does not pass. When no emoji character reports, the
//! check reports the first `:shortcode:` name that `named` does not
//! pass. `named` also passes an emoji that decorates the text directly
//! before or after the word `emoji` (`We love emoji 🎉`,
//! `## ✨ Emoji support`).
//!
//! False Positives
//!
//! The U+2600 to U+27BF block stays out as a range, because a plain check
//! mark in a table cell or a prompt glyph is text, not decoration. The
//! model blanks the `:name: value` option lines under the opening line
//! of a MyST directive (`:::{note}`), so an option such as `:class: tip`
//! does not count as a shortcode.
//!
//! An emoji that the text names is the subject of the sentence and not
//! decoration, so it passes. `named` takes an emoji as named when the
//! word `emoji` or `emojis` sits directly before it or directly after it
//! (`the emoji 🎉 has a code point`, `the 🎉 emoji`). A colon can follow
//! the word before the emoji, as in a glossary entry (`Emoji: 🎉`). A
//! parenthesis or a straight quote can sit between the word and the
//! emoji (`the heart emoji (❤️)`, `the "🎉" emoji`). An emoji in a code
//! span passes too, because the model blanks code spans.
//!
//! A named emoji reports when a curly quote sits between it and the word
//! `emoji`. `named` passes only the emoji next to the
//! word, so the other emoji in a list of named emoji report
//! (`the emojis 🎉, 🚀, and 🔥`). Each row of a table that lists emoji
//! by name reports too (`| 🎉 | party popper |`), and the code span fits
//! each one. `named` reads one line at a time, so a named emoji also
//! reports when a line break falls between it and the word `emoji`. For
//! example, one line ends in `the emoji`, and the next line opens with
//! `🎉 has a code point`.
//!
//! Measurement
//!
//! The rule hit 14 lines in the measure.py corpus, and 12 of 14 sampled
//! hits decorated the text or stood in for a word. Eight of the 12 were
//! the `:heavy_check_mark:` in the cells of one table, where the fix
//! writes `yes`. The two misfires named an emoji without the word
//! `emoji`. One quoted the label of a link on another site
//! (`the link to 📦 Source`), and the code span fits it. The other was
//! the sample output of an emoji function (`I :heart: Hugo!`), which the
//! page renders on purpose, so no fix fits it.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::markdown::{Line, Markdown};

/// Characters below U+1F000 whose default presentation is emoji. A
/// text-default symbol such as ✓, ★, ⚠, or ❯ passes on its own. It
/// counts only when the U+FE0F variation selector or a skin tone
/// follows it.
const BMP: &str = "⌚⌛⏩⏪⏫⏬⏰⏳◽◾☔☕♈♉♊♋♌♍♎♏♐♑♒♓♿⚓⚡⚪⚫⚽⚾⛄⛅⛎⛔⛪⛲⛳⛵⛺⛽✅✊✋✨❌❎❓❔❕❗➕➖➗➰➿⬛⬜⭐⭕";

/// A skin tone, U+1F3FB to U+1F3FF, inclusive.
const TONE: &str = r"[\x{1F3FB}-\x{1F3FF}]";

/// One emoji character. It is a flag of two regional indicator
/// letters, or one of ☝, ⛹, ✌, and ✍ with a skin tone. It can also be a
/// symbol of [`BMP`], a selector, a keycap mark, or a character of
/// U+1F000 to U+1FAFF.
static EMOJI: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(&format!(
        r"[\x{{1F1E6}}-\x{{1F1FF}}][\x{{1F1E6}}-\x{{1F1FF}}]|[☝⛹✌✍]{TONE}|[{BMP}]|\x{{FE0F}}|\x{{20E3}}|[\x{{1F000}}-\x{{1FAFF}}]"
    ))
    .expect("the pattern is valid")
});

/// The code points that follow the first one in the same emoji. Each
/// is a selector, a keycap mark, a skin tone, a tag letter, or a symbol
/// after the U+200D joiner. A second emoji is not among them, so each
/// emoji in a run reads on its own.
static JOINED: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(&format!(
        r"^(?:\x{{FE0F}}|\x{{20E3}}|{TONE}|[\x{{E0020}}-\x{{E007F}}]|\x{{200D}}[^ \t])+"
    ))
    .expect("the pattern is valid")
});

static SHORTCODE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?:^|[ \t(])(:[a-z0-9_+-]*[a-z][a-z0-9_+-]*:)(?:[ \t.,;!?)]|$)")
        .expect("the pattern is valid")
});

static NAMED_BEFORE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r#"\b[Ee]mojis?:?[ \t]+[("']?$"#).expect("the pattern is valid"));
static NAMED_AFTER: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r#"^[)"']?[ \t]+[Ee]mojis?\b"#).expect("the pattern is valid"));

/// Tells whether the text names the emoji that sits between `before`
/// and `after`. The word `emoji` or `emojis` then sits directly before
/// the emoji or directly after it. A colon can follow the word before
/// the emoji, and a `(` or a straight quote can sit between that word
/// and the emoji. A `)` or a straight quote can sit between the emoji
/// and the word after it.
fn named(before: &str, after: &str) -> bool {
    NAMED_BEFORE.is_match(before) || NAMED_AFTER.is_match(after)
}

fn hit(line: &Line, text: &str, start: usize, end: usize) -> Hit {
    Hit {
        at: line.at(text, start),
        message: format!(
            "Emoji `{}` decorates the text or stands in for a word. Delete it, or say the point in words. When the text names the emoji, set it in a code span.",
            &text[start..end]
        ),
    }
}

/// Returns a finding for the first emoji on each line that the text
/// does not name.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model.lines.iter().filter_map(first).collect()
}

fn first(line: &Line) -> Option<Hit> {
    let text = line.text.as_str();
    let mut from = 0;
    while let Some(m) = EMOJI.find_at(text, from) {
        let mut start = m.start();
        // A selector or keycap mark names the symbol before it.
        if matches!(m.as_str(), "\u{FE0F}" | "\u{20E3}")
            && let Some((i, _)) = text[..start].char_indices().next_back()
        {
            start = i;
        }
        let end = m.end() + JOINED.find(&text[m.end()..]).map_or(0, |j| j.end());
        if !named(&text[..start], &text[end..]) {
            return Some(hit(line, text, start, end));
        }
        from = end;
    }
    let mut from = 0;
    while let Some(c) = SHORTCODE.captures(&text[from..]) {
        let name = c.get(1).expect("the pattern captures the name");
        let (start, end) = (from + name.start(), from + name.end());
        if !named(&text[..start], &text[end..]) {
            return Some(hit(line, text, start, end));
        }
        from = end;
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::region::Region;

    #[test]
    fn columns() {
        // The emoji cases of the columns test of the bash prose-lint. A
        // character of more than one byte sits in a code span or a link
        // destination ahead of each finding.
        let text = "Set `\u{1F680}` and then \u{1F389} here.\n\nRead [the guide](https://example.com/caf\u{e9}) \u{1F389} now.\n\nPass `--name\ncaf\u{e9}` to it \u{1F389} now.\n";
        let region = Region::markdown(text);
        let got: Vec<_> = check(&Markdown::new(&region))
            .into_iter()
            .map(|hit| {
                let at = region.position(hit.at);
                (at.line, at.col)
            })
            .collect();
        assert_eq!(got, [(1, 18), (3, 44), (6, 13)]);
    }
}
