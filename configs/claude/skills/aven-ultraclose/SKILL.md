---
name: aven-ultraclose
description: >-
  Close every open aven task in the current project with parallel worktree
  agents and adversarial review.
argument-hint: "[concurrency] [aven list filters]"
disable-model-invocation: true
---

# Aven Ultraclose

**Arguments:** $ARGUMENTS

Close every open task in the project's aven queue. Each task gets its own
worktree, an implementer, and a loop of adversarial review and fixes. Each
finished task lands as one commit on the base branch. The base branch is
the branch that the main checkout has checked out when the run starts.

You are the orchestrator. You schedule tasks, land them, and keep the
records. Workflow agents write and review the code. Runs last for days, so keep
diffs and logs out of your context, and read one only when a landing fails.

A number in the arguments sets how many tasks run at the same time, and the
default is 5. Any other argument is an `aven list` filter that narrows the
queue, such as `--label bug`.

## Tools

`${CLAUDE_SKILL_DIR}/scripts` holds both tools. Run `ultraclose.py` from
inside the repository.

| Tool | Purpose |
| --- | --- |
| `ultraclose.py` | Keeps the run state, picks tasks, creates worktrees, and lands tasks. `--help` lists its commands. |
| `close-task.js` | A Workflow script that takes one task from implementation through review. |

The run directory holds everything that must outlive your context:
`state.json`, `rules.md`, `notes.md`, `logs/`, and `tasks/<ref>/` with the
task file, the commit message (`msg.txt`), and the review history. The
repository's git config records the path of the run directory, so every
command finds it.

The run directory also holds a copy of `close-task.js`. The `init`, `next`,
and `args` commands of `ultraclose.py` each refresh that copy from the skill
directory. Pass only the copy to Workflow. Workflow refuses a script file
that has more than one hard link. The skill directory holds symlinks into
the Nix store. Store optimisation can add a hard link to a store file in
the middle of a run.

## Scope

The queue is every open task that is not an epic, including each task that
anyone files during the run. Triage the new tasks each time `next` returns
nothing.

Every epic stays open while one of its children is open. Set an epic to
`done` when its last child lands.

The run ends when `ultraclose.py status` counts no pending, inflight, or
approved task.

## Setup

1. Run `ultraclose.py status`. If it reports a run, continue that run and
   skip the rest of the setup.
2. Confirm that the main checkout is clean and on the base branch.
3. Find how the project builds, tests, and checks itself, and what its
   commit hooks run. Read CLAUDE.md, the task runner config, and
   `git log -15`.
4. Run `ultraclose.py init --run <scratchpad>/ultraclose --limit <N>`. Pass
   `--check '<command>'` with the full check of the project, unless the
   commit hook already runs that check. Pass `--shared <glob>` for each kind
   of file that two running tasks may both edit. Each `--shared` glob adds to
   the default globs, which cover test files.
5. Write `rules.md` in the run directory. Every agent reads it, so state
   each rule as an instruction:
   - The commands that build, test, and run the full check.
   - The commit message format, taken from the log and the hook config.
   - Whether breaking changes are free, and which callers a change must
     update.
   - The skills to load before an agent writes prose.
   - Commands that agents must not run. A `dagger call` from a linked
     worktree has emptied the index of the main checkout, so forbid Dagger
     in worktrees unless the project requires it.

   Do not tell agents where to report a fault outside their task, even in
   a rule that forbids the commands that file tasks. The Workflow prompts
   ask for each such fault in the `defects` of the result, as the Defects
   section says. Agents put `rules.md` above their defaults, so a rule that
   sends the fault to `notes` hides it in prose again.
6. Triage the queue. For each open task, predict the files that it edits.
   Fan the reading out to Explore agents, and write the result as a JSON
   list:

   ```json
   [{"ref": "APP-7KQ9", "title": "...", "priority": "high",
     "files": ["pkg/a.go"], "after": ["APP-7KQ0"], "last": false}]
   ```

   `after` names the tasks that must land first, which covers each aven
   dependency. Set `last` on a task that changes the commit hooks, the
   check, or the pins between the repository's own modules. Every `last`
   task runs alone after the other tasks, so that the other landings keep
   the hooks and pins they started with. Load the list with
   `ultraclose.py triage <file>`. The same command adds tasks later.

## Loop

Repeat until the run ends:

1. Run `ultraclose.py next`. It starts one task per free slot. No two
   running tasks share a file outside the shared globs. It prints one input
   object per started task.
2. Launch one Workflow per object:
   `Workflow({scriptPath: "<run>/close-task.js", args: <object>})`. The
   `run` field of the object holds the path of the run directory. One task
   per Workflow lets each task land the moment it finishes. Leave `model`
   unset so that agents follow the session model.
3. Keep `ultraclose.py watch` running in a background Bash call. It exits
   when free disk space runs low.
4. End the turn. Each Workflow completion wakes you with a result. Act on
   its `status`, and in the same turn file its `defects` as the Defects
   section says:

| Status | Action |
| --- | --- |
| `approved` | Land the task. |
| `landable` | The task reached its cap as a strict improvement with no regression. File each entry of `followups` with `aven add` and link it with `aven related add`. Then land the task. |
| `obsolete` | Add the evidence with `aven note`, set the task to `done`, and run `ultraclose.py set <ref> closed`. |
| `abandoned`, `rejected`, `unconverged` | Add a note with the open findings and the branch name, set the task to `todo`, and run `ultraclose.py set <ref> noted`. The branch stays. Do not force the task through. |
| `error` | Relaunch the task one time with the output of `ultraclose.py args <ref>`. After a second error, note the task. |

`ultraclose.py status` lists each pending task that waits for a noted task.
Note those tasks too.

