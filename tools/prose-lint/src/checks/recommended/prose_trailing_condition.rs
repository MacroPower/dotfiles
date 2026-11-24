//! Overview
//!
//! A trailing condition is a condition that follows the step it governs
//! (`Use the flag if the port is taken`). The rule reports one in markdown
//! prose. The reader pictures the step before the condition says whether
//! the step applies, so the technical-writing skill puts the condition
//! first. The fix moves the condition to the start of the sentence (`If
//! the port is taken, use the flag`). The rule sits at Recommended, because
//! an `if` after a command does not always state the condition of that
//! command.
//!
//! Expression
//!
//! The check reads the sentences that `sentences` in
//! `src/checks/sentences.rs` finds in each paragraph block of the markdown
//! model. It reads each sentence that opens with an imperative from
//! `IMPERATIVES` (`use`, `set`, `run`). An imperative is the verb form that
//! gives a command, such as `run` in `run make`. The check reports the
//! first `if` after the verb that sits outside parentheses.
//!
//! The check drops the punctuation around each word, a straight `'`
//! included, so `'Run`, `"Run`, and `\u{2018}Run` each count as the
//! imperative `run`. It reads a curly apostrophe inside a word as a
//! straight one, so `Don\u{2019}t` counts as an imperative, the same as
//! `Don't`.
//!
//! Harper skips a footnote reference, so the check drops each one from a
//! word before it drops the punctuation. `if[^note]` and `if[^1]` then both
//! count as `if`. The check reads a sentence only when the first letter of
//! its first word is a capital. It finds that letter after it drops the
//! references, so `[^note]Use` and `[^1]Use` both open a sentence that the
//! check reads.
//!
//! A sentence can open with a quotation, such as the label of a button
//! (`"Save" appears in the menu if the file changed`). The check then reads
//! only the words up to the closing quote, which `quoted_opening` in
//! `src/checks/sentences.rs` finds. False Positives says why the words
//! after the quotation pass. The guard also passes a quoted imperative
//! (`"Run" the build if the tests pass`).
//!
//! In single quotes, the closing quote is the same mark as an apostrophe.
//! `quoted_opening` can therefore end a quotation early at a word inside it
//! that ends in an apostrophe, and an `if` after that word passes (`\u{2018}Keep
//! goin\u{2019} if the build fails\u{2019}`).
//!
//! An attribution can split a quotation that opens the sentence (`"Copy the
//! files," the guide says, "then restart the job if it hangs."`). The
//! second part goes on with the quoted speech, so the check reads the words
//! of both parts, each up to its closing quote. The check skips the
//! attribution, so the last word of the first part counts as the word
//! before the second part. An `if` right after the opening quote of the
//! second part counts too (`"if they changed."`), and its finding names the
//! `if` itself. `split_quotation` in `src/checks/sentences.rs` finds the
//! second part, and `quotation_end` there finds the word that closes it. A
//! second part that no word closes runs to the end of the sentence.
//!
//! False Positives
//!
//! The check skips `if any` and `if so`, because both read worse at the
//! front. It skips `if` after `as` and `even`, where `if` does not open a
//! condition. It skips `if it exists`, `if they exist`, and `if there is
//! one`, because each reads the same at either end. A period, a colon, or a
//! comma must follow `exists` or `exist`, because later words can make the
//! condition longer (`if it exists in the cache`). A closing quote, paren,
//! or bracket can follow that mark, and so can emphasis or a footnote
//! reference (`"if they exist."`, `if it exists.[^1]`).
//!
//! An `if` after `only` passes, because the front position drops the
//! restriction (`Use this option only if the file sits in a
//! subdirectory`). So does an `if` after `;`, `and`, `or`, or `but`,
//! because that `if` opens the next clause (`Keep the file short; if it
//! grows, split it`). A clause is a subject and its verb, such as `the
//! tests pass`.
//!
//! A sentence can open with a quotation, such as the label of a button, and
//! go on after the closing quote (`"Save" appears in the menu if the file
//! changed`). A period, a question mark, or an exclamation mark can sit
//! before that quote when a lowercase word follows it (`"Save changes?"
//! appears if you close the editor`). The imperative then sits inside the
//! quotation, and the `if` after it states the condition of the verb after
//! the quotation. So the check skips that `if`, because it reads only the
//! words of the quotation.
//!
//! Two quoted strings read as a split quotation when a comma follows the
//! first string and the words between them. Those words must also pass the
//! tests of `split_quotation`. An `if` in the second string then fires,
//! although the words between the strings form a main clause and not an
//! attribution (`"Run the build," appears in red, "Retry if busy" in
//! gray`).
//!
//! `unless` passes. After an imperative, it names the exception to a
//! default (`Use the standard edition unless you need extra features`), and
//! the technical-writing skill puts the exception after the common case.
//!
//! The check still reports an `if` that states the condition of a later
//! verb (`Set the timeout so that the job fails if the server hangs`).
//!
//! Related Rules
//!
//! The Recommended ProseVagueCriterion reports `if needed`,
//! `if necessary`, `if applicable`, `if appropriate`, and `if possible`.
//! That rule always matches the plain form of the phrase, with no markup
//! in either word and one space or a soft line break between them. The
//! check skips the `if` of that form and reads on, so a later `if` in the
//! sentence can still draw a finding (`Use the cache if possible, then
//! rerun the job if the build fails`).
//!
//! Markup can sit between the two words (`if **needed**`, `if[^1]
//! needed`), and Harper decides whether ProseVagueCriterion still matches.
//! The check reports the `if` of such a form. `drop_vague_conditions` in
//! `src/guards.rs` drops that finding where the other rule reports at the
//! same position. The phrase then draws one finding, and a later `if` of
//! the same sentence does not draw any.
//!
//! ProseIfWhether reports `if` after a verb of checking (`check if`,
//! `notice if`), so the check skips that `if` too. That `if` means
//! `whether`, so it does not open a condition.
//!
//! ProseDoubleNegative reports `Do not ... unless`.
//!
//! Measurement
//!
//! The rule hit 21 lines in the measure.py corpus, and 19 of 21 sampled
//! hits put a condition after its step.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::checks::sentences::{Token, quotation_end, quoted_opening, sentences, split_quotation};
use crate::markdown::Markdown;

