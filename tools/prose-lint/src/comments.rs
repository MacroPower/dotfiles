//! The comments of a source file, as markdown regions.
//!
//! [`SYNTAXES`] maps each extension of a source file to the way that
//! the binary finds its comments. The table is also the list of the
//! extensions of source files that the binary lints. A tree-sitter
//! grammar finds the comments of most languages. Each node whose kind
//! holds `comment` is one comment, as in harper-comments 3.3.0, whose
//! grammar versions the crate takes. Each Haskell Haddock comment
//! (`-- |`, `-- ^`, `{-|`) is one too, and its region drops the `|` or
//! `^` after the marker. Python, HCL, YAML, Weir, and awk have no such
//! grammar, so the scanner of `scan.rs` finds their full-line `#`
//! comments, Python docstrings, and HCL descriptions.
//!
//! A run of line comments on adjacent lines that open their lines is
//! one comment, and so is each block comment. A line comment that
//! follows code is a comment of its own, because it describes its line.
//! The comment under it often describes the next line. When its text
//! ends in a `--` dash outside a banner, its sentence goes on, so a line
//! comment that opens the next line joins it. A line
//! comment whose marker another character follows, such as a Go
//! directive (`//go:build`), a LuaLS annotation (`---@param`), or a
//! shebang, is not prose and ends the run. In a JavaScript or a
//! TypeScript file, an eslint directive
//! (`// eslint-disable-line no-console -- reason`) and a TypeScript
//! directive (`// @ts-expect-error - reason`) are not prose either, so
//! the text of such a comment starts after the separator before the
//! reason. In a block comment, the directive can span lines, and the
//! eslint configuration comments (`/* eslint eqeqeq: "off" -- reason */`,
//! `/* global name */`) are directives too. eslint reads none of its
//! directives in a block comment that opens with `/**` or `/*!`, but
//! TypeScript reads its own there.
//!
//! [`Comments::new`] builds one region for each comment. The region
//! holds the text of each line without its marker and one space after
//! it. A `*` that opens a later line of a block comment is a marker in
//! two places. Javadoc puts it at most one column right of the `/` of
//! the opener. scalafmt puts it under the `*` of a `*/` that opens the
//! last line. Any other `*` opens a list item, as in the doc comments of
//! nixpkgs. The other lines of a block lose the indent that all of them
//! share. A line of a comment that holds only `-` marks, two or more,
//! frames the comment, so the region holds it as an empty line.
//! Markdown reads such a line as the underline of a heading.
//!
//! The doc comments of Java, JavaScript, TypeScript, and Haskell hold
//! markup that markdown misreads, such as the `@param` tags of Javadoc
//! and the `>>>` examples of Haddock. The pass of `markup.rs` drops block
//! tags and, in a `/**` comment, the tags of HTML block elements, such
//! as `<p>`, and the HTML tables. It turns other markup into code spans
//! and blanks before the region joins the lines.
//!
//! [`RegionParser`] lints the regions of a file as one Harper document,
//! and [`Comments::license`] names the lines of the license header,
//! which the binary does not lint.

mod markup;
mod scan;

use std::fmt;
use std::ops::{Range, RangeInclusive};
use std::sync::LazyLock;

use harper_core::parsers::Parser;
use harper_core::{Span, Token, TokenKind};
use regex::Regex;
use tree_sitter::Language;

use self::markup::{Line, Markup};
use crate::guards;
use crate::markdown::HarperMarkdown;
use crate::region::Region;

/// An error of finding the comments of a file.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The grammar does not load into tree-sitter.
    #[error("loading the grammar of .{0} files")]
    Grammar(&'static str, #[source] tree_sitter::LanguageError),
    /// tree-sitter does not return a tree.
    #[error("parsing the file as .{0}")]
    Parse(&'static str),
}

/// How the binary finds the comments of a file kind.
#[derive(Clone, Copy)]
enum Finder {
    /// Each node of the grammar whose kind holds `comment`, and each
    /// Haddock node, is a comment.
    Grammar(fn() -> Language),
    /// The scanner finds each full-line comment.
    Scan(scan::Dialect),
}

/// The comment syntax of the source files of some extensions.
pub struct Syntax {
    /// The extensions of the files, without the dot.
    pub extensions: &'static [&'static str],
    /// The kind that `comment-checks.txt` of the crate names, such as
    /// `go` or `slash`.
    pub kind: &'static str,
    finder: Finder,
    markup: Markup,
    /// Whether the comments can hold eslint and TypeScript directives.
    directives: bool,
}

