//! The tests of a sentence end that several checks share, so those
//! checks end a sentence at the same word, and the sentences of a
//! region that the sentence checks read.
//!
//! [`sentences`] splits the text of each paragraph block of the markdown
//! model into sentences. pulldown-cmark finds the paragraphs, so a
//! sentence never runs from one block into the next, and a heading or a
//! table does not hold a sentence. A line that GFM reads as more text of
//! a paragraph joins the sentence, at any indent.
//!
//! The text of a sentence is the prose of the model, so a code span, a
//! link destination, and inline HTML are spaces. [`blank_urls`] blanks
//! each bare URL too. The list marker that opens an item is not part of
//! the paragraph block, and neither are the `>` marks of a blockquote.

use std::sync::LazyLock;

use regex::Regex;

use crate::markdown::{BlockKind, Markdown};

/// How a word ends its sentence.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum End {
    /// The word does not end the sentence.
    No,
    /// The word ends the sentence.
    Yes,
    /// The word ends in a mark and a closing quote. The quote can close
    /// a quotation inside the sentence (`prints "Lucky penny!" every
    /// time`), so the next word decides with [`continues_sentence`].
    Quote,
}

/// The closing marks after a `.`, `?`, or `!` that end a sentence.
const CLOSERS: &str = r#"[)*_\]]*"#;

static NO_END: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r#"\.\.\.["')*_\]\x{201d}\x{2019}]*$|(?i)^[("'*_]*(e\.g|i\.e|vs|cf)\.$"#)
        .expect("the pattern is valid")
});
static QUOTE_END: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(&format!(
        r#"[.?!]{CLOSERS}["'\x{{201d}}\x{{2019}}]["')*_\]\x{{201d}}\x{{2019}}]*$"#
    ))
    .expect("the pattern is valid")
});
static PLAIN_END: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(&format!(r"[.?!]{CLOSERS}$")).expect("the pattern is valid"));
/// The footnote references at the end of a word (`exists.[^1]`).
static NOTES_END: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?:\[\^[^\]\[]+\])+$").expect("the pattern is valid"));
static CONTINUES: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[(\[*_]*[a-z]").expect("the pattern is valid"));
static ATTRIBUTE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[a-z][a-z0-9-]*=").expect("the pattern is valid"));
static OPENS_QUOTE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r#"^[(\[*_]*["'\x{201c}\x{2018}]"#).expect("the pattern is valid"));

/// Returns how the word `word` ends its sentence. A word ends a
/// sentence when it ends in `.`, `?`, or `!`, after any closing quote,
/// paren, bracket, or emphasis. A closing quote is straight or curly.
/// Footnote references after the mark do not count (`exists.[^1]`).
/// `e.g.`, `i.e.`, `vs.`, `cf.`, and a three-period ellipsis do not end
/// a sentence.
pub fn ends_sentence(word: &str) -> End {
    let word = NOTES_END.replace(word, "");
    let word = word.as_ref();
    if NO_END.is_match(word) {
        End::No
    } else if QUOTE_END.is_match(word) {
        End::Quote
    } else if PLAIN_END.is_match(word) {
        End::Yes
    } else {
        End::No
    }
}

/// Returns whether the word `word` continues the sentence after a word
/// that ends in [`End::Quote`]. Such a word opens with a lowercase
/// letter, after any opening paren, bracket, or emphasis (`every`,
/// `*every*`). An HTML attribute (`src="img/table.svg"`) also opens with
/// a lowercase letter, but it does not continue a sentence.
pub fn continues_sentence(word: &str) -> bool {
    CONTINUES.is_match(word) && !ATTRIBUTE.is_match(word)
}

/// Returns whether the word `word` opens with a straight or a curly
/// opening quote, after any opening paren, bracket, or emphasis
/// (`"Save`, `(\u{201c}Save`).
pub fn opens_quote(word: &str) -> bool {
    OPENS_QUOTE.is_match(word)
}

/// Returns the byte offset and the text of each run of characters other
/// than a space or a tab in `text`.
pub fn words(text: &str) -> impl Iterator<Item = (usize, &str)> {
    text.split([' ', '\t'])
        .filter(|w| !w.is_empty())
        .map(move |w| (w.as_ptr() as usize - text.as_ptr() as usize, w))
}

/// A list marker that holds the whole word, such as `-` or `2.`.
static LIST_MARKER: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^(?:[-*+]|[0-9]+[.)])$").expect("the pattern is valid"));
static ORDERED_MARKER: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[0-9]+\.$").expect("the pattern is valid"));
/// A bare URL, up to the next space, `<`, `>`, or `)`.
static URL: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"https?://[^ \t<>)]*").expect("the pattern is valid"));
static OPENS_DOUBLE_QUOTE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r#"^[(\[*_]*["\x{201c}]"#).expect("the pattern is valid"));

