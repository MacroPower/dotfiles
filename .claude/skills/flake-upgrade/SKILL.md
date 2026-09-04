---
name: flake-upgrade
description: >-
  Upgrade this repo's Nix flake inputs the right way -- bump the lock, then fan out
  parallel agents to read each input's upstream changelog and adapt the config (migrate
  off deprecated options, adopt worthwhile new features) before verifying the flake
  still evaluates. Use whenever the user asks to update, upgrade, or bump flake inputs
  or flake.lock (all inputs or a specific one), asks what changed upstream in an input
  like home-manager or stylix, or asks to bring the dotfiles up to date. Do not just
  run `nix flake update` on its own for these requests -- that is exactly the blind bump
  this skill exists to avoid.
---

# Flake Upgrade

A bare `nix flake update` moves the lock but leaves the config behind: deprecated
options keep evaluating with warnings until they break, renamed options fail at the
worst time, and new upstream features go unnoticed. This workflow bumps the lock,
then actually reads what changed upstream and adapts the config to it.

The expensive middle step (changelog reading) is fanned out to parallel read-only
agents, one per changed input and one per installed package that a
package-carrying input moved, so a full update stays fast. The main agent
applies all edits itself -- research agents never touch the repo, which avoids
conflicting edits and keeps the final judgment in one place.

## Step 1: Preflight

Confirm `flake.nix` and `flake.lock` are clean in git (`git status --porcelain
flake.nix flake.lock`). If they are dirty, stop and ask -- otherwise the lock diff
mixes the user's pending changes with the bump, and reverting becomes destructive.
Other dirty files are fine. A clean lock also means the committed `flake.lock` at
HEAD serves as the "before" for diffing -- no snapshot needed.

## Step 2: Bump

- All inputs: `nix flake update`
- Specific inputs: `nix flake update <name> [<name>...]`
- Inputs whose URL in `flake.nix` carries a `narHash=` pin need
  `task update:pinned -- <name>` instead (it rewrites the pin in `flake.nix` too).

If `git diff --quiet flake.lock` shows nothing changed, report that everything is
already current and stop.

## Step 3: Diff the lock

Get structured per-input changes:

```bash
task nix:lock-diff
```

It diffs HEAD's `flake.lock` against the working tree (run
`task nix:lock-diff -- <rev-or-file> [new-file]` to compare other points) and prints
a JSON array of changed root inputs with repo, old/new rev, dates, and a compare
URL. Transitive nodes (`flake-parts_2`, `systems_3`, ...) are excluded on purpose -
they are not actionable here.

A rev range describes what moved in that input's own tree. For an input that
exists to package other software, the range hides the bumps that matter, and
Step 4 expands them through `task nix:pkg-diff`.

## Step 4: Classify inputs

Two tiers, because changelog reading only pays off where this repo consumes an API:

**Research tier** -- fan out an agent per input (Step 5). Everything not listed
below, e.g. home-manager, nix-darwin, stylix, sops-nix, nix-homebrew, krewfile,
treefmt-nix, flake-parts, dagger, workmux, nixos-lima, nix-index-database.
These expose modules, options, and functions this repo configures directly, so
deprecations and new features there are actionable.

**Package tier -- diff versions, don't read logs**: `nixpkgs`, `llm-agents`,
the Homebrew taps (`homebrew-core`, `homebrew-cask`, `homebrew-bundle`,
`homebrew-fuse-t`), and the NURs (`rycee-nur`, `nur-jacobcolvin`). These inputs
exist to carry other software. Their commit logs are version bumps and
packaging fixes, never the changelog of anything this repo configures, whether
that log is thousands of nixpkgs commits or a dozen `claude-code: 2.1.0 ->
2.2.0` commits in llm-agents.nix. Reading such a log and reporting "package
bumps, nothing actionable" is the failure this skill exists to prevent. The
bump to Claude Code is the finding, and its changelog lives in Claude Code's
own repo. The surface that matters is what these hosts actually install, and
that is knowable. Narrow to it:

```bash
task nix:pkg-diff
```

It evaluates the flake's `inventory` output at HEAD and in the working tree and
reports version changes for exactly the packages the host configs install
(whichever input provides them - nixpkgs, NURs, custom `pkgs/`), plus version
changes for the declared Homebrew casks and brews between the bumped tap revs.
It runs two full inventory evals, so it takes a couple of minutes: start it in
the background first, then spawn Step 5's agents while it runs.

Triage its output package by package:

- Any bump of a tool whose configuration lives in this repo is research tier,
  whatever its size and whichever input carries it. Configuration means a file
  under `configs/`, options beyond `enable` under `programs.<name>`, or settings
  a `home/` module generates. Add a wave 2 agent (Step 5) per such tool to read
  its own release notes for config-breaking changes and new config options.
  Claude Code is the canonical case: `home/claude.nix` generates its settings,
  hooks, and MCP config, and its minor and patch releases rename keys and add
  features, so every `claude-code` bump gets an agent reading Claude Code's
  release notes. The same rule covers fish, neovim, kitty, git, and every other
  tool with config here.
- Minor/patch bumps of leaf tools with no config here (a bare package entry or
  a lone `enable = true`) need no research; fold them into the final report as
  a one-line version summary.
