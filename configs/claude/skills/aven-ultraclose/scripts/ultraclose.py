#!/usr/bin/env python3
"""Scheduler and landing queue for the aven-ultraclose skill.

Run every command from inside the repository. The run directory holds
state.json, rules.md, notes.md, logs/, and one directory per task. The
repository's git config records its path as ultraclose.run, so a later
session finds the run without arguments.

The state of a run records the version of the skill that created the
run, and the session of the run. The version is a hash of SKILL.md, of
close-task.js, and of this script. The session of a run is the Claude
Code session of the init command that created it, and then the session
of each later command that changes it. Claude Code names the session of
a command in the CLAUDE_CODE_SESSION_ID variable. The status command
prints the version and the session. It says when the command comes from
another version of the skill, and when the command runs in another
session.

The script decides whether a session continues the run that the
repository names, and the setup line of the status command prints the
decision. A session continues each run that has an unfinished task, with
one exception. It starts a new run when the run comes from another
session and from another version of the skill. That run holds the
config, the rules.md, and the notes.md of that version. A session also
starts a new run when the run has ended. The script cannot compare the
sessions when the run records no session, or when the command runs
outside a session. A run with a task then counts as a run of the session
of the command.

A run without a task has not started, so a new run loses nothing of it.
A session continues that run only when the run records the session of
the command and the version of the skill of the command. So a session
starts a new run in place of a run without a task that records no
session, or that another version of the skill created. A command outside
a session continues a run without a task only when the init command that
created the run ran outside a session too.

The init command follows that decision. It starts a new run in place of
a run that the session does not continue, and it refuses to replace a
run that the session continues. With --force, it replaces that run too.
Each other command that changes the run first refuses a run of another
session that its own session does not continue. Then it records its
session as the session of the run.

A new run does not take the config, the rules.md, or the notes.md of an
earlier run. When init replaces a run that has unfinished tasks, it
moves those tasks and each noted task into the new run. Each task keeps
its triage row, its state, and its directory, so the new run resumes its
branch and its worktree. A proposed task keeps its branch and has no
worktree. The earlier run keeps its landed and closed tasks. The state
of the new run names the earlier run, and each run that the state of the
earlier run names, and the status command prints
each of them. The batch of the new run goes on from those runs, so its
report covers their landed tasks and their notes.md. Init does not move
the tasks while a landing of the earlier run runs. With --force, init
starts an empty run and leaves the tasks in the earlier run.

A noted task is still open in aven. So the untriaged command of a new
run lists each noted task that the new run does not hold, and a triage
starts that task again. The new run holds each noted task that init
moves, so a pending task that waits for it keeps waiting, and the status
command still prints that pending task as stuck.

The next and args commands write a copy of close-task.js into the
directory of each task that they start, and so does a landing that runs
next. The meta of the copy names its task. The name holds the ref, and
the description holds the title, so the Workflow list shows which task
each Workflow closes. The script field of each Workflow input holds the
path of the copy. Pass that path to the Workflow tool as scriptPath. The
tool refuses the script in the skill directory after Nix store
optimisation hard-links it.

The directory of a task holds its task file, its commit message, its
review history, its copy of close-task.js, and the summary and the notes
of its change. The agents of close-task.js write the summary to
summary.txt and the notes to notes.txt. The args command reads both files
into the resume field of its output, so a relaunched task starts with the
summary and the notes of its earlier launch. The next and args commands
remove both files when they start a task at the implementer, so a
relaunch never carries the text of an attempt that the implementer
replaced.

A run ends when no task is pending, inflight, approved, or proposed.
Each run takes a directory of its own. When the directory of init --run
holds the state.json or the tasks directory of an earlier run, the new
run takes the next free directory beside it, such as DIR-2. The init
command prints the directory that the run takes. With --force, init
takes the directory of --run when it holds the tasks directory and no
state.json. The new run then treats the branch and the worktree of each
task that has a directory there as its own work. Init takes no other
such directory, even with --force.

A run resumes only the task branches and the task worktrees that it
created. An earlier run leaves the branch of each task that it noted, and
a run that stopped before its end leaves the worktree of each task that
it started. Such a branch can sit far behind the base branch. The next
command starts no task while a pending task has a branch or a worktree
that the run did not create, and neither does a landing that runs next.
The args command refuses such a task. Each of these commands prints one
line per such task. The line names the branch, the count of its commits
that the base branch lacks, and the worktree. Commit the uncommitted
changes of the worktree, and remove the worktree. Then count the commits
of the branch again, because the printed count does not include that
commit. Delete the branch when the new count is 0, and rename it
otherwise. Then run next or args again, and run next after a landing.
The Leftover work section of SKILL.md holds these steps. Each command
that starts a task creates the directory of the task first, so the
branch and the worktree of a task without a directory come from an
earlier run. The init command moves the directory of each task that a
new run takes from the run that it replaces, so the new run resumes the
work of those tasks as its own.

An earlier run also keeps the branch of each task that a landing
proposed as a pull request, when init does not move that task to the new
run, such as with --force. That branch has another name than task/REF,
and its pull request can still be open. So init records the branch and
the pull request of each such task in the left field of the new state,
and it prints one line per task. The record also keeps each branch that
the earlier state records there and that still exists. The next and args
commands count a recorded branch as leftover work of its task, and the
line names the pull request, or says that the earlier run recorded none.
Before you move such a branch out of the way, close its open pull
request, because a new attempt at the task opens a pull request of its
own.

The set command removes the clean worktree of a task that it sets to
noted or closed. When it removes the worktree of a closed task, it also
deletes the branch. It removes only the work of the run. So it keeps the
branch and the worktree of a task without a directory, and it prints a
line that names a kept worktree.

  init --run DIR [--target TARGET] [--limit N] [--rounds N]
       [--model ROLE=MODEL]... [--check CMD] [--health CMD]
       [--step-timeout SECONDS] [--shared GLOB]... [--disk-floor-gb N]
       [--disk-floor-percent PATH=N]... [--disk-path PATH]... [--force]
  config [--limit N] [--rounds N] [--model ROLE=MODEL]... [--check CMD]
       [--health CMD] [--step-timeout SECONDS] [--shared GLOB]...
       [--unshared GLOB]... [--disk-floor-gb N]
       [--disk-floor-percent PATH=N]... [--disk-path PATH]...
       [--no-disk-path PATH]...
                         change the config of the run, then print it
  triage FILE            add or update tasks from a JSON list
  untriaged [FILTER]...  print the open aven tasks that the run does not hold
  next                   start one task per free slot, print each Workflow input
  args REF [--round N] [--final] [--sync REASON]
                         print the Workflow input that relaunches REF
  set REF STATE [--flag TEXT]
  land REF [--next]      land REF on the target of the run, one landing at a time
  opened REF URL         record the push and the pull request of the proposed
                         task REF
  merged REF [--force]   record that the pull request of REF merged
  status
  watch [--interval SECONDS]
                         exit when free disk space runs low or the run ends

The --target flag of init names where the tasks of the run land, and the
default is main. A branch name makes that branch the base branch of the
run. Each task branch starts from the base branch, and each landing
squashes its task branch into one commit on the base branch. Every such
landing commits in the main checkout. So init refuses a branch that the
main checkout does not have checked out.

The target pr makes each landing propose its task as a pull request
against main. The target pr:BRANCH names another base branch for the
pull requests. Each task branch still starts from the base branch. A
landing of such a run leaves the main checkout alone. It records no
commit on the base branch, so it runs no commit hook. It merges the base
branch into the worktree and runs the check. When the base branch moved
after the task started, that merge adds a merge commit to the task
branch. Then the landing renames the task branch to TYPE/SLUG, removes
the worktree, and sets the task to proposed. TYPE is the Conventional
Commits type of the subject of msg.txt, or feat for a subject without
such a type. SLUG holds the words of the rest of the subject. The
landing prints the result PR with the branch and the base branch. The
script contacts no remote, so the caller pushes the branch and opens the
pull request. The opened command then records the push and the address
of the pull request. A landing after a relaunch can add commits to the
branch of a pull request that the task records, so the caller runs the
opened command again after the push. Until then, the status command
names the branch that waits for its push.

The push publishes each commit of the task branch with its message. So
such a landing fails with the result LOGHASREF when a commit message of
the branch holds a task ref, and it prints the ID and the subject of each
such commit. The merge commit of a landing names only the base branch,
because the name of the task branch holds the task ref.

Git keeps no branch below the name of another branch. So a local branch
or a remote branch named TYPE blocks the name TYPE/SLUG, and the landing
fails with the result BRANCHTAKEN before the check. A landing also fails
with BRANCHTAKEN when git cannot rename the task branch after the check.

A proposed task waits for its pull request to merge. It keeps its file
locks, so no task that locks one of the same files starts. Each task
that comes after it keeps waiting, so no task starts from a base branch
that lacks a change that it builds on. A run does not end while it holds
a proposed task, and the init command moves each proposed task to a new
run with the other unfinished tasks. Init moves the tasks only to a run
with the same base branch, because those tasks start from that branch.
It also moves them only when the landings of both runs propose pull
requests, or the landings of both runs commit. A new run whose landings
commit leaves the pull request of each proposed task open beside the
commit.

The land command refuses a proposed task, because the landing that
proposed the task removed its worktree. The args command starts the task
again on its branch, such as for a pull request that has a conflict with
the base branch. The next landing of the task keeps the name of the
branch, and its RESULT line gives the pull request that the task
records.

After the pull request merged and the base branch holds the merge, the
merged command deletes the task branch, sets the aven task to done, and
sets the task to landed. It first merges the task branch in memory into
the base branch, and into each commit that the base branch gained after
the fork of the task branch. It refuses when every such merge has a
conflict or changes a file, because the base branch then lacks the
change. One merge without a change is enough, because a later commit of
the base branch can change the lines of the change. With --force, it
skips that test, such as for a pull request that gained commits after
the push. It also refuses when git cannot delete the task branch, such
as a branch that a worktree has checked out. Each refusal changes
nothing.

Each --model ROLE=MODEL flag of init and config names the model of the
agents of one role of close-task.js. The roles are implementer, sync,
reviewer, fixer, and narrow. The reviewer and the fixer of the final
cycle take the models of the reviewer role and the fixer role. The
narrow role covers the agent that narrows the claims of a landable
change.

A flag with the role default names the model of each role that has no
model of its own. A flag with an empty MODEL removes the model of ROLE.
When two flags of one command name the same role, the last one applies.
The commands do not check that the Workflow tool can run a model. The
status command prints the models of the run.

The next and args commands print the model of each role in the models
field of each Workflow input, and so does a landing that runs next.
close-task.js starts each agent on the model of its role, so a relaunch
runs on the models of the run. The field leaves out each role that has
neither a model of its own nor a default. The agents of such a role
follow the session model, and so does every agent of a run without a
--model flag.

The triage command asks aven for the dependencies of each task that it
loads, and adds each open dependency to the after list of the task. So
write into the after list of a row only the order that aven does not
record.

The untriaged command prints one triage row per open aven task that the
run does not hold, as a JSON list. The list leaves out every epic. Each
row holds the ref, the title, and the priority of its task. Add the files
of the task to the row before you load it with triage.

Each FILTER of untriaged is an argument of aven list, such as
--project app or --label bug. Without --project, aven list keeps to the
project that aven infers from the main checkout, and it covers every
project when aven infers none. With --limit N, untriaged prints at most
N rows, and the tasks that the run holds do not count toward N.

The land command prints the failing output of a failed landing. The last
line that a failed landing prints is the RESULT line. With --next, a
landing with the result OK or PR runs the next command after it prints
the RESULT line. The list of Workflow inputs that next prints follows
the RESULT line, and the list is empty when no task starts. After a
failed landing, --next does not run next. When next fails after the
landing, its error follows the RESULT line, and the command exits with a
failure status.

The land command sets GOLANGCI_LINT_CACHE for the check and for the
commit, so that neither step reads the golangci-lint cache that every
checkout shares. golangci-lint 2.12 reports the cached issues of another
checkout of the same module, under the paths of that checkout, and a
landing removes the worktree that those paths name. The check gets the
golangci-lint-cache directory inside the git directory of the task
worktree, which git removes together with the worktree. The commit runs
in the main checkout, and it gets the golangci-lint-cache directory of
the run directory. A check that sets the variable itself keeps its own
value.

A landing records how long its check and its commit took. The time of the
commit includes the commit hooks. When the landing commits a second time
because the first commit did not record the staged tree, the time of the
commit covers both attempts. The land command prints both times in a
TIMES line before the RESULT line. Each time comes with the median of the
earlier landings with the result OK or PR. The status command prints the
medians of the run and the times of its last landings, so a slow check or
a slow hook shows beside the usual time.

The watch command measures the file system of each disk path of the run.
A run starts with the repository and the home directory of the user as
its disk paths. Each --disk-path flag adds a path, such as the directory
of a build cache that another file system holds. A file system is low
when its free space is less than --disk-floor-gb, or less than its
percent floor.

Each --disk-floor-percent PATH=N flag with an N of more than 0 gives the
file system that holds PATH a floor of N percent of its size, and adds
PATH to the disk paths. The other file systems keep their floors. Set N
to more than the share of the disk that a build cache on that file system
keeps free, so that the watch command exits before that cache evicts its
content. When two disk paths of one file system have a percent floor,
the higher floor applies. A flag with an N of 0 removes the percent floor
of PATH and adds no disk path. The --no-disk-path flag of config removes
both the path and its percent floor.

The watch command prints one DISK line per low file system and exits.
It prints an ENDED line and exits when the run ends. It prints the DISK
lines first when both apply. The status command prints the free space
and the floor of each file system. The line of a file system whose
percent floor is more than --disk-floor-gb names the flag that sets it.

The health command of init --health and config --health is a fast
command that fails when a service that the check or a commit hook
requires is down. A landing runs it in the main checkout before the
merge, and again after a failed check or a failed commit. A failed health
command ends the landing with the result HEALTHFAIL. The failure does not
show that the task needs a change, so land it again after the service
answers. A check or a commit hook that stops the service also ends each
landing with HEALTHFAIL, and the output then names the log of the failed
step. When a task ends in HEALTHFAIL after the check or the commit a
second time, read that log instead of landing the task again.

A landing stops its check, and each attempt of its commit, when the step
runs longer than the step timeout of the run. The --step-timeout flag of
init and config sets the step timeout in seconds, and the default is
3600. Set it to more than the time that the slowest step of a healthy
landing takes, so that a landing stops only a step that hangs. A hook
that hangs then holds the landing lock only until the landing stops it,
and the queued landings run after it.

Each step of a landing leads a process group of its own. To stop a step,
the landing sends SIGINT to that process group, as an interrupt from the
keyboard does. git then removes its lock files, and lefthook ends the
process groups that it started for its hook commands. The processes of
the group that still run GRACE seconds later get SIGTERM, and the
processes that run GRACE seconds after SIGTERM get SIGKILL. A process
that left the group keeps running when no process of the group ends it.

A landing that stopped a step ends with the result CHECKTIMEOUT or
COMMITTIMEOUT, and its output names the step timeout and the log of the
step. The landing does not run the health command after it stops a step,
so neither result shows whether the service of the health command
answers.

After COMMITTIMEOUT, the landing restores the main checkout as it does
after a failed commit. A killed process can leave a lock file of git
there, and the landing then cannot restore the main checkout. Its output
says so above the RESULT line, and it names the git command that failed.

When SIGINT, SIGTERM, or SIGHUP ends the land command during a step, the
command stops that step first. When one of these signals comes while the
command stops a step, the signal ends the command after the step stops.
Between the steps, SIGTERM and SIGHUP end the command at once, and the
git command that the land command runs keeps running until it ends. So
the git reset that restores the main checkout after a failed or stopped
commit finishes its work. SIGKILL ends the command at once, and the step
then keeps running. A signal that the caller of the land command ignores
stays ignored.

A shared glob names files that two running tasks may both edit. The
scheduler matches each glob against "/" plus the path of a file relative
to the repository, and "*" also matches "/". So /README.md matches the
README.md at the root of the repository, */README.md matches the
README.md of every directory, and README.md matches no file. The triage
command prints a warning for each glob that a --shared flag added and
that matches no file of any task. The config command prints the same
warning for each such glob that its --shared flag adds.
"""

