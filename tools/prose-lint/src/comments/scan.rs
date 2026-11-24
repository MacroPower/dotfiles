//! Finds the full-line `#` comments of the languages that have no
//! grammar in the binary.
//!
//! Each line whose first character other than a blank is `#` is a
//! comment, and so is a line that opens with `//` in HCL. The scanner
//! does not read a comment that follows code, because it cannot tell
//! such a `#` from a `#` in a string. It also skips the lines that hold the code of
//! another program:
//!
//! - In Python, a line inside a string in triple quotes. Each line with
//!   an odd count of `"""`, or of `'''`, flips whether the scanner sits
//!   inside such a string.
//! - In HCL, the body of a heredoc (`<<-EOF` to `EOF`), because a
//!   `user_data` script holds `#` lines of its own. Block comments stay
//!   out, because most of them hold code.
//! - In YAML, the body of a block scalar (`run: |`), which often holds a
//!   shell script. The body runs while lines are blank or indented past
//!   the line that opens it.
//!
//! HCL also lints each `description` and `error_message` string. A
//! reader sees a description in docs and in plan output, and an error
//! message when an input fails a validation. The text of a quoted
//! string or of a heredoc is a region without markers.
//!
//! Python also lints each docstring. A docstring is a string in triple
//! quotes that opens its line and is the first statement of the module,
//! a class, or a function. So no code comes before it in the file, or
//! the last line of code above it ends in `:`, as a `def` line does. A
//! string under other code, such as an argument of a call, holds code
//! or data, so the scanner skips it. The lines after the first line of
//! a docstring lose the indent that all of them share, as PEP 257 trims
//! a docstring.

use std::ops::Range;
use std::sync::LazyLock;

use regex::Regex;

use super::{Found, lines, skip_blanks};

/// The language whose comments the scanner reads.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Dialect {
    /// A language whose only marker is `#`, such as awk or Weir.
    Hash,
    /// Python.
    Python,
    /// HCL, such as Terraform.
    Hcl,
    /// YAML.
    Yaml,
}

static HEREDOC: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"<<-?[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]*$").expect("the pattern is valid")
});
static DESCRIPTION: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^[ \t]*(?:description|error_message)[ \t]*=[ \t]*").expect("the pattern is valid")
});
static HEADER: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r":[ \t]*(?:#.*)?$").expect("the pattern is valid"));
static BLOCK_SCALAR: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"[:-][ \t]*[|>][-+0-9]*[ \t]*(?:#.*)?$").expect("the pattern is valid")
});

/// An open heredoc of HCL or string of Python: its terminator, and the
/// lines of its text when it holds a description or a docstring.
type Open<T> = (T, Option<Vec<Range<usize>>>);

