//! Overview
//!
//! Reports a sentence of more than 32 words in markdown prose. A sentence
//! that long makes the reader hold its opening in mind until its end. The
//! technical-writing skill splits a sentence past about 25 words, and 3.8%
//! of the sentences in the measure.py docs run past 32. The fix splits the
//! sentence between two clauses, and the message names where. A clause is
//! a subject and its verb, such as `the tests pass`. The rule sits at
//! Recommended, because a few sentences of that length do not read better
//! as two.
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
//! The check drops the punctuation around each word that it compares, a
//! straight `'` included, so `'If`, `"If`, and `\u{2018}If` each count as `if`.
//!
//! The message names where to split, from the first of these steps that
//! fits the sentence:
//!
//! 1. A comma, then `and`, `but`, `so`, or `yet`, then a word that opens a
//!    clause, such as `the`, `it`, or `they`. The message names that split
//!    point. It says to drop an `and`, and to open the next sentence with a
//!    `but`, `so`, or `yet`. A comma among the four tokens before the comma
//!    marks a serial list (`futures, streams, and the syntax`), and
//!    `, so that` states a purpose, so neither counts. The step skips a
//!    sentence that opens with a condition or a cause (`If`, `When`,
//!    `Because`), also after `However,` or `For example,`. Its first comma
//!    usually closes the condition, not a thought.
//! 2. A comma, then `which`, `who`, or `where`. The message asks for a
//!    sentence that opens with the noun that the word stands for. A
//!    sentence that opens with `This` instead draws a ProseBroadReference
//!    finding.
//! 3. Neither. The message asks for a split between two thoughts, in the
//!    words of the technical-writing skill.
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
//! The rule hit 1,875 lines in the measure.py corpus, and 19 of 20 sampled
//! hits read better as two sentences. The other hit was an error of
//! lib/sentences.awk at a Hugo shortcode. The first step named a split
//! point on 137 lines, and 15 of 20 sampled points were the right place to
//! split. Two misfires were serial lists with long items. In the other
//! three, the `and` joined a second condition or a second result to an
//! `if` in the middle of the sentence. The split cut that part off from
//! the `if`. The second step named a clause on 154 lines, and 19 of 20
//! sampled clauses read well as a sentence of their own.

use crate::checks::Hit;
use crate::checks::sentences::{Token, bare, count_words, sentences};
use crate::markdown::Markdown;

/// The most words that a sentence holds without a finding.
const LIMIT: usize = 32;

/// The words that open a clause after a comma and a conjunction.
const SUBJECTS: &[&str] = &[
    "the", "this", "these", "those", "it", "they", "we", "you", "i", "he", "she", "there",
];

/// The words that open a condition or a cause.
const SUBORDINATORS: &[&str] = &[
    "if", "when", "whenever", "unless", "although", "though", "because", "since", "while",
    "whereas", "once",
];

/// Returns the bare word of token `i` of `tokens`, or an empty string
/// past the end.
fn word(tokens: &[Token], i: usize) -> String {
    tokens.get(i).map_or_else(String::new, |t| bare(&t.text))
}

/// Returns whether the sentence opens with a condition or a cause at
/// token `from`, after at most one lead-in such as `However,` or `For
/// example,`.
fn opens_subordinate(tokens: &[Token], from: usize) -> bool {
    let w = |i| word(tokens, i);
    SUBORDINATORS.contains(&w(from).as_str())
        || (tokens[from].text.ends_with(',') && SUBORDINATORS.contains(&w(from + 1).as_str()))
        || (w(from) == "for"
            && matches!(w(from + 1).as_str(), "example" | "instance")
            && SUBORDINATORS.contains(&w(from + 2).as_str()))
}

/// Returns whether a comma sits among the four tokens before token `i`,
/// the sign of a serial list (`futures, streams, and the syntax`).
fn serial_list(tokens: &[Token], i: usize) -> bool {
    tokens[i.saturating_sub(4)..i]
        .iter()
        .any(|t| t.text.ends_with(','))
}

