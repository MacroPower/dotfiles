//! The native checks, which read the markdown model of a region in Rust.
//!
//! Each check is a function that takes the [`Markdown`] model of a
//! region and returns a [`Hit`] for each finding. [`CHECKS`] lists each
//! check with its rule name and its tier. The module of a check sits
//! under the module of its tier, as its fixtures sit under
//! `configs/harper/fixtures/<tier>/`.

pub mod optional;
pub mod recommended;
pub mod required;
pub mod runs;
pub mod sentences;

use crate::markdown::Markdown;
use crate::region::{Finding, Region};
use crate::rules::Tier;

/// `configs/harper/comment-checks.txt`, which names each check that
/// reads the comments of a source file. A `!<kind>` after a name skips
/// the comments of that kind, which `SYNTAXES` of `src/comments.rs`
/// gives each extension.
const COMMENT_CHECKS: &str =
    include_str!(concat!(env!("PROSE_LINT_HARPER"), "/comment-checks.txt"));

/// Returns whether the check named `name` reads the comments of the
/// kind `kind`.
pub fn reads_comments(name: &str, kind: &str) -> bool {
    COMMENT_CHECKS
        .lines()
        .filter(|l| !l.trim().is_empty() && !l.starts_with('#'))
        .any(|l| {
            let mut words = l.split_whitespace();
            words.next() == Some(name) && !words.any(|w| w.strip_prefix('!') == Some(kind))
        })
}

/// One finding of a check.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Hit {
    /// The index in the region of the first character of the finding.
    pub at: usize,
    /// The message of the finding.
    pub message: String,
}

/// A native check and the rule that it reports.
pub struct Check {
    /// The name of the rule, without a tier.
    pub name: &'static str,
    /// The tier of the rule.
    pub tier: Tier,
    /// The kind of each finding, such as `Style`.
    pub kind: &'static str,
    /// Returns the findings of the check on a model.
    pub run: fn(&Markdown) -> Vec<Hit>,
}

/// Every native check.
pub const CHECKS: &[Check] = &[
    Check {
        name: "ProseArrow",
        tier: Tier::Required,
        kind: "Style",
        run: required::prose_arrow::check,
    },
    Check {
        name: "ProseEmoji",
        tier: Tier::Required,
        kind: "Style",
        run: required::prose_emoji::check,
    },
    Check {
        name: "ProseHeading",
        tier: Tier::Required,
        kind: "Style",
        run: required::prose_heading::check,
    },
    Check {
        name: "ProsePluralParen",
        tier: Tier::Required,
        kind: "Style",
        run: required::prose_plural_paren::check,
    },
    Check {
        name: "ProseTypography",
        tier: Tier::Required,
        kind: "Punctuation",
        run: required::prose_typography::check,
    },
    Check {
        name: "ProseArrow",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_arrow::check,
    },
    Check {
        name: "ProseCallout",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_callout::check,
    },
    Check {
        name: "ProseCapsEmphasis",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_caps_emphasis::check,
    },
    Check {
        name: "ProseDoubleNegative",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_double_negative::check,
    },
    Check {
        name: "ProseExclamation",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_exclamation::check,
    },
    Check {
        name: "ProseHeading",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_heading::check,
    },
    Check {
        name: "ProseHeadingLevel",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_heading_level::check,
    },
    Check {
        name: "ProseHiddenList",
        tier: Tier::Recommended,
        kind: "Readability",
        run: recommended::prose_hidden_list::check,
    },
    Check {
        name: "ProseLatinAbbreviation",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_latin_abbreviation::check,
    },
    Check {
        name: "ProseLinkText",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_link_text::check,
    },
    Check {
        name: "ProseLongParenthetical",
        tier: Tier::Recommended,
        kind: "Readability",
        run: recommended::prose_long_parenthetical::check,
    },
    Check {
        name: "ProseNumberedReference",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_numbered_reference::check,
    },
    Check {
        name: "ProseOpenList",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_open_list::check,
    },
    Check {
        name: "ProseOverUnder",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_over_under::check,
    },
    Check {
        name: "ProseParagraphLength",
        tier: Tier::Recommended,
        kind: "Readability",
        run: recommended::prose_paragraph_length::check,
    },
    Check {
        name: "ProseRangeEnds",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_range_ends::check,
    },
    Check {
        name: "ProseSentenceLength",
        tier: Tier::Recommended,
        kind: "Readability",
        run: recommended::prose_sentence_length::check,
    },
    Check {
        name: "ProseSlash",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_slash::check,
    },
    Check {
        name: "ProseTrailingCondition",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_trailing_condition::check,
    },
    Check {
        name: "ProseWallOfText",
        tier: Tier::Recommended,
        kind: "Readability",
        run: recommended::prose_wall_of_text::check,
    },
    Check {
        name: "ProseIndefiniteOpener",
        tier: Tier::Optional,
        kind: "Style",
        run: optional::prose_indefinite_opener::check,
    },
    Check {
        name: "ProseOpeningPronoun",
        tier: Tier::Optional,
        kind: "Style",
        run: optional::prose_opening_pronoun::check,
    },
    Check {
        name: "ProseSentenceLength",
        tier: Tier::Optional,
        kind: "Readability",
        run: optional::prose_sentence_length::check,
    },
    Check {
        name: "ProseWallOfText",
        tier: Tier::Optional,
        kind: "Readability",
        run: optional::prose_wall_of_text::check,
    },
];

