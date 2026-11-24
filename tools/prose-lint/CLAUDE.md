# Prose Rules

This file covers the rules of the prose linter, `prose-lint`. Its paths are
relative to `tools/prose-lint`, and its commands run from the repository root
unless a command says otherwise. `rules/<tier>/` holds each Weir rule as a
`.weir` file and a `builtins.txt` that names the Harper built-ins of the tier.
A rule that a Weir pattern cannot express is a native check, a Rust module
under `src/checks/<tier>/`.

`fixtures/<tier>/` holds the `.bad.md` and `.good.md` pair of each rule.
`cargo test` tests each rule against its fixtures, as the Rule Tests section of
`README.md` describes. A fixture line that ends in
`<!-- native check differs -->` holds a case that a native check reads in
another way than GFM. The Native Checks section of `README.md` describes the
marker.

## Names

Rule names are `Prose` plus one term. Each rule's message opens with a
label that states the same term in words (`ProseBroadReference` and `Broad
reference.`). The term must be one of these:

- A term that grammar handbooks or style guides use, in the sense they
  use it: `ProsePassive`, `ProseNominalization`, `ProseExpletive`.
- A plain compound of everyday words: `ProseClosingCliche`,
  `ProseVagueCriterion`, `ProseOpenList`.
- The word or phrase the rule flags: `ProseSeam`, `ProseServesAs`,
  `ProseInOrderTo`.

Never coin a term that sounds technical but appears in no grammar or
style guide. Past rules carried `Certificate`, `Puff`, `Delta tense`,
`Pivot`, `ArticleRun`, `StockWord`, and `Bare this`. Each one made the
reader learn a new word before acting on the finding. When no standard
term fits, write the plain compound, even when it runs long.

The term must fit every word the rule flags. `ProseDemonstrative` also
flagged `which`, a relative pronoun, so it became `ProseBroadReference`.

Each label must be unique. Only the tiers of one rule share a name and
a label (the Recommended and Optional `ProseHedge`).

## Messages

Wrap every word that a message or a description mentions in backticks,
so the reader can tell the word apart from the sentence around it.
Quote each flagged word, each replacement, each example phrase, and
each part of a native check's message that prints the matched text. A label that
is the flagged word itself is a lowercase code span (`` `seam`. ``),
and a label that names a grammar term stays plain (`Passive voice.`).

## Lists

When an `expr` list does not fit on one line, give each entry a line
of its own. A diff of the rule then shows each entry that changes.

```text
expr main [
    (rich set),
    (wide range),
    numerous
]
```

Harper ends a statement at each newline outside a `[...]` of the
statement. A `<...>` or a `(...)` that spans lines therefore needs a
`[` that stays open across each of its line breaks. `plain_entries` in
`src/rule_tests.rs` reads a statement over several lines in the same
way.

## Comments

Write each rule's comment for an engineer with no linguistics
background. Every `.weir` file opens with a comment, and every native
check module opens with a `//!` doc comment. Every comment uses one
layout. Readers then find the fix, the words that a rule leaves out, and
its precision in the same place in each file.

The prose hook lints the comments of `.weir` and `.rs` files, so every
comment must also pass the prose rules. The hook lints only files that
Write and Edit touch, so run `prose-lint <file>` on each rule file you
edit through Bash. A new or changed rule can also report a phrase in
the comment of a rule file that you did not edit. The `rule_comments`
test in Verification lints every rule file, so rewrite each comment
that it reports in the same change.

The `layouts` test in Verification checks the comment of every rule file
against the layout in Sections. It prints one line for each breach, and
`layout_failures` in `src/rule_tests.rs` lists the breaches.

### Sections

Every comment holds these sections in this order.

- **Overview** says what the rule flags and gives an example. It
  defines each grammar term in the name or the label in plain words.
  It then states what the flagged text costs the reader, the fix, and
  why the rule sits in its tier.
- **Expression** says how to read the pattern: each branch, each
  guard, and each Weir or tagger workaround. For a native check, it
  says how the check reads the markdown model and which shared module
  of `src/checks`, such as `sentences.rs`, it calls.
- **False Positives** names each word and each sense that the rule
  leaves out because the text does not need the fix, with the reason
  and an example. It also names each misfire that remains and that the
  measured sample did not find.
