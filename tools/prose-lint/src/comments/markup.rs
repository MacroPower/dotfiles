//! The doc comment markup of some languages, which a region reads in
//! another way than markdown does.
//!
//! [`Markup::apply`] edits the lines of a comment before the region
//! joins them. Each character that the pass keeps holds the source
//! index of its own character, so a finding after the markup reports
//! the right column. The pass turns each code span of the markup into a
//! markdown code span of the same width. Such a span still reads as one
//! word of its sentence. The pass drops a block tag and the tag of a
//! block element, as the Javadoc items below state, and turns other
//! markup into blanks. The pass then drops each blank that opens a
//! line. Markup at the start of a paragraph then does not turn it into
//! an indented code block. A line that blanks fully reads as an empty
//! line and ends a paragraph.
//!
//! Javadoc and JSDoc comments hold four kinds of markup:
//!
//! - A block tag (`@param name`, `@returns`) opens a line of a block
//!   comment. Each tag line opens a paragraph, so a run of tags does not
//!   read as one sentence. The region drops the tag, its `{type}`, the
//!   name after a parameter tag, and a TSDoc `-` after them, as it drops
//!   a comment marker. When the tag fills its line, the region drops
//!   the indent of the next line too. Markdown then reads the
//!   description under the tag as prose, not as an indented code block.
//! - The tag of an HTML block element, such as `<p>`, `<li>`, or
//!   `</ul>`, can open a line of a doc comment, whose opener is `/**`.
//!   Markdown reads such a line as an HTML block that runs to the next
//!   blank line and hides its prose. So the region drops each such tag that opens the line,
//!   with the blanks around it, and the line opens a paragraph. When the
//!   tags fill their line, the region drops the indent of the next line
//!   too, as it does for a block tag. A `<pre>` tag stays, so its block
//!   still ends at `</pre>`. The tags of a line of an indented code
//!   block stay too, so an HTML sample in the block still reads as code.
//!   The tags of a plain `/* */` comment stay, so the markup that such a
//!   comment hides does not draw findings.
//! - An inline tag (`{@code x}`, `{@link Map label}`) on one line reads
//!   as a code span. An inline tag that no `}` on its line closes stays
//!   text.
//! - An HTML entity (`&lt;`) reads as blanks.
//!
//! Haddock comments hold four kinds:
//!
//! - A code block runs from a line that holds only `@` to the next line
//!   that opens with `@`, and the pass blanks it up to that `@`. Among
//!   the result lines of an example, a line that holds only `@` is a
//!   result and does not open a code block.
//! - An example (`>>> f 1`) reads as blank lines, and so do its result
//!   lines up to the next empty line. A property (`prop> f 1 == 1`) and a
//!   bird track (`> f 1`) read as blank lines too.
//! - Code between two `@` marks on one line (`@f x@`) reads as a code
//!   span. The opening `@` must not follow a letter or a digit, as in
//!   `a@b.org`. A letter or a digit must not follow the closing `@`, as
//!   in `read @Int`.
//! - An identifier in quotes (`'Maybe'`, `'Data.Map.lookup'`, `'(<$>)'`)
//!   reads as a code span.

/// One line of the text of a comment.
pub(super) struct Line {
    /// The source index and the character of each character of the
    /// text, without the marker.
    pub chars: Vec<(usize, char)>,
    /// Whether the line belongs to a block comment.
    pub block: bool,
    /// Whether the line belongs to a doc comment, whose opener is `/**`.
    pub doc: bool,
    /// Whether the line opens a paragraph, so that the region puts an
    /// empty line above it.
    pub opens_paragraph: bool,
}

/// The doc comment markup of a language.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) enum Markup {
    /// The comments hold markdown alone.
    None,
    /// Javadoc and JSDoc comments.
    Javadoc,
    /// Haddock comments.
    Haddock,
}