/// Returns the advice of the message for a sentence whose first word
/// sits at token `from`. The Optional ProseSentenceLength gives the same
/// advice.
pub fn advice(tokens: &[Token], from: usize) -> String {
    let n = tokens.len();
    let w = |i| word(tokens, i);
    if !opens_subordinate(tokens, from) {
        for i in 1..n.saturating_sub(2) {
            let (c, s) = (w(i + 1), w(i + 2));
            if tokens[i].text.ends_with(',')
                && matches!(c.as_str(), "and" | "but" | "so" | "yet")
                && SUBJECTS.contains(&s.as_str())
                && !serial_list(tokens, i)
                && !(c == "so" && s == "that")
            {
                if c == "and" {
                    return format!("End the first sentence before `and {s}`, and drop the `and`.");
                }
                let capital = c[..1].to_uppercase() + &c[1..];
                return format!(
                    "End the first sentence before `{c} {s}`, and open the next one with `{capital}`."
                );
            }
        }
    }
    for pair in tokens.windows(2).skip(1) {
        let c = bare(&pair[1].text);
        if pair[0].text.ends_with(',') && matches!(c.as_str(), "which" | "who" | "where") {
            return format!(
                "Give the `, {c}` clause a sentence of its own, and open it with the noun that `{c}` stands for."
            );
        }
    }
    "Split it between two thoughts, and carry the relation into the next sentence with `so`, `but`, or `then`.".to_string()
}