- **Related Rules** names each rule that reports a neighboring phrase
  and the guard that keeps the two rules off the same words. A word
  that the rule leaves out because another rule reports it sits in
  this section and not in False Positives. The section also states the
  split between the tiers of one rule name.
- **Measurement** gives the hits and the sampled precision that the
  Measurement section of this file requires, and it says what the
  sampled misfires were.

`SECTIONS` in `src/rule_tests.rs` holds the section names in the same order,
and `REQUIRED_SECTIONS` holds the sections that every comment must carry. When you add, rename, reorder, or require a
section, change this file, both constants, and the cases in
`rule_tests.rs` together.

Each heading is a comment line that holds only the section name. One
blank comment line follows each heading, and one separates each section
from the heading after it. The last section ends at the first line of
code, and no blank comment line sits between the two.

```text
# Overview
#
# `and/or` leaves the reader to work out which combinations the
# sentence allows (`Pass the host and/or the port`). The fix writes
# out each combination (`the host, the port, or both`). The rule sits
# at Required, because it matches one exact phrase and the fix is
# mechanical.
#
# Related Rules
#
# ProseSlash reports two lowercase words joined by a slash and skips
# `and/or`, so only this rule reports the phrase.
#
# Measurement
#
# The rule hit 61 lines in the measure.py corpus, and in 20 of 20
# sampled hits `and/or` joined alternatives in a sentence.
```

The `layouts` test also reads a lone comment line as a heading when the
line starts one space after the marker and does not end in a period. A lone line is
a paragraph of one line. Each side of it holds a blank comment line or
an end of the comment. The first line of the comment and the line
directly above the code can therefore be lone lines.

The test fails a lone line whose name `SECTIONS` lacks, such as
`Related rules` or `Notes`. End every paragraph of one line with a
period. An example line that you indent further does not need the
period.

Every comment carries Overview and Measurement, and the test fails a
comment that lacks either one. When a rule does not hit any line in the
corpus, write the sentence that the Measurement section of this file
gives for that case. Leave out any other section that has nothing to
say, and keep the remaining sections in order. Never write a
placeholder sentence such as `No related rules.`, because the prose
rules reject it as filler.

The doc comment of a native check uses the same section names, with `//!`
in place of `#`, and its Expression section covers the Rust code.

These rule files show the layout:

- `rules/required/ProseAndOr.weir` is a rule with a short comment.
- `rules/recommended/ProseBroadReference.weir` is a Weir rule with
  guards, related rules, and a measurement.
- `src/checks/recommended/prose_hidden_list.rs` is a native check.

### Misfires and Misses

A misfire is a finding on text that does not need the fix. A miss is
text that needs the fix and still passes the rule. Give the detail of
each one in exactly one section.

| Case                                     | Section                      |
| ---------------------------------------- | ---------------------------- |
| A misfire that the measured sample found | Measurement, with its count  |
| Every other misfire that remains         | False Positives              |
| A miss that a guard lets pass            | Expression, beside the guard |

The Overview can also name a misfire in one clause as the reason for
the tier. The section from the table then holds the example of that
misfire, and the count of a sampled misfire.

The filter in `ProseJust` needs a word before `just`, so `a sentence
that opens with Just passes` sits in Expression beside that filter.

### References

- Name another rule by its rule name (`ProseFiller`), never by a
  description (`the filler rule`). One search for the name then finds
  every reference when you rename the rule. Write the name without
  backticks. For one tier of a name that two tiers share, put the tier
  first (`the Optional ProseHedge`).
- Name the skill (`the technical-writing skill`, `the prose skill`),
  never `the skill` alone.
- Name a module of the crate by its path (`src/markdown.rs`), and never
  call it `the reader`. In a comment, `the reader` is the person who
  reads the prose, so write `src/markdown.rs blanks code spans`.

### Grammar Terms

- When the name or the label uses a grammar term, open the Overview
  with a plain definition and an example. ProseIndefiniteOpener opens
  with "Grammar calls `a` and `an` the indefinite article, because each
  introduces a noun without pointing at a particular one."
- Define every other grammar term at its first use in each file, or
  replace it with a plain phrase. Readers open one rule file at a time,
  so a definition in another file does not reach them.
- Reuse the glosses below, so one term reads the same in every file.

