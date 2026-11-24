//! The tests that every rule passes.
//!
//! A rule is a Weir rule or a Harper built-in of `rules/<tier>/`, or a
//! native check of [`CHECKS`]. Each rule has a pair of fixtures,
//! `fixtures/<tier>/<Rule>.bad.md` and `.good.md`. The binary must
//! report each line of the bad fixture that [`expected_lines`] returns.
//! It must not report the rule on the good fixture or on the shared
//! `fixtures/common.good.md`, which holds ordinary technical prose. A
//! copy of each of the three files with CRLF line endings must draw the
//! same findings.
//!
//! Two tiers of one rule must not report the same phrase. On each bad
//! line of one tier, another tier of the rule fails when it reports a
//! finding that the own tier does not report with the same message. The
//! binary prints only the higher tier of two such findings.
//!
//! The tests also run the tests inside each Weir rule, as
//! `harper-cli test` does. Each plain phrase that the main expression
//! of a Weir rule lists must fire on its own in a probe sentence. A
//! phrase must not hold a shorter phrase of the list. The comment that
//! opens each Weir rule and the doc comment of each native check follow
//! the layout of the Comments section of the `CLAUDE.md` of the crate.
//!
//! Each check is a function that returns one line for each failure, so
//! the tests of this module can run it on inputs that fail.

use std::collections::{BTreeMap, BTreeSet, HashMap};
use std::fs;
use std::path::Path;
use std::sync::LazyLock;

use harper_core::weirpack::Weirpack;
use regex::Regex;

use crate::checks::CHECKS;
use crate::files::{self, Input};
use crate::region::Finding;
use crate::rules::{BUILTINS, Rules, Tier, WEIR_RULES};

/// The directory of the crate, which holds `rules/` and `fixtures/`.
const CRATE_DIR: &str = env!("CARGO_MANIFEST_DIR");

const TIERS: [Tier; 3] = [Tier::Required, Tier::Recommended, Tier::Optional];

/// The good fixture that every rule must pass, under `fixtures/`.
const COMMON: &str = "common.good.md";

/// Ends a bad line whose finding a finding on a nearby line covers,
/// such as the heading after it that skips a level.
const SAME_FINDING: &str = "<!-- same finding -->";

/// Ends a line of a bad fixture that a native check reads in another
/// way than GFM, so the check does not report it. The markdown model
/// blanks the text of an HTML block, for example, and pulldown-cmark
/// ends some blocks where GFM does not. The tests do not judge a native
/// check on such a line.
const DIFFERS: &str = "<!-- native check differs -->";

/// The sections of a rule comment, in the order that the Comments
/// section of the `CLAUDE.md` of the crate gives them. Change that
/// section and these names together.
const SECTIONS: [&str; 5] = [
    "Overview",
    "Expression",
    "False Positives",
    "Related Rules",
    "Measurement",
];

/// The sections that every rule comment carries.
const REQUIRED_SECTIONS: [&str; 2] = ["Overview", "Measurement"];

static PRETTIER_IGNORE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^<!--\s*prettier-ignore(-start|-end)?\s*-->$").expect("the pattern is valid")
});
static RULE_MARKS: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^[ \t>]*((\*[ \t]*){3,}|(-[ \t]*){3,}|(_[ \t]*){3,})$")
        .expect("the pattern is valid")
});
/// A list entry made only of words, which a probe sentence can quote.
static PLAIN_ENTRY: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[A-Za-z']+( [A-Za-z']+)*$").expect("the pattern is valid"));
/// An `expr` statement, joined onto one line.
static EXPR_STMT: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^expr (\w+) (.*)$").expect("the pattern is valid"));

/// The tags of Harper's part-of-speech tagger and the Weir keyword
/// `PROG`. An entry that holds one names a word class and not a word.
const TAGS: [&str; 18] = [
    "ADJ", "ADP", "ADV", "AUX", "CCONJ", "DET", "INTJ", "NOUN", "NUM", "PART", "PRON", "PROPN",
    "PUNCT", "SCONJ", "SYM", "VERB", "X", "PROG",
];

/// Where the code of a rule comes from.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Source {
    Weir,
    Builtin,
    Native,
}

/// A rule of one tier.
#[derive(Clone, Copy, Debug)]
struct Rule {
    name: &'static str,
    tier: Tier,
    source: Source,
}

impl Rule {
    /// Returns the path under `fixtures/` of the fixture of `kind`,
    /// `bad` or `good`.
    fn fixture(&self, kind: &str) -> String {
        format!("{}/{}.{kind}.md", dir(self.tier), self.name)
    }

    /// Returns the line, the column, and the message of each finding of
    /// the rule in `findings`.
    fn hits(&self, findings: &[Finding]) -> BTreeSet<(usize, usize, String)> {
        findings
            .iter()
            .filter(|f| f.rule == self.name && f.tier == self.tier)
            .map(|f| (f.line, f.col, f.message.clone()))
            .collect()
    }
}

/// Returns the name of the directory of `tier`, such as `required`.
fn dir(tier: Tier) -> String {
    tier.to_string().to_lowercase()
}

