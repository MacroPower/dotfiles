//! Tests of the prose-lint-rs binary.

use std::fs;
use std::io::Write as _;
use std::path::{Path, PathBuf};
use std::process::{Command, Output, Stdio};

const BIN: &str = env!("CARGO_BIN_EXE_prose-lint-rs");

/// The directory that holds `rules/` and `fixtures/`, from build.rs.
const HARPER: &str = env!("PROSE_LINT_HARPER");

/// A directory under the temporary directory that the test removes
/// when it ends.
struct TempDir(PathBuf);

impl TempDir {
    fn new(name: &str) -> Self {
        let dir = std::env::temp_dir().join(format!("prose-lint-rs-{}-{name}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).expect("creating the temporary directory");
        Self(dir)
    }

    /// Writes `text` to `name` under the directory and returns its path.
    fn write(&self, name: &str, text: impl AsRef<[u8]>) -> PathBuf {
        let path = self.0.join(name);
        fs::create_dir_all(path.parent().expect("the path is under the directory"))
            .expect("creating the parent directory");
        fs::write(&path, text).expect("writing the file");
        path
    }
}

impl Drop for TempDir {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn lint(path: &Path) -> Output {
    Command::new(BIN)
        .arg(path)
        .output()
        .expect("running the binary")
}

fn lint_commit(message: &str) -> Output {
    let mut child = Command::new(BIN)
        .arg("--commit")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .expect("running the binary");
    child
        .stdin
        .take()
        .expect("stdin is piped")
        .write_all(message.as_bytes())
        .expect("writing the message");
    child.wait_with_output().expect("waiting for the binary")
}

fn stdout(output: &Output) -> String {
    String::from_utf8(output.stdout.clone()).expect("the output is UTF-8")
}

/// Returns the sorted lines of the findings of `rule` in `output`, with
/// no line twice.
fn lines_of(output: &Output, rule: &str) -> Vec<usize> {
    let mut lines: Vec<usize> = stdout(output)
        .lines()
        .filter(|l| l.contains(&format!("::{rule}: ")))
        .map(|l| {
            l.split(':')
                .nth(1)
                .and_then(|n| n.parse().ok())
                .expect("each finding names its line")
        })
        .collect();
    lines.sort_unstable();
    lines.dedup();
    lines
}

#[test]
fn output_order() {
    let tmp = TempDir::new("order");
    let path = tmp.write(
        "order.md",
        "In my opinion, the plan holds.\n\nPass the host and/or the port.\n",
    );
    let output = lint(&path);
    let want = format!(
        "{p}:3:15: [Required] Style::ProseAndOr: `and/or`. Write `a, b, or both`.\n\
         {p}:1:1: [Recommended] Style::ProseOpinion: ",
        p = path.display()
    );
    assert!(stdout(&output).starts_with(&want), "{}", stdout(&output));
}

#[test]
fn native_checks() {
    // The native checks print their findings with the findings of the
    // Weir rules. The arrow in the code span does not draw one.
    let tmp = TempDir::new("native");
    let path = tmp.write(
        "native.md",
        "The request goes client \u{2192} proxy.\n\nRun `a \u{2192} b` to print the mapping.\n",
    );
    let output = lint(&path);
    assert_eq!(output.status.code(), Some(1));
    assert_eq!(
        stdout(&output),
        format!(
            "{}:1:25: [Required] Style::ProseArrow: Arrow `\u{2192}` stands in for a word. Write the verb, a colon after a label, or `>` for a menu path.\n",
            path.display()
        )
    );
}

#[test]
fn two_tiers() {
    // The Required ProseFiller reports `please note that`, and the
    // Recommended one reports the `note that` inside it with the same
    // message. Only the Required finding prints.
    let tmp = TempDir::new("tiers");
    let path = tmp.write("tiers.md", "Please note that the cache is cold.\n");
    let output = lint(&path);
    let want = format!(
        "{}:1:1: [Required] Style::ProseFiller: Filler phrase. Delete the phrase and state the point.\n",
        path.display()
    );
    assert_eq!(stdout(&output), want);
}

#[test]
fn changelogs() {
    let tmp = TempDir::new("changelogs");
    let text = "# Changelog\n\nWe will add a retry flag in the next release.\n";
    for name in ["CHANGELOG.md", "CHANGES.md", "HISTORY.md"] {
        let output = lint(&tmp.write(name, text));
        assert_eq!(output.status.code(), Some(0), "{name}");
        assert_eq!(stdout(&output), "", "{name}");
    }
    let output = lint(&tmp.write("notes.md", text));
    assert_eq!(lines_of(&output, "ProseFutureTense"), [3], "notes.md");
}

#[test]
fn commit() {
    let output = lint_commit("We will add a flag.\n\nPass the host and/or the port.\n");
    assert_eq!(output.status.code(), Some(1));
    assert_eq!(
        stdout(&output),
        "<stdin>:3:15: [Required] Style::ProseAndOr: `and/or`. Write `a, b, or both`.\n"
    );

    let output = lint_commit("fix(cache): warm the cache\n\nThe cache starts warm.\n");
    assert_eq!(output.status.code(), Some(0));
    assert_eq!(stdout(&output), "");
}

#[test]
fn curly_apostrophes() {
    // The Weir rules read a curly apostrophe as a straight one, and
    // ProseTypography reports the curly one.
    let tmp = TempDir::new("apostrophes");
    let path = tmp.write("quotes.md", "The fix doesn\u{2019}t help.\n");
    let output = lint(&path);
    assert_eq!(lines_of(&output, "ProseNegativeContraction"), [1]);
    assert_eq!(lines_of(&output, "ProseTypography"), [1]);
}

#[test]
fn prompt_files() {
    // The case of the prompt-files test of pkgs/prose-lint.nix.
    // ProseCapsEmphasis skips a prompt file for an agent and reports a
    // word in capitals in each other file, a file of a worktree under
    // `.claude/worktrees/` included.
    let tmp = TempDir::new("prompts");
    let text = "Do NOT retry the call.\n";
    for (name, want) in [
        ("CLAUDE.md", false),
        ("configs/claude/agents/demo.md", false),
        (".claude/worktrees/demo/.claude/agents/demo.md", false),
        ("docs/guide.md", true),
        (".claude/worktrees/demo/docs/guide.md", true),
        (".claude/plans/demo.md", true),
    ] {
        let output = lint(&tmp.write(name, text));
        let got = !lines_of(&output, "ProseCapsEmphasis").is_empty();
        assert_eq!(got, want, "{name}");
    }
}

#[test]
fn label_colons() {
    // The first item opens with a bold label, and the colon after it
    // names a term. The second item holds the same words without the
    // marks, and the third holds a second colon after its label.
    let tmp = TempDir::new("labels");
    let path = tmp.write(
        "labels.md",
        "- **The parser is strict**: a stray tab fails the whole file.\n\n\
         - The parser is strict: a stray tab fails the whole file.\n\n\
         - **Parser**: the parser is strict: a stray tab fails the whole file.\n",
    );
    let output = lint(&path);
    assert_eq!(lines_of(&output, "ProseColonClause"), [3, 5]);
    let message = lint_commit(&fs::read_to_string(&path).expect("reading the file"));
    assert_eq!(lines_of(&message, "ProseColonClause"), [3, 5], "message");
}

#[test]
fn transcripts() {
    let tmp = TempDir::new("transcripts");
    let fixtures = Path::new(HARPER).join("fixtures/transcript");
    let read = |name: &str| fs::read_to_string(fixtures.join(name)).expect("reading the fixture");
    let interview = read("interview.md");
    // Prettier writes every fence with backticks, so the test rewrites
    // the fence of podcast.md to tildes.
    let podcast: String = read("podcast.md")
        .lines()
        .map(|l| match l.strip_prefix("```") {
            Some(rest) => format!("~~~{rest}\n"),
            None => format!("{l}\n"),
        })
        .collect();

    struct Case {
        name: &'static str,
        output: Output,
        rule: &'static str,
        want: Vec<usize>,
    }
    let cases = [
        Case {
            name: "interview.md",
            output: lint(&tmp.write("interview.md", &interview)),
            rule: "ProseOpinion",
            want: vec![3, 32],
        },
        Case {
            name: "interview.md as a message",
            output: lint_commit(&interview),
            rule: "ProseOpinion",
            want: vec![3, 32],
        },
        Case {
            name: "interview.md and/or",
            output: lint(&tmp.write("interview.md", &interview)),
            rule: "ProseAndOr",
            want: vec![19],
        },
        Case {
            name: "interview.md and/or as a message",
            output: lint_commit(&interview),
            rule: "ProseAndOr",
            want: vec![19],
        },
        Case {
            name: "interview.markdown",
            output: lint(&tmp.write("interview.markdown", &interview)),
            rule: "ProseOpinion",
            want: vec![3, 32],
        },
        Case {
            name: "podcast.md",
            output: lint(&tmp.write("podcast.md", &podcast)),
            rule: "ProseOpinion",
            want: vec![3, 25, 27],
        },
        Case {
            name: "labels.md",
            output: lint(&tmp.write("labels.md", read("labels.md"))),
            rule: "ProseOpinion",
            want: vec![3, 5, 7, 10],
        },
    ];
    for case in cases {
        assert_eq!(
            lines_of(&case.output, case.rule),
            case.want,
            "{}",
            case.name
        );
    }
}

#[test]
fn only() {
    // --only runs the named rules, Weir rules, built-ins, and native
    // checks alike, and a commit message takes it too.
    let tmp = TempDir::new("only");
    let path = tmp.write(
        "only.md",
        "In my opinion, the plan holds.\n\nPass the host and/or the port.\n\n\
         The request goes client \u{2192} proxy.\n\nRead the the file.\n",
    );
    struct Case {
        name: &'static str,
        only: &'static str,
        commit: bool,
        want: Vec<&'static str>,
    }
    let cases = [
        Case {
            name: "one weir rule",
            only: "ProseAndOr",
            commit: false,
            want: vec!["ProseAndOr"],
        },
        Case {
            name: "one native check",
            only: "ProseArrow",
            commit: false,
            want: vec!["ProseArrow"],
        },
        Case {
            name: "one built-in",
            only: "RepeatedWords",
            commit: false,
            want: vec!["RepeatedWords"],
        },
        Case {
            name: "two rules",
            only: "ProseOpinion,ProseArrow",
            commit: false,
            want: vec!["ProseArrow", "ProseOpinion"],
        },
        Case {
            name: "commit message",
            only: "ProseOpinion",
            commit: true,
            want: vec!["ProseOpinion"],
        },
    ];
    for case in cases {
        let mut command = Command::new(BIN);
        command.args(["--only", case.only]);
        let output = if case.commit {
            let mut child = command
                .arg("--commit")
                .stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .spawn()
                .expect("running the binary");
            child
                .stdin
                .take()
                .expect("stdin is piped")
                .write_all(&fs::read(&path).expect("reading the file"))
                .expect("writing the message");
            child.wait_with_output().expect("waiting for the binary")
        } else {
            command.arg(&path).output().expect("running the binary")
        };
        assert_eq!(output.status.code(), Some(1), "{}", case.name);
        let mut got: Vec<String> = stdout(&output)
            .lines()
            .map(|l| {
                l.split("::")
                    .nth(1)
                    .and_then(|rest| rest.split(':').next())
                    .expect("each finding names its rule")
                    .to_string()
            })
            .collect();
        got.sort_unstable();
        got.dedup();
        assert_eq!(got, case.want, "{}", case.name);
    }
}

#[test]
fn only_source() {
    // A source file takes --only too. ProseTypography also reads the
    // raw file, string literals included, and --only picks that pass by
    // the name of the check.
    let tmp = TempDir::new("only-source");
    let path = tmp.write(
        "a.go",
        "package a\n\n// The cache is probably small.\nvar s = \"\u{201c}q\u{201d}\"\n",
    );
    let cases = [
        ("ProseHedge", vec![(3, "ProseHedge")]),
        ("ProseTypography", vec![(4, "ProseTypography")]),
        ("required/ProseTypography", vec![(4, "ProseTypography")]),
    ];
    for (only, want) in cases {
        let output = Command::new(BIN)
            .args(["--json", "--only", only])
            .arg(&path)
            .output()
            .expect("running the binary");
        assert_eq!(output.status.code(), Some(1), "{only}");
        let mut got: Vec<(u64, String)> = stdout(&output)
            .lines()
            .map(|l| {
                let f: serde_json::Value = serde_json::from_str(l).expect("each line is JSON");
                (
                    f["line"].as_u64().expect("a line number"),
                    f["rule"].as_str().expect("a rule").to_string(),
                )
            })
            .collect();
        got.dedup();
        let want: Vec<(u64, String)> = want
            .into_iter()
            .map(|(line, rule)| (line, rule.to_string()))
            .collect();
        assert_eq!(got, want, "{only}");
    }
}

#[test]
fn only_tier() {
    // A tier-qualified entry runs the rule of one tier, so the
    // Recommended ProseHedge of the second paragraph does not hide the
    // Optional one there.
    let tmp = TempDir::new("only-tier");
    let path = tmp.write(
        "hedge.md",
        "The cache is relatively small.\n\nThe cache is probably relatively small.\n",
    );
    struct Case {
        only: &'static str,
        want: Vec<(u64, &'static str)>,
    }
    let cases = [
        Case {
            only: "optional/ProseHedge",
            want: vec![(1, "Optional"), (3, "Optional")],
        },
        Case {
            only: "recommended/ProseHedge",
            want: vec![(3, "Recommended")],
        },
        Case {
            only: "ProseHedge",
            want: vec![(3, "Recommended"), (1, "Optional")],
        },
    ];
    for case in cases {
        let output = Command::new(BIN)
            .args(["--json", "--only", case.only])
            .arg(&path)
            .output()
            .expect("running the binary");
        assert_eq!(output.status.code(), Some(1), "{}", case.only);
        let got: Vec<(u64, String)> = stdout(&output)
            .lines()
            .map(|l| {
                let f: serde_json::Value = serde_json::from_str(l).expect("each line is JSON");
                assert_eq!(f["rule"], "ProseHedge", "{}", case.only);
                (
                    f["line"].as_u64().expect("a line number"),
                    f["tier"].as_str().expect("a tier").to_string(),
                )
            })
            .collect();
        let want: Vec<(u64, String)> = case
            .want
            .iter()
            .map(|&(line, tier)| (line, tier.to_string()))
            .collect();
        assert_eq!(got, want, "{}", case.only);
    }
}

#[test]
fn json() {
    let tmp = TempDir::new("json");
    let path = tmp.write(
        "json.md",
        "In my opinion, the plan holds.\n\nPass the host and/or the port.\n",
    );
    let output = Command::new(BIN)
        .arg("--json")
        .arg(&path)
        .output()
        .expect("running the binary");
    assert_eq!(output.status.code(), Some(1));
    let text = lint(&path);
    let lines: Vec<serde_json::Value> = stdout(&output)
        .lines()
        .map(|l| serde_json::from_str(l).expect("each line is JSON"))
        .collect();
    assert_eq!(
        lines.len(),
        stdout(&text).lines().count(),
        "one line per finding"
    );
    assert_eq!(
        lines[0],
        serde_json::json!({
            "path": path.display().to_string(),
            "line": 3,
            "col": 15,
            "tier": "Required",
            "kind": "Style",
            "rule": "ProseAndOr",
            "message": "`and/or`. Write `a, b, or both`.",
        })
    );
    assert_eq!(lines[1]["rule"], "ProseOpinion");
    assert_eq!(lines[1]["tier"], "Recommended");
}

#[test]
fn exit_status() {
    let tmp = TempDir::new("status");
    let finding = "Pass the host and/or the port.\n";
    struct Case {
        name: &'static str,
        args: Vec<PathBuf>,
        want: i32,
        stdout: bool,
    }
    let cases = [
        Case {
            name: "clean file",
            args: vec![tmp.write("clean.md", "The cache starts warm.\n")],
            want: 0,
            stdout: false,
        },
        Case {
            name: "findings",
            args: vec![tmp.write("finding.md", finding)],
            want: 1,
            stdout: true,
        },
        Case {
            name: "missing file",
            args: vec![tmp.0.join("missing.md")],
            want: 0,
            stdout: false,
        },
        Case {
            name: "directory",
            args: vec![tmp.0.clone()],
            want: 0,
            stdout: false,
        },
        Case {
            name: "unknown extension",
            args: vec![tmp.write("notes.txt", finding)],
            want: 0,
            stdout: false,
        },
        Case {
            name: "skipped directory",
            args: vec![tmp.write("node_modules/pkg/README.md", finding)],
            want: 0,
            stdout: false,
        },
        Case {
            name: "fixtures",
            args: vec![tmp.write("harper/fixtures/a.md", finding)],
            want: 0,
            stdout: false,
        },
        Case {
            name: "source file",
            args: vec![tmp.write("run.py", "# Pass the host and/or the port.\n")],
            want: 1,
            stdout: true,
        },
        Case {
            name: "file that is not UTF-8",
            args: vec![tmp.write("latin1.md", b"caf\xe9\n")],
            want: 2,
            stdout: false,
        },
        Case {
            name: "no argument",
            args: vec![],
            want: 2,
            stdout: false,
        },
        Case {
            name: "two arguments",
            args: vec![tmp.0.join("a.md"), tmp.0.join("b.md")],
            want: 2,
            stdout: false,
        },
        Case {
            name: "unknown flag",
            args: vec![PathBuf::from("--shadow")],
            want: 2,
            stdout: false,
        },
        Case {
            name: "unknown rule",
            args: vec![
                PathBuf::from("--only"),
                PathBuf::from("ProseNoSuchRule"),
                tmp.0.join("missing.md"),
            ],
            want: 2,
            stdout: false,
        },
        Case {
            name: "rule of another tier",
            args: vec![
                PathBuf::from("--only"),
                PathBuf::from("optional/ProseAndOr"),
                tmp.0.join("finding.md"),
            ],
            want: 2,
            stdout: false,
        },
        Case {
            name: "unknown tier",
            args: vec![
                PathBuf::from("--only"),
                PathBuf::from("Required/ProseAndOr"),
                tmp.0.join("finding.md"),
            ],
            want: 2,
            stdout: false,
        },
        Case {
            name: "only with no rules",
            args: vec![tmp.0.join("finding.md"), PathBuf::from("--only")],
            want: 2,
            stdout: false,
        },
        Case {
            name: "only and a clean result",
            args: vec![
                PathBuf::from("--only"),
                PathBuf::from("ProseHedge"),
                tmp.0.join("finding.md"),
            ],
            want: 0,
            stdout: false,
        },
        Case {
            name: "file and commit",
            args: vec![PathBuf::from("--commit"), tmp.0.join("finding.md")],
            want: 2,
            stdout: false,
        },
    ];
    for case in cases {
        let output = Command::new(BIN)
            .args(&case.args)
            .output()
            .expect("running the binary");
        assert_eq!(output.status.code(), Some(case.want), "{}", case.name);
        assert_eq!(!output.stdout.is_empty(), case.stdout, "{}", case.name);
        assert_eq!(
            !output.stderr.is_empty(),
            case.want == 2,
            "{}: {}",
            case.name,
            String::from_utf8_lossy(&output.stderr)
        );
    }

    let output = Command::new(BIN)
        .arg("--version")
        .output()
        .expect("running the binary");
    assert_eq!(output.status.code(), Some(0), "--version");
}