| Term        | Gloss                                                                                            |
| ----------- | ------------------------------------------------------------------------------------------------ |
| determiner  | a word that introduces a noun, such as `the`, `a`, or `this`                                     |
| auxiliary   | a helping verb such as `is`, `has`, or `can`                                                     |
| modal verb  | a helping verb that marks possibility, ability, or obligation, such as `can`, `might`, or `must` |
| imperative  | the verb form that gives a command, such as `run` in `run make`                                  |
| participle  | the `-ing` or `-ed` form of a verb, such as `returning` or `cached`                              |
| gerund      | an `-ing` form used as a noun                                                                    |
| progressive | a form of `be` plus an `-ing` verb                                                               |
| noun phrase | a noun and the words attached to it                                                              |
| proper noun | a name that takes a capital letter, such as `Kubernetes`                                         |
| adverb      | a word that describes a verb, an adjective, or a whole sentence, such as `safely` or `also`      |
| preposition | a word that ties a noun to the rest of the sentence, such as `of`, `with`, or `by`               |
| conjunction | a word that joins two words or two clauses, such as `and`, `but`, or `because`                   |
| clause      | a subject and its verb, such as `the tests pass`                                                 |
| agent       | the actor that a passive sentence names after `by`                                               |

School grammar teaches `noun`, `verb`, `adjective`, `pronoun`,
`subject`, and `object`. These terms count as everyday words and do
not need a gloss.

Replace these terms with the plain phrase instead of defining them:

| Term           | Plain phrase                   |
| -------------- | ------------------------------ |
| copula         | a form of `be`                 |
| inflections    | its forms for tense and person |
| predicate noun | a noun after `is`              |
| finite verb    | a verb that carries a tense    |

### Weir Terms

Many rule files explain the same Weir behavior. Write the sentence from
this table. Put the rule's own words in place of `need` and `seam`, and
fit `the filter`, `the guard`, and `the branch` to the rule, such as
`each guard` or ``the `which` branches``.

| Behavior               | Sentence                                                                                                          |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------- |
| No lookbehind          | Weir has no lookbehind, so the filter consumes the word before `need` and narrows the finding back to `need`.     |
| Whitespace tokens      | A space in a pattern is a whitespace token, so the guard sits glued to `seam` and reads the token right after it. |
| Longest match          | A list picks its longest match and never backtracks, so each gap length is its own alternative.                   |
| Negation after a space | A negated word after a space needs a word to consume, so the branch has a twin for the end of a clause.           |
| Sentence scope         | By default, a Weir match stops at a comma, so `let scope "Sentence"` lets the branch cross one.                   |

Harper's part-of-speech tagger marks each word with a tag for its word
class, and a pattern names a tag in capitals. Write `Harper's
part-of-speech tagger` at the first mention in a file and `the tagger`
after that. Set each tag in backticks, and gloss it at its first use in
a file with the phrase from this table.

| Tag     | Gloss                                                           |
| ------- | --------------------------------------------------------------- |
| `ADJ`   | an adjective                                                    |
| `ADP`   | a preposition                                                   |
| `ADV`   | an adverb                                                       |
| `AUX`   | a helping verb                                                  |
| `CCONJ` | a conjunction that joins two equal parts, such as `and` or `or` |
| `DET`   | a determiner                                                    |
| `INTJ`  | an exclamation, such as `oh`                                    |
| `NOUN`  | a noun                                                          |
| `NUM`   | a number word, such as `three`                                  |
| `PART`  | a short function word, such as `not` or the `to` before a verb  |
| `PRON`  | a pronoun                                                       |
| `PROPN` | a proper noun                                                   |
| `PUNCT` | a punctuation mark                                              |
| `SCONJ` | a conjunction that opens a clause, such as `because`            |
| `SYM`   | a symbol                                                        |
| `VERB`  | a verb                                                          |
| `PROG`  | the `-ing` form of a verb                                       |

`PROG` is a Weir keyword, and the tagger does not assign it. The tagger
tags only words, so `PUNCT` and `SYM` do not match a punctuation mark
or a symbol in the text, and `NUM` does not match digits.

Some glosses use a term from the Grammar Terms table, and that term
still needs its own gloss at its first use in the file.

## Renames

To rename a rule:

1. Rename the rule file and both fixtures with `git mv`.
2. For a native check, rename its module and update the name in `CHECKS`
   of `src/checks.rs`.
3. Update the label at the start of the message.
4. Update the name in `comment-checks.txt` and in `src`. The crate names some
   rules for commit messages, such as `TIME_SENSITIVE` in `src/rules.rs`, for
   agent prompt files in `src/files.rs`, and for the findings that
   `src/guards.rs` drops after Harper reports them.
