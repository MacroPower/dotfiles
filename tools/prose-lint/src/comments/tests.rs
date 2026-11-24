//! The tests of the comment regions.
//!
//! The fixture tests hold the cases of the Nix tests `comment-code`,
//! `trailing-dash`, `license-headers`, `crlf`, `doc-comments`, and
//! `java-spans` of `pkgs/prose-lint.nix` that state a finding on source
//! text. A comment
//! above each list of positions names each place where the Rust linter
//! reads a comment in another way than the bash script.

use std::fs;
use std::path::Path;

use super::*;
use crate::rules::Rules;

/// The directory that holds `rules/` and `fixtures/`, from build.rs.
const HARPER: &str = env!("PROSE_LINT_HARPER");

/// Returns the text of each region of `source`, a file with the
/// extension `ext`.
fn texts(ext: &str, source: &str) -> Vec<String> {
    let syntax = syntax(ext).expect("the table lists the extension");
    Comments::new(syntax, source)
        .expect("the file parses")
        .regions
        .into_iter()
        .map(|r| r.text)
        .collect()
}

#[test]
fn regions() {
    struct Case {
        name: &'static str,
        ext: &'static str,
        source: &'static str,
        want: Vec<&'static str>,
    }
    let cases = [
        Case {
            name: "run of line comments",
            ext: "go",
            source: "// One\n// two.\nvar x = 1\n// Three.\n",
            want: vec!["One\ntwo.", "Three."],
        },
        Case {
            name: "blank line ends a run",
            ext: "rs",
            source: "// One.\n\n// Two.\nfn f() {}\n",
            want: vec!["One.", "Two."],
        },
        Case {
            name: "comment after code",
            ext: "sh",
            source: "a=1 # One\n# two.\nb=2 # Three.\nc=3 # Four.\n",
            want: vec!["One", "two.", "Three.", "Four."],
        },
        Case {
            name: "directive and shebang end a run",
            ext: "go",
            source: "// One.\n//go:build linux\n// Two.\npackage p\n",
            want: vec!["One.", "Two."],
        },
        Case {
            name: "reason after a directive",
            ext: "go",
            source: "package p\n\nvar a = 1 //nolint:gosec // One.\n//----------\n",
            want: vec!["One."],
        },
        Case {
            name: "shebang",
            ext: "sh",
            source: "#!/bin/sh\n# One.\n",
            want: vec!["One."],
        },
        Case {
            name: "doc markers and a tab",
            ext: "rs",
            source: "//! One.\n/// Two.\n//\tcode\nfn f() {}\n",
            want: vec!["One.\nTwo.\n\tcode"],
        },
        Case {
            name: "inner doc comments",
            ext: "rs",
            source: "//! One\n//!\n//! two.\n//! Three\nfn f() {}\n",
            want: vec!["One\n\ntwo.\nThree"],
        },
        Case {
            name: "javadoc",
            ext: "java",
            source: "class A {\n  /**\n   * One `a\n   * b` two.\n   *\n   * - Item.\n   */\n  int a;\n}\n",
            want: vec!["One `a\nb` two.\n\n- Item."],
        },
        Case {
            name: "nixpkgs doc comment",
            ext: "nix",
            source: "{\n  /**\n    One.\n\n    * An item\n      wraps.\n  */\n  a = 1;\n}\n",
            want: vec!["One.\n\n* An item\n  wraps."],
        },
        Case {
            name: "scalafmt",
            ext: "scala",
            source: "object A {\n  /** One\n    * two.\n    */\n  def f = 1\n}\n",
            want: vec!["One\ntwo."],
        },
        Case {
            name: "go block with a gofmt code block",
            ext: "go",
            source: "package p\n\nfunc f() {\n\t/*\n\t\tOne.\n\n\t\t\tcode\n\t*/\n}\n",
            want: vec!["One.\n\n\tcode"],
        },
        Case {
            name: "block of one line and an empty block",
            ext: "ts",
            source: "/** One. */\nconst a = 1;\n/**/\n/* Two. */ const b = 1;\n",
            want: vec!["One.", "Two."],
        },
        Case {
            name: "block next to a line comment",
            ext: "c",
            source: "/* One. */\n// Two.\nint a;\n",
            want: vec!["One.", "Two."],
        },
        Case {
            name: "lua and haskell blocks",
            ext: "lua",
            source: "--[[ One. ]]\nlocal a = 1\n--[==[\nTwo.\n]==]\nlocal b = 1\n",
            want: vec!["One.", "Two."],
        },
        Case {
            name: "haskell block",
            ext: "hs",
            source: "{- One.\n   Two. -}\nmain = pure ()\n",
            want: vec!["One.\nTwo."],
        },
        Case {
            name: "haddock line comments",
            ext: "hs",
            source: "-- | One\n-- two.\nf = 1\n\ng = 1 -- ^ Three.\n\nclass C a where\n  -- | Four\n  --   five.\n  h :: a\n",
            want: vec!["One\ntwo.", "Three.", "Four\n  five."],
        },
        Case {
            name: "haddock blocks",
            ext: "hs",
            source: "{-| One. -}\nh = 1\n\n{- ^ Two\n   three. -}\ni = 1\n",
            want: vec!["One.", "Two\nthree."],
        },
        Case {
            name: "powershell block",
            ext: "ps1",
            source: "<# One. #>\n$a = 1\n",
            want: vec!["One."],
        },
        Case {
            name: "lines of a nix string",
            ext: "nix",
            source: "{\n  a = ''\n    # One.\n    echo hi\n  '';\n  b = \"# Two.\";\n}\n",
            want: vec!["One."],
        },
        Case {
            name: "javascript template",
            ext: "ts",
            source: "const a = `\n// One.\n`;\n// Two.\n",
            want: vec!["Two."],
        },
        Case {
            name: "go raw string",
            ext: "go",
            source: "package p\n\nconst a = `\n// One.\n/* Two. */\n`\n",
            want: vec![],
        },
        Case {
            name: "frames and banners",
            ext: "sh",
            source: "# -----\n# One --\n# --- Two ---\n# -- Three --\n# --force four\n# ---\n",
            want: vec!["\nOne --\nTwo\nThree\n--force four\n"],
        },
        Case {
            name: "lua banner",
            ext: "lua",
            source: "--- One ---\n--- Two.\nlocal a = 1\n",
            want: vec!["One\nTwo."],
        },
        Case {
            name: "crlf",
            ext: "rs",
            source: "// One\r\n// two.\r\nfn f() {}\r\n",
            want: vec!["One\ntwo."],
        },
    ];
    for case in cases {
        assert_eq!(texts(case.ext, case.source), case.want, "{}", case.name);
    }
}

