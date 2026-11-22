---
name: aven-ultraclose
description: >-
  Close every open aven task in the current project with parallel worktree
  agents and adversarial review.
argument-hint: "[concurrency] [filters] [models] [target]"
disable-model-invocation: true
---

# Aven Ultraclose

**Arguments:** $ARGUMENTS

Close every open task in the project's aven queue. Each task gets its own
worktree, an implementer, and a loop of adversarial review and fixes. Each
finished task lands on the target of the run. By default, each task lands
as one commit on `main`. The arguments can name another branch, or ask
for one pull request per task, as the Target section says.

You are the orchestrator. You schedule tasks, land them, and keep the
records. Workflow agents write and review the code. Runs last for days, so keep
diffs and logs out of your context, and read one only when a landing fails.

A number in the arguments sets how many tasks run at the same time, and the
default is 5. When the memory of the machine cannot hold that many tasks,
lower the limit, as the Setup section says. The arguments can also name a
model for the agents of a role, such as `opus-5-5 for the reviewers`, and
the Models section says how to set it. They can name where the tasks land,
such as `on release/v1` or `as pull requests`, and the Target section says
how to set it. They can narrow the queue by status, by priority, and by
label, such as `only bugs` or `medium priority and up`, and the Filters
section says how to set the filters. Any other argument is an `aven list`
filter that narrows the queue, such as `--has-metadata review`.

## Tools

`${CLAUDE_SKILL_DIR}/scripts` holds both tools. Run `ultraclose.py` from
inside the repository.

| Tool | Purpose |
| --- | --- |
| `ultraclose.py` | Keeps the run state, picks tasks, creates worktrees, and lands tasks. `--help` lists its commands. |
| `close-task.js` | A Workflow script that takes one task from implementation through review. |

Both tools print what you do next, so act on their output:

- Each landing prints an `ACTION` line before its `RESULT` line.
- Each Workflow result carries an `action` field.
- Each command that refuses its task names the command that removes the
  cause.

This document names each command and the output that you act on. It adds
the judgment that a script cannot make, such as the cause of a failed
test.

The run directory holds everything that must outlive your context:
`state.json`, `rules.md`, `notes.md`, `logs/`, and `tasks/<ref>/`. The
directory of a task holds its task file, its commit message (`msg.txt`),
and its review history. It also holds the summary and the notes of its
change (`summary.txt` and `notes.txt`). The repository's git config records
the path of the run directory, so every command finds it.

`state.json` also records where the run comes from, and `ultraclose.py
status` prints each record:

- **The skill version:** a hash of the `SKILL.md`, the `close-task.js`,
  and the `ultraclose.py` that created the run. The `skill` line of
  `status` says when the command comes from another version of the skill.
- **The session:** the session of the `init` command, and then of each
  later command that changes the run. The `session` line of `status` says
  when the command runs in another session.
- **The earlier runs:** the run whose unfinished tasks `init` moved to
  this run, and each earlier run that the state of that run records. Each
  `earlier run` line of `status` names one of them.

The directory of a task also holds its own copy of `close-task.js`. The
`next` and `args` commands of `ultraclose.py` write the copy of each task
that they start, and so does a landing that runs `next`. In the `meta` of
the copy, the name holds the ref of the task, and the description holds its
title. So the Workflow list and `/workflows` show which task each Workflow
closes.

Each `next` and `args` command prints one input object per task that it
starts, and the `script` field of the object holds the path of the copy.
Launch every Workflow from that path, for a first launch and for a
relaunch. The `args` command writes the copy again when `close-task.js` in
the skill directory or the title of the task has changed. So a relaunch
runs the script that the skill directory holds at the time of the relaunch.

Never pass the `close-task.js` of the skill directory to Workflow. Workflow
refuses a script file that has more than one hard link. The skill directory
holds symlinks into the Nix store. Store optimisation can add a hard link
to a store file in the middle of a run.

## Scope

The queue is every open task that is not an epic and that passes the
filters of the run, as the Filters section says. The queue includes each
such task that anyone files during the run. Triage the new tasks each time
`next` prints an empty list, whether you run `next` or a landing runs it.
After a triage that adds tasks, run `ultraclose.py next`, so that the new
tasks take the free slots.

`ultraclose.py untriaged --project <key>` prints each task of the queue
that the run does not hold. It applies the filters of the run itself. Add
each other `aven list` filter that the arguments hold, such as
`--has-metadata review`. The run config does not keep those other filters,
so pass them to each `untriaged` command. The command prints a JSON list
with the ref, the title, and the priority of each task. The list is empty
when the run holds the whole queue.

`<key>` is the key of the aven project of the repository. Always pass
`--project`. Without it, `aven list` keeps to the project that aven infers
from the main checkout, and it covers every project when aven infers none.

`--project` takes the key of a project, not the prefix of its refs. For
example, the refs of the project `terrarium` start with `TRR`, and aven
rejects `--project TRR` with the error `unknown-project`. The `Project:`
line of the session-start aven context gives the key of the project that
aven infers from the directory where the session started.
`aven project list` prints one line per project, with the key first and
the prefix of the refs after it:

```text
terrarium prefix=TRR name="terrarium"
```

When the arguments name the project by its prefix, such as `TRR`, pass
the key from the line that holds that prefix.

Every epic stays open while one of its children is open. Set an epic to
`done` when its last child lands.

The run ends when `ultraclose.py status` counts no pending, inflight,
approved, or proposed task. `status` then prints an `ended` line. Its
`setup` line then names `init` for the setup of a later batch, so do not
start a new run at the end of a run.

## Filters

The filters of a run narrow its queue by status, by priority, and by
label. `init` sets them, and the run config keeps them, so every later
command and every later session of the run applies them.
`ultraclose.py status` prints them in its `filters` line as the flags that
set them, or as `none`.

Each filter flag takes one value. Repeat a flag for each value, except
`--min-priority`, which keeps its last value. This table lists each filter
flag with the tasks that the queue holds under it:

| Flag | The queue holds |
| --- | --- |
| `--status <status>` | Only the tasks with the status of one of these flags |
| `--skip-status <status>` | No task with this status |
| `--priority <priority>` | Only the tasks with the priority of one of these flags |
| `--min-priority <priority>` | Only the tasks of this priority or higher |
| `--skip-priority <priority>` | No task with this priority |
| `--label <label>` | Only the tasks that carry the label of one of these flags |
| `--skip-label <label>` | No task that carries this label |

Each task joins the queue only when it passes every filter. The statuses
are `inbox`, `backlog`, `todo`, and `active`, because the queue holds only
open tasks. From low to high, the priorities are `none`, `low`, `medium`,
`high`, and `urgent`. `init` and `config` refuse a label that aven does
not know. They also refuse the filters of a dimension that let no task
in, such as `--status todo --skip-status todo`.

Pass one flag for each value that the arguments name, as in these
examples:

| Arguments | Flags |
| --- | --- |
| `only todo and inbox` | `--status todo --status inbox` |
| `not the backlog` | `--skip-status backlog` |
| `medium priority and up` | `--min-priority medium` |
| `the bug and docs tasks` | `--label bug --label docs` |
| `everything except self-improve` | `--skip-label self-improve` |

`aven list` takes one value of each of these filters, and it has no filter
that leaves a value out. So `untriaged` applies the filters of the run
itself, and it refuses a `--status`, `--priority`, or `--label` filter.

To change the filters, pass the filter flags to `ultraclose.py config`. The
flags of one dimension replace every filter of that dimension, and each
other dimension keeps its filters. `--any-status`, `--any-priority`, and
`--any-label` remove the filters of their dimension. For example, after
`init --label bug --min-priority medium`, the command
`config --skip-label self-improve` leaves the filters
`--min-priority medium --skip-label self-improve`.

The filters decide which tasks join the run. They apply to each task that
the run files too, such as a defect that a result reports. Such a task
joins the run only when it passes the filters. Otherwise it stays open in
aven for a later run. `untriaged` leaves it out, and `triage` refuses a
list that holds it, with the reason. The run keeps each task that it
holds, such as a task that joined before a change of the filters.

`triage` also refuses a task when one of its open aven blockers sits
outside the run and the filters keep that blocker from the run. `next`
waits only for the tasks that the run holds, so such a task starts before
its blocker lands. The filters keep a blocker from the run when they leave
out the blocker or a task on the chain of its own open aven blockers.
`triage` refuses such a blocker too, so it never joins the run. The chain
ends at each task that the run holds.

The refusal names the chain in a `waits for` reason, as in this line:

```text
APP-0001: it waits for APP-0002, which waits for APP-0003, which the queue filters leave out
```

The refusal covers each task that joins the run and each pending task that
the run holds. It also covers each task of the list that waits for a
refused task that joins the run. Act on each task that the refusal names:

