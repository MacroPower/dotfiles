---
name: create-skill
description: >-
  Generate a new Claude Code skill from a repo or docsite. Discovers an index,
  dispatches parallel crawler and reviewer agents, writes a SKILL.md with a
  references folder.
disable-model-invocation: true
---

# Create Skill

**Source:** $ARGUMENTS
**Date:** !`date '+%Y-%m-%d %H:%M'`

You are an orchestrator. You turn a single repo or docsite URL into a fully-formed
Claude Code skill: a `SKILL.md` plus a populated `references/` folder. You dispatch
crawler and reviewer subagents in parallel and synthesize their output. You do NOT
crawl or summarize sources yourself.

Establish absolute paths up front. All later subagent prompts must use absolute
paths because subagents have their own cwd that resets per Bash call.

Resolve the dotfiles root from git rather than hardcoding it. This works from
any subdirectory of the working tree and from worktrees.

```bash
DOTFILES="$(git rev-parse --show-toplevel)"
# Sanity-check we're in the dotfiles repo (worktree or main checkout).
test -f "$DOTFILES/home/claude.nix" || {
  echo "create-skill must be invoked from inside the dotfiles repo (or a worktree of it)." >&2
  exit 1
}
# NEW_NAME and SKILL_DIR are populated after Step 4.
```

## Step 1: Classify the source

Inspect `$ARGUMENTS` (the Source field above) and pick one bucket:

- Matches `github.com/<owner>/<repo>` or ends with `.git` -> **repo**.
- Bare local path that resolves to a git checkout (`.git/` exists) -> **repo**
  (already cloned; skip the clone step).
- Anything else with `http(s)://` -> **docsite**.

If the source matches none of the above, abort and ask the user for a clearer URL.

## Step 2: Acquire the source

- **Repo:** call `mcp__git__git_clone` with `dest=/tmp/git/<owner>/<repo>`.
  Set `REPO_ROOT=/tmp/git/<owner>/<repo>`.
- **Docsite:** keep the base URL exactly as given (strip a trailing `/`). Set
  `BASE_URL=<that URL>`.

## Step 3: Discover the index

Try methods in order, stopping at the first that yields > 0 sections in Step 5.

For docsites:
1. `mcp__fetch__fetch` `<BASE_URL>/llms-full.txt`.
2. `<BASE_URL>/llms.txt`.
3. `<BASE_URL>/sitemap.xml`, then `<BASE_URL>/sitemap_index.xml`.
4. `<BASE_URL>/robots.txt`. Parse for any `Sitemap:` directive and fetch that.
5. Fallback: fetch `<BASE_URL>/` and extract the top-level navigation
   (`<nav>` element, header link list, or sidebar).

For repos:
1. `llms.txt` or `llms-full.txt` at `$REPO_ROOT/`.
2. `$REPO_ROOT/docs/` directory.
3. `$REPO_ROOT/mkdocs.yml`, `$REPO_ROOT/_sidebar.md`, `$REPO_ROOT/SUMMARY.md`
   (mdBook / GitBook).
4. `$REPO_ROOT/README.md` H2 sections.

Record the discovery method (e.g. `llms-full.txt`, `sitemap.xml`,
`docs/ traversal`). It feeds the "snapshot of <source>" line in the generated
SKILL.md (see the template in Step 8).

**If every method fails:** abort with a clear message. "No index found at
<source>. This skill cannot crawl JS-rendered SPAs or sites without a
sitemap / llms.txt / structured nav. Provide a different source or supply an
explicit sitemap URL."

## Step 4: Auto-derive name + description, then confirm

Derive defaults before asking the user.

**Name:**
- Repo: slugify the `<repo>` segment. Call `slugify` bare on PATH.
- Docsite: slugify the page `<title>` or `og:site_name` from the homepage fetch.

**Description:** start from the first paragraph of the README, the first
`<meta name="description">`, or the first H1+intro from the discovered index.
Then append a "Trigger when..." clause built from the discovered section titles
(file extensions, CLI names, schema keys, common command verbs the source uses).
The generated skill is **model-invocable** (no `disable-model-invocation` in its
frontmatter), so trigger keywords matter and a single sentence leaves the
trigger thin. Aim for 2-4 sentences: a one-line summary, a "Trigger when..."
clause naming concrete file patterns and CLI verbs, and an optional "SKIP when..."
clause for likely false positives. Example shape:

> Reference and guidance for writing X (config files, the `x` CLI). Trigger when
> the user edits `x.yml` / `.xrc.yml`, asks about schema keys (a, b, c), runs or
> troubleshoots the `x` CLI, or asks about templating syntax. SKIP when the user
> is discussing unrelated tool Y or generic concept Z.

Then call `AskUserQuestion` with two questions, in order:

1. "Skill name?" with options:
   - `Use derived value (Recommended)` -> the auto value
   - `Edit` -> user types a replacement in the auto-provided "Other" field
2. "Skill description?" with the same two-option shape.

Loop the question until both fields are confirmed. Then:

```bash
NEW_NAME="<confirmed name>"
SKILL_DIR="$DOTFILES/configs/claude/skills/$NEW_NAME"
mkdir -p "$SKILL_DIR/references"
```

## Step 5: Partition into sections

Build a list of `(section_title, source_refs[])` tuples from the discovered index.

| Index type | Partition rule |
| --- | --- |
| `llms.txt` / `llms-full.txt` | Split on H2 headings. |
| sitemap | Group URLs by their first path segment. Drop low-value segments (`/blog`, `/changelog`, `/community`, `/news`, `/legal`). If a remaining segment has > 30 URLs, subdivide by second path segment. |
| `mkdocs.yml` | Parse the `nav:` key with `yq` if available (`yq '.nav' mkdocs.yml`). If `yq` is unavailable, skip mkdocs.yml and fall through to `docs/`. Each top-level `nav:` entry is one section. If a top-level entry has > 8 children, treat each child as its own section. |
| `_sidebar.md` / `SUMMARY.md` | Parse the markdown list structure. Each top-level entry is one section, with the same > 8 children subdivision rule. |
| repo `docs/` | Each top-level file or subfolder is one section. Same > 8 children subdivision rule. |
| README H2 fallback | Each H2 is one section. |

**Source format per partition.** For docsite partitions, `source_refs` are
absolute URLs. For repo partitions, `source_refs` are absolute file paths under
`$REPO_ROOT/`. Crawler agents read both transparently (Read tool for files,
`mcp__fetch__fetch` for URLs).

**Edge cases:**
- **Zero sections** (e.g. README with no H2s, llms.txt with only a single H1):
  fall through to the next discovery method from Step 3. If the next method also
  yields zero, abort and ask the user to point at a more structured source.
- **One section:** dispatch a single crawler. Skip the parallelism scaffolding.

**Per-section URL cap.** If a section has > 30 `source_refs`, the crawler agent
is told to fetch the first 30 and note truncation in its Sources list. This
prevents one crawler running for an hour on an `/api/*` segment with hundreds
of entries.

## Step 6: Spawn crawler agents in parallel

One `Agent` call per section, all in **a single message**. Multiple Agent calls
in one message run concurrently by design. The number of launched subagents is
**unlimited**, spawn as many agents as are necessary to complete your task.

Each prompt is fully self-contained and uses absolute paths. Substitute the
section title, source list, and slug:

```
You are a focused documentation extractor for one section of a larger source.

SECTION: <title>
SOURCES:
  - <absolute URL or absolute file path>
  - ...
OUTPUT FILE: <SKILL_DIR>/references/<section-slug>.md   (absolute path)
REVIEW SIDECAR: <SKILL_DIR>/references/<section-slug>.review.md  (absolute path; not used by you, mentioned for awareness)

RULES:
1. Fetch each source URL with mcp__fetch__fetch (docsites) or read each file
   with the Read tool (repos).
2. Cap at 30 sources. If your list has more, take the first 30 and note the
   truncation in your Sources list.
3. After EACH fetch/read, immediately Edit your output file. Do not hold
   findings in memory across multiple tool calls.
4. If a fetch returns 404 or a file is missing, skip it, note it in Sources
   as "[unreachable]", and continue. Do not fail the whole agent.
5. Every claim must include an inline citation: a URL for docsites, or a
   file:line reference for repos.
6. Write a condensed reference, not a verbatim copy. Preserve code blocks,
   schemas, CLI flag tables, and concrete examples. Drop marketing prose,
   navigation chrome, and changelogs.
7. End with a "Sources" list of every URL/file you cited.

PROCESS:
1. For each source, fetch/read, then Edit your findings into the output file.
2. Final pass: deduplicate H2 headings within this file and reorder them so
   prerequisite material comes first.
```

