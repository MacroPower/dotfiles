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
background. Every rule file opens with a comment, and every comment
uses one layout. Readers then find the fix, the words that a rule
leaves out, and its precision in the same place in each file.

The prose hook lints the comments in `.weir` and `.awk` files, so every
comment must also pass the prose rules. The hook lints only files that
Write and Edit touch, so run `prose-lint <file>` on each rule file you
edit through Bash. A new or changed rule can also report a phrase in
the comment of a rule file that you did not edit. The `rule-comments`
test in Verification lints every rule file, so rewrite each comment
that it reports in the same change.

The weirpack build in Verification checks the comment of every rule
file against the layout in Sections. `check.py` prints one line for
each breach and fails the build, and its docstring lists the breaches.

### Sections

Every comment holds these sections in this order.

- **Overview** says what the rule flags and gives an example. It
  defines each grammar term in the name or the label in plain words.
  It then states what the flagged text costs the reader, the fix, and
  why the rule sits in its tier.
- **Expression** says how to read the pattern: each branch, each
  guard, and each Weir or tagger workaround. For an awk rule, it says
  how the check reads the text and which `lib/` file the check
  includes.
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

`SECTIONS` in `check.py` holds the section names in the same order, and
`REQUIRED_SECTIONS` holds the sections that every comment must carry.
When you add, rename, reorder, or require a section, change this file,
`check.py`, and the cases in `test_check.py` together.

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

`check.py` also reads a lone comment line as a heading when the line
starts one space after `#` and does not end in a period. A lone line is
a paragraph of one line. Each side of it holds a blank comment line or
an end of the comment. The first line of the comment and the line
directly above the code can therefore be lone lines.

`check.py` fails a lone line whose name `SECTIONS` lacks, such as
`Related rules` or `Notes`. End every paragraph of one line with a
period. An example line that you indent further does not need the
period.

Every comment carries Overview and Measurement, and `check.py` fails a
comment that lacks either one. When a rule does not hit any line in the
corpus, write the sentence that the Measurement section of this file
gives for that case. Leave out any other section that has nothing to
say, and keep the remaining sections in order. Never write a
placeholder sentence such as `No related rules.`, because the prose
rules reject it as filler.

Awk rules use the same section names, and the Expression section of an
awk rule covers its awk logic. The `Output format` lines stay last,
after every section. One blank comment line separates the last section
from the `Output format` lines. `check.py` reads the paragraph that
opens with `Output format` as those lines. It fails when a blank
comment line follows that paragraph, so keep the lines together and
put the code directly after them.

These rule files show the layout:

- `rules/required/ProseAndOr.weir` is a rule with a short comment.
- `rules/recommended/ProseBroadReference.weir` is a Weir rule with
  guards, related rules, and a measurement.
- `rules/recommended/ProseHiddenList.awk` is an awk rule.

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
- Name an awk library by its path (`lib/markdown.awk`), and never call
  it `the reader`. In a comment, `the reader` is the person who reads
  the prose, so write `lib/markdown.awk blanks code spans`.

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
2. In an awk rule, update the name in the output-format comment and in
   every `printf`.
3. Update the label at the start of the message.
4. Update the name in `configs/harper/comment-checks.txt`, in
   `pkgs/prose-lint.nix`, and in `configs/harper/measure.py`.
   `prose-lint.nix` names some rules for commit messages, for agent
   prompt files, and for the findings that it drops after Harper
   reports them. `measure.py` names the rules whose handling it copies
   from `prose-lint.nix`.
5. Search `configs/harper/rules` and `configs/claude` for the old rule
   name, and update each reference. Rule comments name a rule only by
   its rule name. Messages and skills can state the old term in words
   (`stock word`), so search for those words too.

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

## Verification

Build the weirpack from the repository root. The build runs every rule
against its fixtures and fails with one line for each rule that misses
a fixture. It also fails with one line for each breach of the comment
layout in Comments.

The build also runs each awk rule on a copy of its fixtures with CRLF
line endings. It fails with one line for each finding that the copy
drops and one line for each finding that the copy adds. A finding that
moves to another column therefore prints two lines.

The fixture checks and the CRLF check run each awk rule under a UTF-8
locale. Under the C locale, gawk counts bytes in place of characters.
The build therefore also runs each awk rule under the C locale, on its
fixtures and on their copies with CRLF line endings. It fails with one
line for each finding that the C locale drops and one line for each
finding that it adds.

The build also runs `test_check.py`, the unit tests of `check.py`. They
cover the comment check, the CRLF check, the locale check, and the
lines that `check.py` expects a bad fixture to lint. Every rule file
follows the layout, and every awk rule prints the same findings for
CRLF line endings and under the C locale. Only these tests therefore
show that a breach or a misread fails the build. The tests also export
`RAW` and `SHADOW` and expect each awk check of `check.py` and
`measure.py` to keep the mode that the script chose.

```bash
nix build --no-link --impure --expr 'let f = builtins.getFlake (toString ./.); pkgs = import f.inputs.nixpkgs { system = builtins.currentSystem; }; in pkgs.callPackage ./pkgs/prose-weirpack.nix { }'
```

Build the tests of prose-lint after you change `pkgs/prose-lint.nix`,
a rule file, or a file under `lib/`. Each test runs the built wrapper
on a set of files.

- `license-headers` lints source files that open with a license header
  and expects no finding.
- `crlf` lints the source files in `fixtures/crlf/` and copies of them
  with CRLF line endings. The files hold a YAML block scalar, an HCL
  heredoc, and license headers, whose lines the comment shadow skips.
  They also hold a shell comment between two lines of `-` marks, and
  ProseSentenceLength must report the long sentence in it. A shell
  comment that indents its text four columns past a `>` draws a
  ProseLatinAbbreviation finding, because lib/markdown.awk reads
  indented code in a comment as prose. The test expects the findings
  that it lists from both copies of each file, and the same shadow from
  both.