impl Syntax {
    const fn grammar(
        extensions: &'static [&'static str],
        kind: &'static str,
        language: fn() -> Language,
    ) -> Self {
        Self {
            extensions,
            kind,
            finder: Finder::Grammar(language),
            markup: Markup::None,
            directives: false,
        }
    }

    const fn scan(
        extensions: &'static [&'static str],
        kind: &'static str,
        dialect: scan::Dialect,
    ) -> Self {
        Self {
            extensions,
            kind,
            finder: Finder::Scan(dialect),
            markup: Markup::None,
            directives: false,
        }
    }

    /// Returns the syntax with the doc comment markup `markup`.
    const fn markup(self, markup: Markup) -> Self {
        Self { markup, ..self }
    }

    /// Returns the syntax whose comments can hold eslint and
    /// TypeScript directives.
    const fn directives(self) -> Self {
        Self {
            directives: true,
            ..self
        }
    }
}

impl PartialEq for Syntax {
    fn eq(&self, other: &Self) -> bool {
        self.extensions == other.extensions
    }
}

impl Eq for Syntax {}

impl fmt::Debug for Syntax {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "Syntax({})", self.extensions.join(", "))
    }
}

/// The comment syntax of each extension of a source file.
pub const SYNTAXES: &[Syntax] = &[
    Syntax::grammar(&["sh", "bash"], "hash", || {
        tree_sitter_bash::LANGUAGE.into()
    }),
    Syntax::grammar(&["ex", "exs"], "hash", || {
        tree_sitter_elixir::LANGUAGE.into()
    }),
    Syntax::grammar(&["nix"], "hash", || tree_sitter_nix::LANGUAGE.into()),
    Syntax::grammar(&["ps1"], "hash", || tree_sitter_powershell::LANGUAGE.into()),
    Syntax::grammar(&["rb"], "hash", || tree_sitter_ruby::LANGUAGE.into()),
    Syntax::grammar(&["toml"], "hash", || tree_sitter_toml_ng::LANGUAGE.into()),
    Syntax::scan(&["awk", "weir"], "hash", scan::Dialect::Hash),
    Syntax::scan(&["hcl", "tf", "tfvars"], "hcl", scan::Dialect::Hcl),
    Syntax::scan(&["py", "pyi"], "py", scan::Dialect::Python),
    Syntax::scan(&["yaml", "yml"], "yaml", scan::Dialect::Yaml),
    Syntax::grammar(&["go"], "go", || tree_sitter_go::LANGUAGE.into()),
    Syntax::grammar(&["c"], "slash", || tree_sitter_c::LANGUAGE.into()),
    Syntax::grammar(&["cpp", "h"], "slash", || tree_sitter_cpp::LANGUAGE.into()),
    Syntax::grammar(&["cs"], "slash", || tree_sitter_c_sharp::LANGUAGE.into()),
    Syntax::grammar(&["dart"], "slash", || {
        harper_tree_sitter_dart::LANGUAGE.into()
    }),
    Syntax::grammar(&["gleam"], "slash", || tree_sitter_gleam::LANGUAGE.into()),
    Syntax::grammar(&["groovy", "gradle"], "slash", || {
        tree_sitter_groovy::LANGUAGE.into()
    }),
    Syntax::grammar(&["java"], "slash", || tree_sitter_java::LANGUAGE.into())
        .markup(Markup::Javadoc),
    Syntax::grammar(&["js"], "slash", || tree_sitter_javascript::LANGUAGE.into())
        .markup(Markup::Javadoc)
        .directives(),
    Syntax::grammar(&["jsx", "tsx"], "slash", || {
        tree_sitter_typescript::LANGUAGE_TSX.into()
    })
    .markup(Markup::Javadoc)
    .directives(),
    Syntax::grammar(&["kt", "kts"], "slash", || {
        tree_sitter_kotlin_ng::LANGUAGE.into()
    }),
    Syntax::grammar(&["php"], "slash", || tree_sitter_php::LANGUAGE_PHP.into()),
    Syntax::grammar(&["rs"], "slash", || tree_sitter_rust::LANGUAGE.into()),
    Syntax::grammar(&["scala"], "slash", || tree_sitter_scala::LANGUAGE.into()),
    Syntax::grammar(&["sol"], "slash", || tree_sitter_solidity::LANGUAGE.into()),
    Syntax::grammar(&["swift"], "slash", || tree_sitter_swift::LANGUAGE.into()),
    Syntax::grammar(&["ts"], "slash", || {
        tree_sitter_typescript::LANGUAGE_TYPESCRIPT.into()
    })
    .markup(Markup::Javadoc)
    .directives(),
    Syntax::grammar(&["zig"], "slash", || tree_sitter_zig::LANGUAGE.into()),
    Syntax::grammar(&["hs"], "dash", || tree_sitter_haskell::LANGUAGE.into())
        .markup(Markup::Haddock),
    Syntax::grammar(&["lua"], "dash", || tree_sitter_lua::LANGUAGE.into()),
];