/// Returns every rule that the binary embeds.
fn rules() -> Vec<Rule> {
    let weir = WEIR_RULES.iter().map(|&(name, tier, _)| Rule {
        name,
        tier,
        source: Source::Weir,
    });
    let builtins = BUILTINS.iter().map(|&(name, tier)| Rule {
        name,
        tier,
        source: Source::Builtin,
    });
    let native = CHECKS.iter().map(|check| Rule {
        name: check.name,
        tier: check.tier,
        source: Source::Native,
    });
    weir.chain(builtins).chain(native).collect()
}

/// The text of each markdown fixture, by its path under `fixtures/`.
type Fixtures = BTreeMap<String, String>;

/// Reads the shared good fixture and each markdown file of the tier
/// directories under `fixtures/`.
fn read_fixtures() -> Fixtures {
    let root = Path::new(CRATE_DIR).join("fixtures");
    let read = |path: &Path| {
        fs::read_to_string(path).unwrap_or_else(|e| panic!("reading {}: {e}", path.display()))
    };
    let mut fixtures = Fixtures::new();
    fixtures.insert(COMMON.to_string(), read(&root.join(COMMON)));
    for tier in TIERS {
        let tier_dir = root.join(dir(tier));
        for entry in fs::read_dir(&tier_dir)
            .unwrap_or_else(|e| panic!("reading {}: {e}", tier_dir.display()))
        {
            let path = entry.expect("listing a fixtures directory").path();
            let name = path
                .file_name()
                .and_then(|n| n.to_str())
                .expect("fixture names are UTF-8");
            if name.ends_with(".md") {
                fixtures.insert(format!("{}/{name}", dir(tier)), read(&path));
            }
        }
    }
    fixtures
}

/// Returns a function that lints markdown text as the binary does,
/// before it drops the finding of a lower tier that a higher tier
/// repeats. The function lints each text one time.
fn linter() -> impl FnMut(&str) -> Vec<Finding> {
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    let mut seen: HashMap<String, Vec<Finding>> = HashMap::new();
    move |text| {
        seen.entry(text.to_string())
            .or_insert_with(|| crate::findings(&mut rules, &[], text))
            .clone()
    }
}

/// Returns the number of each line of `text` that ends in [`DIFFERS`].
fn differs(text: &str) -> BTreeSet<usize> {
    text.lines()
        .enumerate()
        .filter(|(_, line)| line.trim_end().ends_with(DIFFERS))
        .map(|(i, _)| i + 1)
        .collect()
}

/// Returns the number of each line of a bad fixture that needs a
/// finding.
///
/// A blank line does not need one, and a line that holds only `>`
/// marks is a blank line inside a blockquote. A line that ends in
/// [`SAME_FINDING`] does not need one either. Nor does a line that
/// holds only a prettier-ignore comment, so a fixture can keep prettier
/// from adding a blank line between two blocks. A line that holds only
/// `>` marks and the marks of a thematic break (`***`, `- - -`) does
/// not hold text for a finding. For a native check, a line that ends in
/// [`DIFFERS`] does not need one.
fn expected_lines(text: &str, native: bool) -> BTreeSet<usize> {
    text.lines()
        .enumerate()
        .filter(|(_, line)| {
            !line.replace('>', "").trim().is_empty()
                && !line.trim_end().ends_with(SAME_FINDING)
                && !(native && line.trim_end().ends_with(DIFFERS))
                && !PRETTIER_IGNORE.is_match(line.trim())
                && !RULE_MARKS.is_match(line)
        })
        .map(|(i, _)| i + 1)
        .collect()
}

/// Checks each rule of `rules` against its fixtures with `lint`.
fn fixture_failures(
    rules: &[Rule],
    fixtures: &Fixtures,
    lint: &mut dyn FnMut(&str) -> Vec<Finding>,
) -> Vec<String> {
    let Some(common) = fixtures.get(COMMON) else {
        return vec![format!("{COMMON}: missing the shared good fixture")];
    };
    let mut failures = Vec::new();
    for rule in rules {
        let (bad, good) = (rule.fixture("bad"), rule.fixture("good"));
        let (Some(bad_text), Some(good_text)) = (fixtures.get(&bad), fixtures.get(&good)) else {
            failures.push(format!(
                "{} {}: missing the fixture pair {bad} and {good}",
                rule.tier, rule.name
            ));
            continue;
        };
        let native = rule.source == Source::Native;

        let got: BTreeSet<usize> = rule.hits(&lint(bad_text)).iter().map(|h| h.0).collect();
        for line in expected_lines(bad_text, native).difference(&got) {
            failures.push(format!(
                "{bad}:{line}: no {} {} finding",
                rule.tier, rule.name
            ));
        }
        for (path, text) in [(good.as_str(), good_text), (COMMON, common)] {
            let skip = if native {
                differs(text)
            } else {
                BTreeSet::new()
            };
            for (line, _, message) in rule.hits(&lint(text)) {
                if !skip.contains(&line) {
                    failures.push(format!(
                        "{path}:{line}: an unexpected {} {} finding: {message}",
                        rule.tier, rule.name
                    ));
                }
            }
        }
        for (path, text) in [(bad.as_str(), bad_text), (good.as_str(), good_text)] {
            let crlf = text.replace('\n', "\r\n");
            if rule.hits(&lint(&crlf)) != rule.hits(&lint(text)) {
                failures.push(format!(
                    "{path}: other {} {} findings with CRLF line endings",
                    rule.tier, rule.name
                ));
            }
        }
        if rule.hits(&lint(&common.replace('\n', "\r\n"))) != rule.hits(&lint(common)) {
            failures.push(format!(
                "{COMMON}: other {} {} findings with CRLF line endings",
                rule.tier, rule.name
            ));
        }
    }
    failures
}

