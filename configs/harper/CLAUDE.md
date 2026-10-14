# Harper Rules

This directory holds the rules of the prose linter. `rules/<tier>/` holds
each rule as a `.weir` or `.awk` file, and `fixtures/<tier>/` holds its
`.bad.md` and `.good.md` pair. The docstring of `check.py` describes how
the build tests them.

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
each `%s` in an awk message that prints the matched text. A label that
is the flagged word itself is a lowercase code span (`` `seam`. ``),
and a label that names a grammar term stays plain (`Passive voice.`).

## Comments

Write each rule's comment for an engineer with no linguistics
background.

- When the name or the label uses a grammar term, open the comment with
  a plain definition and an example. ProseIndefiniteOpener opens with
  "Grammar calls `a` and `an` the indefinite article, because each
  introduces a noun without pointing at a particular one."
- Define every other grammar term at its first use in each file, or
  replace it with a plain phrase. Readers open one rule file at a time,
  so a definition in another file does not reach them.
- Reuse the glosses below, so one term reads the same in every file.

| Term        | Gloss                                                               |
| ----------- | ------------------------------------------------------------------- |
| determiner  | a word that introduces a noun, such as `the`, `a`, or `this`        |
| auxiliary   | a helping verb such as `is`, `has`, or `can`                        |
| participle  | the `-ing` or `-ed` form of a verb, such as `returning` or `cached` |
| gerund      | an `-ing` form used as a noun                                       |
| progressive | a form of `be` plus an `-ing` verb                                  |
| noun phrase | a noun and the words attached to it                                 |
| clause      | a subject and its verb, such as `the tests pass`                    |
| agent       | the actor that a passive sentence names after `by`                  |

Replace these terms with the plain phrase instead of defining them:

| Term           | Plain phrase                   |
| -------------- | ------------------------------ |
| copula         | a form of `be`                 |
| inflections    | its forms for tense and person |
| predicate noun | a noun after `is`              |
| finite verb    | a verb that carries a tense    |

The prose hook lints the comments in `.weir` and `.awk` files, so every
comment must also pass the prose rules. The hook lints only files that
Write and Edit touch, so run `prose-lint <file>` on each rule file you
edit through Bash.

## Renames

To rename a rule:

1. Rename the rule file and both fixtures with `git mv`.
2. In an awk rule, update the name in the output-format comment and in
   every `printf`.
3. Update the label at the start of the message.
4. Search `configs/harper/rules` and `configs/claude` for the old term,
   including references such as `the stock-word rule`, and update each
   one.

## Measurement

`corpus.json` pins the measurement corpus: the kubernetes, nix.dev,
Rust book, and Hugo docs, and the comments of ten Go standard library
packages. `measure.py` lints it with a rule and prints the hits for
each source and a seeded sample of hits.

```bash
python3 configs/harper/measure.py fetch
python3 configs/harper/measure.py run ProseHedge
python3 configs/harper/measure.py run --weir scratch/ProseDraft.weir --sample 20
```

Judge each sampled hit by hand, and count the hits that the rule
reports correctly. Each new or changed rule cites the hits and the
sampled precision in its comment:

```text
It hit 57 lines in the measure.py corpus, and 16 of 16 sampled hits
were conjunctions.
```

Rule comments that predate the corpus cite counts from other runs. Measure such a rule again the next time it changes,
and replace its numbers.

## Verification

Build the weirpack from the repository root. The build runs every rule
against its fixtures and fails with one line for each rule that misses
a fixture.

```bash
nix build --no-link --impure --expr 'let f = builtins.getFlake (toString ./.); pkgs = import f.inputs.nixpkgs { system = builtins.currentSystem; }; in pkgs.callPackage ./pkgs/prose-weirpack.nix { }'
```

The prose-lint test lints source files that open with a license header
and expects no finding.

```bash
nix build --no-link --impure --expr 'let f = builtins.getFlake (toString ./.); pkgs = import f.inputs.nixpkgs { system = builtins.currentSystem; }; prose-weirpack = pkgs.callPackage ./pkgs/prose-weirpack.nix { }; in (pkgs.callPackage ./pkgs/prose-lint.nix { inherit prose-weirpack; }).passthru.tests.license-headers'
```