impl Markup {
    /// Edits the lines of one comment as the module doc states.
    pub fn apply(self, lines: &mut [Line]) {
        let source: Vec<Vec<(usize, char)>> = lines.iter().map(|l| l.chars.clone()).collect();
        match self {
            Self::None => return,
            Self::Javadoc => javadoc(lines),
            Self::Haddock => haddock(lines),
        }
        for (line, source) in lines.iter_mut().zip(&source) {
            drop_opening_blanks(&mut line.chars, source);
        }
    }
}

/// Drops the blanks of markup from the run of whitespace that opens
/// `text`, whose characters before the pass are `source`. Such blanks
/// indent the line, and markdown reads a paragraph that opens with an
/// indent of four columns as an indented code block. The pass drops the
/// run from the first blank of markup to the last one. That run also
/// holds the source blanks between two pieces of markup. The whitespace
/// of the source before and after the run stays.
fn drop_opening_blanks(text: &mut Vec<(usize, char)>, source: &[(usize, char)]) {
    let markup = |&(i, c): &(usize, char)| {
        c.is_whitespace() && source.iter().any(|&(j, s)| j == i && !s.is_whitespace())
    };
    let opening = text.iter().take_while(|(_, c)| c.is_whitespace()).count();
    let first = text[..opening].iter().position(markup);
    let last = text[..opening].iter().rposition(markup);
    if let (Some(first), Some(last)) = (first, last) {
        text.drain(first..=last);
    }
}

/// The block tags whose first word names a parameter or a property.
const NAMED: &[&str] = &["arg", "argument", "param", "prop", "property", "typeParam"];

fn javadoc(lines: &mut [Line]) {
    let mut bare = false;
    // Whether the source line above is empty or a line of an indented
    // code block. A line indented by four columns then reads as code.
    let mut code_may_follow = true;
    for (k, line) in lines.iter_mut().enumerate() {
        let code = code_may_follow && indent(&line.chars) >= 4;
        code_may_follow = code || line.chars.iter().all(|(_, c)| c.is_whitespace());
        if bare {
            let indent = line
                .chars
                .iter()
                .take_while(|(_, c)| c.is_whitespace())
                .count();
            line.chars.drain(..indent);
        }
        bare = false;
        if line.block
            && let Some(n) = block_tag(&line.chars)
        {
            line.chars.drain(..n);
            line.opens_paragraph = k > 0;
            bare = line.chars.is_empty();
        } else if line.doc
            && !code
            && let Some(n) = block_html(&line.chars)
        {
            line.chars.drain(..n);
            line.opens_paragraph = k > 0;
            bare = line.chars.is_empty();
        }
        blank_entities(&mut line.chars);
        inline_tags(&mut line.chars);
    }
}

/// Returns the column of the first character of `text` that is not
/// whitespace. A tab reaches the next multiple of four columns.
fn indent(text: &[(usize, char)]) -> usize {
    let mut col = 0;
    for &(_, c) in text {
        match c {
            '\t' => col = (col / 4 + 1) * 4,
            c if c.is_whitespace() => col += 1,
            _ => break,
        }
    }
    col
}

/// Returns the count of characters of the block tag that opens `text`.
/// The count holds the `{type}` of the tag, the name after a parameter
/// tag, a `-` after them, and the blanks before the text that follows.
/// Returns `None` when `text` does not open with a block tag.
fn block_tag(text: &[(usize, char)]) -> Option<usize> {
    let c = |i: usize| text.get(i).map(|&(_, c)| c);
    if c(0) != Some('@') || !c(1).is_some_and(|c| c.is_ascii_alphabetic()) {
        return None;
    }
    let blanks = |mut i: usize| {
        while c(i).is_some_and(char::is_whitespace) {
            i += 1;
        }
        i
    };
    let word = |mut i: usize| {
        while c(i).is_some_and(|c| !c.is_whitespace()) {
            i += 1;
        }
        i
    };
    let mut end = 1 + text[1..]
        .iter()
        .take_while(|(_, c)| c.is_ascii_alphanumeric())
        .count();
    let tag: String = text[1..end].iter().map(|&(_, c)| c).collect();
    let mut next = blanks(end);
    if c(next) == Some('{')
        && let Some(close) = closing_brace(text, next)
    {
        end = close + 1;
        next = blanks(end);
    }
    if NAMED.contains(&tag.as_str()) && next < text.len() {
        end = word(next);
        next = blanks(end);
    }
    if c(next) == Some('-') && c(next + 1).is_none_or(char::is_whitespace) {
        end = next + 1;
    }
    Some(blanks(end))
}