- For a task that joins the run, leave its row out of each triage and
  record the task in `notes.md`. `untriaged` still lists the task.
- For a pending task that the run holds, leave its row out of the triage
  and set the task to noted with
  `ultraclose.py set <ref> noted --note '<the reason that triage printed>'`.
  The refusal leaves the task pending, so `next` starts it otherwise.

To let the blocker in instead, widen the filters with `config`, and load
the list again.

When `init` replaces an earlier run that stopped before its end, the
filters of the new run apply to each pending task that the earlier run has
not started. `init` leaves such a task in that run when the filters leave
out the task. It also leaves the task when one of its open aven blockers
sits outside that run and the filters keep that blocker from the new run,
as `triage` judges such a blocker. The task stays open in aven for a later
run.

`init` moves each other pending task. It moves each started task and each
noted task whatever the filters, because each of them holds work or a
result of the batch. It also moves a left-out pending task when a moved
task waits for it, so that the task lands first. Every moved task waits
for the tasks of its `after` list. `init` adds to that list each open aven
blocker that it reads for a pending task, as `triage` does. The Setup
section says what `init` prints.

## Target

The target of a run names where its tasks land. `init` sets the target
with `--target <target>`, and the run keeps it. `ultraclose.py status`
prints the target in its `target` line. This table lists each target with
its base branch and with the end of its landings:

| Target | Base branch | End of each landing |
| --- | --- | --- |
| A branch, such as `main` or `release/v1` | That branch | One commit on the base branch, which holds the whole task |
| `pr` | `main` | A branch that you push and propose as a draft pull request against the base branch |
| `pr:<branch>` | `<branch>` | The same as for `pr` |

Each task branch starts from the base branch, whatever the target. Each
landing merges the base branch into the worktree before it runs the
check.

Choose the target from the arguments and from the default branch of the
repository:

- **The arguments name a branch:** pass `--target <branch>`.
- **The arguments ask for pull requests:** pass `--target pr`. Pass
  `--target pr:<branch>` when the arguments name the branch that the pull
  requests go to, or when the default branch of the repository has
  another name than `main`.
- **The arguments name no target:** the tasks land on the default branch
  of the repository. Pass no `--target` flag when that branch is `main`,
  and pass `--target <branch>` otherwise, such as `--target master`.

Every landing on a branch target commits in the main checkout, where the
commit hooks run. So the main checkout must have that branch checked out,
and it must hold no uncommitted change. `init` fails otherwise, and step 2
of the Setup section says what to do then.

Every landing with a `pr` target leaves the main checkout alone, so the
main checkout can have any branch checked out. Such a landing records no
commit on the base branch, so it runs no commit hook. When the base
branch moved after the task started, the landing merges it into the task
branch with a merge commit. The push then publishes that commit. The
landing ends with the result `PR`, and its task then waits as `proposed`
until the pull request merges. The Pull requests section gives your
steps after that result, and it says which tasks wait for a proposed
task.

## Other repositories

A run with a `pr` target can also change other repositories. Each task
that needs a change in another repository gets a worktree and a branch
there too. Its agents implement and review the change in every worktree
as one change, and its landing proposes one pull request in each
repository that the task changes.

Runs with a branch target take no other repository, because no landing
commits a task on two base branches at once. A pin to a commit of
another repository, such as a flake input, also needs the commit that the
merge of a pull request makes there. So leave the row of each task that
needs another repository out of each triage of such a run, and record
the task with that reason in `notes.md`. Tell the user that a run with a
`pr` target can close the task.

The aven description of a task names each other repository that the
task changes, such as by its GitHub name `MacroPower/x`. Add each such
repository to the run with `--repo <name>=<base>` on `init` or `config`:

- `<name>` is the directory of the main checkout of the repository in the
  repos directory of the host, which the `DOTFILES_REPOS_DIR` variable
  names, such as `x` for `MacroPower/x`.
- `<base>` is the branch that each task of the repository starts from,
  and that its pull requests go to. Pass the default branch of the
  repository.

Pass `--repo-check <name>='<command>'` with the full check of the
repository, which each landing runs in the worktree of the task there.
`config --no-repo <name>` removes a repository that no unfinished or
noted task changes. `ultraclose.py status` prints one `repo` line per
other repository, with its main checkout, its base branch, and its check.
Like `init` for the base branch of a `pr` target, the `--repo` flag
refuses a base branch that holds a commit that its upstream branch lacks.

The `repos` field of a triage row names each other repository that the
task changes, as step 6 of the Setup section says. The worktree of the
task in another repository is `.claude/worktrees/t-<ref>` of its main
checkout, on the branch `task/<ref>`. The message of each other
repository goes in `tasks/<ref>/msg-<name>.txt` of the run directory,
beside the `msg.txt` of the repository of the run. The Workflow input
names each worktree and each message file, so the agents find them.

The landing proposes each repository whose branch holds a change, and
the Pull requests section says how you push and open each pull request.
The pull request of the repository of the run merges last. Its change can
depend on the change of another repository, such as with a pin to a
commit there. The worktree and the branch of the task stay in each
repository in which no landing proposed the task, until the task lands.

A commit of the task branch in another repository reaches a remote only
through the merge of its pull request, because no agent pushes. So no pin
can name that commit before the merge. The agents keep each such pin at
its old value and name it in their notes. After the last other pull
request merges, you sync the task so that each pin names a commit that
holds the merge, as the Pull requests section says.

## Setup

1. Run `ultraclose.py status`. The script decides whether this session
   continues the run that the repository names, and the `setup` line of
   `status` prints the decision:
   - **`setup: continue this run`:** continue that run, and skip the rest
     of the setup. First fit the run to the arguments and to the open
     tasks that are not in it, as the Config section says. When the
     `session` line names another session, also follow the Interrupted
     run entry of the Hazards section. Follow that entry as well when the
     line says `not recorded` and this session has not worked on that run
     before.
   - **`setup: continue this run, which holds no task yet`:** an earlier
     setup of this session stopped early. Do the rest of the setup, and
     in step 4 change the run with `ultraclose.py config` in place of
     `init`. `config` keeps the target of the run. When the target that
     step 2 chooses differs from the `target` line of `status`, run
     `init` with `--force` and `--target` in step 4 in place of `config`.
     The run holds no task, so `--force` loses nothing.
   - **`setup: start a new run with init`:** do the rest of the setup.
     The line gives the reason, such as a run that has ended, or a run
     that comes from another session and another version of the skill.
     A run without a task gets this line as well, unless it records this
     session and this version of the skill. Also do the rest of the setup
     when `status` fails with `no run found`.

   New runs do not take the config, the `rules.md`, or the `notes.md` of
   an earlier run. Set each flag of `init`, such as the target, the limit,
   the models, the filters, and the shared globs, from the arguments and
   from this setup. Write `rules.md` from this setup too. The values of an
   earlier run fit the arguments and the tasks of that run, and the
   version of the skill that created it.
2. Choose the target of the run, as the Target section says. In step 4,
   `init` checks that the run can land on that target. Its error names
   the cause when it fails. Act on that cause:
   - **The main checkout has another branch checked out:** the cause
     applies to a branch target. When the arguments name the branch,
     check out the branch in the main checkout with `git switch <branch>`.
     When the arguments name no target, the user might want the tasks on
     the branch that the main checkout has checked out. Ask the user
     where the tasks land.
   - **The main checkout holds uncommitted changes:** the cause applies
     to a branch target, and the error lists the changes. Ask the user
     before you commit or remove a change that this session did not make.
   - **The base branch holds a commit that its upstream branch lacks:**
     the cause applies to a `pr` target. Each task branch starts from the
     local base branch, so each pull request carries such a commit. Ask
     the user whether to push the base branch, as the Pushes section
     says.

   `init` compares the base branch of a `pr` target with the remote
   branch that it follows. When the base branch follows no remote branch,
   `init` prints that it did not compare the two. Then confirm that the
   remote holds each commit of the local base branch.
3. Find how the project builds, tests, and checks itself, and what its
   commit hooks run. Read CLAUDE.md, the task runner config, and
   `git log -15`. Find the build caches that those commands fill, and the
   file system that holds each cache. Some caches evict their content when
   the free space of the disk falls to a threshold, so find that threshold
   for each cache. Do the same for each other repository that a task of the
   queue changes, as the Other repositories section says.