import argparse
import fcntl
import fnmatch
import hashlib
import json
import math
import os
import re
import shutil
import signal
import statistics
import subprocess
import sys
import time
from contextlib import contextmanager, suppress
from pathlib import Path

STATES = ("pending", "inflight", "approved", "proposed", "landed", "noted", "closed")
SETTLED = ("landed", "closed")
UNFINISHED = ("pending", "inflight", "approved", "proposed")
# The states of a task that hold its file locks. An inflight task changes
# its files. An approved task waits for its landing, and a proposed task
# waits for its pull request to merge. The base branch lacks the change
# of each of them.
LOCKING = ("inflight", "approved", "proposed")
PRIORITY = {"urgent": 0, "high": 1, "medium": 2, "low": 3, "none": 4}
# The aven statuses of an open task.
AVEN_OPEN = ("inbox", "backlog", "todo", "active")
# Files that match a shared glob hold no lock, so two running tasks may
# both edit them. The scheduler matches each glob against "/" plus the
# path relative to the repository. The run config starts with these
# globs plus the glob of each init --shared flag, and the config command
# adds and removes globs.
DEFAULT_SHARED = [
    "*_test.*",
    "*/test_*",
    "*.test.*",
    "*.spec.*",
    "*/tests/*",
    "*/testdata/*",
    "*/__tests__/*",
]
# How a shared glob matches a file. The help of each --shared flag prints
# the rule, and so does the warning about a glob that matches no file.
GLOB_RULE = (
    'A shared glob matches "/" plus the path of a file relative to the'
    ' repository, and "*" also matches "/". So /README.md matches the'
    " README.md at the root of the repository, */README.md matches the"
    " README.md of every directory, and README.md matches no file."
)
# The Workflow script, which sits beside this file. The directory of each
# task keeps a copy of it under the same name.
WORKFLOW = "close-task.js"
# The files of a task directory that hold the summary and the notes of the
# change, by the key of the resume field that carries each one. The
# prompts of close-task.js name the same files.
STORED = {"summary": "summary.txt", "notes": "notes.txt"}
# The roles of the agents of close-task.js. The run config can name one
# model per role, and close-task.js reads the model of each agent under
# the name of its role.
ROLES = ("implementer", "sync", "reviewer", "fixer", "narrow")
# The role of a --model flag that names the model of each role without a
# model of its own.
DEFAULT_ROLE = "default"
# The directory that holds a golangci-lint cache of one checkout. A landing
# keeps one inside the git directory of the task worktree and one inside
# the run directory.
LINT_CACHE = "golangci-lint-cache"
# The steps of a landing whose time the run records, in the order in which
# a landing runs them. The commit step runs the commit hooks.
TIMED = ("check", "commit")
# The step timeout that a run starts with, in seconds. A landing stops its
# check, and each attempt of its commit, when the step runs longer. The
# --step-timeout flag gives a run whose healthy steps take longer a higher
# step timeout.
STEP_TIMEOUT = 3600
# How long a landing waits for the processes of a step to end after each
# signal that it sends them, in seconds.
GRACE = 5
# The signals that end the land command. The command stops the step that
# runs before it ends.
ENDS = (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)
# The results of a landing that ran each of its steps. The medians of the
# timed steps count only the landings with these results.
PASSED = ("OK", "PR")
# How many landings the status command prints with their times.
RECENT = 5
# What the status command and the watch command print for a run that has
# ended.
ENDED = "no task is pending, inflight, approved, or proposed"
# The target that a run takes when init names none.
TARGET = "main"
# The target of a run whose landings propose pull requests. The target
# PR:BRANCH names the base branch of the pull requests, and the target PR
# alone stands for PR:main.
PR = "pr"
# The Conventional Commits types that can open the name of a pull request
# branch, as TYPE/SLUG. On the hosts of this repository, hook-router
# allows the push of a branch with such a name without a prompt. It
# allows a draft pull request from such a branch as well. home/claude.nix
# holds the same types in conventionalBranchTypes.
BRANCH_TYPES = (
    "build",
    "chore",
    "ci",
    "docs",
    "feat",
    "fix",
    "perf",
    "refactor",
    "revert",
    "style",
    "test",
)
# The type of a pull request branch whose commit message names no type of
# BRANCH_TYPES.
BRANCH_TYPE = "feat"
# How many characters the slug of a pull request branch holds at most.
SLUG = 50
# The key of the git config of the repository that names the run directory.
KEY = "ultraclose.run"
# The environment variable in which Claude Code names the session of each
# command that it runs.
SESSION = "CLAUDE_CODE_SESSION_ID"


def die(msg):
    sys.exit(f"ultraclose: {msg}")


def sh(*cmd, cwd, check=True):
    p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, check=False)
    if check and p.returncode:
        die(f"{' '.join(cmd)} failed in {cwd}:\n{p.stdout}{p.stderr}")
    return p


def out(*cmd, cwd):
    return sh(*cmd, cwd=cwd).stdout.strip()


def main_checkout():
    p = subprocess.run(
        ["git", "worktree", "list", "--porcelain"],
        capture_output=True,
        text=True,
        check=False,
    )
    if p.returncode:
        die("run this from inside a git repository")
    return p.stdout.splitlines()[0].split(" ", 1)[1]


def named_run(repo):
    """The run that the git config of the repository names.

    Return the path of the run directory, the state of the run, and what
    keeps a command from the run. The path is empty when the config
    names no directory. The state is None when the config names no
    directory, or a directory whose state file the command cannot read,
    such as a directory that is gone. The last value then names the
    cause.
    """
    path = sh("git", "config", "--get", KEY, cwd=repo, check=False).stdout.strip()
    if not path:
        return "", None, f"the git config of the repository does not set {KEY}"
    file = Path(path, "state.json")
    try:
        st = json.loads(file.read_text())
        if not isinstance(st, dict) or not {"config", "tasks"} <= st.keys():
            raise ValueError("the file holds no config or no tasks")
    except (FileNotFoundError, NotADirectoryError):
        cause = f"{KEY} in the git config names {path}, which holds no state.json"
        return path, None, cause
    except (OSError, ValueError) as e:
        cause = f"{KEY} in the git config names {path}, and the command cannot"
        return path, None, f"{cause} read {file} ({e})"
    return path, st, ""


def current(repo):
    """The directory and the state of the run that the repository names.

    The command ends when the repository names no run that it can read.
    """
    path, st, cause = named_run(repo)
    if st is None:
        die(f"no run found for this repository, because {cause}. Start a run with init")
    return Path(path), st


def run_dir(repo):
    return current(repo)[0]


def skill_version():
    """The version of the skill that holds this script.

    The version is a hash of SKILL.md, of close-task.js, and of this
    script. A run records the version that created it, so a later command
    can tell that the skill has changed.
    """
    here = Path(__file__).absolute()
    digest = hashlib.sha256()
    for file in (here.parent.parent / "SKILL.md", here.with_name(WORKFLOW), here):
        # A copy of the scripts that has no SKILL.md still has a version.
        text = file.read_bytes() if file.is_file() else b""
        digest.update(hashlib.sha256(text).digest())
    return digest.hexdigest()[:12]


def session():
    """The ID of the Claude Code session that runs this command.

    The ID is empty for a command that runs outside a session.
    """
    return os.environ.get(SESSION, "")


def foreign(st):
    """Whether a run comes from another session than the session of this command.

    The session of a run is the session of the init command that created
    it, and then the session of each later command that changes it. The
    script cannot compare the two sessions when the run records none, or
    when this command runs outside a session. The run then counts as a
    run of the session of this command.
    """
    mine, theirs = session(), st.get("session")
    return bool(mine and theirs and mine != theirs)


def new_run_reason(st):
    """Why the session of this command starts a new run in place of a run.

    The result is empty when the session continues the run. A session
    continues each run that has an unfinished task, with one exception.
    It does not continue a run that comes from another session and from
    another version of the skill. That run holds the config, the
    rules.md, and the notes.md of that version.

    A run without a task has not started, so a new run loses nothing of
    it. The session continues that run only when the run records the
    session of this command and the version of the skill of this
    command.
    """
    tasks = st["tasks"]
    other = foreign(st)
    if not tasks:
        if other:
            return "this run holds no task, and another session created it"
        if st.get("skill") != skill_version():
            return "this run holds no task, and another version of the skill created it"
        # The run and the command differ in their sessions, but one of
        # them names no session. So foreign() counts the run as a run of
        # this session.
        if (st.get("session") or "") != session():
            if not session():
                return "this run holds no task, and this command runs outside a session"
            return "this run holds no task, and it records no session"
        return ""
    if ended(tasks):
        return "this run has ended"
    if other and st.get("skill") != skill_version():
        return "this run comes from another session and another version of the skill"
    return ""