When `boundedReview` is true on a result, the last review covered only the
final fixes. Record the task in `notes.md`.

On an `approved` or `landable` result, `summary` and `notes` describe the
change that lands. The implementer writes them, and each fixer revises
them, so a fix that overturns an earlier statement also corrects it. On a
`landable` result, the agent that narrows the claims of the change revises
them last.

A relaunched task that resumes at a review round starts without the
`summary` and `notes` of its earlier launch. Until a fixer runs, its
`summary` stays empty, and its `notes` hold only what a sync agent
reported. When the `summary` of such a result is empty, read `msg.txt` for
what the change does.

## Defects

File each defect that a result reports as a task of its own. A defect is a
fault that an agent found outside its task and left unfixed, such as a
wrong fact in a doc or a bug in nearby code. Every result lists its
defects in `defects`, whatever its status. Each entry holds a `title` and
a `description` in the format of the `track-issue` skill.

When a result arrives, file its defects in the same turn. Act on its status
first, so that its landing or its relaunch runs in the background while you
file:

1. Load the `track-issue` skill, and file each entry as the skill says.
   Its duplicate search finds a defect that an earlier result reported.
2. Link each new task to the task that reported the defect, with
   `aven related add <new-ref> <ref>`.
3. Record each new task in `notes.md`.

Do not wait for the user to ask for these tasks, and do not reproduce a
defect before you file it. Each new task joins the queue as the Scope
section says, and its implementer confirms the fault before it changes
anything.

The implementer starts the list of a result. Each fixer adds the defects
that it finds, and withdraws each entry that the change fixes or that is
not a fault. On a `landable` result, the agent that narrows the claims of
the change adds its defects last. A relaunched task that resumes at a
review round starts with an empty list, or with the list of its sync
agent. Its result does not repeat the defects of the earlier launch. A
launch that returns no result loses its defects.

The `notes` of a result can report a defect that `defects` does not list.
File that defect in the same way, and write its description from the
notes.

## Caps

The Workflow caps each task at 6 review rounds, and `init --rounds` changes
the cap. At the cap it runs one final cycle. In that cycle, a fixer applies
only the exact fixes of the open findings, and a reviewer checks only those
fixes. If findings remain, the reviewer decides between `landable` and
`unconverged`.

Agents must not learn the cap, because an agent that knows it rushes or
softens its work near it. The Workflow prompts never state the cap or say
that a cycle is the last. Keep the cap out of everything that agents read:
`rules.md`, the `--sync` reason, and each note on a task. The task file
carries the notes into a later attempt, so describe the open findings
instead of the round count.

The cap counts rounds, not time. `ultraclose.py status` prints how long each
inflight task has run. To cut a task short, stop its Workflow and relaunch
it with `ultraclose.py args <ref> --final`, which sends the task to its
final cycle.

Fixers answer each finding with a fix or a rebuttal, and `history.md`
records both. Reviewers read it, so a rebutted finding does not return each
round.

## Landing

Run `ultraclose.py land <ref>` in a background Bash call. Landings queue on
a lock, so start one as soon as a task is ready. The command merges the base
branch into the worktree and runs the check. Then it squashes the branch
into one commit on the base branch with `msg.txt` as the message, removes
the worktree, and sets the task to `done`. It prints one `RESULT` line:

| Result | Action |
| --- | --- |
| `OK` | Run `next`. |
| `CONFLICT`, `CHECKFAIL`, `COMMITFAIL` | The base branch moved under the task. Relaunch with `ultraclose.py args <ref> --sync '<reason and failing output>'`. A sync agent merges and adapts, and a reviewer checks the merge. Land again after approval. Note the task when a second sync fails for the same reason. |
| `WTDIRTY` | An agent left files behind. Delete probe files and built binaries, commit real changes with `--no-verify`, and land again. |
| `MSGHASREF`, `REFINDIFF` | Remove the task ref from the message or the code, and land again. |
| `DIRTY`, `NOTBASE`, `TREEMISMATCH`, `POSTDIRTY` | An agent or a hook changed the main checkout. Stop landing, find the cause, and repair the checkout before the next landing. |

When you fix something by hand before a landing, record the fix in
`notes.md`, because no reviewer saw it.

## Pushes

Ask the user before the first push. After that, push only when a task
requires a pushed commit, such as a pin to a commit of this repository. If
the remote rejects a push, for example because the token lacks the scope for
workflow files, record the rejection for the report and continue.

## Hazards

- **Messages to agents:** never send a message to a Workflow agent by id.
  The message resumes a second copy of the agent. Relaunch the task with
  `ultraclose.py args <ref>` instead.
- **Stash:** never run `git stash`. All worktrees share one stash list.
- **Disk:** when `watch` prints `DISK`, clean the build caches only while no
  landing and no check runs, then start `watch` again. A cache clean races
  running builds, so land again any task that failed during the clean.
- **Interrupted run:** after a usage limit, a model switch, or a new
  session, run `ultraclose.py status` and relaunch each inflight task with
  `ultraclose.py args <ref>`. The agents find the unfinished work in the
  worktree.

## Notes

Append to `notes.md` in the run directory when something happens that the
final report requires, because compaction drops whatever exists only in your
context. Record:

- Each hand fix.
- Each landing without a full final review.
- Each task that you file for a follow-up or a defect, with the task that
  reported it.
- Each artifact that a task made stale, such as a recorded demo or a
  generated file.
- Each behavior change that the user should know about.

## Report

End with a report that lists:

- The landed tasks, each with its commit.
- The noted tasks, each with its reason and its branch.
- The tasks that the run filed for follow-ups and for defects, each with
  the task that reported it.
- The tasks that landed without a full final review.
- The stale artifacts.
- The commits that the run did not push, and each rejected push.