#[test]
fn source_positions() {
    // Each character of a region keeps the line and the column of its
    // source character. The line break takes the position of the line
    // ending above it.
    let source = "\t// \u{e9}a\n  // b\r\n";
    let syntax = syntax("rs").expect("the table lists rs");
    let comments = Comments::new(syntax, source).expect("the file parses");
    let region = &comments.regions[0];
    assert_eq!(region.text, "\u{e9}a\nb");
    let positions: Vec<_> = (0..4)
        .map(|i| {
            let at = region.position(i);
            (at.line, at.col)
        })
        .collect();
    assert_eq!(positions, [(1, 5), (1, 6), (1, 7), (2, 6)]);
}

#[test]
fn parser_tokens() {
    // Each token reads its own text from the source. A paragraph break
    // of zero characters sits between two comments.
    let source = "// One two\n//   three.\nx = 1\n// Four.\n";
    let syntax = syntax("rs").expect("the table lists rs");
    let comments = Comments::new(syntax, source).expect("the file parses");
    let chars: Vec<char> = source.chars().collect();
    let tokens = RegionParser::new(&comments.regions).parse(&chars);
    let words: Vec<String> = tokens
        .iter()
        .filter(|t| matches!(t.kind, TokenKind::Word(_)))
        .map(|t| chars[t.span.start..t.span.end].iter().collect())
        .collect();
    assert_eq!(words, ["One", "two", "three", "Four"]);
    let breaks: Vec<_> = tokens
        .iter()
        .filter(|t| t.kind == TokenKind::ParagraphBreak)
        .map(|t| (t.span.start, t.span.end))
        .collect();
    assert_eq!(breaks, [(22, 22)]);
}

#[test]
fn license_header() {
    struct Case {
        name: &'static str,
        ext: &'static str,
        source: &'static str,
        want: Option<RangeInclusive<usize>>,
    }
    let cases = [
        Case {
            name: "line comments",
            ext: "go",
            source: "// Copyright 2024 A.\n// Text.\n\n// Package p.\npackage p\n",
            want: Some(1..=2),
        },
        Case {
            name: "after a shebang and a blank line",
            ext: "py",
            source: "#!/usr/bin/env python3\n\n# Copyright 2024 A.\n\n# Text.\n",
            want: Some(3..=3),
        },
        Case {
            name: "first comment names no license",
            ext: "go",
            source: "// Package p.\n/* Copyright 2024 A. */\npackage p\n",
            want: None,
        },
        Case {
            name: "block under an SPDX line",
            ext: "c",
            source: "// SPDX-License-Identifier: MIT\n/* Copyright 2024 A. */\n/* Text. */\nint a;\n",
            want: Some(1..=2),
        },
        Case {
            name: "code after the closer",
            ext: "c",
            source: "/* Copyright 2024 A. */ int a;\n// Copyright 2024 B.\n",
            want: Some(1..=1),
        },
        Case {
            name: "banner under the header",
            ext: "sh",
            source: "# Copyright 2024 A.\n##########\n# Text.\n",
            want: Some(1..=1),
        },
        Case {
            name: "file opens with a banner",
            ext: "lua",
            source: "----------\n-- Copyright 2024 A.\n----------\n",
            want: None,
        },
        Case {
            name: "empty marker line in the header",
            ext: "sh",
            source: "## Copyright 2024 A.\n##\n## Licensed under MIT.\n",
            want: Some(1..=3),
        },
    ];
    for case in cases {
        let syntax = syntax(case.ext).expect("the table lists the extension");
        let comments = Comments::new(syntax, case.source).expect("the file parses");
        assert_eq!(comments.license, case.want, "{}", case.name);
    }
}

/// Lints `text`, a file named `name`, and returns `line:col` of each
/// finding of the rules that `names` lists, by line and column.
fn positions(rules: &mut Rules, name: &str, text: &str, names: &[&str]) -> Vec<String> {
    let ext = name.rsplit('.').next().expect("the name has an extension");
    let syntax = syntax(ext).expect("the table lists the extension");
    let mut found: Vec<(usize, usize)> = crate::lint_source(rules, syntax, &[], text)
        .expect("the file parses")
        .into_iter()
        .filter(|f| names.contains(&f.rule.as_str()))
        .map(|f| (f.line, f.col))
        .collect();
    found.sort_unstable();
    found.iter().map(|(l, c)| format!("{l}:{c}")).collect()
}

