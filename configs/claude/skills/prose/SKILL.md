---
name: prose
description: >-
  ALWAYS load this skill BEFORE writing ANY prose.
  This includes documentation, READMEs, commit messages, PR descriptions,
  code comments, docstrings, or any other text meant for a human reader.
---

# Prose

## Linter

A hook runs `prose-lint` after every Write and Edit of a markdown or
source file and returns its findings before your next turn. A Bash hook
lints the message that a `git commit` or `gh pr create` carries.
`prose-lint` tags each finding with its rule's tier, and the tier sets
what it asks of you.

A Required finding names an exact phrase with a mechanical fix, so fix
it. The Bash hook denies a commit or PR message with one every time.

A Recommended finding comes from a shape detector, so fix it unless it
misreads the sentence. The Bash hook denies a commit or PR message
with one once. When you judge the finding false, run the unchanged
command again and it passes.

An Optional finding is a suggestion, so fix it where the rewrite reads
better. It never denies a command on its own.

The rules are deliberately blunt, so a fix is usually cheaper than the
exception. The linter matches word lists, so it misses many sentences
with the shapes below. Write to the rule, not to the linter. Run
`prose-lint <file>` by hand on a file the hook does not cover, such as
one written through Bash.

## Sentence Shape

Make the actor the subject, so `the scheduler retries the job` rather
than `the job is retried`. Passive creeps into the back half of a
sentence, after `so`, `and`, or `which`, so check the second half of
every sentence as closely as the opening.

Each point gets its own sentence with a subject and a verb. Never use a
semicolon, and never put a colon between two clauses. If the words
after a colon have their own subject and verb, end the sentence there
with a period, or join the clauses with `because` or `so`.

Do not end a sentence on a participle tacked on after a comma (`,
returning an error`, `, ensuring the cache stays warm`). Do not wrap a
claim in `not only X but Y`. Do not hedge a claim. State it or drop it.
Put a negation on the verb, so `does not need npm` rather than `needs
no npm`.

Point `this` and `which` at a noun, never at a whole clause. In `This
means the pool is empty`, the reader has to pick which part of the last
sentence `this` names. Name the noun instead (`The nil default
means`).
Never write `respectively`, `the former`, or `the latter`. Each makes
the reader map items back to an earlier list, and the mapping breaks
when the list changes order. Repeat the noun.

Open a sentence with its real subject, not with `there is` or `it is`.
`There are three files that hold the config` becomes `Three files
hold the config`, and `It is likely that the pod restarts` becomes
`The pod might restart`.

Write what the system does in the simple present and a past event in
the simple past. Do not stack helping verbs around a participle
(`would have been configured`, `can be configured`). Give the verb an
actor instead (`you can configure`). The progressive names an ongoing
state, and these rules allow it (`is retrying`, `has been running`).

## Words

Give each word one meaning, so the reader never has to choose between
two. Some common words carry a second meaning, and each rule below bans
that second one.

- Never write `since` or `as` for a cause. Both also mark time. Write
  `because`.
- Never write `while` for a contrast, because it also marks time. Write
  `but`, or split the sentence.
- Never write `over`, `under`, `above`, or `below` for a quantity.
  Write `more than`, `fewer than`, or `less than`.
- Prefer a one-word verb to an idiomatic phrasal verb, so `find` rather
  than `figure out`, `start` rather than `kick off`, and `remove` rather
  than `get rid of`. The words of an idiom do not add up to its
  meaning, so a reader who does not know it cannot parse it.
- Never write an idiom (`heavy lifting`, `rule of thumb`, `behind the
  scenes`). Write its literal meaning.
- Spell out every negation, so `do not` rather than `don't`. A reader
  who misses the `n't` reads the opposite claim.
- Never write `and/or`. Write `the host, the port, or both`.
- Never form a plural with `(s)`, as in `file(s)`. Write the plural, or
  write `one or more`.
- Grade an obligation with `must` for a requirement or `should` for
  advice. `needs to`, `is expected to`, and `is recommended` hide the
  grade.
- Never write `may not`, because it reads as a prohibition or as a
  possibility. Write `must not` or `might not`.
- Never write `please`. Give the instruction.
- Write `whether`, not `if`, after a verb of checking, so `check
  whether the file exists`. After such a verb, `if` also reads as a
  condition.

## Specificity

Detail scales with proximity. A docstring describes its own method but
not the private helpers it calls. A README, announcement, or
introduction names only what a user can observe. It never names the data
structure behind the API, a buffer size, or a retry count that nothing
outside the code depends on. When an internal detail matters, state its
observable consequence instead.

A stated fact does not need reassurance. A follow-up that vouches for a
fact (`It does exactly that`), fences off a scope nobody questioned
(`returns the number, and nothing else`), or restates it in other words
names nothing the reader can observe, and the same words fit any fact in
any project's docs. A negation (`It does not execute the statement`) is
the same reassurance unless the opposite is a behavior a reader expects
from that name, the way `does not follow symlinks` corrects a real
expectation. Read every sentence's last clause on its own, because most
of these land there.

Name every item of a list, or say at the start that the list is
partial (`such as`). A list that ends with `etc.` or `and so on` leaves
its contents to the reader.

## Durability

Docs and comments stay in place while the code under them changes, so
write each sentence to stay true through those changes. A commit
message describes one change, and this section does not apply to it.

One fact per sentence also means that a change to the system edits or
deletes one sentence. Do not tie a fact to another fact that changes on
its own (`unlike the v1 API`, `faster than the old path`) unless the
comparison is the point. Do not date a claim (`currently`, `new`, `no
longer`). A count of things the system can grow (`the three modes`,
`both backends`) goes stale with the next one. Give the command that
regenerates the count, or drop it.

## Structure

Let ideas come in the number they naturally have. Do not force groups
of three.

One name per concept, including verbs. If the bucket `spends` tokens in
one sentence, it does not `hand out` or `consume` them in the next.
Repetition is clearer than synonym rotation.

## Headers

A header names the topic, and the body makes the claim. Keep every
header below the title to a short noun phrase, usually one to three
words. The request that asked for the document lists its topics in
sentence form (`cover how matching works and how the SDK caches rules`).
Copying that phrasing into headers turns the outline into a paraphrase
of the brief. Translate each item to its label (`Matching`, `Rule
Caching`) and let the body answer the `how`.