/// Fails each bad line of a rule that another tier of the same rule
/// reports, unless the own tier reports that line with the same
/// message.
fn tier_failures(
    rules: &[Rule],
    fixtures: &Fixtures,
    lint: &mut dyn FnMut(&str) -> Vec<Finding>,
) -> Vec<String> {
    let mut failures = Vec::new();
    for own in rules {
        let bad = own.fixture("bad");
        let Some(text) = fixtures.get(&bad) else {
            continue;
        };
        let others: Vec<_> = rules
            .iter()
            .filter(|r| r.name == own.name && r.tier != own.tier)
            .collect();
        if others.is_empty() {
            continue;
        }
        let findings = lint(text);
        let want = expected_lines(text, own.source == Source::Native);
        let mine: BTreeSet<(usize, String)> = own
            .hits(&findings)
            .into_iter()
            .map(|(line, _, message)| (line, message))
            .collect();
        for other in others {
            let lines: BTreeSet<usize> = other
                .hits(&findings)
                .into_iter()
                .filter(|(line, _, message)| {
                    want.contains(line) && !mine.contains(&(*line, message.clone()))
                })
                .map(|h| h.0)
                .collect();
            for line in lines {
                failures.push(format!(
                    "{bad}:{line}: the {} {} also reports this line",
                    other.tier, other.name
                ));
            }
        }
    }
    failures
}

/// Fails each fixture whose name `known` lacks. Each entry of `known` is
/// the path of a rule under its tier, such as `required/ProseAndOr`.
fn orphan_failures(fixtures: &Fixtures, known: &BTreeSet<String>) -> Vec<String> {
    fixtures
        .keys()
        .filter(|path| path.as_str() != COMMON)
        .filter_map(|path| {
            let stem = path
                .strip_suffix(".bad.md")
                .or_else(|| path.strip_suffix(".good.md"))
                .unwrap_or(path);
            let (tier, name) = stem.split_once('/').unwrap_or(("", stem));
            (!known.contains(stem)).then(|| format!("{path}: no {name} rule under rules/{tier}"))
        })
        .collect()
}

/// Fails each entry of the rules directory `root` that build.rs does
/// not read. build.rs reads only the tier directories, and in each one
/// only the `.weir` files and `builtins.txt`. Any other entry holds a
/// rule that the binary does not run. The check skips each name that
/// opens with a dot, such as a `.DS_Store` file.
fn rule_dir_failures(root: &Path) -> Vec<String> {
    let names = |path: &Path| -> Vec<String> {
        let mut names: Vec<String> = fs::read_dir(path)
            .unwrap_or_else(|e| panic!("reading {}: {e}", path.display()))
            .map(|entry| {
                let name = entry.expect("listing a rules directory").file_name();
                name.into_string().expect("rule names are UTF-8")
            })
            .filter(|name| !name.starts_with('.'))
            .collect();
        names.sort();
        names
    };
    let tiers: Vec<String> = TIERS.into_iter().map(dir).collect();
    let mut failures = Vec::new();
    for name in names(root) {
        let path = root.join(&name);
        if !tiers.contains(&name) || !path.is_dir() {
            failures.push(format!("rules/{name}: not a tier directory"));
            continue;
        }
        for file in names(&path) {
            if !file.ends_with(".weir") && file != "builtins.txt" {
                failures.push(format!(
                    "rules/{name}/{file}: not a Weir rule or builtins.txt"
                ));
            }
        }
    }
    failures
}

/// Fails each built-in that two tiers list.
fn builtin_failures(builtins: &[(&str, Tier)]) -> Vec<String> {
    let mut seen: BTreeMap<&str, Tier> = BTreeMap::new();
    let mut failures = Vec::new();
    for &(name, tier) in builtins {
        match seen.get(name) {
            Some(first) => failures.push(format!(
                "rules/{}/builtins.txt: {name} is already listed under {}",
                dir(tier),
                dir(*first)
            )),
            None => {
                seen.insert(name, tier);
            }
        }
    }
    failures
}

/// Runs the tests inside each Weir rule of `rules`, by its name and its
/// source.
fn weir_test_failures(rules: &[(String, &str)]) -> Vec<String> {
    let mut pack = Weirpack::default();
    for (name, source) in rules {
        pack.add_rule(name.clone(), *source);
    }
    let results = pack.run_tests().expect("every Weir rule compiles");
    let mut failures: Vec<String> = results
        .into_iter()
        .flat_map(|(name, results)| {
            results.into_iter().map(move |r| {
                if r.expected == r.got {
                    format!("{name}: `{}` still draws a finding", r.got)
                } else {
                    format!(
                        "{name}: the test that expects `{}` gets `{}`",
                        r.expected, r.got
                    )
                }
            })
        })
        .collect();
    failures.sort();
    failures
}

/// Splits a `[a, (b c), [d, e]]` list into its top-level alternatives.
fn split_alternatives(body: &str) -> Vec<String> {
    let inner = &body.trim()[1..body.trim().len() - 1];
    let mut parts = Vec::new();
    let mut depth = 0i32;
    let mut current = String::new();
    for c in inner.chars() {
        match c {
            '(' | '[' | '<' => depth += 1,
            ')' | ']' | '>' => depth -= 1,
            _ => {}
        }
        if c == ',' && depth == 0 {
            parts.push(current.trim().to_string());
            current.clear();
        } else {
            current.push(c);
        }
    }
    if !current.trim().is_empty() {
        parts.push(current.trim().to_string());
    }
    parts
}

