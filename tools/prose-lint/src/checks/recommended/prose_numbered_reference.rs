//! Overview
//!
//! Reports a reference to a numbered line of a file in markdown prose and
//! source comments. It reports a file name with a line number after a
//! colon, the word `line` or `lines` before a number, and a GitHub link
//! to a line on a branch (`blob/main/cli.go#L264`). The numbers move when
//! someone edits the file, and nothing points the editor at the
//! reference. The reader then lands on the wrong line. The fix names the
//! function or the section, or links a commit permalink, whose lines stay
//! put. The rule sits at Recommended, because a line number also appears
//! where no edit moves it, such as in a backtrace.
//!
//! Expression
//!
//! Most of these references sit inside a code span or a link
//! destination, which the model blanks in the prose of a line. The check
//! therefore reads the source of each line that holds prose. It skips
//! the opening line of a MyST directive and the option lines under it,
//! because the model blanks their text. The check reads the code spans
//! of this comment too, so the examples here describe the forms instead
//! of spelling them out.
//!
//! A file reference needs an extension from a list of source and config
//! formats. Commit messages and changelogs skip the check, because a
//! commit pins the lines it names.
//!
//! False Positives
//!
//! A host and port passes (`example.com:443`), because a file reference
//! needs an extension from the list. A link to a commit hash of six or
//! more hex digits or to a version tag (`v1.2.3`) passes too. `step N`
//! stays out, because the skills cite their own steps on purpose.
//!
//! Measurement
//!
//! The rule hit 12 lines in the measure.py corpus, and 10 of 12 named a
//! line of a file that can change. The misfires numbered the lines of a
//! backtrace and the lines that a Hugo option highlights.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::markdown::{Line, Markdown};

fn re(pattern: &str) -> Regex {
    Regex::new(pattern).expect("the pattern is valid")
}

static FILE_LINE: LazyLock<Regex> = LazyLock::new(|| {
    re(
        r"[A-Za-z0-9][A-Za-z0-9_./-]*\.(?:go|py|pyi|rs|ts|tsx|js|jsx|mjs|cjs|nix|c|h|cc|cpp|hpp|java|kt|rb|sh|bash|fish|md|yaml|yml|json|toml|awk|weir|lua|swift|zig|ex|exs|hs|cs|php|scala|sql|tf|proto|vue|css|html):[0-9]+(?:-[0-9]+)?",
    )
});
static LINE_REF: LazyLock<Regex> =
    LazyLock::new(|| re(r"(?:^|[^A-Za-z])([Ll]ines?[ \t]+[0-9]+(?:-[0-9]+)?)"));
static ANCHOR: LazyLock<Regex> =
    LazyLock::new(|| re(r"/(?:blob|tree)/([^/ \t]+)/[^ \t)]*#L[0-9]+"));
static HASH: LazyLock<Regex> = LazyLock::new(|| re(r"^[0-9a-f]{6,}$"));
static VERSION: LazyLock<Regex> = LazyLock::new(|| re(r"^v?[0-9]+\."));

fn hit(line: &Line, start: usize, what: &str) -> Hit {
    Hit {
        at: line.at(&line.source, start),
        message: format!(
            "Numbered reference `{what}`. Name the function or the section, or link a commit permalink."
        ),
    }
}

/// Returns a finding for the first numbered reference on each line.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .lines
        .iter()
        .filter(|line| line.prose && !line.directive)
        .filter_map(|line| {
            let source = line.source.as_str();
            if let Some(m) = FILE_LINE.find(source) {
                return Some(hit(line, m.start(), m.as_str()));
            }
            if let Some(c) = ANCHOR.captures(source) {
                let link = c.get(0).expect("a match");
                // The first `#L` of the link names the line.
                let at = link.start() + link.as_str().find("#L").expect("the pattern holds `#L`");
                if !HASH.is_match(&c[1]) && !VERSION.is_match(&c[1]) {
                    return Some(hit(line, at, &source[at..link.end()]));
                }
            }
            let m = LINE_REF.captures(source)?.get(1)?;
            Some(hit(line, m.start(), m.as_str()))
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::region::Region;

    #[test]
    fn references() {
        struct Case {
            name: &'static str,
            text: &'static str,
            /// The line and the column of each finding.
            want: Vec<(usize, usize)>,
        }
        let cases = [
            Case {
                name: "file and line in a code span",
                text: "The bug sits in `main.go:12` today.\n",
                want: vec![(1, 18)],
            },
            Case {
                name: "directive options",
                text: "The pod runs.\n\n:::{figure} ./img.png\n:name: fig-main.go:12\n:alt: see [here](x) for line 5\n:::\n\nThe pod stops.\n",
                want: vec![],
            },
        ];
        for case in cases {
            let region = Region::markdown(case.text);
            let got: Vec<_> = check(&Markdown::new(&region))
                .into_iter()
                .map(|hit| {
                    let at = region.position(hit.at);
                    (at.line, at.col)
                })
                .collect();
            assert_eq!(got, case.want, "{}", case.name);
        }
    }
}
