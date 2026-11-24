//! prose-lint-rs lints prose with harper-core. It does not lint any file
//! yet. It loads the curated dictionary and builds an empty lint group.

use std::process::ExitCode;

use harper_core::linting::LintGroup;
use harper_core::spell::FstDictionary;

fn main() -> ExitCode {
    if std::env::args().nth(1).as_deref() == Some("--version") {
        println!("{}", harper_core::core_version());
        return ExitCode::SUCCESS;
    }
    let _dict = FstDictionary::curated();
    let _group = LintGroup::empty();
    ExitCode::SUCCESS
}
