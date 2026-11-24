//! prose-lint-rs lints prose with the Weir rules and the Harper
//! built-ins of `configs/harper/rules`, and with the native checks of
//! `src/checks`.
//!
//! ```text
//! prose-lint-rs <file>     lint a markdown file
//! prose-lint-rs --commit   lint a commit message read from stdin
//! prose-lint-rs --version  print the harper-core version
//! ```
//!
//! Each finding prints as `path:line:col: [Tier] Kind::Rule: message`,
//! Required findings first and then by line. A commit message prints
//! as the path `<stdin>`. The binary exits 1 when it prints a finding,
//! 2 when it fails, and 0 otherwise. It also exits 0
//! for a missing file, for a file whose extension it does not know, and
//! for a file under a skipped directory. It does not lint the comments
//! of a source file yet, so it exits 2 for one.

mod checks;
mod files;
mod guards;
mod markdown;
mod region;
mod rules;

use std::error::Error as _;
use std::io::{self, Read};
use std::path::{Path, PathBuf};
use std::process::ExitCode;

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
    #[error("{}: linting the comments of a source file is not supported yet", .0.display())]
    Source(PathBuf),
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
    let (name, text, skips) = match args {
        [flag] if flag == "--version" => {
            println!("{}", harper_core::core_version());
            return Ok(false);
        }
        [flag] if flag == "--commit" => {
            let mut text = String::new();
            io::stdin()
                .read_to_string(&mut text)
                .map_err(Error::Stdin)?;
            ("<stdin>".to_string(), text, TIME_SENSITIVE.to_vec())
        }
        [file] if !file.starts_with("--") => {
            let path = Path::new(file);
            let skips = match files::classify(path) {
                _ if !path.is_file() => return Ok(false),
                Input::Skip => return Ok(false),
                Input::Source => return Err(Error::Source(path.to_path_buf())),
                Input::Markdown { skips } => skips,
            };
            let text = std::fs::read_to_string(path).map_err(|source| Error::Read {
                path: path.to_path_buf(),
                source,
            })?;
            (file.clone(), text, skips)
        }
        _ => return Err(Error::Usage),
    };

    let mut rules = Rules::new(&skips)?;
    let findings = lint_markdown(&mut rules, &skips, &text);
    for finding in &findings {
        println!("{}", finding.display(&name));
    }
    Ok(!findings.is_empty())
}

/// Lints the markdown text of a file or a commit message with the rules
/// and with each native check that `skips` does not name. Returns the
/// findings in the order of the output.
fn lint_markdown(rules: &mut Rules, skips: &[&str], text: &str) -> Vec<Finding> {
    let region = Region::markdown(&guards::straighten(text));
    let mut findings: Vec<Finding> = rules
        .lint(&region.text)
        .into_iter()
        .map(|(rule, tier, lint)| {
            let at = region.position(lint.span.start);
            Finding {
                rule: rule.to_string(),
                tier,
                kind: lint.lint_kind.to_string(),
                line: at.line,
                col: at.col,
                message: lint.message,
            }
        })
        .collect();
    findings.extend(checks::run(&region, skips));
    guards::drop_label_colons(text, &mut findings);
    guards::drop_transcript_opinions(text, &mut findings);
    region::order(findings)
}