/// Returns `text` with `marker` in place of the `#` that opens each
/// comment line, and `code` in place of each `x=1` line.
fn convert(text: &str, marker: &str, code: &str) -> String {
    text.lines()
        .map(|l| match l.strip_prefix('#') {
            Some(rest) => format!("{marker}{rest}\n"),
            None if l == "x=1" => format!("{code}\n"),
            None => format!("{l}\n"),
        })
        .collect()
}

/// Returns `text` with each run of `#` lines turned into a block
/// comment that opens with `/*`, opens each later line with ` *`, and
/// ends in ` */`, and with `code` in place of each `x=1` line.
fn blocks(text: &str, code: &str) -> String {
    let lines: Vec<&str> = text.lines().collect();
    let comment = |i: usize| lines.get(i).is_some_and(|l| l.starts_with('#'));
    let mut out = String::new();
    for (i, line) in lines.iter().enumerate() {
        if !comment(i) {
            out.push_str(if *line == "x=1" { code } else { line });
        } else {
            let first = i == 0 || !comment(i - 1);
            out.push_str(if first { "/*" } else { " *" });
            out.push_str(&line[1..]);
            if !comment(i + 1) {
                out.push_str(" */");
            }
        }
        out.push('\n');
    }
    out
}

fn crlf(text: &str) -> String {
    text.replace('\n', "\r\n")
}

/// Returns the positions of `hash` with each column moved by `shift`.
fn shifted(hash: &[&str], shift: usize) -> Vec<String> {
    hash.iter()
        .map(|p| {
            let (line, col) = p.split_once(':').expect("each position holds a colon");
            let col: usize = col.parse().expect("each column is a number");
            format!("{line}:{}", col + shift)
        })
        .collect()
}

/// The ProseDash and ProseMayPossibility findings of `code.sh`, which
/// lists the markdown cases that its comments hold.
const HASH: &[&str] = &[
    "2:38", "7:6", "15:6", "23:6", "31:6", "36:8", "42:6", "47:6", "50:20", "53:23", "56:39",
    "59:6", "62:6", "65:29", "68:30", "71:29", "74:13", "77:8", "81:6", "84:17", "88:6", "92:6",
    "96:6", "100:6", "104:6", "107:6", "110:8", "114:8", "117:29", "122:6", "125:6", "128:6",
    "131:6", "133:16", "136:16", "174:18", "177:16", "180:18", "183:18", "186:18", "189:16",
    "192:20", "195:20", "198:18", "201:18", "204:16", "233:18", "236:16", "239:25", "244:19",
    "247:20", "250:18", "253:28", "281:21", "285:18", "289:21", "293:20", "297:23", "301:21",
    "306:30", "309:16", "314:24",
];

#[test]
fn comment_code() {
    let dir = Path::new(HARPER).join("fixtures/comment-code");
    let read = |name: &str| fs::read_to_string(dir.join(name)).expect("reading the fixture");
    let code = read("code.sh");
    let template = read("template.ts");
    let describe = read("describe.tf");

    let hash: Vec<String> = HASH.iter().map(ToString::to_string).collect();
    let wide = shifted(HASH, 1);
    let deep = shifted(HASH, 2);
    let template_want: Vec<String> = [
        "5:28", "8:28", "11:28", "14:28", "18:7", "22:7", "26:7", "30:7", "33:30", "38:28",
        "41:28", "44:28", "47:28", "50:28", "53:28", "56:28", "61:28", "67:7", "73:28", "78:28",
    ]
    .iter()
    .map(ToString::to_string)
    .collect();
    let describe_want = vec!["6:31".to_string(), "40:18".to_string()];
    let trail = "x=1\n# The job runs -- \nx=1\n# > The rule follows --\n# > ----- \nx=1\n";

    struct Case {
        name: &'static str,
        text: String,
        want: Vec<String>,
    }
    let cases = vec![
        Case {
            name: "code.sh",
            text: code.clone(),
            want: hash.clone(),
        },
        Case {
            name: "crlf.sh",
            text: crlf(&code),
            want: hash.clone(),
        },
        Case {
            name: "code.nix",
            text: code.clone(),
            want: hash.clone(),
        },
        Case {
            name: "code.awk",
            text: code.clone(),
            want: hash.clone(),
        },
        Case {
            name: "code.py",
            text: code.clone(),
            want: hash.clone(),
        },
        Case {
            name: "code.yaml",
            text: code.clone(),
            want: hash.clone(),
        },
        Case {
            name: "code.tf",
            text: code.clone(),
            want: hash.clone(),
        },
        Case {
            name: "code.rs",
            text: convert(&code, "//", "let x = 1;"),
            want: wide.clone(),
        },
        Case {
            name: "code.ts",
            text: convert(&code, "//", "const x = 1;"),
            want: wide.clone(),
        },
        Case {
            name: "code.lua",
            text: convert(&code, "--", "local x = 1"),
            want: wide.clone(),
        },
        Case {
            name: "code.go",
            text: convert(&code, "//", "var x = 1"),
            want: wide.clone(),
        },
        Case {
            name: "slash.tf",
            text: convert(&code, "//", "x=1"),
            want: wide.clone(),
        },
        Case {
            name: "double.sh",
            text: convert(&code, "##", "x=1"),
            want: wide.clone(),
        },
        Case {
            name: "double.py",
            text: convert(&code, "##", "x=1"),
            want: wide.clone(),
        },
        Case {
            name: "doc.lua",
            text: convert(&code, "---", "local x = 1"),
            want: deep,
        },
        Case {
            name: "block.ts",
            text: blocks(&code, "const x = 1;"),
            want: wide.clone(),
        },
        Case {
            name: "block.rs",
            text: blocks(&code, "let x = 1;"),
            want: wide.clone(),
        },
        Case {
            name: "block.go",
            text: blocks(&code, "var x = 1"),
            want: wide.clone(),
        },
        Case {
            name: "block.nix",
            text: blocks(&code, "x = 1;"),
            want: wide.clone(),
        },
        Case {
            name: "crlf-block.ts",
            text: crlf(&blocks(&code, "const x = 1;")),
            want: wide,
        },
        Case {
            name: "template.ts",
            text: template.clone(),
            want: template_want.clone(),
        },
        Case {
            name: "template.js",
            text: template.clone(),
            want: template_want.clone(),
        },
        Case {
            name: "template.jsx",
            text: template.clone(),
            want: template_want.clone(),
        },
        Case {
            name: "template.tsx",
            text: template.clone(),
            want: template_want.clone(),
        },
        Case {
            name: "crlf.ts",
            text: crlf(&template),
            want: template_want,
        },
        Case {
            name: "describe.tf",
            text: describe.clone(),
            want: describe_want.clone(),
        },
        Case {
            name: "crlf.tf",
            text: crlf(&describe),
            want: describe_want,
        },
        Case {
            name: "trail.sh",
            text: trail.to_string(),
            want: vec![],
        },
        Case {
            name: "trail.py",
            text: trail.to_string(),
            want: vec![],
        },
    ];
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    for case in cases {
        let got = positions(
            &mut rules,
            case.name,
            &case.text,
            &["ProseDash", "ProseMayPossibility"],
        );
        assert_eq!(got, case.want, "{}", case.name);
    }
}

