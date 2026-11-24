//! Overview
//!
//! Reports a paragraph of markdown prose that opens with a pronoun (`This
//! is similar to recursive attribute sets`). A pronoun is a word that
//! stands in for a noun named earlier, such as `it`, `they`, or `this`.
//! The noun sits in the paragraph before. Readers who jump to the
//! paragraph from a link or a search read back to find it. A paragraph
//! that someone moves, or a paragraph added between the two, changes what
//! the pronoun names.
//!
//! The fix names the noun, so the paragraph stands on its own (`A
//! submodule is similar to a recursive attribute set`). The rule sits at
//! Optional, because an opening pronoun also points at the section that
//! holds the paragraph, where the reader can see the topic.
//!
//! Expression
//!
//! The check reads the first words of each paragraph block of the
//! markdown model. It reports `It`, `They`, `This`, `These`, `That`, or
//! `Those` before a verb or a helping verb from a list (`is`, `can`,
//! `requires`). The check skips the text of a list item on the line of
//! its list marker. It reads only the later paragraphs of an item.
//! The paragraph of a MyST directive opens at the first line after the
//! opening line and its option lines (`:::{warning}`, `This will result
//! in an error.`).
//!
//! The guards for `to`, `that`, and a colon read the first sentence of
//! the paragraph, which can run onto the lines below its first line
//! (`It may help` above `to restart the pod.`). The paragraph block ends
//! where GFM ends the paragraph, so the end of the block also ends the
//! sentence. `ends_sentence` and `continues_sentence` in
//! `src/checks/sentences.rs` find the end of the sentence, so `e.g.`,
//! `i.e.`, `vs.`, `cf.`, and a three-period ellipsis do not end it (`It
//! may help, e.g. to restart the pod`). A quoted `.`, `?`, or `!` before
//! a lowercase word does not end it either, and that word can open the
//! next line (`It writes "Done!" to the log`).
//!
//! A first sentence that ends in a three-period ellipsis, quoted or not,
//! joins the next sentence. A `to` or `that` in that sentence, or a colon
//! at the end of a line, then passes the paragraph (`It can hang...
//! Then it fails to start`).
//!
//! The guards miss some paragraphs whose pronoun names an earlier noun.
//! A `to` or `that` in another role passes `It` (`It adjusts the
//! replicas to match the load`), and a first sentence that ends in a
//! colon can still point back (`This is equivalent to these commands:`).
//!
//! pulldown-cmark, and Harper with it, reads some lines in another way
//! than GFM does, and the guards then read the words of the next block.
//! A table interrupts a paragraph only when its header row opens with
//! `|` fewer than four columns past the edge of its block. So a header
//! row such as `a | to` or `    | a | to |` above `--- | ---` is more
//! text of the paragraph. A line that holds only one tag, such as
//! `<br>`, is more text of a paragraph in a blockquote or a list item
//! above it. GFM opens an HTML block at that line (`> It may help`,
//! `<br>`, `> to restart the pod.`).
//!
//! False Positives
//!
//! `This cache` names its noun, so a noun after the pronoun passes. A
//! plural pronoun takes a verb without an `-s`, so a word that ends in
//! `s` after `These`, `Those`, or `They` counts as a noun (`These changes
//! simplify`).
//!
//! A paragraph passes when a line ends in a colon before its first
//! sentence ends (`These are the tiers:`). Its pronoun then points
//! forward at the block below. The model blanks a code span or a link
//! destination, so a colon before one at the end of the line also
//! passes. There the pronoun points at the span (``This is the default:
//! `false` ``).
//!
//! A paragraph directly under a heading passes, because there the
//! pronoun points at the heading's topic, which the reader can see. A
//! setext heading counts too, and its underline is the line above the
//! paragraph (`Setup`, `-----`, a blank line, `It runs on the node.`).
//! Lines of only whitespace and `>` marks can sit between the two. The
//! paragraph must sit in the list item or the blockquote that holds the
//! heading. Every block that opens between the two must hold the
//! paragraph too. So a paragraph in a quote right under the heading
//! passes (`## Setup`, `> It runs on the node.`), and so does the text
//! of an alert there (`## Setup`, `> [!NOTE]`, `> It runs on the
//! node.`). A paragraph after the
//! blockquote that holds the heading can draw a finding (`> ## Setup`, a
//! blank line, `It runs on the node.`). So can a paragraph after an empty
//! quote (`## Setup`, `>`, a blank line, `> It runs on the node.`).
//!
//! A definition of a definition list passes, because there the pronoun
//! points at the term right above it (`Strict`, `: This mode rejects each
//! unknown key.`).
//!
//! The first paragraph of a region passes when any line sits above it,
//! such as a title or front matter, because there the pronoun points at
//! the page.
//!
//! pulldown-cmark reads a line that holds only one tag right under a
//! list item or under a paragraph in a blockquote as more text of that
//! block. Right under a table, it reads the line as another row of the
//! table. GFM opens an HTML block at that line, and the block holds the
//! lines up to the next blank line. A paragraph under that line can then
//! draw a finding (`- The pod holds the cache.`, `<br>`, a blank line,
//! `  It may help.`, `---`). So can a quote in the lines of that block
//! (`| a | b |`, `| - | - |`, `<br>`, `> It runs on the node.`). A
//! deeper quote under a blockquote can draw one too (`> <span>x</span>
//! y`, `<br>`, `> > It runs on the node.`).
//!
//! pulldown-cmark opens an HTML block at `<!` and any letter, such as
//! `<!doctype html>`, and that block interrupts a paragraph. cmark-gfm
//! opens one only at `<!` and an uppercase letter. So the first sentence
//! ends above that line, and the paragraph can draw a finding (`It may
//! help`, `<!doctype html> to restart the pod.`).
//!
//! The check reads markdown only. Go doc comments open later paragraphs
//! with `It` by convention, and there the first paragraph names the
//! function.
//!
//! Related Rules
//!
//! Some openings use `it` as a placeholder subject that points at no
//! earlier noun (`It is possible to`, `It is vital that`). `It` before
//! `is` or `was` and an adjective such as `recommended` or `possible`
//! passes, also in quotes (`It is 'common' in clusters`). So does `It`
//! when its first sentence holds `to` or `that` (`It may help to restart
//! the pod`). ProseExpletive, ProseImportance, and both tiers of
//! ProseObligation report those openings.
//!
//! ProseBroadReference reports `This means`, `This leads`, and the other
//! verbs of consequence, with or without `also` or `then` between them,
//! and `This is because`, `why`, or `how`. Those openings pass here too.
//!
//! Measurement
//!
//! The rule hit 105 lines in the measure.py docs corpus, and 19 of 20
//! sampled hits pointed back across the paragraph break. The misfire was
//! a line of sample output that a Hugo page shows (`This is some code.`),
//! where `This` points at the line itself.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::checks::sentences::{End, continues_sentence, ends_sentence, words};
use crate::markdown::{Block, BlockKind, Markdown};

