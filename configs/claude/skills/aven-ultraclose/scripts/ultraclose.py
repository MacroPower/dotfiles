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
branch and its worktree. A proposed task keeps its branch. It keeps its
worktree only when the landing that proposed it could not remove the
worktree. The earlier run keeps its landed and closed tasks. The state
of the new run names the earlier run, and each run that the state of the
earlier run names, and the status command prints
each of them. The batch of the new run goes on from those runs, so its
report covers their landed tasks and their notes.md. Init does not move
the tasks while a landing of the earlier run runs. With --force, init
starts an empty run and leaves the tasks in the earlier run. Init also
leaves in the earlier run each pending task that the earlier run has not
started and that the queue filters of the new run leave out, unless a
moved task waits for it. The paragraphs on the queue filters cover those
tasks.

A noted task is still open in aven. So the untriaged command of a new
run lists each noted task that the new run does not hold and that its
queue filters let in, and a triage starts that task again. The new run
holds each noted task that init moves, so a pending task that waits for
it keeps waiting, and the status command still prints that pending task
as stuck.

The next and args commands write a copy of close-task.js into the
directory of each task that they start, and so does a landing that runs
next. The meta of the copy names its task. The name holds the ref, and
the description holds the title, so the Workflow list shows which task
each Workflow closes. The script field of each Workflow input holds the
path of the copy. Pass that path to the Workflow tool as scriptPath. The
tool refuses the script in the skill directory after Nix store
optimisation hard-links it.

The result of close-task.js names the action that the caller takes in
its action field, with the command of this script that the caller runs.
The task of a Workflow input holds the pull request that the task
records in its pr field, so that the action names that pull request.

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
that the base branch lacks, and the worktree. Each command that starts a
task creates the directory of the task first, so the branch and the
worktree of a task without a directory come from an earlier run. The
init command moves the directory of each task that a new run takes from
the run that it replaces, so the new run resumes the work of those tasks
as its own.

The leftover command moves the work of an earlier run out of the way,
and it keeps each commit and each uncommitted change of that work. It
commits the uncommitted changes of the worktree, and it removes the
worktree. Then it counts the commits of each branch, so the count
includes that commit. It deletes each branch that holds no commit that
the base branch lacks. It renames every other branch to kept/REF-DATE,
with the date of the day, and it adds a counter to a name that a branch
has. It names the kept branch and the old name of that branch in a note
on the aven task. The implementer of a new attempt reads the note in its
task file. The command also keeps the commits of a detached HEAD of the
worktree in a kept branch. After the command, run next or args again,
and run next after a landing.

An earlier run also keeps the branch of each task that a landing
proposed as a pull request, when init does not move that task to the new
run, such as with --force. That branch has another name than task/REF,
and its pull request can still be open. So init records the branch and
the pull request of each such task in the left field of the new state,
and it prints one line per task. The record also keeps each branch that
the earlier state records there and that still exists. The next and args
commands count a recorded branch as leftover work of its task, and the
line names the pull request, or says that the earlier run recorded none.
A new attempt at the task opens a pull request of its own. The script
contacts no remote. So the leftover command moves such a branch only
with the --pr flag, which gives the state of the pull request as the
caller read it: none, open, closed, or merged. The command refuses none
for a branch whose pull request the earlier run recorded. With merged,
the task does not need a new attempt. The command then sets the aven
task to done first. It deletes the branch when the base branch holds its
change, and it keeps the branch otherwise. Then it sets the task to
closed.

The set command also sets the aven task of a task that it sets to
noted, closed, or landed. A noted task gets the aven status todo, and a
closed task or a landed task gets the aven status done. The states noted
and closed require the --note flag, and the command adds the note to the
aven task. To the note of a noted task, it adds the branch of the task
and the pull request that the task records. A failed aven command ends
the command before it changes the run. After it notes a task, the
command prints a stuck line for each pending task that waits for a
noted task, as the status command does.

The set command removes the clean worktree of a task that it sets to
noted, closed, or landed. When it removes the worktree of a closed task,
it also deletes the branch. It deletes the branch of a landed task whose
worktree is gone only when the base branch holds the change of the
branch. It removes only the work of the run. So it keeps the branch and
the worktree of a task without a directory, and it prints a line that
names a kept worktree.

  init --run DIR [--target TARGET] [--limit N] [--rounds N]
       [--model ROLE=MODEL]... [--check CMD] [--health CMD]
       [--step-timeout SECONDS] [--shared GLOB]... [--disk-floor-gb N]
       [--disk-floor-percent PATH=N]... [--disk-path PATH]...
       [--status STATUS]... [--skip-status STATUS]...
       [--priority PRIORITY]... [--min-priority PRIORITY]
       [--skip-priority PRIORITY]... [--label LABEL]...
       [--skip-label LABEL]... [--repo NAME=BASE]...
       [--repo-check NAME=CMD]... [--force]
  config [--limit N] [--rounds N] [--model ROLE=MODEL]... [--check CMD]
       [--health CMD] [--step-timeout SECONDS] [--shared GLOB]...
       [--unshared GLOB]... [--disk-floor-gb N]
       [--disk-floor-percent PATH=N]... [--disk-path PATH]...
       [--no-disk-path PATH]... [--status STATUS]...
       [--skip-status STATUS]... [--any-status] [--priority PRIORITY]...
       [--min-priority PRIORITY] [--skip-priority PRIORITY]...
       [--any-priority] [--label LABEL]... [--skip-label LABEL]...
       [--any-label] [--repo NAME=BASE]... [--repo-check NAME=CMD]...
       [--no-repo NAME]...
                         change the config of the run, then print it
  triage FILE            add or update tasks from a JSON list
  untriaged [FILTER]...  print the open aven tasks that the run does not hold
  next                   start one task per free slot, print each Workflow input
  args REF [--round N] [--final] [--sync REASON]
                         print the Workflow input that relaunches REF
  set REF STATE [--flag TEXT] [--note TEXT]
                         set the state of REF, and its aven task with it
  leftover REF [--pr [NAME=]STATE]...
                         move the work that an earlier run left for REF out of
                         the way
  land REF [--next]      land REF on the target of the run, one landing at a time
  opened REF URL [--repo NAME]
                         record the push and the pull request of the proposed
                         task REF
  merged REF [--force] [--repo NAME]
                         record that the pull request of REF merged
  status
  watch [--interval SECONDS]
                         exit when free disk space runs low or the run ends

The --target flag of init names where the tasks of the run land, and the
default is main. A branch name makes that branch the base branch of the
run. Each task branch starts from the base branch, and each landing
squashes its task branch into one commit on the base branch. Every such
landing commits in the main checkout. So init refuses a branch that the
main checkout does not have checked out, and a main checkout that holds
uncommitted changes.

The target pr makes each landing propose its task as a pull request
against main. The target pr:BRANCH names another base branch for the
pull requests. Each task branch still starts from the base branch, so
each pull request carries the commits of the local base branch that the
remote lacks. Init refuses a base branch that holds a commit that its
upstream branch lacks. It compares the base branch with the
remote-tracking ref of that upstream branch. For a base branch that
follows no remote branch, it prints that it did not compare the two. A
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
such commit. The action of that result replaces the commits of the
branch with one commit whose message is msg.txt. The merge commit of a
landing names only the base branch, because the name of the task branch
holds the task ref.

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

The land command refuses a proposed task, because the task waits for
its pull request to merge, and the merged command lands it. The args
command starts the task again on its branch, such as for a pull request
that has a conflict with the base branch. The next landing of the task
keeps the name of the branch, and its RESULT line gives the pull request
that the task records.

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
as a branch that a worktree has checked out. That worktree can be the
worktree of the task, which the landing did not remove, and the error
then says to remove it. Each refusal changes nothing.

A run whose landings propose pull requests can also change other
repositories. Each --repo NAME=BASE flag of init and config adds the
repository whose main checkout is the directory NAME of the repos
directory, which the DOTFILES_REPOS_DIR variable names. Each task of
that repository starts from its branch BASE, and init and config refuse
a BASE that holds a commit that its upstream branch lacks. The
--repo-check NAME=CMD flag sets the check of the repository. The
--no-repo flag of config removes a repository that no unfinished or
noted task changes. A run whose landings commit takes no other
repository, because a landing cannot commit a task in two repositories
at once. A pin to a commit of another repository also needs the commit
that the merge of its pull request makes.

The repos field of a triage row maps the name of each other repository
that the task changes to the files that the task edits there. The
scheduler locks those files in that repository, and the shared globs
match them as they match the files of the repository of the run. The
start of such a task gives it a worktree in each of those repositories,
at .claude/worktrees/t-REF of the main checkout, on the branch task/REF.
The Workflow input names each of them in the repos field of the task.
The message of each other repository goes to msg-NAME.txt in the task
directory, beside the msg.txt of the repository of the run. A landing
reads the message of a repository only when its branch holds a change
of its own.

A landing of such a task merges the base branch into the worktree of
each repository whose branch holds a change of its own, and of each
repository that an earlier landing proposed. It proposes each of them
whose branch then holds a change against its base branch, and it runs
the check of each repository that it proposes. The worktree and the
branch of the task stay in each repository in which no landing proposed
the task, the repository of the run included, until the task lands. The
landing prints one REPO line per other repository that it proposes,
with the branch, the base branch, and the main checkout. Its RESULT line
names the branch of the repository of the run only when it proposed the
task there. The ACTION line of a failure in another repository opens
with the name of that repository, the worktree of the task there, and
its branch.

The opened and merged commands take --repo NAME for the pull request of
another repository, and the task waits as proposed until every pull
request merged. The merged command takes the pull request of the
repository of the run only after each other pull request merged, because
its change can depend on theirs, such as with a pin to a commit. A
commit of the task branch in another repository reaches a remote only
through that merge, so the agents keep each such pin at its old value.
After the merge of the last other pull request, the merged command says
how to sync the task so that each pin names a commit that holds the
merge. The text names each other repository whose merge the run
recorded, with the commit of its base branch when the run recorded the
merge.

A pull request of another repository can merge before the run records
the merge, and a fix can revert the change of the task there. The branch
there then holds no change, and the next landing of the task ends with
the result EMPTY. Its action names that repository and its pull request.
The merged command with --repo also takes such an approved task. It
removes the clean worktree of the task in that repository, deletes the
branch there, and keeps the task approved, so that the next landing
proposes the rest of the task. A landing that finds no change in the
repository of the run, after each other pull request of the task merged,
ends with EMPTY too, and its action says that the task has landed.

A landing sets its task to approved before it waits for the landing
lock, and it reads the worktrees of the task after it takes the lock. So
the merged command takes an approved task only while it holds the
landing lock, and it refuses such a task while a landing holds that
lock. It takes a proposed task whatever holds the lock, because the land
command refuses a proposed task.

The user can also merge the pull request of the repository of the run
early, and a fix can revert the change there. When the branch there
then holds no change and another repository still holds one, the landing
proposes the other repositories. It removes the worktree of the task in
the repository of the run, and the task keeps the branch and the pull
request there. The action names that pull request. It says to close the
pull request when it is open, to push nothing to it, and to run the
merged command for it after the other pull requests merge. The status
command says that the branch holds no change, in place of a wait for its
push or its merge, and it repeats those steps. The merged command then
lands the task. The merged command for the last other pull request says
so too, when the base branch of the run holds the change of that branch.

The set, args, next, leftover, and init commands cover the worktrees and
the branches of the task in each other repository as they cover those of
the repository of the run. Init records the branch of each pull request
of another repository for a task that stays in an earlier run. The
leftover command then takes one --pr NAME=STATE flag per such branch,
and it closes the task only when every pull request of the task merged.
Init moves a task of another repository to a new run only when the new
run holds that repository with the same main checkout and base branch.

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
run does not hold and that the queue filters of the run let in, as a
JSON list. The list leaves out every epic. Each row holds the ref, the
title, and the priority of its task. Add the files of the task to the
row before you load it with triage.

The queue filters of a run narrow its queue by status, by priority, and
by label. Init sets them with its filter flags, and the run config keeps
them, so every later command and session of the run applies them. The
status command prints them as the flags that set them. Each filter flag
takes one value. Each flag except --min-priority adds a value when it
repeats, and a repeated --min-priority keeps its last value:

- --status, --priority, and --label let into the queue only the tasks
  that carry the value of one of their flags.
- --skip-status, --skip-priority, and --skip-label leave out each task
  that carries the value of one of their flags.
- --min-priority lets in only the tasks of its priority or higher. From
  low to high, the priorities are none, low, medium, high, and urgent.

A task joins the queue when it passes every filter. The status flags
take only the status of an open task, because the queue holds only open
tasks. Init and config refuse a label that aven does not know, because
a misspelled --skip-label skips no task. They also refuse the filters of
a dimension that let no task in, such as --status todo --skip-status
todo. The filter flags of one dimension in a config command replace
every filter of that dimension, and each other dimension keeps its
filters. The --any-status, --any-priority, and --any-label flags of
config remove the filters of their dimension.

aven list takes one value of --status, --priority, and --label, and it
has no filter that leaves a value out. So the untriaged command applies
the queue filters itself, and it refuses a --status, --priority, or
--label FILTER. The triage command applies the filters to each task that
the run does not hold yet, such as a task that the run filed during the
run. It asks aven for the status, the priority, and the labels of each
such task, and it refuses the whole list when the filters leave out one
of those tasks. The run keeps each task that it holds, so a later change
of the filters removes no task from the run.

The next command waits only for the tasks that the run holds. So the
triage command also refuses a task when one of its open aven blockers
sits outside the run and the queue filters keep that blocker from the
run. The filters keep such a blocker from the run when they leave out
the blocker, or a task on the chain of its own open aven blockers. The
chain ends at each task that the run holds. The triage command refuses
such a blocker too, so it never joins the run, and the task otherwise
starts before its blocker lands.

The refusal covers each task that joins the run and each pending task
that the run holds. When the triage command refuses a task of the list
that joins the run, it also refuses each task of the list that waits for
that task. The
untriaged command still lists a refused task that joins. A pending task
that the run holds stays pending after the refusal, so set it to noted,
or let its blocker in with config.

When init replaces a run that has unfinished tasks, it applies the
queue filters of the new run to each pending task that the earlier run
has not started. It asks aven for the status, the priority, the labels,
and the open blockers of each such task. It leaves such a task in the
earlier run when the filters leave out the task. It also leaves the
task when one of its open blockers sits outside the earlier run and the
filters keep that blocker from the new run, as the triage command judges
such a blocker. Init prints each task that it leaves with the reasons.
The task stays open in aven, and a later untriaged command lists it when
the filters let it in.

Init moves every other unfinished task and each noted task of the
earlier run, whatever the filters. Each started task holds work or a
result of the batch, such as a pending task that the earlier run
started and set back to pending. The start of a task also sets its aven
status to active, so the status filters cannot judge a started task.

Init also moves each pending task that the filters leave out when a
moved task waits for it, so that the task lands first. Every task waits
for the tasks of its after list. Each pending task that init asks aven
about also waits for its open aven blockers, and init adds them to its
after list, as the triage command does. Init prints each task that it
moves for the order with the reasons too. When those reasons name a
blocker that the task waits for, next starts the task before that
blocker lands, so set the task to noted, or let the blocker in with
config.

Each FILTER of untriaged is an argument of aven list, such as
--project app or --has-metadata review. Without --project, aven list
keeps to the project that aven infers from the main checkout, and it
covers every project when aven infers none. With --limit N, untriaged
prints at most N rows, and the tasks that the run holds or that the
queue filters leave out do not count toward N.

The land command takes only an inflight task or an approved task. It
sets the task to approved before the landing starts, and a failed
landing leaves the task approved, unless its result is POSTDIRTY. The
command refuses a task in each other state, because that task has no
change that waits for a landing. Each refusal prints an error and no
RESULT line, and it changes nothing. So a second landing of a landed
task leaves the task landed, and the run still ends.

Each landing waits for the landing lock while another landing runs. The
land command checks the state of the task again after it takes the lock.
So it also refuses a task that another landing of that task landed or
proposed during the wait.

The land command prints the failing output of a failed landing. The last
line that a failed landing prints is the RESULT line. With --next, a
landing with the result OK or PR runs the next command after it prints
the RESULT line. The list of Workflow inputs that next prints follows
the RESULT line, and the list is empty when no task starts. After a
failed landing, --next does not run next. When next fails after the
landing, its error follows the RESULT line, and the command exits with a
failure status.

A landing that commits reads the changes of the main checkout after the
commit. When the checkout holds a change that the commit did not record,
such as a file that a commit hook left behind, the landing fails with
the result POSTDIRTY. The base branch holds the commit of the task by
then. So the landing first removes the worktree and the branch, sets the
aven task to done, and sets the task to landed, as a landing with the
result OK does. Its failing output names the commit and lists the
changes of the main checkout.

A landing that commits ends with the commands that remove the worktree,
delete the branch, and set the aven task to done. Each of these commands
can fail, such as git with a locked worktree and aven with a locked
database. git also keeps the branch of a worktree that it did not
remove. The base branch holds the commit by then, so the task still
lands with the result OK or POSTDIRTY. After such a landing with the
result OK, --next runs next after the RESULT line, and the land command
then exits with a failure status.

Before the TIMES line, the landing prints each failed command with its
output. One more line per command says what the command left, which is
the worktree, the branch, or the open aven task. The caller repairs the
cause of the error and runs the command again. git can also fail after
it deleted its record of the worktree, such as when it cannot delete a
file in the worktree. Only the directory of the worktree then stays,
and a second run of the git command fails. So the line for that failure
says that git no longer lists the worktree, and the caller deletes the
directory.

A landing that proposes a pull request ends with the git command that
removes the worktree. The task branch has its name by then. So when that
command fails, the landing prints it the same way above the RESULT line,
sets the task to proposed, and prints the result PR. With --next, it
then runs next, and the land command exits with a failure status. The
merged command cannot delete the branch while the worktree has the
branch checked out, so the caller removes the worktree before it runs
the merged command.

Each landing prints an ACTION line right before its RESULT line. The
line gives the action that the caller takes for the result, with the
command that the caller runs. The command quotes each path and each
branch name in it for a shell, so the caller runs the command as the
line gives it, whatever characters those names hold. The action names
the cause of the result where the run shows it:

- The run counts the failed landings of each task by cause. So the
  action changes when a task fails again for the same cause, such as a
  second HEALTHFAIL after a failed step, or a third CONFLICT after two
  syncs.
- A landing reads the log of a failed check or a failed commit. When a
  line of the log names a lack of memory or a full disk, the output
  quotes that line. The action then says what to do when that line shows
  why the step failed, and what to do when the line is only part of the
  output of a failed test.
- A landing records the commit that it adds to the base branch before
  it removes the worktree. So a landing that finds no worktree, or no
  change to commit, says when an earlier landing of the task left that
  commit on the base branch, such as a landing that a signal ended. The
  set command then sets the task to landed.
- A landing whose git command or aven command failed at its end gives
  the repair of each failed command, in the order of the output. The
  repair says what the command left, which is the worktree, the
  directory of the worktree, the branch, or the open aven task, and it
  gives the command that the caller runs.