#[test]
fn trailing_dash() {
    // A comment after code that ends in `--` goes on in the comment line
    // under it, so the first line draws ProseDash on its `--`. Code
    // follows the second such comment, and an empty comment line follows
    // the third, so neither draws a finding. Nor does the banner after
    // code above a comment line with text.
    let shell = "a=1 # The cache holds the value for a subscription --\n\
                 # setting both pins the split.\n\
                 b=2 # The cache holds the value for a subscription --\n\
                 c=3\n\
                 d=\"# x\" # --- The section ---\n\
                 # It holds the value.\n\
                 e=4 # It holds the value --\n\
                 #\n\
                 f=5\n";
    let swap = |marker: &str, code: &dyn Fn(char, &str) -> String| -> String {
        shell
            .lines()
            .map(|l| {
                let l = match l.split_once('=') {
                    Some((name, rest)) if name.len() == 1 => {
                        let (value, comment) = rest.split_once(" # ").unwrap_or((rest, ""));
                        let code = code(name.chars().next().expect("one letter"), value);
                        if comment.is_empty() {
                            code
                        } else {
                            format!("{code} {marker} {comment}")
                        }
                    }
                    _ => l.replacen('#', marker, 1),
                };
                format!("{l}\n")
            })
            .collect()
    };
    let nix = format!(
        "{{\n{}}}\n",
        swap("#", &|n, v| format!("{n} = {v};"))
            .lines()
            .map(|l| format!("  {l}\n"))
            .collect::<String>()
    );
    struct Case {
        name: &'static str,
        text: String,
        want: Vec<&'static str>,
    }
    let cases = [
        Case {
            name: "trail.sh",
            text: shell.to_string(),
            want: vec!["1:52"],
        },
        Case {
            name: "crlf.sh",
            text: crlf(shell),
            want: vec!["1:52"],
        },
        Case {
            name: "trail.nix",
            text: nix,
            want: vec!["2:57"],
        },
        Case {
            name: "trail.ts",
            text: swap("//", &|n, v| format!("const {n} = {v};")),
            want: vec!["1:62"],
        },
        Case {
            name: "trail.rs",
            text: swap("//", &|n, v| format!("let {n} = {v};")),
            want: vec!["1:60"],
        },
        Case {
            name: "trail.lua",
            text: swap("--", &|n, v| format!("local {n} = {v}")),
            want: vec!["1:61"],
        },
    ];
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    for case in cases {
        let got = positions(&mut rules, case.name, &case.text, &["ProseDash"]);
        assert_eq!(got, case.want, "{}", case.name);
    }
}

#[test]
fn license_headers() {
    let dir = Path::new(HARPER).join("fixtures/license");
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    let mut lint = |name: &str| {
        let text = fs::read_to_string(dir.join(name)).expect("reading the fixture");
        let ext = name.rsplit('.').next().expect("the name has an extension");
        let syntax = syntax(ext).expect("the table lists the extension");
        crate::lint_source(&mut rules, syntax, &[], &text).expect("the file parses")
    };
    for name in [
        "bsd.go",
        "apache.go",
        "apache.py",
        "apache.ts",
        "asf.java",
        "gpl.c",
    ] {
        assert_eq!(lint(name), [], "{name}");
    }
    assert!(!lint("control.go").is_empty(), "control.go");
    for (name, line) in [
        ("spdx.ts", 2),
        ("blockdoc.go", 4),
        ("banner.lua", 7),
        ("banner.sh", 5),
    ] {
        assert!(lint(name).iter().any(|f| f.line == line), "{name}:{line}");
    }
}