/// The pronouns that the check reports.
const PRONOUNS: &[&str] = &["it", "they", "this", "these", "that", "those"];

/// The verbs and the helping verbs that follow a reported pronoun.
const VERBS: &[&str] = &[
    "is", "are", "was", "were", "has", "have", "had", "does", "do", "did", "can", "cannot", "will",
    "would", "could", "should", "must", "may", "might", "also", "then", "only", "still", "runs",
    "returns", "uses", "needs", "requires", "includes", "provides", "helps", "leads", "results",
    "applies", "depends", "reads", "writes", "holds", "set", "sets", "adds", "removes", "creates",
    "changes", "tells", "shows",
];

/// The words after `It is` or `It was` that make `it` a placeholder
/// subject (`It is possible`).
const EXPLETIVES: &[&str] = &[
    "recommended",
    "suggested",
    "certainly",
    "advised",
    "encouraged",
    "discouraged",
    "important",
    "necessary",
    "possible",
    "impossible",
    "advisable",
    "best",
    "common",
    "easy",
    "hard",
    "difficult",
    "useful",
    "helpful",
    "safe",
    "worth",
    "likely",
    "unlikely",
    "not",
    "a",
    "an",
    "good",
    "better",
    "unclear",
    "clear",
    "true",
    "false",
    "essential",
    "required",
    "often",
    "usually",
    "generally",
    "sometimes",
    "up",
    "also",
];