def setup_text(st):
    """What the status command prints about the decision of new_run_reason()."""
    why = new_run_reason(st)
    if not why:
        # The status command prints its ended line for a run without a
        # task, so the text says why the session continues that run.
        unstarted = "" if st["tasks"] else ", which holds no task yet"
        return f"continue this run{unstarted}"
    text = f"start a new run with init, because {why}"
    unfinished, noted = moving(st["tasks"])
    if unfinished:
        text += f". init moves its {counted(len(unfinished), 'unfinished task')}"
        if noted:
            text += f" and its {counted(len(noted), 'noted task')}"
        text += " to the new run"
    return text


def counted(n, noun):
    """The number n and the noun, which takes the plural s unless n is 1."""
    return f"{n} {noun}" if n == 1 else f"{n} {noun}s"


def moving(tasks):
    """The tasks that a new run takes from the run that it replaces.

    Return the refs of the unfinished tasks and the refs of the noted
    tasks. The new run takes no task from a run that has ended, so the
    function returns no ref for such a run.

    A noted task is still open in aven. So the untriaged command of the
    new run lists each noted task that the new run does not hold, and a
    triage starts that task again. A pending task that waits for a noted
    task that the new run does not hold also starts, because next waits
    only for the tasks that the run holds.
    """
    unfinished = [r for r, t in tasks.items() if t["state"] in UNFINISHED]
    if not unfinished:
        return [], []
    noted = [r for r, t in tasks.items() if t["state"] == "noted"]
    return unfinished, noted


def origin_texts(st):
    """What the status command prints about the skill version and the session of a run.

    The first text says when the command comes from another version of
    the skill than the version that created the run. The second text
    says when the command runs in another session than the session of
    the run.
    """
    version, mine = skill_version(), session()
    made, theirs = st.get("skill"), st.get("session")
    if not made:
        skill = f"not recorded, and this command comes from version {version}"
    elif made != version:
        skill = f"{made}, and this command comes from another version, {version}"
    else:
        skill = made
    if not theirs:
        ran = "not recorded"
    elif not mine:
        ran = f"{theirs}, and this command runs outside a session"
    elif theirs != mine:
        ran = f"{theirs}, and this command runs in another session, {mine}"
    else:
        ran = theirs
    return skill, ran


def earlier_texts(st):
    """What the status command prints about each run that a run replaces.

    Init names those runs in the new state, as take_over() says. The
    batch of the run goes on from each of them, so the final report also
    covers the landed tasks and the notes.md of each of them.
    """
    texts = []
    for path in st.get("replaces", []):
        try:
            tasks = json.loads(Path(path, "state.json").read_text())["tasks"]
            landed = sum(1 for t in tasks.values() if t["state"] == "landed")
        except (OSError, ValueError, LookupError, TypeError, AttributeError) as e:
            texts.append(f"{path}, whose state.json this command cannot read ({e})")
        else:
            texts.append(f"{path}, which keeps {counted(landed, 'landed task')}")
    return texts


@contextmanager
def locked(path):
    with open(path, "w") as f:
        fcntl.flock(f, fcntl.LOCK_EX)
        yield


@contextmanager
def edited(run):
    """Load state.json under a lock and save it when the block ends."""
    with locked(run / "state.lock"):
        st = json.loads((run / "state.json").read_text())
        yield st
        tmp = run / "state.json.tmp"
        tmp.write_text(json.dumps(st, indent=1, sort_keys=True) + "\n")
        tmp.replace(run / "state.json")


@contextmanager
def state(run):
    """Load the state of a run for a command that changes the run.

    The function loads state.json under a lock and saves it when the
    block ends. It ends the command when the run comes from another
    session and the session of this command does not continue the run,
    as new_run_reason() says. Otherwise it records the session of this
    command as the session of the run.
    """
    with edited(run) as st:
        if foreign(st) and (why := new_run_reason(st)):
            die(
                f"this session does not continue the run at {run}, because {why}."
                " Start a new run with init, as the setup line of status says"
            )
        if mine := session():
            st["session"] = mine
        yield st


def worktree(cfg, ref):
    return f"{cfg['worktrees']}/t-{ref}"


def task_branch(ref):
    """The branch that a task starts on."""
    return f"task/{ref}"


def branch(task, ref):
    """The branch of a task of the run.

    A landing that proposes a pull request renames the branch, and the
    task then records the name.
    """
    return task.get("branch") or task_branch(ref)


def base_of(target):
    """The base branch of a target, and whether its landings propose pull requests."""
    kind, _, named = target.partition(":")
    if kind == PR:
        return named or TARGET, True
    return target, False


def proposes(cfg):
    """Whether each landing of a run proposes a pull request.

    A run can continue from a state file that has no target key. Each
    landing of that run commits on the base branch.
    """
    return "target" in cfg and base_of(cfg["target"])[1]


def move_refusal(cfg, target):
    """Why init keeps the unfinished tasks of a run from a run with the target.

    Each unfinished task starts from the base branch of its run, and each
    proposed task has an open pull request. So init moves the tasks only
    to a run with the same base branch. It also moves them only when the
    landings of both runs propose pull requests, or the landings of both
    runs commit. The result is None for such a target.
    """
    was, (base, pr) = cfg["base"], base_of(target)
    if was != base:
        return (
            f"each of them starts from its base branch {was}. The base branch"
            f" of the target {target} is {base}, and the landing of such a task"
            f" carries the commits of {was} to {base}"
        )
    if proposes(cfg) and not pr:
        return (
            "each landing of that run proposes its task as a pull request. Each"
            f" landing of the target {target} commits its task on {base} instead,"
            " so the pull request of a proposed task stays open beside the commit"
        )
    if pr and not proposes(cfg):
        return (
            f"each landing of that run commits its task on {was}. Each landing of"
            f" the target {target} proposes its task as a pull request instead"
        )
    return None


def target_text(cfg):
    """What the status command prints about the target of a run."""
    base = cfg["base"]
    if proposes(cfg):
        return f"{cfg['target']}, with a pull request against {base} for each task"
    return base


def branch_exists(repo, name):
    """Whether the repository has a local branch with the given name."""
    head = f"refs/heads/{name}"
    found = sh("git", "rev-parse", "-q", "--verify", head, cwd=repo, check=False)
    return not found.returncode


def known_branches(repo):
    """The names of the local branches and of the remote branches of a repository.

    The name of a remote branch leaves out the name of its remote.
    """
    taken = set()
    # The name of a remote branch follows the name of its remote.
    for refs, strip in (("refs/heads", 2), ("refs/remotes", 3)):
        names = f"--format=%(refname:lstrip={strip})"
        taken |= set(out("git", "for-each-ref", names, refs, cwd=repo).splitlines())
    return taken


def above(name, taken):
    """The taken name that a branch name sits below, or None.

    Git keeps no branch below the name of another branch, so a branch
    named feat blocks the branch feat/x.
    """
    parts = name.split("/")
    leading = ("/".join(parts[:i]) for i in range(1, len(parts)))
    return next((p for p in leading if p in taken), None)


def pr_branch(message, taken):
    """The name of the branch that carries a change as a pull request.

    The name is TYPE/SLUG. TYPE is the Conventional Commits type of the
    subject of the commit message when BRANCH_TYPES holds it, and
    BRANCH_TYPE otherwise. SLUG holds the words of the rest of the
    subject in lowercase, with a hyphen between two words. It holds SLUG
    characters at most and ends with a whole word.

    No name of the taken names equals the name or sits below it. A
    counter at the end of the slug, starting at 2, makes a taken name
    free. A taken name TYPE blocks every name of that type, because git
    keeps no branch below the name of another branch. No counter frees a
    name then, so the function returns the name without a counter, and
    above() names the block.
    """
    subject = next((line for line in message.splitlines() if line.strip()), "")
    found = re.match(r"\s*(\w+)(?:\([^)]*\))?!?:(.*)", subject)
    if found and found.group(1) in BRANCH_TYPES:
        kind, summary = found.groups()
    else:
        kind, summary = BRANCH_TYPE, subject
    slug = "-".join(re.findall(r"[a-z0-9]+", summary.lower())) or "change"
    if len(slug) > SLUG:
        slug = slug[: SLUG + 1].rpartition("-")[0] or slug[:SLUG]
    name, n = f"{kind}/{slug}", 1
    if above(name, taken):
        return name
    while any(t == name or t.startswith(f"{name}/") for t in taken):
        n += 1
        name = f"{kind}/{slug}-{n}"
    return name


def holds(repo, base, name):
    """Whether the base branch holds the change of a branch.

    A commit holds the change when a merge of the branch into the commit,
    in memory, has no conflict and changes no file of the commit. A pull
    request can merge with a merge commit, as one squashed commit, or as
    rebased commits, and the test passes for each of them.

    The base branch holds the change when its last commit holds it, or
    when one of the commits that the base branch gained after the fork of
    the branch holds it. The function tests those commits on the first
    parent line of the base branch, newest first. A later commit of the
    base branch can change the lines of the change, and the merge into
    the last commit then has a conflict.
    """
    fork = out("git", "merge-base", base, name, cwd=repo)
    log = ("git", "log", "--first-parent", "--format=%H %T")
    # A base branch that has not moved since the fork gains no commit, so
    # the function tests its last commit.
    commits = out(*log, f"{fork}..{base}", cwd=repo) or out(*log, "-1", base, cwd=repo)
    for line in commits.splitlines():
        commit, tree = line.split()
        p = sh("git", "merge-tree", "--write-tree", commit, name, cwd=repo, check=False)
        # git exits with 1 for a merge that has a conflict.
        if p.returncode not in (0, 1):
            die(
                f"git merge-tree --write-tree {commit} {name} failed:\n"
                f"{p.stdout}{p.stderr}"
            )
        if not p.returncode and p.stdout.split()[:1] == [tree]:
            return True
    return False


def matches(glob, file):
    """Whether a shared glob matches a file, by the rule in GLOB_RULE."""
    return fnmatch.fnmatch("/" + file, glob)


def locks(cfg, task):
    return {
        f for f in task["files"] if not any(matches(g, f) for g in cfg["shared"])
    }


def unmatched(st):
    """The globs that a --shared flag added and that match no file of any task.

    Such a glob shares no file, so two tasks that edit one file still wait
    for each other. The result leaves out the default globs, because most
    runs hold no file for some of them. A run without a task has no file
    to match, so the result is empty.
    """
    cfg, tasks = st["config"], st["tasks"]
    if not tasks:
        return []
    files = {f for t in tasks.values() for f in t["files"]}
    return [
        g
        for g in cfg["shared"]
        if g not in DEFAULT_SHARED and not any(matches(g, f) for f in files)
    ]


def warn_unmatched(globs):
    """Print a warning for each shared glob that matches no file."""
    for g in globs:
        print(
            f"warning: the shared glob {g} matches no file of any task",
            file=sys.stderr,
        )
    if globs:
        print(
            f"{GLOB_RULE} To replace a glob, run config --unshared OLD --shared NEW.",
            file=sys.stderr,
        )


def blockers(st, task):
    """The tasks that must settle before this task starts."""
    tasks = st["tasks"]
    return [a for a in task["after"] if a in tasks and tasks[a]["state"] not in SETTLED]


def ended(tasks):
    """A run ends when it holds no unfinished task."""
    return not any(t["state"] in UNFINISHED for t in tasks.values())


def pick(st):
    cfg, tasks = st["config"], st["tasks"]
    busy = [r for r, t in tasks.items() if t["state"] in LOCKING]
    free = cfg["limit"] - sum(1 for r in busy if tasks[r]["state"] == "inflight")
    held = set().union(*(locks(cfg, tasks[r]) for r in busy)) if busy else set()
    last_busy = any(tasks[r]["last"] for r in busy)
    others_open = any(
        not t["last"] and t["state"] in UNFINISHED for t in tasks.values()
    )
    pending = sorted(
        (r for r, t in tasks.items() if t["state"] == "pending"),
        key=lambda r: (tasks[r]["last"], PRIORITY.get(tasks[r]["priority"], 9), r),
    )
    chosen = []
    for ref in pending:
        t = tasks[ref]
        if len(chosen) >= free:
            break
        if blockers(st, t):
            continue
        # A task marked last waits for every other task and runs alone.
        if t["last"] and (others_open or last_busy):
            continue
        mine = locks(cfg, t)
        if mine & held:
            continue
        held |= mine
        last_busy = last_busy or t["last"]
        chosen.append(ref)
    return chosen


def workflow_args(st, run, ref, resume=None):
    cfg, t = st["config"], st["tasks"][ref]
    task = {
        "ref": ref,
        "title": t["title"],
        "wt": worktree(cfg, ref),
        "branch": branch(t, ref),
        "dir": str(run / "tasks" / ref),
    }
    if resume:
        task["resume"] = resume
    return {
        "run": str(run),
        "repo": cfg["repo"],
        "base": cfg["base"],
        "pr": proposes(cfg),
        "rounds": cfg["rounds"],
        "models": models(cfg),
        "script": str(script_path(run, ref)),
        "task": task,
    }


