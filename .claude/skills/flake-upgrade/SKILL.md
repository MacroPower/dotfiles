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
agents, one per changed input, so a full update stays fast. The main agent applies
all edits itself -- research agents never touch the repo, which avoids conflicting
edits and keeps the final judgment in one place.

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

## Step 4: Classify inputs

Two tiers, because changelog reading only pays off where this repo consumes an API:

**Research tier** -- fan out an agent per input (Step 5). Everything not listed
below, e.g. home-manager, nix-darwin, stylix, sops-nix, nix-homebrew, krewfile,
treefmt-nix, flake-parts, dagger, llm-agents, workmux, nixos-lima,
nix-index-database. These expose modules, options, and packages this repo
configures directly, so deprecations and new features there are actionable.

**Package tier -- diff versions, don't read logs**: `nixpkgs`, the Homebrew taps
(`homebrew-core`, `homebrew-cask`, `homebrew-bundle`, `homebrew-fuse-t`), and the
NURs (`rycee-nur`, `nur-jacobcolvin`). These are package sets with hundreds or
thousands of commits per bump, so their logs are noise - but the surface that
matters is knowable: what these hosts actually install. Narrow to it:

```bash
task nix:pkg-diff
```

It evaluates the flake's `inventory` output at HEAD and in the working tree and
reports version changes for exactly the packages the host configs install
(whichever input provides them - nixpkgs, NURs, custom `pkgs/`), plus version
changes for the declared Homebrew casks and brews between the bumped tap revs.
It runs two full inventory evals, so it takes a couple of minutes: start it in
the background first, then spawn Step 5's agents while it runs.

Triage its output instead of researching every entry:

- Major-version jumps of tools whose configuration lives in this repo (anything
  configured under `home/` or `configs/` - fish, neovim, kitty, git, ...) are
  research-worthy: add an agent to the Step 5 fan-out to read that tool's own
  release notes for config-breaking changes and new config options.
- Minor/patch bumps of leaf tools with no config here need no research; fold
  them into the final report as a one-line version summary.
- Remaining nixpkgs breakage (evaluation failures, removed attributes) still
  surfaces in Step 7, and only then is it worth researching the specific
  failure (search NixOS/nixpkgs PRs and issues, or use the nixos MCP tools).

## Step 5: Fan out research agents

Spawn one general-purpose agent per research-tier input, all in a single message so
they run concurrently. They inherit the session model by default -- changelog
triage needs real judgment; downgrade only trivial single-package inputs.

Prompt template (fill in from the Step 3 JSON):

```
Research what changed in the flake input "<input>" of the dotfiles repo at
<repo-root>, and what this repo should do about it. You are read-only: do NOT
edit any files. Your final message is a report consumed by another agent.

The input was bumped from <old_rev> (<old_date>) to <new_rev> (<new_date>).
Upstream is <repo> (compare view: <compare_url>).

1. Clone upstream with mcp__git__git_clone into /tmp/git/<owner>/<repo>, then run:
   git -C /tmp/git/<owner>/<repo> log --oneline --no-merges <old_rev>..<new_rev>
2. Read the detail behind interesting commits: CHANGELOG entries, release notes
   (mcp__github__list_releases), docs changes in the range. For home-manager,
   also check news entries added in the range (modules/misc/news* and
   docs/release-notes) -- that is where deprecations are announced.
3. Find what this dotfiles repo actually uses from this input: grep home/,
   hosts/, lib/, configs/, flake.nix for the input's module options, functions,
   and packages. Judge every upstream change against real usage -- a breaking
   change in a module this repo does not import is not a finding.

Report exactly three sections:
- REQUIRED: deprecations, renames, removals, and behavior changes that affect
  this repo's actual usage. For each: the upstream change, the affected
  file:line here, and the concrete fix.
- OPPORTUNITIES: new options/features/packages in the range that fit how this
  repo already uses the input, each with a suggested snippet. Only genuinely
  relevant ones -- an empty section beats a padded one.
- NOTED: one line each for upstream changes you checked and ruled out, so the
  main agent knows what was covered.
If the range is pure maintenance (version bumps, CI, docs), say so in one line
and stop.
```

For package-tier follow-ups out of `task nix:pkg-diff`, adapt the template: give
the agent the tool's upstream repo and the old -> new version range instead of
flake input revs, and point it at that tool's config in this repo (`home/`,
`configs/`) as the usage to judge against.

While agents run, do not busy-wait -- end the turn and act on completion
notifications as they arrive.

## Step 6: Apply changes

Read all reports, then edit as the main agent:

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
as suggestions. Note inputs that were pure maintenance in one line each. Offer
to commit (the repo's commit skill applies); do not commit unasked.
