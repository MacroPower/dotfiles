# prose-lint

`prose-lint` lints markdown files, commit messages, and the comments of
source files with the Weir rules and the Harper built-ins of `rules`, and with
the native checks of `src/checks`. The binary embeds the rules when it builds,
so a change to a rule takes a new build. `CLAUDE.md` describes how to write and
measure a rule.

```sh
prose-lint [--only RULES] [--json] <file>    # lint a markdown file or a source file
prose-lint [--only RULES] [--json] --commit  # lint a commit message read from stdin
prose-lint --version                         # print the harper-core version
```

Each finding prints as `path:line:col: [Tier] Kind::Rule: message`, with the
Required findings first and then by line. The findings of a commit message
name the path `<stdin>`. When two tiers of one rule report one line with one
message, only the finding of the higher tier prints.

Two flags change which rules run and how the findings print:

- `--only RULES` runs only the rules that the comma-separated list names, such
  as `ProseHedge,ProseArrow`. A bare name runs the rule in each tier that holds
  it, and `<tier>/<Name>`, such as `optional/ProseHedge`, runs the rule of one
  tier. The binary exits 2 when an entry does not name any Weir rule, built-in,
  or native check.
- `--json` prints each finding as one line of JSON, an object with the fields
  `path`, `line`, `col`, `tier`, `kind`, `rule`, and `message`.

`measure.py` lints its corpus with both flags.

The prose hook of `home/claude.nix` lints each file whose extension the binary
knows. `passthru.extensions` of `package.nix` reads those extensions from
`MARKDOWN_EXTENSIONS` in `src/files.rs` and `SYNTAXES` in `src/comments.rs`.

The binary exits with one of these statuses:

| Status | Meaning                                                       |
| ------ | ------------------------------------------------------------- |
| 0      | No finding, or a file that the binary skips                   |
| 1      | One or more findings                                          |
| 2      | The linter could not run, such as on a file that is not UTF-8 |

The binary skips a missing file, a file whose extension it does not know, and a
path under `node_modules`, `vendor`, `.git`, `.terraform`, or
`prose-lint/fixtures`. Changelogs skip the time-sensitive rules and
ProseHeading, and prompt files for an agent skip ProseCapsEmphasis, source files
under the same paths included. Commit messages skip the time-sensitive rules
too. `TIME_SENSITIVE` in `src/rules.rs` lists those rules.

## Source Files

The binary lints each comment of a source file as a markdown region. `SYNTAXES`
in `src/comments.rs` maps each extension that the binary knows to the way it
finds the comments of the file. The module doc of that file states which lines
form one comment and how a region drops the comment markers.

- A tree-sitter grammar finds the comments of most languages, the comments that
  follow code included. The grammars are the versions that harper-comments
  3.3.0 links.
- Python, HCL, YAML, Weir, and awk have no such grammar, so a scanner finds
  their full-line `#` comments. It also reads each Python docstring and each
  HCL `description` and `error_message` string.

Each character of a region keeps its source position, so a finding reports the
line and the column of the source file. A phrase that wraps onto the next
comment line matches, and a code span or a fence in a comment reads as code.
Harper reads all regions of a file as one document, and a paragraph break
between two regions keeps a phrase from running from one comment into the next.

The doc comments of Java, JavaScript, TypeScript, and Haskell hold markup that
markdown misreads. The region drops Javadoc and JSDoc block tags and the HTML
tags that open a paragraph. It turns other markup into code spans and blanks of
the same width, and it drops the blanks that open a line. So markup at the start
of a paragraph does not indent the paragraph into a code block. Each character
that the region keeps holds its own column. The module doc of
`src/comments/markup.rs` lists the markup of each language:

- Javadoc and JSDoc: block tags such as `@param name`, inline tags such as
  `{@code x}`, and HTML entities such as `&lt;`. Each block tag opens a
  paragraph, so a run of tags does not read as one long sentence. A line of a
  `/**` comment that opens with the tag of an HTML block element, such as `<p>`
  or `<li>`, also opens a paragraph. Markdown otherwise reads the line as an
  HTML block and skips its prose up to the next blank line. When a tag such as
  `<li>` or `@return` fills its line, the indented text under it reads as prose.
  Each `<pre>` block stays code, and so do the tags of a plain `/* */` comment.
