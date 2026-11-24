//! Overview
//!
//! Reports two misuses of a callout in markdown prose. A callout is a
//! boxed aside such as a note, a tip, or a warning. Readers skip
//! callouts, and a page that stacks them trains readers to skip all of
//! them.
//!
//! The first misuse is an instruction inside a note (`Note: Run the
//! migration first`). The technical-writing skill says a reader who skips
//! every note still completes the procedure, so a note gives information
//! and never a step. The fix moves the instruction into the text, or into
//! a warning when skipping it causes harm. The second misuse is a callout
//! that follows another with no prose between them. The fix merges the
//! two callouts or moves one into the text.
//!
//! The rule sits at Recommended, because the check tells an instruction
//! by its first word or by a phrase such as `you must`. That test can
//! misread a sentence.
//!
//! Expression
//!
//! The check reads the lines of the markdown model in order, with the
//! `>` marks that open each line blanked. It knows these callout forms:
//! a GitHub alert (`> [!NOTE]`), a Hugo shortcode (`{{< note >}}`), a
//! MyST directive (`:::{note}`), and a label that opens a paragraph
//! (`**Note:**`, `Note:`). pulldown-cmark takes the marker of an alert
//! out of the text. The check therefore finds the marker in the source
//! of a line that does not hold prose, inside a blockquote and outside a
//! code block. It finds the opening line of a
//! directive in the source too, because the model blanks that line. The
//! model also blanks the `:name: value` option lines under it, so an
//! option does not open the first sentence of the note. The check finds
//! the closing `:::` line in the source as well, because pulldown-cmark
//! reads the first `:` of that line as the marker of a definition. A
//! shortcode opens a callout only at the start of its line. The check
//! finds it in the source, because the model blanks `<note>` in
//! `{{<note>}}` as an HTML tag.
//!
//! A label callout runs to the end of its paragraph. A blank line, a line
//! with no letter, a heading, or a list item ends it. A heading opens
//! with one to six `#` marks and a space, as CommonMark reads it. A line
//! that opens with seven or more marks (`####### Run the migration
//! first`) or with an issue number (`#1234 first`) therefore continues
//! the note.
//!
//! For the first misuse, the check reads the sentences of each note,
//! info, or important callout. It reports a sentence that opens with a
//! command verb (`Run`, `Ensure`, `Do not`) or holds `you must`, `you
//! should`, `must not`, `should not`, `make sure`, or `be sure`.
//!
//! A sentence ends at a `.`, `?`, or `!` before a space, after any
//! closing quote, paren, bracket, or emphasis. A closing quote is
//! straight or curly. A quote after the mark can close a quotation
//! inside the sentence, so a lowercase word after the quote continues
//! the sentence (`prints "Done!" always`). `e.g.`, `i.e.`, `vs.`, `cf.`,
//! and a three-period ellipsis do not end a sentence, so the word after
//! one of them does not open a sentence (`a directory, e.g. never the
//! root`). `ends_sentence` and `continues_sentence` in
//! `src/checks/sentences.rs` hold these tests. A note sentence that ends
//! in a three-period ellipsis, quoted or not, therefore joins the next
//! sentence. Command verbs that open the next sentence then pass
//! (`prints Waiting... Run the cleanup`). Phrases such as `you must` in
//! the next sentence still draw the finding.
//!
//! The check drops the punctuation around the first word, a straight `'`
//! included, so `'Run`, `"Run`, and a curly quote before `Run` each count
//! as the command verb `run`. The `keep in mind` guard in Related Rules
//! skips the same leading punctuation. The check reads a curly
//! apostrophe between two letters of the first word as a straight one.
//! A note that opens with a curly `Don't` therefore draws the finding
//! that the straight one draws.
//!
//! The check also skips seven or more `#` marks before the first word, so
//! a continuation line such as `####### Run the migration first` holds a
//! sentence that opens with `run`. A heading inside an alert, a
//! shortcode, or a directive keeps its one to six marks (`> ## Run the
//! Migration`). Its first word then does not hold a letter, so a command
//! verb that opens the heading does not draw the finding.
//!
//! For the second misuse, the check reports a callout when no prose line
//! sits between it and the callout before it. A line without prose,
//! such as code, also separates two callouts. Each finding names
//! the line where its callout starts.
//!
//! False Positives
//!
//! Warnings and cautions can hold an instruction, because they open with
//! the step they guard. `to make sure` and `to be sure` pass, because
//! each states a purpose and not a step.
//!
//! The check tells a command verb by its spelling, so a note that opens
//! with the same word as a noun still draws a finding (`Note: Check boxes
//! appear only in the admin view`).
//!
//! Related Rules
//!
//! A sentence that opens with `keep in mind` passes, because the Required
//! ProseFiller reports the phrase. A label matches only with a leading
//! capital, so an all-caps label (`IMPORTANT:`) passes on purpose. Prompt
//! files for an agent use it for emphasis, and ProseCapsEmphasis reports
//! it in other files.
//!
//! Measurement
//!
//! The rule hit 155 lines in the measure.py docs corpus, and 20 of 20
//! sampled hits put an instruction in a note or stacked two callouts.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::checks::sentences::{End, continues_sentence, ends_sentence};
use crate::markdown::{BlockKind, Markdown, QUOTE_MARKS, blank};