/// Returns `text` with each bare URL blanked to spaces, one space for
/// each character. pulldown-cmark reads a bare URL as text, but it does
/// not hold any word of a sentence. The marks at the end of the URL that close a
/// sentence, a clause, a quote, a bracket, or emphasis stay in the text.
/// The period after the URL in `Read https://example.com.` therefore
/// still ends the sentence, and the comma in `https://example.com/docs,
/// which` still follows the subject of a comma aside.
pub fn blank_urls(text: &str) -> String {
    URL.replace_all(text, |c: &regex::Captures<'_>| {
        let url = &c[0];
        let kept = url.trim_end_matches([
            '.', ',', ':', ';', '?', '!', '*', '_', '~', '"', '\'', ']', '\u{201d}', '\u{2019}',
        ]);
        " ".repeat(kept.chars().count()) + &url[kept.len()..]
    })
    .into_owned()
}

/// One word of a sentence: a run of characters other than a space or a
/// tab, its punctuation included.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Token {
    /// The text of the word.
    pub text: String,
    /// The index in the region of the first character of the word.
    pub at: usize,
}

/// Returns the sentences of each paragraph block of `model`, in order.
///
/// A sentence runs across the lines of its paragraph and ends at a word
/// for which [`ends_sentence`] returns [`End::Yes`], or at the end of the
/// paragraph. After a word that ends in [`End::Quote`], the next word
/// decides with [`continues_sentence`]. A list marker that opens a
/// sentence is not a word of it (`"Done!" 2) Run`). GFM reads an
/// ordered list marker that opens a later line of a paragraph as text,
/// so the word after it decides whether its period ends the sentence, as
/// after a quote (`1. and then`, `2026. Then it stops.`).
pub fn sentences(model: &Markdown) -> Vec<Vec<Token>> {
    let mut all = Vec::new();
    for block in model
        .blocks
        .iter()
        .filter(|b| b.kind == BlockKind::Paragraph)
    {
        let mut current: Vec<Token> = Vec::new();
        let mut quote_end = false;
        for (n, (i, text)) in model.block_lines(block).enumerate() {
            let text = blank_urls(&text);
            let mut lead = n > 0;
            for (offset, word) in words(&text) {
                if quote_end && !continues_sentence(word) && !current.is_empty() {
                    all.push(std::mem::take(&mut current));
                }
                quote_end = false;
                let first = std::mem::take(&mut lead);
                if current.is_empty() && LIST_MARKER.is_match(word) {
                    continue;
                }
                current.push(Token {
                    text: word.to_string(),
                    at: model.lines[i].at(&text, offset),
                });
                match ends_sentence(word) {
                    End::Yes if first && ORDERED_MARKER.is_match(word) => quote_end = true,
                    End::Yes => all.push(std::mem::take(&mut current)),
                    End::Quote => quote_end = true,
                    End::No => {}
                }
            }
        }
        if !current.is_empty() {
            all.push(current);
        }
    }
    all
}

/// Returns the index of the first token of `tokens` that holds an ASCII
/// letter, and the number of words of `tokens`. Each part of a
/// hyphenated word counts (`read-only` is two words), and a token with
/// no letter (`42`, `|`) counts as none.
pub fn count_words(tokens: &[Token]) -> (Option<usize>, usize) {
    let mut first = None;
    let mut count = 0;
    for (i, token) in tokens.iter().enumerate() {
        let n = words_in(&token.text);
        if n > 0 {
            first.get_or_insert(i);
        }
        count += n;
    }
    (first, count)
}

/// Returns the number of words of the text `text`, which [`count_words`]
/// counts for one token.
pub fn words_in(text: &str) -> usize {
    text.split('-')
        .filter(|part| part.contains(|c: char| c.is_ascii_alphabetic()))
        .count()
}

/// Returns the token `token` in lowercase without the characters other
/// than `a` to `z` at its ends. The strip drops a straight `'` at either
/// end of the word, as it drops a double quote, so `'If`, `"If`, and
/// `\u{2018}If` each read as `if`.
pub fn bare(token: &str) -> String {
    token
        .to_lowercase()
        .trim_matches(|c: char| !c.is_ascii_lowercase())
        .to_string()
}