def models(cfg):
    """The model of each role of close-task.js that has one.

    A role without a model of its own takes the default model of the run.
    The result leaves out each role that then has no model, and the
    agents of that role follow the session model. A run can continue from
    a state file that has no models key. That run names no model.
    """
    held = cfg.get("models", {})
    found = {role: held.get(role) or held.get(DEFAULT_ROLE) for role in ROLES}
    return {role: model for role, model in found.items() if model}


def role_models(held, flags):
    """The model of each role after the --model flags.

    A flag with an empty model removes the model of its role.
    """
    held = dict(held)
    for role, model in flags:
        if model:
            held[role] = model
        else:
            held.pop(role, None)
    return held


def stored(run, ref):
    """The summary and the notes that the agents of a task stored.

    The Workflow script cannot read a file, so the args command passes
    both to a relaunch. A value is empty when no agent wrote its file.
    """
    d = run / "tasks" / ref
    return {
        key: (d / name).read_text(errors="replace").strip()
        if (d / name).is_file()
        else ""
        for key, name in STORED.items()
    }


def unstore(run, ref):
    """Remove the summary and the notes that the agents of a task stored.

    A launch at the implementer starts with no stored text. The text of
    an earlier attempt describes work that the new implementer replaces,
    so a later relaunch must not carry it.
    """
    d = run / "tasks" / ref
    for name in STORED.values():
        (d / name).unlink(missing_ok=True)


def script_path(run, ref):
    """The copy of close-task.js that the Workflow of a task loads."""
    return run / "tasks" / ref / WORKFLOW


def task_script(ref, title):
    """The text of close-task.js with the meta of one task.

    The Workflow list shows the name and the description of the meta. The
    Workflow tool reads the meta as a literal, so the script cannot build
    either value from its input. The name line of the result holds the
    ref, and the description line holds the title. Each value is a JSON
    string, which JavaScript reads as a string literal with the same
    value. The string escapes each control character, because the tool
    refuses a script file that holds one. The tool also refuses an empty
    description, so a task without a title keeps the description of
    close-task.js.
    """
    source = Path(__file__).with_name(WORKFLOW).read_text(encoding="utf-8")
    # The meta is the first statement of the script, and its last line is
    # the first line that holds only a closing brace.
    meta, end, body = source.partition("\n}\n")
    lines = meta.split("\n")
    # Every task checks both lines, so a script that the check refuses
    # fails at the first task that a command starts, whatever its title.
    index = {}
    for key in ("name", "description"):
        held = [
            n
            for n, line in enumerate(lines)
            if line.startswith(f"  {key}: ") and line.endswith(",")
        ]
        if len(held) != 1:
            die(f"the meta of {WORKFLOW} must hold its {key} on one line")
        index[key] = held[0]
    lines[index["name"]] = f"  name: {json.dumps(f'close {ref}')},"
    if title.strip():
        lines[index["description"]] = f"  description: {json.dumps(title)},"
    return "\n".join(lines) + end + body


def install_workflow(st, run, ref):
    """Write the copy of close-task.js that the Workflow of a task loads.

    The Workflow tool refuses a script file that has more than one hard
    link. The skill directory holds symlinks into the Nix store, where
    store optimisation can add a hard link to a file in the middle of a
    run. The copy in the task directory is a regular file with one link.
    Its content matches the script beside this file, except that its
    meta names the task, as task_script says.

    The tool also refuses a script file that a rename replaces during
    the load. A copy that the tool can load stays in place, so the tool
    can load the copy of a task while a command starts that task again.
    """
    script = task_script(ref, st["tasks"][ref]["title"]).encode()
    copy = script_path(run, ref)
    loadable = (
        copy.is_file()
        and not copy.is_symlink()
        and copy.stat().st_nlink == 1
        and copy.read_bytes() == script
    )
    if not loadable:
        copy.parent.mkdir(parents=True, exist_ok=True)
        # A rename gives the copy a new inode, so the links of the old
        # file do not carry to it.
        tmp = copy.with_name(f"{WORKFLOW}.tmp")
        tmp.unlink(missing_ok=True)
        tmp.write_bytes(script)
        tmp.replace(copy)


def free_dir(want, force):
    """The directory that a new run takes.

    Each run takes a directory of its own, so a new run starts without
    the rules.md, the notes.md, and the task directories of an earlier
    run. The new run takes the wanted directory when no earlier run
    holds it. Otherwise it takes the directory beside the wanted
    directory that no earlier run holds and whose name ends with the
    lowest number, starting at DIR-2. A directory that holds state.json
    or the tasks directory is the directory of an earlier run. With
    force, the wanted directory is free when it holds the tasks directory
    and no state.json. Force frees no other directory, because the caller
    named only the wanted directory.
    """
    run, n = want, 1
    while (run / "state.json").exists() or (
        (run / "tasks").is_dir() and not (force and run == want)
    ):
        n += 1
        run = want.with_name(f"{want.name}-{n}")
    return run


def take_over(repo, old, run, new):
    """Write the state of a new run that takes the unfinished tasks of an earlier run.

    The function moves the entry of each task that moving() names from
    the state of the earlier run to the new state. Those tasks are the
    unfinished tasks and the noted tasks. It also moves the directory of
    each task to the new run directory. The directory records that a run
    created the branch and the worktree of the task, as owns() says. So
    the new run resumes that work, and the earlier run keeps its landed
    and closed tasks. The new state names the earlier run first under
    replaces, and then each run that the state of the earlier run names
    there. The batch of the new run goes on from each of those runs. Then
    the function names the new run in the git config of the repository,
    and it saves the state of the earlier run last.

    The function holds the landing lock of the earlier run, and it ends
    the command when a landing of that run holds the lock. A landing
    reads the commit message of its task from the directory of the task,
    so a move during the landing breaks it. When a step fails, the
    function moves each directory back, names the earlier run in the git
    config again, and ends the command. When git cannot set the key back,
    the error names the git config command that does.
    """
    moved, named = [], False
    try:
        with open(old / "land.lock", "w") as lock:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                die(
                    f"a landing of the run at {old} is running, so another"
                    " session still continues that run. Init moves the"
                    " unfinished tasks of that run only after that session"
                    " stops"
                )
            with edited(old) as st:
                unfinished, noted = moving(st["tasks"])
                for ref in unfinished + noted:
                    d = old / "tasks" / ref
                    if d.is_dir():
                        d.rename(run / "tasks" / ref)
                        moved.append(ref)
                    new["tasks"][ref] = st["tasks"].pop(ref)
                new["replaces"] = [str(old), *st.get("replaces", [])]
                (run / "state.json").write_text(json.dumps(new))
                # An error here ends the block before edited() saves the
                # state of the earlier run, so that state keeps each task.
                p = sh("git", "config", "--local", KEY, str(run), cwd=repo, check=False)
                if p.returncode:
                    raise OSError(f"git cannot write {KEY}: {p.stderr.strip()}")
                named = True
    except OSError as e:
        for ref in moved:
            (run / "tasks" / ref).rename(old / "tasks" / ref)
        lost = ""
        if named:
            p = sh("git", "config", "--local", KEY, str(old), cwd=repo, check=False)
            if p.returncode:
                lost = (
                    f" Git also cannot set {KEY} back to that run, so run git"
                    f" config --local {KEY} {old} before init."
                )
        die(
            f"init cannot move the unfinished tasks of the run at {old} to a"
            f" new run: {e}. Those tasks stay in that run.{lost} Remove the"
            " cause and run init again, or pass --force to start an empty run"
            " without those tasks"
        )


def cmd_init(a):
    repo = main_checkout()
    old, old_st, cause = named_run(repo)
    # The script decides whether the session continues the run that the
    # repository names, as new_run_reason() says. Only --force replaces a
    # run that the session continues.
    if old_st is not None and not a.force and not new_run_reason(old_st):
        if old_st["tasks"]:
            die(
                f"the run at {old} has not ended, and this session continues it."
                " Pass --force to replace it with an empty run, which leaves"
                " its unfinished tasks in that run"
            )
        die(
            f"the run at {old} holds no task yet, and this session continues"
            " it. Change its config with the config command, or pass --force"
            " to replace it, such as with another target. The config command"
            " keeps the target of the run"
        )
    base, pr = base_of(a.target)
    if pr:
        if not branch_exists(repo, base):
            die(
                f"the target {a.target} proposes each task as a pull request"
                f" against the branch {base}, and the repository has no branch"
                f" {base}. Name the base branch of the pull requests with"
                f" --target {PR}:BRANCH"
            )
    elif (current := out("git", "branch", "--show-current", cwd=repo)) != base:
        # Every landing commits on the base branch in the main checkout,
        # where the commit hooks run.
        has = f"has {current} checked out" if current else "has a detached HEAD"
        die(
            f"the target of the run is the branch {base}, but the main checkout"
            f" {has}. Every landing commits on the target in the main checkout."
            f" Check out {base} there, or name another branch with --target"
        )
    # A run that the session does not continue can hold unfinished tasks,
    # as new_run_reason() says. The new run takes them, and --force leaves
    # them.
    takes = old_st is not None and not a.force and not ended(old_st["tasks"])
    if takes and (why := move_refusal(old_st["config"], a.target)):
        # A run can continue from a state file that has no target key.
        was = old_st["config"].get("target", old_st["config"]["base"])
        die(
            f"the run at {old} has unfinished tasks of the target {was}, and"
            f" {why}. Name the target {was} to move those tasks to the new run,"
            " or pass --force to start an empty run without them"
        )
    left, added = ({}, []) if old_st is None else left_branches(repo, old_st, takes)
    run = free_dir(Path(a.run).resolve(), a.force)
    percents = floor_percents({}, a.disk_floor_percent)
    ignored = sh(
        "git", "check-ignore", "-q", ".claude/worktrees/x", cwd=repo, check=False
    )
    if ignored.returncode:
        common = out(
            "git", "rev-parse", "--path-format=absolute", "--git-common-dir", cwd=repo
        )
        with open(Path(common, "info", "exclude"), "a") as f:
            f.write(".claude/worktrees/\n")
    cfg = {
        "repo": repo,
        "target": a.target,
        "base": base,
        "worktrees": f"{repo}/.claude/worktrees",
        "limit": a.limit,
        "rounds": a.rounds,
        "models": role_models({}, a.model),
        "check": a.check,
        "health": a.health,
        "stepTimeout": a.step_timeout,
        "shared": list(dict.fromkeys(DEFAULT_SHARED + (a.shared or []))),
        "diskFloorGb": a.disk_floor_gb,
        "diskFloorPercent": percents,
        "diskPaths": list(
            dict.fromkeys(default_disk_paths(repo) + a.disk_path + list(percents))
        ),
    }
    # The new run takes its config from the flags of this command. It
    # does not take the config, the rules.md, or the notes.md of an
    # earlier run.
    new = {
        "config": cfg,
        "tasks": {},
        "skill": skill_version(),
        "session": session(),
        "left": left,
    }
    # Init creates the run directory after each step that can fail, and a
    # failed init removes each directory that it created. So a failed
    # init leaves no tasks directory, which a retry counts as the
    # directory of an earlier run. Init names the new run in the git
    # config inside the same guard. A failed init leaves the git config as
    # it was, unless take_over() cannot set it back. Its error then gives
    # the git config command that does.
    made = [d for d in (run, run / "tasks", run / "logs") if not d.exists()]
    for d in made:
        d.mkdir(parents=True)
    try:
        if takes:
            take_over(repo, Path(old), run, new)
        else:
            (run / "state.json").write_text(json.dumps(new))
            sh("git", "config", "--local", KEY, str(run), cwd=repo)
    except BaseException:
        (run / "state.json").unlink(missing_ok=True)
        for d in reversed(made):
            with suppress(OSError):
                d.rmdir()
        raise
    if old_st is not None:
        print(f"replaced the run at {old}")
        if added:
            tasks = counted(len(added), "task")
            print(f"left {tasks} with the branch of a pull request in that run:")
            for ref in added:
                entry = left[ref]
                opened = f" pr={entry['pr']}" if "pr" in entry else ""
                print(f"{ref} {entry['branch']}{opened}")
    elif old:
        print(f"replaced a run that init cannot read, because {cause}")
    moved = sorted(new["tasks"].items())
    if unfinished := [(r, t) for r, t in moved if t["state"] in UNFINISHED]:
        tasks = counted(len(unfinished), "unfinished task")
        print(f"moved {tasks} of that run to the new run:")
        print(" ".join(f"{ref} ({t['state']})" for ref, t in unfinished))
    if noted := [r for r, t in moved if t["state"] == "noted"]:
        print(f"moved {counted(len(noted), 'noted task')} of that run to the new run:")
        print(" ".join(noted))
    print(f"run={run} repo={repo} target={a.target} base={base} limit={a.limit}")


