---
name: prose
description: >-
  ALWAYS load this skill BEFORE writing ANY prose.
  This includes documentation, READMEs, commit messages, PR descriptions,
  code comments, docstrings, or any other text meant for a human reader.
---

# Prose

Apply these rules to all prose.

## Linter

A hook runs `prose-lint` after every Write and Edit of a markdown or
source file and returns its findings before your next turn. It reports
passive voice, a colon between two clauses, a participle tacked on after
a comma (`, ensuring`), puff words (`simply`, `serves as`), certificates
(`by design`, `no surprises`), delta tense in durable prose (`now
supports`, `previously`), `not only`, em dashes, and a header that
narrates instead of naming its topic. The commit skills run the same
check on commit messages. Fix every finding rather than arguing with it.
A rewrite that satisfies the rule costs less than the exception, and the
rule is deliberately blunt so the fix is mechanical. Run `prose-lint
<file>` by hand on a file the hook does not cover, such as one written
through Bash.

## Active Voice

Make the actor the subject, so `the scheduler retries the job` rather
than `the job is retried`. Passive creeps in where the actor feels
obvious, and mostly in the back half of a sentence, after `so`, `and`,
or `which`, so check the second half of every sentence as closely as the
opening. Passive is right only when the actor is unknown or irrelevant,
or the receiver is the topic of the surrounding sentences.

## Specificity

Name the mechanism, the guarantee, or a number the reader can observe
and rely on (a default, a limit, a return value), never the impression.
`SQL you can read` describes a feeling; `.toSQL() returns the exact
string sent to the database` states a fact. Every sentence passes two
tests. You can restate it as a concrete instruction, fact, or number,
and it could not appear unchanged in another project's docs.

A stated fact needs no certificate. Once a sentence says what a function
does, a follow-up that vouches for it (`That is the entire point of the
function`, `It does exactly that`) names nothing the reader can observe,
and it fails the second test outright, since the same words fit under
any fact in any project's docs. Swapping in a negation (`It does not
execute the statement`) is the same certificate unless the opposite is a
behavior a reader would expect from that name, the way `does not follow
symlinks` corrects a real expectation.

Detail scales with proximity. A docstring describes its own method but
not the private helpers it calls. A README, announcement, or
introduction names only what a user can observe, never the data
structure behind the API, a buffer size, or a retry count nothing
outside the code depends on. When an internal detail matters, state its
observable consequence instead.

## Concision

State the fact and let the reader judge its weight. One hedge per claim
at most. Puff also arrives as a word, clause, or sentence that insists
on a fact already stated, or fences off a scope nobody questioned
(`returns the number, and nothing else`, `without changing it`). The
writer is weighing the fact on the reader's behalf. Delete any of these
when the facts survive without it, and read every sentence's last clause
on its own, since that is where most of them land.

## Structure

Let ideas come in the number they naturally have; do not force groups of
three.

One name per concept, including verbs. If the bucket `spends` tokens in
one sentence, it does not `hand out` or `consume` them in the next;
repetition is clearer than synonym rotation.

## Headers

A header names the topic; the body makes the claim. Keep every header
below the title to a noun phrase of one to three words, so `How matching
works` becomes `Matching` and `Reusing a flag key after the original
ships` becomes `Key Reuse`.

The request that asked for the document lists its topics in sentence
form (`cover how matching works and how the SDK caches rules`); copying
that phrasing into headers turns the outline into a paraphrase of the
brief. Translate each item to its label and let the body answer the
`how`.