/// Returns the body of each `expr` statement of a Weir rule, by the
/// name of the expression.
///
/// harper-core ends a statement at each newline outside a `[...]` of
/// the statement, as the function does. The function joins the lines
/// of a statement with a space, which Weir reads the same as the
/// newline.
fn expr_bodies(source: &str) -> HashMap<String, String> {
    let mut exprs = HashMap::new();
    let mut stmt = String::new();
    let mut depth = 0;
    for line in source.lines() {
        let part = line.trim();
        if stmt.is_empty() && (part.is_empty() || part.starts_with('#')) {
            continue;
        }
        if !stmt.is_empty() {
            stmt.push(' ');
        }
        stmt.push_str(part);
        depth += part.matches('[').count() as isize - part.matches(']').count() as isize;
        if depth <= 0 {
            if let Some(c) = EXPR_STMT.captures(&stmt) {
                exprs.insert(c[1].to_string(), c[2].to_string());
            }
            stmt.clear();
            depth = 0;
        }
    }
    exprs
}

/// Returns the phrases of words alone that the main expression of a
/// Weir rule lists.
///
/// An `@name` alternative that refers to a list adds the phrases of that
/// list, because each one matches on its own. A bare word counts as a
/// phrase, and so does a sequence of bare words in parentheses. A main
/// expression that is one such sequence counts too.
fn plain_entries(source: &str) -> Vec<String> {
    let exprs = expr_bodies(source);

    fn plain(alt: &str) -> bool {
        PLAIN_ENTRY.is_match(alt) && !alt.split(' ').any(|w| TAGS.contains(&w))
    }

    fn walk(exprs: &HashMap<String, String>, body: &str, seen: &[&str]) -> Vec<String> {
        let body = body.trim();
        if !(body.starts_with('[') && body.ends_with(']')) {
            return if plain(body) {
                vec![body.to_string()]
            } else {
                Vec::new()
            };
        }
        let mut found = Vec::new();
        for alt in split_alternatives(body) {
            if let Some(name) = alt.strip_prefix('@')
                && let Some(list) = exprs.get(name)
                && !seen.contains(&name)
            {
                let mut seen = seen.to_vec();
                seen.push(name);
                found.extend(walk(exprs, list, &seen));
                continue;
            }
            let alt = alt
                .strip_prefix('(')
                .and_then(|a| a.strip_suffix(')'))
                .map_or(alt.as_str(), str::trim);
            if plain(alt) {
                found.push(alt.to_string());
            }
        }
        found
    }

    exprs
        .get("main")
        .map(|main| walk(&exprs, main, &["main"]))
        .unwrap_or_default()
}

/// Probes each plain entry of a Weir rule with `lint`, and compares the
/// entries with each other.
fn entry_failures(
    rule: &Rule,
    source: &str,
    lint: &mut dyn FnMut(&str) -> Vec<Finding>,
) -> Vec<String> {
    let entries = plain_entries(source);
    if entries.is_empty() {
        return Vec::new();
    }
    let path = format!("rules/{}/{}.weir", dir(rule.tier), rule.name);
    let probe: Vec<String> = entries
        .iter()
        .map(|e| format!("Then {e} happens here."))
        .collect();
    let got: BTreeSet<usize> = rule
        .hits(&lint(&(probe.join("\n\n") + "\n")))
        .into_iter()
        .map(|h| h.0)
        .collect();
    let mut failures = Vec::new();
    for (i, entry) in entries.iter().enumerate() {
        if !got.contains(&(2 * i + 1)) {
            failures.push(format!("{path}: entry `{entry}` never fires on its own"));
        }
    }
    let words: Vec<String> = entries
        .iter()
        .map(|e| format!(" {} ", e.to_lowercase()))
        .collect();
    for (long, long_words) in entries.iter().zip(&words) {
        for (short, short_words) in entries.iter().zip(&words) {
            if short != long && long_words.contains(short_words.as_str()) {
                failures.push(format!(
                    "{path}: entry `{long}` repeats `{short}`, which already matches it"
                ));
            }
        }
    }
    failures
}

/// Returns the text of each line of the comment that opens `source`,
/// whose lines start with `marker`, without the marker and without the
/// whitespace that ends the line.
fn comment_lines(source: &str, marker: &str) -> Vec<String> {
    source
        .lines()
        .map_while(|line| line.strip_prefix(marker))
        .map(|text| text.trim_end().to_string())
        .collect()
}