4. Run `ultraclose.py init --run <scratchpad>/ultraclose --limit <N>`.
   `init` prints the run directory in its `run=` line, and so does
   `status`. Each run takes a directory of its own. When
   `<scratchpad>/ultraclose` holds the `state.json` or the `tasks/`
   directory of an earlier run, the new run takes the next free directory,
   such as `<scratchpad>/ultraclose-2`. So the new run starts without the
   `rules.md` and the `notes.md` of that run. It also treats each branch
   and each worktree that the earlier run left as leftover work, as the
   Leftover work section says.

   `init` follows the decision of the `setup` line. When this session
   continues the run, `init` fails, and its error names that run. Pass
   `--force` only when the user asks for an empty run, or to change the
   target of a run that holds no task yet, as step 1 says. With `--force`,
   `init` replaces a run that this session continues, and the unfinished
   tasks of that run become leftover work.

   A task that `init` leaves in the earlier run keeps the branch of its
   pull request, such as a proposed task. That pull request can stay
   open. `init` prints one line per such task after a `left` line, with
   the ref, the branch, and the pull request that the earlier run
   recorded.
   The new run refuses a new attempt at the task until you act on that
   pull request and move the branch, as the Leftover work section says.
   Record each such pull request in `notes.md` for the report.

   When the `setup` line says that `init` moves unfinished tasks, the
   earlier run stopped before its end. `init` moves each pending, inflight,
   approved, proposed, and noted task of that run to the new run, and it
   prints those tasks. Each task keeps its triage row, its state, and its
   task directory, so the new run resumes its branch and its worktree.
   Every proposed task keeps its branch and its pull request. It keeps its
   worktree only when the landing that proposed it could not remove the
   worktree, as the Landing section says. It waits in the new run until
   `ultraclose.py merged` records the merge, as the Pull requests section
   says.

   When the arguments name filters, `init` leaves in the earlier run each
   pending task that the earlier run has not started and that those
   filters leave out, as the Filters section says. It prints those tasks
   after a `left` line, one line per task with the ref and the reasons.
   `untriaged` lists such a task again after a change of the filters lets
   it in. `init` prints each task that it moves only because a moved task
   waits for it after a `moved` line that names the filters, in the same
   form. Record each task of both lines in `notes.md` for the report, as
   the Notes section says.

   A task of the `moved` line can wait for a blocker that the filters
   keep from the run, and its `waits for` reason names that blocker. `next`
   starts such a task before its blocker lands. Before you run `next`,
   set the task to noted, as the Filters section says for a pending task
   that the run holds, or widen the filters with `config`.

   The new run holds each noted task, so `untriaged` does not list it.
   `status` still lists each pending task that waits for a noted task as
   stuck, and the Loop section says to note it.

   The earlier run keeps its landed and closed tasks, and the batch of the
   new run goes on from that run. So `status` names that run in an
   `earlier run` line, and the report covers it, as the Report section
   says.

   Before you run `init`, stop each Workflow that still runs one of the
   unfinished tasks. Its agents write to the task directory of the earlier
   run. After the setup, start those tasks again as the Interrupted run
   entry of the Hazards section says.

   Each unfinished task starts from the base branch of its run, and each
   proposed task has an open pull request. So `init` moves the tasks only
   to a run with the same base branch. It also moves them only when the
   landings of both runs propose pull requests, or the landings of both
   runs commit. Otherwise `init` fails, and its error names the target of
   the earlier run. The `target` line of `status` names that target as
   well.

   When `init` fails for that reason, ask the user whether the new
   run keeps that target, or starts without the unfinished tasks as an
   empty run with `--force`. With `--force`, each proposed task keeps its
   open pull request, as the Leftover work section says. A new run
   without `--target` has the target `main`, so it fails after a run with
   a `pr` target.

   Pass `--target <target>` as the Target section says.

   Pass `--model <role>=<model>` for each model that the arguments name,
   as the Models section says.

   Pass a filter flag for each status, priority, and label that the
   arguments name, as the Filters section says.

   Pass `--check '<command>'` with the full check of the project, unless
   the commit hook already runs that check. Always pass the full check for
   a `pr` target, because its landings run no commit hook.

   With a `pr` target, pass `--repo <name>=<base>` and
   `--repo-check <name>='<command>'` for each other repository that a task
   of the queue changes, as the Other repositories section says. A
   repository that only a later task needs joins the run with
   `ultraclose.py config` and the same flags.

   Pass `--health '<command>'` when the check or a commit hook requires a
   service, such as Docker. The health command is a fast command that
   fails when the service is down, such as `docker version`.

   Pass `--step-timeout <seconds>` when the check or the commit of a
   healthy landing can run longer than half an hour, such as a build with
   empty caches. Each landing stops its check and its commit when the
   step runs longer than the step timeout, as the Landing section says.
   The default is 3600 seconds. Set the step timeout to at least twice
   the longest time that a healthy step takes.

   Pass `--shared <glob>` for each kind of file that two running tasks may
   both edit. Each `--shared` glob adds to the default globs, which cover
   test files. The scheduler matches each glob against `/` plus the path
   of a file relative to the repository, and `*` also matches `/`. So
   `/README.md` matches the `README.md` at the root of the repository,
   `*/README.md` matches the `README.md` of every directory, and
   `README.md` matches no file.

   Pass the disk flags that fit the build caches of the check and the
   commit hooks. `ultraclose.py watch` exits when a watched file system is
   low, so that you can free space before the disk fills and before a
   cache evicts its content. A landing with an evicted cache builds
   everything again, so it takes many times as long.

   - `--disk-path <path>`: `watch` measures the file system of the
     repository and the file system of your home directory. Pass a path
     for each build cache that another file system holds, such as the
     data directory of Docker.
   - `--disk-floor-percent <dir>=<N>`: `watch` counts the file system
     that holds `<dir>` as low when less than N percent of it is free, and
     the flag adds `<dir>` to the watched paths. The floor does not apply
     to the other file systems. Pass this flag for each cache that has a
     threshold in percent of the disk. Set N to more than that threshold,
     and set `<dir>` to a directory on the file system of the cache. For
     example, the default garbage collection of the Dagger engine prunes
     its cache until 20% of the disk is free. So pass
     `--disk-floor-percent <dir>=25` with a `<dir>` on the file system
     that holds the state of the Dagger engine, such as the data directory
     of Docker or your home directory.
   - `--disk-floor-gb <N>`: `watch` counts every watched file system as
     low when fewer than N GB of it are free. The default is 30. Pass more
     when a cache has a higher threshold in GB.
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
   - A section for each other repository of the run, named after it, with
     the rules of this list for that repository.
   - A private lint cache for each worktree, when the project lints with
     golangci-lint. With the cache that all checkouts share, golangci-lint
     2.12 reports the issues of a worktree that a landing removed, under
     the paths of that worktree. Tell agents to set
     `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint-cache"`
     for each command that runs golangci-lint, such as `task lint`.

   Do not tell agents where to report a fault outside their task, even in
   a rule that forbids the commands that file tasks. The Workflow prompts
   ask for each such fault in the `defects` of the result, as the Defects
   section says. Agents put `rules.md` above their defaults, so a rule that
   sends the fault to `notes` hides it in prose again.

   Do not give agents a commit command in `rules.md` either. The Workflow
   prompts tell each agent that commits to search the skills of its
   session for each skill that covers committing. The agent follows such
   a skill for each commit of the task branch and for `msg.txt`. The
   commit rules of the prompts do not name a skill, and git is the only
   tool that they name. So the rules hold when the skills of a session
   change. An agent whose session lists no such skill uses its own
   judgment, such as for a message that says what a commit changes. Where
   a skill and `rules.md` disagree, the agent follows `rules.md`.

   Whatever a skill says, the agent follows the rules that the run
   depends on. It runs git in its worktree. No commit of the agent runs
   the commit hooks, because the hooks of a project can run the full
   check on every commit. It commits on the task branch, and it never
   creates, switches, or renames a branch. It adds commits and never
   rewrites one. It never pushes, never opens a pull request, and writes
   no task ref in a message.

   In a run with a `pr` target, each agent that commits also searches for
   a skill that covers writing a pull request. It follows that skill only
   for the title and the body in `msg.txt`. You push the branch and open
   the pull request, as the Pull requests section says.

   All agents of the run share the memory and the CPUs of one machine. So
   the Workflow prompts allow each agent at most two build, test, or probe
   commands at the same time. The prompts also forbid load that an agent
   creates to reproduce a failure.

   Compare the memory of the machine with the number of these commands
   that can run at the same time. A run with a limit of 5 can run 10 of
   them in its agents. The check and the commit hooks of a landing run
   beside them. The limit stops counting a task when its landing starts.
   So each task that you relaunch to sync it after a failed landing adds
   one agent beyond the limit.

   When the memory cannot hold that number of copies of the heaviest test
   or build, allow fewer commands in `rules.md`, such as one race test at
   a time in each agent. Let `rules.md` allow each agent at least one and
   at most two such commands at the same time. When the memory cannot hold
   a landing and one copy for each task that the limit allows, lower the
   limit with `ultraclose.py config --limit <N>`. Lower the limit even when
   the arguments set it. Tell the user the limit that you set and the
   reason, and record both in `notes.md`.