/// Returns the comment syntax of the files with the extension `ext`.
pub fn syntax(ext: &str) -> Option<&'static Syntax> {
    SYNTAXES.iter().find(|s| s.extensions.contains(&ext))
}

/// A comment, or other prose, that a finder returns. Each range holds
/// the indexes of characters in the source.
#[derive(Debug, PartialEq, Eq)]
enum Found {
    /// A comment with its markers.
    Comment(Range<usize>),
    /// Lines of prose with no marker, such as an HCL `description`.
    Text(Vec<Range<usize>>),
}

/// One line of the text of a comment.
#[derive(Debug)]
struct TextLine {
    /// The source line, counted from 1.
    line: usize,
    /// The indexes in the source of the text, without the marker.
    text: Range<usize>,
    /// The mark of the marker of a line comment and the count of its
    /// marks, such as `('/', 2)` for `//`.
    marker: Option<(char, usize)>,
}

/// One comment of a file.
#[derive(Debug)]
struct Comment {
    /// The first and the last source line of the comment.
    span: RangeInclusive<usize>,
    /// The lines of text of the comment.
    lines: Vec<TextLine>,
    /// Whether code follows the comment on its last line.
    code_after: bool,
    /// Whether the comment is a doc comment, whose opener is `/**`.
    doc: bool,
}

/// The regions of the comments of one source file.
pub struct Comments {
    /// The region of each comment, in the order of the source.
    pub regions: Vec<Region>,
    /// The lines of the license header of the file, if it has one.
    pub license: Option<RangeInclusive<usize>>,
}

impl Comments {
    /// Finds the comments of `text`, a file of `syntax`.
    pub fn new(syntax: &Syntax, text: &str) -> Result<Self, Error> {
        let chars: Vec<char> = text.chars().collect();
        let file = Region::markdown(text);
        let found = match syntax.finder {
            Finder::Grammar(language) => grammar(syntax, language(), text)?,
            Finder::Scan(dialect) => scan::find(dialect, &chars),
        };
        let mut comments = group(&chars, &file, found, syntax.directives);
        let license = license(&chars, &comments);
        if let Some(header) = &license {
            for comment in &mut comments {
                comment.lines.retain(|l| !header.contains(&l.line));
            }
        }
        let regions = comments
            .iter()
            .filter(|c| !c.lines.is_empty())
            .map(|c| region(&chars, &file, c, syntax.markup))
            .collect();
        Ok(Self { regions, license })
    }
}

/// Returns each comment that the grammar finds in `text`. In a Nix
/// file, each line of an indented string that opens with `#` counts as
/// a comment too. Such lines mostly hold the comments of a shell script.
fn grammar(syntax: &Syntax, language: Language, text: &str) -> Result<Vec<Found>, Error> {
    let ext = syntax.extensions[0];
    let mut parser = tree_sitter::Parser::new();
    parser
        .set_language(&language)
        .map_err(|e| Error::Grammar(ext, e))?;
    let tree = parser.parse(text, None).ok_or(Error::Parse(ext))?;

    let mut bytes = Vec::new();
    let mut stack = vec![tree.root_node()];
    while let Some(node) = stack.pop() {
        if node.kind().contains("comment") || node.kind() == "haddock" {
            bytes.push((node.byte_range(), true));
            continue;
        }
        if node.kind() == "indented_string_expression" {
            bytes.push((node.byte_range(), false));
        }
        let mut cursor = node.walk();
        stack.extend(node.children(&mut cursor));
    }

    // The index of the character at each byte offset of the text.
    let starts: Vec<usize> = text.char_indices().map(|(i, _)| i).collect();
    let char_at = |byte: usize| starts.partition_point(|&s| s < byte);
    let chars: Vec<char> = text.chars().collect();
    let mut found = Vec::new();
    for (range, comment) in bytes {
        let range = char_at(range.start)..char_at(range.end);
        if comment {
            found.push(Found::Comment(range));
            continue;
        }
        for line in lines(&chars, range.clone()).skip(1) {
            let lead = skip_blanks(&chars, line.clone());
            if chars.get(lead) == Some(&'#') && lead < line.end {
                found.push(Found::Comment(lead..line.end));
            }
        }
    }
    found.sort_by_key(|f| match f {
        Found::Comment(r) => r.start,
        Found::Text(lines) => lines.first().map_or(0, |l| l.start),
    });
    Ok(found)
}