/// Returns whether the word `word` opens with a straight or a curly
/// double quote, after any opening paren, bracket, or emphasis.
pub fn opens_double_quote(word: &str) -> bool {
    OPENS_DOUBLE_QUOTE.is_match(word)
}

/// Returns the index of the token that closes the quotation that token
/// `s` opens, or `None` when no token closes it. Token `s` opens with a
/// quote, as [`opens_quote`] tests.
///
/// The closing token ends in a closing quote of the family of the
/// opening quote, before any mark, closing paren, bracket, or emphasis
/// (`"Save",`). A double quote opens with `"` or `\u{201c}` and closes
/// with `"` or `\u{201d}`. A single quote opens with `'` or `\u{2018}`
/// and closes with `'` or `\u{2019}`. An apostrophe at the end of a
/// word (`"Copy the users' files"`) therefore does not close a quotation
/// in double quotes.
///
/// A later token that opens with a quote of the family opens an inner
/// quotation, and the next closing quote ends it (`\u{201c}Run "make" if
/// the tests pass,\u{201d} the guide says`).
///
/// A single closing quote is the same mark as an apostrophe. In single
/// quotes, a word that ends in `s` and the quote, with only emphasis
/// after it, can therefore be a plural possessive (`'Copy the users'
/// files'`). The function holds the first such token and skips each
/// later one. It returns the first token that ends in the quote and that
/// it neither holds nor skips, such as `data'`, `fills!'`, or
/// `fails',`, and the held token when no such token follows.
pub fn quotation_end(tokens: &[Token], s: usize) -> Option<usize> {
    let double = opens_double_quote(&tokens[s].text);
    let (open, close): (&[char], &[char]) = if double {
        (&['"', '\u{201c}'], &['"', '\u{201d}'])
    } else {
        (&['\'', '\u{2018}'], &['\'', '\u{2019}'])
    };
    let lead: &[char] = &['(', '[', '*', '_'];
    // Drop the opening quote, so a token that holds only the opening
    // quote (`" Save" appears`) does not close the quotation.
    let first = tokens[s].text.trim_start_matches(lead);
    let first = first
        .strip_prefix(['"', '\'', '\u{201c}', '\u{2018}'])
        .unwrap_or(first);
    let mut held = None;
    let mut inner = 0;
    for (i, token) in tokens.iter().enumerate().skip(s) {
        let t = if i == s { first } else { token.text.as_str() };
        if i > s && t.trim_start_matches(lead).starts_with(open) {
            inner += 1;
        }
        if !t
            .trim_end_matches([')', '*', '_', ']', ',', ';', ':', '.', '?', '!'])
            .ends_with(close)
        {
            continue;
        }
        if inner > 0 {
            inner -= 1;
            continue;
        }
        let possessive = t
            .trim_end_matches(['*', '_'])
            .strip_suffix(close)
            .is_some_and(|w| w.ends_with(['s', 'S']));
        if !double && possessive {
            held.get_or_insert(i);
            continue;
        }
        return Some(i);
    }
    held
}

/// Returns the index of the token that closes a quotation that opens
/// the sentence `tokens`, which [`quotation_end`] finds. When no token
/// closes the quotation, the function returns the first token that ends
/// in [`End::Quote`], a mark before a closing quote of the other family
/// (`"Save changes?\u{2019} appears`). It returns `None` when the sentence
/// does not open with a quote, or when it finds neither token.
///
/// The sentence goes on past that token (`"Save" appears if the file
/// changed`), so its first word sits inside the quotation. A check that
/// reads the first word as the opener of the sentence applies it only to
/// the words up to that token.
pub fn quoted_opening(tokens: &[Token]) -> Option<usize> {
    if !opens_quote(&tokens.first()?.text) {
        return None;
    }
    quotation_end(tokens, 0).or_else(|| {
        tokens
            .iter()
            .position(|t| ends_sentence(&t.text) == End::Quote)
    })
}

/// The words that can join a second clause to the words before them.
const CLAUSE_JOINERS: &[&str] = &[
    "and", "but", "or", "nor", "so", "yet", "then", "while", "whereas", "because", "although",
    "though", "if", "unless", "when", "whenever", "where", "wherever", "until", "since", "after",
    "before", "once",
];

