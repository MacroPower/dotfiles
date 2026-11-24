//! Tests of the prose-lint-rs binary.

use std::fs;
use std::io::Write as _;
use std::path::{Path, PathBuf};
use std::process::{Command, Output, Stdio};
use std::sync::Mutex;

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
fn fixtures() {
    // Each Weir rule and built-in must report its bad fixture and pass
    // its good one. The binary skips each path under `harper/fixtures`, so
    // the test lints a copy.
    let mut jobs = Vec::new();
    for (dir, tier) in [
        ("required", "Required"),
        ("recommended", "Recommended"),
        ("optional", "Optional"),
    ] {
        let rules = Path::new(HARPER).join("rules").join(dir);
        let mut names: Vec<String> = fs::read_dir(&rules)
            .expect("reading the rules")
            .filter_map(|e| {
                let name = e.expect("listing the rules").file_name();
                name.to_str()?.strip_suffix(".weir").map(String::from)
            })
            .collect();
        let builtins = fs::read_to_string(rules.join("builtins.txt")).expect("reading builtins");
        names.extend(
            builtins
                .lines()
                .map(str::trim)
                .filter(|l| !l.is_empty() && !l.starts_with('#'))
                .map(String::from),
        );
        for name in names {
            for (kind, want) in [("bad", true), ("good", false)] {
                let fixture = Path::new(HARPER)
                    .join("fixtures")
                    .join(dir)
                    .join(format!("{name}.{kind}.md"));
                jobs.push((fixture, format!("[{tier}] "), name.clone(), want));
            }
        }
    }
    assert!(!jobs.is_empty(), "the rules directory holds rules");

    let tmp = TempDir::new("fixtures");
    let jobs = Mutex::new(jobs);
    let failures = Mutex::new(Vec::new());
    std::thread::scope(|s| {
        for worker in 0..4 {
            let (tmp, jobs, failures) = (&tmp, &jobs, &failures);
            s.spawn(move || {
                while let Some((fixture, tier, rule, want)) =
                    jobs.lock().expect("no worker panics").pop()
                {
                    let text = fs::read(&fixture).expect("reading the fixture");
                    let path = tmp.write(&format!("{worker}/fixture.md"), text);
                    let output = lint(&path);
                    let got = stdout(&output)
                        .lines()
                        .any(|l| l.contains(&tier) && l.contains(&format!("::{rule}: ")));
                    if got != want {
                        failures
                            .lock()
                            .expect("no worker panics")
                            .push(format!("{}: reported {rule}: {got}", fixture.display()));
                    }
                }
            });
        }
    });
    let failures = failures.into_inner().expect("no worker panics");
    assert!(failures.is_empty(), "{failures:#?}");
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
    let tmp = TempDir::new("apostrophes");
    let path = tmp.write("quotes.md", "The fix doesn\u{2019}t help.\n");
    assert_eq!(lines_of(&lint(&path), "ProseNegativeContraction"), [1]);
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
            want: 2,
            stdout: false,
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