6. Triage the queue. List it with `ultraclose.py untriaged`, as the Scope
   section says. For each task, predict the files that it edits. Fan the
   reading out to Explore agents, and write the result as a JSON list:

   ```json
   [{"ref": "APP-7KQ9", "title": "...", "priority": "high",
     "files": ["pkg/a.go"], "after": ["APP-7KQ0"], "last": false}]
   ```

   Load the list with `ultraclose.py triage <file>`. The same command adds
   tasks later.

   For a task that changes another repository, add the `repos` field to
   its row. The field maps the name of each such repository to the files
   that the task edits there, such as `"repos": {"x": ["cmd/main.go"]}`,
   and each path is relative to that repository. `triage` refuses a
   repository that the run does not hold. The scheduler locks the files of
   each repository apart, so one path in two repositories gives two files.
   A row of a task that already has its branch in a repository keeps that
   repository.

   `after` names the tasks that must land first. `triage` reads the
   dependencies of each task from aven, and adds each open dependency to
   the `after` of the task. So write into `after` only the order that aven
   does not record, such as a task that builds on the change of another
   task. `triage` reads the dependencies each time it loads a task. After
   the aven dependencies of a pending task change, load the task again.

   `triage` refuses a task when an open dependency of the task sits
   outside the run and the filters of the run keep that dependency from
   the run. The filters keep such a dependency from the run when they
   leave out the dependency or a task on the chain of its own
   dependencies. A refused task that the run holds stays pending, so set
   it to noted, as the Filters section says.

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
change it. `ultraclose.py status` prints the target, the limit, the
filters, the shared globs, the models, the check, the health command, and
the step timeout. It also prints the free space and the floor of each
watched file system.

`ultraclose.py config` changes the config and prints the result. It takes
the `--limit`, `--rounds`, `--model`, `--check`, `--health`,
`--step-timeout`, `--shared`, `--disk-floor-gb`, `--disk-floor-percent`,
and `--disk-path` flags of `init`, and each filter flag of `init`. Its
`--unshared <glob>` flag removes a shared glob, and its
`--no-disk-path <path>` flag removes a disk path and the percent floor of
that path. `--disk-floor-percent <dir>=0` removes the percent floor of
`<dir>` and keeps `<dir>` as a disk path. `--model <role>=` removes the
model of `<role>`, as the Models section says. The filter flags of one
dimension replace the filters of that dimension, as the Filters section
says. `config` prints the warning of `triage` for each glob that its
`--shared` flag adds and that matches no file of any task.

`config` also takes the `--repo` and `--repo-check` flags of `init`, and
its `--no-repo <name>` flag removes another repository, as the Other
repositories section says. The `repo` lines of `status` print the other
repositories of the run.

`config` has no flag for the target, because each task of the run starts
from the base branch of that target.

Every config change applies to the commands that start after it. So a
running Workflow and a queued landing keep the values that they started
with. The `next` that a landing runs reads the config when it runs, after
the landing.

A run that you continue carries the config and the `rules.md` of the tasks
that it started with. When you continue the run, fit both to the arguments
and to the open tasks that are not in the run. `ultraclose.py untriaged`
prints those tasks, as the Scope section says. Fit the run in these ways:

- If the arguments name another target than the `target` line of `status`
  prints, tell the user that the run keeps its target. Ask the user
  whether to continue the run with that target.
- If the arguments hold a number, set the limit to it. Set a lower limit
  when the memory cannot hold that many tasks, as the Setup section says.
- If the arguments name a model, set it with `ultraclose.py config`, as the
  Models section says. The run keeps each model that the arguments do not
  change.
- If the arguments name a status, a priority, or a label, set the filters
  of that dimension with `ultraclose.py config`, as the Filters section
  says. The run keeps the filters of each dimension that the arguments do
  not name.
- If one of those tasks changes another repository that the run does not
  hold, add that repository with `ultraclose.py config --repo`, and add
  its rules to `rules.md`, as the Other repositories section says.
- When you change the limit, or when those tasks run a heavier test or
  build than the tasks of the run, compare the memory again, as the Setup
  section says for `rules.md`. Then fit the rule in `rules.md` that limits
  the commands of each agent.
- Remove each shared glob that covers files that two of those tasks must
  not edit at the same time, such as the source files of one module.
  `next` starts such tasks together, and a landing returns `CONFLICT` when
  both tasks changed the same lines.
- Change the check when it skips a part of the repository that those tasks
  change. When the new check requires a service that the health command
  does not test, change the health command as well. When the new check
  fills a build cache that the disk flags do not cover, set those flags as
  the Setup section says.
- Raise the step timeout when the check or the commit of those tasks can
  run longer than half of it, as the Setup section says.
- Rewrite each rule in `rules.md` that the landed tasks made false, such as
  the layout of a module that they split. Remove each exception that the
  run made for one of them.
- When the `skill` line of `status` says that the command comes from
  another version, the skill changed after the run started. Compare
  `rules.md` with step 5 of the Setup section, and rewrite each rule that
  the step states in another way. Do the same when the line says that
  the run records no version.

## Models

Every agent follows the session model unless the run names a model for the
role of that agent. `init` and `config` take one `--model <role>=<model>`
flag per role. This table lists each role with the agents that run on its
model:

| Role | Agents |
| --- | --- |
| `implementer` | The implementer of each task |
| `sync` | The sync agent of each task that you sync |
| `reviewer` | Each reviewer, including the reviewer of the final cycle |
| `fixer` | Each fixer, including the fixer of the final cycle |
| `narrow` | The agent that narrows the claims of a `landable` change |
| `default` | Each agent whose role has no model of its own |

Pass one flag for each role whose model the arguments name. Pass the
`default` role for a model that the arguments give to every agent, or to
every agent that they do not name. For example, the arguments
`fable-5-2 for the implementer and opus-5-5 elsewhere` become
`--model implementer=claude-fable-5-2 --model default=claude-opus-5-5`.
For `<model>`, pass the id of the model, such as `claude-opus-5-5` for
`opus-5-5`. When the arguments name no model, pass no `--model` flag.

The `default` model covers only the roles that have no model of their own.
So when you pass the `default` role to a run that already names
models, also pass `--model <role>=` for each role other than `default` that
the `models` line of `ultraclose.py status` names and that the arguments do
not name. Those roles then run on the new `default` model. For example, in
a run whose `models` line names `implementer=claude-fable-5-2`, the
arguments `opus-5-5 for every agent` become
`--model default=claude-opus-5-5 --model implementer=`.

The run config keeps the models. `ultraclose.py status` prints them, and
it prints `models: none` for a run in which every agent follows the
session model. `next` and `args` write the model of each role into the
`models` field of each input object, and so does the `next` of a landing.
`close-task.js` starts each agent on the model of its role. So every
launch and every relaunch runs on the models of the run. Do not edit the
copy of `close-task.js` to set a model.

To change a model during the run, run
`ultraclose.py config --model <role>=<model>`. With an empty model, such
as `--model reviewer=`, `config` removes the model of the role. The agents
of that role then run on the `default` model, or follow the session model
when the run has no `default` model. Every running Workflow keeps the
models of its input object, as the Config section says.

`ultraclose.py` does not check that Workflow can run a model. Agents that
Workflow cannot start on their model return nothing, and the `notes` of
the result name the agent that returned nothing. When the run names a
model for the role of that agent, the `action` field of the result names
that model. Check the model, as the action says. The action also gives
the command that relaunches the task after you correct a wrong model
with `config --model`.

When the reviewer of the final cycle returns nothing, the `action` field
of the result notes the task and names no model. A reviewer of the same
Workflow answered on the model of that role before the final cycle, so
the model works.

## Leftover work

Each run resumes only the branches and the worktrees that it created, and
those of the tasks that `init` moved to it. Earlier runs can leave work
for a task that is open again:

- **The `task/<ref>` branch:** every run leaves the branch of each task
  that it notes.
- **The `t-<ref>` worktree:** runs that stop before their end leave the
  worktree of each task that they started.
- **The branch of a pull request:** landings with a `pr` target propose
  each task as a pull request. Runs leave the branch of such a task when
  `init` does not move the task to the new run, such as with `--force`.
  That branch has the name `<type>/<slug>`, and its pull request can
  still be open. So `init` records the branch and the pull request of
  each such task in the new run.

A task that changes other repositories can leave each of these in each of
those repositories too. The line of such work names the main checkout of
its repository.

The branch of an earlier run can sit far behind the base branch. Every
landing carries the whole branch, as one squashed commit or as the commits
of a pull request. So a task that starts on that branch lands the commits
that the earlier run did not land. Each new attempt at a task with a pull
request also opens a pull request of its own beside the first one.