/// The imperatives that open a sentence that the check reads.
const IMPERATIVES: &[&str] = &[
    "add",
    "apply",
    "avoid",
    "build",
    "call",
    "change",
    "choose",
    "click",
    "combine",
    "configure",
    "copy",
    "create",
    "define",
    "delete",
    "deploy",
    "disable",
    "do",
    "don't",
    "download",
    "edit",
    "enable",
    "follow",
    "include",
    "increase",
    "install",
    "keep",
    "limit",
    "leave",
    "mark",
    "move",
    "open",
    "pass",
    "pick",
    "place",
    "provide",
    "push",
    "put",
    "read",
    "remove",
    "rename",
    "replace",
    "restart",
    "restrict",
    "retry",
    "return",
    "run",
    "save",
    "see",
    "select",
    "send",
    "set",
    "skip",
    "specify",
    "start",
    "stop",
    "store",
    "try",
    "turn",
    "update",
    "upgrade",
    "use",
    "wait",
    "write",
];

/// The verbs of checking, after which `if` means `whether`.
const CHECKING: &[&str] = &[
    "check",
    "checks",
    "checked",
    "checking",
    "test",
    "tests",
    "tested",
    "testing",
    "determine",
    "determines",
    "determined",
    "determining",
    "verify",
    "verifies",
    "verified",
    "verifying",
    "see",
    "sees",
    "seeing",
    "decide",
    "decides",
    "decided",
    "deciding",
    "ask",
    "asks",
    "asked",
    "asking",
    "detect",
    "detects",
    "detected",
    "detecting",
    "tell",
    "tells",
    "telling",
    "know",
    "knows",
    "notice",
    "notices",
    "wonder",
    "matter",
    "matters",
];

