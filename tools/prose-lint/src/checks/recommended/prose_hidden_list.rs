//! Overview
//!
//! Reports a paragraph of markdown prose that hides a list. The
//! technical-writing skill names two signs. Sentences that walk through
//! steps with `first`, `then`, and `finally` hide a numbered list.
//! Consecutive sentences that open the same way hide a bulleted list or
//! a table. Such a paragraph makes the reader pick the steps or the
//! items out of its sentences. The fix moves the steps into a numbered
//! list, and the items into a bulleted list or a table. The rule sits at
//! Recommended, because a plan of two steps and a list that continues
//! from an earlier paragraph also match.
//!
//! Expression
//!
//! The check reads each paragraph block of the markdown model, and the
//! text of each list item counts as a paragraph of its own. The sentence
//! boundaries follow ProseParagraphLength.
//!
//! The first form is two sentences in one paragraph that open with
//! different sequence words, where at least one of them counts (`first`,
//! `second`, `next`, `finally`).
//!
//! The second form is three sentences in a row that open with the same
//! two words (`The node controller`). The model blanks code spans, so
//! `The --force flag skips` and `The --dry-run flag prints` both open
//! with `The flag`.
//!
//! A sentence can open with a quotation that ends in `.`, `?`, or `!`,
//! when a lowercase word after the closing quote continues it (`"Save
//! changes?" appears`). Three sentences in a row that open with the same
//! quotation repeat one item, such as a dialog title, as three sentences
//! that open with `The node controller` do. They hide a list of the cases
//! where the item appears. The check keys them on their first two words,
//! as it keys any other sentence.
//!
//! When the opening quotation holds two or more words, the first two
//! words of the sentence sit inside it (`save changes`). Three such
//! sentences then draw a finding whatever words follow the quote. After a
//! quotation of one word, the two words cross the closing quote
//! (`"Ready?" the` keys on `ready the`). Three sentences that open with
//! the same quotation of one word therefore pass when the word after the
//! quote differs (`"Ready?" the wizard`, `"Ready?" a prompt`, `"Ready?"
//! one dialog`).
//!
//! A sentence of one word has no opening to compare, and a blanked code
//! span does not count as a word. Three sentences that each hold one verb
//! and a code span therefore pass, though they hide a list of commands
//! (``Run `make`. Run `make test`. Run `make lint`.``). The same
//! sentences in plain words draw a finding.
//!
//! Each finding names the line and column where the paragraph starts.
//!
//! False Positives
//!
//! `then` alone narrates (`Then the pod restarts`), so two `then`
//! sentences pass. `now` stays out, because it marks a turn in an
//! argument (`Now say you have ten pods`).
//!
//! A `Finally` that continues a list from an earlier paragraph draws a
//! finding when another sentence of the paragraph opens with a sequence
//! word (`Finally, adding the device name can select one specific
//! device.`). Three sentences can also open the same way only after
//! the model blanks their code spans (`The largest_i32 function is`,
//! `The largest_char function finds`, `The function bodies have`).
//!
//! Related Rules
//!
//! Openings with `a` or `an` (`A pod`) pass, because
//! ProseIndefiniteOpener reports those sentences and asks for a plural
//! subject instead of a list. A quotation that ends in a comma does not
//! end the sentence, so `"A lease," the guide says` opens with `a` for
//! both rules, and this rule skips it.
//!
//! A sentence that opens with a quotation and goes on past a quoted `.`,
//! `?`, or `!` keeps its key, even when the quotation opens with `a` or
//! `an` (`"A lease?" the guide asks`). ProseIndefiniteOpener stops its
//! subject scan at the closing quote. It does not count such a sentence
//! as the first of a run of sentences that open with `a` or `an`. Three
//! sentences in a row that open with `"A lease?"` therefore draw a
//! finding from this rule.
//!
//! ProseIndefiniteOpener reports some of these sentences too. When a
//! sentence that opens with `a` or `an` comes right before the three (`A
//! lease is a lock. "A lease?" the guide asks.`), ProseIndefiniteOpener
//! reports the first of them as the second sentence of a run. When a
//! present-tense verb sits inside the quotation (`"A lease is held?" the
//! guide asks`), ProseIndefiniteOpener reports each of the three
//! sentences.
//!
//! Measurement
//!
//! The rule hit 27 lines in the measure.py docs corpus, and 17 of 20
//! sampled hits hid steps, parallel points, or cases. The three misfires
//! were plans of two steps (`First, we'll... Then, we'll...`).

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::checks::sentences::{End, continues_sentence, ends_sentence, opens_quote, words};
use crate::markdown::{Block, BlockKind, Markdown, blank};

/// The words that open a step.
const SEQUENCE: &[&str] = &[
    "first",
    "firstly",
    "second",
    "secondly",
    "third",
    "thirdly",
    "fourth",
    "then",
    "next",
    "finally",
    "lastly",
    "afterward",
    "afterwards",
];

/// The words of [`SEQUENCE`] that count a step.
const ORDINALS: &[&str] = &[
    "first", "firstly", "second", "secondly", "third", "thirdly", "fourth", "next", "finally",
    "lastly",
];

/// A run-in label in bold that opens a paragraph (`**Tutorial.**`).
pub static LABEL: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^[ \t]*(?:(?:[-*+]|[0-9]+[.)])[ \t]+)?(?:\*\*[^*]+[.:]\*\*|__[^_]+[.:]__)")
        .expect("the pattern is valid")
});

