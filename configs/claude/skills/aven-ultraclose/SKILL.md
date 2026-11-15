---
name: aven-ultraclose
description: >-
  Close every open aven task in the current project with parallel worktree
  agents and adversarial review.
argument-hint: "[concurrency] [aven list filters] [models]"
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
default is 5. When the memory of the machine cannot hold that many tasks,
lower the limit, as the Setup section says. The arguments can also name a
model for the agents of a role, such as `opus-5-5 for the reviewers`, and
the Models section says how to set it. Any other argument is an
`aven list` filter that narrows the queue, such as `--label bug`.

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

The queue is every open task that is not an epic, including each task that
anyone files during the run. Triage the new tasks each time `next` prints an
empty list, whether you run `next` or a landing runs it. After a triage that
adds tasks, run `ultraclose.py next`, so that the new tasks take the free
slots.

`ultraclose.py untriaged --project <key>` prints each task of the queue
that the run does not hold. `<key>` is the key of the aven project of the
repository. Always pass `--project`. Without it, `aven list` keeps to the
project that aven infers from the main checkout, and it covers every
project when aven infers none. Add each `aven list` filter that the
arguments hold.
The command prints a JSON list with the ref, the title, and the priority
of each task. The list is empty when the run holds the whole queue.

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

The run ends when `ultraclose.py status` counts no pending, inflight, or
approved task. `status` then prints an `ended` line. Its `setup` line then
names `init` for the setup of a later batch, so do not start a new run at
the end of a run.

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
     `init`.
   - **`setup: start a new run with init`:** do the rest of the setup.
     The line gives the reason, such as a run that has ended, or a run
     that comes from another session and another version of the skill.
     A run without a task gets this line as well, unless it records this
     session and this version of the skill. Also do the rest of the setup
     when `status` fails with `no run found`.

   New runs do not take the config, the `rules.md`, or the `notes.md` of
   an earlier run. Set each flag of `init`, such as the limit, the models,
   and the shared globs, from the arguments and from this setup. Write
   `rules.md` from this setup too. The values of an earlier run fit the
   arguments and the tasks of that run, and the version of the skill that
   created it.
2. Confirm that the main checkout is clean and on the base branch.
3. Find how the project builds, tests, and checks itself, and what its
   commit hooks run. Read CLAUDE.md, the task runner config, and
   `git log -15`. Find the build caches that those commands fill, and the
   file system that holds each cache. Some caches evict their content when
   the free space of the disk falls to a threshold, so find that threshold
   for each cache.
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
   `--force` only when the user asks for an empty run. With `--force`,
   `init` replaces a run that this session continues, and the unfinished
   tasks of that run become leftover work.

   When the `setup` line says that `init` moves unfinished tasks, the
   earlier run stopped before its end. `init` moves each pending, inflight,
   approved, and noted task of that run to the new run, and it prints
   those tasks. Each task keeps its triage row, its state, and its task
   directory, so the new run resumes its branch and its worktree. The new
   run holds each noted task, so `untriaged` does not list it. `status`
   still lists each pending task that waits for a noted task as stuck, and
   the Loop section says to note it.

   The earlier run keeps its landed and closed tasks, and the batch of the
   new run goes on from that run. So `status` names that run in an
   `earlier run` line, and the report covers it, as the Report section
   says.

   Before you run `init`, stop each Workflow that still runs one of the
   unfinished tasks. Its agents write to the task directory of the earlier
   run. After the setup, start those tasks again as the Interrupted run
   entry of the Hazards section says.

   Pass `--model <role>=<model>` for each model that the arguments name,
   as the Models section says.

   Pass `--check '<command>'` with the full check of the project, unless
   the commit hook already runs that check. Pass `--health '<command>'`
   when the check or a commit hook requires a service, such as Docker. The
   health command is a fast command that fails when the service is down,
   such as `docker version`.

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
models, the check, the health command, and the step timeout. It also prints
the free space and the floor of each watched file system.

