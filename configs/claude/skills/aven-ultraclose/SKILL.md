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
`state.json`, `rules.md`, `notes.md`, `logs/`, and `tasks/<ref>/`. The
directory of a task holds its task file, its commit message (`msg.txt`),
and its review history. It also holds the summary and the notes of its
change (`summary.txt` and `notes.txt`). The repository's git config records
the path of the run directory, so every command finds it.

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

`ultraclose.py untriaged --project <key>` prints each task of the queue
that the run does not hold. `<key>` names the aven project of the
repository. Always pass `--project`, because `aven list` covers every
project without it. Add each `aven list` filter that the arguments hold.
The command prints a JSON list with the ref, the title, and the priority
of each task. The list is empty when the run holds the whole queue.

Every epic stays open while one of its children is open. Set an epic to
`done` when its last child lands.

The run ends when `ultraclose.py status` counts no pending, inflight, or
approved task. `status` then prints an `ended` line.

## Setup

1. Run `ultraclose.py status`, and act on what it reports:
   - **No run:** do the rest of the setup.
   - **A run with an `ended` line:** do the rest of the setup, which
     starts a new run. The arguments ask for a new batch of tasks. Do not
     copy the limit, the check, the health command, the shared globs, or the
     `rules.md` of the ended run. They fit the tasks that it closed.
   - **A run without an `ended` line:** continue that run, and skip the
     rest of the setup. First fit the run to the arguments and to the
     open tasks that are not in it, as the Config section says.
2. Confirm that the main checkout is clean and on the base branch.
3. Find how the project builds, tests, and checks itself, and what its
   commit hooks run. Read CLAUDE.md, the task runner config, and
   `git log -15`.
4. Run `ultraclose.py init --run <scratchpad>/ultraclose --limit <N>`. If
   that directory holds an earlier run, pass a new directory, such as
   `<scratchpad>/ultraclose-2`. `init` refuses the directory of an earlier
   run, so the new run starts without the `rules.md` and the `notes.md` of
   that run. Pass `--check '<command>'` with the full check of the project,
   unless the commit hook already runs that check. Pass
   `--health '<command>'` when the check or a commit hook requires a
   service, such as Docker. The health command is a fast command that fails
   when the service is down, such as `docker version`.

   Pass `--shared <glob>` for each kind of file that two running tasks may
   both edit. Each `--shared` glob adds to the default globs, which cover
   test files. The scheduler matches each glob against `/` plus the path
   of a file relative to the repository, and `*` also matches `/`. So
   `/README.md` matches the `README.md` at the root of the repository,
   `*/README.md` matches the `README.md` of every directory, and
   `README.md` matches no file.
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
6. Triage the queue. List it with `ultraclose.py untriaged`, as the Scope
   section says. For each task, predict the files that it edits. Fan the
   reading out to Explore agents, and write the result as a JSON list:

   ```json
   [{"ref": "APP-7KQ9", "title": "...", "priority": "high",
     "files": ["pkg/a.go"], "after": ["APP-7KQ0"], "last": false}]
   ```

   Load the list with `ultraclose.py triage <file>`. The same command adds
   tasks later.

   `after` names the tasks that must land first. `triage` reads the
   dependencies of each task from aven, and adds each open dependency to
   the `after` of the task. So write into `after` only the order that aven
   does not record, such as a task that builds on the change of another
   task. `triage` reads the dependencies each time it loads a task. After
   the aven dependencies of a pending task change, load the task again.

   Set `last` on a task that changes the commit hooks, the check, or the
   pins between the repository's own modules. Every `last` task runs alone
   after the other tasks, so that the other landings keep the hooks and
   pins they started with.

   `triage` prints a warning for each `--shared` glob that matches no file
   of any task. Such a glob shares no file, so two tasks that edit one
   file still wait for each other. Replace a misspelled glob with
   `ultraclose.py config --unshared <old> --shared <new>`. Keep a glob
   that covers a file of a task that you have not triaged yet.

## Config

