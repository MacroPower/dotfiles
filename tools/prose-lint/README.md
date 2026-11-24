# prose-lint

`prose-lint-rs` lints markdown files and commit messages with the Weir rules
and the Harper built-ins of `configs/harper/rules`, and with the native checks
of `src/checks`. The binary embeds the rules when it builds, so a change to a
rule takes a new build.

```sh
prose-lint-rs <file>     # lint a markdown file
prose-lint-rs --commit   # lint a commit message read from stdin
prose-lint-rs --version  # print the harper-core version
```

Each finding prints as `path:line:col: [Tier] Kind::Rule: message`, with the
Required findings first and then by line. The findings of a commit message
name the path `<stdin>`. When two tiers of one rule report one line with one message, only
the finding of the higher tier prints.

The binary exits with one of these statuses:

| Status | Meaning                                                                                                                 |
| ------ | ----------------------------------------------------------------------------------------------------------------------- |
| 0      | No finding, or a file that the binary skips                                                                             |
| 1      | One or more findings                                                                                                    |
| 2      | The linter could not run, such as on a file that is not UTF-8, or on a source file, whose comments it does not lint yet |

The binary skips a missing file, a file whose extension it does not know, and a
path under `node_modules`, `vendor`, `.git`, `.terraform`, or `harper/fixtures`.
Changelogs skip the time-sensitive rules and ProseHeading, and prompt files for
an agent skip ProseCapsEmphasis. Commit messages skip the time-sensitive rules
too. `TIME_SENSITIVE` in `src/rules.rs` lists those rules.

## Native Checks

A native check is a rule in Rust, for a pattern that a Weir rule cannot
express. Each check reads the markdown model of `src/markdown.rs`, which
pulldown-cmark builds. The model gives the check the blocks of the text and the
prose of each line. Code, HTML, link reference definitions, and link
destinations are blank in that prose, and each character keeps its column.

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
   `configs/harper/fixtures/<tier>/`. The `fixtures` test of `src/checks.rs`
   runs each check on its fixtures with the pass criteria of
   `configs/harper/check.py`. It also expects the same findings from a copy of
   each fixture with CRLF line endings.

A native check and the awk check that it replaces can read a line in different
ways, and some fixtures hold such a line. The awk checks read the text of an
HTML block as prose, but the model blanks it. pulldown-cmark also reads some
lines in another way than GFM does. For example, it ends an HTML block such as
`<pre>` only at the end tag of the same name. A fixture line that ends in
`<!-- native check differs -->` holds such a case, and the `fixtures` test does
not judge a native check on that line. The awk checks still read the line, so
`check.py` judges them on it.

The Weir rules read a copy of the text in which each curly apostrophe between
two letters is straight. The native checks read the text as the file holds it,
so ProseTypography reports the curly apostrophe.

## Building

To build the package, run `nix build .#prose-lint-rs` from the root of the
repository. The package links the harper of `pkgs.harper`, with the patches
and the embedded dictionary of `harperOverlay` in `lib/default.nix`.

The crate depends on `harper-core` and `harper-comments` by path, because
crates.io does not hold `harper-comments` at the version of `pkgs.harper`.
Cargo reads both crates from `./harper`, which git ignores. To build the crate
outside of Nix, run these commands in `tools/prose-lint`:

```sh
git clone --depth 1 --branch v3.3.0 https://github.com/Automattic/harper.git harper
git -C harper apply ../../../pkgs/harper-precomputed-dict.patch
cargo build
```

`build.rs` reads the rules from `configs/harper`, and the `PROSE_LINT_HARPER`
variable names another directory with the same `rules` and `fixtures`
directories.

This build does not embed the dictionary, so the binary builds the dictionary
from its rune files on each start.

## Harper Version

The crate must link the harper version of `pkgs.harper`. Rule authors test
rules with `harper-cli test`, so both tools must read a rule the same way. The
Nix build fails when the version of `harper-core` in `Cargo.lock` differs from
`pkgs.harper.version`.

When nixpkgs bumps harper, follow these steps:

1. Clone the new tag into `./harper` and apply the patch.
2. Set the new version on both dependencies in `Cargo.toml`.
3. Run `cargo build` to update `Cargo.lock`.