`ultraclose.py config` changes the config and prints the result. It takes
the `--limit`, `--rounds`, `--model`, `--check`, `--health`,
`--step-timeout`, `--shared`, `--disk-floor-gb`, `--disk-floor-percent`,
and `--disk-path` flags of `init`. Its `--unshared <glob>` flag removes a
shared glob, and its `--no-disk-path <path>` flag removes a disk path and
the percent floor of that path. `--disk-floor-percent <dir>=0` removes the
percent floor of `<dir>` and keeps `<dir>` as a disk path. `--model <role>=`
removes the model of `<role>`, as the Models section says. `config` prints
the warning of `triage` for each glob that its `--shared` flag adds and
that matches no file of any task.

Every config change applies to the commands that start after it. So a
running Workflow and a queued landing keep the values that they started
with. The `next` that a landing runs reads the config when it runs, after
the landing.

A run that you continue carries the config and the `rules.md` of the tasks
that it started with. When you continue the run, fit both to the arguments
and to the open tasks that are not in the run. `ultraclose.py untriaged`
prints those tasks, as the Scope section says. Fit the run in these ways:

- If the arguments hold a number, set the limit to it. Set a lower limit
  when the memory cannot hold that many tasks, as the Setup section says.
- If the arguments name a model, set it with `ultraclose.py config`, as the
  Models section says. The run keeps each model that the arguments do not
  change.
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
the result name the agent that returned nothing. When a result names such
an agent and the run names a model for its role, either of its own or the
`default` model, check that model. Correct a wrong model with
`config --model`, then relaunch the task.

The role of the agent that returned nothing sets the status of the result,
and this table gives the relaunch that follows the correction:

| Role | Status | Relaunch |
| --- | --- | --- |
| `narrow` | `unconverged` | Run `ultraclose.py args <ref> --final` in place of the action that the Loop table gives for `unconverged` |
| `sync` | `error` | Run `ultraclose.py args <ref> --sync '<reason and failing output>'` with the reason that started the sync agent, as the `error` row of the Loop table says |
| Each other role | `error` | Follow the `error` row of the Loop table |

## Leftover work

Each run resumes only the branches and the worktrees that it created, and
those of the tasks that `init` moved to it. Earlier runs can leave a
`task/<ref>` branch or a `t-<ref>` worktree for a task that is open again.
Every run leaves the branch of each task that it notes. Runs that stop
before their end also leave the worktree of each task that they started.
That branch can sit far behind the base branch. Every landing squashes the
whole branch, so a task that starts on that branch lands the commits that
the earlier run did not land.

`ultraclose.py next` starts no task while a pending task has leftover
work, and neither does the `next` of a landing. `ultraclose.py args <ref>`
refuses a task that has leftover work. Each of these commands then prints
one line per such task. The line names the branch, the count of its commits
that the base branch lacks, and the worktree. Move the leftover work of
each task out of the way:

1. If the worktree holds uncommitted changes, commit them in the worktree
   with `--no-verify`, so that the branch keeps them.
2. Remove the worktree with `git worktree remove <path>`.
3. Count the commits that the branch holds and the base branch lacks with
   `git rev-list --count <base>..task/<ref>`. The `<base>` placeholder
   names the base branch. The count that the command printed does not
   include the commit of step 1, so use the new count.
4. If the new count is 0, delete the branch with
   `git branch -D task/<ref>`. Otherwise, rename it with
   `git branch -m task/<ref> kept/<ref>-<date>`. The date is the date of
   the cleanup, such as `kept/APP-7KQ9-20261114`. If a branch with that
   name exists, add a counter, such as `kept/APP-7KQ9-20261114-2`.
5. For each branch that you rename, add a note to the task with
   `aven note`. Name the new branch in the note. Say that the branch was
   `task/<ref>` before the rename, so that an earlier note that names
   `task/<ref>` means the kept branch and not the branch of the new
   attempt. Say that the branch holds an earlier attempt as a reference,
   and that the implementer must not merge or rebase it. The implementer
   reads the notes of the task in its task file.