`init` writes the config of a run, and the run keeps that config until you
change it. `ultraclose.py status` prints the limit, the shared globs, the
check, and the health command. `ultraclose.py config` changes the config
and prints the result. It takes the `--limit`, `--rounds`, `--check`,
`--health`, `--shared`, and `--disk-floor-gb` flags of `init`, and its
`--unshared <glob>` flag removes a shared glob. `config` prints the warning
of `triage` for each glob that its `--shared` flag adds and that matches no
file of any task. Every change applies to the commands that start after it,
so a running Workflow and a queued landing keep the values that they
started with.

A run that you continue carries the config and the `rules.md` of the tasks
that it started with. When you continue the run, fit both to the arguments
and to the open tasks that are not in the run. `ultraclose.py untriaged`
prints those tasks, as the Scope section says. Fit the run in these ways:

- If the arguments hold a number, set the limit to it.
- Remove each shared glob that covers files that two of those tasks must
  not edit at the same time, such as the source files of one module.
  `next` starts such tasks together, and a landing returns `CONFLICT` when
  both tasks changed the same lines.
- Change the check when it skips a part of the repository that those tasks
  change. When the new check requires a service that the health command
  does not test, change the health command as well.
- Rewrite each rule in `rules.md` that the landed tasks made false, such as
  the layout of a module that they split. Remove each exception that the
  run made for one of them.

## Loop

Repeat until the run ends:

1. Run `ultraclose.py next`. It starts one task per free slot. No two
   running tasks share a file outside the shared globs. It prints one input
   object per started task. After a user message about other work, the
   User messages section says in which turn to run it.
2. Launch one Workflow per object:
   `Workflow({scriptPath: "<run>/close-task.js", args: <object>})`. The
   `run` field of the object holds the path of the run directory. One task
   per Workflow lets each task land the moment it finishes. Leave `model`
   unset so that agents follow the session model.
3. Keep `ultraclose.py watch` running in a background Bash call. It exits
   when free disk space runs low.
4. End the turn. Each Workflow completion wakes you with a result. File its
   `defects` as the Defects section says, in the same turn as you act on its
   status. When the result gives a user message about other work as the
   reason for its status, relaunch the task as the User messages section
   says. Otherwise act on its `status` as this table says:

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
them, so a fix that overturns an earlier statement also corrects it. A
sync agent revises them after its merge. On a `landable` result, the agent
that narrows the claims of the change revises them last.

Each agent that writes or revises the `summary` and `notes` also stores
them in the task directory, as `summary.txt` and `notes.txt`. The agent
writes both files before it commits its work, the way it writes
`msg.txt`. So an agent that stops after its commit leaves text that
describes that commit. `ultraclose.py args` passes the stored text to the relaunch. So the result
of a relaunched task carries the `summary` and `notes` of its earlier
launch until an agent of the relaunch revises them. The relaunch after an
interrupted run carries the stored text too, although the interrupted
launch returned no result.

A launch at the implementer starts with no stored text. `next` and `args`
remove both files when they start a task there, so a relaunch never
carries the text of an attempt that the new implementer replaced. An
implementer that stops before it stores the two files leaves no text for
the relaunch. The `summary` of the relaunched task then stays empty
until an agent revises it. When the `summary` of a result is empty, read
`msg.txt` for what the change does.

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

The Workflow caps each task at 6 review rounds, and the `--rounds` flag of
`init` or `config` changes the cap. At the cap it runs one final cycle. In
that cycle, a fixer applies only the exact fixes of the open findings, and
a reviewer checks only those fixes. If findings remain, the reviewer
decides between `landable` and `unconverged`.

The task is `landable` only when the reviewer reports that the change
leaves the base branch strictly better and that no open finding is a
regression. Otherwise the task is `unconverged`. The reviewer decides
whether each open finding is a regression by this rule:

| Open finding | Regression |
| --- | --- |
| A fault in behavior that the change introduced, such as a wrong result or a broken build | Yes |
| A wrong instruction that the change introduced in text that an agent acts on, such as a prompt, a skill, or a CLAUDE.md file | Yes |
| A fault that the base branch also has | No |
| A missing or weak test, even in code that the change added | No |
| A doc, comment, or convention fault that does not change an instruction, even in code that the change added | No |