#[test]
fn crlf_endings() {
    // The bash script read the text after `# >` and four blanks on line
    // 10 of boxed.sh as prose, so ProseLatinAbbreviation reported it. A
    // region reads it as markdown, where the text is indented code.
    let want = [
        "boxed.sh:4:3 ProseSentenceLength",
        "deploy.yaml:1:17 ProseAndOr",
        "license.go:5:31 ProseAndOr",
        "license.py:7:17 ProseAndOr",
        "license.py:9:21 ProseDash",
        "main.tf:1:17 ProseAndOr",
    ];
    let names = [
        "ProseSentenceLength",
        "ProseLatinAbbreviation",
        "ProseAndOr",
        "ProseDash",
    ];
    let dir = Path::new(HARPER).join("fixtures/crlf");
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    for endings in ["lf", "crlf"] {
        let mut got = Vec::new();
        for name in [
            "boxed.sh",
            "deploy.yaml",
            "license.go",
            "license.py",
            "main.tf",
        ] {
            let mut text = fs::read_to_string(dir.join(name)).expect("reading the fixture");
            if endings == "crlf" {
                text = crlf(&text);
            }
            let ext = name.rsplit('.').next().expect("the name has an extension");
            let syntax = syntax(ext).expect("the table lists the extension");
            let mut findings = crate::lint_source(&mut rules, syntax, &[], &text)
                .expect("the file parses")
                .into_iter()
                .filter(|f| names.contains(&f.rule.as_str()))
                .map(|f| (f.line, f.col, f.rule))
                .collect::<Vec<_>>();
            findings.sort();
            got.extend(
                findings
                    .into_iter()
                    .map(|(l, c, r)| format!("{name}:{l}:{c} {r}")),
            );
        }
        assert_eq!(got, want, "{endings}");
    }
}