/// Returns the range of each line of `range`, without its line ending.
/// A line ending at the end of the range does not open another line,
/// and an empty range holds one empty line.
fn lines(chars: &[char], range: Range<usize>) -> impl Iterator<Item = Range<usize>> + '_ {
    let mut at = Some(range.start);
    std::iter::from_fn(move || {
        let start = at?;
        let end = chars[start..range.end]
            .iter()
            .position(|&c| c == '\n')
            .map_or(range.end, |i| start + i);
        at = Some(end + 1).filter(|&next| next < range.end);
        let body = if end > start && chars[end - 1] == '\r' {
            end - 1
        } else {
            end
        };
        Some(start..body)
    })
}

/// Returns the index of the first character of `range` that is not a
/// space or a tab, or the end of the range.
fn skip_blanks(chars: &[char], range: Range<usize>) -> usize {
    chars[range.clone()]
        .iter()
        .position(|&c| c != ' ' && c != '\t')
        .map_or(range.end, |i| range.start + i)
}

/// Returns `range` without the whitespace at its end.
fn trim_end(chars: &[char], range: Range<usize>) -> Range<usize> {
    let end = chars[range.clone()]
        .iter()
        .rposition(|c| !c.is_whitespace())
        .map_or(range.start, |i| range.start + i + 1);
    range.start..end
}

/// Returns the index of the first character of the line that holds the
/// character at `index`.
fn line_start(chars: &[char], index: usize) -> usize {
    chars[..index]
        .iter()
        .rposition(|&c| c == '\n')
        .map_or(0, |i| i + 1)
}

/// Returns the length of the opener and of the closer of the block
/// comment `text`, or `None` for a line comment.
fn delimiters(text: &[char]) -> Option<(usize, usize)> {
    let ends = |close: &[char]| text.len() >= 2 + close.len() && text.ends_with(close);
    let (open, close) = match text {
        ['/', '*', ..] => {
            let marks = text[2..]
                .iter()
                .take_while(|&&c| c == '*' || c == '!')
                .count();
            (2 + marks, if ends(&['*', '/']) { 2 } else { 0 })
        }
        ['{', '-', rest @ ..] => {
            // The `|` or `^` of a Haddock comment (`{-|`, `{- |`) is
            // part of the opener.
            let open = match rest {
                ['|' | '^', ..] => 3,
                [' ', '|' | '^', ' ' | '\t' | '\n' | '\r', ..] => 4,
                _ => 2,
            };
            (open, if ends(&['-', '}']) { 2 } else { 0 })
        }
        ['<', '#', ..] => (2, if ends(&['#', '>']) { 2 } else { 0 }),
        ['-', '-', '[', rest @ ..] => {
            let level = rest.iter().take_while(|&&c| c == '=').count();
            if rest.get(level) != Some(&'[') {
                return None;
            }
            let close: Vec<char> = std::iter::once(']')
                .chain(std::iter::repeat_n('=', level))
                .chain(std::iter::once(']'))
                .collect();
            (4 + level, if ends(&close) { close.len() } else { 0 })
        }
        _ => return None,
    };
    Some((open.min(text.len() - close), close))
}