/// Runs each check of [`CHECKS`] on `region`, other than the checks of
/// the rules that `skips` names, and returns the findings.
pub fn run(region: &Region, skips: &[&str]) -> Vec<Finding> {
    run_some(region, |check| !skips.contains(&check.name))
}

/// Returns the findings of the checks on a source file of the comment
/// kind `kind`, other than the checks of the rules that `skips` names.
/// Each check that [`reads_comments`] names runs on each region of
/// `regions`. ProseTypography reads the whole text of `file`, strings
/// included.
pub fn run_source(file: &Region, regions: &[Region], kind: &str, skips: &[&str]) -> Vec<Finding> {
    let mut findings: Vec<Finding> = regions
        .iter()
        .flat_map(|region| {
            run_some(region, |check| {
                !skips.contains(&check.name) && reads_comments(check.name, kind)
            })
        })
        .collect();
    if !skips.contains(&"ProseTypography") {
        let hits = required::prose_typography::raw(&file.text);
        findings.extend(hits.into_iter().map(|hit| {
            let at = file.position(hit.at);
            Finding {
                rule: "ProseTypography".to_string(),
                tier: Tier::Required,
                kind: "Punctuation".to_string(),
                line: at.line,
                col: at.col,
                message: hit.message,
            }
        }));
    }
    findings
}

/// Runs each check of [`CHECKS`] that `keep` keeps on `region`, and
/// returns the findings.
fn run_some(region: &Region, keep: impl Fn(&Check) -> bool) -> Vec<Finding> {
    let model = Markdown::new(region);
    CHECKS
        .iter()
        .filter(|check| keep(check))
        .flat_map(|check| {
            (check.run)(&model).into_iter().map(|hit| {
                let at = region.position(hit.at);
                Finding {
                    rule: check.name.to_string(),
                    tier: check.tier,
                    kind: check.kind.to_string(),
                    line: at.line,
                    col: at.col,
                    message: hit.message,
                }
            })
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeSet;

    use super::*;

    #[test]
    fn comment_checks() {
        // Each name of comment-checks.txt is a native check, and each
        // kind that it skips is a kind of the table of comment syntaxes.
        let kinds: BTreeSet<_> = crate::comments::SYNTAXES.iter().map(|s| s.kind).collect();
        for line in COMMENT_CHECKS.lines() {
            if line.trim().is_empty() || line.starts_with('#') {
                continue;
            }
            let mut words = line.split_whitespace();
            let name = words.next().expect("the line holds a word");
            assert!(CHECKS.iter().any(|c| c.name == name), "{name}");
            for word in words {
                let kind = word.strip_prefix('!').expect("a kind opens with `!`");
                assert!(kinds.contains(kind), "{name} {word}");
            }
        }
        assert!(reads_comments("ProseSlash", "slash"));
        assert!(!reads_comments("ProseSlash", "go"));
        assert!(!reads_comments("ProseArrow", "slash"));
    }

    #[test]
    fn names() {
        // Each check reports a rule of its own, and no Weir rule of the
        // same tier shares the name.
        let mut seen = BTreeSet::new();
        for check in CHECKS {
            assert!(
                seen.insert((check.name, check.tier)),
                "{} {}",
                check.tier,
                check.name
            );
            assert!(
                !crate::rules::WEIR_RULES
                    .iter()
                    .any(|&(name, tier, _)| name == check.name && tier == check.tier),
                "{} {}",
                check.tier,
                check.name
            );
        }
    }
}