/// Checks the lines of a rule comment against the section layout.
///
/// A heading is a comment line that holds only a section name. Some
/// other lines count as a heading too, so a misspelled or invented
/// section name fails. Such a line is a paragraph of its own, is not
/// indented past the one space after the marker, and does not end in a
/// period. A line is a
/// paragraph of its own when a blank comment line or an end of the
/// comment sits on each side of it. A failure on one line opens with
/// its number.
fn layout_failures(comment: &[String]) -> Vec<String> {
    if comment.iter().all(String::is_empty) {
        return vec!["the file does not open with a comment".to_string()];
    }
    let index = |name: &str| SECTIONS.iter().position(|&s| s == name);
    let mut failures = Vec::new();
    if comment[0].trim() != SECTIONS[0] {
        failures.push(format!(
            "1: the comment does not open with the {} heading",
            SECTIONS[0]
        ));
    }
    let mut seen: Vec<&str> = Vec::new();
    for (i, text) in comment.iter().enumerate() {
        let n = i + 1;
        let name = text.trim();
        let blank_before = i > 0 && comment[i - 1].is_empty();
        let blank_after = comment.get(i + 1).is_some_and(String::is_empty);
        if let Some(at) = index(name) {
            let later = seen
                .iter()
                .filter(|&&s| index(s) > Some(at))
                .max_by_key(|&&s| index(s));
            if seen.contains(&name) {
                failures.push(format!("{n}: the {name} heading repeats"));
            } else if let Some(later) = later {
                failures.push(format!("{n}: the {name} heading must come before {later}"));
            }
            seen.push(name);
            if i > 0 && !blank_before {
                failures.push(format!(
                    "{n}: the {name} heading needs a blank comment line before it"
                ));
            }
            if !blank_after {
                failures.push(format!(
                    "{n}: the {name} heading needs a blank comment line after it"
                ));
            }
            let body = comment[i + 1..].iter().find(|l| !l.is_empty());
            if body.is_none_or(|b| index(b.trim()).is_some()) {
                failures.push(format!("{n}: the {name} section holds no text"));
            }
        } else if !name.is_empty()
            && !text.starts_with("  ")
            && !text.starts_with('\t')
            && !name.ends_with('.')
            && (i == 0 || blank_before)
            && (n == comment.len() || blank_after)
        {
            failures.push(format!(
                "{n}: `{name}` is not a section heading that CLAUDE.md names"
            ));
        }
    }
    for name in REQUIRED_SECTIONS {
        if !seen.contains(&name) {
            failures.push(format!("the comment lacks the {name} section"));
        }
    }
    failures
}

/// Returns the failures of the layout of the comment in `source`, each
/// after `path`.
fn file_layout_failures(path: &str, source: &str, marker: &str) -> Vec<String> {
    layout_failures(&comment_lines(source, marker))
        .into_iter()
        .map(|failure| {
            let sep = if failure.starts_with(|c: char| c.is_ascii_digit()) {
                ":"
            } else {
                ": "
            };
            format!("{path}{sep}{failure}")
        })
        .collect()
}

#[test]
fn fixtures() {
    let failures = fixture_failures(&rules(), &read_fixtures(), &mut linter());
    assert!(failures.is_empty(), "{failures:#?}");
}

#[test]
fn tiers() {
    let failures = tier_failures(&rules(), &read_fixtures(), &mut linter());
    assert!(failures.is_empty(), "{failures:#?}");
}

#[test]
fn structure() {
    let known: BTreeSet<String> = rules()
        .iter()
        .map(|r| format!("{}/{}", dir(r.tier), r.name))
        .collect();
    let mut failures = orphan_failures(&read_fixtures(), &known);
    failures.extend(builtin_failures(BUILTINS));
    failures.extend(rule_dir_failures(&Path::new(CRATE_DIR).join("rules")));
    assert!(failures.is_empty(), "{failures:#?}");
}

#[test]
fn weir_tests() {
    let rules: Vec<(String, &str)> = WEIR_RULES
        .iter()
        .map(|&(name, tier, source)| (format!("{name}_{tier}"), source))
        .collect();
    let failures = weir_test_failures(&rules);
    assert!(failures.is_empty(), "{failures:#?}");
}

#[test]
fn entries() {
    let mut lint = linter();
    let mut failures = Vec::new();
    for &(name, tier, source) in WEIR_RULES {
        let rule = Rule {
            name,
            tier,
            source: Source::Weir,
        };
        failures.extend(entry_failures(&rule, source, &mut lint));
    }
    assert!(failures.is_empty(), "{failures:#?}");
}

#[test]
fn layouts() {
    let mut failures = Vec::new();
    for &(name, tier, source) in WEIR_RULES {
        let path = format!("rules/{}/{name}.weir", dir(tier));
        failures.extend(file_layout_failures(&path, source, "#"));
    }
    let checks = Path::new(CRATE_DIR).join("src/checks");
    let mut count = 0;
    for tier in TIERS {
        let tier_dir = checks.join(dir(tier));
        for entry in fs::read_dir(&tier_dir)
            .unwrap_or_else(|e| panic!("reading {}: {e}", tier_dir.display()))
        {
            let path = entry.expect("listing a checks directory").path();
            let source = fs::read_to_string(&path)
                .unwrap_or_else(|e| panic!("reading {}: {e}", path.display()));
            let file = path.file_name().and_then(|n| n.to_str());
            let name = format!(
                "src/checks/{}/{}",
                dir(tier),
                file.expect("module names are UTF-8")
            );
            failures.extend(file_layout_failures(&name, &source, "//!"));
            count += 1;
        }
    }
    assert_eq!(count, CHECKS.len(), "each native check has a module file");
    assert!(failures.is_empty(), "{failures:#?}");
}