/// Returns the count of the marks of the marker of the line comment at
/// `line` and the range of its text, or `None` when the line is not
/// prose. A marker repeats one mark, such as `#`, `##`, `//`, `///`, or
/// `---`, and `//!` is a marker too. A space or a tab must follow it.
/// The text starts after one space, and a tab stays in the text, where
/// it opens a code block. A directive such as `//nolint:gosec` is not
/// prose, but the reason after a second ` // ` on its line is. When
/// `directives` is set, so is the reason of an eslint or a TypeScript
/// directive. Such a directive without a reason stays in its run with
/// no text. The `|` or `^` that marks a Haddock comment (`-- |`) is
/// part of the marker.
fn line_text(
    chars: &[char],
    line: Range<usize>,
    directives: bool,
) -> Option<(usize, Range<usize>)> {
    let mark = *chars.get(line.start)?;
    let marks = chars[line.clone()]
        .iter()
        .take_while(|&&c| c == mark)
        .count();
    match mark {
        '#' => {}
        '/' | '-' if marks >= 2 => {}
        _ => return None,
    }
    let mut at = line.start + marks;
    if mark == '/' && chars[at..line.end].first() == Some(&'!') {
        at += 1;
    }
    match chars[at..line.end].first() {
        None | Some('\t') => {}
        Some(' ') => {
            at += 1;
            let haddock = mark == '-'
                && marks == 2
                && matches!(
                    chars[at..line.end],
                    ['|' | '^'] | ['|' | '^', ' ' | '\t', ..]
                );
            if haddock {
                at += 1;
                at += usize::from(chars[at..line.end].first() == Some(&' '));
            }
        }
        Some(_) => {
            let rest: String = chars[at..line.end].iter().collect();
            let reason = rest.find(" // ").filter(|_| mark == '/')?;
            at += rest[..reason].chars().count() + 4;
        }
    }
    let found = directives
        .then(|| {
            directive(
                &LINE_DIRECTIVE,
                chars,
                skip_blanks(chars, at..line.end)..line.end,
            )
        })
        .flatten();
    match found {
        Some(end) if trim_end(chars, end..line.end).is_empty() => Some((marks, line.end..line.end)),
        Some(end) => Some((marks, end..line.end)),
        None => Some((marks, at..line.end)),
    }
}

/// Returns the pattern of a directive comment whose eslint directives
/// are the alternatives of `eslint`, or that holds only TypeScript
/// directives when `eslint` is `None`. An eslint directive runs to the
/// `--` before its reason, and a TypeScript directive takes the `-`,
/// `--`, or `:` after it.
fn directive_pattern(eslint: Option<&str>) -> Regex {
    let eslint = eslint.map_or_else(String::new, |e| {
        format!(r"(?:{e})(?:\s.*?)??(?:\s-{{2,}}(?:\s+|$)|$)|")
    });
    Regex::new(&format!(
        r"(?s)^(?:{eslint}@ts-(?:expect-error|ignore|nocheck|check)(?:\s*(?:-+|:)\s*|\s+|$))"
    ))
    .expect("the pattern is valid")
}

/// The directives of a line comment, such as
/// `eslint-disable-next-line no-console -- reason`.
static LINE_DIRECTIVE: LazyLock<Regex> =
    LazyLock::new(|| directive_pattern(Some(r"eslint-(?:disable(?:-next-line|-line)?|enable)")));

/// The directives of a block comment, which add the eslint
/// configuration comments, such as `eslint eqeqeq: "off" -- reason`
/// and `global name`. eslint reads those only in a block comment.
static BLOCK_DIRECTIVE: LazyLock<Regex> = LazyLock::new(|| {
    directive_pattern(Some(
        r"eslint(?:-disable(?:-next-line|-line)?|-enable)?|globals?|exported",
    ))
});

/// The directives of a block comment whose opener holds more marks
/// than `/*`, such as `/**` or `/*!`. TypeScript reads its directives
/// there, but eslint reads none of its own.
static DOC_DIRECTIVE: LazyLock<Regex> = LazyLock::new(|| directive_pattern(None));

/// Returns the index where the reason of the directive comment whose
/// text is `text` starts, or `None` when the text does not open with a
/// directive. An eslint directive
/// (`eslint-disable-next-line no-console -- reason`) and a TypeScript
/// directive (`@ts-expect-error - reason`) are not prose, but the
/// reason after each is.
fn directive(pattern: &Regex, chars: &[char], text: Range<usize>) -> Option<usize> {
    let line: String = chars[text.clone()].iter().collect();
    let found = pattern.find(&line)?;
    Some(text.start + line[..found.end()].chars().count())
}

