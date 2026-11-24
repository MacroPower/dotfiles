//! The rules that pick how the binary lints a path.

use std::path::{Component, Path};

use crate::comments::{self, Syntax};
use crate::rules::TIME_SENSITIVE;

/// How the binary lints one path.
#[derive(Debug, PartialEq, Eq)]
pub enum Input {
    /// The binary does not lint the path.
    Skip,
    /// The path is a markdown file. The binary lints it without the
    /// rules that `skips` names.
    Markdown {
        /// The rules that the file skips.
        skips: Vec<&'static str>,
    },
    /// The path is a source file of `syntax`. The binary lints its
    /// comments without the rules that `skips` names.
    Source {
        /// The comment syntax of the file.
        syntax: &'static Syntax,
        /// The rules that the file skips.
        skips: Vec<&'static str>,
    },
}

/// The directories whose files the binary skips, because other
/// projects own them.
const SKIPPED_DIRS: &[&str] = &["node_modules", "vendor", ".git", ".terraform"];

/// The paths of prompt files for an agent, with a `/` in front. Prompt
/// files write capitals for emphasis on purpose, so ProseCapsEmphasis
/// skips them. The patterns name the directories of `.claude/` that hold
/// prompt files, and not all of `.claude/`, because the path of every
/// file of a worktree under `.claude/worktrees/` holds `.claude/`.
const PROMPT_DIRS: &[&str] = &[
    "/configs/claude/",
    "/.claude/agents/",
    "/.claude/commands/",
    "/.claude/output-styles/",
    "/.claude/rules/",
    "/.claude/skills/",
];

/// The names of prompt files for an agent.
const PROMPT_FILES: &[&str] = &["CLAUDE.md", "AGENTS.md", "SKILL.md"];

/// The extensions of markdown files. `package.nix` reads this table and
/// [`comments::SYNTAXES`] from the source, for the path glob of the
/// prose hook.
const MARKDOWN_EXTENSIONS: &[&str] = &["md", "markdown"];

/// Returns how the binary lints `path`. It does not read the file.
///
/// The binary skips each path under [`SKIPPED_DIRS`] and each path
/// under `harper/fixtures`, whose files break the rules on purpose. It
/// also skips each path whose extension sits neither in
/// [`MARKDOWN_EXTENSIONS`] nor in [`comments::SYNTAXES`]. A changelog
/// skips the rules of [`TIME_SENSITIVE`], because it describes changes.
/// It also skips ProseHeading, because its headings are release labels,
/// such as `[1.2.0] - 2026-09-23 [YANKED]`.
pub fn classify(path: &Path) -> Input {
    let dirs: Vec<_> = path
        .components()
        .filter_map(|c| match c {
            Component::Normal(name) => name.to_str(),
            _ => None,
        })
        .collect();
    let dirs = &dirs[..dirs.len().saturating_sub(1)];
    if dirs.iter().any(|d| SKIPPED_DIRS.contains(d))
        || dirs.windows(2).any(|w| w == ["harper", "fixtures"])
    {
        return Input::Skip;
    }
    let ext = path.extension().and_then(|e| e.to_str()).unwrap_or("");
    let syntax = comments::syntax(ext);
    if syntax.is_none() && !MARKDOWN_EXTENSIONS.contains(&ext) {
        return Input::Skip;
    }

    let base = path.file_name().and_then(|n| n.to_str()).unwrap_or("");
    let mut skips = Vec::new();
    if ["CHANGELOG", "CHANGES", "HISTORY"]
        .iter()
        .any(|p| base.starts_with(p))
    {
        skips.extend(TIME_SENSITIVE);
        skips.push("ProseHeading");
    }
    let slashed = format!("/{}", path.to_string_lossy());
    if PROMPT_FILES.contains(&base) || PROMPT_DIRS.iter().any(|d| slashed.contains(d)) {
        skips.push("ProseCapsEmphasis");
    }
    // A Weir rule holds each character that it reports, so
    // ProseTypography skips the rule files.
    if ext == "weir" {
        skips.push("ProseTypography");
    }
    match syntax {
        Some(syntax) => Input::Source { syntax, skips },
        None => Input::Markdown { skips },
    }
}

#[cfg(test)]
mod tests {
    use regex::Regex;

    use super::*;

