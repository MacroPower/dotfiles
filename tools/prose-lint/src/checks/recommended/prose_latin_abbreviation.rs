//! Overview
//!
//! Reports a Latin abbreviation in markdown prose and in source comments:
//! `e.g.`, `i.e.`, `cf.`, `viz.`, `et al.`, and `n.b.`. Many readers mix
//! up `e.g.` (for example) and `i.e.` (that is), and the two readings
//! disagree on whether the list after it is complete. The English phrase
//! has one reading, so the fix writes it, and each finding names its
//! replacement. The rule sits at Recommended, because a sentence that
//! ends on a variable named `cf` also matches.
//!
//! Expression
//!
//! The check matches one regular expression against the prose of each
//! line of the markdown model and reports each match. Harper reads `e.g.`
//! as one abbreviation token that no Weir pattern names, so the check is
//! native.
//!
//! The abbreviation needs a character before it that is not a letter, a
//! digit, `.`, `/`, `_`, or `-`. A letter, a digit, `/`, `_`, or `-`
//! after it also makes it pass. The model blanks code spans and link
//! destinations, so a match inside one passes.
//!
//! False Positives
//!
//! The character before the abbreviation lets a path (`/etc/i.e./x`) or a
//! name (`foo_e.g.`) pass, and the character after it lets a file name or
//! a field pass (`cf.go`, `cf.Name`). `vs.` stays out, because it has one
//! reading, and Go code often names a variable `vs`.
//!
//! A sentence that ends on a variable named `cf` still matches (`then
//! closes cf.`).
//!
//! Related Rules
//!
//! `etc.` belongs to ProseOpenList.
//!
//! Measurement
//!
//! The rule hit 279 lines in the measure.py corpus, and 20 of 20 sampled
//! hits were Latin abbreviations.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::markdown::Markdown;

static LATIN: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(?:^|[^A-Za-z0-9./_-])([Ee]\. ?[Gg]\.?|[Ii]\. ?[Ee]\.?|[Vv]iz\.|[Cc]f\.|[Ee]t al\.?|[Nn]\. ?[Bb]\.)(?:[^A-Za-z0-9/_-]|$)",
    )
    .expect("the pattern is valid")
});

/// Returns the replacement that a finding names for the abbreviation
/// `hit`.
fn english(hit: &str) -> &'static str {
    let key = hit.to_lowercase().replace(' ', "");
    match key.trim_end_matches('.') {
        "e.g" => "Write `for example` or `such as`.",
        "i.e" => "Write `that is`.",
        "viz" => "Write `namely`.",
        "cf" => "Write `see` or `compare`.",
        "etal" => "Name the others, or open the list with `such as`.",
        _ => "Delete it and state the point.",
    }
}

