---
name: technical-writing
description: >-
  ALWAYS Load BEFORE writing or editing any technical document (docs, READMEs,
  guides, RFCs, design docs, runbooks).
---

# Technical Writing

The `prose` skill sets the sentence rules (voice, specificity, punctuation,
concision, tense, word choice, durability, naming, and headers) and applies
throughout. This skill adds what a document needs beyond the sentence rules.

The Reader, Load, and Ambiguity rules adapt the writing rules of ASD-STE100
Simplified Technical English, not its dictionary. STE conflicts with these rules
in two places, and these rules win. STE turns verbs into nouns ("do a test of").
It writes "can" where these rules write "may" or "might", and "must" where they
write "should".

## 1. Mode

Each document uses one mode. Two questions pick it before the first line. Does
the content serve action or understanding? Does it serve learning or work? The
pair of answers names the mode.

- Action + learning: **tutorial**.
- Action + work: **how-to**.
- Understanding + work: **reference**.
- Understanding + learning: **explanation**.

Apply the two questions to the whole document, and to any sentence that you are
unsure about.

**Tutorial.** The learner's success is your job. Open with what they will build,
never with what they will "learn". Every step produces a visible result. State
what appears: the output, the prompt change, and the log line. Cut explanation
to one clause and a link. Write as "we", in commands: "First, do x. Now, do y."

**How-to.** Solve a problem that a person has, not an operation that a machine
performs. Assume competence and skip teaching. Do not add background or
completeness for its own sake. Link to it instead. Allow forks and judgment: "If
you want x, do y." Name the document by the task ("How to calibrate the radar
array").

**Reference.** Describe, and only describe. Reference gives no instruction, no
persuasion, and no opinion. State facts, options, limits, and errors without
hedging. Mirror the structure of the thing described, and generate from code
where you can.

**Explanation.** Cover one bounded topic that reads well away from the product.
Each title tolerates an implicit "About..." in front. Anchor on a real "why":
design decisions, history, constraints, and alternatives. Opinion belongs here
and nowhere else. Weigh the trade-offs and state your judgment. Do not list pros
and cons.

You must not mix modes in a reader-facing document. A tutorial carries no
reference table, reference carries no teaching, and a how-to carries no
argument. Split and link instead. A rulebook or a skill file can carry its
reference glossary inline, because the reader applies the rules at the point of
reading them.

## 2. Reader

Write for your reader, not about the system.

- Address the reader as "you", in the present tense. Reserve "will" for what
  happens later.
- Write instructions as commands: "Click Submit." Never write "should be done",
  and never narrate ("the component must be installed").
- Put the condition before the instruction: "To delete the document, click
  Delete." The reader skips what does not apply. Put the warning before the step
  it guards.
- Open a warning with the command or the condition. State the risk in the next
  sentence: "Do not delete the volume while the pod runs. The pod loses its
  data."
- A note gives information and never an instruction. A reader who skips every
  note still completes the procedure.
- Put the common case first and the exceptions after it.
- Never write `please` in an instruction. Never write `simply`, `easy`, or
  `quickly` in a procedure. A reader who found the step simple did not open the
  docs.
- Never pre-announce ("we will soon support...").
- Name the destination in the link text, and never write "click here". You
  should prefer a sentence of context on the page to a link off it.
- Use numbered lists for sequences and bullets everywhere else. Introduce a list
  with a complete sentence and keep the items parallel. Keep a list one level
  deep, and do not mix steps with descriptions in one list.
- Set code in code font, set UI elements in bold, and use serial commas.

## 3. Load

Each sentence carries one thing, so the reader loads one statement at a time.

- Give each sentence one instruction, or one thought everywhere else. Two
  instructions share a sentence only when the reader does both at the same time.
  You should split instructions past about 20 words, and other sentences past
  about 25.
- When you split a sentence, carry the relation into the second sentence with
  "so", "but", or "then".
- Give each paragraph one topic. Open it with the sentence that names the topic,
  and keep it to six sentences or fewer.
- Those limits are a ceiling, not a target. Split the sentence that carries two
  thoughts. Keep the long one that carries a single fact with its condition or
  consequence.
- Keep "the" and "a". "Remove backup file" reads two ways, "Remove the backup
  file" reads one.
- Give each word one meaning and one job. If "check" means inspect, do not also
  use it for restrain.
- Prefer a finite verb to an "-ing" modifier. A participle attached to a noun
  reads as a relative clause, an adjective, or a new predicate, and the reader
  picks the wrong one.

## 4. Ambiguity

No sentence is open to two readings.