/// The verbs of consequence after `This`, which ProseBroadReference
/// reports.
const CONSEQUENCES: &[&str] = &[
    "means", "meant", "ensures", "happens", "prevents", "causes", "keeps", "works", "matters",
    "leads", "led", "results", "resulted", "makes", "allows", "lets", "gives",
];

/// A `to` or a `that` after the first word of the first sentence.
static TO_THAT: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[ \t\n](?:to|that)[ \t\n]").expect("the pattern is valid"));

/// A colon at the end of a line of the first sentence.
static COLON: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r":[ \t]*(?:\n|$)").expect("the pattern is valid"));

/// The `:` that opens a definition of a definition list.
pub static DEFINITION: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[ \t>]*:[ \t]").expect("the pattern is valid"));

/// The marker line of a GitHub alert (`> [!NOTE]`).
static ALERT: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^[ \t>]*\[!(?i:note|tip|important|warning|caution)\][ \t]*$")
        .expect("the pattern is valid")
});

/// Returns `token` in lowercase without the marks around it.
fn word(token: &str) -> String {
    token
        .to_lowercase()
        .trim_matches(|c: char| !c.is_ascii_lowercase())
        .to_string()
}

/// Returns whether a heading sits directly above `block`, in the block
/// that holds the heading or in a block that this block opens. The
/// marker line of an alert can sit between the two.
fn under_heading(model: &Markdown, block: &Block) -> bool {
    let first = block.lines.start;
    let Some(prev) = (0..first).rev().find(|&i| {
        let source = &model.lines[i].source;
        !source.trim_matches([' ', '\t', '>']).is_empty() && !ALERT.is_match(source)
    }) else {
        return false;
    };
    let Some(heading) = model
        .blocks
        .iter()
        .find(|b| matches!(b.kind, BlockKind::Heading(_)) && b.lines.end == prev + 1)
    else {
        return false;
    };
    let at = block.chars.start;
    let container = model
        .blocks
        .iter()
        .filter(|b| {
            matches!(b.kind, BlockKind::ListItem | BlockKind::BlockQuote)
                && b.chars.contains(&heading.chars.start)
        })
        .max_by_key(|b| b.chars.start);
    if container.is_some_and(|c| !c.chars.contains(&at)) {
        return false;
    }
    model
        .blocks
        .iter()
        .filter(|b| b.chars.start >= heading.chars.end && b.chars.start < at)
        .all(|b| b.chars.contains(&at))
}

/// The first sentence of a paragraph, read one line at a time.
#[derive(Default)]
struct First {
    /// The text of the sentence, with a newline after each line.
    text: String,
    /// How the last word read ends its sentence.
    end: Option<End>,
    /// Whether the sentence has ended.
    done: bool,
}

impl First {
    /// Adds the words of the line `text` up to the end of the sentence.
    fn read(&mut self, text: &str) {
        let mut cut = text.len();
        for (offset, token) in words(text) {
            if self.end == Some(End::Quote) && !continues_sentence(token) {
                self.done = true;
                cut = offset;
                break;
            }
            let end = ends_sentence(token);
            self.end = Some(end);
            if end == End::Yes {
                self.done = true;
                cut = offset + token.len();
                break;
            }
        }
        self.text.push_str(&text[..cut]);
    }
}

