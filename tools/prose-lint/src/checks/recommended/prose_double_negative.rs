//! Overview
//!
//! A double negative puts two negative words in one clause, a subject and
//! its verb, such as `the tests pass`. The reader must cancel the two to
//! find the claim (`the hook does not run unless the caller holds the
//! lock`). `unless` means `if not`, so it counts as the second negative.
//! The fix writes the positive form, which states the claim directly (`the
//! hook runs only when the caller holds the lock`). The rule sits at
//! Recommended, because a `without` that describes the noun before it also
//! matches.
//!
//! Expression
//!
//! The check reads the sentences that `sentences` in
//! `src/checks/sentences.rs` finds in each paragraph block of the markdown
//! model. The negative words are `not`, `never`, `no`, `cannot`, `none`,
//! `nothing`, `neither`, `nor`, `nobody`, and every word that ends in
//! `n't`. The check drops the punctuation around each word, a straight `'`
//! included, so `'Never` counts as `never` and `nothing'` as `nothing`, the
//! same as in double quotes. It reads a curly apostrophe inside a word as a
//! straight one, so `can\u{2019}t` counts the same as `can't`. The check
//! reports three forms:
//!
//! - `unless` with a negative word in the eight words before it. The scan
//!   stops at `and`, `but`, `so`, `or`, and `yet`, which open a new
//!   clause, and it skips the words inside parentheses, whose negation
//!   stays inside them. It also stops at a word that ends in a period, a
//!   question mark, or an exclamation mark and a closing quote (`says
//!   "Never!" unless the lock is held`). The quotation holds a clause of
//!   its own. The scan also stops at the closing quote of a quotation that
//!   opens the sentence, such as the text of a sign (`"Do not disturb"
//!   stays on the door unless the room is free`). The negation of that
//!   quotation stays inside it. A sentence that opens with `Do not`,
//!   `Don't`, or `Never` negates every step in it, so any `unless` outside
//!   parentheses counts, at any distance. The opener does not count when it
//!   sits inside a quotation that opens the sentence and closes before the
//!   `unless` (`"Don't panic!" appears in the log unless the run fails`).
//!   `quoted_opening` in `src/checks/sentences.rs` finds that quotation.
//!   In single quotes, the closing quote is the same mark as an apostrophe.
//!   `quotation_end` there can therefore end a quotation early at a word
//!   inside it that ends in an apostrophe, and an `unless` after that word
//!   passes.
//!
//!   An attribution can split the quotation that opens the sentence
//!   (`"Copy nothing," the guide says, "unless the tests pass."`). An
//!   `unless` in the second part then belongs to the quoted speech, so the
//!   opener counts for it. The scan from that `unless` skips the
//!   attribution and goes on into the first part, so the eight words count
//!   only the words of the quotation. `split_quotation` in
//!   `src/checks/sentences.rs` finds the second part, and its doc comment
//!   lists the tests that the words between the two parts must pass.
//! - `without` or `except` with a negative word in the six words before it
//!   (`cannot start without a certificate`). The scan stops at a word that
//!   opens a clause (`and`, `because`, `which`). It also stops at a word
//!   that ends in a comma, a colon, a semicolon, a period, a question mark,
//!   an exclamation mark, or `)`. A straight or a curly closing quote can
//!   follow the mark (`said "not now," then ran without a lock`).
//! - `not` before a word with a negative prefix from a list (`not unlike`,
//!   `not unsafe`). Here the positive form also makes the claim stronger
//!   (`not unsafe` becomes `safe`), so the writer must settle which claim
//!   they meant. The branch skips a `not` that ends in a comma, a colon, a
//!   semicolon, a period, a question mark, or an exclamation mark, before
//!   any closing quote. The mark closes the clause of `not`.
//!
//! False Positives
//!
//! `no matter` is not a negation, so it stays out. The prefix list leaves
//! out `unknown`, which names a value (`means zero, not unknown`).
//!
//! A `without` that names a property of the noun before it (`nodes without
//! swap`) still fires, because a word list cannot tell it from `cannot
//! start without a certificate`.
//!
//! In single quotes, a quotation that opens the sentence can also run past
//! its closing quote. A word that ends in `s` and the quote can be a plural
//! possessive, so `quoted_opening` passes over it. The quotation ends
//! instead at the first later word that ends in the quote without an `s`
//! before it, or with a mark after the quote. An `unless` before that word
//! then counts the opener.
//!
//! Two quoted strings read as a split quotation when a comma follows the
//! first string and the words between them. Those words must also pass the
//! tests of `split_quotation`. An `unless` in the second string then fires when
//! the first string opens with the opener, although the words between the
//! strings form a main clause (`"Don't panic," appears in red on the
//! screen, "Retry unless busy" in gray`).
//!
//! Related Rules
//!
//! ProseNotOnly reports `not only` and `not just` and their contracted
//! forms (`doesn't just`), so neither `not` nor such a contraction counts
//! as a negative word before `only` or `just`. `SHORT` holds the
//! contractions of the `short` list in ProseNotOnly. `can't` stays out of
//! both, because ProseNotOnly passes `can't just`, so `can't just run make
//! without the docs` still counts.
//!
//! The built-in LitotesDirectPositive reports `not` before `unusual`,
//! `unreasonable`, `unlikely`, `unimportant`, `infrequent`, `inaccurate`,
//! `irrelevant`, `unclear`, `uncommon`, `insignificant`, and `impossible`,
//! so the prefix list leaves those out.
//!
//! Measurement
//!
//! The rule hit 55 lines in the measure.py corpus, and 20 of 20 sampled
//! hits stacked two negatives.

