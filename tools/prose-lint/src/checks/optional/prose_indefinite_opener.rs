//! Overview
//!
//! Grammar calls `a` and `an` the indefinite article, because each
//! introduces a noun without pointing at a particular one. It calls `the`
//! the definite article. The rule reports a sentence in markdown prose
//! that opens with `a` or `an` and makes a claim about a whole class (`A
//! reader opens one rule file at a time`).
//!
//! The singular `a reader` reads two ways, as one particular reader or as
//! any reader. The plural reads one way (`Readers open one rule file at a
//! time`). The prose skill writes a claim about a whole class with a
//! plural subject or with `every`, and the fix does the same. The prose
//! skill keeps `a` and `an` for one instance that the sentence
//! introduces. The rule sits at Optional, because the check guesses the
//! verb from its spelling, and a sentence that does not name a class
//! also matches.
//!
//! Expression
//!
//! The check reads each paragraph block of the markdown model, and the
//! text of each list item counts as a paragraph of its own. The sentence
//! boundaries follow ProseParagraphLength, run-in labels and ellipses
//! included. Sentences that open with a code span open with the first
//! word after it, because the model blanks the span. Each finding names
//! the line and column of the sentence's first word.
//!
//! The check reports two forms. The first is an opening `a` or `an` with
//! a present-tense verb in the next four words of the sentence (`A task
//! is`, `A failed task can retry`). The check has no part-of-speech
//! tagger, so a lowercase word that ends in `s` counts as the verb. `is`,
//! `has`, `can`, and the other helping verbs in `HELPERS` count too. The
//! scan stops at a comma and at a word in `STOPS` that ties the subject
//! to more words, such as `of`, `with`, or `and`, so `A list of tasks`
//! passes. A comma stops the scan before a straight or a curly closing
//! quote too (`"A lease," the guide says`). So does a period, a question
//! mark, or an exclamation mark before a closing quote, even when a
//! lowercase word after the quote continues the sentence (`"A lease?"
//! the guide asks`).
//!
//! The check holds each finding of the first form until the paragraph
//! ends, and drops it when the text after it names the noun with `the`.
//!
//! The second form is the second of two sentences in a row that open with
//! `a` or `an` in one paragraph (`A task is a unit of work. A task runs
//! on one worker.`). It catches openers that the verb test misses. Each
//! run reports once however long it grows, and its later sentences skip
//! the verb test. A sentence that opens with a quotation and goes on past
//! a quoted `.`, `?`, or `!` does not open a run (`"A lease?" the guide
//! asks.`). The subject of such a sentence follows the quotation.
//!
//! False Positives
//!
//! Definitions pass (`A task is a unit of work`). When the verb is `is`
//! and `a`, `an`, `the`, or `one` follows it, the sentence names a term,
//! and no reader takes the subject for one particular task. Glossaries
//! open each entry this way, and the plural reads worse there (`Tasks are
//! units of work`). The skip drops 156 of 602 hits in the measure.py docs
//! corpus, and 20 of 20 sampled drops were definitions.
//!
//! A sentence that introduces one instance takes the same form as a claim
//! about a class (`A cache sits in front of the store`), and a later
//! sentence names the instance with `the` (`The cache holds the last
//! hundred reads`). Such a sentence passes.
//!
//! The first sentence of a definition of a definition list passes,
//! because it describes the term above it (`Term`, `: A task runs on
//! one worker.`). The check still reads the later sentences of the
//! definition.
//!
//! Past-tense verbs do not end in `s`, so `A user reported the bug`
//! passes, and so does a quantity after the article (`A few seconds
//! later`).
//!
//! The check reports a run at the first word of its second sentence,
//! before it reads the rest of that sentence. A sentence that opens with
//! a quotation and goes on after it therefore still draws the run
//! finding when it follows a sentence that opens with `a` or `an` (`A
//! lease is a lock. "A lease?" the guide asks.`).
//!
//! A sentence that opens with a quotation that ends in a comma still
//! opens a run, although its subject follows the quotation. Two such
//! sentences in a row draw the run finding (`"A lease," the guide says
//! before the build. "A lease," the guide says after the tests.`).
//!
//! List labels and speaker labels that are the letter `a` read as the
//! article (`**(a)** You must give any other recipients`, `**A: That
//! sounds interesting!**`).
//!
//! Related Rules
//!
//! ProseHiddenList reports three sentences in a row that open with the
//! same two words. It skips openings with `a` or `an`, so only this rule
//! reports those sentences. A sentence that opens with a quotation and
//! goes on past a quoted `.`, `?`, or `!` does not open a run here (`"A
//! lease?" the guide asks`). ProseHiddenList therefore compares its
//! opening too. Three sentences in a row that open with `"A lease?"` draw
//! a finding only from ProseHiddenList, unless a sentence that opens with
//! `a` or `an` comes right before them. When a present-tense verb sits
//! inside the quotation (`"A lease is held?" the guide asks`), this rule
//! reports each of the three sentences too.
//!
//! Measurement
//!
//! The rule hit 443 lines in the measure.py docs corpus, and 15 of 20
//! sampled hits made a claim about a whole class. Three misfires
//! introduced one instance (`A TLS certificate will be automatically
//! obtained for you`), and two did not name a class (`A simple case is
//! to create one object`, `A typical PostFilter implementation is
//! preemption`).

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::checks::optional::prose_opening_pronoun::DEFINITION;
use crate::checks::recommended::prose_hidden_list::LABEL;
use crate::checks::sentences::{End, continues_sentence, ends_sentence, opens_quote, words};
use crate::markdown::{Block, BlockKind, Markdown, blank};

