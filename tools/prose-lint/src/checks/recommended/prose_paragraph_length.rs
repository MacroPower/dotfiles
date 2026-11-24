//! Overview
//!
//! Reports a paragraph of more than six sentences in markdown prose. The
//! technical-writing skill keeps a paragraph to one topic and six
//! sentences or fewer, and a longer one usually carries a second topic. A
//! long paragraph makes the reader find where one topic ends and the next
//! one starts. The fix splits the paragraph, so each paragraph carries
//! one topic. The rule sits at Recommended, because a paragraph of more
//! than six sentences can also keep to one topic.
//!
//! Expression
//!
//! The check reads each paragraph block of the markdown model, and the
//! text of each list item counts as a paragraph of its own. A heading or
//! a table is a block of its own, so its lines do not count.
//!
//! A line that opens with seven or more `#` marks is text, as CommonMark
//! reads it. Such a line right after a paragraph continues the paragraph,
//! and its sentences count toward the paragraph (`####### It starts the
//! worker.`).
//!
//! Sentences end at a `.`, `?`, or `!` before whitespace or the line end,
//! after any closing quote, paren, bracket, or emphasis. A closing quote
//! is straight or curly. `e.g.`, `i.e.`, `vs.`, and `cf.` do not end a
//! sentence, and neither does a three-period ellipsis (`an implicit
//! "About..." in front`). A lowercase word after a closing quote
//! continues the sentence, because the quote closed a quotation inside it
//! (`prints "Lucky penny!" every time`). `ends_sentence` and
//! `continues_sentence` in `src/checks/sentences.rs` hold these tests.
//! Words after the last sentence end count as one more sentence.
//!
//! The model blanks code spans and link destinations to spaces. The
//! period after a span or a link then stands apart from the words. A
//! `.`, `?`, or `!` that stands apart ends the sentence in the same way,
//! and so does one after a number (`It defaults to 1.`). A sentence
//! counts only when it holds a word, so a code span with a period after
//! it is not a sentence of its own.
//!
//! A run-in label in bold that ends in a period or a colon names the
//! paragraph's topic (`**Tutorial.** The learner's success`). Such a
//! label counts as no sentence when it opens the paragraph. Each finding
//! names the line and column where the paragraph starts.
//!
//! False Positives
//!
//! An abbreviation that ends in a period counts as a sentence end when
//! it sits inside a sentence (`a TERM (aka. SIGTERM) signal`, `logs,
//! metrics, etc. in one place`). A paragraph of six sentences that holds
//! one such abbreviation therefore draws a finding.
//!
//! Measurement
//!
//! The rule hit 69 lines in the measure.py docs corpus, and 13 of 19
//! sampled hits carried a second topic or a hidden list. Five misfires
//! kept to one topic, and one counted the closing tag of a Hugo
//! shortcode (`{{< /note >}}`) as a sentence.

use crate::checks::Hit;
use crate::checks::recommended::prose_hidden_list::LABEL;
use crate::checks::sentences::{End, continues_sentence, ends_sentence, words};
use crate::markdown::{Block, BlockKind, Markdown, blank};

/// The most sentences that a paragraph holds without a finding.
const MAX_SENTENCES: usize = 6;

/// Returns the finding of one paragraph, if any.
fn paragraph(model: &Markdown, block: &Block) -> Option<Hit> {
    let mut sentences = 0;
    let mut open = false;
    let mut quote_end = false;
    let mut start = None;
    for (n, (i, text)) in model.block_lines(block).enumerate() {
        let text = if n == 0 { blank(&text, &LABEL) } else { text };
        for (offset, word) in words(&text) {
            // The word before ended in a mark and a closing quote. The
            // sentence ended there unless this word continues it.
            if quote_end && !continues_sentence(word) {
                sentences += 1;
                open = false;
            }
            quote_end = false;
            if word.contains(|c: char| c.is_ascii_alphabetic()) {
                start.get_or_insert_with(|| model.lines[i].at(&text, offset));
                open = true;
            }
            match if open { ends_sentence(word) } else { End::No } {
                End::Yes => {
                    sentences += 1;
                    open = false;
                }
                End::Quote => quote_end = true,
                End::No => {}
            }
        }
    }
    if open {
        sentences += 1;
    }
    (sentences > MAX_SENTENCES).then(|| Hit {
        at: start.unwrap_or(block.chars.start),
        message: format!(
            "Paragraph of {sentences} sentences. Split it so each paragraph carries one topic."
        ),
    })
}

/// Returns a finding for each paragraph of more than six sentences.
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
            /// The line, the column, and the count of sentences of each
            /// finding.
            want: Vec<(usize, usize, usize)>,
        }
        let cases = [
            Case {
                name: "definition in a tight definition list",
                text: "Term\n: One. Two. Three. Four. Five. Six. Seven.\n",
                want: vec![(2, 3, 7)],
            },
            Case {
                name: "blockquote",
                text: "> He wrote \"Done.\" every time. One. Two. Three. Four. Five.\n> Six \"Stop!\"\n> every time.\n",
                want: vec![(1, 3, 7)],
            },
            Case {
                name: "definition of six sentences",
                text: "Term\n: One. Two. Three. Four. Five. Six.\n",
                want: vec![],
            },
        ];
        for case in cases {
            let region = Region::markdown(case.text);
            let got: Vec<_> = check(&Markdown::new(&region))
                .into_iter()
                .map(|hit| {
                    let at = region.position(hit.at);
                    let count = hit
                        .message
                        .split(' ')
                        .nth(2)
                        .and_then(|n| n.parse().ok())
                        .unwrap_or(0);
                    (at.line, at.col, count)
                })
                .collect();
            assert_eq!(got, case.want, "{}", case.name);
        }
    }
}
