//! prose-lint-rs lints prose with the Weir rules and the Harper
//! built-ins of `configs/harper/rules`, and with the native checks of
//! `src/checks`. It lints markdown files, commit messages, and the
//! comments of source files.
//!
//! ```text
//! prose-lint-rs <file>     lint a markdown file or a source file
//! prose-lint-rs --commit   lint a commit message read from stdin
//! prose-lint-rs --version  print the harper-core version
//! ```
//!
//! Each finding prints as `path:line:col: [Tier] Kind::Rule: message`,
//! Required findings first and then by line. A commit message prints
//! as the path `<stdin>`. The binary exits 1 when it prints a finding,
//! 2 when it fails, and 0 otherwise. It also exits 0
//! for a missing file, for a file whose extension it does not know, and
//! for a file under a skipped directory.

mod checks;
mod comments;
mod files;
mod guards;
mod markdown;
mod region;
#[cfg(test)]
mod rule_tests;
mod rules;

use std::error::Error as _;
use std::io::{self, Read};
use std::path::{Path, PathBuf};
use std::process::ExitCode;

use harper_core::parsers::Markdown;

use crate::comments::{Comments, RegionParser, Syntax};
use crate::files::Input;
use crate::region::{Finding, Region};
use crate::rules::{Rules, TIME_SENSITIVE};

/// An error that ends the run with exit status 2.
#[derive(Debug, thiserror::Error)]
enum Error {
    #[error("usage: prose-lint-rs <file> | prose-lint-rs --commit | prose-lint-rs --version")]
    Usage,
    #[error("reading {}", .path.display())]
    Read {
        path: PathBuf,
        #[source]
        source: io::Error,
    },
    #[error("reading the commit message from stdin")]
    Stdin(#[source] io::Error),
    #[error("finding the comments of {}", .path.display())]
    Comments {
        path: PathBuf,
        #[source]
        source: comments::Error,
    },
    #[error("building the rules")]
    Rules(#[from] rules::Error),
}

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match run(&args) {
        Ok(false) => ExitCode::SUCCESS,
        Ok(true) => ExitCode::from(1),
        Err(err) => {
            let mut message = format!("prose-lint-rs: {err}");
            let mut source = err.source();
            while let Some(cause) = source {
                message.push_str(&format!(": {cause}"));
                source = cause.source();
            }
            eprintln!("{message}");
            ExitCode::from(2)
        }
    }
}

/// Runs the command of `args` and returns whether it printed a
/// finding.
fn run(args: &[String]) -> Result<bool, Error> {
    let (name, text, skips, syntax) = match args {
        [flag] if flag == "--version" => {
            println!("{}", harper_core::core_version());
            return Ok(false);
        }
        [flag] if flag == "--commit" => {
            let mut text = String::new();
            io::stdin()
                .read_to_string(&mut text)
                .map_err(Error::Stdin)?;
            ("<stdin>".to_string(), text, TIME_SENSITIVE.to_vec(), None)
        }
        [file] if !file.starts_with("--") => {
            let path = Path::new(file);
            let (skips, syntax) = match files::classify(path) {
                _ if !path.is_file() => return Ok(false),
                Input::Skip => return Ok(false),
                Input::Source { syntax, skips } => (skips, Some(syntax)),
                Input::Markdown { skips } => (skips, None),
            };
            let text = std::fs::read_to_string(path).map_err(|source| Error::Read {
                path: path.to_path_buf(),
                source,
            })?;
            (file.clone(), text, skips, syntax)
        }
        _ => return Err(Error::Usage),
    };

    let mut rules = Rules::new(&skips)?;
    let findings = match syntax {
        Some(syntax) => {
            lint_source(&mut rules, syntax, &skips, &text).map_err(|source| Error::Comments {
                path: PathBuf::from(&name),
                source,
            })?
        }
        None => lint_markdown(&mut rules, &skips, &text),
    };
    for finding in &findings {
        println!("{}", finding.display(&name));
    }
    Ok(!findings.is_empty())
}

/// Lints the markdown text of a file or a commit message with the rules
/// and with each native check that `skips` does not name. Returns the
/// findings in the order of the output.
fn lint_markdown(rules: &mut Rules, skips: &[&str], text: &str) -> Vec<Finding> {
    region::order(findings(rules, skips, text))
}

/// Returns the findings of [`lint_markdown`] before [`region::order`]
/// drops each finding that the same finding of a higher tier hides.
fn findings(rules: &mut Rules, skips: &[&str], text: &str) -> Vec<Finding> {
    let region = Region::markdown(&guards::straighten(text));
    let mut findings = harper(rules, &region, &Markdown::default());
    // The native checks read the text as the file holds it, so
    // ProseTypography reports a curly apostrophe that the Weir rules
    // read as a straight one.
    findings.extend(checks::run(&Region::markdown(text), skips));
    guards::drop_label_colons(text, &mut findings);
    guards::drop_transcript_opinions(text, &mut findings);
    guards::drop_vague_conditions(&mut findings);
    findings
}

/// Lints the comments of a source file of `syntax` with the rules and
/// with each native check that reads comments, other than the rules
/// that `skips` names. Returns the findings in the order of the output.
/// The binary does not report a finding on the license header.
fn lint_source(
    rules: &mut Rules,
    syntax: &Syntax,
    skips: &[&str],
    text: &str,
) -> Result<Vec<Finding>, comments::Error> {
    let comments = Comments::new(syntax, text)?;
    let file = Region::markdown(&guards::straighten(text));
    let mut findings = harper(rules, &file, &RegionParser::new(&comments.regions));
    findings.extend(checks::run_source(
        &Region::markdown(text),
        &comments.regions,
        syntax.kind,
        skips,
    ));
    if let Some(header) = &comments.license {
        findings.retain(|f| !header.contains(&f.line));
    }
    guards::drop_region_label_colons(&comments.regions, &mut findings);
    guards::drop_vague_conditions(&mut findings);
    Ok(region::order(findings))
}

/// Returns the findings of the rules on the text of `file`, which
/// `parser` reads.
fn harper(
    rules: &mut Rules,
    file: &Region,
    parser: &impl harper_core::parsers::Parser,
) -> Vec<Finding> {
    rules
        .lint(&file.text, parser)
        .into_iter()
        .map(|(rule, tier, lint)| {
            let at = file.position(lint.span.start);
            Finding {
                rule: rule.to_string(),
                tier,
                kind: lint.lint_kind.to_string(),
                line: at.line,
                col: at.col,
                message: lint.message,
            }
        })
        .collect()
}
