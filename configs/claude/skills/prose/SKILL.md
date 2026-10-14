---
name: prose
description: >-
  ALWAYS load this skill BEFORE writing ANY prose.
  This includes documentation, READMEs, commit messages, PR descriptions,
  code comments, docstrings, or any other text meant for a human reader.
---

# Prose

## Linter

After every Write and Edit of a markdown or source file, a hook runs
`prose-lint` and returns its findings before your next turn. The Bash
hook also lints the message that a `git commit` or `gh pr create`
carries. `prose-lint` tags each finding with its rule's tier. Each tier
says how often its rules misread a sentence, so it sets what a finding
asks of you.

| Tier        | Rules match                      | Your fix                               | Commit or PR message                    |
| ----------- | -------------------------------- | -------------------------------------- | --------------------------------------- |
| Required    | An exact phrase                  | Apply the mechanical fix               | The hook denies it every time           |
| Recommended | A pattern that can misread       | Fix it unless it misreads the sentence | The hook denies it once, then passes it |
| Optional    | A pattern that suggests a change | Fix it where the rewrite reads better  | The hook never denies it                |

When you judge a Recommended finding false, run the unchanged command
again.

The rules are deliberately blunt, so a fix is usually cheaper than the
exception. The linter matches word lists, so it misses many sentences
with the patterns below. Write to the rule, not to the linter. Run
`prose-lint <file>` by hand on a file the hook does not cover, such as
one written through Bash.

## Sentences

Name the actor after a modal such as `must`, `should`, or `can` (`you
can configure the timeout`, not `the timeout can be configured`).
Elsewhere, name the actor when the reader cannot tell who acts (`the
scheduler retries the job`, not `the job is retried`). Passive creeps
into the back half of a sentence, after `so`, `and`, or `which`, so
check the second half of every sentence as closely as the opening.

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

Write a claim about every member of a class with a plural subject
(`Required findings name an exact phrase`) or with `every`. Keep `a`
and `an` for one instance that the sentence introduces. Definitions
keep `a` (`A task is a unit of work`). The singular
`a finding` reads two ways, as one particular finding or as any
finding. When each member has its own one of something, write `each`
(`each worker owns a queue`), because the plural `workers own a queue`
also reads as one shared queue. When the next sentence keeps the same
subject, open it with `it` or `they` instead of repeating the noun.

Open a sentence with its real subject, not with `there is` or `it is`.
`There are three files that hold the config` becomes `Three files
hold the config`, and `It is likely that the pod restarts` becomes
`The pod might restart`.

Write what the system does in the simple present and a past event in
the simple past. Keep `will` for a later event, so `running make
builds the binary` rather than `running make will build the binary`.
Do not stack helping verbs around a participle (`would have been
configured`, `can be configured`). Give the verb an actor instead
(`you can configure`). The progressive names an ongoing state, and
these rules allow it (`is retrying`, `has been running`).

## Words

Give each word one meaning, so the reader never has to choose between
two. Some common words carry a second meaning, and each rule below bans
that second one.

- Never write `since` or `as` for a cause. Both also mark time. Write
  `because`.
- Never write `while` for a contrast, because it also marks time. Write
  `but`, or split the sentence.
- Never write `once` for `after` or `when`, because it also counts one
  time (`runs once`). Write `after` or `when`.
- Never write `over`, `under`, `above`, or `below` for a quantity.
  Write `more than`, `fewer than`, or `less than`.
- Never write `biweekly`, `bimonthly`, or `biannual`, because each names
  two intervals. Write `twice a week` or `every two weeks`.
- Write `that` before a clause that picks out which thing you mean
  (`the file that holds the key`). Write `, which` before a clause that
  adds a fact about a thing already named (`the config file, which
  holds the key`).
- Never put `not` on the verb after `all` or `both` (`all nodes are not
  ready`), because it can deny `all` or each node. Write `not all` or
  `none`.
- Say whether a range includes each end (`between 1 and 100,
  inclusive`).
- Prefer a one-word verb to an idiomatic phrasal verb, so `find` rather
  than `figure out`, `start` rather than `kick off`, and `remove` rather
  than `get rid of`. The words of an idiom do not add up to its
  meaning, so a reader who does not know it cannot parse it.
- Never write an idiom (`heavy lifting`, `rule of thumb`, `behind the
  scenes`). Write its literal meaning.
- Never write `shape` or `X-shaped` for an interface, a type, or a
  format. The reader has to guess which one it means. Name it, so
  `implements Duck` rather than `Duck-shaped`, and `the same fields as
  the v1 response` rather than `the same shape as the v1 response`.
