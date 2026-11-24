//! The tests of the comment regions.
//!
//! The fixture tests hold the cases of source comments that the tests
//! of the bash prose-lint stated a finding on, before this crate
//! replaced it. A comment above each list of positions names each place
//! where the crate reads a comment in another way than the bash script.

use std::fs;
use std::path::Path;

use super::*;
use crate::rules::Rules;

/// The directory of the crate, which holds `rules/` and `fixtures/`.
const CRATE_DIR: &str = env!("CARGO_MANIFEST_DIR");

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
    let dir = Path::new(CRATE_DIR).join("fixtures/comment-code");
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

/// The position of each ProseLatinAbbreviation and ProseMayPossibility
/// finding in `fixtures/comment-code/spans.sh`.
const SPANS: &[&str] = &[
    "10:13", "10:29", "13:17", "13:32", "16:18", "16:33", "19:15", "19:30", "24:30", "29:19",
    "29:34", "32:34", "34:21", "34:37", "36:21", "36:37", "39:21", "39:37", "41:21", "41:37",
    "50:25", "50:35", "52:21", "52:37", "55:18", "55:33", "59:21", "59:37", "62:23", "62:39",
    "68:21", "68:37", "74:21", "74:37", "76:21", "76:37", "83:21", "83:37", "89:21", "89:37",
    "95:21", "95:37", "97:21", "97:37", "105:21", "105:37", "107:21", "107:37", "115:21", "115:37",
    "117:21", "117:37", "120:20", "120:35", "124:17", "124:32", "126:21", "126:37", "132:45",
    "132:55", "135:48", "135:58", "138:16", "138:32", "141:16", "141:32", "145:21", "145:37",
    "153:21", "153:37", "155:21", "155:37", "159:21", "159:37", "161:21", "161:37", "163:28",
    "163:38", "164:21", "164:37", "167:51", "167:61", "170:51", "170:61", "173:51", "173:61",
    "177:42", "177:52", "180:53", "180:63", "183:53", "183:63", "186:51", "186:61", "189:42",
    "189:52", "192:53", "192:63", "195:51", "195:61", "198:18", "198:34", "201:19", "201:35",
    "206:19", "206:35", "209:20", "209:36", "212:20", "212:36", "216:23", "216:39", "224:21",
    "224:37", "227:16", "227:32", "233:21", "233:37", "238:21", "238:37", "242:21", "242:37",
    "246:21", "246:37", "251:51", "251:61", "255:21", "255:37", "258:45", "258:55", "261:21",
    "261:37", "265:23", "265:39", "268:23", "268:39", "270:28", "271:22", "271:32", "276:21",
    "276:37", "284:21", "284:37", "288:13", "288:29", "292:19", "292:34", "296:16", "296:31",
    "300:21", "300:36", "304:15", "304:31", "309:19", "309:29", "314:19", "314:29", "319:16",
    "319:26", "325:21", "325:37", "332:21", "332:36", "339:16", "339:26", "344:16", "344:26",
    "349:16", "349:26", "355:16", "355:26", "360:20", "360:30", "364:18", "364:28", "368:42",
    "368:52", "372:9", "372:19", "372:45", "372:55", "375:48", "375:58", "380:16", "380:26",
    "386:21", "386:37", "393:21", "393:36", "400:16", "400:32", "408:20", "408:36", "413:23",
    "413:38", "420:23", "420:38", "425:16", "425:26", "429:19", "429:34", "435:42", "435:52",
    "439:6", "439:16", "439:42", "439:52", "444:42", "444:52", "451:21", "451:36", "458:16",
    "458:32", "464:13", "464:29", "469:19", "469:34", "476:19", "476:34", "479:25", "479:41",
    "482:19", "482:35", "485:18", "485:34", "488:21", "488:37", "493:15", "493:25", "498:16",
    "498:26", "505:18", "505:28", "509:17", "509:27", "516:21", "516:37", "522:21", "522:37",
    "527:21", "527:37", "533:25", "533:41", "543:23", "543:39",
];