Only a fault in behavior is a regression, because an `unconverged` task
stays open together with each task that waits for it. Every `landable`
task lands, and a follow-up task tracks each of its open findings.

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
the worktree, and sets the task to `done`. Each landing of a run with a
health command runs that command before the merge, and again after a failed
check or a failed commit.

When a landing fails, the command prints the last lines of the failing
output. The run directory holds the full output of each step in
`logs/<ref>.<step>.log`. The last line that the command prints is the
`RESULT` line:

| Result | Action |
| --- | --- |
| `OK` | Run `next`. |
| `CONFLICT` | The base branch moved under the task. Sync the task. |
| `CHECKFAIL`, `COMMITFAIL` | The check or a commit hook failed. Find the cause in the failing output, and act on that cause. |
| `HEALTHFAIL` | The service that the check or a commit hook requires is down. Repair the service and land again. Do not sync the task. When the same task ends in `HEALTHFAIL` after the check or the commit a second time, read the log that the output names above the `RESULT` line, and do not land the task again. |
| `WTDIRTY` | An agent left files behind. Delete probe files and built binaries, commit real changes with `--no-verify`, and land again. |
| `MSGHASREF`, `REFINDIFF` | Remove the task ref from the message or the code, and land again. |
| `DIRTY`, `NOTBASE`, `TREEMISMATCH`, `POSTDIRTY` | An agent or a hook changed the main checkout. Stop landing, find the cause, and repair the checkout before the next landing. |

To sync a task, relaunch it with
`ultraclose.py args <ref> --sync '<reason and failing output>'`. A sync
agent merges and adapts, and a reviewer checks the merge. Land again after
approval. Note the task when a second sync fails for the same reason.

`CHECKFAIL` and `COMMITFAIL` do not show that the task must change, and each
sync costs an agent run and a review. Find the cause in the failing output
before you sync a task. Each cause has its own action:

| Cause | Action |
| --- | --- |
| A down service, a network failure, a full disk, or a lack of memory stopped the check or the hook, such as a test that the kernel killed (`Killed`, `signal: killed`, exit status 137). | Repair the environment and land again. Do not sync the task. When the same task fails a second time for the same cause, read the log of the failed step, and do not land the task again. |
| A failed test covers code that the task did not change. | Land again one time. Sync the task when the same test fails again. |
| The check or the hook reports a fault in the change, such as a broken build, a linter finding, or a failed test of the changed code. | Sync the task. |

A `HEALTHFAIL` after the check or the commit shows that the environment
failed during that step, but not why. The failing output of `CHECKFAIL` or
`COMMITFAIL` shows the same when it names a down service, a network failure,
a lack of memory, or a full disk. The change might cause the failure, such as
with a test that fills the memory or the disk. Then each landing of the task causes the
failure again. After the same task fails a second time for the same cause,
sync the task with the log of the failed step when that log shows that the
change causes the failure. Otherwise, note the task.

When you fix something by hand before a landing, record the fix in
`notes.md`, because no reviewer saw it.

## Pushes

Ask the user before the first push. After that, push only when a task
requires a pushed commit, such as a pin to a commit of this repository. If
the remote rejects a push, for example because the token lacks the scope for
workflow files, record the rejection for the report and continue.

## User messages

Handle each message that the user sends during the run yourself, and keep
a message about other work away from the agents.

Workflow relays the latest message that the user typed in a turn to every
agent of a Workflow that you launch in that turn. The message can start
the turn or arrive in the middle of it. Workflow presents the message as
the user request behind the run. So an agent that reads a message about
other work might abandon its task or act on the message. The Workflow
prompts tell every agent that you handle those messages. A Workflow that
you launch in a turn without a user message relays nothing.

When the user sends a message about other work, do not launch a
`close-task.js` Workflow in the turn that receives it. Run
`ultraclose.py next` and each relaunch in the turn that the next
notification starts. Launch them in the same turn only when no Workflow
and no landing is running, because no notification comes then. Launch a
Workflow that the user asks for in the turn of the request, because the
relayed message is the request of that Workflow.

When a result gives a user message about other work as the reason for its
status, the status says nothing about the task. Relaunch the task with
`ultraclose.py args <ref>` in place of the action that the Loop table gives
for the status. When the message was about the task, act on the status as
the Loop table says.

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
