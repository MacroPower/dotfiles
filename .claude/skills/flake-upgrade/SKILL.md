---
name: flake-upgrade
description: >-
  Upgrade this repo's Nix flake inputs the right way -- bump the lock, then fan out
  parallel agents to read each input's upstream changelog, apply the fixes the bump
  requires (migrate off deprecated, renamed, and removed options), and file worthwhile
  new features as aven tasks before verifying the flake still evaluates. Use whenever
  the user asks to update, upgrade, or bump flake inputs or flake.lock (all inputs or a
  specific one), asks what changed upstream in an input like home-manager or stylix, or
  asks to bring the dotfiles up to date. Do not just run `nix flake update` on its own
  for these requests -- that is exactly the blind bump this skill exists to avoid.
---

# Flake Upgrade

Running `nix flake update` on its own moves the lock but leaves the config
behind. Deprecated options keep evaluating with warnings until they break,
renamed options fail at the worst time, and new upstream features go unnoticed.
This workflow bumps the lock, then reads what changed upstream. It applies the
fixes that the bump requires, and it files each new feature worth adopting as
an aven task.

The expensive middle step (changelog reading) is fanned out to parallel read-only
agents, one per changed input and one per installed package that a
package-carrying input moved, so a full update stays fast. The main agent
applies all edits itself -- research agents never touch the repo, which avoids
conflicting edits and keeps the final judgment in one place.

## Step 1: Preflight

Confirm `flake.nix` and `flake.lock` are clean in git (`git status --porcelain
flake.nix flake.lock`). If they are dirty, stop and ask -- otherwise the lock diff
mixes the user's pending changes with the bump, and reverting becomes destructive.
Other dirty files are fine. Keeping the lock clean also means the committed
`flake.lock` at HEAD is the "before" for diffing, so you do not need a snapshot.

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

**Research tier:** fan out an agent per input (Step 5). Everything not listed
below, such as home-manager, nix-darwin, stylix, sops-nix, nix-homebrew,
krewfile, treefmt-nix, flake-parts, dagger, workmux, nixos-lima,
nix-index-database. These expose modules, options, and functions this repo
configures directly, so deprecations and new features there are actionable.

**Package tier:** diff versions, and do not read logs. This tier holds
`nixpkgs`, `llm-agents`, the Homebrew taps (`homebrew-core`, `homebrew-cask`,
`homebrew-bundle`, `homebrew-fuse-t`), and the NURs (`rycee-nur`,
`nur-jacobcolvin`). These inputs carry other software. Their commit logs are
version bumps and packaging fixes, never the changelog of anything this repo
configures, whether that log is thousands of nixpkgs commits or a dozen
`claude-code: 2.1.0 -> 2.2.0` commits in llm-agents.nix. Reading such a log and
reporting "package bumps, nothing actionable" is the failure that this skill
prevents. The bump to Claude Code is the finding, and its changelog lives in
Claude Code's own repo.

The surface that matters is what these hosts install, and that is knowable.
Narrow to it:

```bash
task nix:pkg-diff
```

It evaluates the flake's `inventory` output at HEAD and in the working tree and
reports version changes for exactly the packages the host configs install
(whichever input provides them - nixpkgs, NURs, custom `pkgs/`), plus version
changes for the declared Homebrew casks and brews between the bumped tap revs.
It runs two full inventory evals, so it takes a couple of minutes: start it in
the background first, then spawn Step 5's agents while it runs. Start
`task nix:build-plan` in the background at the same time. It takes longer
still, and Step 7 reads its output.

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
  a lone `enable = true`) do not need research; fold them into the final report as
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

1. Clone upstream with mcp__git__git_clone into the directory that your
   instructions name for clones, then run:
   git -C <clone> log --oneline --no-merges <old_rev>..<new_rev>
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
  repo already uses the input. For each: the upstream change, the version or
  commit that introduced it, the file:line here that would adopt it, and a
  suggested snippet. Only genuinely relevant ones -- an empty section beats a
  padded one.
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

## Step 6: Act on the reports

Read all reports before you act on any of them. A CARRIED section that names a
package no agent has researched yet means Step 5 is not finished. Spawn the
missing wave 2 agents and wait for their reports before editing anything.

The upgrade change holds only what the bump requires, so it reads as "upgrade
inputs and adapt". Each REQUIRED item becomes an edit, and each OPPORTUNITIES
item becomes an aven task.

### Required fixes

Edit as the main agent:

- Apply every REQUIRED fix. Verify each claim before editing. Grep the cited
  file:line yourself, because research agents can hallucinate option names.
- Keep unrelated refactors out.

### Opportunities

Do not adopt an opportunity in the upgrade change, however small the edit.
Each adopted opportunity grows the change with work that the bump does not
require. A suggestion that lives only in the final summary is lost when the
session ends. File each OPPORTUNITIES item as an aven task in the `dotfiles`
project instead, so a later session can adopt it on its own.

Load the `aven` skill before you run an `aven` command. Pass
`--project dotfiles` to `aven search` and `aven add`. `aven note` does not
take `--project`. Its ref already names the task. File the tasks without
asking the user first. The `aven` skill creates a task only on an
instruction, and this step is that instruction.