5. Search `rules` and the `configs/claude` directory of the repository for the
   old rule name, and update each reference. Rule comments name a rule only by
   its rule name. Messages and skills can state the old term in words
   (`stock word`), so search for those words too.

## Measurement

`corpus.json` pins the measurement corpus: the kubernetes, nix.dev,
Rust book, and Hugo docs, and the comments of ten Go standard library
packages. `measure.py` lints each file of the corpus with
`prose-lint --only <rules> --json`. It prints the hits for each source
and a seeded sample of hits.

`prose-lint` runs the Weir rules, the Harper built-ins that the
`builtins.txt` of a tier lists, and the native checks of `src/checks`. It
fails on a name that none of these hold. The binary embeds the rules when it builds, so build it from the
branch that holds the rules you measure. On that branch, make the change
in the file that the binary reads:

- For a Weir rule, add or change its `.weir` file.
- For a native check, change its module.
- For a Harper built-in that no tier lists, add its name to the
  `builtins.txt` of a tier.

The flake build reads only the files that git tracks, so `git add` a new
rule file before you build. The `.#prose-lint` build also runs the
rule tests. They fail on a rule without a fixture pair, without the
Overview and Measurement sections in its comment, or with a finding on
`fixtures/common.good.md`. A draft rule or a candidate built-in lacks
these, so build the binary for it without the tests:

```bash
export PROSE_LINT="$(nix build --no-link --print-out-paths --impure --expr 'let f = builtins.getFlake "git+file://${toString ./.}"; in f.packages.${builtins.currentSystem}.prose-lint.overrideAttrs { doCheck = false; }')/bin/prose-lint"
```

`measure.py` runs the binary that `PROSE_LINT` names, and it looks for
`prose-lint` on `PATH` when the variable is unset. The `prose-lint` on
`PATH` embeds the rules of the configuration that the host runs, so set
the variable to measure a branch. A rule that passes its tests can use
the plain build:

```bash
export PROSE_LINT="$(nix build --no-link --print-out-paths .#prose-lint)/bin/prose-lint"
python3 tools/prose-lint/measure.py fetch
python3 tools/prose-lint/measure.py run ProseHedge
python3 tools/prose-lint/measure.py run optional/ProseHedge --sample 20
```

A bare rule name, such as `ProseHedge`, runs the rule in every tier.
The binary then drops a hit of a lower tier when a higher tier reports
the same line with the same message, as the hook does. To count every
hit of one tier, name the rule as `<tier>/<Rule>`, such as
`optional/ProseHedge`. When you name two or more rules, `measure.py`
also prints the hits of each name, zero included.

Judge each sampled hit by hand, and count the hits that the rule
reports correctly. Each new or changed rule cites the hits and the
sampled precision in the Measurement section of its comment:

```text
The rule hit 57 lines in the measure.py corpus, and 16 of 16 sampled
hits were conjunctions.
```

The counts in every rule comment come from the `measure.py` corpus.
When a rule does not hit any line in the corpus, its comment says that
no sample measures the precision:

```text
The rule did not hit any line in the measure.py corpus, so no sample
measures its precision.
```

## Measured and Rejected

These candidates fired mostly on clear prose when matched by words and
part-of-speech tags, or their fix did not make the text clearer. The
counts come from measure.py with harper-cli 2.12.0 on corpus
d23da847be27. Measure a candidate again before reviving it.

- Stacked prepositions (`the number of devices of this type`): 25
  hits, and about 5 of 25 slowed a reader.
- Reduced relatives with `by` (`the URL returned by this function`):
  22 hits, and 0 of 22 misread.
- Nested relative clauses: 14 hits, and about 3 of 14 forced a re-read.
- Comparatives without `than`: 312 docs hits, and about 6 of 20 lacked
  a baseline.
- Long openers in sentences of 25 words or fewer: 529 hits, and about
  6 of 25 forced a re-read.
- `not` before `because`: 50 hits, and about 5 of 25 read two ways.
- Noun clusters of four nouns (the removed ProseNounCluster): 313 hits,
  and about 5 of 20 read better broken up. The tagger reads many verbs
  as nouns.
- Frequency adverbs (`usually`, `typically`): 882 hits, mostly accurate
  statements of typical behavior.