/// Returns the finding of one paragraph, if any.
fn paragraph(model: &Markdown, block: &Block) -> Option<Hit> {
    // The opening line of a MyST directive and its option lines open the
    // paragraph of the directive, and the model blanks them.
    let mut lines = model
        .block_lines(block)
        .skip_while(|(i, _)| model.lines[*i].directive);
    let (i, text) = lines.next()?;
    if DEFINITION.is_match(&model.lines[i].source) {
        return None;
    }
    let mut tokens = words(&text);
    let (offset, t1) = tokens.next()?;
    let t2 = tokens.next().map_or("", |t| t.1);
    let t3 = tokens.next().map_or("", |t| t.1);
    let (w1, w2, w3) = (word(t1), word(t2), word(t3));
    if !t1.starts_with(|c: char| c.is_ascii_uppercase())
        || t1.ends_with([',', '.', ':', ';'])
        || !PRONOUNS.contains(&w1.as_str())
        || !t2.starts_with(|c: char| c.is_ascii_lowercase())
        || !VERBS.contains(&w2.as_str())
    {
        return None;
    }
    let be = w2 == "is" || w2 == "was";
    if w1 == "it" && be && EXPLETIVES.contains(&w3.as_str()) {
        return None;
    }
    // A plural pronoun does not take an `-s` verb, so `These changes`
    // names a noun.
    if matches!(w1.as_str(), "these" | "those" | "they") && w2.ends_with('s') {
        return None;
    }
    // ProseBroadReference reports these openings.
    if w1 == "this"
        && ((be && matches!(w3.as_str(), "because" | "why" | "how"))
            || CONSEQUENCES.contains(&w2.as_str())
            || (matches!(w2.as_str(), "also" | "then") && CONSEQUENCES.contains(&w3.as_str())))
    {
        return None;
    }
    let mut first = First::default();
    first.read(&text);
    for (_, line) in lines {
        if first.done {
            break;
        }
        first.text.push('\n');
        first.read(&line);
    }
    if (w1 == "it" && TO_THAT.is_match(&first.text)) || COLON.is_match(&first.text) {
        return None;
    }
    let shown: String = t1.chars().filter(char::is_ascii_alphabetic).collect();
    Some(Hit {
        at: model.lines[i].at(&text, offset),
        message: format!(
            "Opening pronoun. The paragraph opens with `{shown}`, so the reader must look back for its noun. Name the noun."
        ),
    })
}

