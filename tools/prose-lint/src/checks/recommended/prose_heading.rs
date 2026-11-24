//! Overview
//!
//! Reports markdown headings below the title that narrate instead of
//! naming a topic (`How matching works`, `Is it safe?`), or that run past
//! six words. A reader scans the headings to find a section. A heading
//! that narrates makes the reader pick the topic out of a sentence or a
//! question. The fix names the topic as a short noun phrase, a noun and
//! the words attached to it (`Matching`), as the prose skill asks. The
//! rule sits at Recommended, because a FAQ page keeps its questions as
//! headings. A heading of seven or eight words also gains less from a
//! shorter name.
//!
//! Expression
//!
//! The check reads each heading block of the markdown model, at level
//! two and below, through `heading_text` of the Required ProseHeading.
//! The model does not hold a heading in code or in front matter.
//!
//! At any level from two down, the check reports a heading that ends with
//! a question mark or starts with a question word. It also reports one
//! that starts with an auxiliary, a helping verb such as `is`, `has`, or
//! `can`. At level two, it reports a heading that holds a pronoun, an
//! auxiliary, or a joining word such as `because`, `until`, or `if`
//! anywhere. It also reports a level-two heading that runs past six
//! words.
//!
//! The check drops the punctuation around each word, a straight `'`
//! included, so `'How'` counts as `how`, the same as `"How"`. It reads a
//! curly apostrophe between two letters as a straight one, so a curly
//! `Don't skip the backup` draws the finding that the straight one
//! draws.
//!
//! A trailing attribute list (`{#linux-security}`) is markup, so the
//! check drops it first. The word count treats a code span as one word
//! and skips a token with no letter or digit, such as a lone `-`. A
//! heading that holds a code span prints in a double-backtick span, so
//! its own backticks stay inside the quote.
//!
//! The check does not use Harper's part-of-speech tagger, because a
//! heading has no sentence around it and the tagger reads `works` and
//! `ships` as nouns. It matches word lists instead, so the rule misses a
//! short sentence whose words all fall outside the lists, such as `The
//! scheduler retries jobs`.
//!
//! False Positives
//!
//! The check skips a level-one heading, because it is the document title.
//! It also skips a heading that is one code span, because a command
//! reference names the exact command.
//!
//! Headings below level two skip the two level-two checks, because they
//! often label a single case (`Retries after a lease error`) and can run
//! longer than a section name.
//!
//! `your` or `our` at the start of a level-two heading passes, because it
//! starts a noun phrase (`Your Task`), but one after a verb still counts
//! (`Setting up your editor`). The word lists hold only small fixed word
//! classes, such as pronouns and helping verbs, so a noun phrase such as
//! `Printing YAML with Lipgloss Styles` passes.
//!
//! Related Rules
//!
//! The Required ProseHeading reports wrap-ups (`Conclusion`) and headings
//! that end in a colon (`Setup:`). The Recommended ProseHeading reports
//! headings that narrate (`How matching works`) or run long. The
//! Recommended triggers misread a heading more often, so a commit that
//! draws them passes on its second try.
//!
//! This check skips each heading that the Required ProseHeading reports,
//! so the two tiers never report the same heading. When a heading that
//! narrates (`How it works:`) or runs long ends in a colon, it draws only
//! the Required finding. This check reports the heading after the writer
//! deletes the colon.
//!
//! Measurement
//!
//! The rule hit 289 lines in the measure.py docs corpus, and 12 of 20
//! sampled headings read better as a noun phrase (`What do you need?`
//! becomes `Prerequisites`). Six more ran to seven or eight words, where
//! a shorter name helps less. The other 2 were questions on FAQ pages,
//! which readers search for by the question. A writer can keep such a
//! heading on the second try of a commit.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::checks::required::prose_heading::{heading_text, quote, reports};
use crate::markdown::Markdown;

/// Words that report a heading when it starts with one of them.
const OPENERS: &[&str] = &[
    "how", "why", "what", "what's", "when", "where", "which", "who", "do", "does", "did", "is",
    "are", "was", "were", "has", "have", "can", "could", "will", "would", "should", "must",
];

/// Words that report a level-two heading wherever they appear.
const ANYWHERE: &[&str] = &[
    "you", "your", "we", "we're", "our", "us", "i", "my", "let's", "it", "it's", "they", "they're",
    "that", "is", "are", "was", "were", "has", "have", "can", "will", "should", "must", "does",
    "don't", "if", "before", "after", "because", "until", "while", "once", "so",
];

/// Words from [`ANYWHERE`] that pass as the heading's first word.
const POSSESSIVES: &[&str] = &["your", "our"];

static SPAN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new("`[^`]*`").expect("the pattern is valid"));
static CURLY: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"([a-z])\x{2019}([a-z])").expect("the pattern is valid"));

/// Returns a finding for each heading that narrates or runs past six
/// words.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .blocks
        .iter()
        .filter_map(|block| heading_text(model, block))
        .filter(|h| !reports(&h.text))
        .filter_map(|h| {
            let counted = SPAN.replace_all(&h.text, "code").to_lowercase();
            let words: Vec<String> = counted
                .split([' ', '\t'])
                .filter(|w| w.contains(|c: char| c.is_ascii_lowercase() || c.is_ascii_digit()))
                .map(|w| {
                    CURLY
                        .replace_all(w, "$1'$2")
                        .trim_matches(|c: char| !c.is_ascii_lowercase())
                        .to_string()
                })
                .collect();
            let narrates = h.text.ends_with('?')
                || words.iter().enumerate().any(|(i, w)| {
                    (i == 0 && OPENERS.contains(&w.as_str()))
                        || (h.level == 2
                            && ANYWHERE.contains(&w.as_str())
                            && !(i == 0 && POSSESSIVES.contains(&w.as_str())))
                });
            let message = if narrates {
                format!(
                    "Heading {} narrates. Name the topic as a short noun phrase.",
                    quote(&h.text)
                )
            } else if h.level == 2 && words.len() > 6 {
                format!(
                    "Heading {} runs past six words. Name the topic in fewer words.",
                    quote(&h.text)
                )
            } else {
                return None;
            };
            Some(Hit { at: h.at, message })
        })
        .collect()
}