use crate::checks::Hit;
use crate::checks::sentences::{
    End, Token, bare, ends_sentence, quotation_end, quoted_opening, split_quotation,
};
use crate::markdown::Markdown;

/// The negative words, other than those that end in `n't`.
const NEGATIVES: &[&str] = &[
    "not", "never", "no", "cannot", "none", "nothing", "neither", "nor", "nobody",
];

/// The words that end the scan before `without` or `except`.
const BOUNDARIES: &[&str] = &[
    "unless", "without", "except", "but", "and", "or", "nor", "yet", "so", "because", "if", "when",
    "while", "since", "although", "though", "whereas", "until", "which", "that",
];

/// The contractions that ProseNotOnly reports before `only` or `just`.
const SHORT: &[&str] = &[
    "don't",
    "doesn't",
    "didn't",
    "isn't",
    "aren't",
    "wasn't",
    "weren't",
    "couldn't",
    "won't",
    "wouldn't",
    "shouldn't",
    "mustn't",
    "hasn't",
    "haven't",
    "hadn't",
    "needn't",
];

/// The words with a negative prefix that the `not` branch reports.
const PREFIXED: &[&str] = &[
    "unlike",
    "unexpected",
    "unnecessary",
    "unsafe",
    "invalid",
    "incorrect",
    "inconsistent",
    "insecure",
    "improbable",
    "unaware",
    "uncommonly",
    "unreasonably",
    "unusually",
    "infrequently",
];

const MESSAGE: &str = "Double negative. State the positive case, so `does not run unless the caller holds the lock` becomes `runs only when the caller holds the lock`.";

/// Returns the bare word of token `i` of `tokens` with each curly
/// apostrophe straight, or an empty string past the end.
fn word(tokens: &[Token], i: usize) -> String {
    tokens
        .get(i)
        .map_or_else(String::new, |t| bare(&t.text).replace('\u{2019}', "'"))
}

/// Returns whether token `i` is a negative word.
fn negation(tokens: &[Token], i: usize) -> bool {
    let (w, next) = (word(tokens, i), word(tokens, i + 1));
    if w == "no" && next == "matter" {
        return false;
    }
    if (w == "not" || SHORT.contains(&w.as_str())) && (next == "only" || next == "just") {
        return false;
    }
    NEGATIVES.contains(&w.as_str()) || w.ends_with("n't")
}

/// Returns whether the token `token` ends in a comma, a colon, a
/// semicolon, a period, a question mark, or an exclamation mark, which
/// closes a clause. A straight or a curly closing quote, a paren, a
/// bracket, or emphasis can follow the mark.
fn closes_clause(token: &str) -> bool {
    token
        .trim_end_matches(['"', '\'', ')', '*', '_', ']', '\u{201d}', '\u{2019}'])
        .ends_with([',', ';', ':', '.', '?', '!'])
}

/// Returns whether the token `token` closes a paren, before at most a
/// mark, a closing quote, and another mark, and opens none.
fn closes_paren(token: &str) -> bool {
    if token.contains('(') {
        return false;
    }
    let t = token.strip_suffix([',', ';', ':', '.']).unwrap_or(token);
    let t = t
        .strip_suffix(['"', '\'', '\u{201d}', '\u{2019}'])
        .unwrap_or(t);
    let t = t.strip_suffix([',', ';', ':', '.']).unwrap_or(t);
    t.ends_with(')')
}