- `via` in ProseFancyWord: 301 hits for a small gain.
- `latest`: 88 hits, mostly durable instructions or runtime state.
- `still`: 101 hits, and about 10% dated a claim.
- Bold or italic on one stress word: 157 hits, mostly contrastive focus
  that carries meaning.
- One-item lists: 115 hits, mostly a See also section with one link.
- A heading followed directly by a subheading: 79 hits, and about 11 of
  20 needed a lead sentence.
- A code block of 8 or more lines right under a heading: 87 docs hits.
  The heading or the comments in the block already introduced most of
  them.
- Undefined acronyms: 348 docs hits, and about 35 of 44 named an
  acronym the file never spelled out. The rule needs a list of about
  150 known acronyms, which needs upkeep.
- ProseBroadReference for any verb after `this`: 306 hits, and 16 of
  25 were correct.
- ProseSemicolon after a word the tagger leaves untagged: 394 more
  hits, and 9 of 25 were correct. The misfires were HTML entities, code
  such as `);`, and vCard data, and a guard against them is unmeasured.
- The CondenseAllThe built-in: 72 hits, and none of 20 sampled read
  better as `all the`.

## Linter Comparison

`task prose:compare` runs two `prose-lint` binaries on the same inputs
and reports where their findings differ. Review the report before a
change that moves the findings of many rules, such as a change to the
markdown model of the crate or a new version of harper-core. Each
difference must be a choice. Fix a difference that makes a rule worse,
and accept the others. The report exits 0 when the binaries differ, so
it does not gate a change.

```bash
nix build .#prose-lint
task prose:compare -- "$(command -v prose-lint)" ./result/bin/prose-lint
task prose:compare -- "$(command -v prose-lint)" ./result/bin/prose-lint ProseHedge
```

Both binaries lint each of these inputs:

- A copy of each file under `fixtures/`, because both binaries skip a path
  under `prose-lint/fixtures`. The report names each copy by the path of its
  fixture.
- Each file that git tracks. Each binary skips a file whose extension
  it does not know.
- The files of the `measure.py` corpus, for each source whose clone
  the root of `measure.py fetch` holds. `--root` names another root.
- The last 200 commit messages, each read with `--commit`.

The report prints one row for each rule. Its columns count the findings
that only the first binary prints, the findings that only the second
prints, and the findings that both print. Two findings count as the
same only when both binaries print the same line, message included.
Each rule name after the paths, such as `ProseHedge`, adds a list of
each finding of that rule that only one binary prints. A `<` marks a
finding of the first binary, and a `>` marks a finding of the second.
The report prints each listed finding above the source line that the
finding names.

`taskfiles/prose/compare.py` documents the other flags. To test the
report, run `python3 taskfiles/prose/test_compare.py`.

## Verification

The Nix build of `prose-lint` runs `cargo test`, which tests every rule.
Build it from the repository root after you change a rule file, a
fixture, or the crate. The flake build reads only the files that git
tracks, so `git add` a new file first.

```bash
nix build --no-link .#prose-lint
nix build --no-link .#prose-lint.tests.clippy
```

The tests fail with one line for each failure. These tests cover the
rules:

- `fixtures`, `tiers`, `structure`, `weir_tests`, and `entries` in
  `src/rule_tests.rs` check each rule against its fixtures and against the
  tests inside its Weir file. The Rule Tests section of `README.md` lists what
  each rule must pass.
- `layouts` checks the comment of every rule file against the layout in
  Comments.
- `rule_comments` lints the comment of every rule file and fails on each
  Required or Recommended finding. A change to one rule therefore fails
  the test when the rule reports a phrase in the comment of another rule
  file.
- The tests of `src/comments/tests.rs` lint the source files under
  `fixtures/comment-code`, `fixtures/crlf`, and `fixtures/license`, and
  they expect the findings that each test lists.
- The tests of `tests/cli.rs` run the binary on markdown files, source
  files, and commit messages, and they check its output and its exit
  status.

To run the tests outside of Nix, follow the Building section of `README.md`,
and run `cargo test` in `tools/prose-lint`.

`harper-cli test` runs the tests inside one Weir file, such as
`harper-cli test rules/required/ProseAndOr.weir` in `tools/prose-lint`. The `harper-cli` of `pkgs.harper` reads
a rule the same way as the crate, because both link the same harper-core.