/// Returns the lines of text of the block comment at `range`, whose
/// opener and closer hold `open` and `close` characters. When
/// `directives` is set, the text starts at the reason of an eslint or a
/// TypeScript directive that opens the comment.
fn block_lines(
    chars: &[char],
    file: &Region,
    range: Range<usize>,
    (open, close): (usize, usize),
    directives: bool,
) -> Vec<TextLine> {
    let inner = range.start + open..range.end - close;
    let open_col = range.start - line_start(chars, range.start);
    // The column of the `*` of a `*/` that opens the last line.
    let closer = range.end - close;
    let close_col = (close == 2 && chars[range.start] == '/')
        .then(|| closer - line_start(chars, closer))
        .filter(|&col| {
            chars[closer - col..closer]
                .iter()
                .all(|&c| c == ' ' || c == '\t')
        });
    let stars = chars[range.start] == '/';

    let lines: Vec<Range<usize>> = lines(chars, inner.clone()).collect();
    // Whether each line opens with a `*` marker.
    let marked: Vec<bool> = lines
        .iter()
        .enumerate()
        .map(|(k, l)| {
            let lead = skip_blanks(chars, l.clone());
            let col = lead - l.start;
            k > 0
                && stars
                && lead < l.end
                && chars[lead] == '*'
                && (col <= open_col + 1 || Some(col) == close_col)
        })
        .collect();
    let indent = lines
        .iter()
        .zip(&marked)
        .skip(1)
        .filter(|(l, m)| !**m && skip_blanks(chars, (*l).clone()) < l.end)
        .map(|(l, _)| skip_blanks(chars, l.clone()) - l.start)
        .min()
        .unwrap_or(0);

    // The start of the text after a directive that opens the comment.
    let body = inner.start
        + chars[inner.clone()]
            .iter()
            .take_while(|c| c.is_whitespace())
            .count();
    let pattern = if open == 2 {
        &BLOCK_DIRECTIVE
    } else {
        &DOC_DIRECTIVE
    };
    let reason = directives
        .then(|| directive(pattern, chars, body..inner.end))
        .flatten()
        .unwrap_or(inner.start);

    let mut out = Vec::new();
    for (k, (l, m)) in lines.iter().zip(&marked).enumerate() {
        if l.end < reason {
            continue;
        }
        let start = if k == 0 {
            l.start + usize::from(chars[l.clone()].first() == Some(&' '))
        } else if *m {
            let star = skip_blanks(chars, l.clone()) + 1;
            star + usize::from(chars[star..l.end].first() == Some(&' '))
        } else {
            (l.start + indent).min(skip_blanks(chars, l.clone()))
        };
        let text = trim_end(chars, start.max(reason)..l.end);
        let blank = text.is_empty();
        if blank && (k == 0 || k + 1 == lines.len()) {
            continue;
        }
        out.push(TextLine {
            line: file.position(l.start).line,
            text,
            marker: None,
        });
    }
    out
}

/// Turns what a finder returns into comments, and joins each run of
/// line comments on adjacent lines into one comment.
fn group(chars: &[char], file: &Region, found: Vec<Found>, directives: bool) -> Vec<Comment> {
    let line_of = |i: usize| file.position(i).line;
    let mut comments: Vec<Comment> = Vec::new();
    // Whether the last comment is a run that a line comment can join.
    let mut open = false;
    let mut seen = 0;
    for found in found {
        match found {
            Found::Text(lines) => {
                let (Some(first), Some(last)) = (lines.first(), lines.last()) else {
                    continue;
                };
                comments.push(Comment {
                    span: line_of(first.start)..=line_of(last.end),
                    lines: lines
                        .iter()
                        .map(|l| TextLine {
                            line: line_of(l.start),
                            text: trim_end(chars, l.clone()),
                            marker: None,
                        })
                        .collect(),
                    code_after: false,
                    doc: false,
                });
                open = false;
            }
            Found::Comment(range) => {
                if range.start < seen {
                    continue;
                }
                seen = range.end;
                if let Some(delims) = delimiters(&chars[range.clone()]) {
                    let end = lines(chars, range.end..chars.len())
                        .next()
                        .unwrap_or(range.end..range.end);
                    comments.push(Comment {
                        span: line_of(range.start)..=line_of(range.end.saturating_sub(1)),
                        lines: block_lines(chars, file, range.clone(), delims, directives),
                        code_after: skip_blanks(chars, end.clone()) < end.end,
                        doc: chars[range.clone()].starts_with(&['/', '*', '*']),
                    });
                    open = false;
                    continue;
                }
                for line in lines(chars, range) {
                    // A Haddock node holds the indent of its later lines.
                    let line = skip_blanks(chars, line.clone())..line.end;
                    let Some((marks, text)) = line_text(chars, line.clone(), directives) else {
                        open = false;
                        continue;
                    };
                    let n = line_of(line.start);
                    let opens_line =
                        skip_blanks(chars, line_start(chars, line.start)..line.start) == line.start;
                    let joins = open
                        && opens_line
                        && comments.last().is_some_and(|c| *c.span.end() + 1 == n);
                    if !joins {
                        comments.push(Comment {
                            span: n..=n,
                            lines: Vec::new(),
                            code_after: false,
                            doc: false,
                        });
                    }
                    let text = TextLine {
                        line: n,
                        text: trim_end(chars, text),
                        marker: Some((chars[line.start], marks)),
                    };
                    open = opens_line || dangles(chars, &text);
                    let comment = comments.last_mut().expect("a comment was pushed above");
                    comment.span = *comment.span.start()..=n;
                    comment.lines.push(text);
                }
            }
        }
    }
    comments
}

