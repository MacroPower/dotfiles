//! Overview
//!
//! Reports two lowercase words joined by a slash in markdown prose and in
//! source comments (`read/write`, `client/server`). The slash can mean
//! `and`, `or`, or `per`, so the reader must pick one, and the
//! technical-writing skill bans it. The fix joins the words with `and` or
//! `or`, or sets a path or a name in a code span. The rule sits at
//! Recommended, because a pair that names one thing also matches.
//!
//! Expression
//!
//! The check reads the prose of each line of the markdown model. Code
//! spans and link destinations are blank there, so a match inside one
//! passes. `PAIR` matches two runs of lowercase letters joined by a
//! slash. It also reads the characters on each side of the pair, so the
//! check can skip a path, a URL, or a name.
//!
//! `MEDIA` holds every top-level media type that the Internet Assigned
//! Numbers Authority (IANA) registers, and `KINDS` holds the endings that
//! mark two joined words. The media guard skips each pair that `MEDIA`
//! matches and `KINDS` does not. Two joined words therefore pass when the
//! first is a top-level type and `KINDS` lacks the second (`model/view`).
//!
//! False Positives
//!
//! Paths and URLs pass. A slash before or after the pair marks one
//! (`/etc/hosts`, `a/b/c`), and so does a `.`, `~`, `:`, or `@` before
//! the pair or a file extension after it. A capitalized pair passes too
//! (`I/O`, `CI/CD`, `TCP/IP`).
//!
//! Names pass as well. A quote, a `[`, or an `=` before the pair marks a
//! quoted value, a link, or an attribute (`"text/plain"`,
//! `[database/sql]`, `targetDir="images/qr"`). A pair before `package`
//! names an import path (`the net/http package`), and a pair that opens
//! with `golang/` or `kubernetes/` names a repository.
//!
//! A pair that opens with a top-level media type passes as a media type
//! (`text/html`, `message/http`). When such a pair ends in another
//! top-level type or in `binary`, it joins two words and still reports
//! (`audio/video`, `text/binary`). The type `example` is the exception.
//! IANA registers it as a subtype of other types, so `text/example`
//! passes.
//!
//! Related Rules
//!
//! ProseAndOr reports `and/or`, so this check skips the phrase.
//!
//! Measurement
//!
//! The rule hit 162 lines in the measure.py corpus, and 17 of 20 sampled
//! hits joined two words that `and` or `or` can join. Each of the three
//! misfires named one thing. They were a Hugo edition
//! (`extended/deploy`), the shorthand `k/k` for the kubernetes
//! repository, and a namespace and name in a table.
//!
//! measure.py reads configs/harper/comment-checks.txt too and skips the
//! Go comments of the corpus, so every hit is in a markdown file.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::markdown::Markdown;

fn re(pattern: &str) -> Regex {
    Regex::new(pattern).expect("the pattern is valid")
}

static PAIR: LazyLock<Regex> = LazyLock::new(|| {
    re(
        r#"(?:^|[^A-Za-z0-9_./~:@"'=\[-])([a-z]+/[a-z]+)(?:[^A-Za-z0-9_/.-]|\.(?:[^A-Za-z0-9]|$)|$)"#,
    )
});
static OWNER: LazyLock<Regex> = LazyLock::new(|| re("^(?:golang|kubernetes)/"));
static MEDIA: LazyLock<Regex> = LazyLock::new(|| {
    re("^(?:application|audio|example|font|haptics|image|message|model|multipart|text|video)/")
});
static KINDS: LazyLock<Regex> = LazyLock::new(|| {
    re("/(?:application|audio|font|haptics|image|message|model|multipart|text|video|binary)$")
});
static PACKAGE: LazyLock<Regex> = LazyLock::new(|| re(r"^[ \t]+packages?(?:[^a-z]|$)"));

/// Returns a finding for the first pair of words joined by a slash on
/// each line.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .lines
        .iter()
        .filter_map(|line| {
            let text = line.text.as_str();
            let mut from = 0;
            while let Some(c) = PAIR.captures(&text[from..]) {
                let pair = c.get(1).expect("the pattern captures the pair");
                let hit = pair.as_str();
                let end = from + pair.end();
                if hit != "and/or"
                    && !OWNER.is_match(hit)
                    && !(MEDIA.is_match(hit) && !KINDS.is_match(hit))
                    && !PACKAGE.is_match(&text[end..])
                {
                    return Some(Hit {
                        at: line.at(text, from + pair.start()),
                        message: format!(
                            "Slash in `{hit}`. Join the words with `and` or `or`, or set a path or name in a code span."
                        ),
                    });
                }
                from = end;
            }
            None
        })
        .collect()
}
