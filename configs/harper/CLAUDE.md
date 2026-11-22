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
show that a breach or a misread fails the build.

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
  as a shell, Nix, awk, Python, Rust, TypeScript, Lua, and Go file. The
  comments hold code spans in one, two, and three backticks that wrap
  onto the next line, and fenced code blocks. The blocks include two
  fences in tildes, one with a backtick in its info string. They also
  include a fence in four backticks that holds a line of three, and a
  fence after `2.`, `- `, or `> `. The test expects the same
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

  The last comment line ends in `--`. In the files that Harper lints
  through the copy that merge_comments in `pkgs/prose-lint.nix` prints,
  the test expects no ProseDash finding on that line, as in their
  source. The test also lints a copy of the shell file with CRLF line
  endings and expects the same findings from it.

  The test also lints `fixtures/comment-code/template.ts`, whose
  template literals hold `//` lines that end the template or open a
  fence. Its code also holds backticks in strings, in regular
  expressions, and in comments. Some of the strings use single quotes
  or hold an escaped quote. Some of the regular expressions hold a
  character class or an escaped `/`, and one opens its line. The last
  block comment ends on a `//` line, and a template follows it.

  The test expects a finding on each comment after these cases, and on
  the text after each wrapped span. It lints copies of the fixture as a
  JavaScript, JSX, and TSX file too, and it expects the same findings
  from each.

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
    row as more text of the paragraph and opens no table there.
  - A line under a delimiter row with fewer cells than the row above
    it. The line sits four columns past the text of the item. GFM opens
    no table there, so it reads the line as more text of the paragraph.
    lib/markdown.awk reads the row as a table. But it skips indented
    code in an item only after a blank line or after more code, so the
    line stays prose.

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
    paragraph. One such list item holds a table row above the paragraph.
    Another sits right under a line of inline HTML, where
    lib/markdown.awk does not open an item. A `same finding` comment
    after the tag makes the line more text of the paragraph.
  - A line of `---` left of the text of a paragraph in a list item after
    a heading. GFM reads the line as a rule that ends the item and not
    as an underline, and the line does not hold a finding.
  - A paragraph at the text of a list item that lib/markdown.awk does
    not count, with a line of `---` or `<br>` at the margin under it.
    lib/markdown.awk drops one item, right under a paragraph, at a line
    of its text at the margin. It does not open an item that starts with
    `2.` right under a heading, a blockquote, or a fence, nor an item
    with a nested item right under a line of inline HTML. It also drops
    an item at a link reference definition at the margin right under
    the item. GFM reads that definition as more text of the item. GFM
    ends each item at the line under the paragraph, and the line does
    not hold a finding.
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

This command builds every test.

```bash
nix build --no-link --impure --expr 'let f = builtins.getFlake (toString ./.); pkgs = import f.inputs.nixpkgs { system = builtins.currentSystem; }; prose-weirpack = pkgs.callPackage ./pkgs/prose-weirpack.nix { }; in (pkgs.callPackage ./pkgs/prose-lint.nix { inherit prose-weirpack; }).passthru.tests'
```
