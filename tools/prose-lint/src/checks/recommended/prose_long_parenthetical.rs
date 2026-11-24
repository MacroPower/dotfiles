//! Overview
//!
//! A parenthetical is an aside that interrupts a sentence. It sits in
//! parentheses (`the pool (which the scheduler sizes once at startup from
//! the node count) holds the workers`), or in commas after the subject
//! (`The hook, which runs after every Write and Edit of a markdown file,
//! tags each finding`). The rule reports a long parenthetical in the middle
//! of a sentence in markdown prose.
//!
//! The reader holds the sentence open across the aside. An aside between a
//! subject and its verb costs the most, because the reader holds the
//! subject until the verb comes. A long aside reads better as a sentence of
//! its own after this one, so the fix moves it there. The rule sits at
//! Recommended, because the comma form finds the subject and the end of the
//! aside from word lists. A stray comma also matches.
//!
//! Expression
//!
//! The check reads the sentences that `sentences` in
//! `src/checks/sentences.rs` finds in each paragraph block of the markdown
//! model. It reports an aside of 10 or more words. The model blanks code
//! spans, so a span counts as no words. Each part of a hyphenated word
//! counts, the way ProseSentenceLength counts it. A Hugo shortcode (`{{<
//! param "version" >}}`) counts as no words. Each finding names the line
//! and column where the aside opens.
//!
//! A parenthesized aside counts when a lowercase word follows it, a sign
//! that the sentence goes on.
//!
//! A comma aside needs a short subject at the start of the sentence, with
//! its comma within five words. The subject opens with a capitalized
//! determiner, a word that introduces a noun, such as `the`, `a`, or
//! `this`. `Each` and `Any` are determiners too. The subject can also be a
//! code span, a bare URL, or a capitalized name, which needs `which` or
//! `who` after the comma. The aside ends at the next comma, and a lowercase
//! word after it must carry on the sentence.
//!
//! The check drops the punctuation around each word that it compares, a
//! straight `'` included, so `'The`, `"The`, and `\u{2018}The` each count
//! as the determiner `the`.
//!
//! False Positives
//!
//! A parenthesized aside at the end of a sentence passes.
//!
//! After a comma aside, a joining word (`and`, `because`, `until`), a
//! pronoun such as `it`, a determiner, or an `-ing` word does not carry on
//! the sentence. It starts a list item or a new clause, a subject and its
//! verb, such as `the tests pass`. The check skips such an aside. The check
//! also skips an opener with a verb before the first comma (`The results
//! were alarming,`) and a time or manner phrase (`This time,`).
//!
//! A stray comma between a subject and its verb still reads as the start of
//! a comma aside (`This application written in Go, simplifies`), and the
//! rule accepts that misfire.
//!
//! Measurement
//!
//! The rule hit 90 lines in the measure.py corpus, and 20 of 20 sampled
//! hits held the sentence open across an aside of a clause or a list.
//!
//! The comma form produced 7 of the 90 hits, and 6 of the 7 were asides.
//! The seventh hit fell outside the sample.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::checks::sentences::{Token, bare, sentences, words_in};
use crate::markdown::Markdown;

/// The fewest words of an aside that the check reports.
const MIN_WORDS: usize = 10;

/// The determiners that open the subject of a comma aside.
const DETERMINERS: &[&str] = &[
    "the", "a", "an", "this", "that", "these", "those", "each", "every", "its", "our", "your",
    "their", "any", "all", "some", "most", "no",
];

/// The verbs that make the words before the first comma a clause.
const VERBS: &[&str] = &[
    "is", "are", "was", "were", "be", "been", "should", "can", "will", "would", "could", "must",
    "may", "might", "has", "have", "had", "do", "does", "did", "include", "includes",
];

/// The words of a time or manner phrase before the first comma.
const PHRASES: &[&str] = &[
    "time", "way", "year", "week", "day", "point", "case", "end", "least", "most",
];

/// The words after a comma aside that start a new clause or a list item.
const STOPS: &[&str] = &[
    "and", "or", "but", "the", "a", "an", "this", "that", "it", "they", "we", "you", "which",
    "who", "so", "then", "for", "in", "on", "at", "to", "of", "with", "as", "if", "when",
    "because", "until", "before", "after", "unless", "although", "though", "since", "while",
    "once", "whereas",
];

const MESSAGE: &str = "Move the aside to its own sentence after this one, and name its subject.";

static SHORTCODE_OPEN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\{\{[<%]").expect("the pattern is valid"));
static SHORTCODE_CLOSE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[>%]\}\}").expect("the pattern is valid"));
static NAME: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[^A-Za-z]*[A-Z][A-Za-z0-9]*,$").expect("the pattern is valid"));
static LOWERCASE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[^A-Za-z]*[a-z]").expect("the pattern is valid"));

/// Returns the text of each token of `tokens` without the Hugo shortcodes,
/// which can run across several tokens (`{{<`, `param`, `"version"`,
/// `>}}`).
fn strip_shortcodes(tokens: &[Token]) -> Vec<String> {
    let mut open = false;
    tokens
        .iter()
        .map(|token| {
            let mut t = token.text.as_str();
            let mut out = String::new();
            while !t.is_empty() {
                if open {
                    match SHORTCODE_CLOSE.find(t) {
                        Some(m) => {
                            t = &t[m.end()..];
                            open = false;
                        }
                        None => t = "",
                    }
                } else if let Some(m) = SHORTCODE_OPEN.find(t) {
                    out.push_str(&t[..m.start()]);
                    t = &t[m.end()..];
                    open = true;
                } else {
                    out.push_str(t);
                    t = "";
                }
            }
            out
        })
        .collect()
}