- Name the destination in link text (`the installation guide`), never
  `here` or `this page`.
- Spell out every negation, so `do not` rather than `don't`. A reader
  who misses the `n't` reads the opposite claim.
- Never join two words with a slash (`read/write`, `and/or`). Write
  `reads and writes`, or `the host, the port, or both`.
- Never form a plural with `(s)`, as in `file(s)`. Write the plural, or
  write `one or more`.
- Grade an obligation with `must` for a requirement or `should` for
  advice, in lowercase. `needs to`, `is expected to`, and `is
  recommended` hide the grade. For what the system does, write the
  present tense, so `the server listens` rather than `the server needs
  to listen`.
- Never write `may not`, because it reads as a prohibition or as a
  possibility. Write `must not` or `might not`.
- Write `might` for a possibility, `can` for an ability, and the present
  tense for a consequence (`the cache might be stale`, `the write
  fails`). Do not write `may`, `could`, or `would` for any of the
  three. Keep `may` for an option a person is free to take (`you may
  skip the tag`).
- Name the criterion instead of writing `as needed`, `where applicable`,
  or `appropriate`. `Set the timeout as needed` leaves the reader to
  guess what makes a timeout right (`longer than the slowest request`).
- Put `only` beside the word it limits, so `fails only on growth`
  rather than `only fails on growth`.
- Never write `please`. Give the instruction.
- Write `whether`, not `if`, after a verb of checking, so `check
  whether the file exists`. After such a verb, `if` also reads as a
  condition.
- Write `for example` or `such as` instead of `e.g.`, and `that is`
  instead of `i.e.`. Readers confuse the two, which turns a partial
  list into a complete one.

## Specificity

Detail scales with proximity. Docstrings describe their own method but
not the private helpers it calls. READMEs, announcements, and
introductions name only what a user can observe. They never name the
data structure behind the API, a buffer size, or a retry count that
nothing outside the code depends on. When an internal detail matters,
state its observable consequence instead.

Stated facts do not need reassurance. Follow-ups that vouch for a fact
(`It does exactly that`), fence off a scope nobody questioned
(`returns the number, and nothing else`), or reword the fact name
nothing the reader can observe. The same words fit any fact in any
project's docs. Negations (`It does not execute the statement`) are the
same reassurance unless a reader expects the opposite behavior from
that name. `does not follow symlinks` passes, because it corrects a
real expectation. Read every sentence's last clause on its own, because most
of these land there.

Name every item of a list, or say at the start that the list is
partial (`such as`). Lists that end with `etc.` or `and so on` leave
their contents to the reader.

## Durability

Docs and comments stay in place while the code under them changes, so
write each sentence to stay true through those changes. Commit
messages describe one change, and this section does not apply to them.

One fact per sentence also means that a change to the system edits or
deletes one sentence. Do not tie a fact to another fact that changes on
its own (`unlike the v1 API`, `faster than the old path`) unless the
comparison is the point. Do not date a claim (`currently`, `new`, `no
longer`). Name the version instead of `older`, `newer`, `recent`, or
`modern` (`kernels 5.8 and later`). Counts of things the system can
grow (`the three modes`, `both backends`) go stale when the system
adds one more. Give the command that regenerates the count, or drop
it.

Link a passage by name, never by position (`described above`, `the
next section`). Positional references break when someone moves the
passage or adds one between.

## Structure

Let ideas come in the number they naturally have. Do not force groups
of three.

Give each paragraph one topic and six sentences or fewer. Longer
paragraphs usually carry a second topic.

Give parallel content a parallel form. Number steps that run in order.
Put items that play the same role and each carry their own clause in a
bulleted list. Leave a series of short items in its sentence. In
markdown that renders tables, put items compared on two or more
attributes in a table.

Keep reasoning in prose, because a list drops the `because` and `so`
that tie one claim to the next. Give every element content. Do not
bullet a single point, and do not repeat a table in the sentences
around it. Do not add a heading or bold text only to break up a page.
The Layout section of the `technical-writing` skill covers documents.

One name per concept, including verbs. If the bucket `spends` tokens in
one sentence, it does not `hand out` or `consume` them in the next.
Repetition is clearer than synonym rotation. Pronouns are not synonyms,
so `it` and `they` still stand in for the subject of the sentence
before.

## Headers

Each header names a topic, and the body under it makes the claim. Keep every
header below the title to a short noun phrase, usually one to three
words. The request that asked for the document lists its topics in
sentence form (`cover how matching works and how the SDK caches rules`).
Copying that phrasing into headers turns the outline into a paraphrase
of the brief. Translate each item to its label (`Matching`, `Rule
Caching`) and let the body answer the `how`.