#[test]
fn doc_comments() {
    // The cases of the doc-comments test. A block comment above
    // `import "C"` holds C code for cgo.
    // The bash script kept it from the native checks, but a region reads
    // it as prose, so ProseLatinAbbreviation reports its `e.g.`. A block
    // comment that code follows on its line is prose too, so
    // ProseLatinAbbreviation reports the `e.g.` before `const d`. The
    // bash script reports the long sentence of the `@return` tag of
    // Tags.java at the tag. The region drops each block tag, so the Rust
    // linter reports the sentence at the first word after the tag.
    let doc_nix = "{\n  /**\n    A span wraps over `it may\n    stop` here, and then it may stop.\n\n    * It may help, e.g. in the item.\n    * The item wraps `it may\n      stop` and then it may stop.\n\n    ```nix\n    It may help.\n    ```\n    It may help after the fence.\n  */\n  add = a: b: a + b;\n  glob = ''\n    ${lib.concatStrings [ \"a\" ]}\n    /* It may help, e.g. in a string.\n  '';\n  src = \"src/**/*.c\";\n}\n";
    let doc_go = "/*\nPackage doc wraps `it may\nstop` here, and then it may stop.\n\nIt may help, e.g. here.\n*/\npackage doc\n\n/*\n#include <stdio.h>\n// It may help, e.g. in C.\n*/\nimport \"C\"\n\n/*\n* A span wraps over `it may\n* stop` here, and then it may stop.\n*/\nvar x = 1\n\n/*\nRun it like this:\n\n\tdoc -x e.g. a | b/c\n\tdoc --may-stop it may stop\n*/\nvar y = 1\n\nfunc f() {\n\t/*\n\t\tIt may help, e.g. in a function.\n\n\t\t\tdoc -x e.g. it may stop\n\t*/\n}\n\nfunc g() {\n\t/* It may help in a function.\n\tIt may help, e.g. on the next line.\n\n\tIt may stop here.\n\t*/\n}\n\nconst src = `\n/*\n * It may help, e.g. in C.\n */\n`\n";
    let doc_ts = "const s = `\n/* It may help, e.g. in a template.\n*/\n`;\n/** It may help, e.g. on one line. */\nconst a = 1;\n/**/\n/* A span wraps over `it may\n  stop` here, and then it may stop. */\nconst b = 1;\n\t/**\n\t * A tab indents `it may\n\t * stop` here, and then it may stop.\n\t */\nconst c = 1;\n/* It may help, e.g. before code. */ const d = 1;\n/**\n * Adds three numbers.\n *\n * @param a - the first number to add to the running total\n * @param b - the second number to add to the running total\n * @param c - the third number to add to the running total\n * @returns the sum of all the numbers that the caller passes in\n */\nconst add = (a: number, b: number, c: number) => a + b + c;\n/** It may read {@link store/cache} values, as {@linkcode Store e.g. here} does. */\nconst e = 1;\n";
    let tags_java = "class Tags {\n  /**\n   * Adds three numbers.\n   *\n   * @param a the first number to add to the running total\n   * @param b the second number to add to the running total\n   * @param c the third number to add to the running total\n   * @return the sum of all the numbers that the caller passes in, and it\n   *     helps to know that the caller sums them in the order that it reads\n   *     them from the input, so the sum can overflow before the last one\n   */\n  int add(int a, int b, int c) {\n    return a + b + c;\n  }\n\n  /** Opens it in {@code read/write} mode, as {@link Files e.g. here}, and it may stop. */\n  void open() {}\n}\n";
    let raw_rs = "let s = r\"\n/* It may help, e.g. in a string.\n\";\n// A span wraps over `it may\n// stop` here, and then it may stop.\nfn f() {}\n";
    let ldoc_lua = "--- Returns the sum, e.g. of two numbers.\n---@param a number It may help, e.g. here.\n--- @return number\nlocal function add(a) return a end\n--- The section ---\n--- It holds the value.\nlocal b = 1\n";
    let doc_scala = "object Doc {\n  /** Runs the code below, and a span wraps over `it may\n    * stop` here, and then it may stop.\n    *\n    * ```scala\n    * val x = 1 // it may stop\n    * ```\n    * It may help after the fence.\n    */\n  def f = 1\n}\n";
    let fence_c = "int a;\n/*\nSet ```CFLAGS\n-O2``` first, and then it may stop.\nIt may stop here.\n*/\nint b;\n";
    let next = "/* Return `true' when the file is open. */\n/* It may help to set `flag' first. */\nint a;\n/** Set `x. */\n// It may help `here` and it may stop.\nint b;\n// Run it:\n// ```\n// make it\n/** It may help here. */\nint c;\n/**\n * Run it:\n * ```\n * make it\n */\n// It may help here.\nint d;\n";
    let dash_ts = "// The cache holds the value for a subscription --\n/* setting both pins the split. */\nconst a = 1;\n/* The cache holds the value for a subscription -- */\n// setting both pins the split.\nconst b = 1;\n/* The cache holds the value for a subscription -- */\n/* setting both pins the split. */\nconst c = 1;\nconst d = 1; // The cache holds the value for a subscription --\n/* setting both pins the split. */\nconst e = 1;\n/*\n * The cache holds the value for a subscription --\n * setting both pins the split.\n */\nconst f = 1;\n";
    let dash_nix = "{\n  # The cache holds the value for a subscription --\n  /* setting both pins the split. */\n  a = 1;\n  /* The cache holds the value for a subscription -- */\n  # setting both pins the split.\n  b = 1;\n  /* The cache holds the value for a subscription -- */\n  /* setting both pins the split. */\n  c = 1;\n  d = 1; # The cache holds the value for a subscription --\n  /* setting both pins the split. */\n  e = 1;\n  /*\n   * The cache holds the value for a subscription --\n   * setting both pins the split.\n   */\n  f = 1;\n}\n";
    let adj_ts = "// The name of the host that the client sends each of its requests to by default when the caller names none\n/** The port on that host that the client connects to when the caller sets none and the host publishes none */\nexport const port = 1;\n";
    let adj_c = "struct opts {\n    /* Delay in milliseconds that the client waits between two of those retries when the server answers busy */\n    /* Upper bound in milliseconds on the delay that the client waits between retries when the server keeps answering busy */\n    int delay;\n};\n";

    struct Case {
        name: &'static str,
        text: &'static str,
        want: Vec<&'static str>,
    }
    let cases = [
        Case {
            name: "doc.nix",
            text: doc_nix,
            want: vec![
                "4:29 ProseMayPossibility",
                "6:10 ProseMayPossibility",
                "6:20 ProseLatinAbbreviation",
                "8:25 ProseMayPossibility",
                "13:8 ProseMayPossibility",
            ],
        },
        Case {
            name: "doc.go",
            text: doc_go,
            want: vec![
                "3:25 ProseMayPossibility",
                "5:4 ProseMayPossibility",
                "5:14 ProseLatinAbbreviation",
                "11:7 ProseMayPossibility",
                "11:17 ProseLatinAbbreviation",
                "17:27 ProseMayPossibility",
                "31:6 ProseMayPossibility",
                "31:16 ProseLatinAbbreviation",
                "38:8 ProseMayPossibility",
                "39:5 ProseMayPossibility",
                "39:15 ProseLatinAbbreviation",
                "41:5 ProseMayPossibility",
            ],
        },
        Case {
            name: "doc.ts",
            text: doc_ts,
            want: vec![
                "5:8 ProseMayPossibility",
                "5:18 ProseLatinAbbreviation",
                "9:27 ProseMayPossibility",
                "13:29 ProseMayPossibility",
                "16:7 ProseMayPossibility",
                "16:17 ProseLatinAbbreviation",
                "26:8 ProseMayPossibility",
            ],
        },
        Case {
            name: "Tags.java",
            text: tags_java,
            want: vec!["8:14 ProseSentenceLength", "16:79 ProseMayPossibility"],
        },
        Case {
            name: "raw.rs",
            text: raw_rs,
            want: vec!["5:28 ProseMayPossibility"],
        },
        Case {
            name: "ldoc.lua",
            text: ldoc_lua,
            want: vec!["1:22 ProseLatinAbbreviation"],
        },
        Case {
            name: "Doc.scala",
            text: doc_scala,
            want: vec!["3:31 ProseMayPossibility", "8:10 ProseMayPossibility"],
        },
        Case {
            name: "fence.c",
            text: fence_c,
            want: vec!["4:27 ProseMayPossibility", "5:4 ProseMayPossibility"],
        },
        Case {
            name: "next.ts",
            text: next,
            want: vec![
                "2:7 ProseMayPossibility",
                "5:7 ProseMayPossibility",
                "5:30 ProseMayPossibility",
                "10:8 ProseMayPossibility",
                "17:7 ProseMayPossibility",
            ],
        },
        Case {
            name: "next.c",
            text: next,
            want: vec![
                "2:7 ProseMayPossibility",
                "5:7 ProseMayPossibility",
                "5:30 ProseMayPossibility",
                "10:8 ProseMayPossibility",
                "17:7 ProseMayPossibility",
            ],
        },
        Case {
            name: "dash.ts",
            text: dash_ts,
            want: vec!["14:49 ProseDash"],
        },
        Case {
            name: "dash.nix",
            text: dash_nix,
            want: vec!["15:51 ProseDash"],
        },
        Case {
            name: "adj.ts",
            text: adj_ts,
            want: vec![],
        },
        Case {
            name: "adj.c",
            text: adj_c,
            want: vec![],
        },
    ];
    let names = [
        "ProseMayPossibility",
        "ProseLatinAbbreviation",
        "ProseDash",
        "ProseSentenceLength",
        "ProseSlash",
    ];
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    for case in cases {
        let ext = case
            .name
            .rsplit('.')
            .next()
            .expect("the name has an extension");
        let syntax = syntax(ext).expect("the table lists the extension");
        let mut found: Vec<_> = crate::lint_source(&mut rules, syntax, &[], case.text)
            .expect("the file parses")
            .into_iter()
            .filter(|f| names.contains(&f.rule.as_str()))
            .map(|f| (f.line, f.col, f.rule))
            .collect();
        found.sort();
        let got: Vec<String> = found
            .into_iter()
            .map(|(l, c, r)| format!("{l}:{c} {r}"))
            .collect();
        assert_eq!(got, case.want, "{}", case.name);
    }
}