/// Returns the index of the token that opens the second part of a
/// quotation that an attribution splits, such as `the guide says` in
/// `"Copy nothing," the guide says, "unless the tests pass."`. The
/// quotation opens the sentence, and its first part closes at token
/// `quoted`, which [`quoted_opening`] returns.
///
/// The second part opens at the first later token that opens with a
/// quote of the family of the first part. The words between the two
/// parts form the attribution. The function returns that token only
/// when these tests pass:
///
/// - A comma sits among the marks at the end of the first part, before
///   or after its closing quote (`nothing,"`).
/// - The attribution holds one or more words, and the last one ends in
///   a comma (`says,`).
/// - No other word of the attribution ends in a comma, a semicolon, a
///   colon, a period, a question mark, or an exclamation mark.
/// - No word of the attribution, without its emphasis, joins a second
///   clause, such as `and`, `but`, `then`, `if`, `when`, or `because`
///   (`appears in the log when the tooltip says,`).
///
/// These tests keep out most main clauses that go on after a quoted
/// string and quote another one. The list leaves out `that`, `which`,
/// `who`, and `as`, because each of them also opens a phrase inside an
/// attribution (`as the guide says,`).
pub fn split_quotation(tokens: &[Token], quoted: Option<usize>) -> Option<usize> {
    let quoted = quoted?;
    let end_marks = |t: &str| {
        t.trim_end_matches(|c: char| !c.is_ascii_alphanumeric())
            .len()
    };
    let closing = &tokens[quoted].text;
    if !closing[end_marks(closing)..].contains(',') {
        return None;
    }
    let double = opens_double_quote(&tokens[0].text);
    let i = (quoted + 1..tokens.len())
        .find(|&i| opens_quote(&tokens[i].text) && opens_double_quote(&tokens[i].text) == double)?;
    if i == quoted + 1 || !tokens[i - 1].text.ends_with(',') {
        return None;
    }
    for (k, token) in tokens.iter().enumerate().take(i).skip(quoted + 1) {
        let word = token.text.to_lowercase();
        if CLAUSE_JOINERS.contains(&word.trim_matches(['*', '_'])) {
            return None;
        }
        let tail = &token.text[end_marks(&token.text)..];
        if k < i - 1 && tail.contains([',', ';', ':', '.', '?', '!']) {
            return None;
        }
    }
    Some(i)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ends() {
        struct Case {
            name: &'static str,
            word: &'static str,
            want: End,
        }
        let cases = [
            Case {
                name: "period",
                word: "runs.",
                want: End::Yes,
            },
            Case {
                name: "after a paren",
                word: "runs).",
                want: End::Yes,
            },
            Case {
                name: "paren after the period",
                word: "runs.)",
                want: End::Yes,
            },
            Case {
                name: "straight quote",
                word: "penny!\"",
                want: End::Quote,
            },
            Case {
                name: "curly quote",
                word: "done.\u{201d}",
                want: End::Quote,
            },
            Case {
                name: "footnote reference after the period",
                word: "exists.[^1]",
                want: End::Yes,
            },
            Case {
                name: "abbreviation",
                word: "E.g.",
                want: End::No,
            },
            Case {
                name: "ellipsis",
                word: "About...\"",
                want: End::No,
            },
            Case {
                name: "no mark",
                word: "runs",
                want: End::No,
            },
        ];
        for case in cases {
            assert_eq!(ends_sentence(case.word), case.want, "{}", case.name);
        }
    }

    #[test]
    fn continues() {
        assert!(continues_sentence("*every*"));
        assert!(!continues_sentence("Every"));
        assert!(!continues_sentence("src=\"x\""));
        assert!(opens_quote("(\u{201c}Save"));
        assert!(!opens_quote("Save"));
    }

    #[test]
    fn split_words() {
        let got: Vec<_> = words(" a\tbc  d").collect();
        assert_eq!(got, [(1, "a"), (3, "bc"), (7, "d")]);
    }

    /// Returns the words of each sentence of `text`.
    fn split(text: &str) -> Vec<Vec<String>> {
        let model = Markdown::new(&crate::region::Region::markdown(text));
        sentences(&model)
            .into_iter()
            .map(|s| s.into_iter().map(|t| t.text).collect())
            .collect()
    }

    /// Returns the tokens of the words of `text`, each at its offset.
    fn tokens(text: &str) -> Vec<Token> {
        words(text)
            .map(|(at, w)| Token {
                text: w.to_string(),
                at,
            })
            .collect()
    }

    #[test]
    fn split_sentences() {
        struct Case {
            name: &'static str,
            text: &'static str,
            want: Vec<Vec<&'static str>>,
        }
        let cases = [
            Case {
                name: "sentences run across lines and end at the paragraph",
                text: "The pod runs. It\nstops\n\nThe node",
                want: vec![
                    vec!["The", "pod", "runs."],
                    vec!["It", "stops"],
                    vec!["The", "node"],
                ],
            },
            Case {
                name: "a lowercase word continues a quotation",
                text: "It prints \"Done!\" every\ntime. \"Done!\" Then",
                want: vec![
                    vec!["It", "prints", "\"Done!\"", "every", "time."],
                    vec!["\"Done!\""],
                    vec!["Then"],
                ],
            },
            Case {
                name: "a list marker that opens a sentence",
                text: "\"Done!\" 2) Run it.\n",
                want: vec![vec!["\"Done!\""], vec!["Run", "it."]],
            },
            Case {
                name: "an ordered marker that opens a later line",
                text: "Restart the agent if\n2. the pod fails.\n\nIt waits\n2026. Then it stops.\n",
                want: vec![
                    vec![
                        "Restart", "the", "agent", "if", "2.", "the", "pod", "fails.",
                    ],
                    vec!["It", "waits", "2026."],
                    vec!["Then", "it", "stops."],
                ],
            },
            Case {
                name: "headings, tables, and code hold no sentence",
                text: "# Setup\n\n| a |\n| - |\n| b |\n\n```\nx\n```\n- An item.\n",
                want: vec![vec!["An", "item."]],
            },
            Case {
                name: "a quote under an item at the margin",
                text: "- The pod.\n\n  > The node runs\n> it retries.\n",
                want: vec![
                    vec!["The", "pod."],
                    vec!["The", "node", "runs"],
                    vec!["it", "retries."],
                ],
            },
            Case {
                name: "a footnote reference after the period",
                text: "Use the flag if it exists.[^1] The build runs.\n",
                want: vec![
                    vec!["Use", "the", "flag", "if", "it", "exists.[^1]"],
                    vec!["The", "build", "runs."],
                ],
            },
            Case {
                name: "bare URLs",
                text: "Read https://example.com/a-b-c. Then go.\n",
                want: vec![vec!["Read", "."], vec!["Then", "go."]],
            },
        ];
        for case in cases {
            assert_eq!(split(case.text), case.want, "{}", case.name);
        }
    }

    #[test]
    fn urls() {
        assert_eq!(
            blank_urls("See \u{201c}https://example.com.\u{201d} (https://x.io/a) now"),
            "See \u{201c}                   .\u{201d} (              ) now"
        );
    }

    #[test]
    fn words_of_tokens() {
        let (first, count) = count_words(&tokens("| 42 read-only here"));
        assert_eq!((first, count), (Some(2), 3));
    }

    #[test]
    fn quotations() {
        struct Case {
            name: &'static str,
            text: &'static str,
            /// The tokens that close the opening quotation and open its
            /// second part.
            want: (Option<usize>, Option<usize>),
        }
        let cases = [
            Case {
                name: "no quotation",
                text: "Save appears.",
                want: (None, None),
            },
            Case {
                name: "double quotes",
                text: "\"Save\" appears.",
                want: (Some(0), None),
            },
            Case {
                name: "an apostrophe in double quotes",
                text: "\"Copy the users' files\" now",
                want: (Some(3), None),
            },
            Case {
                name: "an inner quotation",
                text: "\u{201c}Run \"make\" if it passes,\u{201d} the guide says.",
                want: (Some(4), None),
            },
            Case {
                name: "a plural possessive in single quotes",
                text: "'Copy the users' data' now",
                want: (Some(3), None),
            },
            Case {
                name: "a held plural possessive",
                text: "'Copy the users' files' now",
                want: (Some(2), None),
            },
            Case {
                name: "a mark before a quote of the other family",
                text: "\"Save changes?\u{2019} appears",
                want: (Some(1), None),
            },
            Case {
                name: "an attribution",
                text: "\"Copy nothing,\" the guide says, \"unless the tests pass.\"",
                want: (Some(1), Some(5)),
            },
            Case {
                name: "an attribution that joins a clause",
                text: "\"Copy nothing,\" the guide and the wiki say, \"unless they pass.\"",
                want: (Some(1), None),
            },
            Case {
                name: "an attribution with a comma inside",
                text: "\"Copy nothing,\" the guide, page 4, says, \"unless they pass.\"",
                want: (Some(1), None),
            },
        ];
        for case in cases {
            let tokens = tokens(case.text);
            let quoted = quoted_opening(&tokens);
            assert_eq!(
                (quoted, split_quotation(&tokens, quoted)),
                case.want,
                "{}",
                case.name
            );
        }
    }
}