- Remaining nixpkgs breakage (evaluation failures, removed attributes) still
  surfaces in Step 7, and only then is it worth researching the specific
  failure (search NixOS/nixpkgs PRs and issues, or use the nixos MCP tools).

## Step 5: Fan out research agents

Research runs in two waves, and the upgrade is not researched until both have
run:

- **Wave 1**, from the Step 3 JSON: one agent per research-tier input.
- **Wave 2**, from `task nix:pkg-diff`: one agent per package promoted in
  Step 4. Spawn it as soon as pkg-diff finishes, even while wave 1 reports
  are still arriving. Skipping this wave is the most common way this skill
  fails; the final report then covers what llm-agents.nix or nixpkgs did to
  their packaging and says nothing about what Claude Code or neovim changed.

Within a wave, spawn all general-purpose agents in a single message so they run
concurrently. They inherit the session model by default -- changelog triage
needs real judgment; downgrade only trivial single-package inputs.

Prompt template (fill in from the Step 3 JSON):

```
Research what changed in the flake input "<input>" of the dotfiles repo at
<repo-root>, and what this repo should do about it. You are read-only: do NOT
edit any files. Your final message is a report consumed by another agent.

The input was bumped from <old_rev> (<old_date>) to <new_rev> (<new_date>).
Upstream is <repo> (compare view: <compare_url>).

1. Clone upstream with mcp__git__git_clone into /tmp/git/<owner>/<repo>, then run:
   git -C /tmp/git/<owner>/<repo> log --oneline --no-merges <old_rev>..<new_rev>
   If most commits in the range bump versions of other packages, this input is
   a package carrier. Do not summarize those bumps as its changelog. List
   every bumped package this repo installs under CARRIED, with old and new
   version and upstream repo, and cover only the input's own API (modules,
   overlays, functions) in the other sections.
2. Read the detail behind interesting commits: CHANGELOG entries, release notes
   (mcp__github__list_releases), docs changes in the range. For home-manager,
   also check news entries added in the range (modules/misc/news* and
   docs/release-notes) -- that is where deprecations are announced.
3. Find what this dotfiles repo actually uses from this input: grep home/,
   hosts/, lib/, configs/, flake.nix for the input's module options, functions,
   and packages. Judge every upstream change against real usage -- a breaking
   change in a module this repo does not import is not a finding.

Report exactly four sections:
- REQUIRED: deprecations, renames, removals, and behavior changes that affect
  this repo's actual usage. For each: the upstream change, the affected
  file:line here, and the concrete fix.
- OPPORTUNITIES: new options/features/packages in the range that fit how this
  repo already uses the input, each with a suggested snippet. Only genuinely
  relevant ones -- an empty section beats a padded one.
- NOTED: one line each for upstream changes you checked and ruled out, so the
  main agent knows what was covered.
- CARRIED: one line per package this repo installs whose version this input
  moved, with old and new version and upstream repo. Write "none" when the
  input carries nothing.
If the range is pure maintenance (CI, docs, its own dependency bumps) and
CARRIED is empty, say so in one line and stop.
```

For wave 2, adapt the template: name the tool, give its own upstream repo and
the old -> new version range from pkg-diff instead of flake input revs, and
point the agent at that tool's config in this repo (`home/`, `configs/`) as the
usage to judge against. Never hand a wave 2 agent the carrying input's repo. A
commit titled `claude-code: 2.1.0 -> 2.2.0` in llm-agents.nix is a pointer to
a changelog, not a changelog; the agent reads anthropics/claude-code's release
notes for that range.

While agents run, do not busy-wait -- end the turn and act on completion
notifications as they arrive.

## Step 6: Apply changes

Read all reports, then edit as the main agent:

- A CARRIED section that names a package no agent has researched yet means
  Step 5 is not finished. Spawn the missing wave 2 agents and wait for their
  reports before editing anything.
- Apply every REQUIRED fix. Verify each claim before editing -- grep the cited
  file:line yourself; research agents can hallucinate option names.
- For OPPORTUNITIES, use judgment: adopt clear wins that match how this repo
  already configures things (explicit imports, spelled-out lists -- see repo
  CLAUDE.md). Anything speculative or taste-dependent goes in the final summary
  as a suggestion instead of an edit.
- Keep unrelated refactors out; this change should read as "upgrade inputs and
  adapt".

## Step 7: Verify

```bash
nix flake check --no-build --all-systems
```

This evaluates every host configuration (darwin, NixOS, and home-manager checks)
without building, which is exactly where removed or renamed options blow up.
Fix evaluation errors and rerun until clean. Watch stderr for deprecation
warnings too -- fixing them now is the point of this skill.

If any `.nix` files were edited, run `task format`. The full `task check`
(Dagger e2e) is slow; leave it to the user unless they asked for it.

## Step 8: Report

Summarize per input: what changed upstream, what was migrated (with file
references), which opportunities were adopted, and which were deliberately left
as suggestions. Each wave 2 package gets its own entry under the input that
carried it, with the same detail as an input. Note inputs and packages that
were pure maintenance in one line each. Offer
to commit (the repo's commit skill applies); do not commit unasked.