#[test]
fn java_spans() {
    // The case of the java-spans test. Code spans sit in a `//`, a `///`,
    // and a block comment, and a span wraps in a Javadoc comment. Other
    // comments share their line with code. Each `may` after a span draws
    // a finding, and no `may` inside a span or the fence does.
    let text = "class Spans {\n  // Run `it may stop` here, and then it may stop.\n  /// Run ``it may stop`` here, and then it may stop.\n  /** Run `it may stop` here, and then it may stop. */\n  int a;\n  /**\n   * A span wraps over `it may\n   * stop` here, and then it may stop.\n   *\n   * ```java\n   * It may help.\n   * ```\n   * It may help after the fence, and `a <b> c` may stop.\n   * Set `x` and it may stop, or `y` may stop.\n   * Set `caf\u{e9}` and then it may stop.\n   */\n  int b;\n  // Set `caf\u{e9}` and then it may stop.\n  int c; // Run `it may stop` here, and then it may stop.\n  /* Run `it may stop` here. */ int d; // Then it may stop.\n  int e; /* Run `it may stop` here, and then it may stop. */\n  int f; /* A block opens after code with `it may stop`\n   * and goes on with `it may stop`, and then it may stop.\n   */\n  /**\n   * Run `it may stop` here, and then it may stop. */ int g;\n  char q = '\"'; // Run `it may stop` here, and then it may stop.\n}\n";
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    assert_eq!(
        positions(&mut rules, "Spans.java", text, &["ProseMayPossibility"]),
        [
            "2:42", "3:45", "4:43", "8:30", "13:9", "13:49", "14:21", "14:38", "15:29", "18:29",
            "19:49", "20:51", "21:49", "23:50", "26:42", "27:56",
        ]
    );
}

#[test]
fn doc_markup() {
    // Each file holds the doc comment markup of its language next to
    // prose that draws a finding. Without the markup pass, the rules
    // read the markup as prose. ProseSemicolon reports an HTML entity,
    // ProseJust and ProseDash report a Haddock example, and ProseHedge
    // reports the identifier `'Maybe'`. Each file draws only the
    // ProseMayPossibility findings of its prose. An inline tag that no
    // `}` closes stays text, and the file still lints. A block tag that
    // fills its line leaves the indented description on the next line as
    // prose, not as an indented code block. A line that holds only `@`
    // among the result lines of an example does not open a code block.
    // An entity that opens a paragraph does not turn the paragraph into
    // an indented code block. A Haddock span that holds a backtick reads
    // as a code span, so ProseFutureTense still finds the subject of
    // `will` in lead.hs.
    struct Case {
        name: &'static str,
        text: &'static str,
        want: Vec<&'static str>,
    }
    let cases = [
        Case {
            name: "Entity.java",
            text: "class Entity {\n  /**\n   * Returns -1 if a &lt; b, and it may help here.\n   *\n   * @param a the first value\n   * @param b the second value\n   * @throws IllegalArgumentException if a &gt; b\n   */\n  int cmp(int a, int b) { return 0; }\n}\n",
            want: vec!["3:37 ProseMayPossibility"],
        },
        Case {
            name: "Ret.java",
            text: "class Ret {\n  /**\n   * Returns the value.\n   *\n   * @return\n   *     the value, and it may help here\n   * @param foo\n   *\tthe foo, and it may help here\n   */\n  int get(int foo) { return 0; }\n}\n",
            want: vec!["6:28 ProseMayPossibility", "8:22 ProseMayPossibility"],
        },
        Case {
            name: "ret.ts",
            text: "/**\n * Saves the value.\n *\n * @param {string} name\n *     the name, and it may help here\n * @returns {Promise<void>}\n *     a promise, and it may help here\n */\nexport const save = (name: string) => Promise.resolve();\n",
            want: vec!["5:25 ProseMayPossibility", "7:26 ProseMayPossibility"],
        },
        Case {
            name: "Lead.java",
            text: "class Lead {\n  /**\n   * Compares two values.\n   *\n   * @return &lt; 0, if it may help here\n   */\n  int cmp(int a, int b) { return 0; }\n\n  /**\n   * Shifts the value.\n   *\n   * &lt;&lt; is a shift, and it may help here.\n   */\n  int shl(int a) { return 0; }\n}\n",
            want: vec!["5:28 ProseMayPossibility", "12:34 ProseMayPossibility"],
        },
        Case {
            name: "lead.hs",
            text: "module Lead where\n\n-- | Sorts it.\n--\n-- @'sortBy' ('compare' ``on`` f)@ will be faster, and it may help.\nf :: Int\nf = 1\n",
            want: vec!["5:36 ProseFutureTense", "5:59 ProseMayPossibility"],
        },
        Case {
            name: "tag.ts",
            text: "// Use {@link x here, and it may help.\nexport const x = 1;\n",
            want: vec!["1:30 ProseMayPossibility"],
        },
        Case {
            name: "markup.hs",
            text: "module Markup where\n\n-- | It may help here.\n--\n-- >>> lookup 1 m\n-- Just 2\n--\n-- > lookup 1 m == Just 2   -- O(n)\n--\n-- prop> lookup 1 m == Just 2\n-- It may help here.\n--\n-- @\n-- lookup 1 m == Just 2   -- O(n)\n-- l '%%@=' f\n-- @\n-- It may help here.\n--\n-- Compares @a@ and @b@ and returns @Just -- x@. It may help here.\n-- Use 'Maybe' or 'Data.Maybe.fromMaybe', and it may help here.\n-- Mail a@b.org, and call read @Int here. It may help here.\nf :: Int\nf = 1\n\n-- | It runs.\n--\n-- >>> lookup 1 m\n-- @\n--\n-- It may help here.\n-- @since 1.0\ng :: Int\ng = 1\n",
            want: vec![
                "3:9 ProseMayPossibility",
                "11:7 ProseMayPossibility",
                "17:7 ProseMayPossibility",
                "19:53 ProseMayPossibility",
                "20:50 ProseMayPossibility",
                "21:46 ProseMayPossibility",
                "30:7 ProseMayPossibility",
            ],
        },
    ];
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    for case in cases {
        let ext = case
            .name
            .rsplit('.')
            .next()
            .expect("the name has an extension");
        let syntax = syntax(ext).expect("the table lists the extension");
        let got: Vec<String> = crate::lint_source(&mut rules, syntax, &[], case.text)
            .expect("the file parses")
            .into_iter()
            .map(|f| format!("{}:{} {}", f.line, f.col, f.rule))
            .collect();
        assert_eq!(got, case.want, "{}", case.name);
    }
}

