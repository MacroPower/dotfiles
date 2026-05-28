---
name: worktree
description: Launch one or more tasks in new git worktrees using workmux.
allowed-tools: Bash, Write
---

Launch one or more tasks in new git worktrees using workmux.

Tasks: $ARGUMENTS

## You are a relay, not an author

The worktree agent has the same tools you do — Read, Grep, Glob, Bash, the
Explore subagent. It will plan and implement. Your job is to relay the user's
intent and any context already established in this conversation, then get out
of the way.

If you invent details the user didn't give you — file paths, function names,
a proposed approach, what to change — the worktree agent treats them as
ground truth and builds on top of fabricated context. Anything you write that
isn't grounded in the user's message, this conversation, or a file you just
re-read is a hallucination the agent will commit to.

## Size the prompt to the context that exists

The prompt's length should track the context the worktree agent will
inherit, not how thorough you'd like to sound.

**Cold-start tasks** (user's request stands alone, no relevant prior
conversation): relay the task in the user's framing, plus pointers to
anything authoritative — spec file paths, PR numbers, issue links. That's
it. The agent explores from there.

**Tasks built on this conversation** (user says "do option 2", "use the
approach we discussed", "apply the plan we just drafted"): the agent does
not see this conversation. Summarize the decisions that actually matter
for the task — what was chosen, what was ruled out, any constraints the
user named — and include that in the prompt. Stick to things that were
actually said; don't fill gaps with plausible-sounding extras.

`--fork` copies the entire conversation into the new worktree, which
bloats the agent's context with everything you discussed, including
tangents and dead ends. Reach for it only when the discussion is dense
enough that a faithful summary would be nearly as long, or when the user
explicitly asks to fork.

**Tasks pointing at a file** (plan, spec, notes): re-read the file so any
recent edits are reflected, then cite the relative path in the prompt.
Don't paraphrase the file's contents — point at it.

## Keep out of the prompt

- File paths, function names, module names the user didn't mention and that
  don't appear in a file you just re-read.
- A step-by-step implementation plan, unless the user gave one. The agent
  plans its own work.
- Your guess at what they "probably mean." If you're guessing, relay
  as-is and let the agent ask or explore.
- Generic preamble ("This is an important task...", "Be careful to...",
  "Make sure to think carefully...").

## Don't explore the codebase

Reading, grepping, or spawning Explore from the dispatcher seat is wasted
work — the agent redoes it with proper context. Skip it.

The only file to read is a markdown file the user explicitly referenced
(plan, spec, notes), and only to confirm its current content before citing
the path. Even then, cite the path; don't copy the contents into the prompt.

If the user's message is too thin to write even a faithful relay, relay it
as-is. The agent can ask follow-up questions or explore. Don't paper over
thinness by guessing.

## Skill delegation

If the user references a skill (e.g., `/auto`, `/plan-review`), instruct
the agent to use that skill rather than writing out manual steps. Pass
through any flags the user gave.

Example prompt body:

```
[Task in the user's framing]

Use the skill: /skill-name [flags] [task]
```

## Per-task steps

For each task:

1. Generate a short, descriptive worktree name (2-4 words, kebab-case).
2. Write the prompt to a temp file.
3. Run `workmux add <worktree-name> -b -P <temp-file>`.

Use relative paths in the prompt — each worktree has its own root.

## Flags

**`--merge`**: When passed, add instruction to use `/merge` skill at the end to
commit, rebase, and merge the branch.

```
...
Then use the /merge skill to commit, rebase, and merge the branch.
```

Only instruct worktree agent to `/merge` if explicitly requested by user in
task.

**`--fork`**: When passed, add `--fork` to the `workmux add` command. This copies
the current conversation into the new worktree so the agent resumes with full
context of what was discussed. Useful when the current conversation has built up
context that the new worktree agent needs.

## Workflow

Write ALL temp files first, THEN run all workmux commands.

**IMPORTANT:** Run `workmux add` from the CURRENT directory. Do NOT `cd` to the
main repo or any other directory. The new worktree branches from whatever branch
is checked out in the current directory.

Step 1 - Write all prompt files (in parallel):

```bash
tmpfile=$(mktemp).md
cat > "$tmpfile" << 'EOF'
[Prompt content]
EOF
echo "$tmpfile"
```

Step 2 - After ALL files are written, run workmux commands (in parallel):

```bash
workmux add feature-x -b -P /tmp/tmp.abc123.md
workmux add feature-y -b -P /tmp/tmp.def456.md
```

After creating the worktrees, inform the user which branches were created.

**Remember:** Your task is COMPLETE once worktrees are created. Do NOT implement
anything yourself.
