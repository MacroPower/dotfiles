//! The markdown model that the native checks read.
//!
//! [`Markdown::new`] parses a region with pulldown-cmark, the markdown
//! parser of harper-core. It passes the options that harper-core
//! passes. The model holds the blocks of the region and the prose of
//! each of its lines. A check reads each line as the source wrote it,
//! with its list markers, `>` marks, and emphasis marks. The model
//! blanks the text that is not prose to spaces:
//!
//! - Each line that does not hold inline text of a paragraph, a heading,
//!   a table, or a list item. Such lines hold code blocks, HTML blocks,
//!   front matter, link reference definitions, thematic breaks, and
//!   blank lines.
//! - Code spans, inline math, and inline HTML. The text of a `code`,
//!   `kbd`, `samp`, or `pre` element counts as inline HTML too.
//! - Link destinations, from the `]` that closes the text of a link to
//!   the end of the link, and each autolink.
//!
//! Blanking writes one space for each character, so each character
//! keeps its index in the region, and through the region its source
//! position. Each region starts as a new document, so a block that one
//! region leaves open does not reach the next region.

use std::ops::Range;

use pulldown_cmark::{Event, LinkType, Options, Parser, Tag, TagEnd};
use regex::Regex;

use crate::region::Region;

/// The kind of a block of the model.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum BlockKind {
    /// A paragraph.
    Paragraph,
    /// An ATX or setext heading, with its level from 1 to 6.
    Heading(u8),
    /// A list item.
    ListItem,
    /// A block quote.
    BlockQuote,
    /// A table.
    Table,
    /// A fenced or indented code block.
    CodeBlock,
}

/// A block of the model. Container blocks, such as a list item, come
/// before the blocks that they hold.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Block {
    /// The kind of the block.
    pub kind: BlockKind,
    /// The indexes of the characters of the block in the region.
    pub chars: Range<usize>,
    /// The indexes of the lines of the block in [`Markdown::lines`].
    pub lines: Range<usize>,
}

/// One line of a region.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Line {
    /// The prose of the line, without its line ending. Each character
    /// that is not prose is a space.
    pub text: String,
    /// The index in the region of the first character of the line.
    pub start: usize,
}

impl Line {
    /// Returns the index in the region of the character at the byte
    /// offset `offset` of `text`. `text` is the text of the line, or a
    /// copy of it that [`blank`] changed.
    pub fn at(&self, text: &str, offset: usize) -> usize {
        self.start + text[..offset].chars().count()
    }
}

/// The blocks and the lines of one region.
pub struct Markdown {
    /// The blocks of the region, in the order of their first character.
    #[allow(
        dead_code,
        reason = "the blocks serve the checks of paragraphs and sentences"
    )]
    pub blocks: Vec<Block>,
    /// The lines of the region, in order.
    pub lines: Vec<Line>,
}