#[test]
fn wrapped_phrase() {
    // A phrase that wraps across two comment lines matches in every
    // language of the table.
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    for syntax in SYNTAXES {
        let marker = match syntax.kind {
            "go" | "slash" => "//",
            "dash" => "--",
            _ => "#",
        };
        for ext in syntax.extensions {
            let open = if *ext == "php" { "<?php\n" } else { "" };
            let text =
                format!("{open}{marker} Run the job in order\n{marker} to warm the cache.\n");
            let line = 1 + usize::from(!open.is_empty());
            let col = marker.len() + 14;
            let got = positions(&mut rules, &format!("f.{ext}"), &text, &["ProseInOrderTo"]);
            assert_eq!(got, [format!("{line}:{col}")], "{ext}");
        }
    }
}

#[test]
fn haddock() {
    // A Haddock comment draws the findings of a plain comment, and a
    // phrase wraps across its lines.
    let text = "module M where\n\n-- | Run the job in order\n-- to warm the cache.\nf :: Int\nf = 1\n\ng = 2 -- ^ Run it in order to warm it.\n\n{-| Run it in order to warm it. -}\nh = 3\n";
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    assert_eq!(
        positions(&mut rules, "doc.hs", text, &["ProseInOrderTo"]),
        ["3:18", "8:19", "10:12"]
    );
}

#[test]
fn columns() {
    // A finding after a multi-byte character reports the column of its
    // character. A file with CRLF line endings reports the findings of
    // its LF copy.
    let text =
        "x = 1 // Set the caf\u{e9} \u{2014} and/or\n// the t\u{e9}a and/or the milk.\nfn f() {}\n";
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    for (name, text) in [("lf.rs", text.to_string()), ("crlf.rs", crlf(text))] {
        let got = positions(&mut rules, name, &text, &["ProseAndOr"]);
        assert_eq!(got, ["1:25", "2:12"], "{name}");
    }
}

#[test]
fn chunk_cache() {
    // The first comment is a setext heading, and the second is a
    // blockquote whose paragraph goes on in a lazy line of `=` marks.
    // Both hold the same source text from their first token to the
    // comment after them. Harper caches the lints of a paragraph by that
    // text. A paragraph break that spans the code between two comments
    // gives the second comment the lints of the heading, and those do
    // not hold a ProseDash finding.
    let text = "# The job runs --\n# ===\nx=1\n# The cache.\nx=1\n# > The job runs --\n# ===\nx=1\n# - The cache.\n";
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    assert_eq!(
        positions(&mut rules, "cache.sh", text, &["ProseDash"]),
        ["6:18"]
    );
}

#[test]
fn label_colons() {
    // A bold label that opens a comment line draws no ProseColonClause,
    // as in a markdown file. A second colon after the label still draws
    // one.
    struct Case {
        name: &'static str,
        file: &'static str,
        text: &'static str,
        want: Vec<&'static str>,
    }
    let cases = [
        Case {
            name: "label in a hash comment",
            file: "lab.sh",
            text: "# - **The parser is strict**: a stray tab fails the whole file.\n",
            want: vec![],
        },
        Case {
            name: "label in a slash comment",
            file: "lab.go",
            text: "package p\n\n// - **The parser is strict**: a stray tab fails the whole file.\nvar x = 1\n",
            want: vec![],
        },
        Case {
            name: "second colon after the label",
            file: "lab.sh",
            text: "# - **Parser**: the parser is strict: a stray tab fails the whole file.\n",
            want: vec!["1:21"],
        },
    ];
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    for case in cases {
        let got = positions(&mut rules, case.file, case.text, &["ProseColonClause"]);
        assert_eq!(got, case.want, "{}", case.name);
    }
}
