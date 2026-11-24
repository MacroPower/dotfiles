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
pub mod sentences;

use crate::markdown::Markdown;
use crate::region::{Finding, Region};
use crate::rules::Tier;

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
        name: "ProseSlash",
        tier: Tier::Recommended,
        kind: "Style",
        run: recommended::prose_slash::check,
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
];

/// Runs each check of [`CHECKS`] on `region`, other than the checks of
/// the rules that `skips` names, and returns the findings.
pub fn run(region: &Region, skips: &[&str]) -> Vec<Finding> {
    let model = Markdown::new(region);
    CHECKS
        .iter()
        .filter(|check| !skips.contains(&check.name))
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