/// A footnote reference with its label.
static FOOTNOTE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\[\^[^\]\[]+\]").expect("the pattern is valid"));

/// The opening quote of the second part of a split quotation, after any
/// emphasis.
static OPEN_QUOTE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r#"^[*_]*["'\x{201c}\x{2018}]"#).expect("the pattern is valid"));

/// A token that holds only a vague word of ProseVagueCriterion, before
/// any closing marks (`needed.`, `possible,`).
static VAGUE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r#"^(?:needed|necessary|applicable|appropriate|possible)[.,;:?!)"\x{201d}]*$"#)
        .expect("the pattern is valid")
});

/// Returns the token `token` without its footnote references.
fn drop_notes(token: &str) -> String {
    FOOTNOTE.replace_all(token, "").into_owned()
}

/// Returns the token `token` in lowercase without its footnote references
/// and the characters other than `a` to `z` at its ends, with each curly
/// apostrophe straight.
fn word(token: &str) -> String {
    drop_notes(&token.to_lowercase())
        .trim_matches(|c: char| !c.is_ascii_lowercase())
        .replace('\u{2019}', "'")
}

/// Returns whether the token `token` ends in a period, a colon, or a
/// comma, after which only closing quotes, parens, brackets, emphasis, and
/// footnote references sit (`exists.`, `exist."`, `exists.[^1]`).
fn ends_at_mark(token: &str) -> bool {
    drop_notes(token)
        .trim_end_matches(['"', ')', '*', '_', ']', '\'', '\u{201d}', '\u{2019}'])
        .ends_with(['.', ':', ','])
}

/// Returns whether Harper reads the tokens `a` and `b` side by side, with
/// no markup that the model blanked in either. The tokens sit on one line
/// of `model` with one space between them, or `b` opens the next line
/// after a soft line break. That break leaves at most one space or tab
/// after `a`, and only an indent or `>` marks before `b`.
fn side_by_side(model: &Markdown, a: &Token, b: &Token) -> bool {
    let line_of = |at: usize| {
        model
            .lines
            .partition_point(|l| l.start <= at)
            .checked_sub(1)
    };
    let (Some(i), Some(j)) = (line_of(a.at), line_of(b.at)) else {
        return false;
    };
    let first = &model.lines[i];
    let rest: String = first.source.chars().skip(a.at - first.start).collect();
    if i == j {
        return b.at == a.at + a.text.chars().count() + 1
            && rest.starts_with(&format!("{} {}", a.text, b.text));
    }
    let second = &model.lines[j];
    let from = b.at - second.start;
    j == i + 1
        && matches!(rest.strip_prefix(a.text.as_str()), Some("" | " " | "\t"))
        && second
            .source
            .chars()
            .take(from)
            .all(|c| matches!(c, ' ' | '\t' | '>'))
        && second
            .source
            .chars()
            .skip(from)
            .take(b.text.chars().count())
            .eq(b.text.chars())
}