/// The names of the HTML elements whose tag, at the start of a line,
/// opens a CommonMark HTML block that runs to the next blank line.
/// `pre` is not one, so a `<pre>` block still ends at `</pre>`.
const BLOCK_ELEMENTS: &str = "address article aside base basefont blockquote body caption \
    center col colgroup dd details dialog dir div dl dt fieldset figcaption figure footer form \
    frame frameset h1 h2 h3 h4 h5 h6 head header hr html iframe legend li link main menu \
    menuitem nav noframes ol optgroup option p param search section summary table tbody td \
    tfoot th thead title tr track ul";

/// Returns the count of characters of the run of block element tags,
/// such as `<p>` or `</li>`, that opens `text`. The count holds the
/// blanks before each tag and after the last one. Returns `None` when
/// `text` does not open with such a tag.
fn block_html(text: &[(usize, char)]) -> Option<usize> {
    let c = |i: usize| text.get(i).map(|&(_, c)| c);
    let blanks = |mut i: usize| {
        while c(i).is_some_and(char::is_whitespace) {
            i += 1;
        }
        i
    };
    let mut end = None;
    let mut at = blanks(0);
    while c(at) == Some('<') {
        let start = at + 1 + usize::from(c(at + 1) == Some('/'));
        let name: String = text[start..]
            .iter()
            .map(|&(_, c)| c)
            .take_while(char::is_ascii_alphanumeric)
            .collect::<String>()
            .to_ascii_lowercase();
        let after = start + name.len();
        if !BLOCK_ELEMENTS.split_whitespace().any(|n| n == name)
            || !c(after).is_some_and(|c| c.is_whitespace() || c == '>' || c == '/')
        {
            break;
        }
        let Some(close) = text[after..].iter().position(|&(_, c)| c == '>') else {
            break;
        };
        at = blanks(after + close + 1);
        end = Some(at);
    }
    end
}

/// Returns the index of the `}` that closes the `{` at `open`, or
/// `None` when no `}` on the line closes it.
fn closing_brace(text: &[(usize, char)], open: usize) -> Option<usize> {
    let mut depth = 0;
    for (i, &(_, c)) in text.iter().enumerate().skip(open) {
        match c {
            '{' => depth += 1,
            '}' => {
                depth -= 1;
                if depth == 0 {
                    return Some(i);
                }
            }
            _ => {}
        }
    }
    None
}

/// Blanks each HTML entity of `text`, such as `&lt;`, `&#38;`, or
/// `&#x26;`.
fn blank_entities(text: &mut [(usize, char)]) {
    let mut i = 0;
    while i < text.len() {
        if text[i].1 == '&' {
            let name = text[i + 1..]
                .iter()
                .take_while(|(_, c)| c.is_ascii_alphanumeric() || *c == '#')
                .count();
            if name > 0 && text.get(i + 1 + name).map(|&(_, c)| c) == Some(';') {
                blank(&mut text[i..=i + 1 + name]);
                i += name + 2;
                continue;
            }
        }
        i += 1;
    }
}