impl Markdown {
    /// Parses the markdown text of `region`.
    pub fn new(region: &Region) -> Self {
        let text = region.text.as_str();
        let options = Options::all().difference(Options::ENABLE_SMART_PUNCTUATION);
        let mut prose = vec![false; text.len()];
        let mut hidden: Vec<Range<usize>> = Vec::new();
        let mut blocks: Vec<(BlockKind, Range<usize>)> = Vec::new();
        // The depth of the code blocks and front matter that the parser is
        // in, whose text is not prose.
        let mut raw = 0;
        // The start of an open `code`, `kbd`, `samp`, or `pre` element,
        // and its closing tag.
        let mut element: Option<(usize, String)> = None;
        // The range of each open link, and the end of its text so far.
        let mut links: Vec<(LinkType, Range<usize>, usize)> = Vec::new();

        for (event, range) in Parser::new_ext(text, options).into_offset_iter() {
            if let Some(link) = links.last_mut()
                && !matches!(event, Event::End(TagEnd::Link | TagEnd::Image))
            {
                link.2 = link.2.max(range.end);
            }
            match event {
                Event::Start(tag) => {
                    let kind = match tag {
                        Tag::Paragraph => Some(BlockKind::Paragraph),
                        Tag::Heading { level, .. } => Some(BlockKind::Heading(level as u8)),
                        Tag::Item => Some(BlockKind::ListItem),
                        Tag::BlockQuote(_) => Some(BlockKind::BlockQuote),
                        Tag::Table(_) => Some(BlockKind::Table),
                        Tag::CodeBlock(_) => Some(BlockKind::CodeBlock),
                        _ => None,
                    };
                    if let Some(kind) = kind {
                        blocks.push((kind, range.clone()));
                    }
                    match tag {
                        Tag::CodeBlock(_) | Tag::MetadataBlock(_) => raw += 1,
                        Tag::Link { link_type, .. } | Tag::Image { link_type, .. } => {
                            // The text starts after `[` or `![`.
                            let open = text[range.start..].find('[').map_or(0, |i| i + 1);
                            links.push((link_type, range.clone(), range.start + open));
                        }
                        Tag::Emphasis
                        | Tag::Strong
                        | Tag::Strikethrough
                        | Tag::Superscript
                        | Tag::Subscript
                            if raw == 0 =>
                        {
                            prose[range].fill(true);
                        }
                        _ => {}
                    }
                }
                Event::End(end) => match end {
                    TagEnd::CodeBlock | TagEnd::MetadataBlock(_) => raw -= 1,
                    TagEnd::Link | TagEnd::Image => {
                        if let Some((link_type, range, text_end)) = links.pop() {
                            prose[range.clone()].fill(true);
                            if matches!(link_type, LinkType::Autolink | LinkType::Email) {
                                hidden.push(range);
                            } else {
                                hidden.push(text_end..range.end);
                            }
                        }
                    }
                    TagEnd::Emphasis
                    | TagEnd::Strong
                    | TagEnd::Strikethrough
                    | TagEnd::Superscript
                    | TagEnd::Subscript => {}
                    // An element ends with the paragraph, the heading, the
                    // cell, or the item that holds it.
                    _ => element = None,
                },
                Event::Text(_) | Event::FootnoteReference(_) | Event::TaskListMarker(_) => {
                    if raw == 0 {
                        prose[range].fill(true);
                    }
                }
                Event::SoftBreak | Event::HardBreak => {}
                Event::Code(_) | Event::InlineMath(_) | Event::DisplayMath(_) => {
                    prose[range.clone()].fill(true);
                    hidden.push(range);
                }
                Event::InlineHtml(html) => {
                    prose[range.clone()].fill(true);
                    let tag = html.to_ascii_lowercase();
                    match &element {
                        Some((start, close)) if tag.starts_with(close.as_str()) => {
                            hidden.push(*start..range.end);
                            element = None;
                        }
                        Some(_) => {}
                        None => {
                            element = ["code", "kbd", "samp", "pre"]
                                .into_iter()
                                .find(|name| {
                                    tag.strip_prefix('<')
                                        .and_then(|t| t.strip_prefix(name))
                                        .is_some_and(|t| t.starts_with(['>', ' ', '\t', '\n']))
                                })
                                .map(|name| (range.start, format!("</{name}")));
                        }
                    }
                    hidden.push(range);
                }
                Event::Html(_) | Event::Rule => {}
            }
        }

        let mut blanked = vec![false; text.len()];
        for range in hidden {
            blanked[range].fill(true);
        }

        // The index of the character at each byte of the text.
        let mut char_at = vec![0; text.len() + 1];
        let mut lines = Vec::new();
        let mut count = 0;
        for source in text.split_inclusive('\n') {
            let offset = source.as_ptr() as usize - text.as_ptr() as usize;
            let body = source.strip_suffix('\n').unwrap_or(source);
            let body = body.strip_suffix('\r').unwrap_or(body);
            let bytes = offset..offset + body.len();
            let has_prose = prose[bytes.clone()].contains(&true);
            let line_text = body
                .char_indices()
                .map(|(i, c)| {
                    if has_prose && !blanked[offset + i] {
                        c
                    } else {
                        ' '
                    }
                })
                .collect();
            lines.push(Line {
                text: line_text,
                start: count,
            });
            for (i, c) in source.char_indices() {
                char_at[offset + i..offset + i + c.len_utf8()].fill(count);
                count += 1;
            }
        }
        char_at[text.len()] = count;

        let line_of = |index: usize| lines.partition_point(|l| l.start <= index).max(1) - 1;
        let blocks = blocks
            .into_iter()
            .map(|(kind, range)| {
                // A list item takes the blank lines after it, which the
                // block leaves out.
                let end = range.start + text[range.clone()].trim_end().len();
                let chars = char_at[range.start]..char_at[end];
                let last = chars.end.max(chars.start + 1) - 1;
                Block {
                    kind,
                    lines: line_of(chars.start)..line_of(last) + 1,
                    chars,
                }
            })
            .collect();
        Self { blocks, lines }
    }
}

