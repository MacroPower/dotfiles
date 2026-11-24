//! Prose regions and the findings on them.

use std::fmt;

use crate::rules::Tier;

/// A line and a column of a source file, both counted from 1. The
/// column counts characters.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub struct Position {
    /// The line, counted from 1.
    pub line: usize,
    /// The column in characters, counted from 1.
    pub col: usize,
}

/// Markdown text and the source position of each of its characters.
/// A markdown file or a commit message is one region, and so is each
/// comment of a source file.
pub struct Region {
    /// The markdown text that the rules read.
    pub text: String,
    positions: Vec<Position>,
    /// The index in the source of each character of the text.
    indices: Vec<usize>,
}

impl Region {
    /// Returns the region of a whole markdown file, in which each
    /// character sits at its own position.
    pub fn markdown(source: &str) -> Self {
        let mut positions = Vec::new();
        let mut at = Position { line: 1, col: 1 };
        for c in source.chars() {
            positions.push(at);
            if c == '\n' {
                at = Position {
                    line: at.line + 1,
                    col: 1,
                };
            } else {
                at.col += 1;
            }
        }
        Self {
            text: source.to_string(),
            indices: (0..positions.len()).collect(),
            positions,
        }
    }

    /// Returns the region of `text`, which pairs each character with the
    /// index of a character of `file`. The character takes the position
    /// of that character of `file`. It can differ from the character of
    /// `file`, such as a blank in place of doc comment markup, and two
    /// characters can share one index.
    pub fn part(file: &Region, text: Vec<(usize, char)>) -> Self {
        Self {
            text: text.iter().map(|&(_, c)| c).collect(),
            positions: text.iter().map(|&(i, _)| file.position(i)).collect(),
            indices: text.into_iter().map(|(i, _)| i).collect(),
        }
    }

    /// Returns the index in the source of each character of the text.
    pub fn indices(&self) -> &[usize] {
        &self.indices
    }

    /// Returns the source position of the character at `index`. An
    /// index at or past the end maps to the column after the last
    /// character.
    pub fn position(&self, index: usize) -> Position {
        match (self.positions.get(index), self.positions.last()) {
            (Some(&at), _) => at,
            (None, Some(&last)) => Position {
                line: last.line,
                col: last.col + 1,
            },
            (None, None) => Position { line: 1, col: 1 },
        }
    }
}

/// One finding of a rule.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Finding {
    /// The name of the rule, without a tier.
    pub rule: String,
    /// The tier of the rule.
    pub tier: Tier,
    /// The kind of the lint, such as `Style`.
    pub kind: String,
    /// The line, counted from 1.
    pub line: usize,
    /// The column in characters, counted from 1.
    pub col: usize,
    /// The message of the rule.
    pub message: String,
}

impl Finding {
    /// Returns the line of output for the finding in the file at
    /// `path`.
    pub fn display<'a>(&'a self, path: &'a str) -> impl fmt::Display + 'a {
        Display {
            finding: self,
            path,
        }
    }

    /// Returns the finding in the file at `path` as one line of JSON,
    /// an object with the fields `path`, `line`, `col`, `tier`, `kind`,
    /// `rule`, and `message`.
    pub fn json(&self, path: &str) -> String {
        serde_json::json!({
            "path": path,
            "line": self.line,
            "col": self.col,
            "tier": self.tier.to_string(),
            "kind": self.kind,
            "rule": self.rule,
            "message": self.message,
        })
        .to_string()
    }
}

struct Display<'a> {
    finding: &'a Finding,
    path: &'a str,
}

impl fmt::Display for Display<'_> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let Finding {
            rule,
            tier,
            kind,
            line,
            col,
            message,
        } = self.finding;
        write!(
            f,
            "{}:{line}:{col}: [{tier}] {kind}::{rule}: {message}",
            self.path
        )
    }
}

