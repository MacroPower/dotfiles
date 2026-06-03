---
name: self-improve
description: >-
  Retrospective on the current session's friction (broken tool calls,
  repeated prompts, wasted effort), with proposed fixes.
---

# Self-Improve

Look back over this session, find where you hit friction, and propose fixes
to the user's dotfiles repo at `~/Documents/repos/dotfiles`. The rough edges
you just hit should turn into concrete changes to the config that shapes
future sessions.

This is a **report-only** skill. Diagnose and propose; do not edit any files.
The user decides what to act on.

## Mindset

You are writing an honest retrospective, not padding a list. A short report
with two real findings beats ten speculative ones. The value is in catching
things that will recur -- a missing permission that prompts every time, a
CLAUDE.md line that misled you, a tool you used wrong because nothing told you
the right way. One-off slips that were your own fault and won't repeat are
noise; leave them out or note them briefly without a proposed fix.

Cite evidence. Every finding should point at something that actually happened
this session -- quote the tool error, the denied command, the user's
correction, the dead end you backtracked from. If you can't point to it, you're
inventing it.

## Step 1: Gather the friction

Re-read the session looking for:

- **Tool calls that broke**: errors, denials (a Bash command blocked by
  hook-router, a fetch blocked by the allowlist), MCP tools that returned
  unexpected shapes or that you called the wrong way.
- **Recurring permission prompts**: the same safe command asking for approval
  over and over.
- **Confusing or contradictory guidance**: a CLAUDE.md line, skill, or agent
  instruction that pointed you wrong, was ambiguous, or contradicted what the
  user actually wanted.
- **Missing context**: something you had to discover by trial and error that a
  doc could have told you up front.
- **Repeated manual work**: a multi-step dance you did by hand that a skill,
  script, or Taskfile target could capture.
- **Context bloat**: actions that pulled far more into the conversation than
  the task needed. Look for whole large files read when one section would do,
  unfiltered command output dumped into context (a full log instead of a
  grep), broad searches run inline instead of delegated to an Explore agent,
  re-reads of files already in context, and verbose MCP responses you only
  needed one field from. A single oversized read can cost more context than
  the rest of the session combined, and that cost compounds: it triggers
  earlier compaction, which loses detail and causes rework. Only flag cases
  where guidance or tooling could have steered around it.
- **User corrections**: anywhere the user redirected you. Each is a signal
  that something upstream set the wrong expectation.

If the session is long and context was compacted, detail may have dropped out.
Reflect on what's in context; you don't need the raw transcript unless the user
points you at one.

## Step 2: Diagnose root cause

For each friction point, ask: *was this systemic, or a one-off?* Then trace
each systemic one to a cause specific enough to fix. "The fetch failed" is an
observation; "the host isn't in the mcp-fetch allowlist" is a cause you can
route.

## Step 3: Route each fix to the right place

Our dotfiles repo is **nix-darwin + home-manager**, and Claude Code's entire
config is generated from it and symlinked into `~/.claude/`.

**Never propose editing anything under `~/.claude/` directly.** Those are
symlinks; the next `home-manager` activation overwrites them. Always route to
the source in the dotfiles repo.

Routing table -- map the cause to its source file:

| Friction | Source to change |
|---|---|
| A skill gave bad/missing guidance | `configs/claude/skills/<name>/SKILL.md` |
| An agent underperformed | `configs/claude/agents/<name>.md` |
| A **new** skill or agent is warranted | create the file above **and** register it in the `dotfiles.claude.skills` / `.agents` attrset in `home/claude.nix` (recommend the `/skill-creator` skill) |
| Recurring permission prompt on a safe command | `permissions.allow` via `home/claude.nix` (or recommend the `/fewer-permission-prompts` skill) |
| A safe Bash command was wrongly denied | the `commandRules` / `extraCommandRules.deny` entry in `home/claude.nix` -- relax the rule or add an `except` |
| A fetch was blocked by the allowlist | add the host to `extraFetchRules.allow` (or the bundle's `fetchRules`) in `home/claude.nix` |
| A formatter mangled or skipped a file | `formatterRules` in `home/claude.nix` |
| You used an MCP tool wrong | add/clarify a one-liner in the tool bundle's `instructions.items` in `home/claude.nix` -- those render into the global CLAUDE.md `## Tools` section |
| A context-bloating habit (see Step 1) | a working-style line in global CLAUDE.md (the heredoc in `home/claude.nix`) or the tool bundle's `instructions.items` -- whichever shapes the behavior at its source |
| A general "how to work here" rule was missing/wrong | global CLAUDE.md prose lives in the `".claude/CLAUDE.md".text` heredoc in `home/claude.nix` (Writing Style, Agents); project rules live in `CLAUDE.md` at the repo root (recommend the `/revise-claude-md` skill for these) |
| Settings, hooks, MCP servers, env, status line | `home/claude.nix` |
| Wrong/confusing repo code or docs unrelated to Claude | the relevant `.nix` / config file directly |

Keep proposals minimal and in the repo's idiom: explicit config over magic,
present-tense comments, named lists over discovery. A proposal that fights the
house style won't get applied.

## Step 4: Write the report

Use this structure. Tag each finding **docs**, **code**, or **tooling**, and
give a confidence level so the user can triage.

```
# Self-Improvement Review

## Session summary
<1-2 lines: what this session was actually about>

## Findings

### 1. <short title> [tooling] (confidence: high)
- **What happened**: <evidence -- quote the error / denial / correction>
- **Root cause**: <the specific, fixable cause>
- **Proposed fix**: `<file path>` -- <the concrete change>
- **How to apply**: <hand edit + `task switch`, or `/fewer-permission-prompts`, etc.>

### 2. ...

## Noted, not proposed
<one-off slips or low-value observations, one line each -- or omit if none>
```

If you find nothing systemic worth fixing, say so plainly. "This session ran
clean; nothing to propose" is a perfectly good result and far better than
manufacturing findings to fill the template.