/// The words after `a` that name a quantity and not a subject (`A few
/// seconds later`).
const QUANTITIES: &[&str] = &[
    "few", "couple", "lot", "number", "bit", "little", "series", "set", "list", "pair", "total",
    "variety", "range", "handful", "dozen", "hundred", "thousand", "majority", "minority", "while",
    "year", "day", "week", "month", "minute", "second", "moment", "time", "single",
];

/// The words that tie the subject to more words, so the scan for a verb
/// stops at them (`A list of tasks`).
const STOPS: &[&str] = &[
    "of", "for", "with", "in", "on", "at", "to", "from", "by", "that", "which", "who", "whose",
    "where", "when", "if", "and", "or", "but", "as", "than", "like", "about", "into", "over",
    "under", "between", "per", "without", "within", "was", "were", "had", "did", "could", "would",
    "are",
];

/// The helping verbs that count as a present-tense verb.
const HELPERS: &[&str] = &[
    "is", "has", "does", "can", "cannot", "must", "should", "may", "might", "will", "needs",
];

/// The words that end in `s` but are not a verb.
const NOT_VERBS: &[&str] = &[
    "always",
    "sometimes",
    "perhaps",
    "afterwards",
    "towards",
    "besides",
    "unless",
    "thus",
    "whereas",
    "across",
    "plus",
    "its",
    "yes",
    "was",
    "news",
];

/// The closing marks that a comma, a semicolon, a colon, or a paren can
/// carry after it.
const CLOSERS: &str = "[\"')*_\\]\u{201d}\u{2019}]*$";

/// A word that ends in a comma, a semicolon, a colon, or a paren, which
/// ends the subject.
static SUBJECT_END: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(&format!("[,;:()]{CLOSERS}")).expect("the pattern is valid"));

/// An `is` that ends in a mark, which ends its clause.
static CLAUSE_END: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(&format!("[,;:.]{CLOSERS}")).expect("the pattern is valid"));

const VERB_MESSAGE: &str = "Indefinite opener. `a` or `an` opens a sentence before a present-tense verb. Write a claim about a whole class with a plural subject or with `every`.";
const RUN_MESSAGE: &str = "Indefinite opener. `a` or `an` opens two sentences in a row. Write a claim about a whole class with a plural subject or with `every`.";

/// Returns whether the word `token`, which reads as `word` without its
/// marks, is a present-tense verb. A lowercase word that ends in `s`
/// counts as one.
fn verb(token: &str, word: &str) -> bool {
    if HELPERS.contains(&word) {
        return true;
    }
    let lowercase = token
        .chars()
        .find(char::is_ascii_alphabetic)
        .is_some_and(|c| c.is_ascii_lowercase());
    lowercase
        && word.len() > 1
        && word.ends_with('s')
        && word.chars().all(|c| c.is_ascii_lowercase())
        && !NOT_VERBS.contains(&word)
        && !["ss", "us", "is", "ics", "ous", "ness"]
            .iter()
            .any(|end| word.ends_with(end))
}

/// A finding of the first form, which waits for the end of the
/// paragraph.
struct Held {
    /// The index in the region of the `a` or `an`.
    at: usize,
    /// The noun before the verb.
    noun: String,
    /// The byte offset in the text of the paragraph after the verb.
    after: usize,
}