/// Orders the findings for output, Required findings first and then by
/// line. Two tiers of one rule can report one phrase. When two findings
/// share a line, a kind, a rule, and a message, only the one of the
/// higher tier stays.
pub fn order(mut findings: Vec<Finding>) -> Vec<Finding> {
    findings.sort_by(|a, b| {
        (a.tier, a.line, a.col, &a.kind, &a.rule, &a.message)
            .cmp(&(b.tier, b.line, b.col, &b.kind, &b.rule, &b.message))
    });
    let mut out: Vec<Finding> = Vec::new();
    for finding in findings {
        let shadowed = out.iter().any(|kept| {
            kept.tier < finding.tier
                && kept.line == finding.line
                && kept.kind == finding.kind
                && kept.rule == finding.rule
                && kept.message == finding.message
        });
        if !shadowed {
            out.push(finding);
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn position() {
        struct Case {
            name: &'static str,
            source: &'static str,
            index: usize,
            want: Position,
        }
        let cases = [
            Case {
                name: "first character",
                source: "ab\ncd",
                index: 0,
                want: Position { line: 1, col: 1 },
            },
            Case {
                name: "second line",
                source: "ab\ncd",
                index: 4,
                want: Position { line: 2, col: 2 },
            },
            Case {
                name: "multi-byte character",
                source: "é b",
                index: 2,
                want: Position { line: 1, col: 3 },
            },
            Case {
                name: "end",
                source: "ab",
                index: 2,
                want: Position { line: 1, col: 3 },
            },
            Case {
                name: "empty",
                source: "",
                index: 0,
                want: Position { line: 1, col: 1 },
            },
        ];
        for case in cases {
            let region = Region::markdown(case.source);
            assert_eq!(region.position(case.index), case.want, "{}", case.name);
        }
    }

    fn finding(tier: Tier, line: usize, col: usize, rule: &str, message: &str) -> Finding {
        Finding {
            rule: rule.to_string(),
            tier,
            kind: "Style".to_string(),
            line,
            col,
            message: message.to_string(),
        }
    }

    #[test]
    fn order_findings() {
        struct Case {
            name: &'static str,
            findings: Vec<Finding>,
            want: Vec<Finding>,
        }
        let cases = [
            Case {
                name: "required first, then by line",
                findings: vec![
                    finding(Tier::Optional, 1, 1, "A", "a"),
                    finding(Tier::Recommended, 2, 1, "B", "b"),
                    finding(Tier::Required, 5, 1, "C", "c"),
                    finding(Tier::Recommended, 1, 9, "D", "d"),
                ],
                want: vec![
                    finding(Tier::Required, 5, 1, "C", "c"),
                    finding(Tier::Recommended, 1, 9, "D", "d"),
                    finding(Tier::Recommended, 2, 1, "B", "b"),
                    finding(Tier::Optional, 1, 1, "A", "a"),
                ],
            },
            Case {
                name: "two tiers of one rule on one line",
                findings: vec![
                    finding(Tier::Recommended, 3, 8, "ProseFiller", "Filler."),
                    finding(Tier::Required, 3, 1, "ProseFiller", "Filler."),
                ],
                want: vec![finding(Tier::Required, 3, 1, "ProseFiller", "Filler.")],
            },
            Case {
                name: "two tiers with other messages",
                findings: vec![
                    finding(Tier::Recommended, 3, 8, "ProseHedge", "b"),
                    finding(Tier::Optional, 3, 1, "ProseHedge", "a"),
                ],
                want: vec![
                    finding(Tier::Recommended, 3, 8, "ProseHedge", "b"),
                    finding(Tier::Optional, 3, 1, "ProseHedge", "a"),
                ],
            },
            Case {
                name: "two tiers on other lines",
                findings: vec![
                    finding(Tier::Recommended, 4, 1, "ProseFiller", "Filler."),
                    finding(Tier::Required, 3, 1, "ProseFiller", "Filler."),
                ],
                want: vec![
                    finding(Tier::Required, 3, 1, "ProseFiller", "Filler."),
                    finding(Tier::Recommended, 4, 1, "ProseFiller", "Filler."),
                ],
            },
            Case {
                name: "one tier twice on one line",
                findings: vec![
                    finding(Tier::Recommended, 3, 9, "ProseHedge", "a"),
                    finding(Tier::Recommended, 3, 1, "ProseHedge", "a"),
                ],
                want: vec![
                    finding(Tier::Recommended, 3, 1, "ProseHedge", "a"),
                    finding(Tier::Recommended, 3, 9, "ProseHedge", "a"),
                ],
            },
        ];
        for case in cases {
            assert_eq!(order(case.findings), case.want, "{}", case.name);
        }
    }

    #[test]
    fn display() {
        let finding = finding(Tier::Required, 3, 7, "ProseAndOr", "`and/or`.");
        assert_eq!(
            finding.display("a.md").to_string(),
            "a.md:3:7: [Required] Style::ProseAndOr: `and/or`."
        );
    }

    #[test]
    fn json() {
        let finding = finding(Tier::Recommended, 3, 7, "ProseHedge", "Write \"is\".");
        let line = finding.json("docs/a \"b\".md");
        assert!(!line.contains('\n'), "{line}");
        let got: serde_json::Value = serde_json::from_str(&line).expect("the line is JSON");
        assert_eq!(
            got,
            serde_json::json!({
                "path": "docs/a \"b\".md",
                "line": 3,
                "col": 7,
                "tier": "Recommended",
                "kind": "Style",
                "rule": "ProseHedge",
                "message": "Write \"is\".",
            })
        );
    }
}
