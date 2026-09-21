---
name: prose
description: >-
  ALWAYS load this skill BEFORE writing ANY prose.
  This includes documentation, READMEs, commit messages, PR descriptions,
  code comments, docstrings, or any other text meant for a human reader.
---

# Prose

Apply these rules to all prose.

## 1. Active Voice

Make the actor the subject, so "the scheduler retries the job" rather than "the
job is retried". Passive creeps in where the actor feels obvious, and mostly in
the back half of a sentence, after "so", "and", or "which":

- "All mutation is guarded by a lock" -> "A lock guards all mutation"
- "Must be called while holding the lock" -> "Call this while holding the lock"
- "Files with a unique size have no twin, so those files are dropped" -> "so
  dupefile drops those files"

Check the second half of every sentence as closely as the opening. Passive is
right only when the actor is unknown or irrelevant, or the receiver is the topic
of the surrounding sentences.

## 2. Specificity

Name the mechanism, the guarantee, or a number the reader can observe and rely
on (a default, a limit, a return value), never the impression. "SQL you can
read" describes a feeling; "`.toSQL()` returns the exact string sent to the
database" states a fact. Every sentence passes two tests. You can restate it as
a concrete instruction, fact, or number, and it could not appear unchanged in
another project's docs.

A stated fact needs no certificate. Once a sentence says what a function does,
a follow-up that vouches for it ("That is the entire point of the function",
"This is by design", "It does exactly that") names nothing the reader can
observe, and it fails the second test outright, since the same words fit under
any fact in any project's docs. Swapping in a negation ("It does not execute
the statement") is the same certificate unless the opposite is a behavior a
reader would expect from that name, the way "does not follow symlinks" corrects
a real expectation.

Detail scales with proximity. A docstring describes its own method but not the
private helpers it calls. A README, announcement, or introduction names only
what a user can observe, never the data structure behind the API, a buffer size,
or a retry count nothing outside the code depends on. When an internal detail
matters, state its observable consequence instead.

## 3. Punctuation

Never put a colon between two clauses. What follows a colon is a list, a
literal (a quoted string, a command, a path, a value), or the body of a
labeled line ("Note: ..."). If the words after a colon have their own subject
and verb, the colon is wrong, so end the sentence there with a period:

- "Robots handling fails open: any fetch error counts as allowed" ->
  "Robots handling fails open. Any fetch error counts as allowed."
- "The check is a heuristic: a short page with no script also trips it" ->
  "The check is a heuristic. A short page with no script also trips it."
- "v1.0 is out: a thread-safe token bucket for Go" -> "v1.0 ships a
  thread-safe token bucket for Go."

The habit is a claim with its reason or example spliced on after a colon, and
it grows with the length of the document, because each one feels natural on
its own. When a reason must stay attached, join it with "because" or "so".

Commas, periods, and conjunctions carry ordinary clause breaks. Save the em
dash for the rare break that earns it; a page with several reads as sales
copy.

## 4. Concision

Prefer the short everyday form. Write "to" over "in order to", "use" over
"utilize" and "leverage", "because" over "due to the fact that". State the fact
and let the reader judge its weight; "stands as", "serves as", and "plays a
vital role" puff without informing. One hedge per claim at most.

The same puff arrives as a word, clause, or sentence that insists on a fact
already stated, or fences off a scope nobody questioned:

- ".Number() returns the number, and nothing else" -> ".Number() returns the
  number"
- "As the name suggests, Flush simply writes the buffer to disk, no more and no
  less" -> "Flush writes the buffer to disk"
- "Close releases the file handle. This is deliberate." -> "Close releases the
  file handle."
- "Reset clears the counter to zero, which is exactly what you would expect" ->
  "Reset clears the counter to zero"
- "Value returns the current count without changing it" -> "Value returns the
  current count"
- "Len returns the number of entries. No magic, no caching, no surprises." ->
  "Len returns the number of entries."

The writer is weighing the fact on the reader's behalf. Delete any of these when
the facts survive without it, and read every sentence's last clause on its own,
since that is where most of them land.

## 5. Sentence Scope

Each point gets its own sentence with a concrete subject and verb. Do not end a
sentence on a participle tacked on for depth (", ensuring...", ",
highlighting...", ", fostering..."). Make the direct claim instead of wrapping
it in "not only X but Y" or "X, not just Y"; if the contrast matters, give each
side its own sentence.

## 6. Tense

Durable prose (docs, READMEs, comments, docstrings) describes the system as it
is, never as a delta from a prior version, so no "now supports", "previously",
or "the new flag". Git history records what changed. Prose whose job is
describing a change (commit messages, PR descriptions, changelogs) is exempt.

## 7. Structure

Let ideas come in the number they naturally have; do not force groups of three.

One name per concept, including verbs. If the bucket "spends" tokens in one
sentence, it does not "hand out" or "consume" them in the next; repetition is
clearer than synonym rotation.

## 8. Headers

A header names the topic; the body makes the claim. Keep every header below the
title to a noun phrase of one to three words. A header that narrates ("How
matching works", "Deletion behavior and safety") scans slower, drifts out of
date as the body changes, and breaks its fragment link on every rewording. The
tells are a leading "how", "why", "what", or "when", a verb, "and" or a comma
joining two ideas, or a question mark. Drop the frame and keep the noun:

- "How matching works" -> "Matching"
- "Why we're doing this" -> "Motivation"
- "Deletion behavior and safety" -> "Deletion"
- "Reusing a flag key after the original ships" -> "Key Reuse"

The request that asked for the document lists its topics in sentence form
("cover how matching works and how the SDK caches rules"); copying that phrasing
into headers turns the outline into a paraphrase of the brief. Translate each
item to its label and let the body answer the "how".