`ultraclose.py next` starts no task while a pending task has leftover
work, and neither does the `next` of a landing. `ultraclose.py args <ref>`
refuses a task that has leftover work. Each of these commands then prints
one line per such task. The line names the branch, the count of its commits
that the base branch lacks, and the worktree. For the branch of a pull
request, the line also names the pull request, or says that the earlier
run recorded none.

Move the leftover work of each task out of the way with
`ultraclose.py leftover <ref>`. Then run `next` or `args <ref>` again.
After a landing that printed the lines, run `ultraclose.py next`, because
the task of the landing has landed. Each task starts on a new branch from
the base branch.

The `leftover` command keeps each commit and each uncommitted change of
the earlier run. It prints the branch that keeps them, which has the name
`kept/<ref>-<date>`. It also names that branch in a note on the aven
task. The implementer of the new attempt reads the note in its task file.

`ultraclose.py` contacts no remote. So the `leftover` command moves the
branch of a pull request only with the `--pr <state>` flag, which gives
the state of that pull request. Each refusal gives the steps that find
the state. Take those steps, and follow these rules:

- Record each pull request that you find in `notes.md`.
- Pass `--pr open` only when the user asks you to keep the pull request
  open. Otherwise close an open pull request, and pass `--pr closed`.
- Before you pass `--pr merged`, bring the merge into the local base
  branch, as step 2 of the merge steps of the Pull requests section
  says. A merged task does not need a new attempt, so the command then
  closes the task and sets the aven task to `done`.
- For the branch of a pull request in another repository, pass
  `--pr <name>=<state>` with the name of that repository. Pass one `--pr`
  flag for each such branch. The command closes the task only when every
  pull request of the task has merged.

`ultraclose.py set <ref> noted` removes the clean worktree of the task.
`set <ref> closed` removes that worktree and then deletes the branch.
Neither command deletes a branch whose worktree is gone or dirty.
`set <ref> landed` removes the clean worktree as well. It deletes the
branch only when the worktree is gone and the base branch holds the
change of the branch. Each of these commands removes only the work of the
run.

Each `set` command also acts on the worktree and the branch of the task
in each other repository that the task changes, in the same way.

For a task with leftover work, each `set` command keeps the worktree and
the branch. When the worktree exists, the command prints a line that names
the worktree. `next` starts only pending tasks, so the leftover work of a
noted or closed task can stay. To remove that work, run the `leftover`
command, so that a branch keeps its commits.

`ultraclose.py` creates the directory of a task in the run directory when
it starts the task. It treats the branch or the worktree of a task without
that directory as leftover work. So do not create or remove a task
directory yourself.

`ultraclose.py init` takes no run directory that holds the `tasks/`
directory of an earlier run, as the Setup section says. A run in that
directory treats each task directory there as its own, so it resumes the
branch of that task. With `--force`, `init` takes the `--run` directory
when it holds `tasks/` and no `state.json`. It takes no other such
directory, even with `--force`. Pass `--force` for such a directory only
when the user asks you to resume the tasks of that run.

## Loop

Repeat until the run ends:

1. Run `ultraclose.py next`. It starts one task per free slot. No two
   running tasks share a file outside the shared globs. It prints one input
   object per started task. After a user message about other work, the
   User messages section says in which turn to run it. When `next`, or the
   `next` of a landing, reports the branch or the worktree of an earlier
   run, move that work out of the way as the Leftover work section says.

   A landing with the result `OK` or `PR` runs `next`, as the Landing
   section says. In a turn that acts on such a landing, run this step only
   after an action that can let a task start. These actions are a triage
   that adds tasks, a move of leftover work as the Leftover work section
   says, a `merged <ref>`, a landing that you start, and a config change.
   The commands `set <ref> noted`, `set <ref> closed`, `set <ref> landed`,
   and `set <ref> pending` are such actions too. The `next` of the landing
   ran before those actions, so it missed each task that they add or let
   start. Skip this step in a turn that takes none of those
   actions, even after a triage that adds no task.
2. Launch one Workflow per object:
   `Workflow({scriptPath: <script>, args: <object>})`. For `<script>`, pass
   the `script` field of the object. That field holds the path of the copy
   of `close-task.js` that names the task of the object. One task per
   Workflow lets each task land the moment it finishes. Do not set a model
   in the call. The object names the model of each role, as the Models
   section says.
3. Keep `ultraclose.py watch` running in a background Bash call. It exits
   with a `DISK` line when a watched file system is low, and the Hazards
   section says what to do then. It exits with an `ENDED` line when the
   run ends, so start it again after you add tasks to an ended run.
4. End the turn. Each Workflow completion wakes you with a result. File its
   `defects` as the Defects section says, in the same turn as you act on
   the result. When the result gives a user message about other work as
   the reason for its status, relaunch the task as the User messages
   section says. Otherwise take the action that the `action` field of
   the result gives.

The action of a result follows its `status`, and it gives the command
that you run:

| Status | Action |
| --- | --- |
| `approved` | Land the task. |
| `landable` | File each entry of `followups` as a task, and land the task. |
| `obsolete` | Close the task. |
| `abandoned`, `rejected`, `unconverged` | Note the task. Do not force the task through. |
| `error` | Relaunch the task one time, and note the task after a second error. |

To note or to close a task, run `ultraclose.py set` with the `--note`
flag. The command runs the aven commands of the state that it sets:

| Command | Aven task | Work of the run |
| --- | --- | --- |
| `set <ref> noted --note '<reason>'` | Gets the reason, the branch, and the pull request of the task as a note, and the status `todo` | The branch stays |
| `set <ref> closed --note '<evidence>'` | Gets the evidence as a note, and the status `done` | The branch goes with a clean worktree |
| `set <ref> landed` | Gets the status `done` | The branch goes when the base branch holds its change |

Run `set <ref> landed` only when an `ACTION` line of a landing says that
the task has landed. `set <ref> noted` prints a `stuck` line for each
pending task that waits for a noted task, and so does `status`. Note
those tasks too.

A run with a `pr` target ends only after each of its pull requests has
merged or closed. When no Workflow and no landing runs, and `next` starts
no task, the run waits for the user to merge pull requests. Give the user
the `proposed` lines of `ultraclose.py status`, and end the turn. When the
user reports a merge, continue as the Pull requests section says.

The relaunch of a proposed task works on the branch of its open pull
request. When such a relaunch does not end in a landing, its action also
says what to do with the pull request.

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
defects in `defects`, whatever its status. Each entry holds a `title` in
sentence case that names the symptom, and a `description` in Markdown with
the sections Observed, Expected, Reproduction, and Evidence.

When a result arrives, file its defects in the same turn. Act on its status
first, so that its landing or its relaunch runs in the background while you
file:

1. Search the skills that your session lists for each skill that covers
   filing a task. Load each skill that you find, and file each entry as it
   says. Where your session lists no such skill, use your own judgment.
   For example, you might file each entry in the aven project of the
   repository, so that its task joins the queue. Two results can report the
   same defect, so search the open tasks for a duplicate before you file an
   entry.
2. Link each new task to the task that reported the defect, with
   `aven related add <new-ref> <ref>`.
3. Record each new task in `notes.md`. When the filters of the run leave
   the task out, record that too.

Do not wait for the user to ask for these tasks, and do not reproduce a
defect before you file it. Each new task joins the queue when it passes
the filters of the run, as the Filters section says. Its implementer
confirms the fault before it changes anything.

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

Run `ultraclose.py land <ref> --next` in a background Bash call. Landings
queue on a lock, so start one as soon as a task is ready. The command merges
the base branch into the worktree and runs the check. The target of the run
sets the rest of the landing:

- **A branch target:** the command squashes the branch into one commit on
  the base branch with `msg.txt` as the message. Then it removes the
  worktree and sets the aven task to `done`.
- **A `pr` target:** the command names the branch for its pull request,
  removes the worktree, and sets the task to `proposed` in the run. It
  records no commit on the base branch, and the aven task stays `active`.
  You push the branch and open the pull request, as the Pull requests
  section says.

A landing of a task that changes other repositories takes each
repository whose branch holds a change of its own, and each repository
that an earlier landing proposed. It merges the base branch into the
worktree of each of them, and it proposes each of them whose branch then
holds a change. It runs the check of each repository that it proposes,
and the `TIMES` line gives the sum of those checks. The `ACTION` line of
a failure in another repository opens with the name of that repository,
its worktree, and its branch. The log of a step there adds the name of
the repository, such as `logs/<ref>.x.check.log`.

A branch of the task can lose its change, such as when its pull request
merged before the run recorded the merge, or when a fix reverted the
change there. What the landing then does depends on the repositories
whose branch lost its change:

- **Another repository that an earlier landing proposed:** the landing
  ends with `EMPTY`, and its `ACTION` line names that repository and its
  pull request. The Pull requests section gives the steps that record
  the merge there.
- **The repository of the run, when another repository still holds a
  change:** the landing proposes the other repository and removes the
  worktree of the task in the repository of the run. Its `RESULT` line
  then names no branch, and its `ACTION` line names the pull request of
  the repository of the run.
- **Every repository of the task:** the landing ends with `EMPTY`. A
  commit of a base branch can make the change of the task. The `ACTION`
  line then names each repository whose branch held a change of its own,
  with a `git log` command that finds that commit there. Otherwise the
  `ACTION` line gives a `git reset` of each worktree whose branch holds
  commits before the relaunch. `args` starts a task at the implementer
  only when no branch of the task holds a commit that its base branch
  lacks. Commits that cancel each other out still count.

The landing reads the message of a repository only when its branch holds
a change of its own. So a fix that reverts the change of a repository
can remove its message.

Each landing of a run with a health command runs that command before the
merge, and again after a failed check or a failed commit.

`ultraclose.py land` sets `GOLANGCI_LINT_CACHE` for the check and for the
commit, so neither step reads the lint cache that all checkouts share. The
check gets the directory that the lint cache rule of the Setup section
names, inside the git directory of the worktree. Git removes that directory
together with the worktree. The commit hooks run in the main checkout, and
they get the `golangci-lint-cache` directory of the run directory.

The command ends its output with these lines, in this order:

| Line | Content |
| --- | --- |
| The failing output | The last lines of the output of a failed step. The run directory holds the full output of each step in `logs/<ref>.<step>.log`. |
| `TIMES` | How long the check and the commit took, such as `TIMES check=1m22s (median 1m24s) commit=11m52s (median 18s)`. |
| `SLOW` | One line for each step that took more than twice its median, and more than a minute longer than its median. |
| `REPO` | One line per other repository in which a landing with the result `PR` proposed the task, as the Pull requests section says. |
| `ACTION` | The action that you take. |
| `RESULT` | The result. It is the last line of a failed landing. |
| The list of input objects | The tasks that `next` started after an `OK` or a `PR` result of a landing with `--next`. The list is empty when no task starts. |

The time of the commit in the `TIMES` line includes the commit hooks. When
a commit hook changes the index, the landing commits a second time, and
the time of the commit covers both attempts. Each median covers the
earlier landings of the run with the result `OK` or `PR`.
`ultraclose.py status` prints the medians and the times of the last
landings.

Do not report the time of a step with a `SLOW` line as normal. Find the
cause before you start another landing. A build cache that evicted its
content is a common cause, and the Disk entry of the Hazards section says
how to free space.

Take the action that the `ACTION` line gives. The action names the
command that you run, and the cause of the result where the script can
tell it from the run. The script counts the failed landings of each task
by cause. So the action changes when a task fails again for the same
cause.

After an `OK` or a `PR` result, the action of a landing with `--next` says
to launch the objects of its list. Skip each object whose task you
relaunched after the landing started, because a Workflow runs that task
already.

Some actions tell you to sync the task. To sync a task, relaunch it
with the reason:

```bash
ultraclose.py args <ref> --sync '<reason and failing output>'
```

A sync agent merges and adapts, and a reviewer checks the merge. Land the
task again after its Workflow ends `approved` or `landable`. Note the task
when a second sync fails for the same reason.

With the `--next` flag, one background call lands the task and starts the
next tasks. After a failed landing, the flag does not run `next`.

After a landing commits its task, or names the branch of the pull request,
it does not stop at a command that fails. The target of the run sets the
commands that the landing ends with:

- **A branch target:** `git worktree remove`, `git branch -D`, and
  `aven edit`.
- **A `pr` target:** `git worktree remove`.

Each of these commands can fail, such as git with a locked worktree, or
aven with a locked database. git also keeps the branch of a worktree that
it did not remove. The task then still lands with the result `OK` or
`POSTDIRTY`, or the landing still proposes it with the result `PR`. After
`OK` or `PR`, the `--next` flag runs `next`, and the command exits with a
failure status. Before the `TIMES` line and the `RESULT` line, the output
gives each failed command, its error, and one line that says what the
command left:

```text
git worktree remove --force /repo/.claude/worktrees/t-APP-7KQ9 failed:
fatal: cannot remove a locked working tree;
use 'remove -f -f' to override or unlock first
The task landed, but its worktree still exists. Repair the cause of the error, and run the git command again.
git branch -q -D task/APP-7KQ9 failed:
error: cannot delete branch 'task/APP-7KQ9' used by worktree at '/repo/.claude/worktrees/t-APP-7KQ9'
The task landed, but its branch still exists. Repair the cause of the error, and run the git command again.
TIMES check=1m22s (median 1m24s) commit=18s (median 18s)
```

The `ACTION` line of such a landing gives the repair of each failed
command, in the order of the output, with the command that you run.

Every failed landing leaves its task `approved`, with one exception. A
landing with the result `POSTDIRTY` lands its task before it fails,
because the base branch holds the commit of the task by then. Every
`approved` task keeps the files of its triage row, and each task that
comes after it keeps waiting. The run also does not end while it holds an
`approved` task.

`ultraclose.py land` takes only an inflight or an approved task. For a task
in another state, it prints an error that names the state, and it prints no
`RESULT` line. The command then changes nothing, and it does not run `next`.
So a second landing of a landed task leaves the task landed. Take no action
for a landed or a closed task. Every noted task takes a landing only after
you relaunch it with `ultraclose.py args <ref>` and its Workflow ends
`approved` or `landable`.

`ultraclose.py land` checks the state again after it waits for the landing
lock. So it prints the same error for a task that another landing landed
or proposed during that wait. Take no action for that error, and act on
the result of the other landing.

The action of `CHECKFAIL`, `COMMITFAIL`, `CHECKTIMEOUT`, and
`COMMITTIMEOUT` can send you to this section, because only the failing
output shows the cause of these results.

`CHECKFAIL` and `COMMITFAIL` do not show that the task must change, and
each sync costs an agent run and a review. `land` reads the log of the
failed step, and it quotes the first line that names a lack of memory or
a full disk. Failed tests can print such a line too, such as a test that
reports a killed process. The action says what to do when that line shows
why the step failed. In every other case, find the cause in the failing
output, and act on that cause:

| Cause | Action |
| --- | --- |
| A failure of the environment that `land` did not name stopped the check or the hook, such as a down service or a network failure. | Repair the environment and land again. Do not sync the task. When the same task fails a second time for the same cause, do not land the task again. Sync the task with the log of the failed step when that log shows that the change causes the failure. Otherwise, note the task. |
| A failed test covers code that the task did not change. | Land again one time. Sync the task when the same test fails again. |
| The check or the hook reports a fault in the change, such as a broken build, a linter finding, or a failed test of the changed code. | Sync the task. |

`CHECKTIMEOUT` and `COMMITTIMEOUT` do not show that the task must change
either. Each landing stops its check and its commit when the step runs
longer than the step timeout of the run. So a hook that hangs holds the
queued landings only until the landing stops it. The landing does not run
the health command after it stops a step, so neither result shows whether
the service answers. The last lines of the log of the step come before
the `TIMES` line, and they show what the step did when the landing
stopped it. Act on the cause before you start another landing:

| Cause | Action |
| --- | --- |
| The step waited for a service that did not answer, such as a Dagger call whose engine another client removed. | Confirm that the service answers, such as with the health command of the run, and repair the service when it does not. Then land again. Do not sync the task. |
| The step still did its work, such as a build that fills an empty cache. | Find why the step was slow. When a healthy step takes that long, raise the step timeout with `ultraclose.py config --step-timeout <seconds>`. Then land again. Do not sync the task. |
| The step ran a build or a test of the changed code that does not end. | Sync the task. |

When you fix something by hand before a landing, record the fix in
`notes.md`, because no reviewer saw it.

## Pull requests

Each landing with a `pr` target ends with the result `PR`. You then push
the branch of the task and open its pull request, because `ultraclose.py`
contacts no remote. The `RESULT` line names the branch as `head` and the
base branch as `base`:

```text
RESULT APP-7KQ9 PR head=fix/retry-a-timeout base=main
```

The landing gives the branch a name of the form `<type>/<slug>`. `<type>`
is the Conventional Commits type of the subject of `msg.txt`, or `feat`
for a subject without such a type. `<slug>` holds the words of the rest of
the subject. Some hosts have permission rules that allow the push of a
branch with such a name, and a draft pull request from it. Both calls then
run without a prompt.