/// Whether the text of `line` ends in a `--` dash outside a banner.
/// The sentence of such a line goes on in the comment line under it.
fn dangles(chars: &[char], line: &TextLine) -> bool {
    chars[line.text.clone()].ends_with(&['-', '-']) && unframed(chars, line) == line.text
}

static LICENSE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"Copyright|SPDX-License-Identifier|Licensed (under|to)|license that can be found")
        .expect("the pattern is valid")
});

/// Returns the lines of the license header of the file, whose text
/// must stay verbatim, so no finding on it can be fixed.
///
/// The header opens with the first comment of the file, after any
/// blank lines and a shebang, when one of its lines names a copyright
/// or a license. Each comment right under the header joins it when one
/// of its own lines also names one. So a description in a block comment
/// under a `// SPDX-License-Identifier` line stays out of the header.
/// Code after the closer of a block comment ends the header.
///
/// A banner is a line of a run without text whose marker holds more
/// marks than the marker of the first line of the run. A line of `#`
/// marks under `# Copyright` is one. A banner ends the header, and the
/// lines of the run after it are prose. A run that opens with a banner
/// of two or more `#` marks or three or more `-` marks is not a header,
/// so a description under the banner draws findings.
fn license(chars: &[char], comments: &[Comment]) -> Option<RangeInclusive<usize>> {
    let mut first = 1;
    for (n, line) in lines(chars, 0..chars.len()).enumerate() {
        let text: String = chars[line].iter().collect();
        if !(text.trim().is_empty() || (n == 0 && text.starts_with("#!"))) {
            first = n + 1;
            break;
        }
    }
    let mut end: Option<usize> = None;
    for comment in comments {
        if *comment.span.start() != end.map_or(first, |e| e + 1) {
            break;
        }
        let mut lines = &comment.lines[..];
        let mut banner = false;
        if let Some((mark, marks)) = comment.lines.first().and_then(|l| l.marker) {
            if comment.lines[0].text.is_empty() && marks >= if mark == '#' { 2 } else { 3 } {
                break;
            }
            if let Some(b) = lines
                .iter()
                .position(|l| l.text.is_empty() && l.marker.is_some_and(|(_, n)| n > marks))
            {
                lines = &lines[..b];
                banner = true;
            }
        }
        let named = lines.iter().any(|l| {
            let text: String = chars[l.text.clone()].iter().collect();
            LICENSE.is_match(&text)
        });
        if !named {
            break;
        }
        end = Some(if banner {
            lines.last().map_or(*comment.span.start(), |l| l.line)
        } else {
            *comment.span.end()
        });
        if banner || comment.code_after {
            break;
        }
    }
    end.map(|e| first..=e)
}

/// Returns the region of `comment`, after `markup` edits its lines. A
/// line break joins its lines, and each line break sits at the line
/// ending of the source line above. Two line breaks sit there above a
/// line that opens a paragraph.
fn region(chars: &[char], file: &Region, comment: &Comment, markup: Markup) -> Region {
    let mut lines: Vec<Line> = comment
        .lines
        .iter()
        .map(|line| Line {
            chars: unframed(chars, line).map(|i| (i, chars[i])).collect(),
            block: line.marker.is_none(),
            doc: comment.doc,
            opens_paragraph: false,
        })
        .collect();
    markup.apply(&mut lines);
    let mut text = Vec::new();
    for (k, line) in lines.into_iter().enumerate() {
        if k > 0 {
            let above = &comment.lines[k - 1];
            let newline = chars[above.text.end..]
                .iter()
                .position(|&c| c == '\n')
                .map_or(above.text.end, |i| above.text.end + i);
            text.push((newline, '\n'));
            if line.opens_paragraph {
                text.push((newline, '\n'));
            }
        }
        text.extend(line.chars);
    }
    Region::part(file, text)
}

