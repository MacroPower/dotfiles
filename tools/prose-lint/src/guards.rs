//! Steps around Harper that the Weir rules depend on.

use std::collections::{HashMap, HashSet};
use std::sync::LazyLock;

use regex::Regex;

use crate::region::Finding;
use crate::rules::Tier;

/// Straightens a curly apostrophe (U+2019) between two letters. The
/// Weir rules list contractions with a straight apostrophe, so without
/// this step a curly one hides them. Both apostrophes are one
/// character, so columns stay put.
pub fn straighten(text: &str) -> String {
    let chars: Vec<char> = text.chars().collect();
    chars
        .iter()
        .enumerate()
        .map(|(i, &c)| {
            let letter = |j: Option<usize>| {
                j.and_then(|j| chars.get(j))
                    .is_some_and(char::is_ascii_alphabetic)
            };
            if c == '\u{2019}' && letter(i.checked_sub(1)) && letter(Some(i + 1)) {
                '\''
            } else {
                c
            }
        })
        .collect()
}

static LABEL: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^[ \t]*(?:(?:[-*+]|[0-9]+[.)])[ \t]+)?\*\*[^*]+(?:\*\*:|:\*\*)")
        .expect("the pattern is valid")
});

/// Drops each ProseColonClause finding that starts inside a bold label
/// that opens its line (`- **Retry budget**: the job stops after five
/// attempts`). Harper's markdown parser drops the emphasis, so the rule
/// reads the label words as the subject of a clause. The colon after a
/// label names a term and does not join two clauses. A finding that
/// starts after the label, on a second colon in the same line, stays.
pub fn drop_label_colons(markdown: &str, findings: &mut Vec<Finding>) {
    let lines: Vec<&str> = markdown.lines().collect();
    findings.retain(|f| {
        if f.rule != "ProseColonClause" {
            return true;
        }
        let Some(label) = lines.get(f.line - 1).and_then(|l| LABEL.find(l)) else {
            return true;
        };
        f.col > label.as_str().chars().count()
    });
}

/// Drops each ProseTrailingCondition finding on an `if` where a finding
/// of the Recommended ProseVagueCriterion starts (`Restart the agent if
/// needed`). The vague criterion is the worse fault of the two, and its
/// fix rewrites the condition. The phrase therefore draws one finding.
/// The native check skips the plain form of the phrase itself. This
/// guard covers the forms with markup between the two words, such as
/// emphasis, a footnote reference, or a subscript. Harper decides
/// whether ProseVagueCriterion matches such a form after it drops the
/// markup.
pub fn drop_vague_conditions(findings: &mut Vec<Finding>) {
    let vague: HashSet<(usize, usize)> = findings
        .iter()
        .filter(|f| f.rule == "ProseVagueCriterion" && f.tier == Tier::Recommended)
        .map(|f| (f.line, f.col))
        .collect();
    findings.retain(|f| f.rule != "ProseTrailingCondition" || !vague.contains(&(f.line, f.col)));
}

