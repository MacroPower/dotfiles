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

Detail scales with proximity. A docstring describes its own method but not the
private helpers it calls. A README, announcement, or introduction names only
what a user can observe, never the data structure behind the API, a buffer size,
or a retry count nothing outside the code depends on. When an internal detail
matters, state its observable consequence instead.

## 3. Punctuation

Commas, periods, and conjunctions carry ordinary clause breaks. The em dash and
the colon both announce a reveal, and a page with several reads as sales copy.
Save the em dash for the rare break that earns it. A colon does three jobs only.
It introduces a list, introduces a literal (a quoted string, a command, a
value), or labels a line ("Note: ..."). It never introduces an explanation,
consequence, restatement, or example sentence, however natural it feels there.

The common misuse is a colon standing in for a banned em dash:

- "A rate of zero is the mirror image: the bucket never refills"
- "v1.0 is out: a thread-safe token bucket for Go"

Each reads identically with an em dash in the colon's place, which is the tell.
If the words after the colon have their own subject and verb, or an em dash
would fit the same slot, the colon is wrong.

## 4. Concision

Prefer the short everyday form: "to" over "in order to", "use" over "utilize"
and "leverage", "because" over "due to the fact that". State the fact and let
the reader judge its weight; "stands as", "serves as", and "plays a vital role"
puff without informing. One hedge per claim at most.

## 5. Sentence Scope

Each point gets its own sentence with a concrete subject and verb. Do not end a
sentence on a participle tacked on for depth (", ensuring...", ",
highlighting...", ", fostering..."). Make the direct claim instead of wrapping
it in "not only X but Y" or "X, not just Y"; if the contrast matters, give each
side its own sentence.

## 6. Tense

Durable prose (docs, READMEs, comments, docstrings) describes the system as it
is, never as a delta from a prior version: no "now supports", "previously", "the
new flag". Git history records what changed. Prose whose job is describing a
change (commit messages, PR descriptions, changelogs) is exempt.

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
tells are sentence shapes: a leading "how", "why", "what", or "when"; a verb;
"and" or a comma joining two ideas; a question mark. Drop the frame and keep the
noun:

- "How matching works" -> "Matching"
- "Why we're doing this" -> "Motivation"
- "Deletion behavior and safety" -> "Deletion"
- "Reusing a flag key after the original ships" -> "Key Reuse"

The request that asked for the document lists its topics in sentence form
("cover how matching works and how the SDK caches rules"); copying that phrasing
into headers turns the outline into a paraphrase of the brief. Translate each
item to its label and let the body answer the "how".