The landing fails with `BRANCHTAKEN` when git cannot give the task branch
that name. Git keeps no branch below the name of another branch, so a
branch with the name `<type>` blocks every name that starts with
`<type>/`. The `ACTION` line says whether to ask the user about such a
branch, or to land again.

Take these steps for each `PR` result:

1. If the `RESULT` line ends with `pr=<url>`, read that pull request with
   `mcp__github__pull_request_read` before the push. Act on its state as
   the list after these steps says, in place of steps 2 to 4.
2. Push the `head` branch with `mcp__git__git_push`. Pass the main
   checkout as `repo` and the branch as `ref`, and set `set_upstream`.
3. Open the pull request with `mcp__github__create_pull_request`. Set
   `draft` to true, `head` to the `head` branch, and `base` to the `base`
   branch. Take `owner` and `repo` from the URL of the remote. In the run
   directory, the first line of `tasks/<ref>/msg.txt` is the title, and
   the text after the first blank line of that file is the body.
4. Record the push and the pull request with
   `ultraclose.py opened <ref> <url>`.

The `RESULT` line ends with `pr=<url>` when the task records a pull request
from an earlier landing, such as after a relaunch. The relaunch might
change `msg.txt`, and the user might close or merge the pull request in
the meantime. Act on the state that step 1 read:

- **Open:** take step 2, so that the push updates the pull request. Then
  set its title and its body from `tasks/<ref>/msg.txt` with
  `mcp__github__update_pull_request`, as step 3 splits the file. Then
  take step 4 with the same address, which records the push.
- **Closed without a merge:** take steps 2 to 4. They open a new pull
  request, and `opened` replaces the recorded address.
- **Merged:** push nothing, and take the merge steps of this section. If
  `ultraclose.py merged <ref>` still fails after step 2 of those steps,
  the branch holds commits of the relaunch that the merge lacks. Ask the
  user whether to propose those commits in a new pull request.

When the remote rejects the push, or the pull request does not open,
record the failure in `notes.md` for the report. The task stays
`proposed`, and its `status` line names the branch that waits for its push.
When the task records a pull request, the line names it as well, because
the push updates that pull request.

The push publishes each commit of the branch with its message. So the
landing fails with `LOGHASREF` when one of those messages holds a task
ref, and its output names each such commit. The `ACTION` line gives the
command that replaces the commits of the branch with one commit, whose
message is `tasks/<ref>/msg.txt`. The pull request then shows that
message, which the landing has already checked for task refs. If the
remote holds the branch already, the next push replaces commits there,
so set `force_with_lease` on that push.

A landing of a task that changes other repositories prints one `REPO`
line per other repository in which it proposed the task, before its
`ACTION` line:

```text
REPO x head=fix/retry-a-timeout base=main path=/home/me/repos/x
ACTION ...
RESULT APP-7KQ9 PR head=fix/retry-a-timeout base=master
```

The `RESULT` line names no `head` when the task holds no change in the
repository of the run. Take the steps for each `PR` result for each
`REPO` line first, and for the `RESULT` line last. For a `REPO` line,
change the steps in these ways:

- Pass the main checkout that `path` names as the `repo` of the push, and
  take `owner` and `repo` of the pull request from the URL of its remote.
- Take the title and the body from `tasks/<ref>/msg-<name>.txt`, where
  `<name>` is the name that opens the line.
- Record the pull request with `ultraclose.py opened <ref> <url> --repo <name>`.
- A line that ends with `pr=<url>` names a recorded pull request, as the
  `RESULT` line does.

The pull request of the repository of the run merges after the pull
requests of the other repositories, because its change can depend on
theirs. So end its body with a line that names each of those pull
requests, such as `Depends on <url>.`, and tell the user to merge them
first.

Every proposed task waits for its pull request to merge, and the run
holds back the tasks that the merge affects:

- The aven task stays `active`.
- The proposed task keeps the files of its triage row. So `next` starts
  no task whose row holds one of those files, unless a shared glob covers
  the file.
- Each task that comes after the proposed task keeps waiting, so it
  starts from a base branch that holds the change.
- `ultraclose.py status` prints one `proposed` line per task and
  repository. The line names the pull request that waits for its merge,
  or the branch that waits for its push. Each landing sets the line to the
  branch, also after a relaunch, and `opened` sets it to the pull request.
  The line of a branch in the repository of the run that lost its change
  says that the branch holds no change, as the Landing section says. It
  also names the pull request of that branch, which you close when it is
  open.
  The line of another repository names that repository after the ref,
  such as `proposed APP-7KQ9 in x`.

When a pull request has merged, land its task:

1. Confirm the merge with `mcp__github__pull_request_read`.
2. Bring the merge into the local base branch. When the main checkout has
   the base branch checked out, pull with `mcp__git__git_pull`. Otherwise
   fetch with `mcp__git__git_fetch`, and move the base branch forward
   with
   `git merge-base --is-ancestor <base> <remote>/<base> && git branch -f <base> <remote>/<base>`.
3. Run `ultraclose.py merged <ref>`. The command deletes the local branch
   of the task, sets the aven task to `done`, and sets the task to
   `landed`.
4. Run `ultraclose.py next`, because the merge frees the files of the
   task and the tasks that come after it.

When `merged` fails in step 3, it changes nothing. Act on the cause that
its error gives:

- **The base branch lacks the change of the branch:** the base branch
  holds the change when a merge of the branch into it has no conflict and
  changes no file. A merge into a commit that the base branch gained after
  the fork of the branch counts too. So a later commit of the base branch
  that changes the same lines does not hide the merge. Take step 2 again.
  Pass `--force` only when the pull request merged with commits that the
  local branch lacks, such as commits that the user pushed during the
  review.
- **The worktree of the task keeps the branch:** the landing that
  proposed the task did not remove the worktree. git deletes no branch
  that a worktree has checked out. Remove the worktree with the
  `git worktree remove --force <worktree>` command that the error gives,
  in the main checkout that the error names. Then run
  `ultraclose.py merged <ref>` again.

  When git fails to remove the worktree, repair the cause that its error
  names, and run the git command again. When git then answers that the
  path is not a working tree, git deleted its record of the worktree, and
  only the directory stays. Delete the directory with `rm -rf <worktree>`.
  Then run `ultraclose.py merged <ref>` again.
- **Another worktree has the branch checked out:** git deletes no such
  branch. The main checkout can have the branch checked out after a
  review. Ask the user to check out another branch there, and run the
  command again.

When the pull request of another repository has merged, take the same
steps with these changes:

- In step 2, bring the merge into the local base branch of that
  repository, in its main checkout.
- In step 3, run `ultraclose.py merged <ref> --repo <name>`. The command
  deletes the branch of the task there. The task lands when every pull
  request of the task has merged. `merged <ref>` without `--repo` fails
  until each other pull request of the task has merged.
- When `merged <ref> --repo <name>` fails, run each git command of the
  repair in the main checkout of that repository, which the error names.
  git acts only on the worktrees of the checkout that it runs in. So in
  the main checkout of the run, `git worktree remove` answers that the
  worktree is not a working tree. The other repository still lists the
  worktree, and the worktree keeps the branch there. Delete the directory
  with `rm -rf <worktree>` only after git gives that answer in the main
  checkout of that repository.
- After the merge of the last other pull request, the command says how
  to sync the task for a pin to a commit that holds the merge. A flake
  input in the repository of the run is such a pin. The command names
  each merged repository and the commit of its base branch when the run
  recorded the merge. The agents kept each such pin at its old value and
  named it in their notes. For such a pin, sync the task with each merged
  commit and each pin, and land the task again after its Workflow ends
  `approved` or `landable`. The relaunch has no worktree in a merged
  repository.
- When the pull request of the repository of the run still waits, the
  `PR` result of the landing after the sync ends with `pr=<url>`. When
  the task proposed no pull request in the repository of the run, the
  merge lands the task. The sync then starts the landed task again, and
  its landing proposes the pin.

The landing of a relaunch can end with `EMPTY` in another repository, as
the Landing section says. Its `ACTION` line names that repository and
its pull request, and the task stays `approved`. Take these steps:

1. Read that pull request, and close it when it is open.
2. When it merged, bring the merge into the base branch of that
   repository.
3. Run `ultraclose.py merged <ref> --repo <name>`, which takes the
   approved task. The command removes the clean worktree of the task
   there, deletes the branch, and keeps the task `approved`.
4. Land the task again, or sync it first for a pin, as the command says.
   The command gives the sync only when no other pull request of the
   task waits for its merge.

When the task then holds no change in the repository of the run, the
landing ends with `EMPTY`. Its `ACTION` line then says to set the task
to `landed`.