## Step 7: Spawn reviewer agents in parallel

After all crawlers finish, launch one reviewer per references file in **a single
message**. Reviewers write their log to a **sidecar file**, never inline in the
reference. This makes leakage into the final SKILL.md impossible.

```
You are a reference-doc reviewer.

FILE TO REVIEW: <SKILL_DIR>/references/<section-slug>.md   (absolute path)
SIDECAR FILE:   <SKILL_DIR>/references/<section-slug>.review.md  (absolute path)

RULES:
- You may Edit the file under review to fix factually wrong claims, broken
  citations, or hallucinated code. Replace wrong claims with correct ones drawn
  from the cited source. If you cannot find a correct version in the cited
  source, mark the claim with a markdown blockquote one-liner "> unverified"
  rather than removing it. The orchestrator decides what to do with unverified
  claims.
- You may NOT add new topics or sections that weren't already in the file.
  "Fix missing context" means clarifying an existing claim, not introducing a
  new one.
- Write your review log (what you changed, what you flagged, what you couldn't
  verify) to the SIDECAR file. The reference file itself must contain only
  reference content plus inline "> unverified" hedges.

PROCESS:
1. Read the reference file. List every claim and its citation.
2. Re-fetch each cited URL / re-read each cited file.
3. Edit the reference file in place to fix or hedge. Do not append a "Review
   notes" section to it.
4. Write your full review log to the sidecar file.
```

## Step 8: Synthesize the final SKILL.md

Read every `references/*.md` in full (token budget is unlimited), then write
`<SKILL_DIR>/SKILL.md` using this template:

```markdown
---
name: <NEW_NAME>
description: <confirmed description from Step 4>
---

# <Title>

<1-2 sentence intro with a link to the source.> This skill is a snapshot of
<source URL or repo> as of <date>.

## Quick Reference

| Topic | Reference |
| --- | --- |
| <section title> | [<slug>.md](references/<slug>.md) |
| ... | ... |

Open the matching reference file for full detail. Summaries below are for quick
lookup.

## <Section 1>

<Summary written from a full read of the reference, condensed to bullets or
short prose. Preserve the most-reached-for information: CLI flags, schema keys,
setup steps, code shapes.>

## <Section 2>

...
```

Critical: synthesis reads `references/*.md` only, NEVER `references/*.review.md`.
The sidecars exist solely for your Step 10 report to the user.

No artificial line cap. Density target: each section summary is dense enough
that the user rarely needs to open the linked reference for routine questions,
but short enough that the SKILL.md fits comfortably in a single read. Preserve
canonical schemas, full CLI flag tables, and short runnable code shapes inline;
push longer code blocks, exhaustive enumerations, and rarely-touched detail to
the references file.

## Step 9: Register the skill in home-manager

Edit `$DOTFILES/home/claude.nix`. Add a new line to the `skills` attrset in
alphabetical position:

```nix
<NEW_NAME> = ../configs/claude/skills/<NEW_NAME>;
```

This is the step that makes `task switch` actually surface the new skill. Without
it, the file exists on disk but is never symlinked into `~/.claude/skills`.

## Step 10: Verify and report

Verify the skill's own output before reporting success.

1. `<SKILL_DIR>/SKILL.md` exists, has matching `^---$` frontmatter boundaries,
   and the YAML between them parses. Use `yq`. The parsed YAML must contain
   `name:` and `description:` keys.
2. Every `references/<slug>.md` referenced from the SKILL.md's Quick Reference
   table exists on disk. This catches slug drift between Step 5 and Step 8.
3. `$DOTFILES/home/claude.nix` now contains a line for `<NEW_NAME>`.

Then report to the user:

1. Path to the new skill folder.
2. Section count and total reference word count.
3. Any sidecar files containing `> unverified` hedges or non-trivial review
   logs -- list them so the user can spot-check.
4. Reminder to run `task switch` (per `CLAUDE.md`) to activate the new skill.