static FENCE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[ \t>]*(?:```|~~~)").expect("the pattern is valid"));
static HEADING: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^ {0,3}#{1,6}(?:[ \t]|$)").expect("the pattern is valid"));
static BOLD_SPEAKER: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^ {0,3}\*\*([^* :]+(?: [^* :]+){0,2})(?:\*\*:|:\*\*)")
        .expect("the pattern is valid")
});
static CAPS_SPEAKER: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^ {0,3}(?:\*\*|<b>)?([A-Z][A-Z'.-]+(?: [A-Z][A-Z'.-]+){1,2}):")
        .expect("the pattern is valid")
});

/// Drops each ProseOpinion finding inside the turn of a speaker in a
/// transcript (`**MJ**: I think the split helps`). The speaker gives a
/// view on purpose, and the writer keeps the words as spoken.
///
/// A speaker label opens a paragraph. It is a name of one to three
/// words in `**` marks, with its colon on either side of the closing
/// marks (`**MJ**:` or `**Michael:**`). It can also be a name of two or
/// three words in capitals with a colon after it, which `**` or `<b>`
/// can open (`CRAIG BOX:`). One word in capitals often names a field
/// (`NOTE:`), so this form asks for two.
///
/// A file is a transcript when one name opens two or more of its
/// paragraphs. A speaker talks more than once, but a label such as
/// `**Note:**` rarely repeats. Each turn runs from a label to the
/// next heading, because an answer often takes several paragraphs. A
/// line inside a fenced code block is neither a label nor a heading.
pub fn drop_transcript_opinions(markdown: &str, findings: &mut Vec<Finding>) {
    let mut fenced = false;
    let mut blank = true;
    let mut headings = HashSet::new();
    let mut labels = HashSet::new();
    let mut seen: HashMap<&str, usize> = HashMap::new();
    let lines: Vec<&str> = markdown.lines().collect();
    for (i, line) in lines.iter().enumerate() {
        if FENCE.is_match(line) {
            fenced = !fenced;
        } else if !fenced && HEADING.is_match(line) {
            headings.insert(i + 1);
        } else if !fenced && blank {
            let speaker = BOLD_SPEAKER
                .captures(line)
                .or_else(|| CAPS_SPEAKER.captures(line));
            if let Some(name) = speaker.and_then(|c| c.get(1)) {
                labels.insert(i + 1);
                *seen.entry(name.as_str()).or_default() += 1;
            }
        }
        blank = line.trim_matches([' ', '\t', '\r']).is_empty();
    }
    if !seen.values().any(|&n| n >= 2) {
        return;
    }
    let mut speech = HashSet::new();
    let mut turn = false;
    for n in 1..=lines.len() {
        if headings.contains(&n) {
            turn = false;
        } else if labels.contains(&n) {
            turn = true;
        }
        if turn {
            speech.insert(n);
        }
    }
    findings.retain(|f| f.rule != "ProseOpinion" || !speech.contains(&f.line));
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::rules::Tier;

    fn finding(rule: &str, line: usize, col: usize) -> Finding {
        Finding {
            rule: rule.to_string(),
            tier: Tier::Recommended,
            kind: "Style".to_string(),
            line,
            col,
            message: String::new(),
        }
    }

    #[test]
    fn straighten_apostrophes() {
        struct Case {
            name: &'static str,
            text: &'static str,
            want: &'static str,
        }
        let cases = [
            Case {
                name: "contraction",
                text: "It doesn\u{2019}t run.",
                want: "It doesn't run.",
            },
            Case {
                name: "closing quote after a letter",
                text: "\u{2018}cache\u{2019} names it.",
                want: "\u{2018}cache\u{2019} names it.",
            },
            Case {
                name: "two contractions",
                text: "a\u{2019}b\u{2019}c",
                want: "a'b'c",
            },
            Case {
                name: "start of the text",
                text: "\u{2019}t",
                want: "\u{2019}t",
            },
        ];
        for case in cases {
            assert_eq!(straighten(case.text), case.want, "{}", case.name);
        }
    }

    #[test]
    fn label_colons() {
        struct Case {
            name: &'static str,
            markdown: &'static str,
            col: usize,
            rule: &'static str,
            want: bool,
        }
        let cases = [
            Case {
                name: "colon after the marks",
                markdown: "- **Retry budget**: the job stops after five attempts.",
                col: 5,
                rule: "ProseColonClause",
                want: false,
            },
            Case {
                name: "colon inside the marks",
                markdown: "**Retry budget:** the job stops.",
                col: 3,
                rule: "ProseColonClause",
                want: false,
            },
            Case {
                name: "numbered item",
                markdown: "12) **Retry budget**: the job stops.",
                col: 7,
                rule: "ProseColonClause",
                want: false,
            },
            Case {
                name: "second colon after the label",
                markdown: "**Retry**: the job stops: it waits.",
                col: 13,
                rule: "ProseColonClause",
                want: true,
            },
            Case {
                name: "bold that opens no label",
                markdown: "The **retry budget** caps it: the job stops.",
                col: 1,
                rule: "ProseColonClause",
                want: true,
            },
            Case {
                name: "other rule",
                markdown: "**Retry budget**: the job stops.",
                col: 3,
                rule: "ProseHedge",
                want: true,
            },
        ];
        for case in cases {
            let mut findings = vec![finding(case.rule, 1, case.col)];
            drop_label_colons(case.markdown, &mut findings);
            assert_eq!(!findings.is_empty(), case.want, "{}", case.name);
        }
    }

    #[test]
    fn transcript_opinions() {
        struct Case {
            name: &'static str,
            markdown: &'static str,
            line: usize,
            rule: &'static str,
            want: bool,
        }
        let interview = "**Ana:** Hi.\n\n**Raj**: I think so.\n\nMore.\n\n## Next\n\n**Ana:** Bye.\n\nI think not.";
        let cases = [
            Case {
                name: "turn of a speaker",
                markdown: interview,
                line: 3,
                rule: "ProseOpinion",
                want: false,
            },
            Case {
                name: "paragraph of a turn without a label",
                markdown: interview,
                line: 5,
                rule: "ProseOpinion",
                want: false,
            },
            Case {
                name: "heading ends the turn",
                markdown: interview,
                line: 7,
                rule: "ProseOpinion",
                want: true,
            },
            Case {
                name: "other rule in a turn",
                markdown: interview,
                line: 3,
                rule: "ProseAndOr",
                want: true,
            },
            Case {
                name: "no label repeats",
                markdown: "**Note:** I think so.\n\n**Tip:** I think not.",
                line: 1,
                rule: "ProseOpinion",
                want: true,
            },
            Case {
                name: "names in capitals",
                markdown: "<b>CRAIG BOX: Hi.</b>\n\nTARA O'BRIEN: I think so.\n\n**CRAIG BOX: Bye.**",
                line: 3,
                rule: "ProseOpinion",
                want: false,
            },
            Case {
                name: "one word in capitals",
                markdown: "NOTE: I think so.\n\nNOTE: I think not.",
                line: 1,
                rule: "ProseOpinion",
                want: true,
            },
            Case {
                name: "label after a line of prose",
                markdown: "**Ana:** Hi.\n\n## Notes\n\nText.\n**Ana:** I think so.\n\n**Ana:** Bye.",
                line: 6,
                rule: "ProseOpinion",
                want: true,
            },
            Case {
                name: "label in a fence",
                markdown: "**Ana:** Hi.\n\n# End\n\n~~~\n\n**Ana:** I think so.\n~~~\n\n**Ana:** Bye.",
                line: 7,
                rule: "ProseOpinion",
                want: true,
            },
            Case {
                name: "seven marks do not open a heading",
                markdown: "**Ana:** Hi.\n####### I think so.\n\n**Ana:** Bye.",
                line: 2,
                rule: "ProseOpinion",
                want: false,
            },
        ];
        for case in cases {
            let mut findings = vec![finding(case.rule, case.line, 1)];
            drop_transcript_opinions(case.markdown, &mut findings);
            assert_eq!(!findings.is_empty(), case.want, "{}", case.name);
        }
    }

    #[test]
    fn vague_conditions() {
        let optional = Finding {
            tier: Tier::Optional,
            ..finding("ProseVagueCriterion", 3, 19)
        };
        let mut findings = vec![
            finding("ProseVagueCriterion", 1, 19),
            finding("ProseTrailingCondition", 1, 19),
            finding("ProseTrailingCondition", 2, 19),
            optional.clone(),
            finding("ProseTrailingCondition", 3, 19),
            finding("ProseHedge", 1, 19),
        ];
        drop_vague_conditions(&mut findings);
        assert_eq!(
            findings,
            [
                finding("ProseVagueCriterion", 1, 19),
                finding("ProseTrailingCondition", 2, 19),
                optional,
                finding("ProseTrailingCondition", 3, 19),
                finding("ProseHedge", 1, 19),
            ]
        );
    }
}