`merged <ref> --repo <name>` refuses an approved task while a landing
holds the landing lock. A landing of the task might wait for that lock,
and the landing reads the worktree that the command removes. Run the
command again after the landing ends. The land command refuses every
proposed task, so `merged` takes a proposed task at any time.

The landing of a relaunch can also find no change in the repository of
the run, and a change in another repository, as the Landing section
says. Its `ACTION` line names the pull request of the repository of the
run. Read that pull request, close it when it is open, and push nothing
to it. Take the steps for each `REPO` line. After you record the merge
of each other pull request, run `ultraclose.py merged <ref>`, which
lands the task. Do not set the task to `landed` or `closed` before then.

Some pull requests end without a merge, or stop merging. Act on each of
these events as this table says:

| Event | Action |
| --- | --- |
| The pull request has a conflict with the base branch | Bring the base branch up to date as step 2 of the merge steps says. Sync the task with `ultraclose.py args <ref> --sync '<reason>'`, as the Landing section says. The relaunch works on the branch of the pull request. Land the task again after its Workflow ends `approved` or `landable`. The `PR` result of that landing ends with `pr=<url>`. |
| The user closes the pull request without a merge | Note the task with `ultraclose.py set <ref> noted --note '<reason>'`. The branch stays. When the user asks for the task again, relaunch it with `ultraclose.py args <ref>`. Its landing names the closed pull request, so the steps for each `PR` result open a new one. |
| A relaunch of the task ends `obsolete` | Take the action of the result. It names the pull request, and it says to close that pull request with `mcp__github__update_pull_request` before you close the task. Set `state` to `closed`, and record the closed pull request in `notes.md`. |
| A relaunch of the task ends `abandoned`, `rejected`, or `unconverged` | Take the action of the result. It names the pull request, and it says to leave that pull request open for the user. `set <ref> noted` adds the address of the pull request to the note of the task. Record the open pull request in `notes.md`. |

`ultraclose.py opened` records the pull request in the `pr` field of the
task in `state.json`. Each later launch of the task carries that pull
request to the `action` of its result.

The branch of a pull request does not have the name `task/<ref>`. So a
new run finds that branch only in its record of the tasks that `init` left
in the earlier run, as the Leftover work section says. A new attempt at
the task starts on a new `task/<ref>` branch.

## Pushes

Ask the user before the first push. After that, push only when a task
requires a pushed commit, such as a pin to a commit of this repository. If
the remote rejects a push, for example because the token lacks the scope for
workflow files, record the rejection for the report and continue.

With a `pr` target, the user asked for one pull request per task. So push
the branch of each proposed task without a question, as the Pull requests
section says. Ask before every other push.

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
`close-task.js` Workflow in the turn that receives it. Take each of these
actions in the turn that the next notification starts:

- Run `ultraclose.py next`, and launch each object that it prints.
- Run each relaunch.
- Launch each object that a landing printed and that you have not launched,
  except the objects that the Landing section skips.

Take them in the turn that receives the message only when no Workflow and
no landing is running, because no notification comes then. Launch a
Workflow that the user asks for in the turn of the request, because the
relayed message is the request of that Workflow.

When a result gives a user message about other work as the reason for its
status, the status says nothing about the task. Relaunch the task with
`ultraclose.py args <ref>` in place of the action that the result gives.
When the message was about the task, take the action of the result.

## Hazards

- **Messages to agents:** never send a message to a Workflow agent by id.
  The message resumes a second copy of the agent. Relaunch the task with
  `ultraclose.py args <ref>` instead.
- **Stash:** never run `git stash`. All worktrees share one stash list.
- **Disk:** when `watch` prints `DISK`, free space on each file system
  that the `DISK` lines name, then start `watch` again. Clean the build
  caches only while no landing and no check runs. A cache clean races
  running builds, so land again any task that failed during the clean.
  Start with what the check and the hooks do not reuse, such as unused
  container volumes, because the caches that they reuse keep landings
  fast. When a `DISK` line says that `watch` cannot measure a path, remove
  that path with `ultraclose.py config --no-disk-path <path>`.

  When you cannot free enough space, lower the floor that the `DISK` line
  names with `ultraclose.py config`, and record the change in `notes.md`.
  A line that ends with `set by --disk-floor-percent <dir>=<N>` names the
  percent floor of `<dir>`. Pass `--disk-floor-percent <dir>=<M>` with an
  M less than N, or with an M of 0 to remove the floor. Every other line
  names the floor in GB, so lower `--disk-floor-gb`, which applies to
  every watched file system. A file system that holds no build cache and
  no worktree, such as a small home partition, does not need a floor.
  Remove its path with `--no-disk-path <path>`.
- **Lint cache:** golangci-lint issues under the path of another worktree,
  such as `../t-<ref>/...`, come from the lint cache that all checkouts
  share. The command that reported them ran without the private cache
  that the Setup section describes. If `rules.md` lacks that rule, add
  it. Do not clean the shared cache to remove the issues. Every clean
  races the commands that lint with that cache, and the issues return
  when a landing removes the next worktree that filled the cache.
- **Interrupted run:** after a usage limit, a model switch, or a new
  session, run `ultraclose.py status`. A landing that ran `next` during
  the interruption set each task of its list to inflight, but no Workflow
  runs those tasks. Launch the objects of such a list when you act on the
  result of the landing, and do not relaunch their tasks. Relaunch each
  other inflight task with `ultraclose.py args <ref>`. The agents find the
  unfinished work in the worktree. A list that arrives after the relaunch
  can name a task that you relaunched, and the Landing section says to
  skip its object.

  Each new session first follows the `setup` line of `status`, as the
  Setup section says. New sessions do not run `watch`, so start it again.
  After the earlier session has ended, no landing runs for its approved
  tasks. Land each of them again, as the Landing section says.

  No proposed task takes a landing. When the `proposed` line of a task
  names a branch that waits for its push, take the steps of the Pull
  requests section for each `PR` result, with that branch as `head`. A
  line that also names a pull request stands for a `RESULT` line that ends
  with `pr=<url>`. For a line without a pull request, the earlier session
  might have opened one before it ended. If the pull request of that
  branch is open already, record it with `ultraclose.py opened <ref> <url>`
  in place of steps 2 to 4. A line that names another repository after the
  ref stands for a `REPO` line of that repository, and `opened` then takes
  `--repo <name>`.

  A `proposed` line that says that the branch holds no change stands for
  the pull request of the repository of the run that a landing left out,
  as the Pull requests section says. The line names that pull request, or
  the branch when the task records no pull request. Close that pull
  request, or an open pull request of that branch, when it is open. Push
  nothing to the branch.
  After you record the merge of each other pull request, run
  `ultraclose.py merged <ref>`, which lands the task.

## Notes

Append to `notes.md` in the run directory when something happens that the
final report requires, because compaction drops whatever exists only in your
context. Record:

- Each hand fix.
- Each limit that you lowered to fit the memory, with the reason.
- Each landing without a full final review.
- Each task that you file for a follow-up or a defect, with the task that
  reported it. Say whether the filters of the run leave it out.
- Each pending task that `init` left in an earlier run because of the
  filters, with the reasons that `init` printed.
- Each task that `init` moved only because a moved task waits for it,
  with the reasons that `init` printed.
- Each task that you left out of a triage or set to noted because the
  filters keep its blocker from the run, with the reason that `triage`
  printed.
- Each artifact that a task made stale, such as a recorded demo or a
  generated file.
- Each behavior change that the user should know about.

## Report

End with a report that lists:

- The landed tasks, each with its commit. For a run with a `pr` target,
  list each landed task with the pull request that `state.json` records
  for it. Also list the pull request that the `repos` field of the task
  records for each other repository.
- The proposed tasks of a run that stops before its end, each with its
  pull request.
- The noted tasks, each with its reason and its branch. Also give the
  pull request of each noted task that `state.json` records one for.
- The tasks that the run filed for follow-ups and for defects, each with
  the task that reported it. Mark each task that the filters of the run
  left out, because it waits for a later run.
- The pending tasks that `init` left in an earlier run because of the
  filters, each with its reasons. Each of them waits for a later run.
- The tasks that `init` moved although the filters leave them out,
  because a moved task waits for each of them.
- The tasks that you left out of a triage or set to noted because the
  filters keep their blockers from the run, each with its reason. Each of
  them waits for a later run.
- The tasks that landed without a full final review.
- Each limit that you lowered to fit the memory, with the reason.
- The stale artifacts.
- The commits that the run did not push, and each rejected push.

Each `earlier run` line of `status` names a run that this batch goes on
from, because `init` moved the unfinished tasks of that run to this run.
Cover each such run in the report as well. List each landed task in the
`state.json` of that run with its commit, and report each entry of its
`notes.md` in the matching item of this list. Read those files in that run
directory, and do not copy them into the `notes.md` or the `rules.md` of
this run.
