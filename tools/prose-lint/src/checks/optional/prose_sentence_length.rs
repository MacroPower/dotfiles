//! Overview
//!
//! Reports a sentence of 26 to 32 words that joins two clauses, in
//! markdown prose. A clause is a subject and its verb, such as `the tests
//! pass`. A sentence that long makes the reader hold its opening in mind
//! until its end. The technical-writing skill splits a sentence past about
//! 25 words. It keeps a long one that carries a single fact with its
//! condition or consequence. The fix splits the sentence between the two
//! clauses, and the message names where. The rule sits at Optional,
//! because a sentence in this band that carries a single fact also
//! matches, so the finding is a suggestion.
//!
//! Expression
//!
//! The check reads the sentences that `sentences` in
//! `src/checks/sentences.rs` finds in each paragraph block of the markdown
//! model. `count_words` there counts the words. Each part of a hyphenated
//! word counts (`read-only` is two words), and a token with no letter
//! (`42`, `|`) counts as none. The model blanks code spans, so a span
//! counts as none either. Each finding names the line and column where the
//! sentence starts.
//!
//! A sentence joins two clauses when it holds a semicolon, a colon after
//! its third word, or a comma before a word in `JOINERS` (`, and`,
//! `, which`, `, because`).
//!
//! The check drops the punctuation around each word that it compares, a
//! straight `'` included, so `'If`, `"If`, and `\u{2018}If` each count as
//! `if`.
//!
//! The message names where to split, with the steps of the Recommended
//! ProseSentenceLength, whose `advice` it calls.
//!
//! False Positives
//!
//! A colon closer to the start of the sentence ends a label (`**Note**:`,
//! `Step 3:`), so it does not count as a join. `, or` stays out of
//! `JOINERS`, because it mostly ends a serial list (`AVIF, PNG, or WebP`).
//! `, then` after a sentence that opens with `If` or `When` stays out too,
//! because it closes the condition.
//!
//! Related Rules
//!
//! The Recommended ProseSentenceLength reports every sentence of more than
//! 32 words. The Optional ProseSentenceLength reports a sentence of 26 to
//! 32 words that joins two clauses, so one sentence never draws both
//! findings.
//!
//! Measurement
//!
//! The rule hit 1,574 lines in the measure.py corpus, and 14 of 20 sampled
//! hits read better as two sentences. The misfires were serial lists, a
//! single fact with its condition or reason, and a colon inside
//! parentheses (`(for example: ...)`). The first step named a split point
//! on 219 lines, and 19 of 20 sampled points were the right place to
//! split. The second step named a clause on 256 lines, and 19 of 20
//! sampled clauses read well as a sentence of their own.

use crate::checks::Hit;
use crate::checks::recommended::prose_sentence_length::advice;
use crate::checks::sentences::{Token, bare, count_words, sentences};
use crate::markdown::Markdown;

/// The fewest words of a sentence that the check reports.
const MIN_WORDS: usize = 26;

/// The most words of a sentence that the check reports. The Recommended
/// ProseSentenceLength reports a longer one.
const MAX_WORDS: usize = 32;

/// The word limit that the message names.
const LIMIT: usize = 25;

/// The words after a comma that join two clauses.
const JOINERS: &[&str] = &[
    "and", "but", "so", "then", "which", "who", "where", "while", "because", "although", "though",
    "whereas", "since",
];

/// Returns whether the sentence `tokens`, whose first word sits at token
/// `from`, joins two clauses: a comma before a word of [`JOINERS`], a
/// semicolon, or a colon after the third word.
fn has_join(tokens: &[Token], from: usize) -> bool {
    let condition = matches!(bare(&tokens[from].text).as_str(), "if" | "when");
    let mut seen = 0;
    for pair in tokens.windows(2) {
        let (t, next) = (pair[0].text.as_str(), bare(&pair[1].text));
        if t.contains(|c: char| c.is_ascii_alphabetic()) {
            seen += 1;
        }
        if t.ends_with(',') && JOINERS.contains(&next.as_str()) && !(condition && next == "then") {
            return true;
        }
        if t.ends_with(';') || (t.ends_with(':') && seen >= 3) {
            return true;
        }
    }
    false
}

/// Returns a finding for each sentence of 26 to 32 words that joins two
/// clauses.
pub fn check(model: &Markdown) -> Vec<Hit> {
    sentences(model)
        .iter()
        .filter_map(|tokens| {
            let (from, words) = count_words(tokens);
            let from = from?;
            ((MIN_WORDS..=MAX_WORDS).contains(&words) && has_join(tokens, from)).then(|| Hit {
                at: tokens[from].at,
                message: format!(
                    "Sentence of {words} words, past the {LIMIT}-word limit. {}",
                    advice(tokens, from)
                ),
            })
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::region::Region;

    /// Returns the line and the column of each finding on `text`.
    fn found(text: &str) -> Vec<(usize, usize)> {
        let region = Region::markdown(text);
        check(&Markdown::new(&region))
            .into_iter()
            .map(|hit| {
                let at = region.position(hit.at);
                (at.line, at.col)
            })
            .collect()
    }

    #[test]
    fn sentences() {
        struct Case {
            name: &'static str,
            text: &'static str,
            /// The line and the column of each finding.
            want: Vec<(usize, usize)>,
        }
        let cases = [
            // The second and third sentences of the single-quotes test of
            // the bash prose-lint.
            Case {
                name: "a comma before `and`",
                text: "\"If the cache is cold when the node restarts, the scheduler moves every pod to the spare pool, and the team waits before it opens the gate.\" the runbook says.\n",
                want: vec![(1, 1)],
            },
            Case {
                name: "`, then` after a condition",
                text: "'If the cache is cold when the node restarts after the upgrade, then the scheduler moves every pod to the spare pool before it opens the gate.' the runbook says.\n",
                want: vec![],
            },
            Case {
                name: "a semicolon",
                text: "The scheduler moves every pod on the node to the spare pool; the team then waits for the warmup to finish before it opens the gate.\n",
                want: vec![(1, 1)],
            },
            Case {
                name: "a colon after a label",
                text: "Note: the scheduler moves every pod on the node to the spare pool and the team then waits for the warmup to finish before it opens the gate.\n",
                want: vec![],
            },
        ];
        for case in cases {
            assert_eq!(found(case.text), case.want, "{}", case.name);
        }
    }
}