/// Returns `text` with each match of `re` blanked to spaces, one space
/// for each character.
pub fn blank(text: &str, re: &Regex) -> String {
    re.replace_all(text, |c: &regex::Captures<'_>| {
        " ".repeat(c[0].chars().count())
    })
    .into_owned()
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Returns the prose of each line of `text`, without the spaces at
    /// its end.
    fn prose(text: &str) -> Vec<String> {
        Markdown::new(&Region::markdown(text))
            .lines
            .into_iter()
            .map(|line| line.text.trim_end().to_string())
            .collect()
    }

    #[test]
    fn lines() {
        struct Case {
            name: &'static str,
            text: &'static str,
            want: Vec<&'static str>,
        }
        let cases = [
            Case {
                name: "markers stay",
                text: "# Plan\n\n- An item.\n> A quote.\n",
                want: vec!["# Plan", "", "- An item.", "> A quote."],
            },
            Case {
                name: "fenced code",
                text: "Run it.\n\n```sh\nmake build\n```\nAfter.",
                want: vec!["Run it.", "", "", "", "", "After."],
            },
            Case {
                name: "fence in tildes in a blockquote",
                text: "> ~~~\n> make build\n\nThe job runs nightly, e.g. at noon.\n",
                want: vec!["", "", "", "The job runs nightly, e.g. at noon."],
            },
            Case {
                name: "fence marker four columns in that continues a paragraph",
                text: "Restart the pod\n    ~~~\nto clear the cache.\n",
                want: vec!["Restart the pod", "    ~~~", "to clear the cache."],
            },
            Case {
                name: "code span in three backticks opens no fence",
                text: "```x``` opens no fence here.\nThe job restarts.\n",
                want: vec!["        opens no fence here.", "The job restarts."],
            },
            Case {
                name: "unclosed fence ends with its list item",
                text: "- ```sh\n  make build\n\nThe job runs.\n",
                want: vec!["", "", "", "The job runs."],
            },
            Case {
                name: "indented code after a blank line",
                text: "Run it.\n\n    make build\n",
                want: vec!["Run it.", "", ""],
            },
            Case {
                name: "indented code after a thematic break",
                text: "Run it.\n\n***\n    make build\n",
                want: vec!["Run it.", "", "", ""],
            },
            Case {
                name: "indented code after a heading",
                text: "# Setup\n    make build\n",
                want: vec!["# Setup", ""],
            },
            Case {
                name: "indented code in a blockquote",
                text: "> Run it.\n>\n>     make build\n",
                want: vec!["> Run it.", "", ""],
            },
            Case {
                name: "indented code in a list item",
                text: "- Run it.\n\n      make build\n",
                want: vec!["- Run it.", "", ""],
            },
            Case {
                name: "paragraph of a list item",
                text: "- Run it.\n\n    Then wait.\n",
                want: vec!["- Run it.", "", "    Then wait."],
            },
            Case {
                name: "indented code after spaces and a tab",
                text: "Run it.\n\n  \tmake build\n",
                want: vec!["Run it.", "", ""],
            },
            Case {
                name: "code span that wraps",
                text: "Match on `Some(x)\n=> x` to unwrap it.\n",
                want: vec!["Match on", "      to unwrap it."],
            },
            Case {
                name: "code span that wraps onto a line with a pipe",
                text: "It may help with `x\n| y` to restart the pod.\n",
                want: vec!["It may help with", "     to restart the pod."],
            },
            Case {
                name: "run of backticks that no span closes",
                text: "A lone ` mark.\n\nThe next ` mark.\n",
                want: vec!["A lone ` mark.", "", "The next ` mark."],
            },
            Case {
                name: "HTML comment block",
                text: "Run it.\n\n<!-- a\nb -->\nAfter.\n",
                want: vec!["Run it.", "", "", "", "After."],
            },
            Case {
                name: "HTML comment that its list item ends",
                text: "- The pod holds the cache.\n\n  <!-- note\nThe pod holds the cache e.g. in memory.\n  -->\n",
                want: vec![
                    "- The pod holds the cache.",
                    "",
                    "",
                    "The pod holds the cache e.g. in memory.",
                    "  -->",
                ],
            },
            Case {
                name: "HTML comment that its blockquote ends",
                text: "> <!-- note\nThe pod holds the cache e.g. in memory.\n> -->\n",
                want: vec!["", "The pod holds the cache e.g. in memory.", "> -->"],
            },
            Case {
                name: "inline HTML",
                text: "Press <kbd>Ctrl</kbd> or <b>a</b> <!-- b --> now.\n",
                want: vec!["Press                 or    a                now."],
            },
            Case {
                name: "link destinations",
                text: "Read [the guide](https://example.com \"Guide\") or <https://example.com>.\n",
                want: vec![
                    "Read [the guide                               or                      .",
                ],
            },
            Case {
                name: "link reference definitions",
                text: "Run it.\n\n[guide]: https://example.com\n   \"The guide\"\n[api]: https://example.com 'The API'\n[guide]: https://example.org\n",
                want: vec!["Run it.", "", "", "", "", ""],
            },
            Case {
                name: "link reference definition inside a paragraph",
                text: "The pod holds the cache.\n[x]: https://example.com\nIt runs on the node.\n",
                want: vec![
                    "The pod holds the cache.",
                    "[x]: https://example.com",
                    "It runs on the node.",
                ],
            },
            Case {
                name: "front matter",
                text: "---\ntitle: Guide\n---\n\nRun it.\n",
                want: vec!["", "", "", "", "Run it."],
            },
            Case {
                name: "CRLF line endings",
                text: "Run `make`.\r\n\r\n```\r\nx\r\n```\r\nAfter.\r\n",
                want: vec!["Run       .", "", "", "", "", "After."],
            },
        ];
        for case in cases {
            assert_eq!(prose(case.text), case.want, "{}", case.name);
        }
    }

    #[test]
    fn regions_start_fresh() {
        // A fence that one region leaves open does not reach the next
        // region.
        assert_eq!(prose("```sh\nmake build\n"), ["", ""]);
        assert_eq!(prose("The pod runs.\n"), ["The pod runs."]);
    }

    #[test]
    fn positions() {
        let region = Region::markdown("A\r\n- `x` \u{e9} b\n");
        let model = Markdown::new(&region);
        let line = &model.lines[1];
        let text = &line.text;
        let at = line.at(text, text.find('b').expect("the line holds b"));
        assert_eq!(
            region.position(at),
            crate::region::Position { line: 2, col: 9 }
        );
    }

    #[test]
    fn blocks() {
        let text = "# Plan\n\n- An item\n  > A quote.\n\n| a |\n| - |\n| b |\n\n```\nx\n```\n";
        let model = Markdown::new(&Region::markdown(text));
        let got: Vec<_> = model
            .blocks
            .iter()
            .map(|b| (b.kind, b.lines.clone()))
            .collect();
        assert_eq!(
            got,
            [
                (BlockKind::Heading(1), 0..1),
                (BlockKind::ListItem, 2..4),
                (BlockKind::BlockQuote, 3..4),
                (BlockKind::Paragraph, 3..4),
                (BlockKind::Table, 5..8),
                (BlockKind::CodeBlock, 9..12),
            ]
        );
        assert_eq!(model.blocks[0].chars, 0..6);
    }
}
