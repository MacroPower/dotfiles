---
name: upgrade
description: >-
  Upgrade dependencies in any project.
---

# Upgrade

**Scope:** $ARGUMENTS

A bare update command moves the lockfile and leaves the code behind. Deprecated
APIs keep working with warnings until they break, renamed options fail at the
worst time, and new upstream features go unnoticed. This workflow bumps the
pins, then reads what changed upstream and adapts the project to it.

The expensive middle step, reading changelogs, fans out to parallel read-only
agents, one per dependency worth researching, so a full update stays fast. The
main agent applies every edit itself. Research agents never touch the project,
which avoids conflicting edits and keeps the final judgment in one place.

## Step 1: Survey

Read the scope line above. Empty means every dependency the project declares. A
dependency name means just that one. An ecosystem name narrows to that manifest.
The words `latest` or `major` mean raise version constraints rather than update
within them (see Step 3).

Find every manifest and lockfile in the project and note which tool owns each.
You know the commands for the tools you find; the workflow here is the same for
all of them. Before running any generic command, check for project-specific
tooling: Taskfile, Makefile, or justfile targets named `update`, `upgrade`, or
`bump`; a project skill; and upgrade notes in `CONTRIBUTING.md` or the README.
Prefer those. They encode pins, exclusions, and post-update steps that a generic
command skips. Dependabot or Renovate config is worth a line in the report,
since the bot will otherwise re-open the same bumps.

## Step 2: Preflight

Confirm the manifests and lockfiles are clean in git:

```bash
git status --porcelain -- <manifests and lockfiles>
```

If they are dirty, stop and ask. Otherwise the diff mixes the user's pending
edits with the bump and reverting becomes destructive. Other dirty files are
fine. A clean lockfile also means the committed version at HEAD is the "before"
for diffing, so you need no snapshot.

## Step 3: Bump

Two policies, chosen by dependency and scope:

- **Within constraints** is the default wherever the manifest declares a version
  range. Run the tool's in-range update. List the dependencies whose latest
  release falls outside their range in the final report so the user can decide
  on majors separately.
- **To latest** is the default for unconstrained pins (a flake input, a CI
  action ref, a hook rev, an image tag, a toolchain version), and for any
  dependency when the scope says `latest` or `major` or names a single
  dependency. Raising a constraint means editing the manifest as well as the
  lock.

Keep the project's pin style. A digest- or SHA-pinned reference stays pinned
that way, with the new value and an updated version comment. If nothing changed
afterwards, report that everything is current and stop.

## Step 4: Diff

Produce an old-to-new table of every changed dependency, with direct
dependencies separated from transitive ones. Use whatever gets there fastest:
the tool's own update output, a diff of the lockfile against HEAD, or a short
script that parses both versions. For pinned git refs, include the upstream repo
and a compare URL for the range; the research agents need them.

Some dependencies are carriers. Each release of a carrier mostly moves the
versions of other packages, and those packages are what the project actually
runs, so the carrier's own release notes say almost nothing. A package
collection, a base image, a meta-package, an aggregator flake, a plugin bundle,
an umbrella chart, and a monorepo lockfile are all carriers. For each carrier in
the table, add a row per package the project consumes from it, with that
package's own old and new version and its own upstream repo. Take those
versions from what the project installs, by evaluating or resolving it at HEAD
and in the working tree and comparing, rather than from the carrier's commit
log. A carrier row left collapsed hides the bumps that matter.

## Step 5: Triage

Changelog reading only pays off where this project consumes an API, so sort
every changed dependency into one of two tiers.

**Research tier** (an agent per dependency in Step 6):

- A direct dependency whose bump crosses a major version, or a minor version
  while still at `0.x`.
- A framework, module system, provider, or toolchain the project configures
  directly, at any bump size.
- A tool the project configures, at any bump size, whichever dependency
  carries it. Configuration means a config file, settings the project
  generates, hooks, plugins, or flags in scripts. Such tools rename settings
  keys and add flags in minor and patch releases, and the carrier's release
  notes never mention it.
- A pinned git ref that moved by more than a few commits.

**Version-only tier** (a one-line note in the report):

- Patch and minor bumps of leaf libraries with no configuration in this project.
- Transitive dependencies the project reaches only through another library's
  API, unless verification fails on one.
- The carrier itself. The rules above triage its expanded rows from Step 4
  one by one; the carrier's own log is never the changelog.