- `comment-code` lints the comments of `fixtures/comment-code/code.sh`
  as a shell, Nix, awk, Python, YAML, HCL, Rust, TypeScript, Lua, and
  Go file. It also lints a copy with `//` markers as an HCL file.
  comment_lines writes such a marker as `# `, so the copy tests that
  hide_end_dashes reads the text after the `//`.

  The comments hold code spans in one, two, and three backticks that
  wrap onto the next line, and fenced code blocks. The blocks include
  two fences in tildes, one with a backtick in its info string. They
  also include a fence in four backticks that holds a line of three,
  and a fence after `2.`, `- `, or `> `. The test expects the same
  ProseMayPossibility findings from every file, each on the text after
  a span or a block.

  The comments also hold a backtick that no span closes, a backtick
  after a backslash, and a span in two backticks that holds one
  backtick. Some backticks that no span closes sit in a heading, or
  above a heading, a thematic break, a line of `=` marks, a blockquote,
  or a line that holds only `>`. The line after each of them holds a
  span. A span in a blockquote also wraps onto a line without `>`,
  which CommonMark reads as more text of the quote. Some lines open
  with three backticks and hold another backtick, which CommonMark
  reads as paragraph text. In some of them, a `-` or `*` list marker or
  a `#` heading marker comes before the backticks.

  The comments at the end of the file hold lines that end in `--`.
  Harper strips the marks at the end of a comment line, so the comment
  shadow adds ` x` after them. hide_end_dashes in `pkgs/prose-lint.nix`
  keeps that ` x` only when the next comment line continues the
  paragraph, as in a markdown file with the same lines. The test
  expects the same findings from every file. It also lints a copy of
  the shell file with CRLF line endings and expects the same findings
  from it.

  The sentence of the first `--` goes on in the next line. The text of
  the second line ends in an ` x` of its own after the `--`. The test
  expects a ProseDash finding on each of these dashes.

  Four banners, such as `# --- The section ---`, come next. Code
  follows the first, and a comment line follows each of the others.
  The text of the third opens with `---` and no space, and the text of
  the fourth opens with `--` and a space. hide_end_dashes drops the
  ` x` after a banner, whatever the next line holds, so the test
  expects no ProseDash finding on the banners.

  Pairs of comment lines come next, and the first line of each pair
  ends in `--`. The second line of each of the first eight pairs ends
  the paragraph, so the test expects no ProseDash finding on them. Three
  of them open a list item, a heading, and a blockquote. Two more open a
  list item under a list item, one at the text of that item and one at
  `2.`. The sixth holds a line of `=` marks, the seventh opens a list
  item at `1.`, and the eighth holds a thematic break in `_` marks.

  The first line of the ninth pair is a heading. Headings form blocks
  of their own, so the test expects no finding on it either.

  The second line of each of the next six pairs continues the
  paragraph, so the test expects a ProseDash finding on each:

  - A second line in a blockquote.
  - A list item at `2.` under a paragraph. CommonMark reads an item at
    a number other than 1 as more text of a paragraph, but as the next
    item under a list item.
  - A line without `>` under a line in a blockquote, and a line of `=`
    marks without `>` under one. CommonMark reads each of them as more
    text of the quote. It does not read such a line of `=` marks as the
    underline of a heading.
  - A line under a list item, left of the text of the item, which
    CommonMark reads as more text of the item.
  - A list item indented 4 columns under a paragraph, which CommonMark
    reads as more text of the paragraph.

  The next groups of comment lines test the blocks that hold the line
  that ends in `--`. In each of the first five, the line after the `--`
  continues the paragraph, so the test expects a ProseDash finding on
  each:

  - A second line in a blockquote inside a list item.
  - A line of `=` marks under a list item inside a list item, left of
    the text of the inner item. CommonMark reads it as more text of
    that item.
  - A list item indented 4 columns under a paragraph indented 2.
    CommonMark measures the indent of the item from the start of the
    text, so it reads the item as more text of the paragraph.
  - A list item at `2.` under a paragraph indented 2 columns.
  - A line whose own text is `--- x`. close_spans does not add ` x` to
    it, so hide_end_dashes reads it as text and not as a line of `-`
    marks.

  In each of the other groups, the line after the `--` ends the
  paragraph, so the test expects no ProseDash finding on them:

  - A list item at `2.` under a line in a blockquote. CommonMark reads
    a line without `>` there as more text of the quote, but a list item
    at any number ends the quote.
  - A line in a blockquote under a blockquote inside a list item. Its
    `>` sits left of the text of the item, so it opens a blockquote
    outside the item.
  - A list item at `2.` under the second line of a list item. That line
    sits at the text of the item, so CommonMark reads it as text of the
    item, and the `2.` item ends the paragraph.
  - A line of `-` marks in a blockquote under a line in that
    blockquote, which CommonMark reads as the underline of a heading.
    The shadow adds ` x` after that line too, and hide_end_dashes reads
    the line without it.
  - A list item at `2.` under a paragraph of a list item, after an
    empty comment line. The empty line does not end the item.
  - A line in a blockquote under a paragraph that follows a heading in
    a blockquote. CommonMark adds a lazy line only to a paragraph, so
    the paragraph after the heading sits outside the quote, and the `>`
    opens a new one.
  - A list item at `2.` indented 2 columns under a list item whose
    marker a tab follows. The tab reaches column 4, so the text of the
    item starts there, and the `2.` item sits left of it.

  More groups follow. In each of the first seven, the line after the
  `--` continues the paragraph, so the test expects a ProseDash finding
  on each:

  - A line under a line in a blockquote, whose `>` sits 4 columns in.
    That `>` does not open a blockquote, so CommonMark reads the line as
    more text of the quote.
  - A line that opens with seven `#` marks, which do not open a
    heading.
  - A line under a line whose text opens with `--force`, which is not a
    banner.
  - A list item at `2.` under a paragraph that follows a list item at
    `1.` without text and an empty comment line. The empty line ends
    the empty item, so CommonMark reads the `2.` line as more text of
    the paragraph.
  - A list item at `2.` under a list item in a blockquote, at the text
    of that item. The text of the item starts after the `>` and one
    blank, so the `2.` line sits in the item.
  - A line that opens with ten digits and a `.`, under a line in a
    blockquote. A list marker holds 9 digits at most, so the line does
    not open a list item. CommonMark reads it as more text of the
    quote.
  - A list item at `2.` under a line that opens with ten digits and a
    `.`, which does not open a list item.

  In each of the other groups, the line after the `--` ends the
  paragraph, so the test expects no ProseDash finding on them:

  - A list item in a blockquote, whose marker sits 4 columns past the
    `>`. The text of the quote starts after its `>` and one blank, so
    the marker sits 3 columns into that text, and it opens a list item.
  - A list item indented 4 columns under a list item whose marker a tab
    follows. The tab reaches column 4, so the inner item sits at the
    text of the outer item.
  - A list item at `2.` under the second line of a list item at `1.`
    without text. The text of such an item starts one column past its
    marker, so the second line sits in the item.
  - A line in a blockquote under a paragraph that follows a thematic
    break in a blockquote. The paragraph sits outside the quote, so the
    `>` opens a new one.
  - A list item at `2.` under a paragraph of a list item at `1.`
    without text, after an empty comment line. The item holds text
    above the empty line, so the empty line does not end it.
  - A list item at `2.` under a paragraph of a list item whose first
    line holds a list item at `1.` without text, after an empty comment
    line. The empty line ends only the inner item, so the paragraph
    sits in the outer item, and the `2.` item ends it.

  More groups follow, and some of their lines hold only `-` and `*`
  marks. Such a line opens its list items, and a thematic break in it
  ends the blocks that it leaves, as in CommonMark. In each of the
  first eight groups, the line after the `--` continues the paragraph,
  so the test expects a ProseDash finding on each:

  - A line under a line in a blockquote, whose `>` sits 3 columns in.
    The copy with `//` markers holds the same text after each `//`, so
    its `>` opens a blockquote too.
  - A line of `=` marks under a paragraph of a list item whose marker
    line holds only `-`. CommonMark reads that line as an empty list
    item. The line of `=` marks leaves the item, so CommonMark reads it
    as more text of the item.
  - The same lines with the `-` 3 columns in.
  - A line of `=` marks under a paragraph of a list item inside a list
    item, whose marker line holds only `- -`.
  - A list item at `2.` in a blockquote inside a list item, under a
    thematic break in `*` marks, and a line of `=` marks under the `2.`
    item. No paragraph sits above the item, so the item opens a list,
    and the line of `=` marks leaves it.
  - The same lines in a blockquote whose first line holds a `-` 5
    columns past the `>`. CommonMark reads that `-` as indented code,
    so no paragraph sits above the `2.` item either.
  - A line of `=` marks under a paragraph of a list item whose marker 4
    blanks follow, after a line that holds a `-` 4 columns in. The text
    of the item starts 5 columns in, so that `-` sits 4 columns past
    the blocks that its line stays in. That line does not open a block
    and reads as blank, so the item stays open.
  - A line that opens with ten digits and a `.`, under a paragraph
    line. The number has the value 1, but a list marker holds 9 digits
    at most, so the line does not open a list item.

  A heading follows, whose underline is a line that holds only `-`, so
  the test expects no finding on the heading. A list item at `2.`
  follows the heading, and a line of `=` marks follows the item. No
  paragraph sits above the item, so the item opens a list, and the test
  expects a finding on it.

  In each of the other groups, the line after the `--` ends the
  paragraph, so the test expects no ProseDash finding on them:

  - A list item indented 4 columns under a paragraph of a list item
    whose marker line holds only `*`. The inner item sits at the text
    of the outer item.
  - A list item at `01.` under a paragraph. The number has the value 1,
    so the item ends the paragraph.
  - A line that holds only `--` under a paragraph line. CommonMark
    reads it as the underline of a heading. hide_end_dashes reads it as
    a blank line, because Harper strips its marks.
  - A line of `=` marks under a paragraph indented 2 columns. A
    thematic break in `-` marks and blanks sits above the paragraph,
    under a list item, and it ends the item. So the paragraph sits
    outside the item, and the line of `=` marks underlines it.
  - The same lines with a thematic break in `*` marks and no blanks.

  The test also writes a shell file and a copy of it as a Python file.
  `.editorconfig` trims the blanks that end a line of the fixture, but
  two comment lines of this file end in a blank. The first ends in `--`
  and sits above code. The second holds a line of `-` marks in a
  blockquote, under a line of that blockquote that ends in `--`. The
  test expects no ProseDash finding from either file.

  The last paragraphs of comment text in the file each end in `--`. A
  line of `-` marks follows the first, code follows the second, and an
  empty comment line follows the third. The last one ends the file.
  Markdown files do not draw a finding on a `--` that ends a paragraph.
  hide_end_dashes drops the ` x` after each of these lines, so the test
  expects no ProseDash finding there either.

  The test also lints `fixtures/comment-code/describe.tf` and a copy of
  it with CRLF line endings. Some of its quoted `description` and
  `error_message` strings end in `--`, and the closing quote ends the
  paragraph of each. One of them sits above a comment line, which does
  not continue its paragraph. A comment line that ends in `--` sits
  above a quoted `description`, and a heredoc description ends in `--`.

  Two comment lines that each open a list item at `2.` follow the last
  quoted `description`. hide_end_dashes reads the string as code for
  those lines, so the second line opens the next item. In another
  heredoc description, a line that ends in `--` sits above a line that
  opens with `//`. That `//` is text of the description, so the line
  continues the paragraph. The test expects a ProseDash finding on that
  `--` and on the string whose text ends in an ` x` of its own after the
  `--`.

  The test also lints `fixtures/comment-code/template.ts`, whose
  template literals hold `//` lines that end the template or open a
  fence. Its code also holds backticks in strings, in regular
  expressions, and in comments. Some of the strings use single quotes
  or hold an escaped quote. Some of the regular expressions hold a
  character class or an escaped `/`, and three open their line.

  The last three block comments of the fixture each end on a `//` line
  that ends in `*/`. A regular expression with a backtick follows the
  first block, and a template follows the second. A comment line and
  then such a regular expression follow the third. The comment shadow
  adds ` x` after each `*/`. merge_comments in `pkgs/prose-lint.nix`
  drops it, even when comment text follows the line, as after the third
  block. With the ` x`, the `/` of the regular expression reads as a
  division, and its backtick opens a template that hides the comment
  after it.

  The test expects a finding on each comment after these cases, and on
  the text after each wrapped span. It lints copies of the fixture as a
  JavaScript, JSX, and TSX file too, and it expects the same findings
  from each. It also lints a copy with CRLF line endings and expects
  the same findings from it. merge_comments drops the `\r` before it
  tests the end of a line, so it still drops the ` x` after each `*/`
  in that copy.