def cmd_config(a):
    repo = main_checkout()
    run = run_dir(repo)
    with state(run) as st:
        cfg = st["config"]
        if absent := [g for g in a.unshared if g not in cfg["shared"]]:
            die(
                f"no shared glob {' '.join(absent)}. The shared globs are"
                f" {' '.join(cfg['shared'])}"
            )
        paths = disk_paths(cfg)
        if absent := [p for p in a.no_disk_path if p not in paths]:
            die(
                f"no disk path {' '.join(absent)}. The disk paths are {' '.join(paths)}"
            )
        changes = {
            "limit": a.limit,
            "rounds": a.rounds,
            "check": a.check,
            "health": a.health,
            "stepTimeout": a.step_timeout,
            "diskFloorGb": a.disk_floor_gb,
        }
        cfg.update({k: v for k, v in changes.items() if v is not None})
        # A run can continue from a state file that has no models key.
        cfg["models"] = role_models(cfg.get("models", {}), a.model)
        # A run can continue from a state file that has no percent floors.
        percents = floor_percents(cfg.get("diskFloorPercent", {}), a.disk_floor_percent)
        cfg["diskPaths"] = [
            p
            for p in dict.fromkeys(paths + a.disk_path + list(percents))
            if p not in a.no_disk_path
        ]
        cfg["diskFloorPercent"] = {
            p: n for p, n in percents.items() if p not in a.no_disk_path
        }
        cfg["shared"] = [
            g for g in dict.fromkeys(cfg["shared"] + a.shared) if g not in a.unshared
        ]
        # The triage command warns about the globs of earlier commands.
        unused = [g for g in unmatched(st) if g in a.shared]
    print(json.dumps(cfg, indent=1, sort_keys=True))
    warn_unmatched(unused)


def dependencies(repo, ref):
    """The refs of the open tasks that aven records as blockers of a task."""
    listed = json.loads(out("aven", "dep", "list", ref, "--json", cwd=repo))
    return [d["ref"] for d in listed["depends_on"] if d["unresolved"]]


def cmd_triage(a):
    repo = main_checkout()
    run = run_dir(repo)
    rows = json.loads(Path(a.file).read_text())
    # A failed aven command ends the triage before it changes the state.
    deps = {row["ref"]: dependencies(repo, row["ref"]) for row in rows}
    with state(run) as st:
        added = updated = 0
        for row in rows:
            ref = row["ref"]
            files = sorted(
                {f.removeprefix(repo + "/").removeprefix("./") for f in row["files"]}
            )
            new = {
                "title": row["title"],
                "priority": row.get("priority", "none"),
                "files": files,
                "after": list(dict.fromkeys(row.get("after", []) + deps[ref])),
                "last": bool(row.get("last", False)),
            }
            old = st["tasks"].get(ref)
            if old:
                old.update(new)
                updated += 1
            else:
                st["tasks"][ref] = {
                    **new,
                    "state": "pending",
                    "startedAt": 0,
                    "flags": [],
                }
                added += 1
        unused = unmatched(st)
    print(f"added={added} updated={updated}")
    warn_unmatched(unused)


def take_limit(filters):
    """Split the --limit filter from the other filters of aven list.

    Return the other filters and the limit, or None for no limit.
    """
    rest, limit = [], None
    args = iter(filters)
    for f in args:
        if f == "--limit":
            value = next(args, "")
        elif f.startswith("--limit="):
            value = f.removeprefix("--limit=")
        else:
            rest.append(f)
            continue
        if not value.isdecimal() or int(value) < 1:
            die(f"--limit takes a whole number of at least 1, not {value!r}")
        limit = int(value)
    return rest, limit


def cmd_untriaged(a):
    repo = main_checkout()
    run = run_dir(repo)
    # aven list applies --limit before the command drops the tasks that
    # the run holds and the epics. So the command applies the limit itself
    # after it drops them.
    filters, limit = take_limit(a.filters)
    # The command passes --open whenever aven accepts it, so aven lists
    # only the open tasks. aven refuses --open beside --status, --all,
    # --deleted, or --upcoming, and it refuses a second --open. With one
    # of those flags, the command itself drops each closed or deleted
    # task.
    flags = {f.split("=", 1)[0] for f in filters}
    refused = {"--open", "--status", "--all", "--deleted", "--upcoming"}
    opened = [] if flags & refused else ["--open"]
    listed = json.loads(out("aven", "list", "--json", *opened, *filters, cwd=repo))
    held = json.loads((run / "state.json").read_text())["tasks"]
    rows = [
        {"ref": t["ref"], "title": t["title"], "priority": t["priority"]}
        for t in listed
        if t["status"] in AVEN_OPEN
        and not t["deleted"]
        and not t["is_epic"]
        and t["ref"] not in held
    ]
    print(json.dumps(rows[:limit], indent=1))


def owns(run, ref):
    """Whether the branch and the worktree of a task are the work of the run.

    The start of a task creates the directory of the task in the run
    directory before the branch and the worktree, and each run takes a
    run directory of its own. So a task branch or a task worktree whose
    task has no directory comes from an earlier run.

    With --force, the init command takes the directory of --run when that
    directory holds the tasks directory of an earlier run and no
    state.json. The run then owns the work of each task that has a
    directory there. The init command also moves the directory of each
    task that a new run takes from the run that it replaces, as
    take_over() says. So the new run owns the work of those tasks.
    """
    return (run / "tasks" / ref).is_dir()


def left_branches(repo, st, takes):
    """The pull request branches that a new run counts as leftover work.

    The new run replaces the run of the state st. When the new run does
    not take the tasks of that run, such as with init --force, each task
    of that run stays there. The branch of a task that a landing proposed
    as a pull request has another name than task_branch(), so
    leftovers() cannot find that branch by its name. The new run records
    the branch of each such task whose state is neither landed nor
    closed, with the address of its pull request when the task records
    one. It also keeps each branch that the state st records the same
    way. The record holds only the branches that exist.

    Return the record, by ref, and the refs that the record adds for the
    tasks of the state st.
    """
    left = {
        ref: entry
        for ref, entry in st.get("left", {}).items()
        if branch_exists(repo, entry["branch"])
    }
    added = []
    # The new run takes the unfinished tasks and the noted tasks, so each
    # task that stays has the state landed or closed.
    if takes:
        return left, added
    for ref, t in sorted(st["tasks"].items()):
        name = t.get("branch")
        if name and t["state"] not in SETTLED and branch_exists(repo, name):
            left[ref] = {key: t[key] for key in ("branch", "pr") if key in t}
            added.append(ref)
    return left, added


def leftovers(st, run, refs):
    """Describe the work that an earlier run left for the given tasks.

    Return one line per task that has a branch or a worktree that the run
    does not own, as owns() says. The line names the branch, the count of
    its commits that the base branch lacks, and the worktree. For the
    branch of a pull request that left_branches() records, the line also
    names the pull request, or says that the earlier run recorded none.
    That run records no pull request before the orchestrator opens it,
    and the orchestrator might never have pushed the branch. Also return
    the entry of the record for each line that names such a branch.
    """
    cfg = st["config"]
    repo, base = cfg["repo"], cfg["base"]
    # A run can continue from a state file that has no left key.
    left = st.get("left", {})
    heads = out(
        "git",
        "for-each-ref",
        "--format=%(refname:lstrip=2)",
        "refs/heads/task",
        cwd=repo,
    ).splitlines()

    def ahead(name):
        n = int(out("git", "rev-list", "--count", f"{base}..{name}", cwd=repo))
        return f"{n} {'commit' if n == 1 else 'commits'} ahead of {base}"

    lines, proposals = [], []
    for ref in refs:
        if owns(run, ref):
            continue
        found = []
        # A task of an earlier run starts on the branch of task_branch().
        name = task_branch(ref)
        if name in heads:
            found.append(f"the branch {name}, {ahead(name)}")
        entry = left.get(ref)
        if entry and branch_exists(repo, entry["branch"]):
            name = entry["branch"]
            if pr := entry.get("pr"):
                found.append(
                    f"the branch {name} of the pull request {pr}, {ahead(name)}"
                )
            else:
                found.append(
                    f"the branch {name} that a landing proposed, with no recorded"
                    f" pull request, {ahead(name)}"
                )
            proposals.append(entry)
        if Path(worktree(cfg, ref)).exists():
            found.append(f"the worktree {worktree(cfg, ref)}")
        if found:
            lines.append(f"{ref}: {', and '.join(found)}")
    return lines, proposals


def refuse_leftovers(st, run, refs, retry):
    """End the command when an earlier run left work for one of the given tasks.

    A task that starts on the branch of an earlier run edits a tree that
    can sit far behind the base branch. Its landing then records the
    commits of the earlier run in the commit or the pull request of the
    task. A new attempt at a task whose branch belongs to a pull request
    also opens a second pull request beside the open one.

    The retry argument names the ultraclose.py command that starts the
    tasks after the cleanup.
    """
    lines, proposals = leftovers(st, run, refs)
    if lines:
        base = st["config"]["base"]
        close = ""
        if proposals:
            close = "\n"
            if any("pr" not in entry for entry in proposals):
                close += (
                    "For each branch with no recorded pull request, list its pull"
                    " requests with mcp__github__list_pull_requests, and set head"
                    " to <owner>:<branch> and state to all. When the list is"
                    " empty, the branch has no pull request. "
                )
            close += (
                "Before you move the branch of a pull request, close that pull"
                " request with mcp__github__update_pull_request when it is open,"
                " unless the user asks you to keep it open. When it merged, the"
                " task does not need a new attempt, so take the Merged pull"
                " request steps of the Leftover work section of SKILL.md."
            )
        die(
            "these tasks have a branch or a worktree that this run did not"
            " create, and a run resumes only its own work. The command"
            " started no task.\n" + "\n".join(lines) + close + "\nIn each worktree that"
            " holds uncommitted changes, commit them with git add -A and git"
            " commit --no-verify. Remove each worktree with git worktree remove."
            " After those commits, count the commits of each branch again with"
            f" git rev-list --count {base}..<branch>, because the counts above"
            " do not include those commits. Delete each branch whose new count"
            " is 0 with git branch -D. Rename every other branch with git"
            " branch -m to kept/<ref>-<date>, so that it keeps its commits as"
            " a reference, and name the new branch and its old name in a note"
            f" on the task with aven note. Then run ultraclose.py {retry}."
            " The Leftover work section of SKILL.md holds these steps."
        )


def start(st, run, ref):
    """Give a task its worktree, its task file, and the active status.

    The caller first refuses a task that has the work of an earlier run.
    So a branch or a worktree that exists here is the work of this run,
    and the task resumes it.
    """
    cfg, t = st["config"], st["tasks"][ref]
    repo, wt, name = cfg["repo"], worktree(cfg, ref), branch(t, ref)
    # The directory of the task records that this run started the task,
    # and owns() reads that record. So the directory comes before the
    # branch and the worktree.
    d = run / "tasks" / ref
    (d / "scratch").mkdir(parents=True, exist_ok=True)
    if not Path(wt).exists():
        # A landing that proposes a pull request removes the worktree and
        # keeps the branch, so a relaunch of that task takes this path.
        if branch_exists(repo, name):
            sh("git", "worktree", "add", "-q", wt, name, cwd=repo)
        else:
            sh("git", "worktree", "add", "-q", wt, "-b", name, cfg["base"], cwd=repo)
    (d / "task.md").write_text(out("aven", "context", ref, cwd=repo) + "\n")
    sh("aven", "edit", ref, "--status", "active", cwd=repo)
    t["state"] = "inflight"
    t["startedAt"] = time.time()


def fill_slots(run):
    """Start one task per free slot, and print the list of their Workflow inputs."""
    with state(run) as st:
        # A pending task that waits for another task starts in a later
        # call. The command reports its leftover work in the first call,
        # together with the leftover work of every other pending task. A
        # landing that runs next has landed its task, so the retry is next
        # for both callers.
        pending = sorted(r for r, t in st["tasks"].items() if t["state"] == "pending")
        refuse_leftovers(st, run, pending, "next")
        launched = []
        for ref in pick(st):
            install_workflow(st, run, ref)
            start(st, run, ref)
            # A launch without a resume field starts at the implementer.
            unstore(run, ref)
            launched.append(workflow_args(st, run, ref))
    print(json.dumps(launched, indent=1))