/// Returns a finding for each paragraph that opens with a pronoun.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .blocks
        .iter()
        .filter(|b| b.kind == BlockKind::Paragraph)
        // The text of a list item on the line of its marker is not a
        // paragraph of its own here.
        .filter(|b| {
            !model
                .blocks
                .iter()
                .any(|item| item.kind == BlockKind::ListItem && item.lines.start == b.lines.start)
        })
        .enumerate()
        // The first paragraph under a title or front matter points at the
        // page.
        .filter(|(n, b)| !(*n == 0 && b.lines.start > 0))
        .filter(|(_, b)| !under_heading(model, b))
        .filter_map(|(_, b)| paragraph(model, b))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::region::Region;

    /// Returns the line and the column of each finding on `text`.
    fn found(text: &str) -> Vec<(usize, usize)> {
        let region = Region::markdown(text);
        check(&Markdown::new(&region))
            .into_iter()
            .map(|hit| {
                let at = region.position(hit.at);
                (at.line, at.col)
            })
            .collect()
    }

    #[test]
    fn paragraphs() {
        struct Case {
            name: &'static str,
            text: &'static str,
            /// The line and the column of each finding.
            want: Vec<(usize, usize)>,
        }
        let cases = [
            Case {
                name: "paragraph under a setext heading",
                text: "The pod holds the cache.\n\nSetup\n-----\n\nThis is the setup step.\n\n## Setup\n\nThis is the setup step.\n",
                want: vec![],
            },
            Case {
                name: "placeholder subject before a quoted word",
                text: "The pod runs the job.\n\nIt is common in clusters.\n\nIt is \"common\" in clusters.\n\nIt is 'common' in clusters.\n",
                want: vec![],
            },
            Case {
                name: "link reference definition at the text of an item",
                text: "The pod holds the cache.\n\n- The pod holds the cache.\n  [a]: x\n> The pod runs.\n\n  It may help.\n---\n",
                want: vec![],
            },
            Case {
                name: "blockquote left of the text of an item",
                text: "The pod holds the cache.\n\n- The pod holds the cache.\n\n  > It may help\n> to restart the pod.\n",
                want: vec![(5, 5)],
            },
            Case {
                name: "rule left of a paragraph that a tab indents in an item",
                text: "The pod holds the cache.\n\n1. The pod holds the cache.\n\n\tIt may help.\n ---\n",
                want: vec![(5, 2)],
            },
            Case {
                name: "underline that a tab indents in an item",
                text: "The pod holds the cache.\n\n- The pod holds the cache.\n\n  It may help.\n\t---\n",
                want: vec![],
            },
            Case {
                name: "line of dashes four columns past the margin",
                text: "The pod starts.\n\nIt also runs on every commit.\n\nIt also runs on every merge.\n    ---\n",
                want: vec![(3, 1), (5, 1)],
            },
            Case {
                name: "line of dashes four columns past the margin under an indented paragraph",
                text: "The pod holds the cache.\n\n   It runs on the node.\n    ---\n",
                want: vec![(3, 4)],
            },
            Case {
                name: "rule marks with an HTML comment after them",
                text: "The pod holds the cache.\n\n*** <!-- note -->\nIt runs on the node.\n",
                want: vec![],
            },
            Case {
                name: "HTML comment that opens the line",
                text: "The pod holds the cache.\n\n<!-- c --> It runs on the node.\n",
                want: vec![],
            },
            Case {
                name: "header row of a table under the first line",
                text: "The pod holds the cache.\n\nIt runs | on the node.\n--- | ---\n",
                want: vec![],
            },
            Case {
                name: "alert under a heading",
                text: "The pod holds the cache.\n\n## Setup\n\n> [!NOTE]\n> It runs on the node.\n",
                want: vec![],
            },
            Case {
                name: "paragraph under an alert marker",
                text: "The pod holds the cache.\n\n> [!NOTE]\n> This requires root.\n",
                want: vec![(4, 3)],
            },
            Case {
                name: "text of a MyST directive",
                text: "Run the command.\n\n:::{warning}\n:class: tip\nThis will result in an error.\n:::\n",
                want: vec![(5, 1)],
            },
            Case {
                name: "definition under its term",
                text: "The pod holds the cache.\n\nStrict\n: This mode rejects each unknown key.\n",
                want: vec![],
            },
            Case {
                name: "paragraph after the blockquote of a heading",
                text: "The pod holds the cache.\n\n> ## Setup\n\nIt runs on the node.\n\n## Setup\n>\n\n> It runs on the node.\n\n## Setup\n> It runs on the node.\n",
                want: vec![(5, 1), (10, 3)],
            },
        ];
        for case in cases {
            assert_eq!(found(case.text), case.want, "{}", case.name);
        }
    }

    // The case of the opening-pronoun test of pkgs/prose-lint.nix. Its
    // want lists the findings that GFM reads. pulldown-cmark reads some
    // lines in another way, so the findings of these paragraphs are not
    // in the list:
    //
    // - Each paragraph above a header row that does not open with `|`
    //   fewer than four columns past the edge of its block, such as
    //   `a | to` or `    | a | to |` above `--- | ---`. pulldown-cmark
    //   opens a table under a paragraph only at such a header row, so
    //   any other row joins the first sentence. When a line of `---`
    //   sits under such a delimiter row, pulldown-cmark reads it as the
    //   underline of a setext heading (`It may help.`, `Flag`,
    //   `| --- |`, `---`). The paragraph is then a heading, so it does
    //   not draw a finding.
    // - Each paragraph in a blockquote or a list item above a line that
    //   holds only one tag, such as `<br>`. pulldown-cmark reads the tag
    //   as more text of the paragraph, so the line under it joins the
    //   first sentence.
    const OPENING_PRONOUN: &[&str] = &[
        "The pod holds the cache.",
        "",
        "> It may help",
        ">     ",
        "> to restart the pod.",
        "",
        "> It may help",
        ">\t\t",
        "> to restart the pod.",
        "",
        "It may help to restart the pod",
        "    - first",
        "",
        "[guide]: https://example.com",
        "    It runs on the node.",
        "",
        "1. The pod holds the cache.",
        "",
        "   ## Setup",
        "",
        "   It may help to restart the pod",
        "       - - -",
        "",
        "   ~~~",
        "   make build",
        "   ~~~",
        "        make test",
        "    It runs on the node.",
        "",
        "It may help",
        "    | a | to |",
        "| --- | --- |",
        "",
        "It may help",
        "    | a | to |",
        "--- | :--:",
        "",
        "It may help",
        "    - first to restart",
        ":--",
        "",
        "It may help",
        "| to restart | the pod |",
        "| --- | --- |",
        "",
        "It may help",
        "<span>a</span> | to",
        "--- | ---",
        "",
        "It may help.",
        "| a | b |",
        "| --- | --- |",
        "---",
        "",
        "It may help",
        "a | to",
        "--- | ---",
        "",
        "It may help.",
        "Flag",
        "| --- |",
        "---",
        "",
        "It may help",
        "to restart",
        ":--",
        "",
        "> It may help",
        "<br>",
        "> to restart the pod.",
        "",
        "- The pod holds the cache.",
        "",
        "  It may help",
        "<br>",
        "  to restart the pod.",
        "",
        "- The pod holds the cache.",
        "  | a |",
        "",
        "  It may help",
        "<br>",
        "  to restart the pod.",
        "",
        "> It may help",
        "<img src=\"x.png\">",
        "> to restart the pod.",
        "",
        "1. The pod holds the cache.",
        "",
        "  It may help",
        "  | a | to |",
        "| - | - |",
        "",
        "1. The pod holds the cache.",
        "",
        "   ## Setup",
        "",
        "   The pod runs.",
        "",
        "   It may help",
        "  ---",
        "",
        "<span>x</span>",
        "- The pod holds the cache.",
        "",
        "  It may help",
        "<br>",
        "  to restart the pod.",
        "",
        "The pod runs.",
        "- The pod holds the cache and",
        "serves the reads.",
        "",
        "  It may help.",
        "---",
        "",
        "## Setup",
        "2. The pod holds the cache.",
        "",
        "   It may help",
        "<br>",
        "   to restart the pod.",
        "",
        "<span>x</span>",
        "- The pod holds the cache.",
        "",
        "  - The sub item.",
        "",
        "  It may help.",
        "---",
        "",
        "> The pod runs.",
        "2. The pod holds the cache.",
        "",
        "   It may help.",
        "---",
        "",
        "The pod runs.",
        "~~~",
        "make",
        "~~~",
        "2. The pod holds the cache.",
        "",
        "   It may help.",
        "---",
        "",
        "- The pod holds the cache.",
        "",
        "  It may help",
        "  | a | to restart |",
        "\t| - | - |",
        "",
        "- The pod holds the cache.",
        "[a]: x",
        "",
        "  It may help.",
        "---",
        "",
        "- The pod holds the cache.",
        "[a]: x",
        "",
        "  It may help",
        "<br>",
        "  to restart the pod.",
        "",
        "- The pod holds the cache.",
        "",
        "   It may help",
        "   a | to restart",
        "  | --- | --- |",
        "",
        "## Setup",
        "2. The pod holds the cache.",
        "",
        "    It may help",
        "    a | to restart",
        "   | --- | --- |",
        "",
        ">   It may help",
        "> | a | to |",
        "> | - | - |",
        "",
        "| Name | Value |",
        "| ---- | ----- |",
        "> It runs on the node.",
        "",
        "The pod holds the cache.",
        "| Name | Value |",
        "| ---- | ----- |",
        "> It runs on the node.",
        "",
        "> | Name | Value |",
        "> | ---- | ----- |",
        "> > It runs on the node.",
        "",
        "> <div>",
        "[a]: x",
        "> It runs on the node.",
        "",
        "<script>x</script>",
        "[a]: x",
        "It runs on the node.",
        "",
        "<pre>",
        "x",
        "</pre>",
        "```sh",
        "make",
        "```",
        "It runs on the node.",
        "",
        "<span>x</span> y",
        "<br>",
        "```sh",
        "make",
        "```",
        "It runs on the node.",
        "",
        "<span>x</span> y",
        "<br>",
        ">",
        "> It runs on the node.",
        "",
        "<span>x</span> y",
        "| - | - |",
        "<br>",
        "> It runs on the node.",
        "",
        "- The pod holds the cache.",
        "  <br>",
        "```sh",
        "make",
        "```",
        "It runs on the node.",
        "",
        "- Install the agent.",
        "",
        "  <details>",
        "  ```sh",
        "  make",
        "  ```",
        "It runs on the node.",
        "",
        "- Install the agent.",
        "  <div>",
        "  [a]: x",
        "It runs on the node.",
        "",
        "- Install the agent.",
        "  <div>",
        "  <!-- a",
        "  b -->",
        "It runs on the node.",
        "",
        "<span>x</span> y",
        "    ===",
        "<br>",
        ">",
        "> It runs on the node.",
        "",
        "## Setup",
        "    <div>",
        ">",
        "> It runs on the node.",
        "",
        "## Setup",
        "    <div>",
        "<!-- c -->",
        "It runs on the node.",
        "",
        "| a |",
        "| - |",
        "    <div>",
        "<!-- c -->",
        "It runs on the node.",
        "",
        "```sh",
        "make",
        "```",
        "    <div>",
        "<!-- c -->",
        "It runs on the node.",
        "",
        "<span>x</span>",
        "- The pod holds the cache.",
        "  ## Setup",
        "",
        "  The pod runs.",
        "",
        "  It may help",
        "<br>",
        "  to restart the pod.",
        "",
        "<span>x</span>",
        "- The pod holds the cache.",
        "  ## Setup",
        "",
        "  The pod runs.",
        "",
        "  It may help.",
        "---",
        "",
        "- The pod holds the cache and",
        "serves the reads.",
        "  ## Setup",
        "",
        "  The pod runs.",
        "",
        "  It may help.",
        "---",
        "",
        "## Setup",
        "2. The pod holds the cache.",
        "   ## Steps",
        "",
        "   The pod runs.",
        "",
        "   It may help.",
        "---",
    ];
    const OPENING_PRONOUN_WANT: &[(usize, usize)] = &[
        (3, 3),
        (7, 3),
        (15, 5),
        (28, 5),
        (42, 1),
        (50, 1),
        (91, 3),
        (101, 4),
        (115, 3),
        (130, 3),
        (136, 4),
        (145, 4),
        (150, 3),
        (157, 3),
        (180, 5),
        (186, 3),
        (191, 3),
        (195, 5),
        (199, 3),
        (203, 1),
        (211, 1),
        (218, 1),
        (223, 3),
        (228, 3),
        (235, 1),
        (243, 1),
        (248, 1),
        (254, 1),
        (260, 3),
        (265, 3),
        (270, 1),
        (276, 1),
        (283, 1),
        (301, 3),
        (310, 3),
        (319, 4),
    ];

    #[test]
    fn opening_pronoun() {
        let text = OPENING_PRONOUN.join("\n") + "\n";
        assert_eq!(found(&text), OPENING_PRONOUN_WANT);
    }
}
