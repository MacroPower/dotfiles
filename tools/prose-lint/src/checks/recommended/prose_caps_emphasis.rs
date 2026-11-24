//! Overview
//!
//! Reports a word written in capitals for emphasis in markdown prose and
//! in source comments (`The hook ALWAYS runs`, `Do NOT commit the
//! token`). The capitals stress the word and give the reader no reason.
//! The technical-writing skill writes emphasis into the sentence, not the
//! typeface, and keeps the obligation words lowercase. The fix states the
//! consequence that the capitals stood in for (`Do not commit the token,
//! because the push publishes it`). The rule sits at Recommended, because
//! a comment that copies the text of a standard without naming the
//! standard also matches.
//!
//! Expression
//!
//! The check reads the prose of each line of the markdown model. Weir
//! ignores case, so a Weir rule cannot tell `NOT` from `not`, and the
//! check is native.
//!
//! `WORDS` holds the obligation words that standards documents
//! capitalize: `MUST`, `SHOULD`, `MAY`, `REQUIRED`, `RECOMMENDED`, and
//! `OPTIONAL`. It also holds the words that writers capitalize for
//! emphasis: `NEVER`, `ALWAYS`, `NOT`, `ONLY`, `NONE`, `IMPORTANT`,
//! `CANNOT`, and `DON'T`.
//!
//! The check first blanks quoted text, the forms that another rule
//! reports, a word alone in a table cell, and the fixed phrases. It then
//! looks for a word from `WORDS`. A paragraph is a run of lines that
//! hold prose. The opening line of a MyST directive and its option lines
//! end a paragraph. An open quote carries over to the next line of the
//! paragraph, so a quote that wraps across lines passes whole. The check
//! holds each finding until the paragraph ends, so a citation anywhere
//! in the paragraph clears the finding.
//!
//! prose-lint skips the check on every file under `configs/claude/`.
//! It also skips every file under the `agents`, `commands`,
//! `output-styles`, `rules`, and `skills` directories of `.claude/`. The
//! check reads the other directories of `.claude/`, such as a worktree
//! under `.claude/worktrees/`.
//!
//! False Positives
//!
//! `CRITICAL` stays out, because it also names a severity or a log level
//! (`HIGH or CRITICAL`). A word alone in a table cell passes, because an
//! options table labels a column or a value with it. Words glued to an
//! underscore, a hyphen, or a digit are identifiers (`MAY_FAIL`,
//! `X-NOT-CACHED`) and pass too. Some fixed phrases keep their capitals
//! for a reason of their own: a GitHub alert (`> [!IMPORTANT]`), the
//! marker on generated code (`DO NOT EDIT`), and the SQL constraint `NOT
//! NULL`.
//!
//! A word inside double quotes passes, because the quote reproduces a
//! standard's own words (`"A sender MUST NOT generate"`). A paragraph
//! that cites a standard (`RFC 7232`, `Section 5.4.1`, or `§`) passes
//! too, because code comments copy a standard's text without quote marks.
//!
//! prose-lint skips this check on prompt files for an agent (skills,
//! agent definitions, `CLAUDE.md`, and `AGENTS.md`), which write capitals
//! for emphasis on purpose.
//!
//! Related Rules
//!
//! The Recommended ProseObligation reports `is REQUIRED to` and `is
//! RECOMMENDED`, and ProseMayNot reports `MAY NOT`, so this check skips
//! those forms. ProseCallout matches a callout label only with a leading
//! capital, so only this check reports an all-caps label (`IMPORTANT:`).
//!
//! Weir ignores case, so every other Weir rule that reports a word from
//! `WORDS` in lowercase reports it in capitals too. No guard separates
//! such a rule from this check. For example, ProseNegativeContraction
//! reports `DON'T`, and ProseMisplacedOnly reports `ONLY` before a verb.
//! ProseMayPossibility reports `MAY` before a verb, so `The server MAY
//! return a cached copy` draws a finding from that rule and from this
//! check.
//!
//! The Recommended ProseHedge reports `SHOULD` after `it`, `this`, or
//! `that` and before a verb such as `work` (`This SHOULD work`). The
//! Optional ProseHedge reports `SHOULD` after a noun and before a verb
//! such as `pass` (`The test SHOULD pass`).
//!
//! Measurement
//!
//! The rule hit 17 lines in the measure.py corpus, and 8 of 17 sampled
//! hits capitalized the writer's own emphasis or obligation. Six of the
//! misfires copy a standard's text in a Go comment that never names the
//! standard. Three sit in an `AGENTS.md` file that prose-lint skips.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::markdown::{Markdown, blank};