/// The command verbs that open an instruction.
const IMPERATIVES: &[&str] = &[
    "run",
    "use",
    "set",
    "add",
    "make",
    "ensure",
    "do",
    "don't",
    "avoid",
    "remember",
    "check",
    "install",
    "create",
    "delete",
    "remove",
    "configure",
    "enable",
    "disable",
    "click",
    "select",
    "open",
    "update",
    "restart",
    "verify",
    "always",
    "never",
    "keep",
    "include",
    "specify",
    "define",
    "call",
    "pass",
    "change",
    "replace",
    "edit",
    "upgrade",
    "be",
];

fn re(pattern: &str) -> Regex {
    Regex::new(pattern).expect("the pattern is valid")
}

static ALERT: LazyLock<Regex> = LazyLock::new(|| re(r"\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]"));
static SHORTCODE: LazyLock<Regex> = LazyLock::new(|| {
    re(r"^[ \t>]*\{\{[<%][ \t]*(note|caution|warning|tip|info|important)[ \t]*[>%]\}\}")
});
static SHORTCODE_END: LazyLock<Regex> = LazyLock::new(|| {
    re(r"\{\{[<%][ \t]*/[ \t]*(note|caution|warning|tip|info|important)[ \t]*[>%]\}\}")
});
static MYST: LazyLock<Regex> = LazyLock::new(|| {
    re(r"^[ \t>]*:::+[ \t]*\{(note|tip|important|warning|caution|attention|danger|hint|seealso)\}")
});
static MYST_END: LazyLock<Regex> = LazyLock::new(|| re(r"^[ \t>]*:::"));
static LABEL: LazyLock<Regex> = LazyLock::new(|| {
    re(
        r"^[ \t]*(?:\*\*|__)?(Note|Tip|Warning|Caution|Important|Info)(?::(?:\*\*|__)|(?:\*\*|__):|(?:\*\*|__)[ \t]|:)",
    )
});
static NEW_BLOCK: LazyLock<Regex> =
    LazyLock::new(|| re(r"^[ \t]*(?:#{1,6}(?:[ \t]|$)|(?:[-*+]|[0-9]+[.)])[ \t])"));