Then run `next` or `args <ref>` again. After a landing that printed the
lines, run `ultraclose.py next`, because the task of the landing has
landed. Each task starts on a new branch from the base branch.

`ultraclose.py set <ref> noted` removes the clean worktree of the task.
`set <ref> closed` removes that worktree and then deletes the branch.
Neither command deletes a branch whose worktree is gone or dirty. Both
commands remove only the work of the run.

For a task with leftover work, each `set` command keeps the worktree and
the branch. When the worktree exists, the command prints a line that names
the worktree. `next` starts only pending tasks, so the leftover work of a
noted or closed task can stay. If you remove that work, follow the steps
of this section, so that the branch keeps its commits.

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

   A landing with the result `OK` runs `next`, as the Landing section says.
   In a turn that acts on such a landing, run this step only after an
   action that can let a task start. These actions are a triage that adds
   tasks, a move of leftover work as the Leftover work section says, a
   `set <ref> noted`, a `set <ref> closed`, a landing that you start, and a
   config change. The `next` of the landing ran before those
   actions, so it missed each task that they add and each slot that they
   free. Skip this step in a turn that takes none of those actions, even
   after a triage that adds no task.
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
   `defects` as the Defects section says, in the same turn as you act on its
   status. When the result gives a user message about other work as the
   reason for its status, relaunch the task as the User messages section
   says. Otherwise act on its `status` as this table says:

| Status | Action |
| --- | --- |
| `approved` | Land the task. |
| `landable` | The task reached its cap as a strict improvement with no regression. File each entry of `followups` with `aven add` and link it with `aven related add`. Then land the task. |
| `obsolete` | Add the evidence with `aven note`, set the task to `done`, and run `ultraclose.py set <ref> closed`. |
| `abandoned`, `rejected`, `unconverged` | If an `unconverged` result names the narrowing agent and the run names a model for that agent, check that model first, as the Models section says. After you correct a wrong model, relaunch the task as that section says. Otherwise add a note with the open findings and the branch name, set the task to `todo`, and run `ultraclose.py set <ref> noted`. The branch stays. Do not force the task through. |
| `error` | If the run names a model for the agent that returned nothing, check that model first, as the Models section says. Relaunch the task one time. Run `ultraclose.py args <ref>`, and launch the object that it prints from the path in its `script` field, as step 2 says. When the sync agent returned nothing, add `--sync` with the reason that started that agent, so that the relaunch syncs the task again. After a second error, note the task. |

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

Run `ultraclose.py land <ref> --next` in a background Bash call. Landings
queue on a lock, so start one as soon as a task is ready. The command merges
the base branch into the worktree and runs the check. Then it squashes the
branch into one commit on the base branch with `msg.txt` as the message,
removes the worktree, and sets the task to `done`. Each landing of a run
with a health command runs that command before the merge, and again after a
failed check or a failed commit.

`ultraclose.py land` sets `GOLANGCI_LINT_CACHE` for the check and for the
commit, so neither step reads the lint cache that all checkouts share. The
check gets the directory that the lint cache rule of the Setup section
names, inside the git directory of the worktree. Git removes that directory
together with the worktree. The commit hooks run in the main checkout, and
they get the `golangci-lint-cache` directory of the run directory.

The command prints a `RESULT` line. After an `OK` result, the `--next` flag
runs `next`, so one background call lands the task and starts the next
tasks. The list of input objects that `next` prints follows the `RESULT`
line, and the list is empty when no task starts. After a failed landing,
the flag does not run `next`.

When a landing fails, the `RESULT` line is the last line that the command
prints. The last lines of the failing output come before it. The run
directory holds the full output of each step in `logs/<ref>.<step>.log`.

Act on the result as this table says:

| Result | Action |
| --- | --- |
| `OK` | Launch one Workflow per object of the list that follows the `RESULT` line, as the Loop section says. Skip each object whose task you relaunched after the landing started. When an error follows the line instead of the list, the task landed and `next` failed. Repair the cause of the error, and run `ultraclose.py next`. |
| `CONFLICT` | The base branch moved under the task. Sync the task. |
| `CHECKFAIL`, `COMMITFAIL` | The check or a commit hook failed. Find the cause in the failing output, and act on that cause. |
| `CHECKTIMEOUT`, `COMMITTIMEOUT` | The check or the commit ran longer than the step timeout, and the landing stopped it. Find the cause in the last lines of the output, and act on that cause before you start another landing. |
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

Each landing stops its check and its commit when the step runs longer than
the step timeout of the run. The landing then ends the processes of the
step, and its result is `CHECKTIMEOUT` or `COMMITTIMEOUT`. So a hook that
hangs holds the queued landings only until the landing stops it. The
landing does not run the health command after it stops a step, so neither
result shows whether the service answers.

`CHECKTIMEOUT` and `COMMITTIMEOUT` do not show that the task must change.
The output names the log of the step, and the last lines of that log come
before the `TIMES` line. Those lines show what the step did when the
landing stopped it. Each cause has its own action:

| Cause | Action |
| --- | --- |
| The step waited for a service that did not answer, such as a Dagger call whose engine another client removed. | Confirm that the service answers, such as with the health command of the run, and repair the service when it does not. Then land again. Do not sync the task. |
| The step still did its work, such as a build that fills an empty cache. | Find why the step was slow, as this section says for the `TIMES` line. When a healthy step takes that long, raise the step timeout with `ultraclose.py config --step-timeout <seconds>`. Then land again. Do not sync the task. |
| The step ran a build or a test of the changed code that does not end. | Sync the task. |

The change might cause the wait for a service, such as with a test that
stops the service. The same task can end in a timeout result a second
time, with the end of its log at the same place. Then do not land the task
again. Sync the task with the log of the step when that log shows that the
change causes the wait. Otherwise, note the task.

After `COMMITTIMEOUT`, the landing restores the main checkout as it does
after a failed commit. A process that the landing killed can leave a lock
file of git, and the landing then cannot restore the main checkout. Its
output says so above the `TIMES` line, and it names the git command that
failed, such as `git reset -q --hard <commit>`. Repair the main checkout
before the next landing:

1. Confirm that no git process runs in the main checkout.
2. Remove the lock file that the output names.
3. Run the git command that failed again, in the main checkout.
4. Run `git clean -fd` in the main checkout.

When you fix something by hand before a landing, record the fix in
`notes.md`, because no reviewer saw it.

Each landing that runs the check or the commit prints a `TIMES` line before
the `RESULT` line, such as
`TIMES check=1m22s (median 1m24s) commit=11m52s (median 18s)`. The time of
the commit includes the commit hooks. When a commit hook changes the index,
the landing commits a second time, and the time of the commit covers both
attempts. Each median covers the earlier
landings of the run that landed a task. `ultraclose.py status` prints the
medians and the times of the last landings.

Read the `TIMES` line of each landing. When a step takes more than twice
its median, and more than a minute longer than its median, do not report
the time as normal. Find the cause before you start another landing. A
build cache that evicted its content is a common cause. `ultraclose.py
status` prints the free space of each watched file system, and the Disk
entry of the Hazards section says how to free space.

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
`ultraclose.py args <ref>` in place of the action that the Loop table gives
for the status. When the message was about the task, act on the status as
the Loop table says.

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
  skip its object. New sessions do not run `watch`, so start it again.

  Each new session first follows the `setup` line of `status`, as the
  Setup section says. After the earlier session has ended, no landing
  runs for its approved tasks. Land each of them again, as the Landing
  section says.

## Notes

Append to `notes.md` in the run directory when something happens that the
final report requires, because compaction drops whatever exists only in your
context. Record:

- Each hand fix.
- Each limit that you lowered to fit the memory, with the reason.
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