/// Turns each inline tag of `text`, such as `{@code x}`, into a code
/// span.
fn inline_tags(text: &mut [(usize, char)]) {
    let mut i = 0;
    while i < text.len() {
        let opens = text[i].1 == '{'
            && text.get(i + 1).map(|&(_, c)| c) == Some('@')
            && text
                .get(i + 2)
                .is_some_and(|(_, c)| c.is_ascii_alphabetic());
        if opens && let Some(close) = closing_brace(text, i) {
            code_span(&mut text[i..=close]);
            i = close + 1;
        } else {
            i += 1;
        }
    }
}

fn haddock(lines: &mut [Line]) {
    let trimmed = |line: &Line| -> String {
        let text: String = line.chars.iter().map(|&(_, c)| c).collect();
        text.trim().to_string()
    };
    let mut example = false;
    let mut k = 0;
    while k < lines.len() {
        let text = trimmed(&lines[k]);
        if !example
            && text == "@"
            && let Some((last, at)) = code_block_end(&lines[k + 1..])
        {
            for line in &mut lines[k..=k + last] {
                blank(&mut line.chars);
            }
            blank(&mut lines[k + 1 + last].chars[..=at]);
            k += 1 + last;
            example = false;
            continue;
        }
        if text.is_empty() {
            example = false;
        } else if text.starts_with(">>>") {
            example = true;
        }
        if example || text.starts_with('>') || text.starts_with("prop>") {
            blank(&mut lines[k].chars);
        }
        k += 1;
    }
    for line in lines {
        haddock_spans(&mut line.chars);
    }
}

/// Returns the index in `lines` of the line that opens with the `@`
/// that closes a code block, and the index of that `@` in the line. The
/// lines open right under the `@` that opens the block. As in Haddock,
/// an `@` after other text of its line, such as the `@` of the operator
/// `'%@='`, does not close the block.
fn code_block_end(lines: &[Line]) -> Option<(usize, usize)> {
    lines.iter().enumerate().find_map(|(k, line)| {
        let at = line.chars.iter().position(|&(_, c)| !c.is_whitespace())?;
        (line.chars[at].1 == '@').then_some((k, at))
    })
}

/// Turns each Haddock code span (`@f x@`) and each identifier in quotes
/// (`'Maybe'`) of `text` into a markdown code span.
fn haddock_spans(text: &mut [(usize, char)]) {
    let c = |text: &[(usize, char)], i: usize| text.get(i).map(|&(_, c)| c);
    let word = |c: Option<char>| c.is_some_and(|c| c.is_alphanumeric() || c == '_');
    let mut i = 0;
    while i < text.len() {
        let before = i.checked_sub(1).and_then(|j| c(text, j));
        let close = match text[i].1 {
            '@' if !word(before)
                && before != Some('\\')
                && c(text, i + 1).is_some_and(|c| !c.is_whitespace()) =>
            {
                text[i + 2..]
                    .iter()
                    .enumerate()
                    .position(|(j, &(_, c))| c == '@' && text[i + 1 + j].1 != '\\')
                    .map(|p| i + 2 + p)
                    .filter(|&end| !word(c(text, end + 1)))
            }
            '\'' if !word(before) && before != Some('\'') => identifier(text, i),
            _ => None,
        };
        match close {
            Some(end) => {
                code_span(&mut text[i..=end]);
                i = end + 1;
            }
            None => i += 1,
        }
    }
}

/// Returns the index of the quote that closes the Haddock identifier
/// whose opening quote sits at `open`, or `None` when no identifier
/// opens there. An identifier is a name, which can hold quotes and
/// dots, or an operator in parentheses. A letter or a digit must not
/// follow the closing quote.
fn identifier(text: &[(usize, char)], open: usize) -> Option<usize> {
    let rest = &text[open + 1..];
    let first = rest.first()?.1;
    let ends = |i: usize| {
        rest.get(i + 1)
            .is_none_or(|&(_, c)| !(c.is_alphanumeric() || c == '_'))
    };
    let close = if first == '(' {
        let paren = rest
            .iter()
            .position(|&(_, c)| c == ')' || c.is_whitespace())?;
        (rest[paren].1 == ')' && rest.get(paren + 1).map(|&(_, c)| c) == Some('\''))
            .then_some(paren + 1)
    } else if first.is_alphabetic() || first == '_' {
        let run = rest
            .iter()
            .take_while(|&&(_, c)| c.is_alphanumeric() || matches!(c, '_' | '\'' | '.'))
            .count();
        (1..run).rev().find(|&i| rest[i].1 == '\'' && ends(i))
    } else {
        None
    }?;
    ends(close).then_some(open + 1 + close)
}