/// Returns a finding for each sentence of more than 32 words.
pub fn check(model: &Markdown) -> Vec<Hit> {
    sentences(model)
        .iter()
        .filter_map(|tokens| {
            let (from, words) = count_words(tokens);
            let from = from?;
            (words > LIMIT).then(|| Hit {
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
pub(crate) mod tests {
    use super::*;
    use crate::region::Region;

    /// Returns the line, the column, and the message of each finding on
    /// `text`.
    fn found(text: &str) -> Vec<(usize, usize, String)> {
        let region = Region::markdown(text);
        check(&Markdown::new(&region))
            .into_iter()
            .map(|hit| {
                let at = region.position(hit.at);
                (at.line, at.col, hit.message)
            })
            .collect()
    }

    /// A line of 26 words that does not end its sentence.
    const FIRST: &str = "The scheduler places each pod on a node that has enough free memory and enough free CPU for the requests of every container in the pod";

    /// A line of 20 words that ends its sentence.
    const SECOND: &str = "it retries the placement on the next node in the queue when the first node lacks room for the pod.";

    #[test]
    fn sentences() {
        struct Case {
            name: &'static str,
            text: String,
            /// The line and the column of each finding.
            want: Vec<(usize, usize)>,
        }
        let cases = [
            // The cases of the sentence-paragraphs test of
            // pkgs/prose-lint.nix.
            Case {
                name: "a line of `>` and spaces ends the paragraph",
                text: format!("> {FIRST}\n>     \n> {SECOND}\n"),
                want: vec![],
            },
            Case {
                name: "a line of `>` and tabs ends the paragraph",
                text: format!("> {FIRST}\n>\t\t\n> {SECOND}\n"),
                want: vec![],
            },
            Case {
                name: "a deep list marker in a blockquote continues the sentence",
                text: format!("> {FIRST}\n>     - and then\n> {SECOND}\n"),
                want: vec![(1, 3)],
            },
            // GFM reads the `---` as a thematic break in the outer item and
            // the `===` as more text, so neither makes the paragraph a
            // heading.
            Case {
                name: "a rule left of the text of a nested item",
                text: format!("- - {FIRST} {SECOND}\n  ---\n"),
                want: vec![(1, 5)],
            },
            Case {
                name: "a line of `=` left of the text of a nested item",
                text: format!("1. - {FIRST} {SECOND}\n   ===\n"),
                want: vec![(1, 6)],
            },
            // GFM opens a blockquote outside the item at the `>` in the
            // first column, so the two lines hold two sentences.
            Case {
                name: "a quote left of the text of an item",
                text: format!("- The pod holds the cache.\n\n  > {FIRST}\n> {SECOND}\n"),
                want: vec![],
            },
        ];
        for case in cases {
            let got: Vec<_> = found(&case.text)
                .into_iter()
                .map(|(l, c, _)| (l, c))
                .collect();
            assert_eq!(got, case.want, "{}", case.name);
        }
    }

    // The ends.md file of the sentence-ends test of pkgs/prose-lint.nix.
    pub(crate) const ENDS: &[&str] = &[
        "He said \u{201c}the proxy retries each failed request once before it returns the error to the client.\u{201d} The scheduler then reads the queue and picks the oldest job whose lease has expired before it returns.",
        "The guide says \u{2018}the proxy retries each failed request once before it returns the error to the client.\u{2019} The scheduler then reads the queue and picks the oldest job whose lease has expired before it returns.",
        "The importer prints \u{201c}reading rows...\u{201d} for each batch that it reads from the source table, and then it writes the rows in batches of one hundred to the destination table for the operators on call.",
        "The importer prints \u{2018}reading rows...\u{2019} for each batch that it reads from the source table, and then it writes the rows in batches of one hundred to the destination table for the operators on call.",
        "The proxy retries each failed request once before it returns the error to the client and then says \u{201c}see https://example.com/guide.\u{201d} The scheduler then reads the queue and picks the oldest job whose lease has expired before it returns.",
        "The proxy retries each failed request once before it returns the error to the client and then says \u{2018}see https://example.com/guide.\u{2019} The scheduler then reads the queue and picks the oldest job whose lease has expired before it returns.",
        "The importer prints \u{201c}Batch written!\u{201d} each time it writes a batch of rows to the destination table, and then it reads the next batch of rows from the source table for the operators on call.",
        "The importer asks \u{2018}Skip the rejected rows?\u{2019} each time it reads a batch of rows from the source table, and then it writes the remaining rows to the destination table for the operators on call.",
        "\u{201c}Run the tests.\u{201d} shows up in the log if the build passes.",
        "\u{2018}Run the tests.\u{2019} shows up in the log if the build passes.",
        "\u{201c}Save\u{201d} appears in the menu if the file changed.",
        "\u{2018}Save\u{2019} appears in the menu if the file changed.",
    ];

    #[test]
    fn sentence_ends() {
        let got: Vec<_> = found(&(ENDS.join("\n\n") + "\n"))
            .into_iter()
            .map(|(l, c, _)| (l, c))
            .collect();
        assert_eq!(got, [(5, 1), (7, 1), (13, 1), (15, 1)]);
    }

    #[test]
    fn messages() {
        // The first sentence of the single-quotes test of
        // pkgs/prose-lint.nix, in both kinds of quotes. Its condition
        // keeps the `, and the` after the comma out of the split points.
        let double = "\"If the cache is cold and the node restarts after the upgrade, the scheduler moves every pod to the spare pool, and the team waits for the warmup to finish before it opens the gate.\" the runbook says.\n";
        let want = vec![(
            1,
            1,
            "Sentence of 38 words, past the 32-word limit. Split it between two thoughts, and carry the relation into the next sentence with `so`, `but`, or `then`.".to_string(),
        )];
        assert_eq!(found(double), want);
        assert_eq!(found(&double.replace('"', "'")), want);

        let and = format!("{FIRST}, and the queue holds it until a node frees enough memory.\n");
        assert_eq!(
            found(&and)[0].2,
            "Sentence of 37 words, past the 32-word limit. End the first sentence before `and the`, and drop the `and`."
        );
        let which = format!("{FIRST}, which the queue holds until a node frees enough memory.\n");
        assert_eq!(
            found(&which)[0].2,
            "Sentence of 36 words, past the 32-word limit. Give the `, which` clause a sentence of its own, and open it with the noun that `which` stands for."
        );
    }
}
