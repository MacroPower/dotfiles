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
source file and returns its findings before your next turn, and a Bash
hook denies a `git commit` or `gh pr create` whose message breaks a
rule. Fix every finding rather than arguing with it. The rules are
deliberately blunt, so the fix is mechanical and a rewrite costs less
than the exception. The linter matches word lists, so it misses many
sentences with the shapes below; write to the rule, not to the
linter. Run `prose-lint <file>` by hand on a file the hook does not
cover, such as one written through Bash.

## Sentence Shape

Make the actor the subject, so `the scheduler retries the job` rather
than `the job is retried`. Passive creeps into the back half of a
sentence, after `so`, `and`, or `which`, so check the second half of
every sentence as closely as the opening.

Each point gets its own sentence with a subject and a verb. Never put a
colon between two clauses; if the words after a colon have their own
subject and verb, end the sentence there with a period, or join the
clauses with `because` or `so`. Do not end a sentence on a participle
tacked on after a comma (`, returning an error`, `, ensuring the cache
stays warm`), and do not wrap a claim in `not only X but Y`. One hedge
per claim at most.

## Specificity

Detail scales with proximity. A docstring describes its own method but
not the private helpers it calls. A README, announcement, or
introduction names only what a user can observe, never the data
structure behind the API, a buffer size, or a retry count nothing
outside the code depends on. When an internal detail matters, state its
observable consequence instead.

A stated fact needs no certificate. A follow-up that vouches for a fact
(`It does exactly that`), fences off a scope nobody questioned (`returns
the number, and nothing else`), or restates it in other words names
nothing the reader can observe, and the same words fit under any fact
in any project's docs. A negation (`It does not execute the statement`)
is the same certificate unless the opposite is a behavior a reader
would expect from that name, the way `does not follow symlinks`
corrects a real expectation. Read every sentence's last clause on its
own, since that is where most of these land.

## Structure

Let ideas come in the number they naturally have; do not force groups of
three.

One name per concept, including verbs. If the bucket `spends` tokens in
one sentence, it does not `hand out` or `consume` them in the next;
repetition is clearer than synonym rotation.

## Headers

A header names the topic; the body makes the claim. Keep every header
below the title to a noun phrase of one to three words. The request
that asked for the document lists its topics in sentence form (`cover
how matching works and how the SDK caches rules`); copying that
phrasing into headers turns the outline into a paraphrase of the brief.
Translate each item to its label (`Matching`, `Rule Caching`) and let
the body answer the `how`.
