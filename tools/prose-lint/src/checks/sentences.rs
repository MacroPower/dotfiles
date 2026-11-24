//! The tests of a sentence end that several checks share, so those
//! checks end a sentence at the same word.

use std::sync::LazyLock;

use regex::Regex;

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
static CONTINUES: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[(\[*_]*[a-z]").expect("the pattern is valid"));
static ATTRIBUTE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[a-z][a-z0-9-]*=").expect("the pattern is valid"));
static OPENS_QUOTE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r#"^[(\[*_]*["'\x{201c}\x{2018}]"#).expect("the pattern is valid"));

/// Returns how the word `word` ends its sentence. A word ends a
/// sentence when it ends in `.`, `?`, or `!`, after any closing quote,
/// paren, bracket, or emphasis. A closing quote is straight or curly.
/// `e.g.`, `i.e.`, `vs.`, `cf.`, and a three-period ellipsis do not end
/// a sentence.
pub fn ends_sentence(word: &str) -> End {
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
}