- A landing that finds no worktree says whether the branch of the task
  still exists.
- A landing that finds no change to commit says whether the task branch
  held a change of its own before the landing merged the base branch.

The action of a failed check, of a failed commit, and of a stopped step
names the Landing section of SKILL.md when only the failing output shows
the cause.

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
earlier landings with the result OK or PR. A SLOW line follows the TIMES
line for each step that took more than twice its median, and more than a
minute longer than its median. The status command prints the medians of
the run and the times of its last landings, so a slow check or a slow
hook shows beside the usual time.

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
The ACTION line then starts with the repair of the main checkout.

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
import shlex
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
# The states of a task that the land command takes. An inflight task has
# the change that its agents wrote, and an approved task waits for its
# landing. No task in another state has a change that waits for a landing.
LANDABLE = ("inflight", "approved")
PRIORITY = {"urgent": 0, "high": 1, "medium": 2, "low": 3, "none": 4}
# The aven statuses of an open task.
AVEN_OPEN = ("inbox", "backlog", "todo", "active")
# The dimensions of the queue filters of a run, each with the values that
# its flags take. The queue holds only open tasks, so the status flags take
# only an open status. The priorities run from low to high. The label
# flags take each label that aven knows, so the label dimension lists no
# values.
FILTERS = {
    "status": AVEN_OPEN,
    "priority": tuple(reversed(PRIORITY)),
    "label": (),
}
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
# What a landing tells its caller to do about a git command that failed
# after the landing committed or named the branch of its task.
AGAIN = "Repair the cause of the error, and run the git command again."
# The key of the git config of the repository that names the run directory.
KEY = "ultraclose.run"
# The environment variable in which Claude Code names the session of each
# command that it runs.
SESSION = "CLAUDE_CODE_SESSION_ID"
# The environment variable that names the directory that holds the
# repositories of the host. home/claude.nix sets it to dotfiles.reposDir.
# Each --repo flag names another repository by its directory there.
REPOS_DIR = "DOTFILES_REPOS_DIR"
# The name of another repository of a run, which is the name of its
# directory in the repos directory. The name opens with a letter or a
# digit, so it names neither the repos directory nor its parent.
REPO_NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]*")
# The states that the --note flag of the set command applies to. The
# command adds the note to the aven task of a task that it sets to one
# of them.
NOTING = ("noted", "closed")
# The aven status of a task that the set command sets to each of these
# states.
AVEN_STATUS = {"noted": "todo", "closed": "done", "landed": "done"}
# The states that the --pr flag of the leftover command takes. The flag
# gives the state of the pull request of a branch that an earlier run
# proposed.
PR_STATES = ("none", "open", "closed", "merged")
# The name of the branch that keeps the commits of an earlier run as a
# reference, before the ref of its task and the date.
KEPT = "kept"
# How many seconds longer than its median a timed step of a landing takes
# before the landing prints a SLOW line. The step also takes more than
# twice its median.
SLOW = 60
# The causes that a landing names for a failed check or a failed commit,
# each with the pattern of the log lines that show it. Each cause is a
# failure of the environment and not a fault in the change. A kernel that
# runs out of memory kills a process. Tools report the kill as Killed, as
# signal: killed, or as the exit status 137.
ENVIRONMENT = (
    (
        "a lack of memory",
        re.compile(
            r"\bKilled\b|signal: killed|\bexit(?:ed with)? (?:status|code):? 137\b"
            r"|\bError 137\b|Cannot allocate memory"
        ),
    ),
    ("a full disk", re.compile(r"No space left on device")),
)


def die(msg):
    sys.exit(f"ultraclose: {msg}")


def sh(*cmd, cwd, check=True):
    p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, check=False)
    if check and p.returncode:
        die(f"{' '.join(cmd)} failed in {cwd}:\n{p.stdout}{p.stderr}")
    return p


def out(*cmd, cwd):
    return sh(*cmd, cwd=cwd).stdout.strip()


def aven_note(repo, ref, text):
    """Add a note to the aven task of a ref.

    The text goes to aven on standard input, so aven reads no part of it
    as an option.
    """
    cmd = ["aven", "note", ref, "--stdin"]
    p = subprocess.run(
        cmd, cwd=repo, input=text, capture_output=True, text=True, check=False
    )
    if p.returncode:
        die(f"{' '.join(cmd)} failed in {repo}:\n{p.stdout}{p.stderr}")