/// Returns a hit at index `at` for an aside of `words` words, when the
/// aside is long enough.
fn report(at: usize, words: usize) -> Option<Hit> {
    (words >= MIN_WORDS).then(|| Hit {
        at,
        message: format!("Long parenthetical of {words} words. {MESSAGE}"),
    })
}

/// Returns the findings of the asides in parentheses of one sentence.
fn parens(tokens: &[Token], ct: &[String]) -> Vec<Hit> {
    let n = tokens.len();
    let mut hits = Vec::new();
    let mut i = 0;
    while i < n {
        let Some(open) = ct[i].find('(') else {
            i += 1;
            continue;
        };
        let mut depth = 0i32;
        let mut words = 0;
        let mut j = i;
        while j < n {
            let t = if j == i { &ct[j][open..] } else { &ct[j][..] };
            words += words_in(t);
            depth += t.matches('(').count() as i32 - t.matches(')').count() as i32;
            if depth <= 0 {
                break;
            }
            j += 1;
        }
        if j + 1 < n && LOWERCASE.is_match(&tokens[j + 1].text) {
            let before = tokens[i]
                .text
                .find('(')
                .map_or(0, |p| tokens[i].text[..p].chars().count());
            hits.extend(report(tokens[i].at + before, words));
        }
        i = j + 1;
    }
    hits
}

/// Returns the finding of the comma aside of one sentence, if any.
fn commas(tokens: &[Token], ct: &[String]) -> Option<Hit> {
    let n = tokens.len();
    let w = |i: usize| tokens.get(i).map_or_else(String::new, |t| bare(&t.text));
    let text = |i: usize| tokens[i].text.as_str();
    let first = tokens
        .iter()
        .position(|t| t.text.contains(|c: char| c.is_ascii_alphabetic()))?;
    let mut open = None;
    let bare_comma = text(0).ends_with(',') && !text(0).contains(|c: char| c.is_ascii_alphabetic());
    if bare_comma || (first == 0 && NAME.is_match(text(0))) {
        // A code span or a bare URL, which the model blanks to a bare
        // comma, or a name.
        if matches!(w(1).as_str(), "which" | "who" | "whose") {
            open = Some(0);
        }
    } else if text(first)
        .trim_start_matches(|c: char| !c.is_ascii_alphabetic())
        .starts_with(|c: char| c.is_ascii_uppercase())
        && DETERMINERS.contains(&w(first).as_str())
        && !matches!(w(first + 1).as_str(), "more" | "less" | "fewer" | "longer")
    {
        for i in first + 1..(first + 5).min(n) {
            if text(i).contains(['.', ';', ':', '(', ')']) || VERBS.contains(&w(i).as_str()) {
                return None;
            }
            if text(i).ends_with(',') {
                open = Some(i);
                break;
            }
        }
        if open.is_some_and(|o| PHRASES.contains(&w(o).as_str())) {
            return None;
        }
    }
    let open = open?;
    if matches!(w(open + 1).as_str(), "and" | "or") {
        return None;
    }
    let mut words = 0;
    let mut j = open + 1;
    while j < n {
        if text(j).ends_with([';', ':', '.']) && j + 1 < n {
            return None;
        }
        words += words_in(&ct[j]);
        if text(j).ends_with(',') {
            break;
        }
        j += 1;
    }
    let next = w(j + 1);
    if j + 1 >= n
        || !LOWERCASE.is_match(text(j + 1))
        || STOPS.contains(&next.as_str())
        || next.ends_with("ing")
    {
        return None;
    }
    report(tokens[open].at, words)
}

/// Returns a finding for each long parenthetical in the middle of a
/// sentence.
pub fn check(model: &Markdown) -> Vec<Hit> {
    let mut hits = Vec::new();
    for tokens in sentences(model) {
        let ct = strip_shortcodes(&tokens);
        hits.extend(parens(&tokens, &ct));
        hits.extend(commas(&tokens, &ct));
    }
    hits
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
                name: "parens before a lowercase word",
                text: "The pool (which the scheduler sizes once at startup from the node count) holds the workers.\n",
                want: vec![(1, 10)],
            },
            Case {
                name: "parens at the end of a sentence",
                text: "The pool holds the workers (which the scheduler sizes once at startup from the node count).\n",
                want: vec![],
            },
            // The last sentence of the single-quotes test of the bash
            // prose-lint.
            Case {
                name: "a comma aside after a quoted subject",
                text: "'The hook, which runs after every Write and Edit of a markdown file or a source file, tags each finding' is the rule.\n",
                want: vec![(1, 6)],
            },
            Case {
                name: "a bare URL as the subject",
                text: "https://example.com/docs, which the team moved to a new host in the spring of last year, now serves the guide.\n",
                want: vec![(1, 25)],
            },
            Case {
                name: "a Hugo shortcode",
                text: "The pool ({{< param \"version\" >}} and {{< param \"name\" >}} here) holds the workers.\n",
                want: vec![],
            },
        ];
        for case in cases {
            assert_eq!(found(case.text), case.want, "{}", case.name);
        }
    }
}