    /// Returns each entry of each list that the first capture group of
    /// `pattern` finds in `source`, as `package.nix` reads them.
    fn table_entries(source: &'static str, pattern: &str) -> Vec<&'static str> {
        Regex::new(pattern)
            .expect("the pattern is valid")
            .captures_iter(source)
            .flat_map(|c| {
                c.get(1)
                    .expect("the pattern has a group")
                    .as_str()
                    .split(',')
            })
            .map(|e| e.trim().trim_matches('"'))
            .filter(|e| !e.is_empty())
            .collect()
    }

    #[test]
    fn extension_tables() {
        // package.nix reads the extensions of the prose hook from the
        // source of both tables. Its patterns match the same text as
        // these, so a change to a table that hides an extension from them
        // fails the test.
        let markdown = table_entries(
            include_str!("files.rs"),
            r"MARKDOWN_EXTENSIONS: &\[&str\] = &\[([^\]]*)\]",
        );
        assert_eq!(markdown, MARKDOWN_EXTENSIONS);
        let sources = table_entries(
            include_str!("comments.rs"),
            r"Syntax::(?:grammar|scan)\(&\[([^\]]*)\]",
        );
        let want: Vec<&str> = comments::SYNTAXES
            .iter()
            .flat_map(|s| s.extensions.iter().copied())
            .collect();
        assert_eq!(sources, want);
    }

    #[test]
    fn classify_paths() {
        struct Case {
            name: &'static str,
            path: &'static str,
            want: Input,
        }
        let changelog = || {
            let mut skips = TIME_SENSITIVE.to_vec();
            skips.push("ProseHeading");
            Input::Markdown { skips }
        };
        let plain = || Input::Markdown { skips: vec![] };
        let prompt = || Input::Markdown {
            skips: vec!["ProseCapsEmphasis"],
        };
        let cases = [
            Case {
                name: "markdown",
                path: "docs/guide.md",
                want: plain(),
            },
            Case {
                name: "markdown extension",
                path: "guide.markdown",
                want: plain(),
            },
            Case {
                name: "source",
                path: "tools/run.py",
                want: Input::Source {
                    syntax: comments::syntax("py").expect("the table lists py"),
                    skips: vec![],
                },
            },
            Case {
                name: "rule file",
                path: "configs/harper/rules/required/ProseAndOr.weir",
                want: Input::Source {
                    syntax: comments::syntax("weir").expect("the table lists weir"),
                    skips: vec!["ProseTypography"],
                },
            },
            Case {
                name: "source in a prompt directory",
                path: "configs/claude/skills/demo/run.sh",
                want: Input::Source {
                    syntax: comments::syntax("sh").expect("the table lists sh"),
                    skips: vec!["ProseCapsEmphasis"],
                },
            },
            Case {
                name: "awk script",
                path: "scripts/report.awk",
                want: Input::Source {
                    syntax: comments::syntax("awk").expect("the table lists awk"),
                    skips: vec![],
                },
            },
            Case {
                name: "unknown extension",
                path: "notes.txt",
                want: Input::Skip,
            },
            Case {
                name: "no extension",
                path: "Makefile",
                want: Input::Skip,
            },
            Case {
                name: "node_modules",
                path: "web/node_modules/pkg/README.md",
                want: Input::Skip,
            },
            Case {
                name: "relative vendor",
                path: "vendor/pkg/README.md",
                want: Input::Skip,
            },
            Case {
                name: "git",
                path: "/repo/.git/COMMIT_EDITMSG.md",
                want: Input::Skip,
            },
            Case {
                name: "terraform",
                path: "/repo/.terraform/modules/README.md",
                want: Input::Skip,
            },
            Case {
                name: "fixtures",
                path: "/repo/configs/harper/fixtures/required/ProseAndOr.bad.md",
                want: Input::Skip,
            },
            Case {
                name: "fixtures without harper",
                path: "/repo/tests/fixtures/a.md",
                want: plain(),
            },
            Case {
                name: "file named vendor",
                path: "/repo/docs/vendor.md",
                want: plain(),
            },
            Case {
                name: "changelog",
                path: "CHANGELOG.md",
                want: changelog(),
            },
            Case {
                name: "changes",
                path: "/repo/CHANGES.md",
                want: changelog(),
            },
            Case {
                name: "history",
                path: "docs/HISTORY.markdown",
                want: changelog(),
            },
        ];
        for case in cases {
            assert_eq!(classify(Path::new(case.path)), case.want, "{}", case.name);
        }

        // Each prompt file skips ProseCapsEmphasis under its relative
        // path and under an absolute one, and each other file keeps it.
        let prompts = [
            "CLAUDE.md",
            "docs/AGENTS.md",
            "docs/demo/SKILL.md",
            "configs/claude/agents/demo.md",
            ".claude/agents/demo.md",
            ".claude/commands/demo.md",
            ".claude/output-styles/demo.md",
            ".claude/rules/demo.md",
            ".claude/skills/demo/scripts/run.md",
            ".claude/worktrees/demo/configs/claude/agents/demo.md",
            ".claude/worktrees/demo/.claude/agents/demo.md",
        ];
        let others = [
            "docs/guide.md",
            "tools/run.md",
            ".claude/worktrees/demo/docs/guide.md",
            ".claude/worktrees/demo/tools/run.md",
            ".claude/plans/demo.md",
        ];
        for f in prompts {
            for path in [f.to_string(), format!("/repo/{f}")] {
                assert_eq!(classify(Path::new(&path)), prompt(), "{path}");
            }
        }
        for f in others {
            for path in [f.to_string(), format!("/repo/{f}")] {
                assert_eq!(classify(Path::new(&path)), plain(), "{path}");
            }
        }
    }
}
