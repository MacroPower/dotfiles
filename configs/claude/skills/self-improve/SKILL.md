---
name: self-improve
description: >-
  Retrospective on the current session's friction (broken tool calls,
  repeated prompts, wasted effort).
---

# Self-Improve

Review this session, find where you hit friction, and file each problem as
an aven task in the `dotfiles` project, written as a bug report. The
dotfiles repo generates Claude Code's whole config: skills, agents, hooks,
permission rules, MCP servers, and the global CLAUDE.md. Friction from any
of those belongs in a task.

Describe the problem and stop there. Do not read the dotfiles repo, trace
the cause into its files, or propose a fix. Whoever takes the task
investigates the cause in the repo. Your job is to record what happened
while the evidence is still in context.

## Mindset

File honest tasks. Two real tasks beat ten speculative ones. The value is
in catching problems that recur. Examples include a missing permission that
prompts every time, a CLAUDE.md line that misled you, and a tool you used
wrong because nothing told you the right way.

Cite evidence. Every task points at something that happened this session:
the tool error, the denied command, the user's correction, the dead end you
backtracked from. If you cannot point to it, you are inventing it.

## Step 1: Friction

Re-read the session for these kinds of friction:

- **Tool calls that broke**: errors, denials, and MCP tools that returned
  unexpected output or that you called the wrong way. Denials include a Bash
  command that hook-router blocked and a fetch that the allowlist blocked.
- **Recurring permission prompts**: the same safe command, asking for
  approval every time it runs.
- **Confusing or contradictory guidance**: a CLAUDE.md line, skill, or agent
  instruction that misled you, was ambiguous, or contradicted what the user
  wanted.
- **Missing context**: a fact that you found by experiment because no doc
  stated it.
- **Repeated manual work**: a multi-step sequence you did by hand that a
  skill, script, or Taskfile target can capture.
- **Context bloat**: actions that pulled far more into the conversation than
  the task needed, where guidance or tooling can prevent the bloat. Signs
  include whole large files read when one section suffices, and unfiltered
  command output dumped into context (a full log instead of a grep). Other
  signs are broad searches run inline instead of delegated to an Explore
  agent, and re-reads of files already in context. Verbose MCP responses
  count when you needed only one field from them.
- **User corrections**: anywhere the user redirected you. Each correction
  signals that something upstream set the wrong expectation.

Context bloat earns a task even when nothing broke. A single oversized read
can cost more context than the rest of the session combined. That cost
compounds, because it triggers earlier compaction, which loses detail and
causes rework.

Compaction drops detail from long sessions. Work from what is in context,
and read the raw transcript only when the user points you at one.

## Step 2: Triage

For each friction point, decide whether it is systemic or a one-off.
Systemic friction recurs in later sessions until someone changes the
config. Keep each systemic friction point as a problem to file. Drop the
one-offs, such as a slip that was your own fault and does not recur.

## Step 3: Duplicates

Load the `aven` skill before running `aven` commands. For each problem,
search the `dotfiles` project for an existing task:

```sh
aven search --project dotfiles "<key terms>"
```

- **Open match**: append this session's evidence with `aven note <ref>`
  instead of filing a duplicate.
- **Done match**: the problem returned after the fix. File a new task, then
  link the two with `aven related add <new-ref> <old-ref>`.
- **Canceled match**: the user decided not to pursue the problem. Do not
  refile it. Mention the ref in your summary.

## Step 4: Filing

File one task per problem, so the user can triage and close each task
separately. Pass `--project dotfiles` because the session might run in
another repo, and aven otherwise files the task under that repo's project.

```sh
aven add --project dotfiles --label self-improve "<title>" \
  --description-stdin <<'EOF'
## Observed
<the behavior you saw, and how many times it happened this session>

## Expected
<the behavior you expected>

## Reproduction
<the minimal context that triggers it: the repo, the request, the tool call>

## Evidence
<verbatim error text, the denied command, or the user's words>
EOF
```

Write the title in sentence case and name the symptom, not the fix. Write
`Fetch allowlist blocks docs.rs`, not `Add docs.rs to the fetch allowlist`.

When the session shows where a behavior came from, quote that source under
Evidence. Examples include a hook denial that names its rule and the skill
line that told you to act. Do not search for the source beyond what the
session shows.

Redact secrets, tokens, and credentials from every quoted error and command.
Leave the status at the `inbox` default and the priority unset, so the user
sets both during triage.

## Step 5: Summary

Tell the user, in a few lines, which tasks you filed and which existing
tasks you added notes to. Give each ref and its title. If the session had no
systemic friction, say that you filed nothing. Filing nothing beats
inventing tasks.