/// Returns each comment of `chars`, a file of `dialect`, and each HCL
/// description, in the order of the source.
pub(super) fn find(dialect: Dialect, chars: &[char]) -> Vec<Found> {
    let mut found = Vec::new();
    let mut quote: Option<Open<&str>> = None;
    let mut scalar: Option<usize> = None;
    let mut heredoc: Option<Open<String>> = None;
    // Whether the next line of code can open a Python docstring. It can
    // at the top of the file and after a line that ends a block header.
    let mut doc_next = true;
    for line in lines(chars, 0..chars.len()) {
        let text: String = chars[line.clone()].iter().collect();
        let lead = skip_blanks(chars, line.clone());
        let indent = lead - line.start;
        let blank = lead == line.end;

        if let Some((q, doc)) = &mut quote {
            let close = text
                .find(*q)
                .map(|i| line.start + text[..i].chars().count());
            if let Some(lines) = doc {
                lines.push(line.start..close.unwrap_or(line.end));
            }
            if text.matches(*q).count() % 2 == 1 {
                if let Some(lines) = doc.take() {
                    found.push(Found::Text(docstring(chars, lines)));
                }
                quote = None;
                doc_next = HEADER.is_match(&text);
            }
            continue;
        }
        if let Some(n) = scalar {
            if blank || indent > n {
                continue;
            }
            scalar = None;
        }
        if let Some((word, body)) = &mut heredoc {
            if text.trim() == word {
                if let Some(body) = body.take() {
                    found.push(Found::Text(dedent(chars, body)));
                }
                heredoc = None;
            } else if let Some(body) = body {
                body.push(line);
            }
            continue;
        }

        let marked = chars[lead..line.end].starts_with(&['#'])
            || (dialect == Dialect::Hcl && chars[lead..line.end].starts_with(&['/', '/']));
        if marked {
            found.push(Found::Comment(lead..line.end));
            continue;
        }
        match dialect {
            Dialect::Hash => {}
            Dialect::Python if blank => {}
            Dialect::Python => {
                let rest = text.trim_start();
                let prefix = rest.find(['"', '\'']).unwrap_or(0);
                let opener = [r#"""""#, "'''"]
                    .into_iter()
                    .filter(|_| {
                        prefix <= 2 && rest[..prefix].chars().all(|c| c.is_ascii_alphabetic())
                    })
                    .find(|q| rest[prefix..].starts_with(q));
                if let Some(q) = opener {
                    let start = lead + prefix + 3;
                    let after: String = chars[start..line.end].iter().collect();
                    let end = after.find(q).map(|i| start + after[..i].chars().count());
                    let first = start..end.unwrap_or(line.end);
                    match (end, doc_next) {
                        (Some(_), true) => found.push(Found::Text(vec![first])),
                        (Some(_), false) => {}
                        (None, doc) => quote = Some((q, doc.then(|| vec![first]))),
                    }
                } else {
                    quote = [r#"""""#, "'''"]
                        .into_iter()
                        .find(|q| text.matches(q).count() % 2 == 1)
                        .map(|q| (q, None));
                }
                doc_next = HEADER.is_match(&text);
            }
            Dialect::Yaml => {
                if BLOCK_SCALAR.is_match(&text) {
                    scalar = Some(indent);
                }
            }
            Dialect::Hcl => {
                let described = DESCRIPTION.find(&text).map(|m| m.end());
                if let Some(end) = described
                    && let Some(range) = quoted(chars, line.start + text[..end].chars().count())
                {
                    found.push(Found::Text(vec![range]));
                } else if let Some(word) = HEREDOC.captures(&text).and_then(|c| c.get(1)) {
                    let body = described.map(|_| Vec::new());
                    heredoc = Some((word.as_str().to_string(), body));
                }
            }
        }
    }
    found
}

/// Returns the text of the quoted string that opens at `at`, without
/// its quotes, when the string ends on its line.
fn quoted(chars: &[char], at: usize) -> Option<Range<usize>> {
    if chars.get(at) != Some(&'"') {
        return None;
    }
    let mut i = at + 1;
    while let Some(&c) = chars.get(i) {
        match c {
            '\\' => i += 2,
            '"' => return Some(at + 1..i),
            '\n' => return None,
            _ => i += 1,
        }
    }
    None
}

/// Returns the lines of a docstring. The lines after the first lose the
/// indent that all of them share.
fn docstring(chars: &[char], mut lines: Vec<Range<usize>>) -> Vec<Range<usize>> {
    let rest = lines.split_off(1.min(lines.len()));
    lines.extend(dedent(chars, rest));
    lines
}

/// Returns `lines` without the indent that all of their lines with
/// text share.
fn dedent(chars: &[char], lines: Vec<Range<usize>>) -> Vec<Range<usize>> {
    let indent = lines
        .iter()
        .map(|l| skip_blanks(chars, l.clone()) - l.start)
        .zip(&lines)
        .filter(|(n, l)| l.start + n < l.end)
        .map(|(n, _)| n)
        .min()
        .unwrap_or(0);
    lines
        .into_iter()
        .map(|l| (l.start + indent).min(l.end)..l.end)
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Returns the text of each line that `find` returns, with `#` for
    /// each comment and `>` for each line of a description.
    fn texts(dialect: Dialect, source: &str) -> Vec<String> {
        let chars: Vec<char> = source.chars().collect();
        let text = |r: &Range<usize>| chars[r.clone()].iter().collect::<String>();
        find(dialect, &chars)
            .iter()
            .flat_map(|f| match f {
                Found::Comment(r) => vec![format!("# {}", text(r))],
                Found::Text(lines) => lines.iter().map(|l| format!("> {}", text(l))).collect(),
            })
            .collect()
    }

    #[test]
    fn dialects() {
        struct Case {
            name: &'static str,
            dialect: Dialect,
            source: &'static str,
            want: Vec<&'static str>,
        }
        let cases = [
            Case {
                name: "hash",
                dialect: Dialect::Hash,
                source: "# One.\nx = 1 # Two.\n  # Three.\n",
                want: vec!["# # One.", "# # Three."],
            },
            Case {
                name: "python string",
                dialect: Dialect::Python,
                source: "s = \"\"\"\n# In the string.\n\"\"\"\n# After.\n",
                want: vec!["# # After."],
            },
            Case {
                name: "python docstrings",
                dialect: Dialect::Python,
                source: "\"\"\"Module.\"\"\"\n\n\ndef f():\n    \"\"\"One.\n\n    Two\n      three.\n    \"\"\"\n    x = '''\n    # In the string.\n    '''\n\n\nclass A:  # A comment.\n    # Another.\n\n    r'''Four.'''\n",
                want: vec![
                    "> Module.",
                    "> One.",
                    "> ",
                    "> Two",
                    ">   three.",
                    "> ",
                    "# # Another.",
                    "> Four.",
                ],
            },
            Case {
                name: "python strings that are not docstrings",
                dialect: Dialect::Python,
                source: "\"\"\"Module.\"\"\"\n\"\"\"Two.\"\"\"\nrun(\n    \"\"\"\n    # In the string.\n    \"\"\"\n)\nx = 1\n'''\nIn a string.\n'''\n# After.\n",
                want: vec!["> Module.", "# # After."],
            },
            Case {
                name: "python string after a letter that is not ascii",
                dialect: Dialect::Python,
                source: "\u{e9}\"\"\"\n\"\"\"\n# After.\n",
                want: vec!["# # After."],
            },
            Case {
                name: "python string on one line",
                dialect: Dialect::Python,
                source: "s = \"\"\"a\"\"\"\n# After.\n",
                want: vec!["# # After."],
            },
            Case {
                name: "yaml block scalar",
                dialect: Dialect::Yaml,
                source: "a:\n  run: |\n    # In the script.\n\n    cp a b\n  # After.\n",
                want: vec!["# # After."],
            },
            Case {
                name: "hcl heredoc",
                dialect: Dialect::Hcl,
                source: "x = <<-EOF\n  # In the script.\n  EOF\n// After.\n",
                want: vec!["# // After."],
            },
            Case {
                name: "hcl descriptions",
                dialect: Dialect::Hcl,
                source: "description = \"The \\\"job\\\" runs.\"\nerror_message = <<-EOT\n    Set it.\n      More.\n  EOT\n",
                want: vec!["> The \\\"job\\\" runs.", "> Set it.", ">   More."],
            },
        ];
        for case in cases {
            assert_eq!(texts(case.dialect, case.source), case.want, "{}", case.name);
        }
    }
}