/// Returns the index of the token that the finding of one sentence names,
/// if any.
fn sentence(tokens: &[Token]) -> Option<usize> {
    let n = tokens.len();
    let w = |i| word(tokens, i);
    let opener = w(0) == "don't" || w(0) == "never" || (w(0) == "do" && w(1) == "not");
    // An opener inside a quotation that opens the sentence negates only
    // the steps inside the quotation (`"Don't panic!" appears unless`).
    let quoted = quoted_opening(tokens);
    // When an attribution splits that quotation, its second part runs
    // from token `resume` to token `resume_end`. A second part that no
    // token closes runs to the end of the sentence.
    let resume = split_quotation(tokens, quoted);
    let resume_end = resume.map(|r| quotation_end(tokens, r).unwrap_or(n - 1));
    let mut depth_at = Vec::with_capacity(n);
    let mut depth = 0i32;
    for token in tokens {
        depth_at.push(depth);
        depth += token.text.matches('(').count() as i32 - token.text.matches(')').count() as i32;
    }
    for i in 0..n {
        match w(i).as_str() {
            "unless" => {
                // An `unless` in the second part of a split quotation
                // belongs to the quoted speech, which the first part opens.
                let resumed = resume
                    .zip(resume_end)
                    .is_some_and(|(r, e)| i >= r && i <= e);
                if opener && depth_at[i] == 0 && quoted.is_none_or(|q| i < q || resumed) {
                    return Some(i);
                }
                let mut lo = i as isize - 8;
                let mut j = i as isize - 1;
                while j >= 0 && j >= lo {
                    // The attribution does not belong to the quoted
                    // speech, so the scan jumps from the second part to the
                    // end of the first part. The eight words then count
                    // only the words of the quotation.
                    if let (true, Some(r), Some(q)) = (resumed, resume, quoted)
                        && j as usize + 1 == r
                    {
                        lo -= j - q as isize;
                        j = q as isize;
                    }
                    let ju = j as usize;
                    // A quotation that closes with a mark holds a clause of
                    // its own, and so does a quotation that opens the
                    // sentence, for an `unless` outside its second part.
                    if ends_sentence(&tokens[ju].text) == End::Quote
                        || (quoted == Some(ju) && !resumed)
                    {
                        break;
                    }
                    // Skip back over the words in parentheses, whose
                    // negation stays inside them.
                    if closes_paren(&tokens[ju].text) {
                        while j > 0 && !tokens[j as usize].text.contains('(') {
                            j -= 1;
                        }
                        j -= 1;
                        continue;
                    }
                    if negation(tokens, ju) {
                        return Some(i);
                    }
                    if matches!(w(ju).as_str(), "and" | "but" | "so" | "or" | "yet") {
                        break;
                    }
                    j -= 1;
                }
            }
            "without" | "except" => {
                for j in (i.saturating_sub(6)..i).rev() {
                    // A negative word before the end of a clause belongs to
                    // that clause. The scan therefore stops at a word that
                    // closes one.
                    let t = &tokens[j].text;
                    if closes_clause(t) || t.ends_with(')') {
                        break;
                    }
                    if negation(tokens, j) {
                        return Some(i);
                    }
                    if BOUNDARIES.contains(&w(j).as_str()) {
                        break;
                    }
                }
            }
            "not" if PREFIXED.contains(&w(i + 1).as_str()) && !closes_clause(&tokens[i].text) => {
                return Some(i);
            }
            _ => {}
        }
    }
    None
}

/// Returns a finding for each sentence that holds a double negative.
pub fn check(model: &Markdown) -> Vec<Hit> {
    crate::checks::sentences::sentences(model)
        .iter()
        .filter_map(|tokens| {
            sentence(tokens).map(|i| Hit {
                at: tokens[i].at,
                message: MESSAGE.to_string(),
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
            Case {
                name: "`not` before `unless`",
                text: "The hook does not run unless the caller holds the lock.\n",
                want: vec![(1, 23)],
            },
            Case {
                name: "a curly apostrophe",
                text: "The hook can\u{2019}t start without a lock.\n",
                want: vec![(1, 22)],
            },
            Case {
                name: "a negation in parentheses",
                text: "The hook (not the agent) runs unless the lock is held.\n",
                want: vec![],
            },
            Case {
                name: "an opener at any distance",
                text: "Do not deploy the service to the cluster on a Friday afternoon after the freeze unless the team signs off.\n",
                want: vec![(1, 81)],
            },
            Case {
                name: "an opener inside a quotation",
                text: "\"Don't panic!\" appears in the log unless the run fails.\n",
                want: vec![],
            },
            Case {
                name: "the second part of a split quotation",
                text: "\"Copy nothing,\" the guide says, \"unless the tests pass.\"\n",
                want: vec![(1, 33)],
            },
            Case {
                name: "`no matter`",
                text: "It runs no matter what, unless it fails.\n",
                want: vec![],
            },
            Case {
                name: "a negative prefix",
                text: "The change is not unsafe.\n",
                want: vec![(1, 15)],
            },
        ];
        for case in cases {
            assert_eq!(found(case.text), case.want, "{}", case.name);
        }
    }
}
