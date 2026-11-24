# prose-lint

`prose-lint-rs` lints markdown files, commit messages, and the comments of
source files with the Weir rules and the Harper built-ins of
`configs/harper/rules`, and with the native checks of `src/checks`. The binary
embeds the rules when it builds, so a change to a rule takes a new build.

```sh
prose-lint-rs [--only RULES] [--json] <file>    # lint a markdown file or a source file
prose-lint-rs [--only RULES] [--json] --commit  # lint a commit message read from stdin
prose-lint-rs --version                         # print the harper-core version
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

`configs/harper/measure.py` lints its corpus with both flags.

The binary exits with one of these statuses:

| Status | Meaning                                                       |
| ------ | ------------------------------------------------------------- |
| 0      | No finding, or a file that the binary skips                   |
| 1      | One or more findings                                          |
| 2      | The linter could not run, such as on a file that is not UTF-8 |

The binary skips a missing file, a file whose extension it does not know, and a
path under `node_modules`, `vendor`, `.git`, `.terraform`, or `harper/fixtures`.
Changelogs skip the time-sensitive rules and ProseHeading, and prompt files for
an agent skip ProseCapsEmphasis, source files under the same paths included.
Commit messages skip the time-sensitive rules too. `TIME_SENSITIVE` in
`src/rules.rs` lists those rules.

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

The native checks that `configs/harper/comment-checks.txt` names run on each
region, other than the checks that the file skips for a comment kind.
ProseTypography reads the whole text of a source file, strings included, other
than a Weir or awk rule file, which holds each character that it reports. The
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
   of `configs/harper/CLAUDE.md`. To port an awk rule, keep its comment, leave
   out the `Output format` lines, and write the Expression section for the Rust
   code.
3. List the check in `CHECKS` of `src/checks.rs` with its rule name, its tier,
   and the kind of its findings.
4. Put its `.bad.md` and `.good.md` fixtures under
   `configs/harper/fixtures/<tier>/`. The tests of [Rule Tests](#rule-tests)
   run the check on them.

A native check and the awk check that it replaces can read a line in different
ways, and some fixtures hold such a line. The awk checks read the text of an
HTML block as prose, but the model blanks it. pulldown-cmark also reads some
lines in another way than GFM does. For example, it ends an HTML block such as
`<pre>` only at the end tag of the same name. A fixture line that ends in
`<!-- native check differs -->` holds such a case, and the fixture test does not
judge a native check on that line. The awk checks still read the line, so
`check.py` judges them on it.

The Weir rules read a copy of the text in which each curly apostrophe between
two letters is straight. The native checks read the text as the file holds it,
so ProseTypography reports the curly apostrophe.

## Rule Tests

`cargo test` runs the tests of `src/rule_tests.rs` on every Weir rule,
built-in, and native check. The module doc of that file states what each rule
must pass:

- The binary reports each bad line of the fixtures of the rule and no line of
  its good fixture or of `configs/harper/fixtures/common.good.md`. A copy of
  each fixture with CRLF line endings draws the same findings.
- Each rule has a fixture pair, and each fixture pair has a rule. An awk check
  under `configs/harper/rules` that no native check replaces keeps its
  fixtures, and `configs/harper/check.py` runs it on them.
- Each built-in sits in one tier, and two tiers of one rule do not report the
  same line with other messages.
- The tests inside each Weir rule pass, as `harper-cli test` runs them.
- Each phrase of plain words that the main expression of a Weir rule lists,
  directly or through an `@name` list, fires on its own in a probe sentence. The
  test skips each phrase that holds a tag of the tagger, such as `DET`, or a
  character other than a letter, an apostrophe, or a space. No probed phrase
  holds a shorter probed phrase of the same rule.
- The comment that opens each Weir rule and the doc comment of each native
  check follow the layout of the Comments section of
  `configs/harper/CLAUDE.md`.

## Building

To build the package, run `nix build .#prose-lint-rs` from the root of the
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

`build.rs` reads the rules from `configs/harper`, and the `PROSE_LINT_HARPER`
variable names another directory with the same `rules` and `fixtures`
directories and the same `comment-checks.txt` file.

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
