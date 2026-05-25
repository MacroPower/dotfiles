---
name: spawn
description: >-
  Dispatch worktree agents to any repo under ~/Documents/repos from a single
  session. Two modes: targeted dispatch and fire-and-forget fan-out across
  many repos. Maintains an untracked catalog CLAUDE.md.
disable-model-invocation: true
---

# wm-spawn

You are a thin dispatcher. You spawn worktree agents in sibling repos under
`~/Documents/repos` and exit. You **never** implement, monitor, capture output,
send follow-ups, or merge. Use when Claude is running in `~/Documents/repos/`
itself and needs to fan work out to sibling projects without changing
directories.

## Modes

Pick one per invocation. The key question is **who picks the targets**:

- **Mode 1: targeted dispatch** (default). The user named the target(s),
  or the catalog disambiguates trivially. One non-trivial task, one or a
  few named repos, including the case of multiple distinct tasks each
  going to a different named repo. Path: §§1-2, §3, §§4-7.
- **Mode 2: fan-out** (fire-and-forget). Same conceptual change across
  >= 3 repos and discovery is part of the work: "in all repos", "every
  repo with X", "everywhere", "bulk", "all my Nix projects". Each
  spawned worktree agent is scoped strictly to its own repo. Path:
  §§1-2, §4, §M, §6, §7.

## Hard rules

1. **The one allowed user-facing pause is `AskUserQuestion`**: used only
   to disambiguate target repos. That is not exploration.
2. **Write all prompt files first, then dispatch all tmux commands.** Keeps
   filesystem state consistent before any worktree agent starts reading its
   prompt. Issue the prompt-file `Write` calls as parallel tool calls in a
   single turn, then issue the §6 `Bash` dispatches the same way.
3. **Use the catalog at `~/Documents/repos/CLAUDE.md`** as the source of
   truth for what repos exist. Refresh it (see §2) before any target
   selection.
4. **Target-repo source is read-only and the rules differ by mode.**
   - Mode 1: do not read, grep, glob, or search source files inside a
     target repo, and do not spawn Task/Explore agents. The only allowed
     reads are `<repo>/CLAUDE.md` and `<repo>/README.md`, for catalog
     seeding (§2), disambiguation (§3), and scope-drift updates (§2 step
     7). The worktree agent does the implementation.
   - Mode 2: `Explore` agents are allowed for discovery and for grabbing
     example snippets to embed in per-target prompts. They remain
     read-only. The only files this skill `Edit`s/`Write`s directly are
     the catalog and the per-target prompt files; everything else routes
     through the spawned worktree agents.

## 1. Preflight

Resolve both paths through symlinks before comparing:

```bash
REPOS_ROOT="$HOME/Documents/repos"
if [ "$(pwd -P)" != "$(cd "$REPOS_ROOT" && pwd -P)" ]; then
  echo "wm-spawn must be invoked from $REPOS_ROOT. cd there and rerun." >&2
  exit 1
fi
```

## 2. Catalog refresh (lazy drift)

Run this on every invocation, before §3 or §M target selection. Two phases:
bash discovery, then a model step that reconciles the catalog.
Reconciliation must complete before dispatch.

### Discovery

```bash
for d in "$REPOS_ROOT"/*/; do
  [ -e "$d/.git" ] && basename "$d"
done
```

### Reconciliation (model step)

1. `Read` `$REPOS_ROOT/CLAUDE.md` if it exists. Apply the regex
   `^- \*\*([^*]+)\*\*: (.+)$` against file contents the model has read.
2. Set-diff discovery vs catalog:
   - In discovery but not catalog -> **missing** (need description).
   - In catalog but not discovery -> **stale** (drop).
   - In both -> **keep verbatim** (preserve user edits).
3. If both sets are empty, do nothing.
4. For each **missing** repo, read `<repo>/CLAUDE.md` first, falling back to
   `<repo>/README.md`. Write one sentence: description text ≤ 100 chars (not
   counting `- **name**: `), no trailing period, no line wraps. Insert the
   new line in alphabetical order via `Edit`.
5. For each **stale** entry, delete the line via `Edit`.
6. If the catalog does not exist, create it with the header (see "Format")
   plus one entry per discovered repo, sorted alphabetically by name.
7. **Scope-drift updates.** If during target selection (§3) you read a
   repo's `CLAUDE.md` or `README.md` and discover the cataloged description
   no longer accurately describes the project (scope expanded, tech stack
   changed, purpose shifted), rewrite that catalog line via
   `Edit` in the same pass. Same format rules apply (single line, colon
   separator, ≤ 100 chars description text, no trailing period).

### Format

```markdown
# Repos under ~/Documents/repos

<!-- Maintained by /wm-spawn. Per-repo descriptions are safe to edit; keep each
     entry on a single line so the parser recognizes it on the next refresh. -->

- **dotfiles**: Declarative Nix system config (nix-darwin + home-manager) for macOS and NixOS hosts
- **foo-bar**: ...
```

Descriptions stay on one line, however long. Verification of manual-edit
preservation depends on it.

## 3. Target selection (mode 1)

For each task in the user's request:

1. **Substring match (case-insensitive)** of repo names against the task text.
2. **Keyword match** against each repo's description line if no substring hit.
3. **Deeper read** (allowed): if steps 1-2 produce two or more close
   candidates and the catalog descriptions are too thin to disambiguate, read
   `<repo>/CLAUDE.md` (preferred) or `<repo>/README.md` for each candidate to
   make the call. If the deeper read reveals the catalog description is
   misleading or outdated, update the catalog line per §2 step 7 before
   proceeding.
4. If exactly one candidate: use it.
5. If still multiple, zero, or the user named a repo that is not in the
   catalog: call `AskUserQuestion` with the top 2-4 candidates.
6. Never guess silently when scores are close.

## 4. Branch handle

The model picks a 2-4 word kebab-case name from the task description.
Constrained to `[a-z0-9-]+` (no spaces, quotes, dollar signs, or other shell
metacharacters), cap at 40 chars. The dispatch snippet in §6 relies on this
character set for safe shell interpolation.

In **mode 2**, pick the handle **once** and reuse it across every target
so the resulting branches/PRs are trivially groupable.

## 5. Prompt file (mode 1)

`mktemp` a `.md`, write a self-contained prompt:

- Full task description with any context from the user's conversation.
- Relative paths only (the worktree has its own root).
- If the user passed a skill (`/auto`, `/plan`, `/merge`, etc.), instruct the
  agent to use that skill verbatim instead of writing manual steps. Pass any
  skill flags through.

## §M. Mode 2: fan-out

### M.1 Discovery

Phrase the eligibility criterion from the user's request as one short
question ("does this repo have a Taskfile.yaml?", "does this repo's
flake.nix pin nixpkgs-unstable?", "is there a `.github/workflows/ci.yml`
older than the template?").

Pick the cheapest tool that answers it:

- **Mechanical predicate** (file existence, simple grep over a known
  path): one `Bash` pass over `$REPOS_ROOT/*/`. Don't spawn agents for
  what `test -f` and `grep -l` can decide.
- **Semantic predicate** (intent, structure, idiom): one `Explore` agent
  per batch of 5-10 repos, run all batches concurrently in one turn.
  Each agent returns yes/no plus the proof path, ≤ 200 words total.
  Only pass the catalog snippet if the criterion actually needs the
  repo's purpose to resolve.

Optional: pick a **reference repo** (one that already has the desired
state) and have one Explore agent extract the canonical snippet (file
path + small excerpt) so per-target prompts can point at a real example.
The reference repo is read-only; it does not get a dispatch.

If discovery returns zero eligible repos, report that and exit. If it
returns 1-2, switch to mode 1 (fan-out machinery is overkill).

### M.2 Per-target prompt template

`mktemp` one `.md` per eligible repo. Keep each prompt short, plain, and
unassuming. The agent doesn't need a pep talk. Use absolute `~`-rooted
paths for any cross-repo reference.

```
Apply this change to this repo: <one-line summary>.

What to change:
<2-4 sentences max. Name the file(s), describe the shape of the change.
No rationale, no history.>

Reference (read-only):
~/Documents/repos/<reference-repo>/<path/to/example>

The same change is being applied in parallel to:
- <other-repo-1>
- <other-repo-2>
- <other-repo-3>
- ... (+ K others)

Scope: edit only files inside your own worktree. Every path under
~/Documents/repos/ that is not your own repo is read-only: reading for
reference is fine, writing or running modifying commands there is not.
```

Cap the parallel-targets list at 5 names plus a `(+K others)` line so the
prompt stays small when fan-out is wide.

If the user passed a skill, append exactly one line:

```
Use the <skill> skill verbatim; pass <flags> through.
```

Do **not** include the catalog, do **not** include the discovery
reasoning, do **not** include other repos' file contents. Reference
paths plus repo names is enough. The agent can read them on demand.

### M.3 Dispatch

Follow hard rule 2: all prompt-file `Write` calls in one turn, then all
§6 `Bash` dispatches in one turn.

## 6. Dispatch via tmux

Resolve the target session by **project path**, not by name. This handles the
case where the user already has a session open on that path.

```bash
PROJECT_PATH="$REPOS_ROOT/$TARGET_REPO"

SESSION=$(tmux list-sessions -F '#{session_name} #{session_path}' 2>/dev/null \
          | awk -v p="$PROJECT_PATH" '$2 == p { print $1; exit }')

if [ -z "$SESSION" ]; then
  SESSION="$TARGET_REPO"
  i=2
  while tmux has-session -t "$SESSION" 2>/dev/null; do
    SESSION="${TARGET_REPO}-${i}"
    i=$((i+1))
  done
  tmux new-session -d -s "$SESSION" -c "$PROJECT_PATH"
fi

tmux new-window -t "$SESSION" -c "$PROJECT_PATH" \
  "workmux add $(printf %q "$BRANCH") -b -P $(printf %q "$PROMPT_FILE"); exit"
```

`printf %q` keeps the spawned shell parsing safe. `-b` keeps workmux from
switching panes: the dispatcher session stays put.

## 7. Report

A single line back to the user per task:

```
Dispatched `<branch>` to `<repo>` (tmux session `<session>`).
```

In mode 2, emit one line per dispatched repo, then a one-line summary
("Fan-out: N dispatches on branch `<handle>`."). Do NOT monitor, capture,
or wait.
