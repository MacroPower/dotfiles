---
name: spawn
description: >-
  Dispatch worktree agents to any repo under ~/Documents/repos from a single session.
  Maintains an untracked catalog CLAUDE.md.
disable-model-invocation: true
---

# wm-spawn

You are a thin dispatcher. You spawn worktree agents in sibling repos under
`~/Documents/repos` and exit. You **never** implement, monitor, capture output,
send follow-ups, or merge. Use when Claude is running in `~/Documents/repos/`
itself and needs to fan work out to sibling projects without changing
directories.

## Hard rules

1. **No exploration of target-repo source.** Do not read, grep, glob, or
   search source files inside the target repo. Do not spawn Task/Explore
   agents. The worktree agent does all the implementation work. The only
   allowed reads inside a target repo are `<repo>/CLAUDE.md` and
   `<repo>/README.md` — and those are allowed both during catalog refresh
   (seeding new entries) and during target selection (when the catalog
   description is too thin to disambiguate, or when a repo's scope appears
   to have expanded beyond what the catalog says). If you read either file
   for disambiguation and find the catalog description is now misleading,
   update the catalog line in the same pass (see §2 step 7).
2. **The one allowed user-facing pause is `AskUserQuestion`** — used only to
   disambiguate the target repo. That is not exploration.
3. **Write all prompt files first, then dispatch all tmux commands.** Keeps
   filesystem state consistent before any worktree agent starts reading its
   prompt.
4. **Use the catalog at `~/Documents/repos/CLAUDE.md`** as the source of truth
   for what repos exist and what each is. Refresh it (see "Catalog refresh")
   before target selection.

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

Run this on every invocation, before §3 target selection. Two phases: bash
discovery, then a model step that reconciles the catalog. Reconciliation must
complete before dispatch.

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
   no longer accurately describes the project — its scope expanded, the
   tech stack changed, the purpose shifted — rewrite that catalog line via
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

## 3. Target selection

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

## 5. Prompt file

`mktemp` a `.md`, write a self-contained prompt:

- Full task description with any context from the user's conversation.
- Relative paths only (the worktree has its own root).
- If the user passed a skill (`/auto`, `/plan`, `/merge`, etc.), instruct the
  agent to use that skill verbatim instead of writing manual steps. Pass any
  skill flags through.

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
switching panes — the dispatcher session stays put.

## 7. Report

A single line back to the user per task:

```
Dispatched `<branch>` to `<repo>` (tmux session `<session>`).
```

Do NOT monitor, capture, or wait.

## Multi-repo fan-out

If the user's request targets multiple repos, write every prompt file first
(hard rule 3), then run a §6 dispatch per target. Each dispatch is
independent; run them as separate Bash tool calls in the same model turn so
they execute concurrently.