def cmd_next(a):
    repo = main_checkout()
    run = run_dir(repo)
    fill_slots(run)


def cmd_args(a):
    repo = main_checkout()
    run = run_dir(repo)
    with state(run) as st:
        t = st["tasks"].get(a.ref) or die(f"unknown task {a.ref}")
        refuse_leftovers(st, run, [a.ref], f"args {a.ref} again")
        install_workflow(st, run, a.ref)
        history = run / "tasks" / a.ref / "history.md"
        done = 0
        if history.exists():
            done = len(re.findall(r"^## Round \d+", history.read_text(), re.MULTILINE))
        rnd = a.round or done + 1
        if a.final:
            last = rnd
        elif a.sync:
            last = rnd + 1
        else:
            last = max(st["config"]["rounds"], rnd)
        resume = {"round": rnd, "lastRound": last, **stored(run, a.ref)}
        if a.sync:
            resume["sync"] = a.sync
        was = t["state"]
        start(st, run, a.ref)
        # A task with no commit of its own starts again at the implementer.
        ahead = out(
            "git",
            "rev-list",
            "--count",
            f"{st['config']['base']}..{branch(t, a.ref)}",
            cwd=repo,
        )
        if ahead == "0" and not a.sync:
            resume = None
            unstore(run, a.ref)
        print(json.dumps(workflow_args(st, run, a.ref, resume), indent=1))
        print(f"{a.ref}: {was} to inflight", file=sys.stderr)


def cmd_set(a):
    repo = main_checkout()
    run = run_dir(repo)
    with state(run) as st:
        cfg = st["config"]
        t = st["tasks"].get(a.ref) or die(f"unknown task {a.ref}")
        t["state"] = a.state
        if a.flag:
            t["flags"].append(a.flag)
        wt = worktree(cfg, a.ref)
        if a.state in ("noted", "closed") and Path(wt).exists():
            # The command removes only the work of this run. The branch of
            # an earlier run can hold commits that no other branch holds.
            if not owns(run, a.ref):
                print(f"kept worktree {wt}, which this run did not create")
            elif out("git", "status", "--porcelain", cwd=wt):
                print(f"kept dirty worktree {wt}")
            else:
                sh("git", "worktree", "remove", wt, cwd=repo)
                if a.state == "closed":
                    sh("git", "branch", "-q", "-D", branch(t, a.ref), cwd=repo)
    print(f"{a.ref} {a.state}")


def default_disk_paths(repo):
    """The disk paths that a run starts with.

    The worktrees fill the file system of the repository. Most build
    caches fill the home directory of the user, which another file system
    can hold.
    """
    home = os.path.expanduser("~")
    return list(dict.fromkeys([repo, home])) if os.path.isdir(home) else [repo]


def disk_paths(cfg):
    """The paths whose file systems the run watches.

    A run can continue from a state file that has no diskPaths key. That
    run watches the default paths.
    """
    return cfg.get("diskPaths", default_disk_paths(cfg["repo"]))


def floor_percents(percents, flags):
    """The percent floor of each disk path after the --disk-floor-percent flags.

    A flag with 0 percent removes the floor of its path.
    """
    percents = dict(percents)
    for path, n in flags:
        if n:
            percents[path] = n
        else:
            percents.pop(path, None)
    return percents


def disks(cfg):
    """The watched file systems, each as a line of text and whether it is low.

    The line names the first disk path that the file system holds, so two
    paths on one file system give one line. A file system is low when its
    free space is less than the floor in GB, or less than its percent
    floor. The percent floor of a file system is the highest percent floor
    of the disk paths that it holds, and it applies to no other file
    system. A path that the command cannot measure counts as low, so that
    the watch command reports the path.
    """
    # Each disk path with its device and its usage, or with no device and
    # the cause that kept the command from measuring it.
    measured = []
    for path in disk_paths(cfg):
        try:
            measured.append((path, os.stat(path).st_dev, shutil.disk_usage(path)))
        except OSError as e:
            measured.append((path, None, e.strerror or e))
    # A run can continue from a state file that has no percent floors.
    percents = cfg.get("diskFloorPercent", {})
    # The highest percent floor of each device, and the disk path that has it.
    highest = {}
    for path, device, _ in measured:
        n = percents.get(path, 0)
        if device is not None and n > highest.get(device, (0, None))[0]:
            highest[device] = (n, path)
    found, seen = [], set()
    for path, device, usage in measured:
        if device is None:
            found.append((f"{path}: cannot measure the free space, {usage}", True))
            continue
        if device in seen:
            continue
        seen.add(device)
        size, free = usage.total / 2**30, usage.free / 2**30
        floor, source = cfg["diskFloorGb"], ""
        n, by = highest.get(device, (0, None))
        if size * n / 100 > floor:
            floor = size * n / 100
            source = f" set by --disk-floor-percent {by}={n:g}"
        share = 100 * free / size if size else 0
        text = f"{path}: {free:.0f} GB free of {size:.0f} GB ({share:.0f}%)"
        if free < floor:
            text += f", less than the floor of {floor:.0f} GB{source}"
        else:
            text += f", floor {floor:.0f} GB{source}"
        found.append((text, free < floor))
    return found


def span(seconds):
    """A time span as text, such as 42s or 11m50s."""
    s = round(seconds)
    return f"{s}s" if s < 60 else f"{s // 60}m{s % 60:02d}s"


def medians(landings):
    """The median time of each timed step over the landings with a result of PASSED.

    A failed landing can stop a step early, so its times do not count.
    """
    found = {}
    for step in TIMED:
        times = [
            entry["times"][step]
            for entry in landings
            if entry["result"] in PASSED and step in entry["times"]
        ]
        if times:
            found[step] = statistics.median(times)
    return found


def times_text(times, typical=()):
    """The time of each timed step of a landing, in the order of the landing.

    Each time comes with the median of its step when typical holds one.
    """
    parts = []
    for step in TIMED:
        if step in times:
            part = f"{step}={span(times[step])}"
            if step in typical:
                part += f" (median {span(typical[step])})"
            parts.append(part)
    return " ".join(parts)


def cmd_status(a):
    repo = main_checkout()
    # The command changes no state, so it records no session.
    run, st = current(repo)
    cfg, tasks = st["config"], st["tasks"]
    now = time.time()
    counts = {s: sum(1 for t in tasks.values() if t["state"] == s) for s in STATES}
    skill, ran = origin_texts(st)
    print(f"run={run}")
    print(f"skill: {skill}")
    print(f"session: {ran}")
    print(f"setup: {setup_text(st)}")
    for text in earlier_texts(st):
        print(f"earlier run: {text}")
    print(f"target: {target_text(cfg)}")
    print(" ".join(f"{s}={n}" for s, n in counts.items()), f"limit={cfg['limit']}")
    print(f"shared: {' '.join(cfg['shared'])}")
    # A run can continue from a state file that has no models key.
    held = cfg.get("models", {})
    named = [f"{r}={held[r]}" for r in (*ROLES, DEFAULT_ROLE) if r in held]
    print(f"models: {' '.join(named) or 'none'}")
    print(f"check: {cfg['check'] or 'none'}")
    # A run can continue from a state file that has no health key.
    print(f"health: {cfg.get('health') or 'none'}")
    print(f"step timeout: {span(step_timeout(cfg))}")
    if ended(tasks):
        print(f"ended: {ENDED}")
    for ref, t in sorted(tasks.items()):
        if t["state"] == "inflight":
            hours = (now - t["startedAt"]) / 3600
            print(f"inflight {ref} {hours:.1f}h | {t['title']}")
        elif t["state"] == "approved":
            print(f"approved {ref} waits for landing | {t['title']}")
        elif t["state"] == "proposed":
            head, url = branch(t, ref), t.get("pr")
            if not url:
                waits = f"waits for the push of {head} and its pull request"
            elif t["pushed"]:
                waits = f"waits for the merge of {url}"
            else:
                waits = f"waits for the push of {head} to the pull request {url}"
            print(f"proposed {ref} {waits} | {t['title']}")
        elif t["state"] == "pending" and (b := blockers(st, t)):
            noted = [r for r in b if tasks[r]["state"] == "noted"]
            if noted:
                print(f"stuck {ref} waits for noted {' '.join(noted)} | {t['title']}")
    for ref, t in sorted(tasks.items()):
        for flag in t["flags"]:
            print(f"flag {ref} {t['state']}: {flag}")
    # A run can continue from a state file that has no landings key.
    landings = st.get("landings", [])
    if typical := medians(landings):
        print(f"median landing: {times_text(typical)}")
    for entry in landings[-RECENT:]:
        print(f"landing {entry['ref']} {entry['result']}: {times_text(entry['times'])}")
    for text, _ in disks(cfg):
        print(f"disk {text}")


def cmd_watch(a):
    repo = main_checkout()
    run = run_dir(repo)
    while True:
        st = json.loads((run / "state.json").read_text())
        low = [text for text, is_low in disks(st["config"]) if is_low]
        for text in low:
            print(f"DISK {text}")
        over = ended(st["tasks"])
        if over:
            print(f"ENDED {ENDED}")
        if low or over:
            return
        time.sleep(a.interval)


def lint_env(cache):
    """The environment of a landing step that might run golangci-lint.

    The step gets a golangci-lint cache that no other checkout writes.
    golangci-lint 2.12 keys its cache by the path of each file inside its
    module and by the content of the file, not by the directory of the
    checkout. It stores each issue with the absolute path of its file. So
    a checkout reads the issues that another checkout of the same module
    cached, under the paths of that checkout. After a landing removes
    that checkout, golangci-lint cannot read the nolint comments of those
    files, and it reports each issue that they suppressed.
    """
    return {**os.environ, "GOLANGCI_LINT_CACHE": str(cache)}


def step_timeout(cfg):
    """How long a timed step of a landing runs before the landing stops it.

    A run can continue from a state file that has no stepTimeout key. That
    run has the default step timeout.
    """
    return cfg.get("stepTimeout", STEP_TIMEOUT)


def alive(p):
    """Whether a process still runs in the process group that p leads."""
    # A leader that has ended stays in its group until the landing reads
    # its exit status.
    p.poll()
    try:
        os.killpg(p.pid, 0)
    except (ProcessLookupError, PermissionError):
        # The group holds no process, or no process that the landing can
        # signal.
        return False
    return True


def stop_group(p):
    """End every process of the process group that p leads.

    The group first gets SIGINT, the signal of an interrupt from the
    keyboard. git then removes its lock files. lefthook 2.2 runs some
    hook commands in process groups of their own, and it ends those
    groups only after SIGINT. The processes that still run GRACE seconds
    later get SIGTERM, and the processes that run GRACE seconds after
    SIGTERM get SIGKILL. The function waits GRACE seconds at most for the
    processes that it kills. A process that left the group keeps running
    when no process of the group ends it.
    """
    for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGKILL):
        try:
            os.killpg(p.pid, sig)
        except (ProcessLookupError, PermissionError):
            return
        until = time.monotonic() + GRACE
        while alive(p):
            if time.monotonic() >= until:
                break
            time.sleep(0.05)
        else:
            return


def run_step(cmd, cwd, env, log, limit):
    """Run one step of a landing, and return its exit status.

    The output of the step goes to the log file. The step leads a process
    group of its own, so that the landing can stop the step together with
    each process that the step started. When the step runs longer than
    limit seconds, the function stops that group as stop_group says, and
    it returns None. A limit of None gives the step no time limit. The
    function also stops the group when a signal of ENDS ends the land
    command during the step. A signal of ENDS that comes while the
    function stops the group ends the land command after the group stops.
    """
    # During the step, SIGTERM and SIGHUP raise an exit in place of the
    # death of the process, so that the function stops the step. Python
    # raises an interrupt for SIGINT. A signal that the caller ignores
    # stays ignored, and Python keeps an ignored SIGINT ignored. After the
    # step, each signal gets its old handler back. So between the steps,
    # SIGTERM and SIGHUP end the land command at once. The git command that
    # the land command runs then keeps running until it ends. An exit that
    # raises in subprocess.run makes it kill its child with SIGKILL. A
    # killed git command can leave a lock file of git in the main checkout.
    old = {}
    for sig in (signal.SIGTERM, signal.SIGHUP):
        if signal.getsignal(sig) == signal.SIG_DFL:
            old[sig] = signal.signal(
                sig, lambda signum, frame: sys.exit(128 + signum)
            )
    try:
        with open(log, "w") as f:
            p = subprocess.Popen(
                cmd,
                cwd=cwd,
                env=env,
                stdout=f,
                stderr=subprocess.STDOUT,
                start_new_session=True,
            )
            try:
                return p.wait(timeout=limit)
            except BaseException as e:
                # No signal reaches the step after the land command ends.
                # So the command blocks the signals that end it until the
                # group stops, and a blocked signal raises when the mask
                # returns.
                mask = signal.pthread_sigmask(signal.SIG_BLOCK, ENDS)
                try:
                    stop_group(p)
                finally:
                    signal.pthread_sigmask(signal.SIG_SETMASK, mask)
                if isinstance(e, subprocess.TimeoutExpired):
                    return None
                raise
    finally:
        for sig, handler in old.items():
            signal.signal(sig, handler)