#[test]
fn code_spans() {
    // The comments of spans.sh hold code spans that wrap onto the next
    // line, runs of backticks that no span closes, and fenced code
    // blocks. Some blocks sit in lists or in blockquotes, and some have
    // no closing line. Each comment holds an `e.g.` and a `may` inside
    // code and after it, so each finding names the text after a span or
    // a block. The test also lints the comments as an awk file and, with
    // `//` markers, as a Go file.
    //
    // In one comment, a `--` line sits above a line of indented code that
    // opens a span. The region reads the `--` line as a frame, so a blank
    // line precedes the code. The span then stays inside the code, and
    // the crate reports the `e.g.` and the `may` of the next line. The
    // bash script read the `--` as text, so the span ran from the code
    // into the next line.
    let path = Path::new(CRATE_DIR).join("fixtures/comment-code/spans.sh");
    let text = fs::read_to_string(path).expect("reading the fixture");
    let go: String = text
        .lines()
        .map(|line| match line.strip_prefix('#') {
            Some(rest) => format!("//{rest}\n"),
            None if line == "x = 1" => "var x = 1\n".to_string(),
            None => format!("{line}\n"),
        })
        .collect();
    let names = ["ProseLatinAbbreviation", "ProseMayPossibility"];
    let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
    assert_eq!(positions(&mut rules, "spans.sh", &text, &names), SPANS);
    assert_eq!(positions(&mut rules, "spans.awk", &text, &names), SPANS);
    assert_eq!(
        positions(&mut rules, "spans.go", &go, &names),
        shifted(SPANS, 1)
    );
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
    let dir = Path::new(CRATE_DIR).join("fixtures/license");
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
    let dir = Path::new(CRATE_DIR).join("fixtures/crlf");
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
    // `will` in lead.hs. A paragraph or a list item that opens with a
    // tag such as `<p>` or `<li>` draws findings, but a `<pre>` block
    // stays code. So does an HTML sample in an indented code block of
    // card.ts. In Bare.java, the indented prose under a tag that fills
    // its line draws findings too. In Table.java, the cells and the
    // caption of a table do not draw a finding, even across an empty
    // line, but the prose after `</table>` does. In Code.java, a table
    // tag in an inline tag does not open a table, and neither does a
    // self-closing `<table/>`. In grid.tsx, a table tag in a code span or
    // a fenced code block does not open one either. In Pre.java, a table
    // tag in a `<pre>` block does not open a table. In inline.ts, a
    // `<table>` tag opens a table only when a `</table>` closes it. The
    // prose after each of these tags draws findings. The tags in a plain
    // `/* */` comment of c.tsx and c.js stay, so the markup that they
    // hide does not draw a finding.
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
            name: "Html.java",
            text: "class Html {\n  /**\n   * It may help here.\n   *\n   * <p>It may help here.\n   * It may help here.</p>\n   * <ul>\n   *   <li>It may help here.\n   * </ul>\n   * <pre>\n   * It may help here.\n   * </pre>\n   */\n  int a;\n}\n",
            want: vec![
                "3:9 ProseMayPossibility",
                "5:12 ProseMayPossibility",
                "6:9 ProseMayPossibility",
                "8:15 ProseMayPossibility",
            ],
        },
        Case {
            name: "Bare.java",
            text: "class Bare {\n  /**\n   * Gets it.\n   *\n   * <p>\n   *     It may help here.\n   * <ul>\n   *   <li>\n   *       It may help here.\n   *   </li>\n   * </ul>\n   */\n  int a;\n}\n",
            want: vec!["6:13 ProseMayPossibility", "9:15 ProseMayPossibility"],
        },
        Case {
            name: "Table.java",
            text: "class Table {\n  /**\n   * It may help here.\n   * <table border=\"1\">\n   *  <caption>It may help here.</caption>\n   *  <tr>\n   *   <td>It may help here.</td>\n   *\n   *   <td>true/false</td>\n   *  </tr>\n   * </table>\n   *     It may help here.\n   */\n  int a;\n}\n",
            want: vec!["3:9 ProseMayPossibility", "12:13 ProseMayPossibility"],
        },
        Case {
            name: "Code.java",
            text: "class Code {\n  /**\n   * Use {@code <table>} for the rows.\n   * It may help here.\n   */\n  int a;\n\n  /**\n   * Self closing <table/> here.\n   * It may help here.\n   */\n  int b;\n}\n",
            want: vec!["4:9 ProseMayPossibility", "10:9 ProseMayPossibility"],
        },
        Case {
            name: "grid.tsx",
            text: "/**\n * Renders the grid.\n *\n * ```tsx\n * <Table rows={rows} />\n * open(<table>);\n * ```\n *\n * It may help here.\n */\nexport const grid = 1;\n\n/**\n * Wraps the `<table>` element. It may help here.\n */\nexport const wrap = 2;\n",
            want: vec!["9:7 ProseMayPossibility", "14:36 ProseMayPossibility"],
        },
        Case {
            name: "Pre.java",
            text: "class Pre {\n  /**\n   * Parses the markup.\n   * <pre>{@code\n   * if (tag.equals(\"<table>\")) {\n   *   open();\n   * }\n   * }</pre>\n   * It may help here.\n   */\n  int a;\n}\n",
            want: vec!["9:9 ProseMayPossibility"],
        },
        Case {
            name: "inline.ts",
            text: "/**\n * Returns the <table> element of the grid. It may help here.\n *\n * It may help here.\n */\nexport const inline = 1;\n",
            want: vec!["2:48 ProseMayPossibility", "4:7 ProseMayPossibility"],
        },
        Case {
            name: "card.ts",
            text: "/**\n * Renders the card, and it may help here.\n *\n * Example:\n *\n *     <div class=\"card\">\n *       <p>It may help here.</p>\n *     </div>\n *\n * Done.\n */\nexport const card = 1;\n",
            want: vec!["2:29 ProseMayPossibility"],
        },
        Case {
            name: "c.tsx",
            text: "export function C() {\n  return (\n    <div>\n      {/*\n      <div className=\"card\">\n        <p>This section is going to be shown when it may help.</p>\n      </div>\n      */}\n      {/* <p>This section is going to be shown when it may help.</p> */}\n    </div>\n  );\n}\n",
            want: vec![],
        },
        Case {
            name: "c.js",
            text: "function f() {\n  /*\n  <ul>\n  <li>Old item that may be needed later</li>\n  </ul>\n  */\n  return 1;\n}\n",
            want: vec![],
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
fn code_cache() {
    // A code block at the end of a comment holds the text of a sentence
    // of prose. Harper caches the lints of a sentence by its characters,
    // whatever its tokens are. The parser adds the next character of the
    // source to the token of the code block. Without that character, the
    // code block shares the key of the sentence. The code then gets the
    // lints of a sentence above it, and a sentence below it gets the
    // empty lints of the code.
    let cases = [
        (
            "prose first",
            "cache.sh",
            "# The job restarts, and it may stop.\nx = 1\n# Run it like this:\n#\n#     ```sh\n# ```\n# The job restarts, and it may stop.\nx = 1\n# Or:\n#\n#     The job restarts, and it may stop.\nx = 1\n",
            "1:28",
        ),
        (
            "code first",
            "cache.sh",
            "# Run it like this:\n#\n#     The job restarts, and it may stop.\nx = 1\n# The job restarts, and it may stop.\nx = 1\n",
            "5:28",
        ),
        (
            "code first in a block comment",
            "cache.go",
            "package p\n\n/*\n * Run it like this:\n *\n *     The job restarts, and it may stop. */\nvar x = 1\n\n// The job restarts, and it may stop.\nvar y = 1\n",
            "9:29",
        ),
        (
            "code first before a closer with no blank",
            "cache.go",
            "package p\n\n/*\n * Run it like this:\n *\n *     The job restarts, and it may stop.*/\nvar x = 1\n\n// The job restarts, and it may stop.\nvar y = 1\n",
            "9:29",
        ),
    ];
    for (name, file, text, want) in cases {
        let mut rules = Rules::new(&[]).expect("every embedded rule compiles");
        assert_eq!(
            positions(&mut rules, file, text, &["ProseMayPossibility"]),
            [want],
            "{name}"
        );
    }
}

#[test]
fn label_colons() {
    // A bold label that opens a comment line does not draw a
    // ProseColonClause finding, as in a markdown file. A second colon
    // after the label still draws one.
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