/// Turns `span`, whose first and last characters are its delimiters,
/// into a markdown code span. Each backtick inside the span becomes a
/// blank, so the backtick does not end the span. The span still reads
/// as one word of its sentence.
fn code_span(span: &mut [(usize, char)]) {
    for (_, c) in span.iter_mut() {
        if *c == '`' {
            *c = ' ';
        }
    }
    let last = span.len() - 1;
    span[0].1 = '`';
    span[last].1 = '`';
}

/// Turns each character of `text` into a blank.
fn blank(text: &mut [(usize, char)]) {
    for (_, c) in text {
        *c = ' ';
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn apply() {
        struct Case {
            name: &'static str,
            markup: Markup,
            lines: &'static [&'static str],
            want: &'static str,
        }
        let cases = [
            Case {
                name: "block tags",
                markup: Markup::Javadoc,
                lines: &[
                    "Adds two numbers.",
                    "@param a the first",
                    "@param {number} b - the second",
                    "@returns the sum, which",
                    "  wraps",
                    "@throws Error when it fails",
                ],
                want: "Adds two numbers.\n\nthe first\n\nthe second\n\nthe sum, which\n  wraps\n\nError when it fails",
            },
            Case {
                name: "tags that fill their lines",
                markup: Markup::Javadoc,
                lines: &[
                    "Gets it.",
                    "@return",
                    "    the value, which",
                    "    wraps",
                    "@param foo",
                    "\tthe foo",
                ],
                want: "Gets it.\n\n\nthe value, which\n    wraps\n\n\nthe foo",
            },
            Case {
                name: "tag that opens the comment",
                markup: Markup::Javadoc,
                lines: &["@param a the first"],
                want: "the first",
            },
            Case {
                name: "block element tags",
                markup: Markup::Javadoc,
                lines: &[
                    "Gets it.",
                    "<p>It runs.</p>",
                    "  <UL class=\"x\">",
                    "  <li> One <b>item</b>",
                    "</li><li/>Two",
                    "<pre>",
                    "<p><pre>x",
                    "<b>Bold</b> or <para>x",
                ],
                want: "Gets it.\n\nIt runs.</p>\n\n\n\nOne <b>item</b>\n\nTwo\n<pre>\n\n<pre>x\n<b>Bold</b> or <para>x",
            },
            Case {
                name: "block element tags that fill their lines",
                markup: Markup::Javadoc,
                lines: &["<ul>", "  <li>", "    It runs.", "    It wraps.", "  </li>"],
                want: "\n\n\nIt runs.\n    It wraps.\n\n",
            },
            Case {
                name: "block element tags in an indented code block",
                markup: Markup::Javadoc,
                lines: &[
                    "    <p>x",
                    "Gets it.",
                    "",
                    "    <div>",
                    "      <p>It runs.</p>",
                    "",
                    "\t</div>",
                    "<ul>",
                    "    <li>One",
                ],
                want: "    <p>x\nGets it.\n\n    <div>\n      <p>It runs.</p>\n\n\t</div>\n\n\n\nOne",
            },
            Case {
                name: "inline tags and entities",
                markup: Markup::Javadoc,
                lines: &[
                    "Use {@code Map<K, {V}>} if a &lt; b, as {@link Foo e.g. here} does.",
                    "Use {@link x here.",
                ],
                want: "Use `@code Map<K, {V}>` if a      b, as `@link Foo e.g. here` does.\nUse {@link x here.",
            },
            Case {
                name: "inline tag with a backtick",
                markup: Markup::Javadoc,
                lines: &["Use {@code `x`} here."],
                want: "Use `@code  x ` here.",
            },
            Case {
                name: "javadoc blanks that open a line",
                markup: Markup::Javadoc,
                lines: &[
                    "Compares them.",
                    "@return &lt; 0, if it may help",
                    "&lt;&lt; is a shift",
                    "  &gt; x",
                ],
                want: "Compares them.\n\n 0, if it may help\n is a shift\n   x",
            },
            Case {
                name: "haddock blanks that open a line",
                markup: Markup::Haddock,
                lines: &["@'sortBy' ``on``@ it may help", "  @", "  @ and @g@ here."],
                want: "`'sortBy'   on  ` it may help\n  \n   and `g` here.",
            },
            Case {
                name: "haddock code block",
                markup: Markup::Haddock,
                lines: &["It runs.", "@", "f '%@=' -- y", "@ and @g@ here.", "Done."],
                want: "It runs.\n\n\n and `g` here.\nDone.",
            },
            Case {
                name: "haddock code block without a closer",
                markup: Markup::Haddock,
                lines: &["It runs.", "@", "f x"],
                want: "It runs.\n@\nf x",
            },
            Case {
                name: "haddock example with an @ result",
                markup: Markup::Haddock,
                lines: &[">>> f 1", "@", "", "It runs.", "@since 1.0"],
                want: "\n\n\nIt runs.\n@since 1.0",
            },
            Case {
                name: "haddock examples and bird tracks",
                markup: Markup::Haddock,
                lines: &[
                    ">>> f 1",
                    "Just 2",
                    "",
                    "prop> f x == x",
                    "It runs.",
                    "> g 1",
                    "It ends.",
                ],
                want: "\n\n\n\nIt runs.\n\nIt ends.",
            },
            Case {
                name: "haddock spans",
                markup: Markup::Haddock,
                lines: &[
                    "Use @f x@ and 'Maybe', 'foldl'', 'Data.Map.lookup', or '(<$>)'.",
                    "Mail a@b.org, call read @Int, and don't 'quote it'.",
                    "Use @\\@since@ and let s0\\@x be y@ here.",
                ],
                want: "Use `f x` and `Maybe`, `foldl'`, `Data.Map.lookup`, or `(<$>)`.\nMail a@b.org, call read @Int, and don't 'quote it'.\nUse `\\@since` and let s0\\@x be y@ here.",
            },
            Case {
                name: "no markup",
                markup: Markup::None,
                lines: &["@param a {@code x} &lt;"],
                want: "@param a {@code x} &lt;",
            },
        ];
        for case in cases {
            let mut lines: Vec<Line> = case
                .lines
                .iter()
                .map(|l| Line {
                    chars: l.chars().enumerate().collect(),
                    block: true,
                    doc: true,
                    opens_paragraph: false,
                })
                .collect();
            case.markup.apply(&mut lines);
            let got: Vec<String> = lines
                .iter()
                .map(|l| {
                    let text: String = l.chars.iter().map(|&(_, c)| c).collect();
                    if l.opens_paragraph {
                        format!("\n{text}")
                    } else {
                        text
                    }
                })
                .collect();
            assert_eq!(got.join("\n"), case.want, "{}", case.name);
        }
    }

    #[test]
    fn plain_block_comment_keeps_html() {
        // A `/* */` comment that is not a doc comment often holds markup
        // that the code no longer uses, so its tags stay.
        let mut lines = vec![Line {
            chars: "<p>It runs.</p>".chars().enumerate().collect(),
            block: true,
            doc: false,
            opens_paragraph: false,
        }];
        Markup::Javadoc.apply(&mut lines);
        let got: String = lines[0].chars.iter().map(|&(_, c)| c).collect();
        assert_eq!(got, "<p>It runs.</p>");
    }
}