- `haddock` lints Haddock comments in a Haskell file, and it expects a
  ProseMayPossibility finding on each of them and no other finding. The
  file holds these forms of Haddock comment, and each opens a doc
  comment in tree-sitter-haskell:

  - a `-- |` comment and a `--` line that continues it
  - a `-- ^` comment after code and on a line of its own
  - a `{-^` block after code and a `{-  ^` block on a line of its own
  - `{-|`, `{- |`, and `{--|` blocks
  - a `--  |` comment and a `--- |` comment

  In the copy that Harper lints, prose-lint keeps the `{-` of each block
  comment. It puts a space in place of each character after the `{-`
  through the Haddock marker or the last dash. So the `--` of the `{--|`
  block does not draw a ProseDash finding. The last `-- |` comment sits
  between two quasiquotes, and the first one ends in `-- |]`. The file
  ends in the operator `--|`, after code and at the start of a line, and
  the test expects no finding there.

  A Haddock banner, `-- | --- The section ---`, sits above the second
  quasiquote, and a `--` line of text follows it. hide_end_dashes reads
  the copy without Haddock markers, so it finds the banner and drops the
  ` x` that the shadow adds after it. The test expects no finding on the
  banner.

  The test also expects the shadow to hold each full-line `--` comment
  without its Haddock marker, and the banner without the ` x`. The
  shadow takes a `--` marker only before whitespace or the line end, so
  it does not hold a line for the operator or the `--- |` comment. The
  test expects prose-lint to exit 2 on an unreadable copy of the file,
  as on the file of any other language.

  The test also lints a second Haskell file, which holds Haddock code
  examples, fenced code blocks, and block comments that open with `{--`.
  prose-lint puts spaces in place of the code of each Haddock code
  example, in `--` comments and in block comments. It cuts bird tracks
  (`> f x`), examples (`>>> f x`) and their result lines, properties
  (`prop> f x`), and the lines between the two `@` lines of a code
  block. As in Haddock, a line of only `@` opens a code block only when
  a later line of its comment opens with `@`. prose-lint keeps each
  `{-`, `-}`, and `|]` at its column, on the lines that it cuts and on
  the lines of a fence. So the comments and quasiquotes of the copy
  close where they do in the file.

  The code holds `Just`, `Maybe`, and a `--` comment. The test expects a
  ProseMayPossibility finding only on the lines of prose. The shadow
  holds text only on those lines, on the `@` lines, on the lines of text
  under an `@` that no line closes, and on the lines that keep a `{-`, a
  `-}`, or a `|]`. The file holds these cases:

  - an indented bird track, and a bird track in a `---` comment
  - a line of prose right under a bird track and right under a property
  - result lines that end at an empty comment line or at a line of
    code, with a line of prose under each
  - an example that opens a `-- |` comment
  - a block that opens with `{--` and a block that opens with a banner
    of dashes, with no ProseDash finding on either opener
  - a `{---}` block above a string that holds prose, which prose-lint
    rewrites as `{- -}`, so the block still closes
  - a `{-|` block that holds each form of code example, with a result
    line that ends in the `-}` of the block, above a string that holds
    prose
  - a bird track that opens a nested block, inside a block of prose
  - a block whose last line is `-- > lookup 1 m -}`, above a string that
    holds prose
  - a quasiquote whose last line is `-- > lookup 1 m |]`, above a
    comment
  - a quasiquote that closes on a bird track, with code and a comment of
    prose after its `|]`
  - a quasiquote that closes on the line of an example, right above a
    `--` comment of prose
  - a `{-` in a string after the character literal `'"'`, in a line
    comment, and in a quasiquote. Other cases put a `{-` in a string
    after the string `"\""`, in a string after the character literal
    `'\"'`, in the quasiquote `[Q.r|{-|]`, and in a string after
    `i' '"'`. The `'` of the name `i'` does not open a character
    literal. Each `{-` sits on lines of its own, above a line of code
    that opens with `>` and ends in a comment.
  - blocks that open after the operators `<--` and `-->` and after
    `{-}`, each closed on a `--` line with a bird track, with code and a
    comment of prose after the `-}`
  - a block that holds a nested block of prose and then two bird
    tracks, one with a `--` comment and one with a nested block, above a
    string that holds prose
  - a block that opens after code and closes on the line of an example,
    with code and a comment of prose after its `-}`
  - a block that closes on a `--` line with an example, with code and a
    block comment of prose after its `-}`
  - a `{-|` block whose last result line ends in the `-}` of the block,
    right above a `--` comment of prose
  - a `{-` in a string after the character literals `'é'` and `'"'`,
    above a line of code that opens with `>` and ends in a comment
  - a block that closes on a bird track that holds `café`, with code and
    a comment of prose after its `-}`
  - a code block in a `--` comment with an empty comment line in it,
    with a line of prose under its closing `@`
  - a code block whose first line opens with `@`, with a line of prose
    under its closing `@`
  - a line of only `@` right under a line of prose, with a line of text
    and a line of code under it and an `@since` comment of prose under
    the code
  - a `{-|` block that holds a code block and a line of prose
  - a line of only `@` right above the `-}` of its block, with prose
    before the `-}`, code and a comment of prose after it, and an
    `@since` comment of prose under the block
  - result lines of an example that hold a line of only `@`, with an
    empty comment line, a line of prose, and an `@since` line under them
  - a code block that closes on a line of prose that opens with
    `@since`, with a line of prose under it
  - a code block whose opening `@` line ends in a space, with a line of
    prose under its closing `@`
  - a fence in the `--` lines of a block, with a line that holds the
    `-}` of the block, above a string that holds prose
  - a fence whose opening line holds the `-}` of its block, above a
    string that holds prose
  - a fence with a line that opens a nested block, inside a block of
    prose
  - a fence whose last line holds the `-}` of a block and `café`, with
    code and a comment of prose after the `-}`
  - a fence whose last line holds the `|]` of a quasiquote, with code
    and a comment of prose after the `|]`
  - a line of only `@` that no line closes, with an example, its result
    line, an empty comment line, and a line of prose under it
  - a `{-|` block with a line of only `@` that no line closes, and a
    line of text, an empty line, and a line of prose under it
  - a line of only `@` right under a line of prose, above a line that
    closes inline code in the middle of the line, an empty comment line,
    and a line of prose
  - a block comment of prose and a `--` comment of prose on one line,
    with a space between them. `'é'` comes before the block, and the
    prose of the comment holds a `--`.
  - a block comment of prose with a `---` comment of prose right after
    its `-}`
  - a block that closes on a `--` line, with a `--` comment of prose
    after its `-}`
  - an empty `{--}` comment above a string that holds prose, which
    prose-lint rewrites as four spaces, so no block opens there

  Harper joins a block comment and a line comment that only whitespace
  separates. It strips the comment marker only at the start of each
  line, so it reads the dashes that open the line comment as a dash.
  prose-lint drops each finding on those dashes. The test expects a
  ProseDash finding only on the `--` in the prose of the comment.

  tree-sitter-haskell 0.24.1 reads each dash right after a `{-` as part
  of the opener. So for it, a `{--}` that the copy keeps does not close,
  and Harper lints the string after it.

  The test expects the same shadow and findings from a copy of the
  second file with CRLF line endings. In both copies, a trailing space
  ends the empty comment line under the first result lines and the
  opening `@` line of the last closed code block. Editorconfig trims
  those spaces from a fixture, so the test adds them. In the CRLF copy,
  a `\r` follows each space, and prose-lint must not read it as text of
  the line.

  The test reads the shadows of the second file and of its CRLF copy
  under the C locale and under a UTF-8 locale. It lints each copy under
  each locale too. gawk reads `é` as two characters under the C locale.
  So under that locale, a cut that puts a space in place of each byte
  moves the comment after the `-}` of `café` to the right.

  A third Haskell file puts a `{-` and a `--` in strings that run on
  past the end of a line or hold a `\` and whitespace. One multiline
  string, which `"""` opens, holds a `{-`, a `--` line with a bird
  track, a `--` line of prose, and a `{-` after a `"`. One string gap
  runs from the end of a line to the next line, and another opens and
  closes inside one line. A `{-` follows each gap, and a string that
  holds prose follows each case. Two more strings hold a `--` and prose,
  one after a gap of each kind.

  The file ends in a `{-|` block whose last result line ends in the `-}`
  of the block, above a `--` comment of prose. The test expects the
  shadow of the file, and of its copy with CRLF line endings, to hold
  only that comment. prose-lint puts spaces in place of the dashes of
  each `--` line of the multiline string, so the shadow leaves those
  lines out.

  tree-sitter-haskell 0.24.1 reads the text of a multiline string as
  code. It reads the text of a string after a gap inside one line as
  code too. In a file with CRLF line endings, it does so after each gap.
  A `{-` in that text opens a block comment, and a `--` opens a line
  comment. So prose-lint puts spaces in place of each `{-` and each `--`
  in a string. The test lints the third file and its CRLF copy and
  expects a ProseMayPossibility finding only on the comments.

  A fourth Haskell file opens with an example, its result line, an
  empty comment line, and a line of prose. It ends in a multiline string
  that does not close. prose-lint reads each Haskell file twice to find
  the `@` lines that a later line closes. The test expects the shadow of
  the fourth file to hold text only on the line of prose. When the
  second read starts inside that string, the shadow leaves out that
  line.

  harper-cli 3.0.0 from nixpkgs aborts on each Haskell file that holds
  a comment. With that harper-cli, the test checks only the shadows and
  the exit status. The command that builds the `haddock` test from a
  home configuration of the flake uses the patched harper of
  harperOverlay in `lib/default.nix`. Only that command lints the
  first three Haskell files of the test end to end.

- `quote-text` lints lines after a thematic break written with spaces
  (`* * *`) and after empty list items, in a blockquote too (`> -`).
  It also lints lines after a fence marker four columns in after quoted
  code that opens with one (`>     ```sh`), which CommonMark reads as
  indented code. Other lines sit under a thematic break that a blank
  line separates from a list item and that reaches the text of the
  item (`  * * *` under `- `). CommonMark keeps the break in the item,
  so a line two columns past the text of the item is paragraph text.
  A fence indented into a list item keeps the list open too. After the
  fence and a blank line, a line two columns past the text of the item
  is paragraph text of the item.

  The test expects a ProseLatinAbbreviation finding on each line that
  holds `e.g.`, except the last line. CommonMark reads that line as
  indented code in a blockquote of an empty item, which a line of
  whitespace keeps open. check.py lets a line of a bad fixture that
  holds only the marks of a thematic break pass without a finding. Empty
  items and fence markers do not hold text for a finding, so the
  fixtures cannot hold these lines. Some of the lines also need trailing
  whitespace, which editorconfig trims.

- `item-blocks` lints list items that hold a heading or a table, and
  lines left of the text of the item that GFM keeps in the list. Each
  item ends in a paragraph four columns in, which lib/markdown.awk skips
  as code when it ends the list too early. The test expects a
  ProseLatinAbbreviation finding on each line that holds `e.g.`, except
  the last two. The file holds these lines:
  - A line under a link reference definition, which continues the
    paragraph that the definition opens.
  - An empty line inside fenced code or an HTML comment of the item,
    which keeps the list open.
  - A line at the text of an empty list item that opens a new list left
    of the text of the item, after a heading or an HTML block of the
    item.
  - A blank line, a `>` line, a line of indented code, or a heading
    under a table. Each of these lines ends the table, and the paragraph
    after it takes a line under it.
  - A line of `=` marks left of the text of the item, and a line of `=`
    marks under a quoted paragraph. GFM reads each line as more text of
    the paragraph.
  - A line of `-` marks without a `|` under a paragraph. GFM reads the
    line as a setext underline and not as a delimiter row, so the
    paragraph after it takes a line under it.
  - A delimiter row left of the text of the item or four columns past
    it, and one without the `>` of a quoted paragraph. GFM reads each
    row as more text of the paragraph and does not open a table there.
  - A line under a delimiter row with fewer cells than the row above
    it. The line sits four columns past the text of the item. GFM does
    not open a table there, so it reads the row and the line as more
    text of the paragraph.

  GFM ends the list in each of the last two items, so it reads their
  paragraphs four columns in as code. The test expects no finding on
  those two lines:
  - An empty list item with trailing whitespace (`-  `) left of the text
    of the item. The blank line under it ends the list.
  - A line of fenced code left of the text of the item. Fenced code does
    not take a lazy line, so that line ends the list.

  Definitions, empty items, delimiter rows, lines of `=` or `-` marks,
  HTML comments, and code do not hold text for a finding. The
  `same finding` comment that a bad fixture needs at the end of such a
  line changes how GFM reads it. editorconfig trims the trailing
  whitespace of the empty item from a good fixture. GFM reads the
  closing marker of the fence in the last item as the opener of a new
  fence. That fence runs to the end of the file, so the last item ends
  the file.

- `list-items` lints list items that lib/markdown.awk opens or keeps
  open as GFM does. Each item ends in a paragraph four columns in, which
  lib/markdown.awk skips as code when it ends the list too early. The
  test expects a ProseLatinAbbreviation finding on each line that holds
  `e.g.`, except the last. The file holds these items:
  - An item whose first line holds only its marker, `-` or `1.`, with
    no space after it.
  - An item with a link reference definition at the margin right under
    it. GFM reads the definition as more text of the paragraph of the
    item, so the heading under it sits in the item.
  - An item with a lone `2.` at its text under its paragraph. An empty
    item cannot interrupt a paragraph, so GFM reads the line as more
    text of the paragraph, and the lazy line under it keeps the item
    open.
  - An item under a line that holds only `<br>`, which sits right under
    a paragraph. Lines that hold only one tag cannot interrupt a
    paragraph. So GFM reads the line as more text of the paragraph, and
    the item opens.
  - An item right under the delimiter row of a table, with a lazy line
    and a heading under it. The item ends the table. So GFM reads the
    lazy line as more text of the paragraph of the item, and the heading
    sits in the item.
  - An item with an HTML comment or a fence marker four columns past its
    text, under its paragraph. GFM reads the block as more text of the
    paragraph. So it reads the line at the margin in the block as a lazy
    line, and the item stays open.
  - An item whose first line holds an HTML comment, or `<div>` five
    columns past its marker. The comment ends on its line, and GFM reads
    the `<div>` as indented code. So neither opens an HTML block that
    holds the line under it. That line opens a paragraph, and the lazy
    line under it keeps the item open.
  - An item whose first line opens a nested item, with an HTML comment
    after both markers. GFM reads the line under it, at the text of the
    nested item, as a paragraph of the nested item, and not as indented
    code of the outer one.
  - An item under an HTML comment with a blank line in it, in an HTML
    block that `<div>` opens. The blank line ends the HTML block, so GFM
    reads the end of the comment as a paragraph, and the item opens.
  - An item under an item that opens an HTML block with `<div>`, where
    an HTML comment in the block ends at the margin. That line ends the
    item above and the block with it, so the item under the line opens.
  - An item under a line that holds only `<br>`, under a paragraph whose
    last line opens with a `>` four columns in. GFM reads that `>` as
    more text of the paragraph and not as a blockquote. So the `<br>`
    line is more text of the paragraph too, and the item opens.
  - An item under a line that holds only `<br>`, under a delimiter row
    with more cells than the line above it. GFM does not open a table
    there. So it reads the row and the `<br>` line as more text of the
    paragraph, and the item opens.
  - An item that starts at `2.` right under a footnote definition, with
    `<br>` and a lazy line under its paragraph. The item leaves the
    definition, so it interrupts no paragraph. GFM reads the `<br>`
    line as more text of the paragraph of the item, and the lazy line
    under it keeps the item open.
  - A nested item whose text holds only delimiter marks (`  - | - |`)
    under the paragraph of an item, with a lazy line of marks (`-|-`)
    under it. GFM reads a list item before a table, so the line opens
    the nested item and no table. The lazy line keeps the nested item
    open, and the paragraph under the blank line sits at its text.
  - A last item that holds a line of fenced code at the margin. Fenced
    code does not take a lazy line, so that line ends the list, and GFM
    reads the paragraph under it as code.

  Empty items, definitions, HTML comments, fence markers, and indented
  code do not hold text for a finding, so the fixtures cannot hold these
  lines. Each `<br>` line must hold only one tag, and each line of
  delimiter marks only its marks. So none of these lines can hold a
  finding or the `same finding` comment that a bad fixture needs in its
  place. GFM reads the closing marker of the fence in the last item as
  the opener of a new fence. That fence runs to the end of the file, so
  the last item ends the file.

- `opening-pronoun` lints paragraphs that ProseOpeningPronoun reports
  and that its fixtures cannot hold. The test expects the findings that
  it lists:
  - A quoted paragraph above a line of `>` and four or more columns of
    whitespace, which ends the paragraph. editorconfig trims that
    whitespace from a fixture.
  - A line indented four columns right under a link reference
    definition, which GFM reads as more text of the paragraph that the
    definition opens.
  - A paragraph one column past the text of a list item, under a fence
    and indented code below a line of `- - -`.
  - A line above a delimiter row, which GFM reads as the header row of
    a table. The line has a deep indent, opens with `|` or inline HTML,
    or holds plain text with or without a `|`. A line of `---` under the
    table is a rule and not an underline of the paragraph, also when the
    first sentence ends on the first line of the paragraph. The
    `same finding` comment that a bad fixture needs at the end of each
    row breaks the table.
  - A paragraph left of the text of a list item, which puts it outside
    the list, above a delimiter row at the margin. Another paragraph
    sits in a list item, and a tab indents its delimiter row past the
    text of the item. Two more sit past the text of a list item, and their
    delimiter rows sit at the text of the item. One of those items
    starts with `2.` right under a heading. In a blockquote, a delimiter
    row can sit left of the text of the paragraph.
  - A line that holds only one tag, such as `<br>` or an `<img>` tag
    with an attribute, outside the blockquote or the list item of the
    paragraph. GFM opens an HTML block there, and the block ends the
    paragraph. One such list item holds a table row above the paragraph,
    and another sits right under a line of inline HTML. A
    `same finding` comment after the tag makes the line more text of the
    paragraph.
  - A line of `---` left of the text of a paragraph in a list item after
    a heading. GFM reads the line as a rule that ends the item and not
    as an underline, and the line does not hold a finding.
  - A paragraph at the text of a list item, with a line of `---` or
    `<br>` at the margin under it. One item holds a line of its
    paragraph at the margin, and another holds a link reference
    definition there. GFM reads each such line as more text of the
    paragraph and keeps the item open. Other items start with `2.` right
    under a heading, a blockquote, or a fence, or sit right under a line
    of inline HTML. Some of the items hold a heading above the
    paragraph. GFM ends each item at the line under the paragraph, and
    the line does not hold a finding.
  - A line that opens a blockquote right under a table, at the margin
    and in a blockquote. The header row of one table sits right under a
    paragraph. The blockquote ends the table and opens a paragraph in
    the quote.
  - A link reference definition at the margin under an HTML block in a
    blockquote. The definition ends the blockquote and the HTML block,
    so a blockquote under it opens a paragraph.
  - A paragraph under an HTML block of type 1 that ends at its end tag,
    `</script>` or `</pre>`, with a definition or a fence between them.
  - A paragraph under a line that holds only one tag, with a fence or a
    line of `>` between them. The tag sits right under a paragraph that
    opens with inline HTML or holds the text of a list item. GFM reads
    the tag as more text of that paragraph, so it does not open an HTML
    block. In one paragraph, a row of `-` cells sits above the tag. The
    row holds more cells than the line above it, so no table opens.
  - A paragraph at the margin under an HTML block that opens on a later
    line of a list item. Between them sits a fence, a link reference
    definition, or an HTML comment that spans lines. The paragraph ends
    the item and the HTML block.
  - A line of `===` four columns in under a paragraph that opens with
    inline HTML. GFM reads the line as more text of the paragraph, so a
    tag under it is more text too. A blockquote under the tag opens a
    paragraph.
  - An HTML tag four columns in under a heading, a table, or a fence.
    GFM reads the tag as indented code and not as an HTML block. Under
    the tag, a line of `>` or an HTML comment sits above a paragraph.

  Every bad fixture needs a finding on each line. Definitions, fence
  markers, and the rows of a table do not hold one.

- `sentence-paragraphs` lints quoted paragraphs that lib/sentences.awk
  splits and that the fixtures of ProseSentenceLength cannot hold. A
  line of `>` and four or more columns of whitespace ends the paragraph
  and the sentence. editorconfig trims that whitespace from a fixture.
  A list marker four columns past the space after the `>` continues the
  sentence, and the test expects the ProseSentenceLength finding of
  that sentence only.
- `prompt-files` lints one line with a word in capitals at the path of
  a prompt file for an agent and at other paths. It expects the
  ProseCapsEmphasis finding only at the other paths, which include a
  file of a worktree under `.claude/worktrees/`.
- `columns` lints lines that hold a multi-byte character ahead of a
  finding. The character sits inside text that an awk check blanks, or
  earlier in the word that holds the finding. The test expects the
  character column of each finding under the C locale and under a
  UTF-8 locale. `check.py` compares the column of a finding only with
  its column in the copy with CRLF line endings and with its column
  under the C locale. Only this test checks a column against the
  character that holds the finding.
- `code-spans` lints the comments of an awk file, a Go file, and a
  shell file. Harper lints the comment shadow of the first two files,
  and a copy of the shell file whose comment lines come from the
  shadow. The awk checks read the shadow of all three. The test
  expects the same findings from each file, one column further right
  in the Go file. It expects no finding inside a code span, and it
  expects the findings after each span at their source columns. The
  comments hold these spans and runs:
  - A span that opens and closes with a run of two or more backticks
    and holds a shorter run (``` `` ` `` ```).
  - A lone backtick above a blank comment line, a list item, or a
    heading, and a lone backtick in a heading. One heading opens the
    text of a list item. CommonMark reads a run that nothing in its
    paragraph closes as literal text.
  - An escaped backtick, a lone run of two backticks, and a run of
    three backticks with a backtick after it. Each of them sits before
    a span.
  - Spans in one, two, and three backticks that wrap onto later lines,
    with backticks in their code. One span opens at the end of its
    line, and a span in two backticks follows it on the next line.
  - A lone backtick above a line that opens with seven `#` marks.
    CommonMark reads that line as text of the paragraph, so a span
    wraps onto it.
  - A lone run of two backticks above a thematic break or a
    blockquote, and a span in two backticks after it.
  - A lone backtick before a span in two backticks that wraps, on the
    line that opens the span and on the line above it.
  - A span in three or more backticks that closes on its own line, at
    the start of the text and after a quote marker. Harper and
    lib/markdown.awk read a line whose text opens with three backticks
    as a fence, so the shadow puts a `%` in front of the run.

  The comment shadow turns each backtick of a run that nothing in its
  paragraph closes into a `%`. lib/markdown.awk then does not skip the
  text after that run. So the test expects the findings after a lone
  run, on its line and on the next line. Two lone runs sit between two
  copies of one word, and the test expects no repeated word there.

  Other comments wrap a span or a lone backtick onto a line that opens
  with `2)` or `2.`. CommonMark lets only a bullet or a `1.` interrupt
  a paragraph. Another marker opens an item only on a line that leaves
  the item or the blockquote that holds the paragraph. The test holds
  these cases:
  - The marker after a line of prose, and in the text of an item. The
    marker is text of the paragraph there.
  - The marker after a line that opens a bullet item or a blockquote.
    The marker opens an item there.
  - A block in backticks after `2.`, after a line of prose and at the
    start of a paragraph. Only the second one opens a block.

  CommonMark does not open a block on a later line of a paragraph
  whose text sits four or more columns right of the edge of the block
  that holds the paragraph. The comment shadow puts that edge at the
  text of the innermost list item that was open before the first line
  of the paragraph, or after the `>` marks, whichever sits further
  right. When the line reaches the text of an item that the first line
  opens, the shadow counts from that text.

  Other comments wrap a span onto such a line, which opens with
  `- 2)`, `# 2)`, `> 2)`, `***`, or `- and`. The first line of the
  `- and` paragraph sits one column right of the edge. Some of them sit
  in a blockquote, and one wraps onto a line with fewer `>` marks. A
  lazy line with no indent continues a quoted paragraph too.

  A line of `===` or `--` with no paragraph above it opens a paragraph
  in CommonMark, and it is not a setext underline. Other comments put
  such a line under a blank line, a line of code, or a lone `-`. A span
  wraps from a line four columns in under it onto the next line.

  Other comments put a lone backtick above a line that opens an item
  with a span in it. The item opens at these markers:
  - At `1.`, and at a bullet three columns in.
  - At a bullet two columns past the text of an item, and past the
    text of a later paragraph of an item.
  - At a bullet three columns past a `>` with no space after it, and at
    the same bullet under a nested blockquote.

  Other comments put a lone run of two backticks above a thematic
  break, a setext underline, or a quoted line of spaces, with a span in
  two backticks after it. One of them puts the run above a thematic
  break in a blockquote. That break ends in `*`, so the comment shadow
  adds ` x` to its line. A ten-digit number and a period open no item,
  so a span wraps past them, and a fence after them is text.

  Other comments hold a line whose own text is `* x` or `--- x`, with
  and without a `>` in front. The comment shadow does not add ` x` to
  those lines. So each `* x` line opens an item under a lone backtick,
  and a span wraps across each `--- x` line.

  CommonMark reads the first line of a paragraph as indented code when
  its text sits four or more columns right of the edge of its block.
  Each later line with less indent ends the code.

  Other comments put a line of indented code with a lone backtick under
  a blank line. The line under the code opens with `2.` or with prose,
  and it holds a span. A tab indents the code of one comment. In
  another comment, a line of three backticks as deep as the code is
  more code. The line of prose under it ends the code.

  A list item moves the edge of indented code to the text of the item.
  One comment puts code four columns past the text of an item. In
  another comment, a later paragraph of a nested item sits four columns
  in, and a lazy line continues it. Other comments close an item before
  the code with a paragraph at the margin, a line of code, a thematic
  break, a fence, or a `>`.

  Other comments open an empty item at a lone `-` or `*`. The text of
  the item sits one column past its marker, so a line four columns in
  is paragraph text of the item. A lazy line continues that paragraph,
  but a `2)` line opens an item. A blank line under the `*` ends the
  item, so a line four columns in is code there. A fence in tildes three
  columns past the text of the `*` item opens in the item.

  In one more comment, a tab indents a line four columns under a `>`
  line. That line holds more text of the quote, and a span wraps onto
  it.

  A `>` at or right of the text of a list item opens a blockquote in
  the item, and the item stays open after the quote. Other comments
  hold these cases:
  - A later paragraph of the item sits two columns past the text of
    the item, and a span wraps in it. One comment nests the item and
    the quote in a blockquote.
  - A `2)` left of the text of the item opens an item under a lone
    backtick.
  - A later paragraph of a nested item sits four columns in, at the
    text of the item, and a `1.` line under it opens an item.
  - The quote holds a paragraph two columns past its `>`, and a span
    wraps in it. That paragraph is text of the quote.
  - A line without a `>` leaves an item that opened in a blockquote, so
    a line four columns in is code there.

  A fence after a list marker opens the item of that marker. One
  comment puts a later paragraph of such an item four columns in, and a
  lazy line continues it. In another comment, a `2.` line under a later
  paragraph of such an item opens an item under a lone backtick.
  CommonMark reads a list marker on a line of indented code as code, so
  the marker does not open an item. In a third comment, a line at the
  text after such a marker is code too.

  Each list marker that opens a line opens an item, so `- - ` opens an
  item inside an item. Other comments hold these cases under `- - `:
  - A later paragraph two columns past the text of the inner item,
    after a line of text and after a fence, with a lazy line under it.
  - A heading after the markers, which ends its paragraph.
  - A `2)` left of the text of the inner item and a bullet two columns
    past it. Each opens an item under a lone backtick.

  In one more comment, a bullet sits two columns past the text of an
  item and left of the text of a `10.` item inside it. The bullet opens
  an item under a lone backtick.

  Other comments put a lone backtick above a line with a span in these
  cases:
  - A line of indented code holds the backtick, and a quoted line whose
    text sits four columns in ends the code.
  - A lone `-` under a paragraph is a setext underline, and CommonMark
    does not open an item there. So a line four columns in under it is
    code.
  - CommonMark reads a list marker on the first line of indented code
    as code, so the marker does not open an item. A later line at the
    text after the marker is code too.
  - The text of an empty `10.` item sits one column past its marker.
    So a line two columns past that text is paragraph text of the item,
    and a bullet under that line opens an item.

  Other comments hold fenced code blocks in backticks and in tildes,
  after a list marker, and after a quote marker. The info string of
  one block in tildes holds a backtick. The test expects no finding
  inside a block and the findings after it. The blocks include these
  cases:
  - A line of four backticks and a word leaves a block of four open,
    but a line of four backticks closes a block of three.
  - A block sits four columns in, where CommonMark reads its lines as
    indented code.
  - A block opens right under a line of prose.
  - A block has no closing line, and a line of code ends it.

  A span also wraps across a line that opens with three backticks four
  columns right of a line of prose, which CommonMark reads as more text
  of the paragraph.

  Other comments end a block without a closing line, where CommonMark
  ends the indented code, the list item, or the blockquote that holds
  the block. In three of them, a line of three backticks sits four or
  more columns right of a line of prose. That line is text, so a later
  line of backticks opens the block.
  - A line with less indent ends a block that sits four columns in, or
    four columns past the text of an item. In one comment, that line
    opens a block of its own, so the test expects no finding after it.
  - A line left of the text of an item ends a block in the item, but a
    blank line in the block does not.
  - A line without a `>` ends a block in a blockquote.

- `sentence-ends` lints its file under the C locale and under a UTF-8
  locale. It expects the same ProseSentenceLength findings from both,
  and no ProseTrailingCondition finding from either. The file holds
  these sentences, once with each curly quote:
  - Sentences that end at a period and a curly closing quote, after a
    word or right after a bare URL.
  - A sentence with three periods and a curly quote in its middle.
  - A sentence that a lowercase word continues past a quoted `!` or
    `?` and a curly quote.
  - A sentence that opens with a quoted command in curly quotes and
    has an `if` after the quotation.
  - A sentence that opens with a quoted word in curly quotes, with no
    mark before the closing quote, and has an `if` after the quotation.
- `single-quotes` lints sentences that open with a quotation in double
  quotes, and a copy of them in straight single quotes. It expects the
  ProseLongParenthetical and ProseSentenceLength findings that it lists
  from both files, messages included. `check.py` expects a finding on
  each bad line of a fixture, whatever its message. Only this test
  therefore checks the split that a ProseSentenceLength message names.
- `trailing-condition` lints sentences with an `if` and a vague word
  that a line break splits, and it expects one finding from
  ProseTrailingCondition or ProseVagueCriterion for each sentence. A
  hard line break of two or more spaces or tabs, or of a `\`, sits
  between the two words of some sentences. editorconfig trims those
  spaces from a fixture. In other sentences, a line between the two
  words holds only a span in single tildes, a span in single carets, or
  a footnote reference. The bad fixture of ProseTrailingCondition needs
  a `same finding` comment at the end of that line. The rule reads the
  comment as blanked text that splits the two words.
- `transcripts` lints two transcripts. One marks each speaker with a
  bold label, and the test lints it as a `.md` file, as a `.markdown`
  file, and as a commit message. The other names each speaker in
  capitals, and the test rewrites the fence inside one of its turns to
  `~~~`. The test expects the ProseOpinion finding only outside the
  turns of the speakers. It
  expects the ProseAndOr finding inside a turn, because the guard
  drops only ProseOpinion findings. It also lints a file in which no
  label repeats, and it expects every ProseOpinion finding there.
- `rule-comments` lints every rule file under `rules/` and every awk
  library under `lib/`. It fails on each Required or Recommended
  finding. A change to one rule therefore fails the test when the rule
  reports a phrase in the comment of another rule file.
- `exported-variables` lints a file with more than 128 KiB of findings,
  as a file and as a commit message. It exports `out`, `found`, and
  `checks`, the variables in which `pkgs/prose-lint.nix` holds
  findings. A Nix build and a nix-shell export `out` the same way.
  Linux refuses to start a command with an environment string of more
  than 128 KiB. So the wrapper keeps these variables out of the
  environment of each command that it starts. The test expects exit
  status 1 and a ProseMayPossibility and a ProseLatinAbbreviation
  finding on each line of prose.
- `exported-modes` lints a markdown file, the same file as a commit
  message, and a shell file, first with `RAW` and `SHADOW` unset and
  then with both exported. lib/markdown.awk takes its mode from these
  variables, and with `RAW` set it reads each line whole, code included.
  So the wrapper sets both for each awk check that it starts. Each file
  holds an `e.g.` in prose and one in code, in a fence or in a code span
  of a comment. The markdown file also holds an ellipsis character in
  prose and in the fence. The test expects the ProseLatinAbbreviation
  and ProseTypography findings that it lists from both runs, messages
  included.

This command builds every test.

```bash
nix build --no-link --impure --expr 'let f = builtins.getFlake (toString ./.); pkgs = import f.inputs.nixpkgs { system = builtins.currentSystem; }; prose-weirpack = pkgs.callPackage ./pkgs/prose-weirpack.nix { }; in (pkgs.callPackage ./pkgs/prose-lint.nix { inherit prose-weirpack; }).passthru.tests'
```

The command that builds every test uses harper-cli from nixpkgs, so
the `haddock` test checks only the shadows and the exit status there.
On a Linux host, this command builds the `haddock` test from the `dev`
home configuration of the flake for the system of the host. The harper
of that configuration carries the patches of harperOverlay in
`lib/default.nix`, so the test also lints its first three Haskell files
end to end.

```bash
nix build --no-link --impure --expr 'let f = builtins.getFlake (toString ./.); in f.homeConfigurations."dev@${builtins.currentSystem}".pkgs.prose-lint.passthru.tests.haddock'
```
