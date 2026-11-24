//! The Weir rules and the Harper built-ins that the binary embeds.
//!
//! `build.rs` reads them from `configs/harper/rules/<tier>/`. Each tier
//! directory holds a `.weir` file for each Weir rule and a
//! `builtins.txt` that names the Harper built-ins of the tier.

use std::collections::HashMap;
use std::fmt;
use std::sync::Arc;

use harper_core::linting::{Lint, LintGroup};
use harper_core::parsers::Parser;
use harper_core::spell::FstDictionary;
use harper_core::weirpack::Weirpack;
use harper_core::{Dialect, Document};

/// The tier of a rule, which says how often the rule misreads a
/// sentence. `Required` sorts first.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum Tier {
    /// A rule that matches an exact phrase.
    Required,
    /// A rule that matches a pattern that can misread.
    Recommended,
    /// A rule that suggests a change.
    Optional,
}

impl Tier {
    /// Returns the name of the directory of the tier under
    /// `configs/harper/rules`, such as `required`.
    pub fn dir(self) -> &'static str {
        match self {
            Tier::Required => "required",
            Tier::Recommended => "recommended",
            Tier::Optional => "optional",
        }
    }
}

impl fmt::Display for Tier {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            Tier::Required => "Required",
            Tier::Recommended => "Recommended",
            Tier::Optional => "Optional",
        })
    }
}

include!(concat!(env!("OUT_DIR"), "/rules.rs"));

/// The rules that a changelog and a commit message skip. Both describe
/// a change, so ProseChangeNarration, ProseDatedClaim, and ProseRoadmap
/// do not apply. ProseFutureTense does not apply either, because a
/// message might say what the system does after the change. A message
/// describes the system at one commit. So the counts of ProseStaleCount,
/// the line numbers of ProseNumberedReference, and the `older` and
/// `newer` of ProseRelativeVersion stay true there.
pub const TIME_SENSITIVE: &[&str] = &[
    "ProseChangeNarration",
    "ProseDatedClaim",
    "ProseFutureTense",
    "ProseNumberedReference",
    "ProseRelativeVersion",
    "ProseRoadmap",
    "ProseStaleCount",
];

/// Returns the bare name and the tier of each Weir rule and built-in.
pub fn names() -> impl Iterator<Item = (&'static str, Tier)> {
    WEIR_RULES
        .iter()
        .map(|&(name, tier, _)| (name, tier))
        .chain(BUILTINS.iter().copied())
}

/// Returns whether an entry of `entries` names the rule `name` of
/// `tier`. An entry names a rule in every tier by its bare name, such as
/// `ProseHedge`, or in one tier as `<tier>/<Name>`, such as
/// `optional/ProseHedge`.
pub fn matches(entries: &[&str], name: &str, tier: Tier) -> bool {
    entries
        .iter()
        .any(|e| *e == name || e.split_once('/') == Some((tier.dir(), name)))
}

/// An error of building the lint group.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// A Weir rule does not compile.
    #[error("compiling the weir rules")]
    Weir(#[from] harper_core::weirpack::Error),
}

/// The lint group of every rule that a file enables.
pub struct Rules {
    group: LintGroup,
    dictionary: Arc<FstDictionary>,
    /// The bare name and the tier of each rule, by its name in the
    /// group.
    names: HashMap<String, (&'static str, Tier)>,
}

impl Rules {
    /// Builds the lint group of every Weir rule and built-in, other than
    /// the rules that `skips` names in the form that [`matches`] reads.
    /// It lints with the American dialect.
    ///
    /// Two tiers can hold a rule of one name, so the group names each
    /// Weir rule `<Name>_<Tier>`.
    pub fn new(skips: &[&str]) -> Result<Self, Error> {
        let dictionary = FstDictionary::curated();
        let mut names = HashMap::new();
        let mut pack = Weirpack::default();
        for &(name, tier, source) in WEIR_RULES {
            if !matches(skips, name, tier) {
                let internal = format!("{name}_{tier}");
                pack.add_rule(internal.clone(), source);
                names.insert(internal, (name, tier));
            }
        }
        for &(name, tier) in BUILTINS {
            if !matches(skips, name, tier) {
                names.insert(name.to_string(), (name, tier));
            }
        }
        let mut group = LintGroup::new_curated(dictionary.clone(), Dialect::American);
        group.merge_from(pack.to_lint_group()?);
        group.set_all_rules_to(Some(false));
        for name in names.keys() {
            group.config.set_rule_enabled(name, true);
        }
        Ok(Self {
            group,
            dictionary,
            names,
        })
    }

    /// Lints the text that `parser` reads and returns each lint with the
    /// bare name and the tier of its rule. The span of each lint holds
    /// indexes of characters in `text`.
    pub fn lint(&mut self, text: &str, parser: &impl Parser) -> Vec<(&'static str, Tier, Lint)> {
        let document = Document::new(text, parser, &self.dictionary);
        let mut out = Vec::new();
        for (name, lints) in self.group.organized_lints(&document) {
            let Some(&(bare, tier)) = self.names.get(&name) else {
                continue;
            };
            out.extend(lints.into_iter().map(|lint| (bare, tier, lint)));
        }
        out
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rules_compile() {
        let rules = Rules::new(&[]).expect("every embedded rule compiles");
        assert!(
            rules.names.len() == WEIR_RULES.len() + BUILTINS.len(),
            "each rule has its own name in the group"
        );
    }

    #[test]
    fn skips() {
        let rules = Rules::new(TIME_SENSITIVE).expect("every embedded rule compiles");
        let bare: Vec<_> = rules.names.values().map(|(name, _)| *name).collect();
        for name in TIME_SENSITIVE {
            assert!(!bare.contains(name), "{name}");
        }
        assert!(bare.contains(&"ProseAndOr"), "ProseAndOr");
    }

    #[test]
    fn skips_one_tier() {
        let rules = Rules::new(&["recommended/ProseHedge"]).expect("every embedded rule compiles");
        let kept: Vec<_> = rules
            .names
            .values()
            .filter(|(name, _)| *name == "ProseHedge")
            .map(|(_, tier)| *tier)
            .collect();
        assert_eq!(kept, vec![Tier::Optional]);
    }
}