Before you file an item, grep the file:line that its report cites, as for a
REQUIRED fix. Leave out an item whose cited usage does not exist, and name it
in the Step 8 report. When two reports name the same opportunity, file it one
time with the detail of both.

Then search for a task that already covers the item, because a repeated
upgrade reports the same opportunity again. Search on the option or feature
name alone. The search returns only the tasks that hold every term of the
query.

```bash
aven search --project dotfiles "<option or feature name>"
```

The results include done and canceled tasks, and each result prints its
`status`. A task covers the item when it names the same upstream change. The
status of that task decides what you do:

- **Open** (`inbox`, `backlog`, `todo`, or `active`): do not file the item.
  Add what this run found with `aven note <ref> --stdin`. The note gives the
  range of this upgrade and each detail of the report that the task lacks.
- **Done or canceled**: the user adopted or declined the opportunity, so do
  not file the item again.
- **No task covers the item**: file it.

File an item with this command:

```bash
aven add --project dotfiles "Adopt <option or feature> from <input or package>" --description-stdin <<'EOF'
## Upstream change
<what upstream added and why it fits this repo, with a link to the commit,
pull request, or release note>

## Introduced in
<the version or commit that introduced the change, then the range of this
upgrade: the old and the new rev of the input, or the old and the new version
of the package>

## Affected file
<the file:line here that would change>

## Suggested change
<the snippet from the research report>
EOF
```

Fill every section, so the task stands alone after the session ends. Spell
the option or feature name in the title as upstream spells it, so the search
of a later upgrade finds the task. Leave the status and the priority at their
defaults for the user to triage. Keep the ref that `aven add` prints for the
Step 8 report.

## Step 7: Verify

```bash
nix flake check --no-build --all-systems
```

This evaluates every host configuration (darwin, NixOS, and home-manager checks)
without building, which is exactly where removed or renamed options blow up.
Fix evaluation errors and rerun until clean. Watch stderr for deprecation
warnings too -- fixing them now is the point of this skill.

If any `.nix` files were edited, run `task format`. The full `task check`
(Dagger e2e) is slow, so leave it to the user unless they asked for it.

Evaluation passing means that the config is valid, not that the switch is
cheap. Read the build plan that Step 4 started, or run it now:

```bash
task nix:build-plan
```

It evaluates every host, runs `nix build --dry-run` against each system
derivation, and lists the derivations the host compiles itself instead of
fetching from the binary cache. The script counts the derivations that a
cache never carries (text files, wrappers, downloads) and lists the rest.
Each listed cache miss carries a `new` flag from a comparison with the same
evaluation at HEAD. The misses the bump introduced therefore stand apart from
the ones this repo's overlays always cause. The dry run skips paths the local
store already holds. The flag is therefore complete only on a machine that has
built neither revision, such as the Lima VM this skill usually runs in.

A new cache miss on anything larger than a script is a finding, not a build
to wait for. Hydra has not built that derivation for the host's platform, or
its build failed. Either way the switch compiles it for hours and might fail
at the end. Trace it to the installed package with
`nix why-depends --derivation <system-drv> <miss-drv>`. Then choose one of
three fixes:

- Wait for the cache.
- Pin the package to a version the cache holds.
- Drop the dependency, usually by taking a binary runtime in place of a
  source build.

A dotnet runtime appearing here after a swift bump is the canonical case. The
installed tools were photo-cli and marksman, and the fix was in how they chose
their runtime, not in dotnet.

The same output carries `overlay_drift`. The flake's `overlayTargets` output
names the nixpkgs attributes that this repo's overlays modify in place.
Entries appear for those whose plain nixpkgs derivation changed name, src, or
patches between HEAD and the new lock. pkg-diff cannot see
these, because an overlay that re-sources a package also pins the version it
reports. Treat every entry as a finding. Open the overlay in
`lib/default.nix`, read its comment, and pick one outcome:

- nixpkgs now carries the fix: remove the overlay.
- The overlay's patch no longer applies: rebase it.
- The change is unrelated to the overlay: keep it and say so.

The fabric overlay is the canonical case. nixpkgs adopted the invoke 3 patch
that the overlay pulled from upstream, so the overlay's replacement src made
that patch fail to apply. The version diff showed nothing. While there, check
the overlays whose comments state a removal condition ("drop when nixpkgs
ships X") against the new nixpkgs, whether or not they drifted.

## Step 8: Report

Summarize the upgrade for the user:

- **Each input**: what changed upstream and what you migrated, with file
  references.
- **Each wave 2 package**: its own entry under the input that carried it, with
  the same detail as an input.
- **Each opportunity**, under its input or package: the ref and the title of
  the task that you filed. For an opportunity that an existing task covers,
  give the ref of that task and say whether you added a note to it. For an item
  that you left out, say what its report cited and what you found.
- **The build plan**: each new cache miss and each overlay drift entry, with
  the decision taken on it.
- **Pure maintenance**: one line for each input and each package whose range
  held only maintenance.

Offer to commit (the repo's commit skill applies), and do not commit unasked.
Keep task refs out of the commit message and out of the files that you edit,
because they name the user's local tasks.