/// Returns a finding for each Latin abbreviation.
pub fn check(model: &Markdown) -> Vec<Hit> {
    let mut hits = Vec::new();
    for line in &model.lines {
        let text = line.text.as_str();
        let mut from = 0;
        while let Some(c) = LATIN.captures(&text[from..]) {
            let all = c.get(0).expect("a match");
            let hit = c.get(1).expect("the pattern captures the abbreviation");
            hits.push(Hit {
                at: line.at(text, from + hit.start()),
                message: format!(
                    "Latin abbreviation `{}`. {}",
                    hit.as_str(),
                    english(hit.as_str())
                ),
            });
            // Resume at the last character of the match, so the character
            // after one abbreviation can open the next match.
            let end = from + all.end();
            from = text[..end]
                .char_indices()
                .next_back()
                .map_or(end, |(i, _)| i);
        }
    }
    hits
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

    // The cases of the quote-text, item-blocks, and list-items tests of
    // the bash prose-lint. Each test lists the findings that GFM reads.
    // pulldown-cmark reads three lines in another way, so their findings
    // are not in the lists:
    //
    // - The line of QUOTE_TEXT under the empty item at ` 1. ` and the
    //   line that holds only a tab. pulldown-cmark ends the item at the
    //   tab line and reads the line under it as code.
    // - The paragraph of an item in ITEM_BLOCKS after a table and a line
    //   of indented code in the item. pulldown-cmark ends the list and
    //   reads the paragraph as code.
    // - The paragraph of the item at `2.` under a footnote definition in
    //   LIST_ITEMS. pulldown-cmark reads the item as more text of the
    //   definition and the paragraph as code.
    const QUOTE_TEXT: &[&str] = &[
        "* * *",
        "  The rule above, e.g. here,",
        "     >     goes on, e.g. hourly.",
        "",
        "- - -",
        "  The rule above, e.g. here,",
        "    >     goes on, e.g. hourly.",
        "",
        "-   ",
        "    The job runs, e.g. nightly,",
        "       >     and again, e.g. hourly.",
        "",
        "The job runs, e.g. nightly,",
        "* ",
        "    >     and again, e.g. hourly.",
        "",
        "> -",
        ">   # The job runs, e.g. nightly.",
        ">     It reruns, e.g. hourly.",
        "",
        "- > -",
        "  >   # The job runs, e.g. nightly.",
        "  >     It reruns, e.g. hourly.",
        "",
        "-",
        "",
        "  The job runs, e.g. nightly,",
        "    >     and again, e.g. hourly.",
        "",
        "- > > The job runs, e.g. nightly,",
        "and lazily, e.g. at noon.",
        "    > -",
        "  >   # It reruns, e.g. hourly.",
        "  >     It runs again, e.g. at noon.",
        "",
        ">     ```sh",
        ">     make build",
        "    ```",
        "",
        "The job runs, e.g. nightly.",
        "",
        ">     ```sh",
        ">",
        "    make build",
        "    ```",
        "",
        "It reruns, e.g. hourly.",
        "",
        "1. The job runs.",
        "",
        "   - - -",
        "",
        "     It reruns, e.g. hourly.",
        "",
        "- The job runs.",
        "",
        "  * * *",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "- The job runs.",
        "  ```",
        "  make build",
        "  ```",
        "",
        "    It reruns, e.g. hourly.",
        "",
        " 1. ",
        "\t",
        "    The job runs, e.g. nightly,",
        "      >     Run the job, e.g. hourly.",
    ];
    const QUOTE_TEXT_WANT: &[(usize, usize)] = &[
        (2, 19),
        (3, 21),
        (6, 19),
        (7, 20),
        (10, 19),
        (11, 25),
        (13, 15),
        (15, 22),
        (18, 21),
        (19, 18),
        (22, 23),
        (23, 20),
        (27, 17),
        (28, 22),
        (30, 21),
        (31, 13),
        (33, 20),
        (34, 24),
        (40, 15),
        (47, 12),
        (53, 17),
        (59, 16),
        (66, 16),
    ];
    const ITEM_BLOCKS: &[&str] = &[
        "- The job runs, e.g. nightly.",
        "",
        "  ## Steps",
        "",
        "  [ref]: https://example.com",
        "and lazily, e.g. hourly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "1. The job runs.",
        "",
        "   ## Steps",
        "-",
        "  It runs, e.g. nightly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  | a | b |",
        "  | - | - |",
        "  > It runs, e.g. nightly,",
        "and lazily, e.g. hourly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  | a | b |",
        "  | - | - |",
        "      make build",
        "  It runs, e.g. nightly,",
        "and lazily, e.g. hourly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  | a | b |",
        "  | - | - |",
        "  ## Steps",
        "  It runs, e.g. nightly,",
        "and lazily, e.g. hourly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  ## Steps",
        "",
        "  It runs, e.g. nightly,",
        "===",
        "and lazily, e.g. hourly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  ## Steps",
        "",
        "  > It runs, e.g. nightly,",
        "  ===",
        "and lazily, e.g. hourly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  ## Steps",
        "",
        "  It runs, e.g. nightly | daily",
        "--|--",
        "and lazily, e.g. hourly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  ## Steps",
        "",
        "  > It runs, e.g. nightly | daily",
        "  --|--",
        "and lazily, e.g. hourly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  ## Steps",
        "",
        "  <div>",
        "1.",
        "   It runs, e.g. nightly,",
        "and lazily, e.g. hourly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  ## Steps",
        "",
        "  | a | b |",
        "  | - |",
        "      It runs, e.g. nightly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  ## Steps",
        "",
        "  ```sh",
        "  make build",
        "",
        "  make test",
        "  ```",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  ## Steps",
        "",
        "  <!--",
        "  A note.",
        "",
        "  -->",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  | a | b |",
        "  | - | - |",
        "",
        "  It runs, e.g. nightly,",
        "and lazily, e.g. hourly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  ## Steps",
        "",
        "  It runs, e.g. nightly | daily",
        "      --|--",
        "and lazily, e.g. hourly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  ## Steps",
        "",
        "  It runs, e.g. nightly.",
        "  --",
        "  It reruns, e.g. daily,",
        "and lazily, e.g. hourly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  ## Steps",
        "",
        "  It runs, e.g. nightly.",
        "-  ",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  ## Steps",
        "",
        "  ```sh",
        "  cat <<END",
        "port = 80",
        "END",
        "  ```",
        "",
        "    It reruns, e.g. hourly.",
    ];
    const ITEM_BLOCKS_WANT: &[(usize, usize)] = &[
        (1, 17),
        (6, 13),
        (8, 16),
        (16, 12),
        (18, 16),
        (26, 14),
        (27, 13),
        (29, 16),
        (38, 12),
        (39, 13),
        (50, 12),
        (51, 13),
        (53, 16),
        (61, 12),
        (63, 13),
        (65, 16),
        (73, 14),
        (75, 13),
        (77, 16),
        (85, 12),
        (87, 13),
        (89, 16),
        (97, 14),
        (99, 13),
        (101, 16),
        (111, 13),
        (112, 13),
        (114, 16),
        (124, 16),
        (126, 16),
        (140, 16),
        (153, 16),
        (162, 12),
        (163, 13),
        (165, 16),
        (173, 12),
        (175, 13),
        (177, 16),
        (185, 12),
        (187, 14),
        (188, 13),
        (190, 16),
        (198, 12),
    ];
    const LIST_ITEMS: &[&str] = &[
        "-",
        "  The job runs, e.g. nightly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "1.",
        "   The job runs, e.g. nightly.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs, e.g. nightly.",
        "[ref]: https://example.com",
        "",
        "  ## Steps",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs, e.g. nightly,",
        "  2.",
        "and lazily, e.g. hourly.",
        "",
        "  ## Steps",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "<br>",
        "- The job runs.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "| a | b |",
        "| - | - |",
        "- The job runs,",
        "and lazily.",
        "",
        "  ## Steps",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs",
        "      <!-- a",
        "note -->",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs",
        "      ```",
        "more",
        "      ```",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "-     <div>",
        "  The job runs,",
        "and lazily.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- <!-- note -->",
        "  The job runs,",
        "and lazily.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- - <!-- note -->",
        "      The job runs,",
        "and lazily.",
        "",
        "      It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "<div>",
        "<!-- a",
        "",
        "note -->",
        "- The job runs.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- <div>",
        "  <!-- a",
        "note -->",
        "- The job runs.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "Run the command",
        "    > make build",
        "<br>",
        "- The job runs.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "The job runs | daily",
        "| - | - | - |",
        "<br>",
        "- The job runs.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "[^1]: The note.",
        "2. The job runs,",
        "   <br>",
        "and lazily.",
        "",
        "    It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "  - | - |",
        "-|-",
        "",
        "      It reruns, e.g. hourly.",
        "",
        "The job runs.",
        "",
        "- The job runs.",
        "",
        "  ```sh",
        "  make build",
        "```",
        "",
        "    It reruns, e.g. hourly.",
    ];
    const LIST_ITEMS_WANT: &[(usize, usize)] = &[
        (2, 17),
        (4, 16),
        (9, 18),
        (11, 16),
        (15, 17),
        (20, 16),
        (24, 17),
        (26, 13),
        (30, 16),
        (36, 16),
        (47, 16),
        (55, 16),
        (64, 16),
        (72, 16),
        (80, 16),
        (88, 18),
        (98, 16),
        (107, 16),
        (116, 16),
        (125, 16),
        (142, 18),
    ];
    #[test]
    fn blocks() {
        struct Case {
            name: &'static str,
            lines: &'static [&'static str],
            want: &'static [(usize, usize)],
        }
        let cases = [
            Case {
                name: "quote text",
                lines: QUOTE_TEXT,
                want: QUOTE_TEXT_WANT,
            },
            Case {
                name: "item blocks",
                lines: ITEM_BLOCKS,
                want: ITEM_BLOCKS_WANT,
            },
            Case {
                name: "list items",
                lines: LIST_ITEMS,
                want: LIST_ITEMS_WANT,
            },
        ];
        for case in cases {
            let text = case.lines.join("\n") + "\n";
            assert_eq!(found(&text), case.want, "{}", case.name);
        }
    }

    #[test]
    fn code() {
        // The case of the exported-modes test of the bash prose-lint. The
        // `e.g.` in the fence does not draw a finding.
        let text = "# Title\n\nThe build failed, e.g. on a cache miss.\nIt stalled \u{2026} twice.\n\n```sh\necho \"fast, e.g. really \u{2026} and more\"\n```\n";
        assert_eq!(found(text), [(3, 19)]);
    }

    #[test]
    fn many_findings() {
        // The case of the exported-variables test of the bash prose-lint,
        // whose findings take more than 128 KiB.
        let text = "The cache may be stale, e.g. after a restart.\n\n".repeat(1500);
        let want: Vec<_> = (0..1500).map(|i| (2 * i + 1, 25)).collect();
        assert_eq!(found(&text), want);
    }
}