def uncommitted(cwd):
    """The uncommitted changes of a checkout, as git status --porcelain prints them.

    The text is empty for a clean checkout. Each line starts with the two
    status columns of its file. Git prints a space in the first column
    for a change that is not staged, so the text keeps the leading space
    of its first line.
    """
    return sh("git", "status", "--porcelain", cwd=cwd).stdout.rstrip("\n")


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
    new run lists each noted task that the new run does not hold and that
    its queue filters let in, and a triage starts that task again. A
    pending task that waits for a noted task that the new run does not
    hold also starts, because next waits only for the tasks that the run
    holds.

    The refs include each pending task that the queue filters of the new
    run leave out. Init asks aven about each such task that the earlier
    run has not started, and take_over() leaves the tasks of staying() in
    the earlier run.
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
def tried(path):
    """Take the lock of a path without a wait, and yield whether the block holds it.

    The block runs either way. When another process holds the lock, the
    block runs without it.
    """
    with open(path, "w") as f:
        try:
            fcntl.flock(f, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            took = False
        else:
            took = True
        yield took


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


def other_repos(cfg):
    """The other repositories of a run, by name.

    Each entry holds the main checkout of the repository as path, its
    base branch as base, and its check as check. A run can continue from
    a state file that has no repos key. That run has no other repository.
    """
    return cfg.get("repos", {})


def other_worktree(path, ref):
    """The worktree of a task in another repository, whose main checkout is path."""
    return f"{path}/.claude/worktrees/t-{ref}"


def message_name(name):
    """The file of a task directory that holds the message of one repository.

    The name is the name of another repository of the run, or empty for
    the repository of the run.
    """
    return f"msg-{name}.txt" if name else "msg.txt"


def task_repos(cfg, t, ref):
    """The repositories of a task whose change has not merged, each with its facts.

    The first entry stands for the repository of the run. Each later
    entry stands for another repository of the run that the triage row
    of the task names, in the order of the names. The function leaves out
    each other repository whose pull request merged, because its base
    branch holds the change.

    Each entry holds the name of the repository, which is empty for the
    repository of the run, and its main checkout as repo. It also holds
    the base branch, the worktree of the task, the branch of the task,
    the file of the task directory that holds the message, and the check
    of that repository. The pr value holds the pull request that the task
    records there, or an empty text. The proposed value says whether a
    landing proposed the task there, which renames the branch. The record
    value is the part of the task that records the branch, the pull
    request, and the push: the task itself for the repository of the run,
    and the entry of the repository in the repos field of the task
    otherwise.
    """
    found = [
        {
            "name": "",
            "repo": cfg["repo"],
            "base": cfg["base"],
            "wt": worktree(cfg, ref),
            "branch": branch(t, ref),
            "msg": message_name(""),
            "check": cfg["check"],
            "pr": t.get("pr", ""),
            "proposed": "branch" in t,
            "record": t,
        }
    ]
    held = other_repos(cfg)
    for name, entry in sorted(t.get("repos", {}).items()):
        if entry.get("merged"):
            continue
        if name not in held:
            die(
                f"{ref} changes the repository {name}, which the run does not hold."
                f" Add it with config --repo {name}=BASE"
            )
        found.append(
            {
                "name": name,
                "repo": held[name]["path"],
                "base": held[name]["base"],
                "wt": other_worktree(held[name]["path"], ref),
                "branch": entry.get("branch") or task_branch(ref),
                "msg": message_name(name),
                "check": held[name].get("check", ""),
                "pr": entry.get("pr", ""),
                "proposed": "branch" in entry,
                "record": entry,
            }
        )
    return found


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


def repos_refusal(st, repos):
    """Why init keeps the unfinished tasks of a run from a run with the repositories.

    Each task that init moves keeps its worktrees and its branches in the
    other repositories that it changes, and each of them starts from the
    base branch of its repository. So the new run must hold each such
    repository with the same main checkout and the same base branch. The
    repos hold the other repositories of the new run. The result names
    the flags that the new run lacks, and it is None when the new run
    holds every such repository.
    """
    unfinished, noted = moving(st["tasks"])
    held = other_repos(st["config"])
    missing = {}
    for ref in unfinished + noted:
        for name in st["tasks"][ref].get("repos", {}):
            # A task of a repository that the run does not hold names no
            # main checkout, so the flag names the repository alone.
            was = held.get(name, {})
            new = repos.get(name, {})
            if was.get("path") != new.get("path") or was.get("base") != new.get("base"):
                missing[name] = was.get("base", "BASE")
    if not missing:
        return None
    flags = " ".join(f"--repo {name}={base}" for name, base in sorted(missing.items()))
    return f"the new run lacks the flags {flags}"


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


def commits_ahead(p):
    """How many commits the task branch holds that its base branch lacks.

    The p argument names one repository of the task, as task_repos()
    gives it. A branch that does not exist holds no commit.
    """
    if not branch_exists(p["repo"], p["branch"]):
        return 0
    count = ("git", "rev-list", "--count", f"{p['base']}..{p['branch']}")
    return int(out(*count, cwd=p["repo"]))


def listed_worktrees(repo):
    """The worktrees that git lists for a repository, or None when git fails.

    Map the real path of each worktree to the name of the branch that the
    worktree has checked out. A worktree with a detached HEAD maps to
    None. The map holds real paths, so a caller looks up the real path of
    a worktree. git keeps no record of a directory whose worktree it
    removed in part, so the map leaves out that directory.
    """
    p = sh("git", "worktree", "list", "--porcelain", cwd=repo, check=False)
    if p.returncode:
        return None
    found, path = {}, None
    for line in p.stdout.splitlines():
        key, _, value = line.partition(" ")
        if key == "worktree":
            path = os.path.realpath(value)
            found[path] = None
        elif key == "branch":
            found[path] = value.removeprefix("refs/heads/")
    return found


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


def task_files(task):
    """The files that the triage row of a task names, each with its repository.

    Each file is a pair of the name of its repository and its path
    relative to that repository. The name is empty for the repository of
    the run, so one path in two repositories gives two files.
    """
    found = [("", f) for f in task["files"]]
    for name, entry in sorted(task.get("repos", {}).items()):
        found += [(name, f) for f in entry["files"]]
    return found


def locks(cfg, task):
    return {
        (name, f)
        for name, f in task_files(task)
        if not any(matches(g, f) for g in cfg["shared"])
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
    files = {f for t in tasks.values() for _, f in task_files(t)}
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
    # The relaunch of a proposed task works on the branch of the pull
    # request that the task records. The result of close-task.js then
    # names that pull request in its action.
    if t.get("pr"):
        task["pr"] = t["pr"]
    # A task that changes other repositories has a worktree and a branch
    # in each of them. The agents of close-task.js work in every worktree,
    # and each repository has a message file of its own.
    if others := task_repos(cfg, t, ref)[1:]:
        task["repos"] = [
            {
                "name": p["name"],
                "repo": p["repo"],
                "base": p["base"],
                "wt": p["wt"],
                "branch": p["branch"],
                "msg": str(run / "tasks" / ref / p["msg"]),
                **({"pr": p["pr"]} if p["pr"] else {}),
            }
            for p in others
        ]
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


def dimension_reasons(dim, f, carried):
    """Why the filters of one dimension of the queue leave out a task.

    The filters f hold the values of the flags of the dimension: "only"
    for --DIM, "skip" for --skip-DIM, and "min" for --min-priority. The
    carried list holds what the task carries in the dimension: its
    status, its priority, or each of its labels. The result is empty when
    the filters let the task in.
    """
    found = []
    if f.get("only") and not set(carried) & set(f["only"]):
        if dim == "label":
            found.append(f"it carries no label {' or '.join(f['only'])}")
        else:
            found.append(
                f"its {dim} {' '.join(carried)} is not {' or '.join(f['only'])}"
            )
    if skipped := [v for v in f.get("skip", []) if v in carried]:
        found.append(f"the run skips the {dim} {' and '.join(skipped)}")
    rank = FILTERS["priority"].index
    if (least := f.get("min")) and not any(
        v in FILTERS["priority"] and rank(v) >= rank(least) for v in carried
    ):
        found.append(f"its priority {' '.join(carried)} is lower than {least}")
    return found


def left_out(filters, task):
    """Why the queue filters of a run leave out an aven task.

    The task holds the fields that aven list --json and aven show --json
    print. The result holds one reason per filter that the task fails,
    and it is empty when the task passes every filter.
    """
    carried = {
        "status": [task["status"]],
        "priority": [task["priority"]],
        "label": task.get("labels") or [],
    }
    return [
        reason
        for dim in FILTERS
        for reason in dimension_reasons(dim, filters.get(dim, {}), carried[dim])
    ]


def filters_text(filters):
    """The queue filters of a run, as the flags of init that set them.

    The text is empty for a run without a filter.
    """
    flags = []
    for dim in FILTERS:
        f = filters.get(dim, {})
        flags += [f"--{dim} {shlex.quote(v)}" for v in f.get("only", [])]
        if "min" in f:
            flags.append(f"--min-{dim} {f['min']}")
        flags += [f"--skip-{dim} {shlex.quote(v)}" for v in f.get("skip", [])]
    return " ".join(flags)


def queue_filters(held, a):
    """The queue filters of a run after the filter flags of init or config.

    The flags of a dimension replace every filter of that dimension, and
    the --any flag of config removes them. Each other dimension keeps its
    filters. The command ends when the flags of a dimension let no task
    into the queue, such as --status todo --skip-status todo, because the
    run then closes no task.
    """
    filters = dict(held)
    for dim, values in FILTERS.items():
        named = {
            "only": list(dict.fromkeys(getattr(a, dim))),
            "skip": list(dict.fromkeys(getattr(a, f"skip_{dim}"))),
            "min": getattr(a, f"min_{dim}", None),
        }
        named = {k: v for k, v in named.items() if v}
        if getattr(a, f"any_{dim}", False):
            if named:
                die(
                    f"--any-{dim} removes the {dim} filters of the run, and"
                    f" {filters_text({dim: named})} sets them. Pass one or the other"
                )
            filters.pop(dim, None)
            continue
        if not named:
            continue
        # A task carries one status and one priority, and any set of
        # labels. A task with no label, or with one label of --label,
        # passes the label filters whenever any task does.
        tries = [[v] for v in values] or [[v] for v in named.get("only", [])] or [[]]
        if all(dimension_reasons(dim, named, carried) for carried in tries):
            die(
                f"the {dim} filters {filters_text({dim: named})} let no task into"
                " the queue"
            )
        filters[dim] = named
    return filters


def check_labels(repo, a):
    """End the command when a label flag names a label that aven does not know.

    The queue filters apply each label themselves, so a misspelled
    --skip-label skips no task, and a misspelled --label lets no task in.
    The command asks aven for its labels only when a flag names a label.
    """
    named = list(dict.fromkeys(a.label + a.skip_label))
    if not named:
        return
    known = json.loads(out("aven", "label", "list", "--json", cwd=repo))
    if unknown := [name for name in named if name not in known]:
        die(
            f"aven knows no label {' '.join(unknown)}. The labels of aven are"
            f" {' '.join(known) or 'none'}. Name a label that aven knows"
        )


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


def staying(tasks, run, rejected, fresh):
    """The pending tasks that stay in a run that a new run replaces.

    The rejected refs name the pending tasks that the queue filters of
    the new run leave out, as refusals() returns them. Such a task stays
    in the run when it is still pending and the run has not started it,
    as owns() says. A started task holds the work of the batch, so it
    moves whatever the filters. A task also moves when a task that moves
    waits for it, so that it lands before that task. The next command
    waits only for the tasks that the run holds.

    Every task waits for the tasks of its after list and of its list in
    fresh. The fresh lists hold, by ref, the open aven blockers that init
    read for the pending tasks that the run has not started. The after
    list of a task lacks each blocker that aven recorded after the
    triage of the task.

    Return the refs that stay and the refs that move only because a task
    that moves waits for them.
    """
    unfinished, noted = moving(tasks)
    stay = {
        ref
        for ref in rejected
        if ref in unfinished and tasks[ref]["state"] == "pending" and not owns(run, ref)
    }
    waited = set()
    while needed := stay & {
        a
        for r in unfinished + noted
        if r not in stay
        for a in tasks[r]["after"] + fresh.get(r, [])
    }:
        stay -= needed
        waited |= needed
    return stay, waited


def take_over(repo, old, run, new, rejected, fresh):
    """Write the state of a new run that takes the unfinished tasks of an earlier run.

    The function moves the entry of each task that moving() names from
    the state of the earlier run to the new state. Those tasks are the
    unfinished tasks and the noted tasks. It also moves the directory of
    each task to the new run directory. The directory records that a run
    created the branch and the worktree of the task, as owns() says. So
    the new run resumes that work, and the earlier run keeps its landed
    and closed tasks.

    The rejected refs name the pending tasks that the queue filters of
    the new run leave out. Each task of staying() stays in the earlier
    run. Such a task has no task directory there, because the earlier run
    has not started it. The function reads that run under the lock,
    so a task of those refs that started since init asked aven about it
    moves. A later run that takes a task that stays counts its branch
    and its worktree as the work of an earlier run. The function writes
    the record of left_branches() for the tasks that stay into the new
    state.

    The fresh lists hold the open aven blockers of tasks by ref, as
    staying() says. The function adds the list of each task that moves to
    the after list of the task, as the triage command does. So next
    starts a blocker that moves before the task that waits for it.

    The new state names the earlier run first under replaces, and then
    each run that the state of the earlier run names there. The batch of
    the new run goes on from each of those runs. Then the function names
    the new run in the git config of the repository, and it saves the
    state of the earlier run last.

    The function holds the landing lock of the earlier run, and it ends
    the command when a landing of that run holds the lock. A landing
    reads the commit message of its task from the directory of the task,
    so a move during the landing breaks it. When a step fails, the
    function moves each directory back, names the earlier run in the git
    config again, and ends the command. When git cannot set the key back,
    the error names the git config command that does.

    Return the two sets of staying() and the refs that the record of
    left_branches() adds.
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
                stay, waited = staying(st["tasks"], old, rejected, fresh)
                new["left"], added = left_branches(repo, st, stay)
                unfinished, noted = moving(st["tasks"])
                for ref in unfinished + noted:
                    if ref in stay:
                        continue
                    d = old / "tasks" / ref
                    if d.is_dir():
                        d.rename(run / "tasks" / ref)
                        moved.append(ref)
                    task = new["tasks"][ref] = st["tasks"].pop(ref)
                    after = task["after"] + fresh.get(ref, [])
                    task["after"] = list(dict.fromkeys(after))
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
    return stay, waited, added


def ignore_worktrees(repo, command="init"):
    """Make git ignore the task worktrees of a repository.

    The command names the command of this script that runs the
    function, for its error.

    The worktrees sit inside the main checkout. Without the exclude
    entry, git lists them as files that it does not track, and the main
    checkout then never counts as clean.

    When no ignore rule covers the worktrees, the function adds the
    entry to the info/exclude file of the git directory. It creates the
    info directory first when the repository has none. git init copies
    that directory from its template directory, and a template directory
    can hold no info directory.

    The entry is a line of its own. When the last line of the exclude
    file does not end with a newline, the function writes a newline
    before the entry. Without that newline, git reads the entry as the
    end of the pattern of the last line. The joined pattern matches
    neither the worktrees nor the paths that the last line covers.

    When the function cannot write the entry, it ends the command with
    an error that names the exclude file.
    """
    ignored = sh(
        "git", "check-ignore", "-q", ".claude/worktrees/x", cwd=repo, check=False
    )
    if ignored.returncode:
        common = out(
            "git", "rev-parse", "--path-format=absolute", "--git-common-dir", cwd=repo
        )
        exclude = Path(common, "info", "exclude")
        try:
            exclude.parent.mkdir(exist_ok=True)
            # Binary mode reads the last byte of an exclude file in any
            # encoding. Append mode sends each write to the end of the
            # file, wherever the read stops.
            with open(exclude, "ab+") as f:
                entry = b".claude/worktrees/\n"
                if f.seek(0, os.SEEK_END):
                    f.seek(-1, os.SEEK_END)
                    if f.read(1) != b"\n":
                        entry = b"\n" + entry
                f.write(entry)
        except OSError as e:
            die(
                f"{command} cannot add the line .claude/worktrees/ to {exclude}:"
                f" {e}. Without that line, git lists each task worktree as an"
                " untracked file of the main checkout. Remove the cause and run"
                f" {command} again"
            )


def upstream_of(repo, name):
    """The remote branch that a local branch follows, or None.

    The result is the full name of the remote-tracking ref. It is None
    for a branch without an upstream branch, for an upstream branch that
    is a local branch, and for a remote-tracking ref that does not exist.
    """
    found = sh(
        "git",
        "rev-parse",
        "-q",
        "--verify",
        "--symbolic-full-name",
        f"{name}@{{upstream}}",
        cwd=repo,
        check=False,
    )
    ref = found.stdout.strip()
    if found.returncode or not ref.startswith("refs/remotes/"):
        return None
    return ref


def find_repo(name, own):
    """The main checkout of the repository that a --repo flag names.

    The flag names the directory of the repository in the repos directory
    of the host, which the REPOS_DIR variable names. The command ends when
    the variable is unset, when the directory is not the main checkout of
    a git repository, and when the directory holds the repository of the
    run, whose main checkout is own. The result is the path of the main
    checkout as git prints it.
    """
    top = os.environ.get(REPOS_DIR, "")
    if not top:
        die(
            f"the variable {REPOS_DIR} names no repos directory, so the command"
            f" cannot find the repository {name}. Set {REPOS_DIR} to the directory"
            " that holds the repositories of this host"
        )
    path = Path(top, name)
    if not path.is_dir():
        die(
            f"the repos directory {top} holds no directory {name}. Each --repo"
            " flag names the directory of a repository there"
        )
    p = sh("git", "worktree", "list", "--porcelain", cwd=path, check=False)
    if p.returncode:
        die(f"{path} is not a git repository:\n{p.stderr.strip()}")
    main = p.stdout.splitlines()[0].split(" ", 1)[1]
    if os.path.realpath(main) != os.path.realpath(path):
        die(f"{path} is not the main checkout of its repository, which is {main}")
    if os.path.realpath(main) == os.path.realpath(own):
        die(f"{path} holds the repository of the run, so it is no other repository")
    return main


def add_repos(cfg, tasks, a, command):
    """The other repositories of a run after the repository flags of init or config.

    The cfg holds the config of the run, and the tasks hold its tasks.
    Each --no-repo flag of config removes a repository that no
    unfinished or noted task names. Each --repo NAME=BASE flag adds the
    repository NAME of the repos directory with the base branch BASE.
    The flag changes a repository that the run holds only while no
    unfinished or noted task names it, because each task of such a
    repository starts from its base branch. Each --repo-check NAME=CMD
    flag sets the check of a repository, and an empty CMD removes it.

    Only a run whose landings propose pull requests takes another
    repository. A landing that commits cannot commit a task in two
    repositories at once. A change in the repository of the run can pin a
    commit of another repository, and only the merge of the pull request
    there gives that commit.

    A task of another repository starts from its local base branch. So
    the command ends when that branch holds a commit that its upstream
    branch lacks, because the pull request then carries that commit. For
    a base branch that follows no remote branch, the function adds a text
    to the unchecked list that it returns. The function also makes git of
    each repository that it adds ignore the task worktrees, as
    ignore_worktrees() says.

    Return the repositories by name, and the unchecked texts.
    """
    repos = {name: dict(entry) for name, entry in other_repos(cfg).items()}

    def users(name):
        return sorted(
            r
            for r, t in tasks.items()
            if name in t.get("repos", {}) and t["state"] not in SETTLED
        )

    for name in getattr(a, "no_repo", []):
        if name not in repos:
            die(f"the run holds no repository {name}. It holds {' '.join(repos) or 'none'}")
        if held := users(name):
            die(
                f"the tasks {' '.join(held)} change the repository {name}, so the"
                " run keeps it"
            )
        del repos[name]
    if a.repo and not proposes(cfg):
        die(
            f"a run with the target {cfg['base']} commits each task on {cfg['base']},"
            " and it takes no other repository. A landing cannot commit a task in"
            " two repositories at once, and a pin to a commit of another"
            " repository names the commit that the merge of a pull request"
            f" makes. Start the run with the target {PR} or {PR}:BRANCH to change"
            " other repositories"
        )
    unchecked = []
    for name, base in a.repo:
        path = find_repo(name, cfg["repo"])
        if name in repos and (repos[name]["path"], repos[name]["base"]) != (path, base):
            if held := users(name):
                die(
                    f"the tasks {' '.join(held)} start from the base branch"
                    f" {repos[name]['base']} of {name}, so the run keeps that base"
                    " branch"
                )
        if not branch_exists(path, base):
            die(f"the repository {path} has no branch {base}")
        if upstream := upstream_of(path, base):
            remote = upstream.removeprefix("refs/remotes/")
            log = ("git", "log", "--format=%h %s", f"{upstream}..refs/heads/{base}")
            if ahead := out(*log, cwd=path).splitlines():
                die(
                    f"each task of the repository {path} starts from the local"
                    f" branch {base}, which holds {counted(len(ahead), 'commit')}"
                    f" that its upstream branch {remote} lacks, so each pull request"
                    " carries them:\n"
                    + "\n".join(ahead[:20])
                    + f"\nPush {base} there, or fetch {remote} when the remote holds"
                    f" those commits already. Then run {command} again"
                )
        else:
            unchecked.append(
                f"{command} did not compare {base} of {path} with a remote branch,"
                f" because {base} follows no remote branch. Each pull request in"
                f" {name} carries the commits of the local branch {base} that the"
                " remote lacks"
            )
        ignore_worktrees(path, command)
        check = repos.get(name, {}).get("check", "")
        repos[name] = {"path": path, "base": base, "check": check}
    for name, check in a.repo_check:
        if name not in repos:
            die(
                f"the run holds no repository {name}, so --repo-check cannot set its"
                f" check. Add it with --repo {name}=BASE"
            )
        repos[name]["check"] = check
    return repos, unchecked


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
    filters = queue_filters({}, a)
    check_labels(repo, a)
    # A landing counts each task worktree as a change of the main checkout
    # unless git ignores the worktrees. So git ignores them before init
    # reads the changes of the main checkout.
    ignore_worktrees(repo)
    base, pr = base_of(a.target)
    # What init prints about a check that it did not run.
    unchecked = ""
    if pr:
        if not branch_exists(repo, base):
            die(
                f"the target {a.target} proposes each task as a pull request"
                f" against the branch {base}, and the repository has no branch"
                f" {base}. Name the base branch of the pull requests with"
                f" --target {PR}:BRANCH"
            )
        # Each task branch starts from the local base branch. So each pull
        # request carries the commits of that branch that the remote lacks.
        # The script contacts no remote, so it compares the branch with
        # the remote-tracking ref of its upstream branch.
        if upstream := upstream_of(repo, base):
            remote = upstream.removeprefix("refs/remotes/")
            log = ("git", "log", "--format=%h %s", f"{upstream}..refs/heads/{base}")
            if ahead := out(*log, cwd=repo).splitlines():
                die(
                    f"the target {a.target} proposes each task as a pull request"
                    f" against the branch {base}, and each task branch starts from"
                    f" the local branch {base}. That branch holds"
                    f" {counted(len(ahead), 'commit')} that its upstream branch"
                    f" {remote} lacks, so each pull request carries them:\n"
                    + "\n".join(ahead[:20])
                    + f"\nPush {base}, or fetch {remote} when the remote holds"
                    " those commits already. Then run init again"
                )
        else:
            unchecked = (
                f"init did not compare {base} with a remote branch, because {base}"
                " follows no remote branch. Each pull request carries the commits"
                f" of the local branch {base} that the remote lacks"
            )
    else:
        # Every landing commits on the base branch in the main checkout,
        # where the commit hooks run. A landing fails with the result
        # NOTBASE or DIRTY when the main checkout has another branch
        # checked out or holds uncommitted changes.
        if (current := out("git", "branch", "--show-current", cwd=repo)) != base:
            has = f"has {current} checked out" if current else "has a detached HEAD"
            die(
                f"the target of the run is the branch {base}, but the main checkout"
                f" {has}. Every landing commits on the target in the main checkout."
                f" Check out {base} there, or name another branch with --target"
            )
        if dirty := uncommitted(repo).splitlines():
            die(
                f"the target of the run is the branch {base}, and the main"
                " checkout holds uncommitted changes. Every landing commits on"
                " the target in the main checkout, and it fails with the result"
                " DIRTY while the main checkout holds such changes:\n"
                + "\n".join(dirty[:20])
                + "\nCommit or remove them, and run init again"
            )
    target_cfg = {"repo": repo, "target": a.target, "base": base}
    repos, repo_unchecked = add_repos(target_cfg, {}, a, "init")
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
    if takes and (why := repos_refusal(old_st, repos)):
        die(
            f"the run at {old} has unfinished tasks that change other"
            f" repositories, and {why}. Pass those flags to move the tasks to the"
            " new run, or pass --force to start an empty run without them"
        )
    # The queue filters of the new run decide which pending tasks of the
    # earlier run join it, as staying() says. The start of a task sets its
    # aven status to active. So init asks aven only about the pending tasks
    # that the earlier run has not started. Init also judges the open aven
    # blockers of each such task that the earlier run does not hold, as
    # the triage command does for a task that joins. The after list of a
    # task lacks each blocker that aven recorded after the triage of the
    # task. So take_over() orders the moved tasks by every open aven
    # blocker that init reads. Init asks before it takes the landing lock
    # of that run, and a failed aven command ends init before it changes
    # anything.
    rejected, fresh = {}, {}
    if takes and filters:
        held = old_st["tasks"]
        pending = [
            r
            for r in moving(held)[0]
            if held[r]["state"] == "pending" and not owns(Path(old), r)
        ]
        fresh = {r: dependencies(repo, r) for r in pending}
        outside = {r: [d for d in fresh[r] if d not in held] for r in pending}
        # A chain of blockers ends at each task of the earlier run that
        # init does not judge. Such a task moves whatever the filters, or
        # it landed or closed. The chain goes on through each pending task
        # that init judges, because such a task can stay in the earlier
        # run.
        others = set(held) - set(pending)
        rejected = refusals(repo, filters, outside, set(pending), others)
    # When init takes the tasks, take_over() records the branches of the
    # tasks that stay under the lock.
    left, added = ({}, [])
    if old_st is not None and not takes:
        left, added = left_branches(repo, old_st, old_st["tasks"])
    stay, waited = set(), set()
    run = free_dir(Path(a.run).resolve(), a.force)
    percents = floor_percents({}, a.disk_floor_percent)
    cfg = {
        "repo": repo,
        "target": a.target,
        "base": base,
        "repos": repos,
        "worktrees": f"{repo}/.claude/worktrees",
        "limit": a.limit,
        "rounds": a.rounds,
        "models": role_models({}, a.model),
        "check": a.check,
        "health": a.health,
        "stepTimeout": a.step_timeout,
        "filters": filters,
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
            stay, waited, added = take_over(repo, Path(old), run, new, rejected, fresh)
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
                entry = new["left"][ref]
                if "branch" in entry:
                    opened = f" pr={entry['pr']}" if "pr" in entry else ""
                    print(f"{ref} {entry['branch']}{opened}")
                for other in entry.get("repos", {}).values():
                    opened = f" pr={other['pr']}" if "pr" in other else ""
                    print(f"{ref} {other['branch']} repo={other['path']}{opened}")
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
    if waited:
        tasks = counted(len(waited), "pending task")
        why = "a moved task waits for it"
        if len(waited) > 1:
            why = "moved tasks wait for them"
        print(f"moved {tasks} that the queue filters leave out, because {why}:")
        for line in reason_lines({r: w for r, w in rejected.items() if r in waited}):
            print(line)
    if stay:
        tasks = counted(len(stay), "pending task")
        print(f"left {tasks} that the queue filters leave out in that run:")
        for line in reason_lines({r: w for r, w in rejected.items() if r in stay}):
            print(line)
    if unchecked:
        print(unchecked)
    for text in repo_unchecked:
        print(text)
    print(f"run={run} repo={repo} target={a.target} base={base} limit={a.limit}")


def cmd_config(a):
    repo = main_checkout()
    run = run_dir(repo)
    check_labels(repo, a)
    with state(run) as st:
        cfg = st["config"]
        # A run can continue from a state file that has no filters key.
        # That run has no queue filter.
        cfg["filters"] = queue_filters(cfg.get("filters", {}), a)
        repos, unchecked = add_repos(cfg, st["tasks"], a, "config")
        if repos or "repos" in cfg:
            cfg["repos"] = repos
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
    for text in unchecked:
        print(text, file=sys.stderr)
    warn_unmatched(unused)


def dependencies(repo, ref):
    """The refs of the open tasks that aven records as blockers of a task."""
    listed = json.loads(out("aven", "dep", "list", ref, "--json", cwd=repo))
    return [d["ref"] for d in listed["depends_on"] if d["unresolved"]]


def reason_lines(found):
    """One line per task of refusals(), with its ref and its reasons."""
    return [f"{ref}: {', and '.join(why)}" for ref, why in found.items()]


def refusals(repo, filters, outside, judged, skip):
    """Why the queue filters keep each task of outside from a run, by ref.

    The outside lists hold, by ref, the open aven blockers of each task
    that the function checks, without the blockers that the run holds.
    The next command waits only for the tasks that the run holds. So a
    task starts before such a blocker lands, and the function refuses
    the task when the filters keep the blocker from the run. The filters
    keep a blocker from the run when they leave out the blocker, or a
    task on the chain of its own open aven blockers. The triage command
    refuses such a blocker, so it never joins the run.

    The chain ends at each ref of skip, such as a task that the run
    holds. At a task of outside, the chain goes on through the list of
    the task in outside. The filters also judge the status, the
    priority, and the labels of each task of the judged refs. The
    function asks aven for those fields of each judged task and of each
    task on a chain. It also asks aven for the open blockers of each
    task on a chain that is not a task of outside and that the filters
    let in. A failed aven command ends the command.

    The result holds the reasons of left_out() for each judged task, and
    one reason per blocker that the filters keep from the run, with the
    chain to the task that they leave out. The result keeps the order of
    outside, and it leaves out each task that the function does not
    refuse.
    """
    shown, waits_on = {}, {}

    def fails(ref):
        if ref not in shown:
            task = json.loads(out("aven", "show", ref, "--json", cwd=repo))
            shown[ref] = left_out(filters, task)
        return shown[ref]

    def chain(ref, seen):
        # The refs from ref to a task that the filters leave out, or None
        # when the filters let in each task that ref waits for. The seen
        # refs end the chain, so a cycle of blockers ends it too.
        seen.add(ref)
        if ref in outside:
            if ref in judged and fails(ref):
                return [ref]
            after = outside[ref]
        else:
            if fails(ref):
                return [ref]
            if ref not in waits_on:
                deps = dependencies(repo, ref)
                waits_on[ref] = [d for d in deps if d not in skip]
            after = waits_on[ref]
        for d in after:
            if d not in seen and (found := chain(d, seen)):
                return [ref, *found]
        return None

    # The command asks aven about the judged tasks first.
    for ref in outside:
        if ref in judged:
            fails(ref)
    failed = {}
    for ref, blockers in outside.items():
        waits = [
            f"it waits for {', which waits for '.join(found)}, which the queue"
            " filters leave out"
            for d in blockers
            if (found := chain(d, {ref}))
        ]
        if why := (fails(ref) if ref in judged else []) + waits:
            failed[ref] = why
    return failed


def joining_refusal(repo, st, rows, deps):
    """Why the triage command refuses the tasks of rows.

    The queue filters of the run apply to each task that joins the run,
    such as a task that the run filed during the run. The deps hold the
    open aven blockers of each row, as dependencies() returns them. The
    function also refuses a task that joins when one of its blockers
    sits outside the run and the filters keep that blocker from the run,
    as refusals() says. It refuses a pending task that the run holds for
    such a blocker too, because next starts that task.

    A blocker that the run does not hold can have a row of its own. The
    function then judges that blocker as the task of its row, and it
    refuses each task that waits for a refused blocker, because that
    blocker does not join the run. The run keeps each task that it
    holds, so the function does not judge the status, the priority, or
    the labels of those tasks again. The result is empty when the
    function refuses no task.
    """
    # A run can continue from a state file that has no filters key.
    filters = st["config"].get("filters", {})
    if not filters:
        return ""
    held = st["tasks"]
    listed = list(dict.fromkeys(row["ref"] for row in rows))
    outside = {
        ref: [d for d in deps[ref] if d not in held]
        for ref in listed
        if ref not in held or held[ref]["state"] == "pending"
    }
    joining = {r for r in listed if r not in held}
    failed = refusals(repo, filters, outside, joining, set(held))
    if not failed:
        return ""
    text = (
        f"the triage refuses {counted(len(failed), 'task')} of the list because of"
        f" the queue filters of the run, {filters_text(filters)}, so it loads no"
        " task:\n"
        + "\n".join(reason_lines(failed))
        + "\nRemove the rows of those tasks, or change the filters with config"
    )
    if pending := [r for r in failed if r in held]:
        each = "it" if len(pending) == 1 else "each of them"
        text += (
            f". The run holds {' '.join(pending)}, and next starts {each} before"
            f" its blocker lands. So also set {each} to noted with set REF noted"
            " --note TEXT, unless config lets the blocker in"
        )
    return text


def repos_rows_refusal(st, rows):
    """Why the triage command refuses the repos fields of rows.

    The repos field of a row maps the name of each other repository that
    the task changes to the files that the task edits there. The run must
    hold each such repository, and a run whose landings commit holds
    none. A row of a task that the run holds can drop a repository only
    while the task has no branch there, because the run then keeps no
    work of the task there. The result is empty when the function refuses
    no row.
    """
    cfg = st["config"]
    held = other_repos(cfg)
    found = []
    for row in rows:
        ref, named = row["ref"], row.get("repos", {})
        if not isinstance(named, dict) or not all(
            isinstance(f, list) and all(isinstance(x, str) for x in f)
            for f in named.values()
        ):
            found.append(
                f"{ref}: the repos field maps the name of each other repository to"
                " a list of files"
            )
            continue
        if unknown := sorted(set(named) - set(held)):
            how = (
                "Add each of them with config --repo NAME=BASE"
                if proposes(cfg)
                else f"A run with the target {cfg['base']} takes no other repository"
            )
            found.append(
                f"{ref}: the run holds no repository {' '.join(unknown)}. {how}"
            )
            continue
        t = st["tasks"].get(ref)
        for name in sorted(set(t.get("repos", {}) if t else ()) - set(named)):
            entry = t["repos"][name]
            path = held.get(name, {}).get("path")
            name_of = entry.get("branch") or task_branch(ref)
            if path and Path(path).is_dir() and branch_exists(path, name_of):
                found.append(
                    f"{ref}: the task has the branch {name_of} in {path}, so the row"
                    f" keeps the repository {name}"
                )
    if not found:
        return ""
    return "the triage loads no task:\n" + "\n".join(found)


def cmd_triage(a):
    repo = main_checkout()
    run, saved = current(repo)
    rows = json.loads(Path(a.file).read_text())
    if why := repos_rows_refusal(saved, rows):
        die(why)
    # A failed aven command ends the triage before it changes the state.
    deps = {row["ref"]: dependencies(repo, row["ref"]) for row in rows}
    if why := joining_refusal(repo, saved, rows, deps):
        die(why)
    with state(run) as st:
        added = updated = 0
        held = other_repos(st["config"])
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
            # The repos field maps each other repository that the task
            # changes to its files. A landing adds the branch, the pull
            # request, and the push of the task there, and the triage keeps
            # them.
            named = row.get("repos", {})
            if named or (old and old.get("repos")):
                was = old.get("repos", {}) if old else {}
                new["repos"] = {
                    name: {
                        **was.get(name, {}),
                        "files": sorted(
                            {
                                f.removeprefix(held[name]["path"] + "/").removeprefix(
                                    "./"
                                )
                                for f in named[name]
                            }
                        ),
                    }
                    for name in sorted(named)
                }
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
    flags = {f.split("=", 1)[0] for f in filters}
    # aven list takes one value of --status, --priority, and --label, and
    # it has no filter that leaves a value out. So the run config holds
    # the filters of those dimensions, and the command applies them
    # itself.
    if named := sorted(flags & {f"--{dim}" for dim in FILTERS}):
        die(
            f"untriaged takes no {' or '.join(named)} filter, because the queue"
            " filters of the run apply to each status, priority, and label."
            f" Set them with config, such as config {named[0]} VALUE, and run"
            " untriaged without them"
        )
    # The command passes --open whenever aven accepts it, so aven lists
    # only the open tasks. aven refuses --open beside --all, --deleted, or
    # --upcoming, and it refuses a second --open. With one of those flags,
    # the command itself drops each closed or deleted task.
    refused = {"--open", "--all", "--deleted", "--upcoming"}
    opened = [] if flags & refused else ["--open"]
    listed = json.loads(out("aven", "list", "--json", *opened, *filters, cwd=repo))
    st = json.loads((run / "state.json").read_text())
    held = st["tasks"]
    # A run can continue from a state file that has no filters key.
    queue = st["config"].get("filters", {})
    rows = [
        {"ref": t["ref"], "title": t["title"], "priority": t["priority"]}
        for t in listed
        if t["status"] in AVEN_OPEN
        and not t["deleted"]
        and not t["is_epic"]
        and t["ref"] not in held
        and not left_out(queue, t)
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


def left_branches(repo, st, refs):
    """The pull request branches that a new run counts as leftover work.

    The new run replaces the run of the state st, and the refs name the
    tasks that stay in that run. When the new run does not take the
    tasks of that run, such as with init --force, each task of that run
    stays there. When it takes them, the tasks of staying() stay there.
    The branch of a task that a landing proposed as a pull request has
    another name than task_branch(), so leftovers() cannot find that
    branch by its name. The new run records the branch of each such task
    that stays and whose state is neither landed nor closed, with the
    address of its pull request when the task records one. It also keeps
    each branch that the state st records the same way. The record holds
    only the branches that exist.

    A landing also proposes the task in each other repository that the
    task changes. The repos field of an entry records each branch of
    such a pull request that has not merged, with the main checkout and
    the base branch of its repository, because the new run might not
    hold that repository.

    Return the record, by ref, and the refs that the record adds for the
    tasks of the state st.
    """

    def exists(path, name):
        return Path(path).is_dir() and branch_exists(path, name)

    def kept(entry):
        found = {}
        if "branch" in entry and branch_exists(repo, entry["branch"]):
            found = {key: entry[key] for key in ("branch", "pr") if key in entry}
        others = {
            name: other
            for name, other in entry.get("repos", {}).items()
            if exists(other["path"], other["branch"])
        }
        if others:
            found["repos"] = others
        return found

    left = {ref: found for ref, entry in st.get("left", {}).items() if (found := kept(entry))}
    held = other_repos(st["config"])
    added = []
    for ref in sorted(refs):
        t = st["tasks"][ref]
        if t["state"] in SETTLED:
            continue
        others = {
            name: {
                "path": held[name]["path"],
                "base": held[name]["base"],
                **{key: entry[key] for key in ("branch", "pr") if key in entry},
            }
            for name, entry in sorted(t.get("repos", {}).items())
            if "branch" in entry and not entry.get("merged") and name in held
        }
        if found := kept({**t, "repos": others}):
            left[ref] = found
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
    the entry of the record for each line that names such a branch, with
    the name and the base branch of its repository.

    The function also looks in each other repository that the task
    changes, and in each other repository where left_branches() records a
    branch of the task. The text of such a repository names its main
    checkout.
    """
    def ahead(place, name):
        base = place["base"]
        count = ("git", "rev-list", "--count", f"{base}..{name}")
        n = int(out(*count, cwd=place["repo"]))
        return f"{n} {'commit' if n == 1 else 'commits'} ahead of {base}"

    lines, proposals = [], []
    for ref in refs:
        if owns(run, ref):
            continue
        found = []
        for place in leftover_places(st, ref):
            path, base = place["repo"], place["base"]
            where = f" in {path}" if place["name"] else ""
            # A task of an earlier run starts on the branch of task_branch().
            name = task_branch(ref)
            if branch_exists(path, name):
                found.append(f"the branch {name}{where}, {ahead(place, name)}")
            if entry := place["proposal"]:
                name = entry["branch"]
                if pr := entry.get("pr"):
                    found.append(
                        f"the branch {name}{where} of the pull request {pr},"
                        f" {ahead(place, name)}"
                    )
                else:
                    found.append(
                        f"the branch {name}{where} that a landing proposed, with no"
                        f" recorded pull request, {ahead(place, name)}"
                    )
                proposals.append({**entry, "name": place["name"], "base": base})
            if Path(place["wt"]).exists():
                found.append(f"the worktree {place['wt']}")
        if found:
            lines.append(f"{ref}: {', and '.join(found)}")
    return lines, proposals


def leftover_places(st, ref):
    """The repositories where an earlier run can have left work for a task.

    The first entry stands for the repository of the run. Each later
    entry stands for another repository that the triage row of the task
    names and that the run holds, or where left_branches() records a
    branch of the task. Each entry holds the name of the repository,
    which is empty for the repository of the run, its main checkout as
    repo, its base branch, and the worktree of the task there. The
    proposal value holds the record of left_branches() for the branch of
    a pull request there that still exists, or None. The function leaves
    out a repository whose main checkout is gone.
    """
    cfg = st["config"]
    # A run can continue from a state file that has no left key.
    entry = st.get("left", {}).get(ref, {})
    own = {k: entry[k] for k in ("branch", "pr") if k in entry} if "branch" in entry else None
    places = [
        {
            "name": "",
            "repo": cfg["repo"],
            "base": cfg["base"],
            "wt": worktree(cfg, ref),
            "proposal": own if own and branch_exists(cfg["repo"], own["branch"]) else None,
        }
    ]
    held = other_repos(cfg)
    t = st["tasks"].get(ref, {})
    recorded = entry.get("repos", {})
    for name in sorted((set(t.get("repos", {})) & set(held)) | set(recorded)):
        other = recorded.get(name)
        path, base = (held[name]["path"], held[name]["base"]) if name in held else (
            other["path"],
            other["base"],
        )
        if not Path(path).is_dir():
            continue
        if other and not branch_exists(path, other["branch"]):
            other = None
        places.append(
            {
                "name": name,
                "repo": path,
                "base": base,
                "wt": other_worktree(path, ref),
                "proposal": other,
            }
        )
    return places


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
        die(
            "these tasks have a branch or a worktree that this run did not"
            " create, and a run resumes only its own work. The command"
            " started no task.\n" + "\n".join(lines) + "\nMove the work of each"
            " task out of the way with ultraclose.py leftover <ref>. That"
            " command keeps each commit and each uncommitted change of the"
            f" work. {pr_steps(proposals)}Then run ultraclose.py {retry}."
        )


def pr_steps(proposals):
    """What a caller does before the leftover command moves a pull request branch.

    The proposals argument holds the entry of each such branch, as
    leftovers() returns it. The script contacts no remote, so the caller
    reads each pull request and passes its state to the leftover command.
    The result is empty when no branch belongs to a pull request.
    """
    if not proposals:
        return ""
    find = ""
    if any("pr" not in entry for entry in proposals):
        find = (
            " For a branch with no recorded pull request, list its pull requests"
            " with mcp__github__list_pull_requests, and set head to"
            " <owner>:<branch> and state to all. When the list is empty, the"
            " branch has no pull request."
        )
    bases = " and ".join(
        dict.fromkeys(
            f"{e['base']} of {e['name']}" if e["name"] else e["base"] for e in proposals
        )
    )
    other = ""
    if any(e["name"] for e in proposals):
        other = (
            " For the branch of another repository, pass --pr NAME=STATE with the"
            " name of that repository in place of --pr STATE."
        )
    return (
        "Before the command moves the branch of a pull request, read that pull"
        f" request with mcp__github__pull_request_read.{find} Close an open pull"
        " request with mcp__github__update_pull_request, unless the user asks"
        " you to keep it open. Then pass what you found to the command as"
        f" --pr {', --pr '.join(PR_STATES[:-1])}, or --pr {PR_STATES[-1]}.{other}"
        " Before you pass --pr merged, bring the merge into the local branch"
        f" {bases}. The command closes the task when every pull request of the"
        " task merged, because a merged task does not need a new attempt. "
    )


def kept_name(repo, ref):
    """The name of the branch that keeps the commits of an earlier run of a task.

    The name is kept/REF-DATE with the date of today, such as
    kept/APP-7KQ9-20261114. When a branch has that name, the function
    adds a counter to the end of the name, and the counter starts at 2.
    """
    name = stem = f"{KEPT}/{ref}-{time.strftime('%Y%m%d')}"
    n = 1
    while branch_exists(repo, name):
        n += 1
        name = f"{stem}-{n}"
    return name


def cmd_leftover(a):
    repo = main_checkout()
    run = run_dir(repo)
    ref = a.ref
    with state(run) as st:
        cfg = st["config"]
        base = cfg["base"]
        t = st["tasks"].get(ref) or die(f"unknown task {ref}")
        lines, proposals = leftovers(st, run, [ref])
        if not lines:
            die(
                f"{ref} has no branch and no worktree of an earlier run, so the"
                " command changed nothing. A run resumes the branch and the"
                " worktree that it created"
            )
        # The places hold the branch that a landing of an earlier run
        # proposed as a pull request in each repository, as their proposal
        # values say. Each --pr flag gives the state of one such pull
        # request, by the name of its repository.
        places = leftover_places(st, ref)
        states = dict(a.pr)
        named = {p["name"] for p in places if p["proposal"]}
        if named - set(states):
            die(
                "the command moves the branch of a pull request only with the"
                " --pr flag, and it changed nothing.\n"
                + "\n".join(lines)
                + "\n"
                + pr_steps(proposals).strip()
            )
        if extra := sorted(set(states) - named):
            where = " and ".join(name or "the repository of the run" for name in extra)
            die(
                f"{ref} has no branch that a landing proposed as a pull request in"
                f" {where}, so the command takes no --pr flag for it. The command"
                " changed nothing"
            )
        for p in places:
            if states.get(p["name"]) == "none" and "pr" in p["proposal"]:
                die(
                    f"the earlier run recorded the pull request {p['proposal']['pr']}"
                    f" for the branch {p['proposal']['branch']}. Read that pull"
                    " request, and pass its state with --pr. The command changed"
                    " nothing"
                )
        # A task whose every pull request merged does not need a new
        # attempt, so the command closes it.
        closing = bool(states) and all(s == "merged" for s in states.values())

        def note_kept(place, kept, origin):
            """Add the note about a kept branch to the aven task.

            A note of an earlier run can name the branch by its old name,
            so the origin text gives that name. The implementer of a new
            attempt reads the notes of the task in its task file.

            The caller adds the note before it gives the branch its name.
            So a failed aven command leaves the work of the earlier run
            as it is, and a second run of the command finds that work.
            """
            after = (
                "The pull request of that attempt merged, so the task is done."
                if closing
                else f"A new attempt starts on a new branch from {place['base']}."
            )
            where = f" of the repository {place['repo']}" if place["name"] else ""
            aven_note(
                repo,
                ref,
                f"The branch {kept}{where} holds an earlier attempt at this task."
                f" {origin} The branch stays as a reference only, so do not"
                f" merge it and do not rebase onto it. {after}",
            )

        def ahead_of_base(place, commit):
            """How many commits the commit holds that the base branch lacks."""
            count = ("git", "rev-list", "--count", f"{place['base']}..{commit}")
            return int(out(*count, cwd=place["repo"]))

        # In a directory that is no worktree, git acts on the main
        # checkout that holds the directory. The check comes before the
        # aven command, so the refusal leaves the aven task as it is.
        for place in places:
            wt = place["wt"]
            if Path(wt).exists():
                top = sh("git", "rev-parse", "--show-toplevel", cwd=wt, check=False)
                if top.returncode or not Path(top.stdout.strip()).samefile(wt):
                    die(
                        f"{wt} is not a git worktree, so the command changed"
                        " nothing. Move the directory away, and run the command"
                        " again"
                    )
        # A merged task is done. The aven command comes before each
        # change of the branches and of the worktrees. So when it fails, a
        # second run of the command finds the work of the earlier run.
        if closing:
            sh("aven", "edit", ref, "--status", AVEN_STATUS["closed"], cwd=repo)
        for place in places:
            path, wt, proposal = place["repo"], place["wt"], place["proposal"]
            where = f" in {path}" if place["name"] else ""
            if Path(wt).exists():
                if uncommitted(wt):
                    sh("git", "add", "-A", cwd=wt)
                    sh("git", "commit", "-q", "--no-verify", "-m", "wip", cwd=wt)
                    print(f"committed the uncommitted changes of the worktree {wt}")
                # A commit of a detached HEAD belongs to no branch, so the
                # removal of the worktree loses it. A branch keeps the
                # commit before the worktree goes.
                tip = out("git", "rev-parse", "HEAD", cwd=wt)
                detached = not out("git", "branch", "--show-current", cwd=wt)
                if detached and ahead_of_base(place, tip):
                    kept = kept_name(path, ref)
                    note_kept(
                        place,
                        kept,
                        "It holds the commits of the detached HEAD of the worktree"
                        " of that attempt.",
                    )
                    sh("git", "branch", kept, tip, cwd=path)
                    print(
                        f"created the branch {kept}{where}, which keeps the commits"
                        " of the detached HEAD of the worktree, and added a note to"
                        " the aven task"
                    )
                sh("git", "worktree", "remove", wt, cwd=path)
                print(f"removed the worktree {wt}")
            pr_name = proposal["branch"] if proposal else None
            for name in (task_branch(ref), pr_name):
                if not name or not branch_exists(path, name):
                    continue
                ahead = ahead_of_base(place, name)
                # The pull request of the branch merged. The base branch
                # holds the change even when a squash gave it another
                # commit.
                merged = (
                    name == pr_name
                    and states[place["name"]] == "merged"
                    and holds(path, place["base"], name)
                )
                if merged or not ahead:
                    sh("git", "branch", "-q", "-D", name, cwd=path)
                    why = (
                        f"whose change {place['base']} holds"
                        if ahead
                        else f"which held no commit that {place['base']} lacks"
                    )
                    print(f"deleted the branch {name}{where}, {why}")
                    continue
                kept = kept_name(path, ref)
                note_kept(place, kept, f"It had the name {name} before.")
                sh("git", "branch", "-m", name, kept, cwd=path)
                print(
                    f"renamed the branch {name}{where} to {kept}, which keeps its"
                    f" {counted(ahead, 'commit')} as a reference, and added a note"
                    " to the aven task"
                )
        # A run can continue from a state file that has no left key.
        st.get("left", {}).pop(ref, None)
        if closing:
            t["state"] = "closed"
            print(
                f"{ref} closed, because its pull request merged. The aven task is done"
            )
        else:
            print(
                f"{ref} has no leftover work. A new attempt starts on a new"
                f" branch from {base}"
            )


def start(st, run, ref):
    """Give a task its worktree, its task file, and the active status.

    The caller first refuses a task that has the work of an earlier run.
    So a branch or a worktree that exists here is the work of this run,
    and the task resumes it.
    """
    cfg, t = st["config"], st["tasks"][ref]
    repo = cfg["repo"]
    # The directory of the task records that this run started the task,
    # and owns() reads that record. So the directory comes before the
    # branch and the worktree.
    d = run / "tasks" / ref
    (d / "scratch").mkdir(parents=True, exist_ok=True)
    # The task has a worktree in each repository whose change has not
    # merged, as task_repos() lists them.
    for p in task_repos(cfg, t, ref):
        wt, name, path = p["wt"], p["branch"], p["repo"]
        if Path(wt).exists():
            continue
        # A landing that proposes a pull request removes the worktree and
        # keeps the branch, so a relaunch of that task takes this path.
        # When git still lists the worktree, the relaunch resumes it. git
        # can also remove the worktree in part and leave its directory, and
        # the landing then tells the caller to delete that directory.
        if branch_exists(path, name):
            sh("git", "worktree", "add", "-q", wt, name, cwd=path)
        else:
            sh("git", "worktree", "add", "-q", wt, "-b", name, p["base"], cwd=path)
    (d / "task.md").write_text(out("aven", "context", ref, cwd=repo) + "\n")
    sh("aven", "edit", ref, "--status", "active", cwd=repo)
    t["state"] = "inflight"
    t["startedAt"] = time.time()
    # The commit that an earlier landing recorded belongs to an earlier
    # attempt. A landing of this attempt that reads it reports the task
    # as landed, although the base branch lacks the new commits.
    t.pop("commit", None)


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
        # A task with no commit of its own in any of its repositories starts
        # again at the implementer.
        ahead = sum(commits_ahead(p) for p in task_repos(st["config"], t, a.ref))
        if not ahead and not a.sync:
            resume = None
            unstore(run, a.ref)
        print(json.dumps(workflow_args(st, run, a.ref, resume), indent=1))
        print(f"{a.ref}: {was} to inflight", file=sys.stderr)


def stuck(st, ref):
    """The line that names a pending task that waits for a noted task, or None.

    A noted task does not settle, so a pending task that comes after it
    never starts.
    """
    tasks = st["tasks"]
    t = tasks[ref]
    if t["state"] != "pending":
        return None
    noted = [r for r in blockers(st, t) if tasks[r]["state"] == "noted"]
    if not noted:
        return None
    return f"stuck {ref} waits for noted {' '.join(noted)} | {t['title']}"


def cmd_set(a):
    repo = main_checkout()
    run = run_dir(repo)
    note = (a.note or "").strip()
    if a.state in NOTING and not note:
        die(
            f"the state {a.state} takes a note for the aven task. Pass --note"
            " TEXT with the reason, such as the open findings of a noted task"
            " or the evidence that closes a task. The command changed nothing"
        )
    if a.note is not None and a.state not in NOTING:
        die(
            f"--note applies only to the states {' and '.join(NOTING)}. The"
            " command changed nothing"
        )
    with state(run) as st:
        cfg = st["config"]
        t = st["tasks"].get(a.ref) or die(f"unknown task {a.ref}")
        name, url = branch(t, a.ref), t.get("pr")
        places = task_repos(cfg, t, a.ref)
        mine = owns(run, a.ref)
        # A failed aven command ends the command before it changes the
        # run. The status comes before the note. So when the note fails,
        # a second run of the command sets the same status again, and the
        # aven task gets one note.
        if a.state in AVEN_STATUS:
            sh("aven", "edit", a.ref, "--status", AVEN_STATUS[a.state], cwd=repo)
        if a.state == "noted" and mine:
            # The branch of a noted task stays, and a later attempt reads
            # the note in its task file.
            if branch_exists(repo, name):
                note += f"\n\nThe branch {name} holds the work of this attempt."
                if url:
                    note += f" The pull request of that branch is {url}."
            for p in places[1:]:
                if branch_exists(p["repo"], p["branch"]):
                    note += (
                        f"\n\nIn the repository {p['repo']}, the branch"
                        f" {p['branch']} holds the work of this attempt."
                    )
                    if p["pr"]:
                        note += f" The pull request of that branch is {p['pr']}."
        if a.state in NOTING:
            aven_note(repo, a.ref, note)
        t["state"] = a.state
        if a.flag:
            t["flags"].append(a.flag)
        if a.state in AVEN_STATUS:
            for p in places:
                clear_work(p, a.state, mine)
        waiting = [line for r in sorted(st["tasks"]) if (line := stuck(st, r))]
    print(f"{a.ref} {a.state}")
    if a.state == "closed":
        for p in places:
            if p["pr"]:
                print(
                    f"{a.ref} records the pull request {p['pr']}, which the command"
                    " did not close"
                )
    if a.state == "noted":
        for line in waiting:
            print(line)


def clear_work(p, task_state, mine):
    """Remove the work of a task in one repository after the task settles or is noted.

    The p argument holds the facts of the repository, as task_repos()
    gives them, and task_state is noted, closed, or landed. The mine
    argument says whether the run owns the work of the task, as owns()
    says. The function removes only the work of the run, because the
    branch of an earlier run can hold commits that no other branch holds.

    The function removes the clean worktree of the task. When it removes
    the worktree of a closed task, it also deletes the branch. It deletes
    the branch of a landed task whose worktree is gone only when the base
    branch holds the change of the branch. It prints a line for each
    worktree and each branch that it keeps.
    """
    repo, wt, name, base = p["repo"], p["wt"], p["branch"], p["base"]
    gone = not Path(wt).exists()
    if not gone:
        if not mine:
            print(f"kept worktree {wt}, which this run did not create")
        elif uncommitted(wt):
            print(f"kept dirty worktree {wt}")
        else:
            sh("git", "worktree", "remove", wt, cwd=repo)
            gone = True
            if task_state == "closed":
                sh("git", "branch", "-q", "-D", name, cwd=repo)
    # The base branch holds the change of a landed task, so its branch
    # holds no work that the removal loses.
    if task_state == "landed" and gone and mine and branch_exists(repo, name):
        where = f" of {repo}" if p["name"] else ""
        if not holds(repo, base, name):
            print(f"kept the branch {name}{where}, whose change {base} lacks")
        else:
            deleted = sh("git", "branch", "-q", "-D", name, cwd=repo, check=False)
            if deleted.returncode:
                print(
                    f"kept the branch {name}{where}, which git cannot delete:"
                    f" {deleted.stderr.strip()}"
                )


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
    # The worktrees of the tasks in another repository fill the file
    # system of that repository, so the run watches it too.
    others = [entry["path"] for entry in other_repos(cfg).values()]
    for path in dict.fromkeys(disk_paths(cfg) + others):
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
    if not other_repos(cfg):
        print("repos: none")
    for name, entry in sorted(other_repos(cfg).items()):
        print(
            f"repo {name}: {entry['path']}, base {entry['base']}, check"
            f" {entry.get('check') or 'none'}"
        )
    print(" ".join(f"{s}={n}" for s, n in counts.items()), f"limit={cfg['limit']}")
    # A run can continue from a state file that has no filters key.
    print(f"filters: {filters_text(cfg.get('filters', {})) or 'none'}")
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
            # The task waits for each pull request that a landing proposed
            # and that has not merged, one line per repository.
            for p in task_repos(cfg, t, ref):
                if not p["proposed"]:
                    continue
                head, url = p["branch"], p["pr"]
                # A landing can leave out the branch of the repository of the
                # run, which then holds no change, as propose() says. The
                # line repeats the steps of the action of that landing, so a
                # new session finds them.
                if p["record"].get("emptied"):
                    close = (
                        f"Close its pull request {url} when it is open"
                        if url
                        else f"Close an open pull request of {head}"
                    )
                    waits = (
                        f"holds no change on {head}. {close}, and push nothing to"
                        f" {head}. After the other pull requests merge, run"
                        f" merged {ref}, which lands the task"
                    )
                elif not url:
                    waits = f"waits for the push of {head} and its pull request"
                elif p["record"].get("pushed"):
                    waits = f"waits for the merge of {url}"
                else:
                    waits = f"waits for the push of {head} to the pull request {url}"
                where = f" in {p['name']}" if p["name"] else ""
                print(f"proposed {ref}{where} {waits} | {t['title']}")
        elif line := stuck(st, ref):
            print(line)
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


def slow_steps(times, typical):
    """The timed steps of a landing that took much longer than their medians.

    A step is slow when it took more than twice its median, and more than
    SLOW seconds longer than its median. The medians cover the earlier
    landings, as medians() says.
    """
    return [
        step
        for step in TIMED
        if step in times
        and step in typical
        and times[step] > 2 * typical[step]
        and times[step] > typical[step] + SLOW
    ]


def environment_failure(log):
    """The failure of the environment that the log of a step names, or None.

    Return the cause and the first line of the log that shows it, as
    ENVIRONMENT lists them.
    """
    for line in Path(log).read_text(errors="replace").splitlines():
        for cause, pattern in ENVIRONMENT:
            if pattern.search(line):
                return cause, line.strip()[:200]
    return None


# What the land command prints in its ACTION line, by the result of the
# landing. Each function takes the facts of one landing and returns the
# action that the caller takes. The caller runs each command of an
# action in a shell. So the action quotes each path and each branch name
# of the command with shlex.quote. A path that only the words around the
# command name, such as the main checkout that the command runs in, stays
# unquoted. Every landing knows these facts:
#
# - ref, base, branch, wt, repo: the task, the base branch, the task
#   branch, the worktree, and the main checkout. These facts name the
#   repository of the run, unless the landing fails in another repository
#   of the task. They then name that repository.
# - others: one entry per other repository in which a landing with the
#   result PR proposed the task, and an empty list for every other
#   landing. Each entry holds the name of the repository and its main
#   checkout as path. It also holds the base branch, the worktree as wt,
#   the branch of the pull request as head, and the recorded pull request
#   as url. Its unremoved value says what the removal of its worktree
#   left.
# - code: the result.
# - n: how many landings of the task have failed for the same reason so
#   far, with this landing. The reason is the result, or the cause that
#   ENVIRONMENT names.
#
# The functions decide what the script can tell from the run, such as a
# second failure of one task. They leave a cause that only the failing
# output shows to the caller, and name the section of SKILL.md that lists
# those causes.


def sync_command(c):
    return f"ultraclose.py args {c['ref']} --sync '<reason and failing output>'"


def note_command(c):
    return f"ultraclose.py set {c['ref']} noted --note '<reason>'"


def again(c):
    """What follows the sync of a task that failed for the same reason before.

    The orchestrator notes a task when a second sync fails for the same
    reason. A sync follows the first failure and the second failure, so
    the note can follow only the third failure.
    """
    if c["n"] < 3:
        return ""
    return (
        f" The task has ended in {c['code']} {c['n']} times. When the last two"
        " syncs of the task failed for the same reason, note the task in place"
        f" of another sync: {note_command(c)}."
    )


def sync_action(c):
    """The action that syncs a task whose change must meet the base branch again."""
    return (
        f"Sync the task: relaunch it with {sync_command(c)}, and land it again"
        f" after its Workflow ends approved or landable.{again(c)}"
    )


def unremoved_repair(c, unremoved, wt, repo, head, merged):
    """The repair of the git command that did not remove one worktree of a task.

    The unremoved argument says what the command left, as repairs()
    says. The merged argument holds the merged command that fails until
    the worktree goes, for a landing with the result PR.
    """
    path = shlex.quote(wt)
    if unremoved == "worktree":
        text = (
            f"git did not remove the worktree {wt}. Repair the cause that the"
            f" error of git names, and run git worktree remove --force {path} again"
            f" in the main checkout {repo}. When git then answers that {wt} is"
            f" not a working tree, delete the directory with rm -rf {path}. "
        )
        if c["code"] == "PR":
            text += (
                f"Until the worktree goes, {merged} fails, because the worktree"
                f" has the branch {head} checked out, and git deletes no such"
                " branch. "
            )
        return text
    if unremoved == "directory":
        return (
            f"git deleted its record of the worktree {wt}, but the directory"
            " stays. Repair the cause that the error of git names, and delete"
            f" the directory with rm -rf {path}. "
        )
    return ""


def repairs(c):
    """The repair of each command that failed at the end of a landing.

    A landing that commits ends with the git command that removes the
    worktree, the git command that deletes the task branch, and the aven
    command that sets the aven task to done. A landing that proposes a
    pull request ends with the git command that removes each worktree of
    the task that it proposed. The task landed or waits as proposed when
    one of these commands fails, and the output reports each failed
    command. The text gives the repair of each one in the order of the
    reports.

    These facts say what the failed commands left:

    - unremoved: an empty text when git removed the worktree, "worktree"
      when git still lists the worktree, and "directory" when git deleted
      its record of the worktree and only the directory stays.
    - undeleted: whether the task branch still exists, because git did not
      delete it.
    - open: whether the aven task is still open, because aven did not set
      it to done.
    - others: the unremoved value of each other repository, in its entry.
    """
    ref, repo, br = c["ref"], c["repo"], c["branch"]
    merged = f"ultraclose.py merged {ref}"
    # The branch of a repository in which the landing proposed nothing
    # keeps its name.
    head = c["head"] or br
    text = unremoved_repair(c, c["unremoved"], c["wt"], repo, head, merged)
    if c["undeleted"]:
        # git keeps the branch that a worktree has checked out, and the
        # worktree of the task has the task branch checked out.
        first = (
            "Remove the worktree first, because it keeps the branch"
            if c["unremoved"] == "worktree"
            else "Repair the cause that the error of git names"
        )
        text += (
            f"git did not delete the branch {br}. {first}, and then run git"
            f" branch -q -D {shlex.quote(br)} in the main checkout {repo}. "
        )
    if c["open"]:
        text += (
            "The aven task is still open, because its aven command failed. Run"
            f" aven edit {ref} --status done. "
        )
    for o in c["others"]:
        merged = f"ultraclose.py merged {ref} --repo {o['name']}"
        text += unremoved_repair(c, o["unremoved"], o["wt"], o["path"], o["head"], merged)
    return text


def passed_action(c):
    if c["next"]:
        text = (
            "Launch one Workflow per object of the list that follows the RESULT"
            " line, from the path in its script field. Skip each object whose"
            " task you relaunched after this landing started. When an error"
            " follows the RESULT line in place of the list, the landing passed"
            " and next failed. Then repair the cause of the error, and run"
            " ultraclose.py next."
        )
    else:
        text = (
            "Run ultraclose.py next, which starts each task that this landing"
            " lets start."
        )
    text = repairs(c) + text
    if c["code"] != "PR":
        return text
    if c["others"]:
        return f"{text} {others_steps(c)}"
    if c["url"]:
        return (
            f"{text} The task records the pull request {c['url']}. Read its"
            " state before you push, and act on that state as the Pull requests"
            " section of SKILL.md says."
        )
    return (
        f"{text} Then push the branch {c['head']} and open its pull request, as"
        " the Pull requests section of SKILL.md says."
    )


def others_steps(c):
    """What a caller does after a landing that proposed a task in other repositories.

    The landing prints one REPO line per other repository in which it
    proposed the task, and its RESULT line names the branch of the
    repository of the run when it proposed the task there too. The
    caller pushes each branch in its repository and opens each pull
    request. The pull request of the repository of the run merges last,
    because its change can depend on the changes of the other
    repositories, such as with a pin to a commit.
    """
    recorded = [o["url"] for o in c["others"] if o["url"]] + (
        [c["url"]] if c["url"] else []
    )
    read = ""
    if len(recorded) == 1:
        read = (
            f" The task records the pull request {recorded[0]}. Read its state"
            " before you push its branch, and act on that state."
        )
    elif recorded:
        read = (
            f" The task records the pull requests {' and '.join(recorded)}. Read"
            " the state of each of them before you push its branch, and act on"
            " that state."
        )
    own = (
        f" The RESULT line names the branch {c['head']} of {c['repo']}. Its pull"
        " request merges after the pull requests of the REPO lines, so open it"
        " last and tell the user that order."
        if c["head"]
        else f" The task holds no change in {c['repo']}, so the RESULT line names no branch."
    )
    return (
        "Then push the branch of each REPO line in the main checkout that its"
        " path names, and open its pull request against its base branch, as the"
        f" Pull requests section of SKILL.md says.{own}{dropped_steps(c)}{read}"
    )


def dropped_steps(c):
    """What a caller does about the pull request that a landing left out.

    An earlier landing proposed the task in the repository of the run.
    The branch there holds no change against the base branch when the
    user merged that pull request before the other pull requests of the
    task, or when a fix reverted the change there. The landing proposed
    nothing there. The task keeps the branch and the pull request, and
    the merged command records that pull request after the others. The
    text is empty when the landing left out no such pull request.
    """
    dropped = c["dropped"]
    if not dropped:
        return ""
    ref, name, url = c["ref"], dropped["branch"], dropped["url"]
    read = (
        f" Read the state of {url} with mcp__github__pull_request_read, and close"
        " it when it is open. Push nothing to it."
        if url
        else (
            " The task records no pull request there. When the remote holds an"
            f" open pull request of {name}, close it."
        )
    )
    merged = "its pull request" if url else f"a pull request of {name}"
    return (
        f" An earlier landing proposed the task in {c['repo']} on the branch"
        f" {name}, which now holds no change against {c['base']}. So {merged}"
        f" merged, or the task no longer changes {c['repo']}.{read} After"
        " the pull requests of the REPO lines merge, run ultraclose.py merged"
        f" {ref}, which lands the task. Do not set the task to landed or closed."
    )


def failed_step_action(c):
    step, log = c["step"], c["log"]
    if not c["cause"]:
        text = (
            f"The {step} failed, which does not show that the task must change."
            f" Find the cause in the failing output and in {log}. Act on that"
            f" cause as the Landing section of SKILL.md says for {c['code']}."
        )
        if c["n"] > 1:
            text += f" The task has ended in {c['code']} {c['n']} times."
        return text
    # The output of a failed test can hold the words that
    # environment_failure() finds, such as a test that reports a killed
    # process. So the action leaves the caller to judge whether the named
    # line shows why the step failed.
    if c["n"] == 1:
        return (
            f"A line of {log} names {c['cause']}, and the output quotes that"
            f" line before this ACTION line. When that line shows why the {step}"
            f" failed, the environment stopped the {step}. In that case, repair"
            " the environment and land again, and do not sync the task. When"
            " that line is only part of the output of a failed test, such as a"
            " test that reports a killed process, find the cause in the failing"
            " output, and act on it as the Landing section of SKILL.md says for"
            f" {c['code']}."
        )
    return (
        f"The log of the {step} names {c['cause']}, and this is failed landing"
        f" {c['n']} of the task with that cause. Do not land the task again."
        f" Read {log}. When the log shows that the change causes the failure,"
        " such as with a test that fills the memory or the disk, or with a"
        " failed test whose output only quotes such a line, sync the task with"
        f" that log as the reason: {sync_command(c)}. Otherwise note the task:"
        f" {note_command(c)}."
    )


def stopped_step_action(c):
    step, log = c["step"], c["log"]
    text = ""
    if c["unrestored"]:
        text = (
            "The landing did not restore the main checkout. Repair it before"
            f" the next landing. Confirm that no git process runs in {c['repo']},"
            " and remove the lock file that the error names. Then run the git"
            " command that the error names again there, and run git clean -fd"
            " there. "
        )
    text += (
        f"The landing stopped the {step}, which does not show that the task"
        f" must change. Find the cause in the last lines of {log}. Act on that"
        f" cause as the Landing section of SKILL.md says for {c['code']},"
        " before you start another landing."
    )
    if c["n"] > 1:
        text += (
            f" The task has ended in {c['code']} {c['n']} times. When the log"
            " ends at the same place as in the earlier landing, do not land the"
            " task again. Sync the task with the log when the log shows that"
            f" the change causes the wait: {sync_command(c)}. Otherwise note the"
            f" task: {note_command(c)}."
        )
    return text


def unhealthy_action(c):
    if not c["step"]:
        return (
            "The health command failed before the merge, so a service that the"
            " check or a commit hook requires is down. Repair the service and"
            " land again. Do not sync the task."
        )
    if c["n"] == 1:
        return (
            f"The health command failed after the {c['step']} failed, so a"
            f" service stopped during the {c['step']}. Repair the service and"
            " land again. Do not sync the task."
        )
    return (
        "The health command failed after a failed step of this task"
        f" {c['n']} times. Do not land the task again. Read {c['log']}. When the"
        " log shows that the change causes the failure, such as with a test"
        " that stops the service, sync the task with that log as the reason:"
        f" {sync_command(c)}. Otherwise note the task: {note_command(c)}."
    )


def no_worktree_action(c):
    ref, base = c["ref"], c["base"]
    if c["commit"]:
        return (
            f"An earlier landing recorded the commit {c['commit']} of the task"
            f" on {base}, removed the worktree, and then stopped. The task has"
            f" landed. Run ultraclose.py set {ref} landed, which sets the aven"
            " task to done."
        )
    lost = (
        "The worktree of the task went missing after its review, and the run"
        f" records no commit of the task on {base}. Run git worktree prune in"
        f" the main checkout {c['repo']}."
    )
    if c["has_branch"]:
        add = shlex.join(("git", "worktree", "add", c["wt"], c["branch"]))
        return (
            f"{lost} Then run {add} there, record the repair in notes.md, and"
            " land the task again."
        )
    if c["resumes"]:
        # Another repository of the task holds a commit of the task, so
        # the relaunch resumes at a review round, as args says. The sync
        # agent makes the change in the new branch.
        reason = (
            f"The landing found neither the worktree {c['wt']} nor the branch"
            f" {c['branch']} of the task. Make the change of the task in that"
            " worktree again."
        )
        sync = f"ultraclose.py args {ref} --sync {shlex.quote(reason)}"
        return (
            f"{lost} The branch {c['branch']} is gone as well, so the change of the"
            f" task there is lost. Another repository of the task holds a commit"
            f" of the task, so a plain relaunch resumes at a review round. Sync"
            f" the task with {sync} The relaunch cuts a new branch {c['branch']}"
            f" from {base} there. Land the task after its Workflow ends approved"
            " or landable."
        )
    return (
        f"{lost} The branch {c['branch']} is gone as well, so relaunch the task"
        f" with ultraclose.py args {ref}. The relaunch starts from {base} at"
        " the implementer. Land the task after its Workflow ends approved or"
        " landable."
    )


def other_empty_action(c):
    """The action of a landing whose branch in another repository lost its change.

    An earlier landing proposed the task in that repository. The branch
    there holds no change against its base branch when its pull request
    merged before the run recorded the merge, or when a fix reverted the
    change there. Either way the repository of the run can still hold a
    change of the task, so the task neither lands nor closes here. The
    merged command records that the base branch there holds the change,
    and the next landing proposes the rest of the task.
    """
    ref, name, url = c["ref"], c["name"], c["url"]
    read = (
        f"Read the state of the pull request {url} of the task there with"
        " mcp__github__pull_request_read, and close it when it is open."
        if url
        else (
            "The task records no pull request there. When the remote holds an open"
            f" pull request of {c['branch']}, close it."
        )
    )
    return (
        f"An earlier landing proposed the task in {name}, and its branch there now"
        f" holds no change against {c['base']}. So the pull request there merged,"
        f" or the task no longer changes {name}. {read} When it merged, bring the"
        f" merge into {c['base']} in the main checkout {c['repo']}. Then run"
        f" ultraclose.py merged {ref} --repo {name}, which records that"
        f" {c['base']} holds the change of the task there and keeps the task"
        " approved, and land the task again. Do not sync the task, and do not set"
        " it to landed or closed."
    )


def empty_action(c):
    ref, base, url = c["ref"], c["base"], c["url"]
    close = f"ultraclose.py set {ref} closed --note '<evidence>'"
    landed = f"ultraclose.py set {ref} landed"
    if c["name"]:
        return other_empty_action(c)
    if c["commit"]:
        return (
            f"An earlier landing of the task recorded the commit {c['commit']} on"
            f" {base}, and then it stopped before it removed the worktree. The"
            f" task has landed. Run {landed}, which sets the aven task to done"
            " and removes the worktree and the branch. Do not sync the task."
        )
    # A relaunch of a proposed task can end in EMPTY while its pull
    # request is still open, or after its pull request merged.
    merge = shut = ""
    if url:
        merge = (
            f" The merge of the pull request {url} of the task is such a commit,"
            " so read the state of that pull request with"
            " mcp__github__pull_request_read."
        )
        shut = f" Close the pull request {url} first when it is open."
    if c["merged"]:
        # The pull requests of the task in other repositories merged, so
        # the change of the task landed there.
        noun = "pull requests" if len(c["merged"]) > 1 else "pull request"
        return (
            f"The task holds no change in {c['repo']}, and its {noun} in"
            f" {' and '.join(c['merged'])} merged. So the change of the task has"
            f" landed.{shut} Run {landed}, which sets the aven task to done and"
            " removes the worktree and the branch. Do not sync the task."
        )
    if c["own"]:
        # The owners fact is None for a task of one repository. For a task
        # of several repositories, it names the main checkout and the base
        # branch of each repository whose branch held a change of its own.
        # The commit that made the change sits on the base branch there.
        owners = c["owners"]
        if owners is None:
            log = shlex.join(("git", "log", "--oneline", base))
            found = (
                f"A commit of {base} made the change of the task. Find that commit,"
                f" such as with {log} -- <file> for a file of the task. When the"
                " commit comes from this task"
            )
        else:
            found = " ".join(
                f"A commit of {onto} in the main checkout {path} made the change of"
                " the task there. Find that commit, such as with"
                f" {shlex.join(('git', '-C', path, 'log', '--oneline', onto))} --"
                " <file> for a file of the task there."
                for path, onto in owners
            )
            found += (
                " When the commit comes from this task"
                if len(owners) == 1
                else " When each commit comes from this task"
            )
            # The url fact names the pull request of the repository of the
            # run, whose merge made the change only there.
            if c["repo"] not in (path for path, _ in owners):
                merge = ""
        return (
            f"{found}, the task has landed, so run {landed}.{merge} When another"
            " commit made the change, such as the commit of a task that covers"
            f" this task, close the task: {close}.{shut} Do not sync the task."
        )
    # The resets fact is None for a task of one repository.
    several = c["resets"] is not None
    whose = (
        "The branches of the task hold no change of their own"
        if several
        else f"{c['branch']} holds no change of its own"
    )
    text = (
        f"{whose}. Read the summary and the notes of the Workflow result of the"
        " task. When they say that the task does not need a change, close the"
        f" task: {close}.{shut}"
    )
    if c["n"] > 1:
        return (
            f"{text} Otherwise note the task, which has ended in EMPTY"
            f" {c['n']} times: {note_command(c)}. Do not sync the task."
        )
    # The args command starts a task at the implementer only when no branch
    # of the task holds a commit that its base branch lacks. A
    # branch whose commits cancel each other out still holds such commits,
    # so the relaunch then resumes at a review of an empty diff. A task of
    # several repositories resets each branch that holds such commits.
    resets = c["resets"] if several else [(c["wt"], base)]
    steps = " and ".join(
        shlex.join(("git", "-C", wt, "reset", "-q", "--hard", onto))
        for wt, onto in resets
    )
    text += f" Otherwise run {steps}, and relaunch" if steps else " Otherwise relaunch"
    text += f" the task with ultraclose.py args {ref}."
    # The url fact names the pull request of the repository of the run. A
    # reset of the worktree there rewrites the branch of that pull request,
    # so the next push replaces the branch on the remote.
    if url and c["wt"] in (wt for wt, _ in resets):
        text += (
            " The remote holds the branch of that pull request, so set"
            " force_with_lease on the next push."
        )
    return f"{text} Do not sync the task."


def branch_taken_action(c):
    if c["held"]:
        return (
            f"Ask the user whether to rename or delete the branch {c['held']},"
            " and land again after the user does."
        )
    return (
        "Another command created a branch with the name of the pull request"
        " branch during the check. Land again, and the next landing names a"
        " free branch."
    )


def checkout_action(c):
    return (
        f"An agent or a hook changed the main checkout {c['repo']}. Stop"
        " landing, find the cause, and repair the checkout before the next"
        " landing."
    )


ACTIONS = {
    "OK": passed_action,
    "PR": passed_action,
    "CONFLICT": lambda c: (
        f"The base branch {c['base']} moved under the task. {sync_action(c)}"
    ),
    "CHECKFAIL": failed_step_action,
    "COMMITFAIL": failed_step_action,
    "CHECKTIMEOUT": stopped_step_action,
    "COMMITTIMEOUT": stopped_step_action,
    "HEALTHFAIL": unhealthy_action,
    "NOWORKTREE": no_worktree_action,
    "WTDIRTY": lambda c: (
        f"The worktree {c['wt']} holds changes that the task branch lacks."
        " Delete probe files and built binaries there, commit real changes"
        " with git add -A and git commit --no-verify, and land again."
    ),
    "NOMSG": lambda c: (
        f"{c['msg']} does not exist or holds no text. Relaunch the task with"
        f" ultraclose.py args {c['ref']} --sync '<a reason that asks for the"
        " commit message of the whole change>'. The sync agent then writes the"
        " message, and a reviewer checks it. Land the task again after its"
        f" Workflow ends approved or landable.{again(c)}"
    ),
    "MSGHASREF": lambda c: f"Remove each task ref from {c['msg']}, and land again.",
    "REFINDIFF": lambda c: (
        f"Remove each task ref from the lines that the change adds in {c['wt']},"
        " commit with git add -A and git commit --no-verify, and land again."
    ),
    "EMPTY": empty_action,
    # The landing checks msg.txt for task refs before it checks the commit
    # messages of the branch. So the one commit takes msg.txt as its
    # message, and the pull request shows the message of the change.
    "LOGHASREF": lambda c: (
        "The push publishes each commit of the task branch with its message."
        " Replace the commits of the branch with one commit that holds the"
        f" message of the task: cd {shlex.quote(c['wt'])} && git reset --soft"
        f' "$(git merge-base {shlex.quote(c["base"])} HEAD)" && git commit -q'
        f" --no-verify -F {shlex.quote(str(c['msg']))}. Then land again. When"
        " the remote holds the branch already, set force_with_lease on the next"
        " push."
    ),
    "BRANCHTAKEN": branch_taken_action,
    "DIRTY": checkout_action,
    "NOTBASE": checkout_action,
    "TREEMISMATCH": checkout_action,
    "POSTDIRTY": lambda c: (
        f"The task landed, and the main checkout {c['repo']} then held a"
        " change that the commit did not record. Stop landing, find the cause,"
        f" and repair the checkout before the next landing. {repairs(c)}Then run"
        " ultraclose.py next, because this landing did not run it. Do not land"
        " the task again."
    ),
}


def landable(st, ref):
    """The task with the given ref, which the land command takes.

    The command ends when the run holds no such task, or when the task
    is neither inflight nor approved. No task in another state has a
    change that waits for a landing. The error names the state of the
    task, and it says what a task in that state needs.
    """
    t = st["tasks"].get(ref) or die(f"unknown task {ref}")
    # A proposed task waits for its pull request to merge, and the merged
    # command lands it. The branch that the landing named for the pull
    # request already holds the change of the task, so a second landing
    # has nothing to propose.
    if t["state"] == "proposed":
        die(
            f"{ref} is proposed, so it waits for its pull request to merge."
            " Record the merge with the merged command. To change the"
            " pull request, relaunch the task with the args command, and"
            " land it after its review"
        )
    if t["state"] not in LANDABLE:
        # Each error says what a task in its state needs. A relaunch
        # reopens a landed or a closed task, so only the error for a
        # noted task names the args command.
        hint = {
            "pending": (
                "The next command starts the task, and the landing follows"
                " its review"
            ),
            "noted": (
                "To make a new attempt at the task, relaunch it with the"
                " args command, and land it after its review"
            ),
            "landed": "A landed task does not need a landing or a relaunch",
            "closed": "A closed task does not need a landing or a relaunch",
        }[t["state"]]
        die(
            f"{ref} is {t['state']}, so it has no change that waits for a"
            " landing. The land command takes only an inflight task or an"
            f" approved task, and it changed nothing. {hint}"
        )
    return t


def cmd_land(a):
    repo = main_checkout()
    run = run_dir(repo)
    ref = a.ref
    with state(run) as st:
        cfg = st["config"]
        # A failed landing that leaves no commit on the base branch leaves
        # its task approved. An approved task keeps its file locks and
        # keeps the run from its end. So the command refuses each task
        # that has no change to land before it sets the state.
        t = landable(st, ref)
        # A task that waits for its landing frees its slot and keeps its
        # file locks.
        t["state"] = "approved"
        prefixes = sorted({r.split("-")[0] for r in st["tasks"]})
    refs = re.compile(rf"\b(?:{'|'.join(map(re.escape, prefixes))})-[0-9A-Z]{{4,}}\b")
    wt, base = worktree(cfg, ref), cfg["base"]
    msg = run / "tasks" / ref / "msg.txt"
    logs = run / "logs"
    timeout = step_timeout(cfg)

    # How long each timed step of this landing took, in seconds.
    times = {}
    # The facts of this landing that each action of ACTIONS reads. The
    # landing adds the branch of the task and the merged repositories
    # after it takes the landing lock.
    facts = {
        "ref": ref,
        "base": base,
        "wt": wt,
        "repo": repo,
        "msg": msg,
        "next": a.next,
        "head": "",
        "others": [],
        "name": "",
        "dropped": None,
    }

    def place_facts(p):
        """The facts of the landing that name one repository of the task.

        A failure in another repository names that repository in the
        action of its result. The facts of the repository of the run
        equal the facts of the landing.
        """
        return {
            "name": p["name"],
            "base": p["base"],
            "branch": p["branch"],
            "wt": p["wt"],
            "repo": p["repo"],
            "msg": run / "tasks" / ref / p["msg"],
        }

    def step_log(step, p):
        """The name of the log of a step in one repository of the task.

        The logs of the repository of the run take the name of the step,
        and the logs of another repository add the name of that
        repository, such as x.check for the check of the repository x.
        """
        return f"{p['name']}.{step}" if p["name"] else step

    def record(code, reason=""):
        """Record the landing with its result, and print its times.

        The TIMES line gives each time with the median of the earlier
        landings with a result of PASSED. A SLOW line follows it for each
        step of slow_steps(). A landing that ran no timed step records no
        times and prints neither line.

        The reason says why a failed landing failed. The run counts the
        failed landings of each task by reason. Return the count for the
        reason, with this landing. The count is 1 for a landing without a
        reason.

        The command prints the RESULT line after this function returns.
        So the function reports a state file that it cannot write, such
        as on a full disk, and returns.
        """
        if not times and not reason:
            return 1
        typical, n = {}, 1
        try:
            with state(run) as st:
                if times:
                    # A run can continue from a state file that has no
                    # landings key.
                    landings = st.setdefault("landings", [])
                    typical = medians(landings)
                    landings.append({"ref": ref, "result": code, "times": times})
                if reason:
                    failures = st["tasks"][ref].setdefault("failures", {})
                    n = failures[reason] = failures.get(reason, 0) + 1
        except OSError as e:
            print(f"the run did not record this landing: {e}")
        if times:
            print(f"TIMES {times_text(times, typical)}")
        for step in slow_steps(times, typical):
            print(
                f"SLOW the {step} took {span(times[step])}, more than twice its"
                f" median of {span(typical[step])}. Find the cause before you"
                " start another landing. A build cache that evicted its content"
                " is a common cause, and ultraclose.py status prints the free"
                " space of each watched file system."
            )
        return n

    def act(code, n=1, **more):
        """Print the ACTION line of the landing, as ACTIONS gives it for the result.

        The line of a failure in another repository opens with the name of
        that repository, the worktree of the task there, and its branch.
        """
        c = {**facts, "code": code, "n": n, **more}
        text = ACTIONS[code](c)
        if c["name"] and code not in PASSED:
            text = (
                f"The landing failed in the repository {c['name']}, in the worktree"
                f" {c['wt']} of the main checkout {c['repo']}, on the branch"
                f" {c['branch']}. {text}"
            )
        print(f"ACTION {text}")

    def fail(code, detail="", reason=None, **more):
        """End a failed landing with its ACTION line and its RESULT line.

        The reason says why the landing failed, and it defaults to the
        result. The run counts the failed landings of the task by reason,
        as record() says. An empty reason keeps the landing out of the
        count. The other arguments are the facts that the action of the
        result reads beside the facts of every landing. They can replace
        the facts of the landing with the facts of another repository of
        the task, as place_facts() gives them.
        """
        # The RESULT line comes last, so the end of the output shows it
        # however long the detail is.
        if detail:
            print(detail)
        act(code, record(code, code if reason is None else reason), **more)
        print(f"RESULT {ref} {code}")
        sys.exit(2)

    def task_now():
        """The task of the landing, as the state of the run holds it now."""
        return json.loads((run / "state.json").read_text())["tasks"][ref]

    def url_now(p):
        """The pull request that the task records in one repository now, or an empty text."""
        t = task_now()
        if p["name"]:
            t = t.get("repos", {}).get(p["name"], {})
        return t.get("pr", "")

    def landed_commit():
        """The commit of the task that the base branch holds, or an empty text.

        A landing records the commit that it adds to the base branch
        before it removes the worktree. So a landing that stops after its
        commit leaves the record, and a later landing of the task finds
        it.
        """
        commit = task_now().get("commit")
        on_base = ("git", "merge-base", "--is-ancestor", str(commit), base)
        if not commit or sh(*on_base, cwd=repo, check=False).returncode:
            return ""
        return out("git", "rev-parse", "--short", commit, cwd=repo)

    def failed_step(code, step, log, **more):
        """End a landing whose check or commit failed.

        The output names the failure of the environment that the log of
        the step shows, as environment_failure() finds it. The run then
        counts the landing by that cause.
        """
        detail, cause = tail(log), ""
        if found := environment_failure(log):
            cause, line = found
            detail = f"{detail}\nthe log of the {step} names {cause}: {line}".strip()
        fail(
            code, detail, reason=cause or code, step=step, log=log, cause=cause, **more
        )

    def emptied(own, p, detail="", several=False):
        """End a landing whose task branch holds no change against the base branch.

        The p argument names the repository of the branch. The own
        argument says whether the task branch held a change of its own
        before the landing merged the base branch into it. The detail
        defaults to a line that names the branch.

        The several argument says that no repository of a task of
        several repositories holds a change, and the result then holds
        two more facts. The owners fact names the main checkout and the
        base branch of each repository whose branch held a change of its
        own, because a commit of that base branch made the change. The
        resets fact names the worktree and the base branch of each
        repository whose branch holds a commit that its base branch
        lacks.

        When a pull request of the task merged in another repository,
        the action says that the task has landed. Otherwise, when a
        branch held a change of its own, the action names each
        repository of the owners fact with a git log command that finds
        the commit there, and it gives no git reset. When no branch held
        a change of its own and the action asks for a relaunch, it first
        gives a git reset of each worktree of the resets fact.
        """
        fail(
            "EMPTY",
            detail or f"{p['branch']} holds no change against {p['base']}",
            own=own,
            commit="" if p["name"] else landed_commit(),
            url=url_now(p),
            resets=(
                [(q["wt"], q["base"]) for q in places if commits_ahead(q)]
                if several
                else None
            ),
            owners=(
                [(q["repo"], q["base"]) for q in places if q["own"]]
                if several
                else None
            ),
            **place_facts(p),
        )

    def logged(name, *cmd, cwd, env=None, limit=None):
        """Run a step, and return its exit status and the path of its log.

        The exit status is None when the step ran longer than limit
        seconds and the landing stopped it, as run_step says.
        """
        path = logs / f"{ref}.{name}.log"
        return run_step(cmd, cwd, env, path, limit), path

    def timed(name, *cmd, cwd, env=None, log=None):
        """Run a logged step under the step timeout of the run.

        Add how long the step took to the times. The log names the log
        of the step, and it defaults to the name of the step. The checks
        of all the repositories of a task add to one time.
        """
        began = time.monotonic()
        rc, path = logged(log or name, *cmd, cwd=cwd, env=env, limit=timeout)
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

    def require_health(step="", log=""):
        """Fail the landing with HEALTHFAIL when the health command fails.

        The check and the commit hooks cannot run when the health command
        fails. So a failed check or a failed commit then does not show that
        the task needs a change. The step argument names the failed step
        that the health command runs after, and the log argument names the
        log of that step. Without a step, the health command runs before
        the merge. The run counts only the landings whose health command
        failed after a step.
        """
        # A run can continue from a state file that has no health key.
        if not cfg.get("health"):
            return
        rc, path = logged("health", "sh", "-c", cfg["health"], cwd=repo)
        if rc:
            when = f"after the {step} failed, see {log}" if step else "before the merge"
            detail = f"the health command failed {when}\n{tail(path)}"
            reason = "HEALTHFAIL" if step else ""
            fail("HEALTHFAIL", detail.rstrip(), reason=reason, step=step, log=log)

    def passed(*cmd, left, cwd=repo):
        """Run a command that ends the landing, and return whether it passed.

        The base branch holds the commit of the task by then, or the task
        branch has the name of its pull request. So the landing does not
        stop at a failed command. It prints the command and the output of
        the command. Then it prints the text that the left function
        returns, which says what the command left and what the caller does
        about it. The function runs after the command, so the text can
        depend on what the command left. The command runs in the main
        checkout of the repository of the run, unless cwd names another.
        """
        p = sh(*cmd, cwd=cwd, check=False)
        if p.returncode:
            print(f"{' '.join(cmd)} failed:\n{p.stdout}{p.stderr}".rstrip())
            print(left())
        return not p.returncode

    def remove_worktree(did, p):
        """Remove the worktree of the task in one repository, and return what git left.

        The p argument names the repository. The text is empty when git
        removed the worktree. It is "worktree" when git still lists the
        worktree, and "directory" when git no longer lists the worktree
        but its directory stays. The actions of OK, PR, and POSTDIRTY read
        the text as the unremoved fact.

        The did argument opens the line that passed() prints after a
        failure, and it says what the landing did with the task. git can
        fail after it deleted its record of the worktree, such as when it
        cannot delete a file in the directory of the worktree. Only the
        directory then stays, and a second run of the git command fails.
        So the line then tells the caller to delete the directory.
        """
        left = ""
        path = p["wt"]

        def unremoved():
            nonlocal left
            listed = listed_worktrees(p["repo"])
            if listed is None or os.path.realpath(path) in listed:
                left = "worktree"
                return f"{did}, but its worktree still exists. {AGAIN}"
            left = "directory"
            return (
                f"{did}, and git no longer lists its worktree, but the directory of"
                " the worktree still exists. Repair the cause of the error, and"
                " delete the directory."
            )

        remove = ("git", "worktree", "remove", "--force", path)
        passed(*remove, left=unremoved, cwd=p["repo"])
        return left

    def propose(live):
        """End a landing that proposes the task as pull requests.

        The live argument names each repository of the task that the
        landing proposes, with the name of its pull request branch as
        head. The branch of the task takes that name in each of them. The
        worktrees go, and the branches stay for the push. The task waits
        as proposed for its pull requests to merge, and the merged command
        lands it.

        The script contacts no remote. The landing prints one REPO line
        per other repository, before the ACTION line, and the RESULT line
        last. Each line names the branch that the caller pushes and the
        base branch of the pull request. The RESULT line names the branch
        of the repository of the run, when the landing proposes the task
        there. Each line also gives the address of a pull request that
        the task records. The caller then reads the state of that pull
        request, and opens no second pull request while it is open. The
        task records that each branch waits for its push, until the opened
        command records the push.

        The branch of the repository of the run can hold no change of the
        task here, although an earlier landing proposed the task there.
        The facts of the landing then name that repository as dropped. The
        landing removes the worktree there too, and the task keeps the
        branch and the pull request there. The task records that the
        branch holds no change, so the status command does not ask for its
        push. The merged command records that pull request after each other
        pull request merged, and the action says so.

        Return whether git removed every worktree. Each branch has its
        name before those git commands run. So when one of them fails, the
        landing prints it, and the task still waits as proposed.
        """
        for p in live:
            head = p["head"]
            if head == p["branch"]:
                continue
            # Another command can create a branch with the name during the
            # check. The landing then fails with its RESULT line last.
            rename = ("git", "branch", "-m", p["branch"], head)
            renamed = sh(*rename, cwd=p["repo"], check=False)
            if renamed.returncode:
                fail("BRANCHTAKEN", renamed.stderr.strip(), held="", **place_facts(p))
            with state(run) as st:
                t = st["tasks"][ref]
                (t["repos"][p["name"]] if p["name"] else t)["branch"] = head
            p["branch"] = head
        # git can fail here, such as with a locked worktree. A landing
        # that stops at that failure leaves an approved task, and each
        # later landing of that task stops at the same failure. So the
        # landing reports the failure and goes on.
        dropped = facts["dropped"]
        unremoved = {
            p["name"]: remove_worktree("The landing proposed the task", p)
            for p in live + ([dropped] if dropped else [])
        }
        with state(run) as st:
            t = st["tasks"][ref]
            t["state"] = "proposed"
            # A later landing that proposes the branch of the repository of
            # the run clears the record.
            t.pop("emptied", None)
            if dropped:
                t["emptied"] = True
            for p in live:
                held = t["repos"][p["name"]] if p["name"] else t
                # A relaunch can add commits to the branch of a pull
                # request that the task records, so every landing waits
                # for a push.
                held["pushed"] = False
                p["url"] = held.get("pr", "")
        record("PR")
        own = live[0] if not live[0]["name"] else None
        others = [
            {
                "name": p["name"],
                "path": p["repo"],
                "base": p["base"],
                "wt": p["wt"],
                "head": p["head"],
                "url": p["url"],
                "unremoved": unremoved[p["name"]],
            }
            for p in live
            if p["name"]
        ]
        for o in others:
            opened = f" pr={o['url']}" if o["url"] else ""
            print(
                f"REPO {o['name']} head={o['head']} base={o['base']}"
                f" path={o['path']}{opened}"
            )
        act(
            "PR",
            head=own["head"] if own else "",
            url=own["url"] if own else "",
            unremoved=unremoved.get("", ""),
            undeleted=False,
            open=False,
            others=others,
            dropped=dropped and {"branch": dropped["branch"], "url": url_now(dropped)},
        )
        if own:
            opened = f" pr={own['url']}" if own["url"] else ""
            print(f"RESULT {ref} PR head={own['head']} base={base}{opened}", flush=True)
        else:
            print(f"RESULT {ref} PR", flush=True)
        return not any(unremoved.values())

    def squash(own):
        """End a landing that commits the task on the base branch.

        The main checkout gets the task branch as one commit with the
        message of the task. The worktree and the branch go, and the task
        lands. The own argument says whether the task branch held a change
        of its own before the landing merged the base branch into it.

        After the commit, the main checkout can hold a change that the
        commit did not record. The task still lands, and the landing
        then fails with the result POSTDIRTY.

        Return whether git removed the worktree, git deleted the branch,
        and aven set its task to done. The base branch holds the commit
        before these commands run. So when one of them fails, the landing
        prints it, and the task lands.
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
            emptied(own, places[0])
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
                    unrestored = True
                else:
                    unrestored = False
                fail(
                    "COMMITTIMEOUT",
                    detail,
                    step="commit",
                    log=log,
                    unrestored=unrestored,
                )
            if rc:
                rollback()
                require_health("commit", log)
                failed_step("COMMITFAIL", "commit", log)
            if out("git", "rev-parse", "HEAD^{tree}", cwd=repo) == want:
                break
            if out("git", "rev-parse", "HEAD~1", cwd=repo) != head or attempt == 2:
                rollback()
                fail("TREEMISMATCH", f"the commit did not record tree {want}")
            sh("git", "reset", "-q", "--soft", head, cwd=repo)
            sh("git", "read-tree", want, cwd=repo)
        # The base branch holds the commit of the task from here on. The
        # run records the commit, so that a landing that stops before the
        # task lands leaves a record for landed_commit().
        recorded = out("git", "rev-parse", "HEAD", cwd=repo)
        try:
            with state(run) as st:
                st["tasks"][ref]["commit"] = recorded
        except OSError as e:
            print(f"the run did not record the commit of this landing: {e}")
        # Another landing of the task finds no change to commit. So the
        # task lands before the landing reports the changes that the
        # main checkout holds.
        dirty = uncommitted(repo)
        # git and aven can fail here, such as git with a locked worktree
        # and aven with a locked database. A landing that stops at such a
        # failure leaves an approved task, and no second landing can land
        # that task. So the landing reports each failure and goes on. git
        # keeps the branch of a worktree that it did not remove, and the
        # landing then reports both git commands.
        delete = ("git", "branch", "-q", "-D", br)
        edit = ("aven", "edit", ref, "--status", "done")
        landed = "The task landed"
        # What the failed commands left, as the actions of OK and
        # POSTDIRTY read it.
        left = {
            "unremoved": remove_worktree(landed, places[0]),
            "undeleted": not passed(
                *delete, left=lambda: f"{landed}, but its branch still exists. {AGAIN}"
            ),
            "open": not passed(
                *edit,
                left=lambda: f"{landed}, but the aven task is still open. Run the aven"
                " command again.",
            ),
        }
        with state(run) as st:
            st["tasks"][ref]["state"] = "landed"
        commit = out("git", "log", "-1", "--format=%h %s", cwd=repo)
        if dirty:
            # The task landed, so the run does not count this landing
            # among the failed landings of the task.
            fail(
                "POSTDIRTY",
                f"{ref} landed as {commit}, and the main checkout holds changes"
                f" that the commit did not record:\n{dirty}",
                reason="",
                **left,
            )
        record("OK")
        act("OK", **left)
        # Python buffers the standard output of a background call. The
        # flush writes the result to the output before next runs, so the
        # result stays there when the caller stops the command during next.
        print(f"RESULT {ref} OK {commit}", flush=True)
        return not any(left.values())

    # Every landing that proposes a pull request leaves the main checkout
    # alone. So neither the branch nor the changes of the main checkout
    # stop it.
    pr = proposes(cfg)
    with locked(run / "land.lock"):
        # Another landing of the task can hold the lock while this landing
        # waits for it. That landing sets the task to landed or proposed,
        # and the task then has no change that waits for a landing. So the
        # command refuses the task as it does before the wait. The merged
        # command can also record the merge of another repository of the
        # approved task before this landing takes the lock. So the landing
        # reads the repositories of the task after the wait.
        with state(run) as st:
            t = landable(st, ref)
            br = branch(t, ref)
            # The repository of the run comes first. A task that changes
            # other repositories also has a worktree and a branch in each
            # of them.
            places = task_repos(cfg, t, ref)
            # The other repositories whose pull request of the task merged.
            # task_repos() leaves them out.
            merged = sorted(n for n, e in t.get("repos", {}).items() if e.get("merged"))
        facts.update(branch=br, merged=merged)
        if not pr:
            if out("git", "branch", "--show-current", cwd=repo) != base:
                fail("NOTBASE", f"the main checkout is not on {base}")
            if dirty := uncommitted(repo):
                fail("DIRTY", dirty)
        for p in places:
            if not Path(p["wt"]).exists():
                has = branch_exists(p["repo"], p["branch"])
                fail(
                    "NOWORKTREE",
                    p["wt"],
                    commit="" if p["name"] else landed_commit(),
                    has_branch=has,
                    # The args command resumes the task at a review round
                    # when another repository of the task holds a commit
                    # of the task. A branch that is gone then needs a sync,
                    # because no review round makes its change again.
                    resumes=not has
                    and any(commits_ahead(q) for q in places if q is not p),
                    **place_facts(p),
                )
            if dirty := uncommitted(p["wt"]):
                fail("WTDIRTY", dirty, **place_facts(p))

        # Whether each task branch holds a change of its own. The merge of
        # the base branch can make the change of the branch empty, because
        # a commit of the base branch made the same change. The action of
        # the result EMPTY tells that case from a branch whose own commits
        # change nothing.
        for p in places:
            fork = sh("git", "merge-base", p["base"], p["branch"], cwd=p["repo"], check=False)
            unchanged = ("git", "diff", "--quiet", fork.stdout.strip(), p["branch"])
            p["own"] = bool(
                fork.returncode or sh(*unchanged, cwd=p["repo"], check=False).returncode
            )
        own = places[0]["own"]
        # A task of one repository lands its branch whatever the branch
        # holds, so an empty branch ends the landing with EMPTY. A task of
        # several repositories proposes each repository whose branch holds a
        # change of its own, and each repository that an earlier landing
        # proposed. Its worktree and its branch stay in each repository in
        # which no landing proposed the task, the repository of the run
        # included, until the task lands. A task whose pull request merged
        # in another repository still counts as a task of several
        # repositories, although task_repos() leaves that repository out.
        several = len(places) > 1 or bool(merged)
        live = [p for p in places if not several or p["own"] or p["proposed"]]
        if not live:
            emptied(
                False,
                places[0],
                "\n".join(
                    f"{p['branch']} of {p['repo']} holds no change against {p['base']}"
                    for p in places
                ),
                several=True,
            )
        for p in live:
            # A branch without a change of its own equals its base branch
            # after the landing merges the base branch into it. So the
            # landing proposes nothing there and reads no message there.
            # The prompts of close-task.js tell the agents to write no
            # message for a repository that the change does not touch.
            if several and not p["own"]:
                continue
            text = run / "tasks" / ref / p["msg"]
            if not text.exists() or not text.read_text().strip():
                fail("NOMSG", str(text), **place_facts(p))
            if found := refs.findall(text.read_text()):
                fail("MSGHASREF", " ".join(sorted(set(found))), **place_facts(p))
        require_health()

        for p in list(live):
            path, name, onto = p["repo"], p["branch"], p["base"]
            # Bring the base branch into the task branch, so that the check
            # runs on the tree that the commit or the pull request records.
            behind = sh(
                "git", "merge-base", "--is-ancestor", onto, name, cwd=path, check=False
            )
            if behind.returncode:
                # The default message of a merge names the task branch, and
                # that name holds the task ref. A landing that proposes a
                # pull request leaves the merge commit for the push.
                merge = ("git", "merge", "--no-verify", "--no-edit")
                rc, log = logged(
                    step_log("merge", p),
                    *merge,
                    "-m",
                    f"Merge branch '{onto}'",
                    onto,
                    cwd=p["wt"],
                )
                if rc:
                    files = out(
                        "git", "diff", "--name-only", "--diff-filter=U", cwd=p["wt"]
                    )
                    sh("git", "merge", "--abort", cwd=p["wt"], check=False)
                    fail("CONFLICT", files or tail(log), **place_facts(p))
            added = [
                line
                for line in out(
                    "git", "diff", "-U0", f"{onto}...{name}", cwd=path
                ).splitlines()
                if line.startswith("+")
                and not line.startswith("+++")
                and refs.search(line)
            ]
            if added:
                fail("REFINDIFF", "\n".join(added[:20]), **place_facts(p))
            # The task branch holds the base branch here, and a pull request
            # needs a change to show. A landing that commits finds an empty
            # change when it stages the commit.
            if not pr:
                continue
            same = sh("git", "diff", "--quiet", onto, name, cwd=path, check=False)
            if not same.returncode:
                # A commit of the base branch of another repository made
                # the change of the task there. The landing proposes the
                # other repositories of the task, unless an earlier landing
                # proposed the task there.
                if several and not p["proposed"]:
                    live.remove(p)
                    continue
                # The user can merge the pull request of the repository of
                # the run early, and a fix can revert the change there. The
                # other repositories can still hold a change. So the landing
                # proposes them, and the merged command records that pull
                # request after theirs. The landing ends with EMPTY when no
                # other repository holds a change.
                if several and not p["name"]:
                    live.remove(p)
                    facts["dropped"] = p
                    continue
                emptied(p["own"], p)
            # The push publishes each commit of the task branch with its
            # message. Each text holds the short ID of one commit and its
            # message.
            texts = out("git", "log", "-z", "--format=%h %B", f"{onto}..{name}", cwd=path)
            named = [t.splitlines()[0] for t in texts.split("\0") if refs.search(t)]
            if named:
                fail("LOGHASREF", "\n".join(named[:20]), **place_facts(p))
            # An earlier landing of the task named its branch. Otherwise
            # the landing names the branch here, so that a blocked name
            # fails the landing before the check.
            head = p["record"].get("branch")
            if not head:
                taken = known_branches(path)
                text = (run / "tasks" / ref / p["msg"]).read_text()
                head = pr_branch(text, taken)
                if held := above(head, taken):
                    fail(
                        "BRANCHTAKEN",
                        f"a local branch or a remote branch has the name {held},"
                        " and git keeps no branch below the name of another"
                        f" branch. So {held} blocks the branch {head} of the pull"
                        f" request. Rename or delete each branch {held}, and land"
                        " again",
                        held=held,
                        **place_facts(p),
                    )
            p["head"] = head
        if pr and not live:
            emptied(
                any(p["own"] for p in places),
                places[0],
                "\n".join(
                    f"{p['branch']} of {p['repo']} holds no change against {p['base']}"
                    for p in places
                ),
                several=True,
            )
        for p in live:
            if not p["check"]:
                continue
            # The cache sits in the git directory of the worktree. So the
            # worktree stays clean, and git removes the cache together
            # with the worktree.
            git_dir = out("git", "rev-parse", "--absolute-git-dir", cwd=p["wt"])
            rc, log = timed(
                "check",
                "sh",
                "-c",
                p["check"],
                cwd=p["wt"],
                env=lint_env(Path(git_dir, LINT_CACHE)),
                log=step_log("check", p),
            )
            if rc is None:
                fail(
                    "CHECKTIMEOUT",
                    overran("check", log),
                    step="check",
                    log=log,
                    unrestored=False,
                    **place_facts(p),
                )
            if rc:
                require_health("check", log)
                failed_step("CHECKFAIL", "check", log, **place_facts(p))
            if dirty := uncommitted(p["wt"]):
                fail("WTDIRTY", "the check changed files:\n" + dirty, **place_facts(p))
        # Whether each command that ends the landing passed. The landing
        # proposes or lands its task also when one of them failed.
        finished = propose(live) if pr else squash(own)
    # Each failed landing exits before this line. The landing lock is free
    # here, so a queued landing does not wait for the tasks that start.
    if a.next:
        fill_slots(run)
    # The landing printed each command that failed above its RESULT
    # line. The failure status sends the caller to that output.
    if not finished:
        sys.exit(1)


def proposed(st, ref, command, approved=False):
    """The proposed task with the given ref.

    The command ends when the run holds no such task, or when the task
    has another state. Only a landing that proposes a pull request sets
    a task to proposed. With approved, the command also takes an
    approved task.
    """
    t = st["tasks"].get(ref) or die(f"unknown task {ref}")
    if t["state"] == "proposed" or (approved and t["state"] == "approved"):
        return t
    takes = "a proposed task or an approved task" if approved else "a proposed task"
    die(
        f"{ref} is {t['state']}, and the {command} command takes only {takes}. A"
        " landing with the result PR sets its task to proposed"
    )


def proposal(cfg, t, ref, name, command):
    """The facts of one repository in which a landing proposed a task.

    The name names another repository of the run, and None stands for the
    repository of the run. The facts come from task_repos(). The command
    ends when the task does not change that repository, when no landing
    proposed the task there, and when its pull request there merged.
    """
    where = f"the repository {name}" if name else f"the repository {cfg['repo']}"
    if name and name not in t.get("repos", {}):
        changed = " ".join(sorted(t.get("repos", {}))) or "none"
        die(
            f"{ref} does not change the repository {name}. The other repositories"
            f" of {ref} are {changed}"
        )
    if name and t["repos"][name].get("merged"):
        die(f"the pull request of {ref} in {where} merged already")
    found = next(p for p in task_repos(cfg, t, ref) if p["name"] == (name or ""))
    if not found["proposed"]:
        die(
            f"the landing of {ref} proposed no pull request in {where}, because"
            f" the branch of {ref} there held no change. The {command} command"
            " takes the repository of a REPO line or of the RESULT line of that"
            " landing"
        )
    return found


def cmd_opened(a):
    repo = main_checkout()
    run = run_dir(repo)
    with state(run) as st:
        t = proposed(st, a.ref, "opened")
        held = proposal(st["config"], t, a.ref, a.repo, "opened")["record"]
        held["pr"], held["pushed"] = a.url, True
    where = f" in {a.repo}" if a.repo else ""
    print(f"{a.ref} proposed{where} {a.url}")


def pin_hint(ref, t, cfg):
    """What the merged command says about each pin to a commit that a merge made.

    The commit of a task branch in another repository reaches a remote
    only through the merge of its pull request. So the agents keep each
    pin in the repository of the run to a commit of such a repository at
    its old value, and a sync after the merges moves each such pin. The
    text names each other repository whose merge the task records, with
    the commit of its base branch when the run recorded the merge.
    """
    held = other_repos(cfg)
    merged = sorted((n, e["merged"]) for n, e in t.get("repos", {}).items() if e.get("merged"))
    names = " or ".join(n for n, _ in merged)
    commits = " ".join(
        f"The branch {held[n]['base']} of {n} held the merge at {commit} when the"
        " run recorded it."
        for n, commit in merged
    )
    return (
        f"When the change of the task in {cfg['repo']} needs a pin to a commit of"
        f" {names}, such as a flake input, sync the task so that each such pin"
        f" names a commit that holds the merge: ultraclose.py args {ref} --sync"
        f" '<each merged commit and each pin to move>'. {commits} Land the task"
        " after its Workflow ends approved or landable."
    )


def cmd_merged(a):
    repo = main_checkout()
    run = run_dir(repo)
    # A landing sets its task to approved before it waits for the landing
    # lock. It reads the worktrees and the branches of the task while it
    # holds the lock. So the command takes an approved task only while it
    # holds that lock. A proposed task has no landing, so the command takes
    # it while a landing of another task holds the lock.
    with tried(run / "land.lock") as free, state(run) as st:
        cfg = st["config"]
        # The landing of a relaunch leaves its task approved. The pull
        # request of another repository can merge before the run records
        # the merge, and that landing then ends in EMPTY in that
        # repository. So the command records the merge of another
        # repository for an approved task too.
        t = proposed(st, a.ref, "merged", approved=bool(a.repo))
        approved = t["state"] == "approved"
        if approved and not free:
            die(
                f"{a.ref} is approved, and a landing holds the landing lock of the"
                f" run. A landing of {a.ref} can run or wait for that lock, and it"
                f" reads the worktree and the branch of {a.ref} in {a.repo}, which"
                " the merged command removes. The command changed nothing. Run it"
                " again when no landing runs"
            )
        p = proposal(cfg, t, a.ref, a.repo, "merged")
        path, name, base, wt = p["repo"], p["branch"], p["base"], p["wt"]
        places = task_repos(cfg, t, a.ref)
        # The change of the repository of the run can depend on the change
        # of each other repository, such as with a pin to a commit. So its
        # pull request merges last, and the run records its merge last.
        waiting = [q for q in places[1:] if q["proposed"]]
        if not a.repo and waiting:
            names = " ".join(q["name"] for q in waiting)
            die(
                f"the pull request of {a.ref} in {path} merges after the pull"
                f" requests of {a.ref} in the other repositories, because its"
                " change can depend on theirs, such as with a pin to a commit. The"
                f" run records no merge in {names} yet. Record the merge of each of"
                f" them first with merged {a.ref} --repo NAME. The command changed"
                " nothing"
            )
        # A task whose branch is gone has no change to compare.
        has = branch_exists(path, name)
        if has and not a.force and not holds(path, base, name):
            die(
                f"{base} lacks the change of {a.ref}. A merge of {name} into"
                f" {base} has a conflict or changes a file, and so does a merge"
                f" into each commit that {base} gained after the fork of {name}."
                " The command changed nothing. Bring the merged pull request"
                f" into {base} first, such as with a pull while the main"
                f" checkout {path} has {base} checked out. Pass --force when the"
                f" pull request merged with commits that {name} lacks"
            )
        # The base branch of another repository holds the merge here. The
        # merged command names its commit in the text about each pin to a
        # commit of that repository.
        at = out("git", "rev-parse", base, cwd=path) if a.repo else ""
        # The relaunch of an approved task gave it a worktree in the
        # repository again, and that worktree keeps the branch. The base
        # branch holds the change of the branch, so the worktree goes,
        # unless it holds a change that the branch lacks.
        # What the command changed before a later step fails.
        changed = "changed nothing"
        if approved and Path(wt).exists():
            if dirty := uncommitted(wt):
                die(
                    f"the worktree {wt} of {a.ref} holds changes that the branch"
                    f" {name} lacks, so the command changed nothing:\n{dirty}\nCommit"
                    " or delete them, and run the command again"
                )
            removed = sh("git", "worktree", "remove", wt, cwd=path, check=False)
            if removed.returncode:
                die(
                    f"git cannot remove the worktree {wt}, so the command changed"
                    f" nothing:\n{removed.stderr.strip()}\nRepair the cause that git"
                    " names, and run the command again"
                )
            changed = f"removed the worktree {wt} and changed nothing else"
        # The branch goes before the aven task is done, as in a landing
        # that commits. A command that fails between the two steps then
        # leaves a task without a branch, and the next merged command
        # finishes it.
        if has:
            gone = sh("git", "branch", "-q", "-D", name, cwd=path, check=False)
            if gone.returncode:
                # The landing that proposed the task can fail to remove the
                # worktree of the task, which then keeps the branch. A
                # directory that git no longer lists as a worktree keeps no
                # branch, and neither does a worktree on another branch.
                # git acts on the worktrees of the checkout that it runs in,
                # so the hint names the main checkout of the repository.
                listed = listed_worktrees(path) or {}
                if listed.get(os.path.realpath(wt)) == name:
                    hint = (
                        f"The landing that proposed {a.ref} did not remove its"
                        f" worktree {wt}, which keeps the branch. Remove the"
                        f" worktree with git worktree remove --force {wt} in the"
                        f" main checkout {path}, and repair the cause that git"
                        " names when it fails. Then run the command again"
                    )
                else:
                    hint = (
                        f"A worktree that has {name} checked out keeps the"
                        " branch. Check out another branch there, and run the"
                        " command again"
                    )
                die(
                    f"git cannot delete the branch {name}, so the command {changed}:"
                    f"\n{gone.stderr.strip()}\n{hint}"
                )
        if a.repo:
            p["record"]["merged"] = at
        # The task lands when every pull request of the task merged. The
        # landing proposed no pull request in a repository whose branch
        # held no change, and the worktree and the branch of the task stay
        # there until the task lands. An approved task waits for its
        # landing, which proposes the rest of the task.
        rest = [q for q in task_repos(cfg, t, a.ref) if q["proposed"]]
        landing = not approved and (not a.repo or not rest)
        if landing:
            sh("aven", "edit", a.ref, "--status", "done", cwd=repo)
            t["state"] = "landed"
            for q in task_repos(cfg, t, a.ref):
                if not q["proposed"]:
                    clear_work(q, "landed", owns(run, a.ref))
    if a.repo:
        print(f"{a.ref} merged in {a.repo}")
    # The text about the pins waits for the merge of each other pull
    # request, so that one sync moves every pin.
    others = [q["name"] for q in rest if q["name"]]
    if approved and others:
        print(
            f"{a.ref} stays approved, and it waits for the merge of its pull"
            f" requests in {' '.join(others)}. Land the task again: ultraclose.py"
            f" land {a.ref} --next"
        )
        return
    if approved:
        print(
            f"{a.ref} stays approved. {pin_hint(a.ref, t, cfg)} Otherwise land"
            f" the task again: ultraclose.py land {a.ref} --next"
        )
        return
    if landing:
        print(f"{a.ref} landed")
        if a.repo:
            # The task proposed no pull request in the repository of the
            # run, so no pin there names the commit that the merge made.
            print(
                f"The task proposed no pull request in {repo}."
                f" {pin_hint(a.ref, t, cfg)} The sync starts the landed task again"
            )
        return
    if others:
        print(f"{a.ref} waits for the merge of its pull requests in {' '.join(others)}")
        return
    # The user can merge the pull request of the repository of the run
    # early, and a fix can revert the change there. The base branch then
    # holds the change of the branch, and merged lands the task.
    own, url = rest[0]["branch"], rest[0]["pr"]
    if branch_exists(repo, own) and holds(repo, cfg["base"], own):
        named = f" {url}" if url else ""
        print(
            f"{cfg['base']} holds the change of the branch {own} of {a.ref} in"
            f" {repo}. So the pull request{named} of the task there merged, or"
            f" the task no longer changes {repo}. {pin_hint(a.ref, t, cfg)}"
            f" Otherwise run ultraclose.py merged {a.ref}, which lands the task"
        )
        return
    print(
        f"The pull request of {a.ref} in {repo} waits for its merge."
        f" {pin_hint(a.ref, t, cfg)} Otherwise ask the user to merge that pull"
        " request"
    )


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


def repo_name(text):
    """The name of another repository, as a flag names it."""
    if not REPO_NAME.fullmatch(text):
        raise argparse.ArgumentTypeError(
            f"give the name of a directory in the repos directory in place of"
            f" {text!r}. The name opens with a letter or a digit, and it holds"
            " only letters, digits, dots, hyphens, and underscores"
        )
    return text


def repo_base(text):
    """The name and the base branch of a --repo flag.

    The text is NAME=BASE, and the first "=" ends the name.
    """
    name, sep, base = text.partition("=")
    if not sep or not base.strip():
        raise argparse.ArgumentTypeError(
            f"give NAME=BASE, such as x=main, in place of {text!r}"
        )
    return repo_name(name), base.strip()


def repo_check(text):
    """The name and the check of a --repo-check flag.

    The text is NAME=CMD, and the first "=" ends the name. The check is
    empty when the flag removes the check of the repository.
    """
    name, sep, check = text.partition("=")
    if not sep:
        raise argparse.ArgumentTypeError(
            f"give NAME=CMD, such as x='make check', in place of {text!r}"
        )
    return repo_name(name), check


def pr_state(text):
    """The repository and the state of a --pr flag of the leftover command.

    The text is STATE for the repository of the run, or NAME=STATE for
    another repository. The name is empty for the repository of the run.
    """
    name, sep, found = text.rpartition("=")
    if found not in PR_STATES or (sep and not name):
        raise argparse.ArgumentTypeError(
            f"give STATE or NAME=STATE in place of {text!r}. STATE is one of"
            f" {', '.join(PR_STATES)}"
        )
    return (repo_name(name) if sep else ""), found


def label(text):
    """The value of a label flag of the queue filters."""
    if not text.strip():
        raise argparse.ArgumentTypeError("give a label that aven knows")
    return text


def add_filter_flags(s, changes):
    """Add the queue filter flags to the parser of init or of config.

    Each filter flag takes one value. Each flag except --min-priority adds
    a value when it repeats, and a repeated --min-priority keeps its last
    value. With changes, the parser of config also gets the --any flag of
    each dimension, which removes the filters of that dimension.
    """
    for dim, values in FILTERS.items():
        name = dim.upper()
        kind = {"choices": values} if values else {"type": label}
        among = f". {name} is one of {', '.join(values)}" if values else ""
        if dim == "priority":
            among += ", from low to high"
        s.add_argument(
            f"--{dim}",
            action="append",
            default=[],
            metavar=name,
            help=f"the queue holds only the tasks that carry the {dim} of one of"
            f" the --{dim} flags{among}",
            **kind,
        )
        if dim == "priority":
            s.add_argument(
                "--min-priority",
                choices=values,
                metavar=name,
                help="the queue holds only the tasks of PRIORITY or higher",
            )
        s.add_argument(
            f"--skip-{dim}",
            action="append",
            default=[],
            metavar=name,
            help=f"the queue leaves out each task that carries {name}",
            **kind,
        )
        if changes:
            s.add_argument(
                f"--any-{dim}",
                action="store_true",
                help=f"remove the {dim} filters of the run. The other {dim} flags"
                f" of config replace every {dim} filter of the run",
            )


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
REPO_HELP = (
    f"the run also changes the repository NAME in the repos directory that"
    f" {REPOS_DIR} names, and each task of that repository starts from its"
    f" branch BASE. Only a run with the target {PR} or {PR}:BRANCH takes"
    " another repository"
)
REPO_CHECK_HELP = (
    "the full check of the repository NAME, which each landing runs in the"
    " worktree of the task there"
)


def add_repo_flags(s, changes):
    """Add the flags of the other repositories to the parser of init or of config.

    With changes, the parser of config also gets the --no-repo flag, and
    an empty CMD of --repo-check removes the check of a repository.
    """
    s.add_argument(
        "--repo",
        action="append",
        default=[],
        type=repo_base,
        metavar="NAME=BASE",
        help=REPO_HELP,
    )
    s.add_argument(
        "--repo-check",
        action="append",
        default=[],
        type=repo_check,
        metavar="NAME=CMD",
        help=f"{REPO_CHECK_HELP}. An empty CMD removes the check"
        if changes
        else REPO_CHECK_HELP,
    )
    if changes:
        s.add_argument(
            "--no-repo",
            action="append",
            default=[],
            type=repo_name,
            metavar="NAME",
            help="remove the repository NAME, which no unfinished or noted task"
            " changes",
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
    add_filter_flags(s, changes=False)
    add_repo_flags(s, changes=False)
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
    add_filter_flags(s, changes=True)
    add_repo_flags(s, changes=True)
    s.set_defaults(fn=cmd_config)

    s = sub.add_parser("triage")
    s.add_argument("file")
    s.set_defaults(fn=cmd_triage)

    # The untriaged command takes the arguments that the parser does not
    # know, because each one belongs to aven list.
    s = sub.add_parser(
        "untriaged",
        usage="%(prog)s [-h] [FILTER]...",
        description="Print the open aven tasks that the run does not hold and"
        " that the queue filters of the run let in. The run config holds the"
        " queue filters, which the filter flags of init and config set, such as"
        " --status todo --status inbox, --min-priority medium, or --skip-label"
        " self-improve. Each FILTER is an argument of aven list, such as"
        " --project app or --has-metadata review. The command refuses a"
        " --status, --priority, or --label FILTER, because the queue filters"
        " cover each of those dimensions. Without --project, aven list keeps"
        " to the project that aven infers from the main checkout, and it"
        " covers every project when aven infers none. With --limit N, the"
        " command prints at most N rows, and the tasks that the run holds or"
        " that the filters leave out do not count toward N.",
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
    s.add_argument(
        "--note",
        metavar="TEXT",
        help="the note that the command adds to the aven task. The states"
        f" {' and '.join(NOTING)} require it. A noted task takes the reason,"
        " such as its open findings, and the command adds the branch and the"
        " pull request of the task. A closed task takes the evidence that"
        " closes it",
    )
    s.set_defaults(fn=cmd_set)

    s = sub.add_parser(
        "leftover",
        description="Move the branch and the worktree that an earlier run left"
        " for a task out of the way, so that the run starts the task on a new"
        " branch. The command commits the uncommitted changes of the worktree"
        " and removes the worktree. It deletes each branch that holds no"
        " commit that the base branch lacks. It renames every other branch to"
        f" {KEPT}/REF-DATE, and it names that branch in a note on the aven task.",
    )
    s.add_argument("ref")
    s.add_argument(
        "--pr",
        action="append",
        default=[],
        type=pr_state,
        metavar="[NAME=]STATE",
        help="the state of the pull request of the branch that a landing of an"
        " earlier run proposed. STATE is one of"
        f" {', '.join(PR_STATES)}. The command requires one flag for each such"
        " branch, so read each pull request first. NAME names another"
        " repository of the branch, and a flag without NAME stands for the"
        " repository of the run. Pass none for a branch without a pull request,"
        " and open only when the user asks you to keep the pull request open."
        " With merged, the command deletes the branch when the base branch holds"
        " its change. When every pull request of the task merged, the command"
        " sets the aven task to done and sets the task to closed",
    )
    s.set_defaults(fn=cmd_leftover)

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
    s.add_argument(
        "--repo",
        type=repo_name,
        metavar="NAME",
        help="the pull request is in the other repository NAME, as the REPO line"
        " of the landing names it",
    )
    s.set_defaults(fn=cmd_opened)

    s = sub.add_parser(
        "merged",
        description="Record that a pull request of a proposed task merged."
        " The command deletes the branch of the pull request. When every pull"
        " request of the task merged, it sets the aven task to done and sets"
        " the task to landed.",
    )
    s.add_argument("ref")
    s.add_argument(
        "--force",
        action="store_true",
        help="record the merge although the base branch lacks the change of"
        " the task branch, such as for a pull request that gained commits after"
        " the push",
    )
    s.add_argument(
        "--repo",
        type=repo_name,
        metavar="NAME",
        help="the pull request merged in the other repository NAME. The command"
        " takes the pull request of the repository of the run only after the"
        " pull request of each other repository merged. With this flag, it also"
        " takes an approved task, and it keeps that task approved",
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
