//! The native checks, which read the markdown model of a region in Rust.
//!
//! Each check is a function that takes the [`Markdown`] model of a
//! region and returns a [`Hit`] for each finding. [`CHECKS`] lists each
//! check with its rule name and its tier. The module of a check sits
//! under the module of its tier, as its fixtures sit under
//! `configs/harper/fixtures/<tier>/`.

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
    use std::path::Path;
    use std::sync::LazyLock;

    use regex::Regex;

    use super::*;

    /// The directory that holds `rules/` and `fixtures/`, from build.rs.
    const HARPER: &str = env!("PROSE_LINT_HARPER");

    static PRETTIER_IGNORE: LazyLock<Regex> = LazyLock::new(|| {
        Regex::new(r"^<!--\s*prettier-ignore(-start|-end)?\s*-->$").expect("the pattern is valid")
    });
    static RULE_MARKS: LazyLock<Regex> = LazyLock::new(|| {
        Regex::new(r"^[ \t>]*((\*[ \t]*){3,}|(-[ \t]*){3,}|(_[ \t]*){3,})$")
            .expect("the pattern is valid")
    });

    /// Ends a line of a fixture on which a native check and the awk check
    /// that it replaces read the text in different ways. The reading of
    /// the native check stands there. The awk check reads the text of an
    /// HTML block as prose, for example, and pulldown-cmark ends some
    /// blocks where GFM does not. The test does not judge a native check
    /// on such a line.
    const DIFFERS: &str = "<!-- native check differs -->";

    /// Returns the number of each line of `text` that ends in
    /// [`DIFFERS`].
    fn differs(text: &str) -> BTreeSet<usize> {
        text.lines()
            .enumerate()
            .filter(|(_, line)| line.trim_end().ends_with(DIFFERS))
            .map(|(i, _)| i + 1)
            .collect()
    }

    /// Returns the number of each line of a bad fixture that needs a
    /// finding, as `expected_lines` in `configs/harper/check.py` does.
    /// A line that ends in [`DIFFERS`] does not need one.
    fn expected_lines(text: &str) -> BTreeSet<usize> {
        text.lines()
            .enumerate()
            .filter(|(_, line)| {
                !line.replace('>', "").trim().is_empty()
                    && !line.trim_end().ends_with("<!-- same finding -->")
                    && !line.trim_end().ends_with(DIFFERS)
                    && !PRETTIER_IGNORE.is_match(line.trim())
                    && !RULE_MARKS.is_match(line)
            })
            .map(|(i, _)| i + 1)
            .collect()
    }

    /// Returns the line, the column, and the message of each finding of
    /// `check` on `text`.
    fn lint(check: &Check, text: &str) -> BTreeSet<(usize, usize, String)> {
        let region = Region::markdown(text);
        run(&region, &[])
            .into_iter()
            .filter(|f| f.rule == check.name && f.tier == check.tier)
            .map(|f| (f.line, f.col, f.message))
            .collect()
    }

    #[test]
    fn fixtures() {
        // Each check must report each line of its bad fixture that
        // check.py expects a finding on. It must pass its good fixture
        // and the shared good fixture. A copy of each fixture with CRLF
        // line endings must draw the same findings. The test does not
        // judge a line that ends in DIFFERS.
        let fixtures = Path::new(HARPER).join("fixtures");
        let read = |path: &Path| {
            std::fs::read_to_string(path)
                .unwrap_or_else(|e| panic!("reading {}: {e}", path.display()))
        };
        let common = fixtures.join("common.good.md");
        // Each failure, so one run names every check that fails.
        let mut failures = Vec::new();
        for check in CHECKS {
            let dir = fixtures.join(check.tier.to_string().to_lowercase());
            let bad = dir.join(format!("{}.bad.md", check.name));
            let good = dir.join(format!("{}.good.md", check.name));
            let name = format!("{} {}", check.tier, check.name);

            let text = read(&bad);
            let got: BTreeSet<usize> = lint(check, &text).into_iter().map(|f| f.0).collect();
            let missing: Vec<_> = expected_lines(&text).difference(&got).copied().collect();
            if !missing.is_empty() {
                failures.push(format!("{name}: no finding on lines {missing:?}"));
            }
            for path in [&good, &common] {
                let text = read(path);
                let skip = differs(&text);
                let found: Vec<_> = lint(check, &text)
                    .into_iter()
                    .filter(|f| !skip.contains(&f.0))
                    .collect();
                if !found.is_empty() {
                    failures.push(format!("{name}: {}: {found:?}", path.display()));
                }
            }
            for path in [&bad, &good, &common] {
                let text = read(path);
                if lint(check, &text.replace('\n', "\r\n")) != lint(check, &text) {
                    failures.push(format!("{name}: {} with CRLF line endings", path.display()));
                }
            }
        }
        assert!(failures.is_empty(), "{failures:#?}");
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