/// Returns the finding of one sentence, if any.
fn sentence(model: &Markdown, tokens: &[Token]) -> Option<Hit> {
    let first = drop_notes(&tokens[0].text);
    if !IMPERATIVES.contains(&word(&first).as_str())
        || !first
            .trim_start_matches(|c: char| !c.is_ascii_alphabetic())
            .starts_with(|c: char| c.is_ascii_uppercase())
    {
        return None;
    }
    // A quotation that opens the sentence holds the imperative, so the
    // check reads only the words of the quotation. When an attribution
    // splits the quotation, the check also reads the words of its second
    // part, from token `resume` up to its closing quote.
    let n = tokens.len();
    let quoted = quoted_opening(tokens);
    let resume = split_quotation(tokens, quoted);
    let mut at: Vec<usize> = (0..=quoted.unwrap_or(n - 1)).collect();
    if let Some(r) = resume {
        at.extend(r..=quotation_end(tokens, r).unwrap_or(n - 1));
    }
    // The word of the kth token that the check reads, or an empty string
    // past the last one.
    let w = |k: usize| {
        at.get(k)
            .map_or_else(String::new, |&i| word(&tokens[i].text))
    };
    let mut depth = 0i32;
    let mut limited = false;
    for k in 1..at.len() {
        let i = at[k];
        let mut t = tokens[i].text.as_str();
        // The opening quote of the second part sits before its first word
        // (`"if`), so the test for a lowercase word reads the word after
        // it.
        if Some(i) == resume
            && let Some(m) = OPEN_QUOTE.find(t)
        {
            t = &t[m.end()..];
        }
        let current = w(k);
        if current == "if" && depth == 0 && t.starts_with(|c: char| c.is_ascii_lowercase()) {
            let prev = w(k - 1);
            // ProseVagueCriterion always matches the plain form of its
            // phrase, so the check moves on to the next `if`. A hard line
            // break between the two words stops that rule, so the check
            // reports such an `if`.
            let vague = tokens[i].text.ends_with("if")
                && at.get(k + 1) == Some(&(i + 1))
                && VAGUE.is_match(&tokens[i + 1].text)
                && side_by_side(model, &tokens[i], &tokens[i + 1]);
            if vague
                || matches!(w(k + 1).as_str(), "any" | "so")
                || CHECKING.contains(&prev.as_str())
                || matches!(prev.as_str(), "as" | "even" | "only")
            {
                continue;
            }
            let exists = matches!(w(k + 1).as_str(), "it" | "they")
                && matches!(w(k + 2).as_str(), "exist" | "exists")
                && at
                    .get(k + 2)
                    .is_some_and(|&j| ends_at_mark(&tokens[j].text));
            if exists || (w(k + 1) == "there" && w(k + 2) == "is" && w(k + 3) == "one") {
                continue;
            }
            if limited
                || tokens[at[k - 1]].text.ends_with(';')
                || matches!(prev.as_str(), "and" | "or" | "but")
            {
                return None;
            }
            let skipped = tokens[i].text.chars().count() - t.chars().count();
            return Some(Hit {
                at: tokens[i].at + skipped,
                message: format!(
                    "Trailing condition. Move the `{current}` clause to the start of the sentence."
                ),
            });
        }
        if current == "only" {
            limited = true;
        }
        depth += t.matches('(').count() as i32 - t.matches(')').count() as i32;
    }
    None
}