- Keep "only" and "not" beside the word they change. "only fails on growth" and
  "fails only on growth" say different things.
- Keep a noun string to three nouns or fewer. "the proto import budget check
  script" becomes "the script that checks the proto-import budget".
- Make every "it", "they", and "this" point at one obvious noun. Repeat the noun
  whenever the reference is not obvious. Never point "this" or "which" at a
  whole clause.
- Give every clause its verb. "Phase 1 moves the converters and Phase 2 the
  runtime" leaves Phase 2 without one.
- Keep the small words that show structure. "Ensure that the switch is off"
  keeps "that" because it makes the sentence parse one way. Never trade clarity
  for word count.
- Repeat the article in a series of separate things: "the client and the host",
  not "the client and host".
- Say which parts "and" or "or" join when a sentence can group two ways.
  "Both...and", "either...or", and "if...then" are free disambiguators.
- Make text in parentheses a full grammatical unit, its own sentence, or a bare
  citation label. Never form plurals with "(s)".
- Never use a slash. Write `a, b, or both` instead of `a/b` or `and/or`.
- Reread every "with". It can name a companion, a tool, or a property, so
  "Restart the pod with the new config" reads two ways.
- Skip idioms, colloquialisms, and metaphors. A non-native reader, a translator,
  and an agent all parse plain constructions best.

## 5. Obligations

Grade every obligation, and give each word one meaning for the whole document. A
reader who cannot tell a hard rule from advice treats everything as optional, or
everything as mandatory, and both readings cause outages.

- **must**, **must not**: an absolute requirement or prohibition.
- **should**, **should not**: a recommendation. The reader may deviate after
  weighing the consequences, so say what those consequences are.
- **may**: a true option for a person, and the choice costs nothing either way.
  Write "might", not "may", for what a system might do.
- **can**: ability. "The client can retry" says retrying is possible, not
  permitted or advised.
- **might**: possibility. "The cache might be stale" is a fact, not a rule.
- **will**: later behavior. "The server will close idle connections after 30
  seconds" promises and never instructs. "Will" never carries an obligation.

Write `could` only as the past of `can` (`the parser could not read the file`).
Never write `could` or `would` for a possibility or a consequence. Write `might`
for a possibility and the present tense for a consequence: `the write fails`,
not `the write would fail`.

Replace every vague middle word with the graded one. `needs to`, `is expected
to`, `is required to`, `it is important to`, and `it is recommended that` all
hide which level applies. Other phrases hide it too.

Never write "should" for a hard requirement. Never write "must" for advice.
Never soften a `must` with `please` or `try to`. Keep the words lowercase.
Capitalizing them is a convention of standards documents and adds no clarity.

IMPORTANT: An obligation binds a person. Grade what the reader must do, and
leave the system's own behavior in the present tense. "The reaper must run a dry
run first" reads as a rule that the reader can break. The reaper is code, and
"the reaper runs a dry run first" is what you mean.

A rule with a documented default is not a "must". If an unset tag falls back to
`ephemeral`, setting the tag is a "may" or "should", and the reader who skips it
gets the fallback.

Grade obligations in every document. A runbook's data-loss warning and a
how-to's ordering constraint earn the same "must" as a clause in a standard.
Left bare, they read as one more step. An ordinary step does not take "must",
because a bare command already instructs.

## 6. Scope

These rules reach past the documentation set.

- PR descriptions and commit messages are writing too. Every layer except Mode
  applies to them.
- Product UI strings are not documentation. You must use the product's copy
  guidelines for those.
- A count or a directory tree in prose is true only at the commit that lands the
  number. You must include the command that regenerates it.

## 7. Example

Before:

> Configuration of the proto import ratchet budget script parameters is
> performed via budget.json. Note that it's important to remember that
> running with --write, which updates the committed budget to reflect the
> current count, should only be done when lowering it. If exceeded, CI fails.

After:

> `budget.mjs` reads the committed budget from `budget.json` and counts the
> files that import protos. If the count exceeds the budget, CI fails. You must
> run `budget.mjs --write` only to lower the budget.

By layer: "`budget.mjs` reads" supplies an actor (`prose`, Active Voice), and
the script's real name replaces "ratchet" (`prose`, Specificity). The rewrite
breaks the five-noun string into clauses, and "if exceeded" gains its subject,
the count (Ambiguity). The failure condition moves ahead of the step it
explains, and "only" sits beside the verb it changes (Reader, Load). The rewrite
grades the restriction instead of leaving it bare (Obligations). The hedge is
gone (`prose`, Concision).