const WORDS: &str = "MUST|SHOULD|MAY|REQUIRED|RECOMMENDED|OPTIONAL|NEVER|ALWAYS|NOT|ONLY|NONE|IMPORTANT|CANNOT|DON(?:'|\u{2019})T";

fn re(pattern: &str) -> Regex {
    Regex::new(pattern).expect("the pattern is valid")
}

static CITATION: LazyLock<Regex> =
    LazyLock::new(|| re(r"RFC[ \t]*[0-9]+|[Ss]ection[ \t]+[0-9]+(?:\.[0-9]+)+|§"));
static CAPS: LazyLock<Regex> = LazyLock::new(|| {
    re(&format!(
        "(?:^|[^A-Za-z0-9_-])({WORDS})(?:[^A-Za-z0-9_-]|$)"
    ))
});
static OWNED: LazyLock<Regex> = LazyLock::new(|| {
    re(
        "[Ii][Ss][ \t]+(?:REQUIRED|RECOMMENDED)|[Aa][Rr][Ee][ \t]+(?:REQUIRED|RECOMMENDED)|MAY[ \t]+NOT",
    )
});
static CELL: LazyLock<Regex> = LazyLock::new(|| re(&format!(r"\|[ \t]*(?:{WORDS})[ \t]*(?:\||$)")));
static FIXED: LazyLock<Regex> =
    LazyLock::new(|| re(r"\[![A-Z]+\]|DO[ \t]+NOT[ \t]+EDIT|NOT[ \t]+NULL"));

/// Blanks the text inside double quotes, straight or curly. `open`
/// holds whether a quote is open at the start of the line, and the
/// function leaves it as the end of the line leaves it.
fn unquote(text: &str, open: &mut bool) -> String {
    text.chars()
        .map(|c| {
            if matches!(c, '"' | '\u{201c}' | '\u{201d}') {
                *open = !*open;
                c
            } else if *open {
                ' '
            } else {
                c
            }
        })
        .collect()
}

/// Returns a finding for the first word in capitals on each line, unless
/// its paragraph cites a standard.
pub fn check(model: &Markdown) -> Vec<Hit> {
    let mut hits = Vec::new();
    let mut held = Vec::new();
    let mut cites = false;
    let mut open = false;
    for line in &model.lines {
        if !line.prose || line.directive {
            if !cites {
                hits.append(&mut held);
            }
            held.clear();
            cites = false;
            open = false;
            continue;
        }
        cites |= CITATION.is_match(&line.text);
        let text = unquote(&line.text, &mut open);
        let text = blank(&text, &OWNED);
        let text = blank(&text, &CELL);
        let text = blank(&text, &FIXED);
        if let Some(word) = CAPS.captures(&text).and_then(|c| c.get(1)) {
            held.push(Hit {
                at: line.at(&text, word.start()),
                message: format!(
                    "Uppercase emphasis `{}`. Write it in lowercase, and state the consequence when the reader needs a reason.",
                    word.as_str()
                ),
            });
        }
    }
    if !cites {
        hits.append(&mut held);
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
    fn paragraphs() {
        struct Case {
            name: &'static str,
            text: &'static str,
            want: Vec<(usize, usize)>,
        }
        let cases = [
            Case {
                // The case of the columns test of the bash prose-lint.
                name: "multi-byte character in a quotation",
                text: "Set \"caf\u{e9}\" first, and you MUST restart it.\n",
                want: vec![(1, 27)],
            },
            Case {
                name: "quote that wraps",
                text: "The spec says \"a sender\nMUST NOT retry\" here.\n",
                want: vec![],
            },
            Case {
                name: "citation on a later line",
                text: "A sender MUST NOT retry.\nSee RFC 9110.\n",
                want: vec![],
            },
            Case {
                name: "citation in a directive under the paragraph",
                text: "- First item text.\n- Second.\n\nA sender MUST NOT retry.\n:::{note}\nSee RFC 9110.\n:::\n",
                want: vec![(4, 10)],
            },
            Case {
                name: "citation in another paragraph",
                text: "A sender MUST NOT retry.\n\nSee RFC 9110.\n",
                want: vec![(1, 10)],
            },
        ];
        for case in cases {
            assert_eq!(found(case.text), case.want, "{}", case.name);
        }
    }
}