"Transitive" describes how a package arrives, not how much it matters. A tool
the project runs or configures is a direct dependency in every sense that
counts, even when a package collection or an aggregator flake supplies it.
Triage it under its own name and version range.

When unsure, research. An agent that reports "pure maintenance" costs little; a
missed rename costs a broken deploy.

## Step 6: Research

Spawn one general-purpose agent per research-tier dependency, all in a single
message so they run concurrently. Changelog triage needs judgment, so they
inherit the session model; downgrade only trivial single-package checks.

Prompt template, filled from the Step 4 table:

```
Research what changed in the dependency "<name>" of the project at
<repo-root>, and what this project should do about it. You are read-only, so
do not edit any files. Your final message is a report consumed by another agent.

The dependency moved from <old version or rev> to <new version or rev>.
Upstream is <repo URL>. <compare URL, if any>

1. Clone upstream with mcp__git__git_clone into /tmp/git/<owner>/<repo>
   (plain `git clone` if that tool is unavailable), then list the range:
   git -C /tmp/git/<owner>/<repo> log --oneline --no-merges <old>..<new>
   Versions map to tags; try both `v1.2.3` and `1.2.3` forms.
   If most commits in the range bump versions of other packages, this
   dependency is a carrier. Do not summarize those bumps as its changelog.
   List every bumped package this project consumes under CARRIED, with old
   and new version and upstream repo, and cover only the carrier's own API
   (its modules, overlays, functions) in the other sections.
2. Read the detail behind interesting commits: CHANGELOG entries, release
   notes (mcp__github__list_releases), migration or upgrade guides, and docs
   changes in the range. Deprecation notices matter most.
3. Find what this project actually uses from the dependency by grepping the
   source and config for its imports, options, CLI flags, and APIs. Judge every
   upstream change against real usage. A breaking change in a module this
   project never imports is not a finding.

Report exactly four sections:
- REQUIRED: deprecations, renames, removals, and behavior changes that affect
  this project's actual usage. For each: the upstream change, the affected
  file:line here, and the concrete fix.
- OPPORTUNITIES: new options, features, or APIs in the range that fit how this
  project already uses the dependency, each with a suggested snippet. Only
  genuinely relevant ones; an empty section beats a padded one.
- NOTED: one line each for upstream changes you checked and ruled out, so the
  main agent knows what you covered.
- CARRIED: one line per package this project consumes whose version this
  dependency moved, with old and new version and upstream repo. Write "none"
  when the dependency carries nothing.

If the range is pure maintenance (CI, docs, its own dependency bumps) and
CARRIED is empty, say so in one line and stop.
```

For a package that arrived through a carrier, fill the template with the
package's own repo and version range, never the carrier's, and point the agent
at the project's configuration for that package as the usage to judge against.
A carrier commit titled `<tool>: 1.4.0 -> 1.5.0` is a pointer to a changelog,
not a changelog. An agent that reads the carrier's log in place of the tool's
release notes has researched the wrong thing.

While agents run, do not busy-wait. End the turn and act on completion
notifications as they arrive.

## Step 7: Edits

Read every report, then edit as the main agent:

- A CARRIED section that names a package no agent has researched yet means
  Step 6 is not finished. Spawn the missing agents and wait for their reports
  before editing anything.
- Apply every REQUIRED fix. Verify each claim before editing by reading the
  cited file:line yourself; research agents can hallucinate option names and
  line numbers.
- For OPPORTUNITIES, use judgment. Adopt clear wins that match how the project
  already does things and its style rules. Anything speculative or
  taste-dependent goes in the final summary as a suggestion instead of an edit.

## Step 8: Verification

Reinstall or re-evaluate from the updated lock, then run the project's own
check: its Taskfile or Makefile `check`, `test`, or `lint` targets, or the
scripts in its manifest. Evaluation and type-check steps catch removed and
renamed APIs; tests catch behavior changes. Fix failures and rerun until clean.

Treat deprecation warnings on stderr as findings; fixing them now is the point
of this skill. Run the project's formatter on edited files. Slow end-to-end
suites stay with the user unless they asked for them.

## Step 9: Report

Summarize per dependency: what changed upstream, what you migrated (with file
references), which opportunities you adopted, and which you left as suggestions.
A package researched through a carrier gets its own entry under that carrier.
Note version-only bumps in one line each, and list the majors that the
within-constraints policy left behind. Offer to commit (the project's commit
skill applies); do not commit unasked.