#[test]
fn rule_comments() {
    // The comments of the rule files must pass the Required and the
    // Recommended rules. A change to one rule therefore fails the test
    // when the rule reports a phrase in the comment of another rule file.
    let mut sources: Vec<(String, String)> = WEIR_RULES
        .iter()
        .map(|&(name, tier, source)| {
            let path = format!("{CRATE_DIR}/rules/{}/{name}.weir", dir(tier));
            (path, source.to_string())
        })
        .collect();
    let checks = Path::new(CRATE_DIR).join("src/checks");
    for tier in TIERS {
        let tier_dir = checks.join(dir(tier));
        for entry in fs::read_dir(&tier_dir)
            .unwrap_or_else(|e| panic!("reading {}: {e}", tier_dir.display()))
        {
            let path = entry.expect("listing a checks directory").path();
            let source = fs::read_to_string(&path)
                .unwrap_or_else(|e| panic!("reading {}: {e}", path.display()));
            sources.push((path.display().to_string(), source));
        }
    }
    let mut groups: HashMap<Vec<&str>, Rules> = HashMap::new();
    let mut failures = Vec::new();
    for (path, source) in &sources {
        let Input::Source { syntax, skips } = files::classify(Path::new(path)) else {
            panic!("{path} is not a source file");
        };
        let rules = groups
            .entry(skips.clone())
            .or_insert_with(|| Rules::new(&skips).expect("every embedded rule compiles"));
        let findings = crate::lint_source(rules, syntax, &skips, source)
            .unwrap_or_else(|e| panic!("{path}: {e}"));
        failures.extend(
            findings
                .iter()
                .filter(|f| f.tier != Tier::Optional)
                .map(|f| f.display(path).to_string()),
        );
    }
    assert!(failures.is_empty(), "{failures:#?}");
}

mod tests {
    use super::*;

    const RULE: Rule = Rule {
        name: "ProseAndOr",
        tier: Tier::Required,
        source: Source::Weir,
    };

    /// Reports each `and/or` as a finding of [`RULE`].
    fn and_or(text: &str) -> Vec<Finding> {
        text.split('\n')
            .enumerate()
            .filter_map(|(i, line)| {
                let col = line.find("and/or")?;
                Some(Finding {
                    rule: RULE.name.to_string(),
                    tier: RULE.tier,
                    kind: "Style".to_string(),
                    line: i + 1,
                    col: col + 1,
                    message: "`and/or`.".to_string(),
                })
            })
            .collect()
    }

    fn fixtures(files: &[(&str, &str)]) -> Fixtures {
        files
            .iter()
            .map(|&(path, text)| (path.to_string(), text.to_string()))
            .collect()
    }

    #[test]
    fn expected() {
        let lines = [
            "<!-- prettier-ignore-start -->",
            "| Key | Value | <!-- same finding -->",
            "> The proxy reads the route table and records the choice.",
            "<!-- prettier-ignore-end -->",
            "",
            ">",
            "  <!-- prettier-ignore -->  ",
            "The scheduler reads the queue. <!-- prettier-ignore -->",
            "<!-- a note -->",
            "***",
            "> > - - -",
            "    ___",
            "*** <!-- a note -->",
            "**",
            "The scheduler reads the queue. <!-- native check differs -->",
        ];
        let text = lines.join("\n") + "\n";
        assert_eq!(
            expected_lines(&text, false),
            BTreeSet::from([3, 8, 9, 13, 14, 15]),
            "a Weir rule"
        );
        assert_eq!(
            expected_lines(&text, true),
            BTreeSet::from([3, 8, 9, 13, 14]),
            "a native check"
        );
    }

    #[test]
    fn fixture_checks() {
        struct Case {
            name: &'static str,
            files: Vec<(&'static str, &'static str)>,
            want: Vec<&'static str>,
        }
        let common = (COMMON, "The scheduler reads the queue.\n");
        let bad = ("required/ProseAndOr.bad.md", "Pass a and/or b.\n");
        let good = ("required/ProseAndOr.good.md", "Pass a, b, or both.\n");
        let cases = [
            Case {
                name: "a rule that reports each bad line passes",
                files: vec![common, bad, good],
                want: vec![],
            },
            Case {
                name: "a rule with no fixture pair fails",
                files: vec![common, bad],
                want: vec![
                    "Required ProseAndOr: missing the fixture pair \
                     required/ProseAndOr.bad.md and required/ProseAndOr.good.md",
                ],
            },
            Case {
                name: "a bad line that the rule misses fails",
                files: vec![common, (bad.0, "Pass a and/or b.\n\nPass a or b.\n"), good],
                want: vec!["required/ProseAndOr.bad.md:3: no Required ProseAndOr finding"],
            },
            Case {
                name: "a bad line that a nearby finding covers passes",
                files: vec![
                    common,
                    (
                        bad.0,
                        "Pass a and/or b.\nPass a or b. <!-- same finding -->\n",
                    ),
                    good,
                ],
                want: vec![],
            },
            Case {
                name: "a finding on a good fixture fails",
                files: vec![common, bad, (good.0, "Read a and/or b.\n")],
                want: vec![
                    "required/ProseAndOr.good.md:1: an unexpected Required ProseAndOr finding: \
                     `and/or`.",
                ],
            },
            Case {
                name: "a finding on the shared good fixture fails",
                files: vec![(COMMON, "\nRead a and/or b.\n"), bad, good],
                want: vec![
                    "common.good.md:2: an unexpected Required ProseAndOr finding: `and/or`.",
                ],
            },
            Case {
                name: "no shared good fixture fails",
                files: vec![bad, good],
                want: vec!["common.good.md: missing the shared good fixture"],
            },
        ];
        for case in cases {
            let got = fixture_failures(&[RULE], &fixtures(&case.files), &mut and_or);
            assert_eq!(got, case.want, "{}", case.name);
        }
    }