def tail(path, n=40):
    return "\n".join(Path(path).read_text(errors="replace").splitlines()[-n:])


def cmd_land(a):
    repo = main_checkout()
    run = run_dir(repo)
    ref = a.ref
    with state(run) as st:
        cfg = st["config"]
        if ref not in st["tasks"]:
            die(f"unknown task {ref}")
        t = st["tasks"][ref]
        # The landing that proposed the task removed its worktree, so a
        # second landing fails. The task must keep its state until then.
        if t["state"] == "proposed":
            die(
                f"{ref} is proposed, so it waits for its pull request to merge."
                " Record the merge with the merged command. To change the"
                " pull request, relaunch the task with the args command, and"
                " land it after its review"
            )
        # A task that waits for its landing frees its slot and keeps its
        # file locks.
        t["state"] = "approved"
        br = branch(t, ref)
        prefixes = sorted({r.split("-")[0] for r in st["tasks"]})
    refs = re.compile(rf"\b(?:{'|'.join(map(re.escape, prefixes))})-[0-9A-Z]{{4,}}\b")
    wt, base = worktree(cfg, ref), cfg["base"]
    msg = run / "tasks" / ref / "msg.txt"
    logs = run / "logs"
    timeout = step_timeout(cfg)

    # How long each timed step of this landing took, in seconds.
    times = {}

    def record(code):
        """Record the times of the landing with its result, and print them.

        The TIMES line gives each time with the median of the earlier
        landings with a result of PASSED. A landing that ran no timed
        step records nothing and prints nothing.

        The command prints the RESULT line after this function returns.
        So the function reports a state file that it cannot write, such
        as on a full disk, and returns.
        """
        if not times:
            return
        typical = {}
        try:
            with state(run) as st:
                # A run can continue from a state file that has no
                # landings key.
                landings = st.setdefault("landings", [])
                typical = medians(landings)
                landings.append({"ref": ref, "result": code, "times": times})
        except OSError as e:
            print(f"the run did not record the times of this landing: {e}")
        print(f"TIMES {times_text(times, typical)}")

    def fail(code, detail=""):
        # The RESULT line comes last, so the end of the output shows it
        # however long the detail is.
        if detail:
            print(detail)
        record(code)
        print(f"RESULT {ref} {code}")
        sys.exit(2)

    def logged(name, *cmd, cwd, env=None, limit=None):
        """Run a step, and return its exit status and the path of its log.

        The exit status is None when the step ran longer than limit
        seconds and the landing stopped it, as run_step says.
        """
        path = logs / f"{ref}.{name}.log"
        return run_step(cmd, cwd, env, path, limit), path

    def timed(name, *cmd, cwd, env=None):
        """Run a logged step under the step timeout of the run.

        Add how long the step took to the times.
        """
        began = time.monotonic()
        rc, path = logged(name, *cmd, cwd=cwd, env=env, limit=timeout)
        times[name] = round(times.get(name, 0) + time.monotonic() - began, 1)
        return rc, path

    def overran(name, path):
        """What a landing prints about a timed step that it stopped.

        The text names the step timeout and the log of the step, and it
        ends with the last lines of that log.
        """
        return (
            f"the {name} ran longer than the step timeout of {span(timeout)}, so"
            f" the landing stopped it, see {path}\n{tail(path)}"
        ).rstrip()

    def require_health(when):
        """Fail the landing with HEALTHFAIL when the health command fails.

        The check and the commit hooks cannot run when the health command
        fails. So a failed check or a failed commit then does not show that
        the task needs a change. The when argument says when the health
        command ran, and after a failed step it names the log of that step.
        """
        # A run can continue from a state file that has no health key.
        if not cfg.get("health"):
            return
        rc, path = logged("health", "sh", "-c", cfg["health"], cwd=repo)
        if rc:
            detail = f"the health command failed {when}\n{tail(path)}"
            fail("HEALTHFAIL", detail.rstrip())

    def propose(head):
        """End a landing that proposes the task as a pull request.

        The branch of the task takes the name head. The worktree goes, and
        the branch stays for the push. The task waits as proposed for its
        pull request to merge, and the merged command lands it.

        The script contacts no remote. The RESULT line names the branch
        that the caller pushes and the base branch of the pull request. It
        also gives the address of a pull request that the task records.
        The caller then reads the state of that pull request, and opens no
        second pull request while it is open. The task records that the
        branch waits for its push, until the opened command records the
        push.
        """
        if head != br:
            # Another command can create a branch with the name during the
            # check. The landing then fails with its RESULT line last.
            renamed = sh("git", "branch", "-m", br, head, cwd=repo, check=False)
            if renamed.returncode:
                fail("BRANCHTAKEN", renamed.stderr.strip())
            with state(run) as st:
                st["tasks"][ref]["branch"] = head
        sh("git", "worktree", "remove", "--force", wt, cwd=repo)
        with state(run) as st:
            t = st["tasks"][ref]
            t["state"] = "proposed"
            # A relaunch can add commits to the branch of a pull request
            # that the task records, so every landing waits for a push.
            t["pushed"] = False
            url = t.get("pr")
        record("PR")
        opened = f" pr={url}" if url else ""
        print(f"RESULT {ref} PR head={head} base={base}{opened}", flush=True)

    def squash():
        """End a landing that commits the task on the base branch.

        The main checkout gets the task branch as one commit with the
        message of the task. The worktree and the branch go, and the task
        lands.
        """
        head = out("git", "rev-parse", "HEAD", cwd=repo)

        def rollback():
            sh("git", "reset", "-q", "--hard", head, cwd=repo)
            sh("git", "clean", "-qfd", cwd=repo)

        rc, log = logged("squash", "git", "merge", "--squash", br, cwd=repo)
        if rc:
            rollback()
            fail("CONFLICT", tail(log))
        if not sh(
            "git", "diff", "--cached", "--quiet", cwd=repo, check=False
        ).returncode:
            fail("EMPTY", f"{br} holds no change against {base}")
        want = out("git", "write-tree", cwd=repo)
        # A hook once emptied the index of the main checkout during a
        # commit. The commit must record the staged tree, so compare the
        # two and retry one time.
        for attempt in (1, 2):
            # The commit hooks run in the main checkout. Its cache sits in
            # the run directory, where no worktree writes.
            rc, log = timed(
                "commit",
                "git",
                "commit",
                "-F",
                str(msg),
                cwd=repo,
                env=lint_env(run / LINT_CACHE),
            )
            if rc is None:
                detail = overran("commit", log)
                # A process that the landing killed can leave a lock file
                # of git in the main checkout, and the rollback then
                # fails. The landing still prints its RESULT line.
                try:
                    rollback()
                except SystemExit as e:
                    detail += (
                        "\nthe landing did not restore the main checkout, which"
                        f" still holds the changes of {br}:\n{e}"
                    )
                fail("COMMITTIMEOUT", detail)
            if rc:
                rollback()
                require_health(f"after the commit failed, see {log}")
                fail("COMMITFAIL", tail(log))
            if out("git", "rev-parse", "HEAD^{tree}", cwd=repo) == want:
                break
            if out("git", "rev-parse", "HEAD~1", cwd=repo) != head or attempt == 2:
                rollback()
                fail("TREEMISMATCH", f"the commit did not record tree {want}")
            sh("git", "reset", "-q", "--soft", head, cwd=repo)
            sh("git", "read-tree", want, cwd=repo)
        if dirty := out("git", "status", "--porcelain", cwd=repo):
            fail("POSTDIRTY", dirty)
        sh("git", "worktree", "remove", "--force", wt, cwd=repo)
        sh("git", "branch", "-q", "-D", br, cwd=repo)
        sh("aven", "edit", ref, "--status", "done", cwd=repo)
        with state(run) as st:
            st["tasks"][ref]["state"] = "landed"
        record("OK")
        # Python buffers the standard output of a background call. The
        # flush writes the result to the output before next runs, so the
        # result stays there when the caller stops the command during next.
        print(
            f"RESULT {ref} OK {out('git', 'log', '-1', '--format=%h %s', cwd=repo)}",
            flush=True,
        )

    # Every landing that proposes a pull request leaves the main checkout
    # alone. So neither the branch nor the changes of the main checkout
    # stop it.
    pr = proposes(cfg)
    with locked(run / "land.lock"):
        if not pr:
            if out("git", "branch", "--show-current", cwd=repo) != base:
                fail("NOTBASE", f"the main checkout is not on {base}")
            if dirty := out("git", "status", "--porcelain", cwd=repo):
                fail("DIRTY", dirty)
        if not Path(wt).exists():
            fail("NOWORKTREE", wt)
        if dirty := out("git", "status", "--porcelain", cwd=wt):
            fail("WTDIRTY", dirty)
        if not msg.exists() or not msg.read_text().strip():
            fail("NOMSG", str(msg))
        if found := refs.findall(msg.read_text()):
            fail("MSGHASREF", " ".join(sorted(set(found))))
        require_health("before the merge")

        # Bring the base branch into the task branch, so that the check
        # runs on the tree that the commit or the pull request records.
        behind = sh(
            "git", "merge-base", "--is-ancestor", base, br, cwd=repo, check=False
        )
        if behind.returncode:
            # The default message of a merge names the task branch, and
            # that name holds the task ref. A landing that proposes a pull
            # request leaves the merge commit for the push.
            merge = ("git", "merge", "--no-verify", "--no-edit")
            rc, log = logged(
                "merge", *merge, "-m", f"Merge branch '{base}'", base, cwd=wt
            )
            if rc:
                files = out("git", "diff", "--name-only", "--diff-filter=U", cwd=wt)
                sh("git", "merge", "--abort", cwd=wt, check=False)
                fail("CONFLICT", files or tail(log))
        added = [
            line
            for line in out(
                "git", "diff", "-U0", f"{base}...{br}", cwd=repo
            ).splitlines()
            if line.startswith("+") and not line.startswith("+++") and refs.search(line)
        ]
        if added:
            fail("REFINDIFF", "\n".join(added[:20]))
        # The task branch holds the base branch here, and a pull request
        # needs a change to show. A landing that commits finds an empty
        # change when it stages the commit.
        if pr:
            same = sh("git", "diff", "--quiet", base, br, cwd=repo, check=False)
            if not same.returncode:
                fail("EMPTY", f"{br} holds no change against {base}")
            # The push publishes each commit of the task branch with its
            # message. Each text holds the short ID of one commit and its
            # message.
            texts = out("git", "log", "-z", "--format=%h %B", f"{base}..{br}", cwd=repo)
            named = [t.splitlines()[0] for t in texts.split("\0") if refs.search(t)]
            if named:
                fail("LOGHASREF", "\n".join(named[:20]))
            # An earlier landing of the task named its branch. Otherwise
            # the landing names the branch here, so that a blocked name
            # fails the landing before the check.
            head = t.get("branch")
            if not head:
                taken = known_branches(repo)
                head = pr_branch(msg.read_text(), taken)
                if held := above(head, taken):
                    fail(
                        "BRANCHTAKEN",
                        f"a local branch or a remote branch has the name {held},"
                        " and git keeps no branch below the name of another"
                        f" branch. So {held} blocks the branch {head} of the pull"
                        f" request. Rename or delete each branch {held}, and land"
                        " again",
                    )
        if cfg["check"]:
            # The cache sits in the git directory of the worktree. So the
            # worktree stays clean, and git removes the cache together
            # with the worktree.
            git_dir = out("git", "rev-parse", "--absolute-git-dir", cwd=wt)
            rc, log = timed(
                "check",
                "sh",
                "-c",
                cfg["check"],
                cwd=wt,
                env=lint_env(Path(git_dir, LINT_CACHE)),
            )
            if rc is None:
                fail("CHECKTIMEOUT", overran("check", log))
            if rc:
                require_health(f"after the check failed, see {log}")
                fail("CHECKFAIL", tail(log))
            if dirty := out("git", "status", "--porcelain", cwd=wt):
                fail("WTDIRTY", "the check changed files:\n" + dirty)
        if pr:
            propose(head)
        else:
            squash()
    # Each failed landing exits before this line. The landing lock is free
    # here, so a queued landing does not wait for the tasks that start.
    if a.next:
        fill_slots(run)