/// Returns the range of the text of `line` without the runs of `-`
/// marks that frame it. A line that holds only `-` marks, two or more,
/// frames the comment, and so does a banner, whose text opens with three
/// `-` marks or with two and a blank (`--- Section ---`). A line whose
/// marker holds three or more `-` marks is a banner too when its text
/// ends in three or more of them, such as a Lua `--- Section ---` line.
/// A line that opens with a flag such as `--force` is not a banner.
fn unframed(chars: &[char], line: &TextLine) -> Range<usize> {
    let text = &chars[line.text.clone()];
    let lead = text.iter().take_while(|&&c| c == '-').count();
    let tail = text.iter().rev().take_while(|&&c| c == '-').count();
    let dashes = line.marker.is_some_and(|(mark, n)| mark == '-' && n >= 3);
    let banner = lead == text.len() && lead >= 2
        || lead >= 3
        || (lead == 2 && matches!(text.get(2), Some(' ' | '\t')))
        || (dashes && tail >= 3);
    if !banner {
        return line.text.clone();
    }
    let start = line.text.start + lead;
    let end = (line.text.end - tail).max(start);
    let start = skip_blanks(chars, start..end);
    trim_end(chars, start..end)
}

/// A Harper parser that reads the regions of a source file. It parses
/// the text of each region with [`HarperMarkdown`] and moves each token
/// to the source position of its characters. The text holds each curly
/// apostrophe between two letters as a straight one, as
/// [`guards::straighten`] gives the rules the text of a file. A paragraph break
/// sits between two regions, so a phrase does not run from one comment
/// into the next.
///
/// The break is zero characters wide, and it sits at the end of the
/// last token of the region above. Harper caches the lints of a
/// paragraph by the source text from its first token to its last one.
/// A break that spans the code between two comments puts the text of the
/// comment after its last token into that key. A setext heading and a
/// paragraph with a lazy line of `=` marks then share a key, but Harper
/// reads other tokens in each.
///
/// Harper caches the lints of a sentence by its text in the same way.
/// The text of a region does not hold a newline after its last line. So
/// a code block at the end of a region can hold the text of a sentence of
/// prose. The parser moves the end of that code block past the next
/// character of the source, such as a newline, a blank, or the `*` of a
/// closer. So the two share a key only when the code ends the file.
pub struct RegionParser<'a> {
    regions: &'a [Region],
}

impl<'a> RegionParser<'a> {
    /// Returns the parser of `regions`, which hold characters of the
    /// text that the parser reads.
    pub fn new(regions: &'a [Region]) -> Self {
        Self { regions }
    }
}

impl Parser for RegionParser<'_> {
    fn parse(&self, source: &[char]) -> Vec<Token> {
        let mut tokens = Vec::new();
        for (i, region) in self.regions.iter().enumerate() {
            let indices = region.indices();
            let Some(&last) = indices.last() else {
                continue;
            };
            if let Some(end) = tokens
                .last()
                .filter(|t: &&Token| t.kind != TokenKind::ParagraphBreak)
                .map(|t| t.span.end)
            {
                tokens.push(Token::new(Span::new(end, end), TokenKind::ParagraphBreak));
            }
            let chars: Vec<char> = guards::straighten(&region.text).chars().collect();
            for mut token in (HarperMarkdown {
                front_matter: region.front_matter,
            })
            .parse(&chars)
            {
                let (s, e) = (token.span.start, token.span.end);
                let start = indices.get(s).copied().unwrap_or(last + 1);
                let end = if e > s { indices[e - 1] + 1 } else { start };
                token.span = Span::new(start, end.max(start));
                tokens.push(token);
            }
            // The next region starts at the first character that the
            // token must not reach.
            let limit = self.regions[i + 1..]
                .iter()
                .find_map(|r| r.indices().first().copied())
                .unwrap_or(source.len());
            if let Some(code) = tokens
                .last_mut()
                .filter(|t| t.kind == TokenKind::Unlintable && t.span.end < limit)
            {
                code.span.end += 1;
            }
        }
        tokens
    }
}

#[cfg(test)]
mod tests;