- Haddock: code blocks that open with a line of `@`, examples (`>>>`), properties
  (`prop>`), and bird tracks (`>`). Code between two `@` marks and identifiers
  in quotes (`'Maybe'`) read as code spans.

The native checks that `comment-checks.txt` names run on each
region, other than the checks that the file skips for a comment kind.
ProseTypography reads the whole text of a source file, strings included, other
than a Weir rule file, which holds each character that it reports. The
binary does not report a finding on the license header of a file.

## Native Checks

A native check is a rule in Rust, for a pattern that a Weir rule cannot
express. Each check reads the markdown model of `src/markdown.rs`, which
pulldown-cmark builds. The model gives the check the blocks of the text and the
prose of each line. Code, HTML, link reference definitions, and link
destinations are blank in that prose, and each character keeps its column.
ProseTypography also reads the raw text of a source file.

To add a native check:

1. Write the check as a function that takes the model and returns a `Hit` for
   each finding, in `src/checks/<tier>/<name>.rs`, such as
   `src/checks/required/prose_arrow.rs`. Name the module in
   `src/checks/<tier>.rs`.
2. Open the module with a `//!` comment in the layout of the Comments section
   of `CLAUDE.md`.
3. List the check in `CHECKS` of `src/checks.rs` with its rule name, its tier,
   and the kind of its findings.
4. Put its `.bad.md` and `.good.md` fixtures under `fixtures/<tier>/`. The
   tests of [Rule Tests](#rule-tests) run the check on them.

A native check can read a line of a fixture in another way than GFM does. The
model blanks the text of an HTML block, for example, and pulldown-cmark ends an
HTML block such as `<pre>` only at the end tag of the same name. A line of a bad
fixture that ends in `<!-- native check differs -->` holds such a case, and the
fixture test does not judge a native check on that line.

The Weir rules read a copy of the text in which each curly apostrophe between
two letters is straight. The native checks read the text as the file holds it,
so ProseTypography reports the curly apostrophe.

## Rule Tests

`cargo test` runs the tests of `src/rule_tests.rs` on every Weir rule,
built-in, and native check. The module doc of that file states what each rule
must pass:

- The binary reports each bad line of the fixtures of the rule and no line of
  its good fixture or of `fixtures/common.good.md`. A copy of each fixture with
  CRLF line endings draws the same findings.
- Each rule has a fixture pair, and each fixture pair has a rule. Each entry of
  `rules` is a tier directory, and each file of a tier directory is a Weir rule
  or `builtins.txt`.
- Each built-in sits in one tier, and two tiers of one rule do not report the
  same line with other messages.
- The tests inside each Weir rule pass, as `harper-cli test` runs them.
- Each phrase of plain words that the main expression of a Weir rule lists,
  directly or through an `@name` list, fires on its own in a probe sentence. The
  test skips each phrase that holds a tag of the tagger, such as `DET`, or a
  character other than a letter, an apostrophe, or a space. No probed phrase
  holds a shorter probed phrase of the same rule.
- The comment that opens each Weir rule and the doc comment of each native
  check follow the layout of the Comments section of `CLAUDE.md`. These
  comments do not draw a Required or a Recommended finding.

## Building

To build the package, run `nix build .#prose-lint` from the root of the
repository. The package links the harper of `pkgs.harper`, with the patches
and the embedded dictionary of `harperOverlay` in `lib/default.nix`.

The crate depends on `harper-core` by path, because the patch of
`harperOverlay` changes how `harper-core` loads its dictionary. Cargo reads the
crate from `./harper`, which git ignores. To build the crate outside of Nix, run
these commands in `tools/prose-lint`:

```sh
git clone --depth 1 --branch v3.3.0 https://github.com/Automattic/harper.git harper
git -C harper apply ../../../pkgs/harper-precomputed-dict.patch
cargo build
```

This build does not embed the dictionary, so the binary builds the dictionary
from its rune files on each start.

## Harper Version

The crate must link the harper version of `pkgs.harper`. Rule authors test
rules with `harper-cli test`, so both tools must read a rule the same way. The
Nix build fails when the version of `harper-core` in `Cargo.lock` differs from
`pkgs.harper.version`.

When nixpkgs bumps harper, follow these steps:

1. Clone the new tag into `./harper` and apply the patch.
2. Set the new version on the `harper-core` dependency in `Cargo.toml`.
3. Run `cargo build` to update `Cargo.lock`.