    #[test]
    fn crlf() {
        // A rule that reads the `\r` of a line reports another column.
        let mut lint = |text: &str| {
            let mut findings = and_or(text);
            if text.contains('\r') {
                findings.iter_mut().for_each(|f| f.col += 1);
            }
            findings
        };
        let files = fixtures(&[
            (COMMON, "The scheduler reads the queue.\n"),
            ("required/ProseAndOr.bad.md", "Pass a and/or b.\n"),
            ("required/ProseAndOr.good.md", "Pass a, b, or both.\n"),
        ]);
        assert_eq!(
            fixture_failures(&[RULE], &files, &mut lint),
            [
                "required/ProseAndOr.bad.md: other Required ProseAndOr findings with CRLF line endings"
            ]
        );
    }

    #[test]
    fn tier_checks() {
        struct Case {
            name: &'static str,
            message: &'static str,
            want: Vec<&'static str>,
        }
        let cases = [
            Case {
                name: "another tier with the same message passes",
                message: "`and/or`.",
                want: vec![],
            },
            Case {
                name: "another tier with another message fails",
                message: "Slash.",
                want: vec![
                    "required/ProseAndOr.bad.md:1: the Recommended ProseAndOr also reports this line",
                ],
            },
        ];
        let other = Rule {
            tier: Tier::Recommended,
            ..RULE
        };
        let files = fixtures(&[("required/ProseAndOr.bad.md", "Pass a and/or b.\n")]);
        for case in cases {
            let mut lint = |text: &str| {
                let mut findings = and_or(text);
                findings.extend(and_or(text).into_iter().map(|f| Finding {
                    tier: other.tier,
                    message: case.message.to_string(),
                    ..f
                }));
                findings
            };
            let got = tier_failures(&[RULE, other], &files, &mut lint);
            assert_eq!(got, case.want, "{}", case.name);
        }
    }

    #[test]
    fn orphans() {
        let files = fixtures(&[
            (COMMON, ""),
            ("required/ProseAndOr.bad.md", ""),
            ("required/ProseAndOr.good.md", ""),
            ("recommended/ProseAndOr.bad.md", ""),
        ]);
        let known = BTreeSet::from(["required/ProseAndOr".to_string()]);
        assert_eq!(
            orphan_failures(&files, &known),
            ["recommended/ProseAndOr.bad.md: no ProseAndOr rule under rules/recommended"]
        );
    }

    #[test]
    fn rule_dirs() {
        let root = std::env::temp_dir().join(format!("prose-lint-rules-{}", std::process::id()));
        for dir in ["required", "optional", "extra"] {
            fs::create_dir_all(root.join(dir)).expect("creating a rules directory");
        }
        for (path, text) in [
            ("required/ProseAndOr.weir", ""),
            ("required/builtins.txt", ""),
            ("required/.DS_Store", ""),
            ("optional/ProseHedge.awk", ""),
            ("notes.md", ""),
        ] {
            fs::write(root.join(path), text).expect("writing a rule file");
        }
        let failures = rule_dir_failures(&root);
        fs::remove_dir_all(&root).expect("removing the rules directory");
        assert_eq!(
            failures,
            [
                "rules/extra: not a tier directory",
                "rules/notes.md: not a tier directory",
                "rules/optional/ProseHedge.awk: not a Weir rule or builtins.txt",
            ]
        );
    }

    #[test]
    fn builtins() {
        let builtins = [
            ("ThenThan", Tier::Required),
            ("AllowTo", Tier::Recommended),
            ("ThenThan", Tier::Optional),
        ];
        assert_eq!(
            builtin_failures(&builtins),
            ["rules/optional/builtins.txt: ThenThan is already listed under required"]
        );
    }

    #[test]
    fn weir_failures() {
        let source = "expr main and/or\n\
                      let message \"`and/or`.\"\n\
                      let description \"Flags `and/or`.\"\n\
                      let kind \"Style\"\n\
                      let becomes \"\"\n\
                      allows \"Pass a and/or b.\"\n";
        assert_eq!(
            weir_test_failures(&[("ProseAndOr_Required".to_string(), source)]),
            ["ProseAndOr_Required: `Pass a and/or b.` still draws a finding"]
        );
    }

    #[test]
    fn entries() {
        struct Case {
            name: &'static str,
            source: &'static str,
            want: Vec<&'static str>,
        }
        let cases = [
            Case {
                name: "a word list",
                source: "expr main [and/or, (and or), DET]\n",
                want: vec!["and or"],
            },
            Case {
                name: "a referenced list",
                source: "expr pair [(and or), (or and)]\nexpr main [@pair, (ADJ and)]\n",
                want: vec!["and or", "or and"],
            },
            Case {
                name: "one sequence",
                source: "expr main and or\n",
                want: vec!["and or"],
            },
            Case {
                name: "a list over several lines",
                source: "expr pair [\n    (and or),\n    (or and),\n    but\n]\n\
                         expr main [\n    @pair,\n    (ADJ and)\n]\n",
                want: vec!["and or", "or and", "but"],
            },
            Case {
                name: "a statement after a list over several lines",
                source: "expr pair [\n    (or and)\n]\nexpr main [@pair, (and or)]\n",
                want: vec!["or and", "and or"],
            },
        ];
        for case in cases {
            assert_eq!(plain_entries(case.source), case.want, "{}", case.name);
        }
    }