def proposed(st, ref, command):
    """The proposed task with the given ref.

    The command ends when the run holds no such task, or when the task
    has another state. Only a landing that proposes a pull request sets
    a task to proposed.
    """
    t = st["tasks"].get(ref) or die(f"unknown task {ref}")
    if t["state"] != "proposed":
        die(
            f"{ref} is {t['state']}, and the {command} command takes only a"
            " proposed task. A landing with the result PR sets its task to"
            " proposed"
        )
    return t


def cmd_opened(a):
    repo = main_checkout()
    run = run_dir(repo)
    with state(run) as st:
        t = proposed(st, a.ref, "opened")
        t["pr"], t["pushed"] = a.url, True
    print(f"{a.ref} proposed {a.url}")


def cmd_merged(a):
    repo = main_checkout()
    run = run_dir(repo)
    with state(run) as st:
        t = proposed(st, a.ref, "merged")
        name, base = branch(t, a.ref), st["config"]["base"]
        # A task whose branch is gone has no change to compare.
        has = branch_exists(repo, name)
        if has and not a.force and not holds(repo, base, name):
            die(
                f"{base} lacks the change of {a.ref}. A merge of {name} into"
                f" {base} has a conflict or changes a file, and so does a merge"
                f" into each commit that {base} gained after the fork of {name}."
                " The command changed nothing. Bring the merged pull request"
                f" into {base} first, such as with a pull while the main"
                f" checkout has {base} checked out. Pass --force when the pull"
                f" request merged with commits that {name} lacks"
            )
        # The branch goes before the aven task is done, as in a landing
        # that commits. A command that fails between the two steps then
        # leaves a task without a branch, and the next merged command
        # finishes it.
        if has:
            gone = sh("git", "branch", "-q", "-D", name, cwd=repo, check=False)
            if gone.returncode:
                die(
                    f"git cannot delete the branch {name}, so the command changed"
                    f" nothing:\n{gone.stderr.strip()}\nA worktree that has"
                    f" {name} checked out keeps the branch. Check out another"
                    " branch there, and run the command again"
                )
        sh("aven", "edit", a.ref, "--status", "done", cwd=repo)
        t["state"] = "landed"
    print(f"{a.ref} landed")


def target(text):
    """The value of the --target flag of init."""
    kind, sep, named = text.partition(":")
    if not text or (kind == PR and sep and not named):
        raise argparse.ArgumentTypeError(
            f"give a branch, {PR}, or {PR}:BRANCH in place of {text!r}"
        )
    return text


def percent(text):
    """The value of a flag that takes a percent."""
    value = float(text)
    if not 0 <= value <= 100:
        raise argparse.ArgumentTypeError("give a percent between 0 and 100, inclusive")
    return value


def seconds(text):
    """The value of a flag that takes a time span in seconds."""
    value = float(text)
    if not 0 < value < math.inf:
        raise argparse.ArgumentTypeError(
            "give a finite number of seconds greater than 0"
        )
    return value


def disk_path(text):
    """The absolute path of a --disk-path flag, which must exist."""
    path = os.path.abspath(text)
    if not os.path.exists(path):
        raise argparse.ArgumentTypeError(f"the path {path} does not exist")
    return path


def floor_percent(text):
    """The disk path and the percent of a --disk-floor-percent flag.

    The text is PATH=N, and the last "=" ends the path.
    """
    path, sep, n = text.rpartition("=")
    if not sep or not path:
        raise argparse.ArgumentTypeError(
            f"give PATH=N, such as /var/lib/docker=25, in place of {text}"
        )
    return disk_path(path), percent(n)


def role_model(text):
    """The role and the model of a --model flag.

    The text is ROLE=MODEL, and the first "=" ends the role. The model
    is empty when the flag removes the model of the role.
    """
    role, sep, model = text.partition("=")
    model = model.strip()
    if not sep or role not in (*ROLES, DEFAULT_ROLE) or len(model.split()) > 1:
        raise argparse.ArgumentTypeError(
            f"give ROLE=MODEL, such as reviewer=claude-opus-5-5, in place of"
            f" {text!r}. ROLE is one of {', '.join((*ROLES, DEFAULT_ROLE))}, and"
            " MODEL holds no space"
        )
    return role, model


MODEL_HELP = (
    "the agents of ROLE in close-task.js run on MODEL. ROLE is one of"
    f" {', '.join(ROLES)}, or {DEFAULT_ROLE} for each role that has no model"
    " of its own. The agents of a role without a model follow the session"
    " model"
)
FLOOR_GB_HELP = "a watched file system is low when fewer than N GB of it are free"
FLOOR_PERCENT_HELP = (
    "the file system that holds PATH is low when less than N percent of it is"
    " free, and the other file systems keep their floors. Give a PATH on the"
    " file system of a build cache, and give an N of more than the share of"
    " the disk that the cache keeps free. An N of more than 0 adds PATH to the"
    " disk paths"
)
TARGET_HELP = (
    "where the tasks of the run land. A branch makes each landing squash its"
    " task into one commit on that branch, which the main checkout must have"
    f" checked out. {PR} makes each landing propose its task as a pull request"
    f" against {TARGET}, and {PR}:BRANCH as a pull request against BRANCH. The"
    " default is %(default)s"
)
DISK_PATH_HELP = (
    "also watch the file system that holds PATH, such as the directory of a build cache"
)
STEP_TIMEOUT_HELP = (
    "a landing stops its check, and each attempt of its commit, when the step"
    " runs longer than SECONDS. Give more than the time that the slowest step"
    " of a healthy landing takes"
)


def main():
    p = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    sub = p.add_subparsers(dest="cmd", required=True)

    s = sub.add_parser("init")
    s.add_argument(
        "--run",
        required=True,
        metavar="DIR",
        help="the directory of the new run. When DIR holds an earlier run, the"
        " run takes the next free directory beside it, such as DIR-2",
    )
    s.add_argument(
        "--target", type=target, default=TARGET, metavar="TARGET", help=TARGET_HELP
    )
    s.add_argument("--limit", type=int, default=5)
    s.add_argument("--rounds", type=int, default=6)
    s.add_argument(
        "--model",
        action="append",
        default=[],
        type=role_model,
        metavar="ROLE=MODEL",
        help=MODEL_HELP,
    )
    s.add_argument("--check", default="")
    s.add_argument(
        "--health",
        default="",
        metavar="CMD",
        help="a fast command that fails when a service of the check or a hook is down",
    )
    s.add_argument(
        "--step-timeout",
        type=seconds,
        default=STEP_TIMEOUT,
        metavar="SECONDS",
        help=f"{STEP_TIMEOUT_HELP}. The default is %(default)s",
    )
    s.add_argument(
        "--shared",
        action="append",
        metavar="GLOB",
        help=f"add a glob to the default test-file globs. {GLOB_RULE}",
    )
    s.add_argument(
        "--disk-floor-gb", type=float, default=30, metavar="N", help=FLOOR_GB_HELP
    )
    s.add_argument(
        "--disk-floor-percent",
        action="append",
        default=[],
        type=floor_percent,
        metavar="PATH=N",
        help=FLOOR_PERCENT_HELP,
    )
    s.add_argument(
        "--disk-path",
        action="append",
        default=[],
        type=disk_path,
        metavar="PATH",
        help=DISK_PATH_HELP,
    )
    s.add_argument(
        "--force",
        action="store_true",
        help="start an empty run in place of a run that this session continues"
        " or whose unfinished tasks init moves, and take the --run directory"
        " when it holds the tasks directory of an earlier run and no state.json",
    )
    s.set_defaults(fn=cmd_init)

    # A flag that the command line omits leaves its config value as it is.
    s = sub.add_parser("config")
    s.add_argument("--limit", type=int)
    s.add_argument("--rounds", type=int)
    s.add_argument(
        "--model",
        action="append",
        default=[],
        type=role_model,
        metavar="ROLE=MODEL",
        help=f"{MODEL_HELP}. An empty MODEL removes the model of ROLE",
    )
    s.add_argument("--check", help="an empty command removes the check")
    s.add_argument(
        "--health",
        metavar="CMD",
        help="an empty command removes the health command",
    )
    s.add_argument(
        "--step-timeout", type=seconds, metavar="SECONDS", help=STEP_TIMEOUT_HELP
    )
    s.add_argument(
        "--shared",
        action="append",
        default=[],
        metavar="GLOB",
        help=f"add a glob to the shared globs. {GLOB_RULE}",
    )
    s.add_argument(
        "--unshared",
        action="append",
        default=[],
        metavar="GLOB",
        help="remove a glob from the shared globs",
    )
    s.add_argument("--disk-floor-gb", type=float, metavar="N", help=FLOOR_GB_HELP)
    s.add_argument(
        "--disk-floor-percent",
        action="append",
        default=[],
        type=floor_percent,
        metavar="PATH=N",
        help=f"{FLOOR_PERCENT_HELP}. An N of 0 removes the percent floor of PATH",
    )
    s.add_argument(
        "--disk-path",
        action="append",
        default=[],
        type=disk_path,
        metavar="PATH",
        help=DISK_PATH_HELP,
    )
    s.add_argument(
        "--no-disk-path",
        action="append",
        default=[],
        type=os.path.abspath,
        metavar="PATH",
        help=(
            "remove PATH and its percent floor from the disk paths. The run"
            " still watches the file system of PATH while another disk path"
            " sits on it"
        ),
    )
    s.set_defaults(fn=cmd_config)

    s = sub.add_parser("triage")
    s.add_argument("file")
    s.set_defaults(fn=cmd_triage)

    # The untriaged command takes the arguments that the parser does not
    # know, because each one belongs to aven list.
    s = sub.add_parser(
        "untriaged",
        usage="%(prog)s [-h] [FILTER]...",
        description="Print the open aven tasks that the run does not hold."
        " Each FILTER is an argument of aven list, such as --project app or"
        " --label bug. Without --project, aven list keeps to the project"
        " that aven infers from the main checkout, and it covers every"
        " project when aven infers none. With --limit N, the command prints"
        " at most N rows, and the tasks that the run holds do not count"
        " toward N.",
    )
    s.set_defaults(fn=cmd_untriaged)

    sub.add_parser("next").set_defaults(fn=cmd_next)

    s = sub.add_parser("args")
    s.add_argument("ref")
    s.add_argument("--round", type=int)
    s.add_argument("--final", action="store_true")
    s.add_argument("--sync")
    s.set_defaults(fn=cmd_args)

    s = sub.add_parser("set")
    s.add_argument("ref")
    s.add_argument("state", choices=STATES)
    s.add_argument("--flag")
    s.set_defaults(fn=cmd_set)

    s = sub.add_parser("land")
    s.add_argument("ref")
    s.add_argument(
        "--next",
        action="store_true",
        help="after the result OK or PR, run next and print its list of"
        " Workflow inputs",
    )
    s.set_defaults(fn=cmd_land)

    s = sub.add_parser(
        "opened",
        description="Record the pull request of a proposed task after the"
        " push of its branch. The status command then prints that the task"
        " waits for the merge of the pull request. Run the command after each"
        " push, also when the push updates a pull request that the task"
        " records.",
    )
    s.add_argument("ref")
    s.add_argument("url", metavar="URL", help="the address of the pull request")
    s.set_defaults(fn=cmd_opened)

    s = sub.add_parser(
        "merged",
        description="Record that the pull request of a proposed task merged."
        " The command deletes the task branch, sets the aven task to done, and"
        " sets the task to landed.",
    )
    s.add_argument("ref")
    s.add_argument(
        "--force",
        action="store_true",
        help="record the merge although the base branch lacks the change of"
        " the task branch, such as for a pull request that gained commits after"
        " the push",
    )
    s.set_defaults(fn=cmd_merged)

    sub.add_parser("status").set_defaults(fn=cmd_status)

    s = sub.add_parser("watch")
    s.add_argument(
        "--interval",
        type=seconds,
        default=60,
        metavar="SECONDS",
        help="how long the command waits between two measurements",
    )
    s.set_defaults(fn=cmd_watch)

    a, extra = p.parse_known_args()
    if a.fn is cmd_untriaged:
        a.filters = extra
    elif extra:
        p.error(f"unrecognized arguments: {' '.join(extra)}")
    a.fn(a)


if __name__ == "__main__":
    main()