/// The openings of the sentences of one paragraph.
#[derive(Default)]
struct Paragraph {
    /// The sequence words that open a sentence, in order, each once.
    distinct: Vec<String>,
    /// The count of sentences that open with a sequence word.
    sequence: usize,
    /// Whether a sentence opens with a word of [`ORDINALS`].
    ordinal: bool,
    /// The key of the last sentence and the length of its run.
    prev: String,
    run: usize,
    best: usize,
    /// The first two words of the open sentence.
    key: Vec<String>,
    /// Whether the open sentence opens with a quote, and whether it goes
    /// on past a quoted mark.
    quoted: bool,
    quote_opener: bool,
}

impl Paragraph {
    /// Ends the open sentence and compares its opening to the last one.
    /// A sentence of one word has no opening to compare.
    fn end_sentence(&mut self) {
        let mut key = if self.key.len() < 2 {
            String::new()
        } else {
            self.key.join(" ")
        };
        // ProseIndefiniteOpener reports the sentences that open with `a`
        // or `an`. A sentence that opens with a quotation and goes on past
        // a quoted mark keeps its key.
        if (key.starts_with("a ") || key.starts_with("an ")) && !self.quote_opener {
            key.clear();
        }
        if !key.is_empty() && key == self.prev {
            self.run += 1;
            self.best = self.best.max(self.run);
        } else {
            self.run = 1;
        }
        self.prev = key;
        self.key.clear();
        self.quoted = false;
        self.quote_opener = false;
    }
}

/// Returns the finding of one paragraph, if any.
fn paragraph(model: &Markdown, block: &Block) -> Option<Hit> {
    let mut p = Paragraph {
        run: 1,
        best: 1,
        ..Paragraph::default()
    };
    let mut start = None;
    let mut opening = true;
    let mut quote_end = false;
    for (n, (i, text)) in model.block_lines(block).enumerate() {
        let text = if n == 0 { blank(&text, &LABEL) } else { text };
        for (offset, word) in words(&text) {
            if quote_end {
                if !continues_sentence(word) {
                    p.end_sentence();
                    opening = true;
                } else if p.quoted {
                    p.quote_opener = true;
                }
            }
            quote_end = false;
            if !word.contains(|c: char| c.is_ascii_alphanumeric()) {
                match if opening {
                    End::No
                } else {
                    ends_sentence(word)
                } {
                    End::Yes => {
                        p.end_sentence();
                        opening = true;
                    }
                    End::Quote => quote_end = true,
                    End::No => {}
                }
                continue;
            }
            start.get_or_insert_with(|| model.lines[i].at(&text, offset));
            let lower: String = word
                .to_lowercase()
                .chars()
                .filter(|c| c.is_ascii_lowercase() || c.is_ascii_digit())
                .collect();
            if opening {
                p.quoted = opens_quote(word);
                if SEQUENCE.contains(&lower.as_str()) {
                    p.sequence += 1;
                    p.ordinal |= ORDINALS.contains(&lower.as_str());
                    if !p.distinct.contains(&lower) {
                        p.distinct.push(lower.clone());
                    }
                }
            }
            opening = false;
            if p.key.len() < 2 && !lower.is_empty() {
                p.key.push(lower);
            }
            match ends_sentence(word) {
                End::Yes => {
                    p.end_sentence();
                    opening = true;
                }
                End::Quote => quote_end = true,
                End::No => {}
            }
        }
    }
    // A paragraph that ends at a mark and a closing quote ends its last
    // sentence there.
    if quote_end {
        p.end_sentence();
    }
    let message = if p.sequence >= 2 && p.ordinal && p.distinct.len() >= 2 {
        "Hidden list. Sentences that open with `first`, `then`, or `finally` walk through steps. Move the steps into a numbered list.".to_string()
    } else if p.best >= 3 {
        format!(
            "Hidden list. {} sentences in a row open the same way. Move the items into a bulleted list, or into a table when they share attributes.",
            p.best
        )
    } else {
        return None;
    };
    Some(Hit {
        at: start?,
        message,
    })
}

/// Returns a finding for each paragraph that hides a list.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .blocks
        .iter()
        .filter(|b| b.kind == BlockKind::Paragraph)
        .filter_map(|b| paragraph(model, b))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::region::Region;

    #[test]
    fn paragraphs() {
        struct Case {
            name: &'static str,
            text: &'static str,
            /// The line and the column of each finding.
            want: Vec<(usize, usize)>,
        }
        let cases = [
            Case {
                name: "quoted marks that a lowercase word continues",
                text: "The node runs \"Go!\"\ntwice. The node stops \"Wait!\"\ntwice. The node waits here.\n",
                want: vec![(1, 1)],
            },
            Case {
                name: "the same text in a blockquote",
                text: "> The node runs \"Go!\"\n> twice. The node stops \"Wait!\"\n> twice. The node waits here.\n",
                want: vec![(1, 3)],
            },
        ];
        for case in cases {
            let region = Region::markdown(case.text);
            let got: Vec<_> = check(&Markdown::new(&region))
                .into_iter()
                .map(|hit| {
                    let at = region.position(hit.at);
                    (at.line, at.col)
                })
                .collect();
            assert_eq!(got, case.want, "{}", case.name);
        }
    }
}