    #[test]
    fn entry_checks() {
        // The rule fires on each line that holds `and`.
        let mut lint = |text: &str| {
            text.split('\n')
                .enumerate()
                .filter(|(_, line)| line.contains(" and "))
                .map(|(i, _)| Finding {
                    rule: RULE.name.to_string(),
                    tier: RULE.tier,
                    kind: "Style".to_string(),
                    line: i + 1,
                    col: 1,
                    message: String::new(),
                })
                .collect()
        };
        let source = "expr main [and, (and or), but]\n";
        assert_eq!(
            entry_failures(&RULE, source, &mut lint),
            [
                "rules/required/ProseAndOr.weir: entry `but` never fires on its own",
                "rules/required/ProseAndOr.weir: entry `and or` repeats `and`, which already \
                 matches it",
            ]
        );
    }

    const OVERVIEW: &str = "\
# Overview
#
# `and/or` leaves the reader to work out which combinations the
# sentence allows.
";
    const RELATED_RULES: &str = "\
# Related Rules
#
# ProseSlash skips `and/or`, so only this rule reports the phrase.
";
    const MEASUREMENT: &str = "\
# Measurement
#
# The rule hit 61 lines in the measure.py corpus, and in 20 of 20
# sampled hits `and/or` joined alternatives in a sentence.
";

    /// Joins the parts of a comment with one blank comment line between
    /// them, and puts the code of a Weir rule after the comment.
    fn weir_source(parts: &[&str]) -> String {
        parts.join("#\n") + "expr main and/or\n"
    }

    #[test]
    fn layout() {
        struct Case {
            name: &'static str,
            source: String,
            want: Vec<&'static str>,
        }
        let cases = [
            Case {
                name: "the required sections alone pass",
                source: weir_source(&[OVERVIEW, MEASUREMENT]),
                want: vec![],
            },
            Case {
                name: "an optional section between them passes",
                source: weir_source(&[OVERVIEW, RELATED_RULES, MEASUREMENT]),
                want: vec![],
            },
            Case {
                name: "a comment without Measurement fails",
                source: weir_source(&[OVERVIEW, RELATED_RULES]),
                want: vec!["a.weir: the comment lacks the Measurement section"],
            },
            Case {
                name: "a comment without Overview fails",
                source: weir_source(&[RELATED_RULES, MEASUREMENT]),
                want: vec![
                    "a.weir:1: the comment does not open with the Overview heading",
                    "a.weir: the comment lacks the Overview section",
                ],
            },
            Case {
                name: "a file without a comment fails",
                source: "expr main and/or\n".to_string(),
                want: vec!["a.weir: the file does not open with a comment"],
            },
            Case {
                name: "a heading that repeats fails",
                source: weir_source(&[OVERVIEW, MEASUREMENT, MEASUREMENT]),
                want: vec!["a.weir:11: the Measurement heading repeats"],
            },
            Case {
                name: "a heading out of order fails",
                source: weir_source(&[OVERVIEW, MEASUREMENT, RELATED_RULES]),
                want: vec!["a.weir:11: the Related Rules heading must come before Measurement"],
            },
            Case {
                name: "a heading without blank lines around it fails",
                source: weir_source(&[OVERVIEW, &MEASUREMENT.replace("#\n", "")])
                    .replace("#\n# M", "# M"),
                want: vec![
                    "a.weir:5: the Measurement heading needs a blank comment line before it",
                    "a.weir:5: the Measurement heading needs a blank comment line after it",
                ],
            },
            Case {
                name: "a section without text fails",
                source: weir_source(&[OVERVIEW, "# Related Rules\n", MEASUREMENT]),
                want: vec!["a.weir:6: the Related Rules section holds no text"],
            },
            Case {
                name: "a lone line that names no section fails",
                source: weir_source(&[
                    OVERVIEW,
                    "# Notes\n#\n# The rule reads lines.\n",
                    MEASUREMENT,
                ]),
                want: vec!["a.weir:6: `Notes` is not a section heading that CLAUDE.md names"],
            },
            Case {
                name: "a lone line with a period or an indent passes",
                source: weir_source(&[
                    OVERVIEW,
                    "# One sentence.\n",
                    "#   example code\n",
                    MEASUREMENT,
                ]),
                want: vec![],
            },
        ];
        for case in cases {
            let got = file_layout_failures("a.weir", &case.source, "#");
            assert_eq!(got, case.want, "{}", case.name);
        }
    }

    #[test]
    fn native_layout() {
        let source = format!("{}use std::fmt;\n", weir_source(&[OVERVIEW, MEASUREMENT]))
            .replace("expr main and/or\n", "")
            .replace('#', "//!");
        assert_eq!(
            file_layout_failures("a.rs", &source, "//!"),
            Vec::<String>::new(),
            "a doc comment in the layout"
        );
        let source = source.replace("//! Measurement", "//! Measurements");
        assert_eq!(
            file_layout_failures("a.rs", &source, "//!"),
            [
                "a.rs:6: `Measurements` is not a section heading that CLAUDE.md names",
                "a.rs: the comment lacks the Measurement section",
            ],
            "a doc comment with a misspelled heading"
        );
    }
}