static OPENS_MARKUP: LazyLock<Regex> = LazyLock::new(|| re(r"^[ \t]*(?:\{\{|<)"));
static SPLIT: LazyLock<Regex> =
    LazyLock::new(|| re(r#"[.!?](?:["')*_\]]|\x{201d}|\x{2019})*[ \t]+"#));
static LEAD: LazyLock<Regex> = LazyLock::new(|| re(r"^(?:[ \t*_]|#{7,}[ \t])+"));
static CURLY: LazyLock<Regex> = LazyLock::new(|| re(r"([a-z])\x{2019}([a-z])"));
static MODAL: LazyLock<Regex> = LazyLock::new(|| {
    re(r"(?:^|[^a-z])(?:you must|you should|must not|should not|make sure|be sure)(?:[^a-z]|$)")
});
static PURPOSE: LazyLock<Regex> =
    LazyLock::new(|| re(r"(?:^|[^a-z])to (?:make|be) sure(?:[^a-z]|$)"));

/// The kind of block that the open callout runs in.
#[derive(Clone, Copy, PartialEq, Eq)]
enum Form {
    Alert,
    Shortcode,
    Myst,
    Label,
}

/// One callout and the text that it holds so far.
struct Callout {
    kind: String,
    form: Form,
    body: String,
    /// The index in the region of the first character of its line.
    at: usize,
}

#[derive(Default)]
struct State {
    hits: Vec<Hit>,
    open: Option<Callout>,
    /// Whether a prose line sits between the last callout and this line.
    since_prose: bool,
}

impl State {
    fn start(&mut self, kind: &str, form: Form, at: usize) {
        self.end();
        if !self.since_prose {
            self.hits.push(Hit {
                at,
                message: "Callout. Two callouts in a row. Merge them, or move one into the text."
                    .to_string(),
            });
        }
        self.since_prose = false;
        self.open = Some(Callout {
            kind: kind.to_lowercase(),
            form,
            body: String::new(),
            at,
        });
    }

    /// Ends the open callout and reports its first instruction.
    fn end(&mut self) {
        let Some(callout) = self.open.take() else {
            return;
        };
        if matches!(callout.kind.as_str(), "note" | "info" | "important")
            && callout.body.contains(|c: char| c.is_ascii_alphabetic())
            && sentences(&callout.body).iter().any(|s| instruction(s))
        {
            self.hits.push(Hit {
                at: callout.at,
                message: "Callout. A note holds an instruction, and readers skip notes. Move the instruction into the text, or into a warning when skipping it causes harm.".to_string(),
            });
        }
    }
}

/// Splits `body` into its sentences.
fn sentences(body: &str) -> Vec<String> {
    let mut out: Vec<String> = Vec::new();
    let mut from = 0;
    let mut sep: Option<&str> = None;
    let mut pieces: Vec<(&str, Option<&str>)> = Vec::new();
    for m in SPLIT.find_iter(body) {
        pieces.push((&body[from..m.start()], sep));
        sep = Some(m.as_str());
        from = m.end();
    }
    pieces.push((&body[from..], sep));
    let mut last = "";
    for (piece, sep) in pieces {
        let ends = match sep {
            None => true,
            Some(sep) => {
                let word = last.rsplit([' ', '\t']).next().unwrap_or("");
                match ends_sentence(&format!("{word}{}", sep.trim_end())) {
                    End::Yes => true,
                    End::No => false,
                    End::Quote => {
                        !continues_sentence(piece.split([' ', '\t']).next().unwrap_or(""))
                    }
                }
            }
        };
        match (ends, out.last_mut(), sep) {
            (false, Some(open), Some(sep)) => {
                open.push_str(sep);
                open.push_str(piece);
            }
            _ => out.push(piece.to_string()),
        }
        last = piece;
    }
    out
}

/// Returns whether the sentence `s` gives an instruction.
fn instruction(s: &str) -> bool {
    let s = LEAD.replace(s, "").to_lowercase();
    let first = s.split([' ', '\t']).next().unwrap_or("");
    let first = CURLY.replace_all(first, "$1'$2");
    let word = first.trim_matches(|c: char| !c.is_ascii_lowercase());
    let lead = s.trim_start_matches(|c: char| !c.is_ascii_lowercase());
    (IMPERATIVES.contains(&word) && !lead.starts_with("keep in mind"))
        || (MODAL.is_match(&s) && !PURPOSE.is_match(&s))
}

/// Returns a finding for each note that holds an instruction and for
/// each callout that follows another with no prose between them.
pub fn check(model: &Markdown) -> Vec<Hit> {
    let mut state = State {
        since_prose: true,
        ..State::default()
    };
    // Whether a line without prose sits between the last prose line and
    // this one, and the index of the last line of a label callout.
    let mut skipped = false;
    let mut label_last = 0;
    for (i, line) in model.lines.iter().enumerate() {
        // pulldown-cmark takes the marker of an alert out of the prose. A
        // marker in the source counts only inside a blockquote and outside
        // a code block, because a markdown example in a fence can show
        // alerts.
        let probe = if line.prose { &line.text } else { &line.source };
        let alert = ALERT.captures(probe).filter(|c| {
            let at = line.at(probe, c.get(0).expect("a match").start());
            line.prose
                || (model.inside(BlockKind::BlockQuote, at)
                    && !model.inside(BlockKind::CodeBlock, at))
        });
        if !line.prose && alert.is_none() {
            skipped |= line
                .source
                .contains(|c: char| !matches!(c, ' ' | '\t' | '>'));
            continue;
        }
        let form = state.open.as_ref().map(|c| c.form);
        if skipped && !matches!(form, Some(Form::Shortcode | Form::Myst)) {
            state.end();
            state.since_prose = true;
        }
        skipped = false;
        if let Some(alert) = alert {
            state.start(&alert[1], Form::Alert, line.start);
            continue;
        }

        let text = blank(&line.text, &QUOTE_MARKS);
        let lower = line.source.to_lowercase();
        if let Some(c) = SHORTCODE.captures(&lower) {
            state.start(&c[1], Form::Shortcode, line.start);
            continue;
        }
        if SHORTCODE_END.is_match(&lower) {
            state.end();
            continue;
        }
        if let Some(c) = MYST.captures(&lower) {
            state.start(&c[1], Form::Myst, line.start);
            continue;
        }
        let form = state.open.as_ref().map(|c| c.form);
        match form {
            Some(Form::Alert) if line.source.trim_start().starts_with('>') => {
                append(&mut state, &text);
                continue;
            }
            Some(Form::Shortcode) => {
                append(&mut state, &text);
                continue;
            }
            Some(Form::Myst) if MYST_END.is_match(&line.source) => {
                state.end();
                continue;
            }
            Some(Form::Myst) => {
                append(&mut state, &text);
                continue;
            }
            Some(Form::Label)
                if i == label_last + 1
                    && text.contains(|c: char| c.is_ascii_alphabetic())
                    && !NEW_BLOCK.is_match(&text) =>
            {
                append(&mut state, &text);
                label_last = i;
                continue;
            }
            Some(_) => state.end(),
            None => {}
        }
        if let Some(c) = LABEL.captures(&text) {
            state.start(&c[1], Form::Label, line.start);
            append(&mut state, &text[c.get(0).expect("a match").end()..]);
            label_last = i;
            continue;
        }
        if text.contains(|c: char| c.is_ascii_alphabetic()) && !OPENS_MARKUP.is_match(&text) {
            state.since_prose = true;
        }
    }
    state.end();
    state.hits
}

/// Adds a line of text to the body of the open callout.
fn append(state: &mut State, text: &str) {
    if let Some(callout) = state.open.as_mut() {
        callout.body.push(' ');
        callout.body.push_str(text);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::region::Region;

    #[test]
    fn callouts() {
        struct Case {
            name: &'static str,
            text: &'static str,
            /// The line and the message of each finding, by its first
            /// word.
            want: Vec<(usize, &'static str)>,
        }
        let cases = [
            Case {
                name: "shortcode with no spaces",
                text: "The pod runs.\n\n{{<note>}}\nRun the migration first.\n{{</note>}}\n",
                want: vec![(3, "A note")],
            },
            Case {
                name: "shortcode in a code span",
                text: "Use `{{< note >}}` for an aside. Run the migration first.\n",
                want: vec![],
            },
            Case {
                name: "directive under a paragraph",
                text: "The pod runs.\n\n:::{note}\nRun the migration first.\n:::\n",
                want: vec![(3, "A note")],
            },
            Case {
                name: "directive with an option",
                text: "The pod runs.\n\n:::{note}\n:class: dropdown\nRun the migration first.\n:::\n",
                want: vec![(3, "A note")],
            },
            Case {
                name: "directive right under a paragraph",
                text: "The pod runs.\n:::{note}\nRun the migration first.\n:::\n",
                want: vec![(2, "A note")],
            },
            Case {
                name: "directive in indented code",
                text: ":::{tip}\nThe pod runs.\n:::\n\nThe pod stops.\n\n    :::{note}\n    Run it.\n    :::\n",
                want: vec![],
            },
            Case {
                name: "directive after a directive",
                text: ":::{tip}\nThe pod runs.\n:::\n\n:::{note}\nThe pod stops.\n:::\n",
                want: vec![(5, "Two")],
            },
            Case {
                name: "alert",
                text: "> [!NOTE]\n> Run the migration first.\n",
                want: vec![(1, "A note")],
            },
            Case {
                name: "alerts in a fenced code block",
                text: "The pod runs.\n\n```markdown\n> [!NOTE]\n> [!TIP]\n```\n\nThe pod stops.\n",
                want: vec![],
            },
            Case {
                name: "alert after an alert",
                text: "> [!NOTE]\n> The pod runs.\n\n> [!TIP]\n> The pod stops.\n",
                want: vec![(4, "Two")],
            },
        ];
        for case in cases {
            let region = Region::markdown(case.text);
            let mut got: Vec<_> = check(&Markdown::new(&region))
                .into_iter()
                .map(|hit| {
                    let word = hit.message.split(". ").nth(1).unwrap_or("");
                    let word = if word.starts_with("Two") {
                        "Two"
                    } else {
                        "A note"
                    };
                    (region.position(hit.at).line, word)
                })
                .collect();
            got.sort();
            assert_eq!(got, case.want, "{}", case.name);
        }
    }
}