/// Returns a finding for each sentence that opens with an imperative and
/// puts its condition after it.
pub fn check(model: &Markdown) -> Vec<Hit> {
    sentences(model)
        .iter()
        .filter_map(|t| sentence(model, t))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::checks::recommended::prose_sentence_length::tests::ENDS;
    use crate::region::Region;
    use crate::rules::Rules;

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
                name: "an `if` after an imperative",
                text: "Use the flag if the port is taken.\n",
                want: vec![(1, 14)],
            },
            Case {
                name: "an `if` after `only`, `and`, and a verb of checking",
                text: "Use this option only if it helps. Use it, and if it fails, stop. Run the job and check if it passes.\n",
                want: vec![],
            },
            Case {
                name: "`if it exists` before a mark",
                text: "Use the flag if it exists. Use the flag if it exists in the cache.\n",
                want: vec![(1, 41)],
            },
            Case {
                name: "`if it exists` before a footnote reference",
                text: "Use the flag if it exists.[^1] The build runs if the port is taken.\n\nUse the flag if it exists. The build runs if the port is taken.\n\n[^1]: A note.\n",
                want: vec![],
            },
            Case {
                name: "a quotation that opens the sentence",
                text: "\"Save\" appears in the menu if the file changed.\n",
                want: vec![],
            },
            Case {
                name: "the second part of a split quotation",
                text: "\"Copy the files,\" the guide says, \"if they changed.\"\n",
                want: vec![(1, 36)],
            },
            // An unindented line that opens with inline HTML, a `<`, or an
            // ordered marker other than `1.` is more text of the paragraph.
            Case {
                name: "lines that continue the paragraph",
                text: "Restart the agent if\n< 5 workers stay up.\n\nRestart the agent if\n<span>the pod</span> fails.\n\nRestart the agent if\n2. the pod fails.\n",
                want: vec![(1, 19), (4, 19), (7, 19)],
            },
            // ProseVagueCriterion reports the first `if`, so the check
            // reports the next one.
            Case {
                name: "a trailing `if` after a vague `if`",
                text: "Restart the agent if needed and use the flag if the port is taken.\n\nUse the cache if possible, then rerun the job if the build fails.\n\n> Restart the agent if\n> needed and use the flag if the port is taken.\n",
                want: vec![(1, 46), (3, 47), (6, 27)],
            },
            Case {
                name: "a vague word with markup or a longer token",
                text: "Restart the agent if **needed**.\n\nRestart the agent if needed's done.\n\nRestart the agent if  \nneeded.\n\nRestart the agent if\n<b>needed</b>.\n",
                want: vec![(1, 19), (3, 19), (5, 19), (8, 19)],
            },
        ];
        for case in cases {
            assert_eq!(found(case.text), case.want, "{}", case.name);
        }
    }

    // The breaks.md file of the trailing-condition test of the bash
    // prose-lint. A paragraph break between the two words, which
    // Harper reads at a hard line break or at two line breaks with no
    // space between them, stops ProseVagueCriterion.
    const BREAKS: &[&str] = &[
        "Restart the agent if  ",
        "needed.",
        "",
        "Retry the request if  ",
        "possible.",
        "",
        "Restart the agent if\t\t",
        "needed.",
        "",
        "Restart the agent if ~x  ",
        "y~ needed.",
        "",
        "Restart the agent if ^x  ",
        "y^ needed.",
        "",
        "Restart the [agent if](https://example.com)  ",
        "needed.",
        "",
        "Restart the agent if [^1]  ",
        "needed.",
        "",
        "Restart the agent if\\",
        "needed.",
        "",
        "Restart the agent if ",
        "needed.",
        "",
        "Restart the agent if\t",
        "needed.",
        "",
        "Restart the agent if",
        "~x~",
        "needed.",
        "",
        "Restart the agent if",
        "^x^",
        "needed.",
        "",
        "Restart the agent if",
        "~x",
        "y~ needed.",
        "",
        "Restart the agent if ~x",
        "y~",
        "needed.",
        "",
        "Restart the agent if",
        "[^1]",
        "needed.",
        "",
        "Restart the agent if",
        "~x y~",
        "needed.",
        "",
        "[^1]: A note.",
    ];

    #[test]
    fn sentence_ends() {
        let text = ENDS.join("\n\n") + "\n";
        assert_eq!(found(&text), vec![]);
    }

    #[test]
    fn breaks() {
        let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
        let text = BREAKS.join("\n") + "\n";
        let mut got: Vec<_> = crate::findings(&mut rules, &[], &text)
            .into_iter()
            .filter(|f| {
                matches!(
                    f.rule.as_str(),
                    "ProseTrailingCondition" | "ProseVagueCriterion"
                )
            })
            .map(|f| (f.line, f.col, f.rule))
            .collect();
        got.sort();
        let want: Vec<_> = [1, 4, 7, 10, 13, 16, 19, 22, 25, 28, 31, 35, 39, 43, 47, 51]
            .into_iter()
            .map(|line| {
                let rule = if matches!(line, 25 | 28) {
                    "ProseVagueCriterion"
                } else {
                    "ProseTrailingCondition"
                };
                (line, if line == 16 { 20 } else { 19 }, rule.to_string())
            })
            .collect();
        assert_eq!(got, want);
    }
}