/// Returns the findings of one paragraph.
fn paragraph(model: &Markdown, block: &Block) -> Vec<Hit> {
    let mut hits = Vec::new();
    let mut held: Vec<Held> = Vec::new();
    // The text of the paragraph so far, in lowercase.
    let mut para = String::new();
    // The count of sentences in a row that open with `a` or `an`.
    let mut run = 0;
    // The first sentence of a definition of a definition list describes
    // the term above it (`Term`, `: A task runs on one worker.`), so it
    // opens neither a scan nor a run.
    let mut opening = !model
        .block_lines(block)
        .next()
        .is_some_and(|(i, _)| DEFINITION.is_match(&model.lines[i].source));
    // How many words of the subject the scan for a verb has read, or 0
    // when no scan runs.
    let mut scan = 0;
    let mut scan_at = 0;
    let mut noun = String::new();
    let mut pending_def = false;
    let mut quote_end = false;
    let mut quoted = false;
    for (n, (i, text)) in model.block_lines(block).enumerate() {
        let text = if n == 0 { blank(&text, &LABEL) } else { text };
        let base = para.len() + 1;
        for (offset, token) in words(&text) {
            // The word before ended in a mark and a closing quote. The
            // sentence ended there unless this word continues it. When
            // the sentence opened with the quotation, its first word sits
            // inside the quotation, so the sentence does not open a run.
            if quote_end {
                if !continues_sentence(token) {
                    opening = true;
                } else if quoted {
                    run = 0;
                }
            }
            quote_end = false;
            if !token.contains(|c: char| c.is_ascii_alphabetic()) {
                let end = if opening {
                    End::No
                } else {
                    ends_sentence(token)
                };
                if end != End::No {
                    pending_def = false;
                    scan = 0;
                }
                match end {
                    End::Yes => opening = true,
                    End::Quote => quote_end = true,
                    End::No => {}
                }
                continue;
            }
            let word = token
                .to_lowercase()
                .trim_matches(|c: char| !c.is_ascii_lowercase())
                .to_string();
            if pending_def {
                pending_def = false;
                if matches!(word.as_str(), "a" | "an" | "the" | "one") {
                    held.pop();
                }
            }
            if opening {
                opening = false;
                quoted = opens_quote(token);
                if word == "a" || word == "an" {
                    run += 1;
                    if run == 2 {
                        hits.push(Hit {
                            at: model.lines[i].at(&text, offset),
                            message: RUN_MESSAGE.to_string(),
                        });
                    } else if run == 1 {
                        scan = 1;
                        scan_at = model.lines[i].at(&text, offset);
                    }
                } else {
                    run = 0;
                }
            } else if scan > 0 {
                if scan == 1 && QUANTITIES.contains(&word.as_str()) {
                    scan = 0;
                } else if scan >= 2 && verb(token, &word) {
                    held.push(Held {
                        at: scan_at,
                        noun: noun.clone(),
                        after: base + offset + token.len(),
                    });
                    scan = 0;
                    // A mark after `is`, such as the period in `is."`, ends
                    // its clause, so the word after it does not complete a
                    // definition.
                    if word == "is" && !CLAUSE_END.is_match(token) {
                        pending_def = true;
                    }
                } else if (scan >= 2 && STOPS.contains(&word.as_str())) || scan >= 4 {
                    scan = 0;
                } else {
                    noun = word.split('\'').next().unwrap_or_default().to_string();
                    scan += 1;
                }
                if scan > 0 && SUBJECT_END.is_match(token) {
                    scan = 0;
                }
            }
            // The closing quote also ends the subject, whether or not the
            // sentence goes on after it (`"A lease?" the guide asks`).
            let end = ends_sentence(token);
            if end != End::No {
                scan = 0;
            }
            match end {
                End::Yes => opening = true,
                End::Quote => quote_end = true,
                End::No => {}
            }
        }
        para.push(' ');
        para.push_str(&text.to_ascii_lowercase());
    }
    // A later `the` before the noun names one instance that the sentence
    // introduced.
    for h in held {
        let named = Regex::new(&format!(
            r"(?:^|[^a-z])the[ \t]+{}(?:[^a-z]|$)",
            regex::escape(&h.noun)
        ))
        .is_ok_and(|re| re.is_match(&para[h.after.min(para.len())..]));
        if !named {
            hits.push(Hit {
                at: h.at,
                message: VERB_MESSAGE.to_string(),
            });
        }
    }
    hits.sort_by_key(|h| h.at);
    hits
}

/// Returns a finding for each sentence that opens with `a` or `an` and
/// makes a claim about a whole class.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .blocks
        .iter()
        .filter(|b| b.kind == BlockKind::Paragraph)
        .flat_map(|b| paragraph(model, b))
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
                name: "verb on the line of the article",
                text: "A worker reads the queue with\n`queue --list`\nand picks the oldest entry.\n",
                want: vec![(1, 1)],
            },
            Case {
                name: "later sentence that names the noun with the",
                text: "A cache sits in front of the store.\nThe cache holds the last hundred reads.\n",
                want: vec![],
            },
            Case {
                name: "run of two sentences in a blockquote",
                text: "> A task is a unit of work.\n> A task runs on one worker.\n",
                want: vec![(2, 3)],
            },
            Case {
                name: "definition under its term",
                text: "The pod holds the cache.\n\nTerm\n: A task runs on one worker.\n",
                want: vec![],
            },
            Case {
                name: "later sentences of a definition",
                text: "Term\n: A task runs on one worker. A lease holds the task.\n",
                want: vec![(2, 30)],
            },
            Case {
                name: "separate paragraphs open no run",
                text: "A task is a unit of work.\n\nA lease is a lock on one task.\n",
                want: vec![],
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
