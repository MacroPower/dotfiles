---
name: prose
description: >-
  ALWAYS load this skill BEFORE writing ANY prose.
  This includes documentation, READMEs, commit messages, PR descriptions,
  code comments, docstrings, or any other text meant for a human reader.
---

# Prose

Apply these rules to all prose.

## 1. Active voice, named actor

Make the actor the subject, so "the scheduler retries the job" rather than "the
job is retried". Passive voice hides who does what, and it creeps in most where
the actor feels obvious:

- "All mutation is guarded by a lock" -> "A lock guards all mutation"
- "Refills are computed lazily on each acquisition" -> "Each acquisition
  computes the refill lazily"
- "Must be called while holding the lock" -> "Call this while holding the lock"

Passive rarely survives as a whole sentence; it sneaks into the back half of
one, in clauses after "so", "and", or "which": "files with a unique size have no
twin, so those files are dropped" -> "so dupefile drops those files". Check the
second half of every sentence, not just the opening.

Passive earns its place only when the actor is genuinely unknown or irrelevant,
or the receiver is the topic of the surrounding sentences.

## 2. Say what it does, not how it feels

Name the mechanism, the guarantee, or a number the reader can observe or rely on
(a default, a limit, a return value); never the impression, and never an
internal constant (a read-buffer size, a retry count nothing outside the code
depends on). "SQL you can read" and "you stay in control" describe feelings. The
useful version is "`.toSQL()` returns the exact string sent to the database" and
"you choose which file survives". Every sentence passes two tests: you can
restate it as a concrete instruction, fact, or number, and it could not appear
unchanged in another project's docs.

IMPORTANT: Specificity must scale with proximity. A docstring may detail its own
method, but it does _not_ name the private helpers that method calls. A README,
announcement, or introduction page names ONLY what a user can observe. If a
detail matters, state its *observable consequence* instead.

## 3. Ordinary breaks get ordinary punctuation

Commas, periods, and conjunctions carry ordinary clause breaks. The em dash and
the colon are both hinges that announce a reveal, and a page with several of
them reads as sales copy. Save the em dash for the rare break that earns it. Use
colon only for specific jobs: introducing a list, introducing a literal (a
quoted string, a command, a value), or labeling a line (e.g. "Note: ...").

The colon's most common misuse is standing in for an em dash. When an
ASCII-only rule or a "no em dashes" rule takes the dash away, the sentence
that wanted one tends to keep its shape and swap in a colon:

- "A rate of zero is the mirror image: the bucket never refills"
- "v1.0 is out: a thread-safe token bucket for Go"
- "Lazy refill charges the arithmetic to the caller instead: Take runs in 40ns"

Each of these reads identically with a dash in the colon's place, which is the
tell. The reader does not get a list or a literal; they get a second clause
spliced onto the first. What follows a colon is never an explanation, a
consequence, a restatement, or an example sentence, however natural the colon
feels there. If the words after it have their own subject and verb, or a dash
would fit in the same slot, the colon is wrong.

## 4. Every word earns its place

Prefer the short, everyday form: "to" over "in order to", "use" over "utilize"
and "leverage", "because" over "due to the fact that". State the fact plainly
and let the reader judge its significance; claims that something "stands as",
"serves as", or "plays a vital role" puff up importance without adding
information. One hedge per claim at most.

## 5. One point, one sentence

A point worth making gets its own sentence with a concrete subject and verb. Do
not end sentences on a trailing participle tacked on for depth (", ensuring...",
", highlighting...", ", fostering..."). Make the direct claim instead of
wrapping it in scaffolding like "not only X but Y" or "X, not just Y"; if the
contrast matters, give each side its own plain sentence.

## 6. Timeless present tense

Durable prose (docs, READMEs, comments, docstrings) describes the system as it
is, never as a delta from a prior version: no "now supports", "previously", "the
new flag". Git history records what changed. Text whose whole job is describing
a change (commit messages, PR descriptions, changelogs) is exempt.

## 7. Let structure follow content

Let ideas come in whatever number they naturally have; don't force groups of
three.

Pick one name per concept and keep it; repetition is clearer than synonym
rotation, and this includes verbs. If the bucket "spends" tokens in one
sentence, it does not "hand out" or "consume" them in the next.
