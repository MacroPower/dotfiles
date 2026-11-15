#!/usr/bin/env python3
"""Tests for ultraclose.py and close-task.js.

Run with: python3 test_ultraclose.py. The close-task.js tests run the
script under Node, and they skip when node is not on PATH.
"""

import fcntl
import itertools
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("ultraclose.py")
WORKFLOW = Path(__file__).with_name("close-task.js")
SKILL = Path(__file__).parent.with_name("SKILL.md")
NODE = shutil.which("node")
DEFAULT_SHARED = [
    "*_test.*",
    "*/test_*",
    "*.test.*",
    "*.spec.*",
    "*/tests/*",
    "*/testdata/*",
    "*/__tests__/*",
]


# The environment variable in which Claude Code names the session of each
# command that it runs.
SESSION = "CLAUDE_CODE_SESSION_ID"


def clean_env():
    """The environment without the GIT_* variables of a calling git hook.

    The environment also names no Claude Code session. So each command of
    a test runs outside a session, unless the test names one with
    in_session().
    """
    return {
        k: v for k, v in os.environ.items() if not k.startswith("GIT_") and k != SESSION
    }


def in_session(env, session):
    """The environment of a command that the given Claude Code session runs.

    A session of None stands for a command that runs outside a session.
    """
    return {**env, SESSION: session} if session else env


def finished(*args, cwd, env, script=SCRIPT):
    """Run one ultraclose.py command that must pass, and return its process."""
    return subprocess.run(
        [sys.executable, str(script), *args],
        cwd=cwd,
        env=env,
        check=True,
        capture_output=True,
        text=True,
    )


def ultraclose(*args, cwd, env, script=SCRIPT):
    """Run one ultraclose.py command and return its standard output."""
    return finished(*args, cwd=cwd, env=env, script=script).stdout


def refused(*args, cwd, env, script=SCRIPT, timeout=None):
    """Run one ultraclose.py command that must fail, and return its message.

    A timeout in seconds stops a command that keeps running, such as a
    watch command that does not refuse its flags.
    """
    p = subprocess.run(
        [sys.executable, str(script), *args],
        cwd=cwd,
        env=env,
        check=False,
        capture_output=True,
        text=True,
        timeout=timeout,
    )
    if not p.returncode:
        raise AssertionError(f"ultraclose.py {' '.join(args)} passed:\n{p.stdout}")
    return p.stderr


# The script that stands in for aven. The AVEN_ANSWERS directory holds the
# answers that a test recorded. The dep list command prints the
# dependencies of its task, and it reports no dependency for a task without
# an answer. It fails for a ref that starts with NOPE, as aven fails for a
# ref that it does not know. The list command ignores its filters. It
# prints the recorded tasks and saves its arguments, one per line. The
# edit command fails while the AVEN_ANSWERS directory holds the file
# edit.fails. Otherwise it adds its arguments to the file edit.args, as
# one line. Every other command prints one word.
AVEN_STUB = """\
#!/bin/sh
case "$1 $2" in
"dep list")
    case $3 in
    NOPE-*)
        echo "Error: error unknown-ref input=$3" >&2
        exit 1
        ;;
    esac
    answer="$AVEN_ANSWERS/$3.deps.json"
    if [ -e "$answer" ]; then
        cat "$answer"
    else
        echo '{"depends_on": [], "blocks": []}'
    fi
    ;;
"list "*)
    printf '%s\\n' "$@" >"$AVEN_ANSWERS/list.args"
    cat "$AVEN_ANSWERS/list.json"
    ;;
"edit "*)
    if [ -e "$AVEN_ANSWERS/edit.fails" ]; then
        echo "Error: the database is locked" >&2
        exit 1
    fi
    shift
    echo "$*" >>"$AVEN_ANSWERS/edit.args"
    echo stub
    ;;
*) echo stub ;;
esac
"""


def stub_aven(tmp, env):
    """Put the stub for aven first on the PATH of an environment.

    The triage command and each task start call aven, and a test must not
    read the tasks of the user. The AVEN_ANSWERS variable of the
    environment names the directory of the recorded answers.
    """
    bin_dir, answers = Path(tmp, "bin"), Path(tmp, "aven")
    bin_dir.mkdir()
    answers.mkdir()
    aven = bin_dir / "aven"
    aven.write_text(AVEN_STUB)
    aven.chmod(0o755)
    env["PATH"] = f"{bin_dir}{os.pathsep}{env['PATH']}"
    env["AVEN_ANSWERS"] = str(answers)


def aven_dependencies(env, ref, unresolved=(), resolved=(), blocks=()):
    """Record the answer of the aven stub for the dependencies of one task.

    The task depends on each unresolved ref and on each resolved ref, and
    it blocks each blocks ref. The answer has the fields that
    `aven dep list --json` prints.
    """

    def entry(other, is_open):
        return {
            "ref": other,
            "title": "t",
            "status": "todo" if is_open else "done",
            "priority": "none",
            "deleted": False,
            "unresolved": is_open,
        }

    answer = {
        "depends_on_open": len(unresolved),
        "depends_on_total": len(unresolved) + len(resolved),
        "blocks_open": len(blocks),
        "blocks_total": len(blocks),
        "depends_on": [entry(r, True) for r in unresolved]
        + [entry(r, False) for r in resolved],
        "blocks": [entry(r, True) for r in blocks],
    }
    Path(env["AVEN_ANSWERS"], f"{ref}.deps.json").write_text(json.dumps(answer))


def aven_edits(env):
    """The arguments of each edit command that the aven stub ran.

    Each text of the result holds the arguments of one command.
    """
    record = Path(env["AVEN_ANSWERS"], "edit.args")
    return record.read_text().splitlines() if record.exists() else []


def aven_listed_tasks(env, *tasks):
    """Record the tasks that the aven stub lists.

    Each task holds at least a ref. The other fields default to those of
    an open task that is not an epic, under the names that
    `aven list --json` prints.
    """
    plain = {
        "title": "t",
        "status": "todo",
        "priority": "none",
        "deleted": False,
        "is_epic": False,
    }
    rows = [{**plain, **task} for task in tasks]
    Path(env["AVEN_ANSWERS"], "list.json").write_text(json.dumps(rows))


def new_repo(tmp):
    """Create an empty repository, and return it with a clean environment.

    A stub stands in for aven.
    """
    repo, env = Path(tmp, "repo"), clean_env()
    stub_aven(tmp, env)
    subprocess.run(["git", "init", "-q", "-b", "main", str(repo)], env=env, check=True)
    return repo, env


# The git options that let a test commit without the identity and the
# signing key of the user.
IDENTITY = [
    "-c",
    "user.name=test",
    "-c",
    "user.email=test@example.com",
    "-c",
    "commit.gpgsign=false",
]


def empty_commit(cwd, env, message):
    """Record one commit that changes no file."""
    subprocess.run(
        [
            "git",
            *IDENTITY,
            "commit",
            "-q",
            "--no-verify",
            "--allow-empty",
            "-m",
            message,
        ],
        cwd=cwd,
        env=env,
        check=True,
    )


def git(*args, cwd, env):
    """Run one git command that must pass, and return its standard output."""
    p = subprocess.run(
        ["git", *args], cwd=cwd, env=env, check=True, capture_output=True, text=True
    )
    return p.stdout.strip()


def launchable_repo(tmp):
    """Create a repository whose tasks can start, and return it with its env.

    The repository has one commit, so a task branch can start from it.
    """
    repo, env = new_repo(tmp)
    empty_commit(repo, env, "init")
    return repo, env


def running_task(tmp, commits=1, flags=()):
    """Start a run with one running task.

    The flags go to init. The task branch holds the given number of
    commits. The args command resumes a task at a review round only when
    its branch holds a commit. Return the repository, the environment,
    and the Workflow input that next printed for the task.
    """
    repo, env = launchable_repo(tmp)
    ultraclose("init", "--run", str(Path(tmp, "run")), *flags, cwd=repo, env=env)
    triage(tmp, repo, env, {"ref": REF})
    launch = json.loads(ultraclose("next", cwd=repo, env=env))[0]
    for n in range(commits):
        empty_commit(launch["task"]["wt"], env, f"wip {n}")
    return repo, env, launch


def triage(tmp, repo, env, *rows):
    """Add one task per row to the run, and return the standard error.

    Each row holds at least a ref.
    """
    file = Path(tmp, "triage.json")
    file.write_text(json.dumps([{"title": "t", "files": [], **row} for row in rows]))
    return finished("triage", str(file), cwd=repo, env=env).stderr


def state_of(run):
    """The config and the tasks that a run directory holds."""
    return json.loads((run / "state.json").read_text())


def configured_run(repo, env, run, script=SCRIPT):
    """Start a run that sets each config value that a batch inherits.

    The run sets a limit, a model, a check, a health command, a step
    timeout, and a shared glob. Its directory holds a rules.md and a
    notes.md.
    """
    ultraclose(
        "init",
        "--run",
        str(run),
        "--limit",
        "8",
        "--model",
        f"reviewer={OPUS}",
        "--check",
        "sh check.sh",
        "--health",
        "docker version",
        "--step-timeout",
        "90",
        "--shared",
        GLOB,
        cwd=repo,
        env=env,
        script=script,
    )
    for name in RUN_FILES:
        (run / name).write_text(f"the {name} of the run at {run}\n")


def assert_fresh(test, run):
    """Assert that a run holds the default config and none of the RUN_FILES."""
    config = state_of(run)["config"]
    keys = ("limit", "models", "check", "health", "stepTimeout", "shared")
    test.assertEqual(
        {k: config[k] for k in keys},
        {
            "limit": 5,
            "models": {},
            "check": "",
            "health": "",
            "stepTimeout": STEP_TIMEOUT,
            "shared": DEFAULT_SHARED,
        },
    )
    test.assertEqual([n for n in RUN_FILES if (run / n).exists()], [])


def earlier_skill(tmp, changed=SKILL.name):
    """Lay out a copy of the skill, and return the path of its ultraclose.py.

    The copy differs from the skill of SCRIPT in the file that changed
    names, which gets one more line. So the copy is another version of
    the skill. A changed argument of None gives a copy with the same
    files, which is the same version.
    """
    skill = Path(tempfile.mkdtemp(dir=tmp, prefix="skill-"))
    scripts = skill / "scripts"
    scripts.mkdir()
    files = {
        SKILL.name: (SKILL, skill / SKILL.name, "\nAn earlier rule.\n"),
        WORKFLOW.name: (WORKFLOW, scripts / WORKFLOW.name, "\n// An earlier line.\n"),
        SCRIPT.name: (SCRIPT, scripts / SCRIPT.name, "\n# An earlier line.\n"),
    }
    for name, (source, copy, line) in files.items():
        copy.write_text(source.read_text() + (line if name == changed else ""))
    return scripts / SCRIPT.name


def unrecorded(run):
    """Remove the skill version and the session from the state of a run.

    The state file then holds what an earlier version of ultraclose.py
    wrote, before runs recorded their version and their session.
    """
    st = state_of(run)
    del st["skill"], st["session"]
    (run / "state.json").write_text(json.dumps(st))


# A value that stands for the session of a run that records no session
# and no skill version, as unrecorded() leaves it.
UNRECORDED = "unrecorded"


def first_run(tmp, task_state=None, session=None, script=SCRIPT):
    """Start a configured run, and return the repo, the env, and the run.

    The run holds one task in the given state, or no task. The given
    script creates the run, and the given session runs each command. A
    session of UNRECORDED gives a run that records no session and no
    skill version, as unrecorded() leaves it. The environment that the
    function returns names no session.
    """
    repo, env = new_repo(tmp)
    earlier = in_session(env, None if session == UNRECORDED else session)
    run = Path(tmp, "first").resolve()
    configured_run(repo, earlier, run, script=script)
    if task_state:
        triage(tmp, repo, earlier, {"ref": REF})
        ultraclose("set", REF, task_state, cwd=repo, env=earlier)
    if session == UNRECORDED:
        unrecorded(run)
    return repo, env, run


def status_values(repo, env, key):
    """What status prints after the given key and a colon, one text per line."""
    lines = ultraclose("status", cwd=repo, env=env).splitlines()
    return [
        line.removeprefix(f"{key}: ") for line in lines if line.startswith(f"{key}: ")
    ]


def version_of(tmp, script=SCRIPT):
    """The version of the skill of a script, as a run of that script records it."""
    with tempfile.TemporaryDirectory(dir=tmp) as other:
        repo, env = new_repo(other)
        run = Path(other, "run")
        ultraclose("init", "--run", str(run), cwd=repo, env=env, script=script)
        return state_of(run)["skill"]


REF = "APP-7KQ9"
# The files that the orchestrator writes into a run directory. A new run
# must start without the files of an earlier run.
RUN_FILES = ("rules.md", "notes.md")
# The roles of the agents of close-task.js. A --model flag with the
# default role names the model of each role without a model of its own.
ROLES = ("implementer", "sync", "reviewer", "fixer", "narrow")
DEFAULT_ROLE = "default"
# Two models that a --model flag can name.
FABLE = "claude-fable-5-2"
OPUS = "claude-opus-5-5"
# A model that a config command names in place of the models of init.
NEW = "claude-new-5-0"
# The shared glob of a configured run, and a file that the glob covers.
GLOB = "*/tools/mcp/*"
COVERED = "tools/mcp/render.go"
# The sentence that names the glob of a file at the root of the repository.
# The help of each --shared flag prints it, and so does each warning about
# a shared glob that matches no file.
ROOT_GLOB = "/README.md matches the README.md at the root of the repository"
# The line that status prints for a run that has ended, and the line that
# watch prints for it.
ENDED = "ended: no task is pending, inflight, approved, or proposed"
WATCH_ENDED = "ENDED no task is pending, inflight, approved, or proposed"
# What status prints after "disk " for a watched file system, and watch
# after "DISK " for a low one. The text holds the first disk path of the
# file system, the free space, the size, the free share of the size, and
# the floor. The words before the floor say whether the file system is low.
# When the percent floor of a disk path sets the floor, the text ends with
# that path and its percent.
DISK_TEXT = re.compile(
    r"(?P<path>.+): (?P<free>\d+) GB free of (?P<size>\d+) GB \((?P<share>\d+)%\),"
    r" (?P<low>less than the floor of|floor) (?P<floor>\d+) GB"
    r"(?: set by --disk-floor-percent (?P<by>.+)=(?P<percent>[\d.]+))?"
)
LOW = "less than the floor of"
# The states of a task that keep a run from its end, and the other states.
UNFINISHED = ("pending", "inflight", "approved", "proposed")
FINISHED = ("landed", "noted", "closed")
# The files of a task directory that hold the summary and the notes of the
# change, by the key of the resume field that carries each one. The agents
# of close-task.js write the files, and the args command reads them.
STORED_FILES = {"summary": "summary.txt", "notes": "notes.txt"}
# The directory that holds a golangci-lint cache of one checkout.
LINT_CACHE = "golangci-lint-cache"
# The step timeout that a run starts with, in seconds.
STEP_TIMEOUT = 3600
# How long a landing waits for the processes of a step to end after each
# signal that it sends them, in seconds.
GRACE = 5


def home_repo(tmp):
    """Create an empty repository beside a home directory.

    The environment names the home directory. So the default disk paths
    of a run, the repository and the home directory, share the file
    system of the temporary directory. Return the repository, the
    environment, and the home directory.
    """
    repo, env = new_repo(tmp)
    home = Path(tmp, "home")
    home.mkdir()
    env["HOME"] = str(home)
    return repo, env, home


def other_file_system(tmp):
    """An existing path on another file system than tmp, or None.

    The commands can measure the free space of the path. Some of the space
    of its file system is in use, so a floor of 100 percent is more than
    the free space. The file systems of a host differ, so the function
    tries each path in turn.
    """
    device = os.stat(tmp).st_dev
    for path in ("/dev", "/dev/shm", "/run", "/"):
        try:
            if os.stat(path).st_dev != device:
                usage = shutil.disk_usage(path)
                if usage.free < usage.total:
                    return path
        except OSError:
            continue
    return None


def disk_texts(repo, env):
    """What status prints for each watched file system, without the prefix."""
    lines = ultraclose("status", cwd=repo, env=env).splitlines()
    return [line.removeprefix("disk ") for line in lines if line.startswith("disk ")]


def watching(repo, env):
    """Start a watch command that measures 20 times a second."""
    return subprocess.Popen(
        [sys.executable, str(SCRIPT), "watch", "--interval", "0.05"],
        cwd=repo,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )


def watched(repo, env):
    """Run a watch command that must exit, and return the lines it prints."""
    with watching(repo, env) as p:
        try:
            out, err = p.communicate(timeout=60)
        finally:
            p.kill()
    if p.returncode:
        raise AssertionError(f"ultraclose.py watch failed:\n{err}")
    return out.splitlines()


def store_step(task_dir):
    """The text of the prompt step that stores the summary and the notes in
    the files of the given task directory."""
    summary, notes = (Path(task_dir, STORED_FILES[key]) for key in STORED_FILES)
    return f"Store the summary in {summary} and the notes in {notes}."


class InitTest(unittest.TestCase):
    def test_shared(self):
        cases = {
            "no flag keeps the defaults": ([], DEFAULT_SHARED),
            "a flag adds to the defaults": (
                ["*/home/claude.nix"],
                DEFAULT_SHARED + ["*/home/claude.nix"],
            ),
            "flags keep their order": (
                ["*/main.go", "*/home/claude.nix"],
                DEFAULT_SHARED + ["*/main.go", "*/home/claude.nix"],
            ),
            "a default glob appears once": (
                ["*_test.*", "*/main.go"],
                DEFAULT_SHARED + ["*/main.go"],
            ),
        }
        for name, (flags, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                run = Path(tmp, "run")
                args = ["init", "--run", str(run)]
                for glob in flags:
                    args += ["--shared", glob]
                ultraclose(*args, cwd=repo, env=env)
                self.assertEqual(state_of(run)["config"]["shared"], want)

    def test_replaces_run_that_session_does_not_continue(self):
        # name: (the state of the task of the first run, the session of
        # the first run, the session of the second init, the flags of the
        # second init)
        cases = {f"one {s} task": (s, None, None, []) for s in FINISHED}
        cases |= {
            f"a new session and one {s} task": (s, "first", "second", [])
            for s in FINISHED
        }
        # A session continues a run with no task only when the run records
        # the session and the version of the command.
        cases |= {
            "a new session and a run with no task": (None, "first", "second", []),
            "a run with no task that records no session": (None, None, "second", []),
            "a run with no task that records no version and no session": (
                None,
                UNRECORDED,
                "second",
                [],
            ),
            "a command outside a session and a run with no task": (
                None,
                "first",
                None,
                [],
            ),
            "--force and a run with no task": (None, None, None, ["--force"]),
        }
        cases |= {
            f"--force and one {s} task": (s, None, None, ["--force"])
            for s in UNFINISHED
        }
        for name, (task_state, earlier, later, flags) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, first = first_run(tmp, task_state, earlier)
                before = state_of(first)
                second = Path(tmp, "second").resolve()
                env = in_session(env, later)
                if not flags:
                    [setup] = status_values(repo, env, "setup")
                    self.assertTrue(setup.startswith(NEW_RUN), setup)
                out = ultraclose(
                    "init", "--run", str(second), *flags, cwd=repo, env=env
                )
                self.assertIn(f"replaced the run at {first}\n", out)
                # The new run takes no task, no config, and no file from
                # the first run.
                self.assertEqual(state_of(second)["tasks"], {})
                assert_fresh(self, second)
                status = ultraclose("status", cwd=repo, env=env)
                self.assertEqual(status.splitlines()[0], f"run={second}")
                self.assertEqual(state_of(first), before)
                kept = [name for name in RUN_FILES if (first / name).is_file()]
                self.assertEqual(kept, list(RUN_FILES))

    def test_keeps_run_that_session_continues(self):
        unfinished = "has not ended, and this session continues it"
        unstarted = "holds no task yet, and this session continues it"
        # name: (the state of the task of the first run, the session of
        # the first run, the session of the second init, what the refusal
        # says about the first run)
        cases = {f"one {s} task": (s, None, None, unfinished) for s in UNFINISHED}
        cases |= {
            f"a new session and one {s} task": (s, "first", "second", unfinished)
            for s in UNFINISHED
        }
        cases |= {
            "a run with no task": (None, None, None, unstarted),
            "the session of a run with no task": (None, "first", "first", unstarted),
        }
        for name, (task_state, earlier, later, text) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, first = first_run(tmp, task_state, earlier)
                before = state_of(first)
                second = Path(tmp, "second")
                env = in_session(env, later)
                message = refused("init", "--run", str(second), cwd=repo, env=env)
                self.assertIn(f"the run at {first} {text}", message)
                self.assertIn("--force", message)
                self.assertFalse(second.exists())
                status = ultraclose("status", cwd=repo, env=env)
                self.assertEqual(status.splitlines()[0], f"run={first}")
                self.assertEqual(state_of(first), before)

    def test_takes_next_free_directory(self):
        # Each init names the directory of the first run. Each step holds
        # the state in which the task of the run before the init ends. It
        # also holds the flags of the init, and the name of the directory
        # that the init takes.
        steps = [
            ("landed", [], "first-2"),
            ("noted", [], "first-3"),
            ("pending", ["--force"], "first-4"),
        ]
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, first = first_run(tmp)
            runs = [first]
            for task_state, flags, name in steps:
                triage(tmp, repo, env, {"ref": REF})
                ultraclose("set", REF, task_state, cwd=repo, env=env)
                before = [state_of(run) for run in runs]
                out = ultraclose("init", "--run", str(first), *flags, cwd=repo, env=env)
                run = first.with_name(name)
                self.assertIn(f"run={run} ", out)
                self.assertEqual(state_of(run)["tasks"], {})
                assert_fresh(self, run)
                status = ultraclose("status", cwd=repo, env=env)
                self.assertEqual(status.splitlines()[0], f"run={run}")
                self.assertEqual([state_of(run) for run in runs], before)
                runs.append(run)

    def test_force_takes_no_directory_beside_named_directory(self):
        # The directory beside the directory of the first run holds the
        # tasks directory and the files of an earlier run, and no state
        # file. --force frees only the directory that --run names.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, first = first_run(tmp, "pending")
            beside = first.with_name("first-2")
            (beside / "tasks" / "APP-0009").mkdir(parents=True)
            for name in RUN_FILES:
                (beside / name).write_text(f"the {name} of the run at {beside}\n")
            out = ultraclose("init", "--run", str(first), "--force", cwd=repo, env=env)
            run = first.with_name("first-3")
            self.assertIn(f"run={run} ", out)
            self.assertEqual(state_of(run)["tasks"], {})
            assert_fresh(self, run)
            self.assertEqual(os.listdir(run / "tasks"), [])
            self.assertFalse((beside / "state.json").exists())
            self.assertEqual(os.listdir(beside / "tasks"), ["APP-0009"])

    def test_retry_after_failed_init(self):
        # Init fails after its checks when it cannot add the worktrees to
        # the exclude file of the repository. Without a global config and
        # a global ignore file, git ignores no worktree, so init writes
        # the exclude file.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = new_repo(tmp)
            env |= {
                "GIT_CONFIG_GLOBAL": os.devnull,
                "GIT_CONFIG_NOSYSTEM": "1",
                "XDG_CONFIG_HOME": tmp,
            }
            info = Path(repo, ".git", "info")
            shutil.rmtree(info, ignore_errors=True)
            run = Path(tmp, "run").resolve()
            refused("init", "--run", str(run), cwd=repo, env=env)
            self.assertFalse(run.exists())
            info.mkdir()
            ultraclose("init", "--run", str(run), cwd=repo, env=env)
            self.assertEqual(state_of(run)["tasks"], {})
            self.assertIn(".claude/worktrees/\n", (info / "exclude").read_text())
            status = ultraclose("status", cwd=repo, env=env)
            self.assertEqual(status.splitlines()[0], f"run={run}")

    def test_retry_after_failed_config_write(self):
        # Git writes its config through a lock file, and it refuses the
        # write while that file exists. The second session starts a new
        # run in place of the run without a task of the first session.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = new_repo(tmp)
            first = Path(tmp, "first").resolve()
            ultraclose(
                "init", "--run", str(first), cwd=repo, env=in_session(env, "first")
            )
            env = in_session(env, "second")
            lock = repo / ".git" / "config.lock"
            lock.touch()
            run = Path(tmp, "run").resolve()
            message = refused("init", "--run", str(run), cwd=repo, env=env)
            self.assertIn(f"git config --local ultraclose.run {run} failed", message)
            self.assertFalse(run.exists())
            status = ultraclose("status", cwd=repo, env=env)
            self.assertEqual(status.splitlines()[0], f"run={first}")
            lock.unlink()
            ultraclose("init", "--run", str(run), cwd=repo, env=env)
            self.assertEqual(state_of(run)["session"], "second")
            status = ultraclose("status", cwd=repo, env=env)
            self.assertEqual(status.splitlines()[0], f"run={run}")


class OriginTest(unittest.TestCase):
    """A run records the version of the skill that created it, and its session."""

    def test_version_follows_each_file_of_skill(self):
        # name: the file of the skill copy that differs from the skill of
        # SCRIPT, or None for a copy with the same files
        copies = {"the same files": None, "the same files in another place": None}
        copies |= {
            f"another {file.name}": file.name for file in (SKILL, WORKFLOW, SCRIPT)
        }
        with tempfile.TemporaryDirectory() as tmp:
            versions = {
                name: version_of(tmp, earlier_skill(tmp, changed))
                for name, changed in copies.items()
            }
            for name, version in versions.items():
                with self.subTest(name):
                    self.assertRegex(version, r"^[0-9a-f]{12}$")
            same = versions.pop("the same files")
            self.assertEqual(same, version_of(tmp))
            self.assertEqual(same, versions.pop("the same files in another place"))
            self.assertEqual(len({same, *versions.values()}), len(versions) + 1)

    def test_records_session_of_each_command_that_changes_run(self):
        # Each line holds a command, the session that runs it, and the
        # session that the run records after it.
        steps = [
            (["status"], "second", "first"),
            (["untriaged"], "second", "first"),
            (["config", "--limit", "7"], "second", "second"),
            (["config", "--limit", "6"], None, "second"),
            (["set", REF, "pending"], "third", "third"),
        ]
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, run = first_run(tmp, session="first")
            self.assertEqual(state_of(run)["session"], "first")
            # Another session continues only a run that has a task.
            triage(tmp, repo, in_session(env, "first"), {"ref": REF})
            aven_listed_tasks(env)
            for command, session, recorded in steps:
                with self.subTest(command=command, session=session):
                    ultraclose(*command, cwd=repo, env=in_session(env, session))
                    self.assertEqual(state_of(run)["session"], recorded)

    def test_status_flags_another_version_and_another_session(self):
        # Each case names the file of the skill that differs when the run
        # starts, or None. It names the session of the run and the session
        # of the status command. It then says whether status flags the
        # version, and what status prints about the session.
        cases = {
            "the same version and the same session": (
                None,
                "first",
                "first",
                False,
                "first",
            ),
            "another version": (SKILL.name, "first", "first", True, "first"),
            "another session": (
                None,
                "first",
                "second",
                False,
                "first, and this command runs in another session, second",
            ),
            "a command outside a session": (
                None,
                "first",
                None,
                False,
                "first, and this command runs outside a session",
            ),
            "a run that records no session": (
                None,
                None,
                "second",
                False,
                "not recorded",
            ),
        }
        for name, (changed, earlier, later, flagged, session) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                script = earlier_skill(tmp, changed)
                repo, env, run = first_run(tmp, "pending", earlier, script)
                env = in_session(env, later)
                made, this = state_of(run)["skill"], version_of(tmp)
                self.assertEqual(made != this, flagged)
                skill = (
                    f"{made}, and this command comes from another version, {this}"
                    if flagged
                    else this
                )
                self.assertEqual(status_values(repo, env, "skill"), [skill])
                self.assertEqual(status_values(repo, env, "session"), [session])

    def test_state_without_skill_and_session_keys(self):
        # A run can continue from a state file that init wrote without
        # the skill key and the session key.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, run = first_run(tmp, "pending", "first")
            unrecorded(run)
            env = in_session(env, "second")
            this = version_of(tmp)
            self.assertEqual(
                status_values(repo, env, "skill"),
                [f"not recorded, and this command comes from version {this}"],
            )
            self.assertEqual(status_values(repo, env, "session"), ["not recorded"])
            self.assertEqual(status_values(repo, env, "setup"), [CONTINUE])
            # The first command that changes the run records its session.
            ultraclose("config", "--limit", "7", cwd=repo, env=env)
            self.assertEqual(state_of(run)["session"], "second")
            self.assertNotIn("skill", state_of(run))


# What status prints after "setup: " for a run that the session continues,
# and the start of what it prints for every other run.
CONTINUE = "continue this run"
NEW_RUN = "start a new run with init, because "
# The reasons for which a session starts a new run.
HAS_ENDED = "this run has ended"
UNSTARTED = "this run holds no task, and another session created it"
UNSTARTED_VERSION = "this run holds no task, and another version of the skill created it"
UNSTARTED_OUTSIDE = "this run holds no task, and this command runs outside a session"
UNSTARTED_UNRECORDED = "this run holds no task, and it records no session"
STALE = "this run comes from another session and another version of the skill"


class ContinueTest(unittest.TestCase):
    """The script decides whether a session continues the run of the repository."""

    def test_decision(self):
        # Each case names the file of the skill that differs when the run
        # starts, or None. It names the state of the task of the run, or
        # None for a run with no task. It names the session of the run,
        # or UNRECORDED, and the session of the later commands. It then
        # gives the reason for which that session starts a new run, or
        # None for a session that continues the run. Last, it says
        # whether a command of that session changes the run.
        cases = {
            "its own run with an unfinished task": (
                None,
                "pending",
                "first",
                "first",
                None,
                True,
            ),
            "its own run with no task": (None, None, "first", "first", None, True),
            "its own run that has ended": (
                None,
                "landed",
                "first",
                "first",
                HAS_ENDED,
                True,
            ),
            "its own run from another version": (
                SKILL.name,
                "inflight",
                "first",
                "first",
                None,
                True,
            ),
            "the run of another session": (
                None,
                "approved",
                "first",
                "second",
                None,
                True,
            ),
            "the run of another session that has ended": (
                None,
                "noted",
                "first",
                "second",
                HAS_ENDED,
                False,
            ),
            "the run of another session with no task": (
                None,
                None,
                "first",
                "second",
                UNSTARTED,
                False,
            ),
            "its own run with no task from another version": (
                SKILL.name,
                None,
                "first",
                "first",
                UNSTARTED_VERSION,
                True,
            ),
            "a run with no task that records no version and no session": (
                None,
                None,
                UNRECORDED,
                "second",
                UNSTARTED_VERSION,
                True,
            ),
            "a run with no task that records no session": (
                None,
                None,
                None,
                "second",
                UNSTARTED_UNRECORDED,
                True,
            ),
            "a command outside a session and a run with no task": (
                None,
                None,
                "first",
                None,
                UNSTARTED_OUTSIDE,
                True,
            ),
            "a run that records no session": (
                SKILL.name,
                "pending",
                None,
                "second",
                None,
                True,
            ),
            "a command outside a session": (
                SKILL.name,
                "pending",
                "first",
                None,
                None,
                True,
            ),
            "an ended run of another session and another version": (
                SKILL.name,
                "closed",
                "first",
                "second",
                HAS_ENDED,
                False,
            ),
        }
        # Each file of the skill makes another version.
        cases |= {
            f"the run of another session and another {file.name}": (
                file.name,
                "pending",
                "first",
                "second",
                STALE,
                False,
            )
            for file in (SKILL, WORKFLOW, SCRIPT)
        }
        for name, case in cases.items():
            changed, task_state, earlier, later, why, changes = case
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                script = earlier_skill(tmp, changed)
                repo, env, first = first_run(tmp, task_state, earlier, script)
                env = in_session(env, later)
                before = state_of(first)
                unfinished = task_state in UNFINISHED
                # The status command prints the decision.
                line = NEW_RUN + why if why else CONTINUE
                if why and unfinished:
                    line += ". init moves its 1 unfinished task to the new run"
                if not why and not task_state:
                    line += ", which holds no task yet"
                self.assertEqual(status_values(repo, env, "setup"), [line])
                # Each command that changes the run follows the decision.
                command = ["config", "--limit", "3"]
                if changes:
                    ultraclose(*command, cwd=repo, env=env)
                    self.assertEqual(state_of(first)["config"]["limit"], 3)
                    # The command records its session, which can change
                    # the decision. So init reads the state that status
                    # read.
                    (first / "state.json").write_text(json.dumps(before))
                else:
                    message = refused(*command, cwd=repo, env=env)
                    self.assertIn(
                        f"this session does not continue the run at {first},"
                        f" because {why}. Start a new run with init",
                        message,
                    )
                    self.assertEqual(state_of(first), before)
                # The init command follows the decision.
                second = Path(tmp, "second").resolve()
                init = ["init", "--run", str(second)]
                if why:
                    ultraclose(*init, cwd=repo, env=env)
                    tasks = [REF] if unfinished else []
                    self.assertEqual(list(state_of(second)["tasks"]), tasks)
                    assert_fresh(self, second)
                else:
                    message = refused(*init, cwd=repo, env=env)
                    self.assertIn("this session continues it", message)
                    self.assertFalse(second.exists())


class TakeOverTest(unittest.TestCase):
    """A new run takes the unfinished tasks of the run that it replaces."""

    # The tasks of the earlier run, each named after its state. The run
    # started the inflight task and the approved task, so each of the two
    # has a directory, a branch, and a worktree.
    INFLIGHT, APPROVED, PENDING = "APP-0001", "APP-0002", "APP-0003"
    NOTED, LANDED = "APP-0004", "APP-0005"
    UNFINISHED = (INFLIGHT, APPROVED, PENDING)
    # The tasks that a new run takes from the earlier run.
    MOVED = UNFINISHED + (NOTED,)
    # The review history of the inflight task.
    HISTORY = "## Round 1\n"

    def earlier_run(self, tmp):
        """Start a run of an earlier session and an earlier version of the skill.

        The run holds one task per state of this class. Return the
        repository, an environment that names no session, the run
        directory, and the Workflow input of the inflight task.
        """
        repo, env = launchable_repo(tmp)
        earlier = in_session(env, "first")
        first = Path(tmp, "first").resolve()
        configured_run(repo, earlier, first, script=earlier_skill(tmp))
        started = (self.INFLIGHT, self.APPROVED)
        triage(tmp, repo, earlier, *({"ref": ref} for ref in started))
        launches = json.loads(ultraclose("next", cwd=repo, env=earlier))
        launch = {t["task"]["ref"]: t for t in launches}[self.INFLIGHT]
        empty_commit(launch["task"]["wt"], env, "wip")
        Path(launch["task"]["dir"], "history.md").write_text(self.HISTORY)
        rest = {
            self.APPROVED: "approved",
            self.PENDING: "pending",
            self.NOTED: "noted",
            self.LANDED: "landed",
        }
        rows = [
            {"ref": ref, "files": [COVERED], "after": [self.INFLIGHT]}
            for ref in (self.PENDING, self.NOTED, self.LANDED)
        ]
        triage(tmp, repo, earlier, *rows)
        for ref, task_state in rest.items():
            ultraclose("set", ref, task_state, cwd=repo, env=earlier)
        return repo, env, first, launch

    def assert_unchanged(self, repo, env, first, before):
        """The earlier run keeps its state and its task directories."""
        self.assertEqual(state_of(first), before)
        for ref in (self.INFLIGHT, self.APPROVED):
            self.assertTrue((first / "tasks" / ref / "task.md").is_file(), ref)
        status = ultraclose("status", cwd=repo, env=env)
        self.assertEqual(status.splitlines()[0], f"run={first}")

    def test_new_run_takes_unfinished_tasks(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, first, launch = self.earlier_run(tmp)
            before = state_of(first)
            env = in_session(env, "second")
            moves = (
                ". init moves its 3 unfinished tasks and its 1 noted task to the"
                " new run"
            )
            self.assertEqual(
                status_values(repo, env, "setup"), [NEW_RUN + STALE + moves]
            )
            self.assertIn(
                f"this session does not continue the run at {first}",
                refused("next", cwd=repo, env=env),
            )
            second = Path(tmp, "second").resolve()
            out = ultraclose("init", "--run", str(second), cwd=repo, env=env)
            states = (
                f"{self.INFLIGHT} (inflight) {self.APPROVED} (approved)"
                f" {self.PENDING} (pending)"
            )
            self.assertEqual(
                out.splitlines()[:5],
                [
                    f"replaced the run at {first}",
                    "moved 3 unfinished tasks of that run to the new run:",
                    states,
                    "moved 1 noted task of that run to the new run:",
                    self.NOTED,
                ],
            )
            # The new run holds each unfinished task and the noted task
            # with its row and its state, and it holds no config and no
            # file of the first run.
            later = state_of(second)
            self.assertEqual(
                later["tasks"], {ref: before["tasks"][ref] for ref in self.MOVED}
            )
            self.assertEqual(later["session"], "second")
            self.assertEqual(later["skill"], version_of(tmp))
            self.assertEqual(later["replaces"], [str(first)])
            assert_fresh(self, second)
            # The first run keeps the task that it landed, its config, and
            # its files.
            after = state_of(first)
            self.assertEqual(
                after["tasks"], {self.LANDED: before["tasks"][self.LANDED]}
            )
            self.assertEqual(after["config"], before["config"])
            kept = [name for name in RUN_FILES if (first / name).is_file()]
            self.assertEqual(kept, list(RUN_FILES))
            # The directory of each started task moves to the new run.
            for ref in (self.INFLIGHT, self.APPROVED):
                self.assertFalse((first / "tasks" / ref).exists(), ref)
                self.assertTrue((second / "tasks" / ref / "task.md").is_file(), ref)
            # So the new run resumes the work of the task as its own.
            relaunch = json.loads(ultraclose("args", self.INFLIGHT, cwd=repo, env=env))
            task = relaunch["task"]
            self.assertEqual(task["wt"], launch["task"]["wt"])
            self.assertEqual(task["dir"], str(second / "tasks" / self.INFLIGHT))
            self.assertEqual(task["resume"]["round"], 2)
            self.assertEqual(Path(task["dir"], "history.md").read_text(), self.HISTORY)
            # The first run names a model for the reviewer, and the new run
            # names none. So the relaunch follows the session model.
            self.assertEqual(before["config"]["models"], {"reviewer": OPUS})
            self.assertEqual(relaunch["models"], {})
            self.assertEqual(status_values(repo, env, "setup"), [CONTINUE])
            # The batch goes on from the first run, so status names it for
            # the report.
            self.assertEqual(
                status_values(repo, env, "earlier run"),
                [f"{first}, which keeps 1 landed task"],
            )

    def test_moves_no_task_to_run_with_another_target(self):
        # Each unfinished task of the earlier run starts from main, and
        # each landing of that run commits on main. A landing on another
        # branch carries the commits of main to it. A landing that
        # proposes a pull request changes how the task ends.
        # target: the texts of the refusal
        cases = {
            "release/v1": [
                "each of them starts from its base branch main",
                "The base branch of the target release/v1 is release/v1",
            ],
            "pr:release/v1": [
                "each of them starts from its base branch main",
                "The base branch of the target pr:release/v1 is release/v1",
            ],
            "pr": [
                "each landing of that run commits its task on main",
                "Each landing of the target pr proposes its task as a pull"
                " request instead",
            ],
        }
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, first, _ = self.earlier_run(tmp)
            before = state_of(first)
            env = in_session(env, "second")
            git("switch", "-q", "-c", "release/v1", cwd=repo, env=env)
            second = Path(tmp, "second").resolve()
            init = ("init", "--run", str(second))
            for target, texts in cases.items():
                with self.subTest(target):
                    message = refused(*init, "--target", target, cwd=repo, env=env)
                    for text in (
                        f"the run at {first} has unfinished tasks of the target main",
                        *texts,
                        "Name the target main to move those tasks to the new run",
                        "--force",
                    ):
                        self.assertIn(text, message)
                    self.assertFalse(second.exists())
                    self.assert_unchanged(repo, env, first, before)
            # The target of the earlier run moves the tasks.
            git("switch", "-q", "main", cwd=repo, env=env)
            out = ultraclose(*init, "--target", "main", cwd=repo, env=env)
            self.assertIn("moved 3 unfinished tasks of that run to the new run:", out)
            later = state_of(second)
            self.assertEqual(sorted(later["tasks"]), sorted(self.MOVED))
            self.assertEqual(
                (later["config"]["target"], later["config"]["base"]), ("main", "main")
            )

    def test_takes_tasks_of_run_without_target_key(self):
        # An earlier version of the skill wrote the state file without the
        # target key. Each landing of such a run commits on its base
        # branch, so its target is that branch.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, first, _ = self.earlier_run(tmp)
            st = state_of(first)
            del st["config"]["target"]
            (first / "state.json").write_text(json.dumps(st))
            before = state_of(first)
            env = in_session(env, "second")
            second = Path(tmp, "second").resolve()
            init = ("init", "--run", str(second))
            message = refused(*init, "--target", "pr", cwd=repo, env=env)
            for text in (
                f"the run at {first} has unfinished tasks of the target main",
                "Name the target main to move those tasks to the new run",
            ):
                self.assertIn(text, message)
            self.assertFalse(second.exists())
            self.assert_unchanged(repo, env, first, before)
            # The default target is main, so it moves the tasks.
            out = ultraclose(*init, cwd=repo, env=env)
            self.assertIn("moved 3 unfinished tasks of that run to the new run:", out)
            later = state_of(second)
            self.assertEqual(sorted(later["tasks"]), sorted(self.MOVED))
            self.assertEqual(
                (later["config"]["target"], later["config"]["base"]), ("main", "main")
            )

    def test_new_run_names_each_earlier_run(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, first, _ = self.earlier_run(tmp)
            second = Path(tmp, "second").resolve()
            later = in_session(env, "second")
            ultraclose("init", "--run", str(second), cwd=repo, env=later)
            ultraclose("set", self.APPROVED, "landed", cwd=repo, env=later)
            # A session of another version of the skill takes over the
            # second run before its end.
            third = Path(tmp, "third").resolve()
            last = in_session(env, "third")
            other = earlier_skill(tmp, changed=WORKFLOW.name)
            ultraclose("init", "--run", str(third), cwd=repo, env=last, script=other)
            self.assertEqual(state_of(third)["replaces"], [str(second), str(first)])
            self.assertEqual(
                status_values(repo, last, "earlier run"),
                [
                    f"{second}, which keeps 1 landed task",
                    f"{first}, which keeps 1 landed task",
                ],
            )
            # Status still names an earlier run whose state file is gone.
            (first / "state.json").unlink()
            texts = status_values(repo, last, "earlier run")
            self.assertEqual(texts[0], f"{second}, which keeps 1 landed task")
            unreadable = f"{first}, whose state.json this command cannot read ("
            self.assertTrue(texts[1].startswith(unreadable), texts[1])

    def test_new_run_takes_noted_tasks(self):
        # The earlier run started and noted two tasks, so each has a
        # directory and a branch. The pending task waits for the first
        # noted task, and no task waits for the other.
        noted, waiting, other = self.NOTED, self.PENDING, "APP-0006"
        stuck = f"stuck {waiting} waits for noted {noted} | t"
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = launchable_repo(tmp)
            earlier = in_session(env, "first")
            first = Path(tmp, "first").resolve()
            configured_run(repo, earlier, first, script=earlier_skill(tmp))
            triage(tmp, repo, earlier, {"ref": noted}, {"ref": other})
            ultraclose("next", cwd=repo, env=earlier)
            triage(tmp, repo, earlier, {"ref": waiting, "after": [noted]})
            for ref in (noted, other):
                ultraclose("set", ref, "noted", cwd=repo, env=earlier)
            self.assertIn(stuck, ultraclose("status", cwd=repo, env=earlier))
            before = state_of(first)
            env = in_session(env, "second")
            moves = (
                ". init moves its 1 unfinished task and its 2 noted tasks to the"
                " new run"
            )
            self.assertEqual(
                status_values(repo, env, "setup"), [NEW_RUN + STALE + moves]
            )
            second = Path(tmp, "second").resolve()
            out = ultraclose("init", "--run", str(second), cwd=repo, env=env)
            self.assertEqual(
                out.splitlines()[1:5],
                [
                    "moved 1 unfinished task of that run to the new run:",
                    f"{waiting} (pending)",
                    "moved 2 noted tasks of that run to the new run:",
                    f"{noted} {other}",
                ],
            )
            # The new run holds each noted task with its row and its
            # directory, so the pending task keeps waiting for the first.
            moved = (noted, waiting, other)
            self.assertEqual(
                state_of(second)["tasks"], {ref: before["tasks"][ref] for ref in moved}
            )
            self.assertEqual(state_of(first)["tasks"], {})
            for ref in (noted, other):
                self.assertFalse((first / "tasks" / ref).exists(), ref)
                self.assertTrue((second / "tasks" / ref / "task.md").is_file(), ref)
            status = ultraclose("status", cwd=repo, env=env)
            self.assertIn(stuck, status)
            self.assertIn(" noted=2 ", status)
            self.assertEqual(json.loads(ultraclose("next", cwd=repo, env=env)), [])
            self.assertEqual(state_of(second)["tasks"][waiting]["state"], "pending")
            # Aven lists each noted task as open, but the new run holds
            # them, so untriaged prints neither.
            aven_listed_tasks(env, {"ref": noted}, {"ref": other})
            self.assertEqual(json.loads(ultraclose("untriaged", cwd=repo, env=env)), [])

    def test_force_leaves_unfinished_tasks(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, first, _ = self.earlier_run(tmp)
            before = state_of(first)
            env = in_session(env, "second")
            second = Path(tmp, "second").resolve()
            out = ultraclose("init", "--run", str(second), "--force", cwd=repo, env=env)
            self.assertNotIn("moved", out)
            self.assertEqual(state_of(second)["tasks"], {})
            self.assertNotIn("replaces", state_of(second))
            self.assertEqual(status_values(repo, env, "earlier run"), [])
            self.assertEqual(state_of(first), before)
            for ref in (self.INFLIGHT, self.APPROVED):
                self.assertTrue((first / "tasks" / ref / "task.md").is_file(), ref)
            # The new run treats the work of those tasks as leftover work.
            triage(tmp, repo, env, {"ref": self.INFLIGHT})
            self.assertIn(OWN_WORK, refused("next", cwd=repo, env=env))

    def test_moves_no_task_while_earlier_run_lands(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, first, _ = self.earlier_run(tmp)
            before = state_of(first)
            env = in_session(env, "second")
            second = Path(tmp, "second").resolve()
            init = ["init", "--run", str(second)]
            # A landing holds the landing lock of its run.
            with open(first / "land.lock", "w") as lock:
                fcntl.flock(lock, fcntl.LOCK_EX)
                message = refused(*init, cwd=repo, env=env)
            self.assertIn(f"a landing of the run at {first} is running", message)
            self.assertFalse(second.exists())
            self.assert_unchanged(repo, env, first, before)
            ultraclose(*init, cwd=repo, env=env)
            self.assertEqual(sorted(state_of(second)["tasks"]), sorted(self.MOVED))

    def test_failed_move_changes_no_run(self):
        def read_only(repo, first):
            """Keep init from the move of the directory of the approved task.

            The move of a directory into another directory fails when the
            user cannot write the directory. Init moves the directory of
            the inflight task first.
            """
            if os.geteuid() == 0:
                self.skipTest("root moves a directory that it cannot write")
            (first / "tasks" / self.APPROVED).chmod(0o555)

        def unwritable_config(repo, first):
            """Keep init from the git config after it moves each task.

            Git writes its config through a lock file with this name, and
            it refuses the write while the file exists.
            """
            (repo / ".git" / "config.lock").touch()

        def unsaved(repo, first):
            """Keep init from the state file of the earlier run.

            Init saves that state last, after it names the new run in the
            git config, through a file with this name.
            """
            (first / "state.json.tmp").mkdir()

        cases = {
            "a task directory that init cannot move": (read_only, "Permission denied"),
            "a git config that init cannot write": (
                unwritable_config,
                "git cannot write ultraclose.run",
            ),
            "a state file that init cannot save": (unsaved, "state.json.tmp"),
        }
        for name, (block, cause) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, first, _ = self.earlier_run(tmp)
                before = state_of(first)
                env = in_session(env, "second")
                second = Path(tmp, "second").resolve()
                block(repo, first)
                try:
                    message = refused("init", "--run", str(second), cwd=repo, env=env)
                finally:
                    (first / "tasks" / self.APPROVED).chmod(0o755)
                    (repo / ".git" / "config.lock").unlink(missing_ok=True)
                self.assertIn(
                    f"init cannot move the unfinished tasks of the run at {first}",
                    message,
                )
                self.assertIn(cause, message)
                self.assertIn("Those tasks stay in that run.", message)
                self.assertIn("run init again, or pass --force", message)
                self.assertNotIn("set ultraclose.run back", message)
                self.assertFalse(second.exists())
                # The git config still names the earlier run, as the first
                # line of status says.
                self.assert_unchanged(repo, env, first, before)

    def test_config_that_init_cannot_set_back(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, first, _ = self.earlier_run(tmp)
            before = state_of(first)
            env = in_session(env, "second")
            second = Path(tmp, "second").resolve()
            # Init names the new run in the git config, and then it cannot
            # save the state of the earlier run. The wrapper refuses the git
            # command that names the earlier run again.
            (first / "state.json.tmp").mkdir()
            wrapper = Path(tmp, "wrapper")
            wrapper.mkdir()
            real = shutil.which("git", path=env["PATH"])
            (wrapper / "git").write_text(
                "#!/bin/sh\n"
                f'if [ "$1 $2 $3 $4" = "config --local ultraclose.run {first}" ]; then\n'
                '  echo "error: could not lock config file" >&2\n'
                "  exit 1\n"
                "fi\n"
                f'exec "{real}" "$@"\n'
            )
            (wrapper / "git").chmod(0o755)
            refusing = {**env, "PATH": f"{wrapper}{os.pathsep}{env['PATH']}"}
            message = refused("init", "--run", str(second), cwd=repo, env=refusing)
            self.assertIn(
                f"init cannot move the unfinished tasks of the run at {first}",
                message,
            )
            self.assertIn("state.json.tmp", message)
            self.assertIn("Those tasks stay in that run.", message)
            fix = (
                "Git also cannot set ultraclose.run back to that run, so run"
                f" git config --local ultraclose.run {first} before init."
            )
            self.assertIn(fix, message)
            # The git config names the new run directory, which init
            # removed. So no command finds a run until the caller runs the
            # git command of the message.
            self.assertFalse(second.exists())
            self.assertEqual(
                git("config", "--get", "ultraclose.run", cwd=repo, env=env),
                str(second),
            )
            self.assertIn(
                f"ultraclose.run in the git config names {second}, which holds"
                " no state.json",
                refused("status", cwd=repo, env=env),
            )
            self.assertEqual(state_of(first), before)
            for ref in (self.INFLIGHT, self.APPROVED):
                self.assertTrue((first / "tasks" / ref / "task.md").is_file(), ref)
            command = re.search(r"so run (git config .+?) before init", message)
            subprocess.run(command[1].split(), cwd=repo, env=env, check=True)
            self.assert_unchanged(repo, env, first, before)
            # After the caller removes the cause, init moves the tasks.
            (first / "state.json.tmp").rmdir()
            ultraclose("init", "--run", str(second), cwd=repo, env=env)
            self.assertEqual(sorted(state_of(second)["tasks"]), sorted(self.MOVED))


class MissingRunTest(unittest.TestCase):
    """A git config that names no run that a command can read blocks no new run."""

    def test_no_run(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = new_repo(tmp)
            for command in (["status"], ["next"], ["watch"]):
                with self.subTest(command=command):
                    message = refused(*command, cwd=repo, env=env)
                    self.assertIn(
                        "no run found for this repository, because the git config"
                        " of the repository does not set ultraclose.run. Start a"
                        " run with init",
                        message,
                    )

    def test_run_directory_that_holds_no_run(self):
        def remove(run):
            shutil.rmtree(run)

        def lose_state(run):
            (run / "state.json").unlink()

        def cut_state(run):
            (run / "state.json").write_text('{"config": {')

        def empty_state(run):
            (run / "state.json").write_text("{}")

        missing = "which holds no state.json"
        unread = "and the command cannot read {}/state.json"
        # name: (what happens to the run directory, what each command
        # says about the directory)
        cases = {
            "a directory that is gone": (remove, missing),
            "a directory without its state file": (lose_state, missing),
            "a state file that ends early": (cut_state, unread),
            "a state file without a config": (empty_state, unread),
        }
        for name, (lose, cause) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, first = first_run(tmp, "inflight", "first")
                lose(first)
                env = in_session(env, "second")
                named = f"ultraclose.run in the git config names {first}, "
                for command in (["status"], ["next"], ["config", "--limit", "3"]):
                    message = refused(*command, cwd=repo, env=env)
                    self.assertIn("no run found for this repository", message)
                    self.assertIn(named + cause.format(first), message)
                    self.assertIn("Start a run with init", message)
                    self.assertNotIn("Traceback", message)
                second = Path(tmp, "second").resolve()
                out = ultraclose("init", "--run", str(second), cwd=repo, env=env)
                self.assertIn(
                    "replaced a run that init cannot read, because "
                    + named
                    + cause.format(first),
                    out,
                )
                self.assertEqual(state_of(second)["tasks"], {})
                assert_fresh(self, second)
                status = ultraclose("status", cwd=repo, env=env)
                self.assertEqual(status.splitlines()[0], f"run={second}")


class StatusTest(unittest.TestCase):
    """The status command shows what a later batch of tasks inherits."""

    def test_prints_config(self):
        # name: (the init flags, the shared globs, the models, the check,
        # the health command, the step timeout)
        cases = {
            "the defaults": ([], DEFAULT_SHARED, "none", "none", "none", "60m00s"),
            "a shared glob, models, a check, a health command, and a step timeout": (
                [
                    "--shared",
                    GLOB,
                    # The line names the roles in the order of close-task.js
                    # and the default last, whatever the order of the flags.
                    "--model",
                    f"{DEFAULT_ROLE}={OPUS}",
                    "--model",
                    f"fixer={OPUS}",
                    "--model",
                    f"implementer={FABLE}",
                    "--check",
                    "sh check.sh",
                    "--health",
                    "true",
                    "--step-timeout",
                    "90",
                ],
                DEFAULT_SHARED + [GLOB],
                f"implementer={FABLE} fixer={OPUS} {DEFAULT_ROLE}={OPUS}",
                "sh check.sh",
                "true",
                "1m30s",
            ),
        }
        for name, (flags, shared, models, check, health, timeout) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                ultraclose(
                    "init", "--run", str(Path(tmp, "run")), *flags, cwd=repo, env=env
                )
                lines = ultraclose("status", cwd=repo, env=env).splitlines()
                self.assertIn(f"shared: {' '.join(shared)}", lines)
                self.assertIn(f"models: {models}", lines)
                self.assertIn(f"check: {check}", lines)
                self.assertIn(f"health: {health}", lines)
                self.assertIn(f"step timeout: {timeout}", lines)

    def test_state_without_health_key(self):
        # A run can continue from a state file that init wrote without
        # the health key.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = new_repo(tmp)
            run = Path(tmp, "run")
            ultraclose("init", "--run", str(run), cwd=repo, env=env)
            st = state_of(run)
            del st["config"]["health"]
            (run / "state.json").write_text(json.dumps(st))
            lines = ultraclose("status", cwd=repo, env=env).splitlines()
            self.assertIn("health: none", lines)

    def test_state_without_step_timeout_key(self):
        # A run can continue from a state file that init wrote without
        # the stepTimeout key. That run has the default step timeout.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = new_repo(tmp)
            run = Path(tmp, "run")
            ultraclose(
                "init", "--run", str(run), "--step-timeout", "90", cwd=repo, env=env
            )
            st = state_of(run)
            del st["config"]["stepTimeout"]
            (run / "state.json").write_text(json.dumps(st))
            lines = ultraclose("status", cwd=repo, env=env).splitlines()
            self.assertIn("step timeout: 60m00s", lines)

    def test_marks_ended_run(self):
        # name: (the state of each task, whether status marks the run)
        cases = {"no task": ([], True)}
        cases |= {f"one {s} task": ([s], True) for s in FINISHED}
        cases |= {f"one {s} task": ([s], False) for s in UNFINISHED}
        cases |= {
            f"a landed task and one {s} task": (["landed", s], False)
            for s in UNFINISHED
        }
        for name, (task_states, ended) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                ultraclose("init", "--run", str(Path(tmp, "run")), cwd=repo, env=env)
                refs = [f"APP-000{n}" for n in range(len(task_states))]
                if refs:
                    triage(tmp, repo, env, *({"ref": ref} for ref in refs))
                for ref, task_state in zip(refs, task_states):
                    ultraclose("set", ref, task_state, cwd=repo, env=env)
                lines = ultraclose("status", cwd=repo, env=env).splitlines()
                self.assertEqual(ENDED in lines, ended)

    def test_prints_landing_times(self):
        def landing(n, result, **times):
            return {"ref": f"APP-000{n}", "result": result, "times": times}

        # The medians count the landings that landed a task. The times of
        # the three checks are 80, 90, and 85.4 seconds, and the times of
        # the four commits are 10, 20, 710, and 16 seconds.
        landings = [
            landing(1, "OK", check=80, commit=10),
            landing(2, "CHECKFAIL", check=9000),
            landing(3, "OK", check=90, commit=20),
            landing(4, "COMMITFAIL", check=85, commit=9000),
            landing(5, "OK", commit=710),
            landing(6, "OK", check=85.4, commit=16),
        ]
        # name: (the landings of the run, the lines that status prints)
        cases = {
            "no landing": ([], []),
            "a failed landing gives no median": (
                landings[1:2],
                ["landing APP-0002 CHECKFAIL: check=150m00s"],
            ),
            "the medians and the last five landings": (
                landings,
                [
                    "median landing: check=1m25s commit=18s",
                    "landing APP-0002 CHECKFAIL: check=150m00s",
                    "landing APP-0003 OK: check=1m30s commit=20s",
                    "landing APP-0004 COMMITFAIL: check=1m25s commit=150m00s",
                    "landing APP-0005 OK: commit=11m50s",
                    "landing APP-0006 OK: check=1m25s commit=16s",
                ],
            ),
        }
        for name, (recorded, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                run = Path(tmp, "run")
                ultraclose("init", "--run", str(run), cwd=repo, env=env)
                # The state file of a run has no landings key before the
                # first landing that runs a timed step.
                if recorded:
                    st = state_of(run)
                    st["landings"] = recorded
                    (run / "state.json").write_text(json.dumps(st))
                lines = ultraclose("status", cwd=repo, env=env).splitlines()
                found = [
                    line
                    for line in lines
                    if line.startswith(("median landing", "landing "))
                ]
                self.assertEqual(found, want)


class ConfigTest(unittest.TestCase):
    """The config command changes a run after init."""

    def test_changes_config(self):
        shared = DEFAULT_SHARED + [GLOB]
        # name: (the flags, the config values that change)
        cases = {
            "no flag": ([], {}),
            "--limit": (["--limit", "10"], {"limit": 10}),
            "--rounds": (["--rounds", "4"], {"rounds": 4}),
            "--model names the model of a role": (
                ["--model", f"implementer={FABLE}"],
                {"models": {"reviewer": OPUS, "implementer": FABLE}},
            ),
            "--model names the default model": (
                ["--model", f"{DEFAULT_ROLE}={FABLE}"],
                {"models": {"reviewer": OPUS, DEFAULT_ROLE: FABLE}},
            ),
            "--model changes the model of a role": (
                ["--model", f"reviewer={FABLE}"],
                {"models": {"reviewer": FABLE}},
            ),
            "an empty --model removes the model of a role": (
                ["--model", "reviewer="],
                {"models": {}},
            ),
            "an empty --model of a role without a model": (
                ["--model", "fixer="],
                {},
            ),
            "the last --model of a role applies": (
                ["--model", "fixer=", "--model", f"fixer={FABLE}"],
                {"models": {"reviewer": OPUS, "fixer": FABLE}},
            ),
            "--check": (["--check", "sh other.sh"], {"check": "sh other.sh"}),
            "an empty --check": (["--check", ""], {"check": ""}),
            "--health": (["--health", "true"], {"health": "true"}),
            "an empty --health": (["--health", ""], {"health": ""}),
            "--step-timeout": (["--step-timeout", "120.5"], {"stepTimeout": 120.5}),
            "--disk-floor-gb": (["--disk-floor-gb", "12.5"], {"diskFloorGb": 12.5}),
            "--shared adds a glob": (
                ["--shared", "*/main.go"],
                {"shared": shared + ["*/main.go"]},
            ),
            "--shared keeps one copy of a glob": (["--shared", GLOB], {}),
            "--unshared removes an init glob": (
                ["--unshared", GLOB],
                {"shared": DEFAULT_SHARED},
            ),
            "--unshared removes a default glob": (
                ["--unshared", "*/tests/*"],
                {"shared": [g for g in shared if g != "*/tests/*"]},
            ),
            "several flags": (
                ["--limit", "10", "--shared", "*/main.go", "--unshared", GLOB],
                {"limit": 10, "shared": DEFAULT_SHARED + ["*/main.go"]},
            ),
        }
        for name, (flags, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                run = Path(tmp, "run")
                configured_run(repo, env, run)
                triage(tmp, repo, env, {"ref": REF})
                before = state_of(run)
                printed = json.loads(ultraclose("config", *flags, cwd=repo, env=env))
                after = state_of(run)
                self.assertEqual(after["config"], {**before["config"], **want})
                self.assertEqual(printed, after["config"])
                self.assertEqual(after["tasks"], before["tasks"])

    def test_refuses_to_remove_absent_glob(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = new_repo(tmp)
            run = Path(tmp, "run")
            configured_run(repo, env, run)
            before = state_of(run)
            message = refused(
                "config",
                "--limit",
                "10",
                "--unshared",
                "*/absent/*",
                cwd=repo,
                env=env,
            )
            self.assertIn("no shared glob */absent/*", message)
            # The command changes nothing when it refuses one flag.
            self.assertEqual(state_of(run), before)

    def test_refuses_step_timeout_that_is_not_positive_and_finite(self):
        # A landing with a step timeout of 0 stops each step at its start,
        # and a landing with an infinite step timeout stops no step.
        for value in ("0", "-1", "inf", "nan"):
            with self.subTest(value), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                flag = f"--step-timeout={value}"
                run = Path(tmp, "run")
                message = refused("init", "--run", str(run), flag, cwd=repo, env=env)
                self.assertIn("greater than 0", message)
                self.assertFalse(run.exists())
                configured_run(repo, env, run)
                before = state_of(run)
                message = refused("config", flag, cwd=repo, env=env)
                self.assertIn("greater than 0", message)
                self.assertEqual(state_of(run), before)

    def test_next_follows_shared_globs(self):
        # Two tasks edit one file, and the shared glob of the run covers it.
        refs = ["APP-0001", "APP-0002"]
        # name: (the config flags, the tasks that next starts)
        cases = {
            "the run keeps the glob": ([], refs),
            "config removes the glob": (["--unshared", GLOB], refs[:1]),
        }
        for name, (flags, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = launchable_repo(tmp)
                configured_run(repo, env, Path(tmp, "run"))
                triage(tmp, repo, env, *({"ref": r, "files": [COVERED]} for r in refs))
                ultraclose("config", *flags, cwd=repo, env=env)
                started = json.loads(ultraclose("next", cwd=repo, env=env))
                self.assertEqual([t["task"]["ref"] for t in started], want)


def model_flags(models):
    """One --model flag per role of the given models."""
    return [f"--model={role}={model}" for role, model in models.items()]


class ModelTest(unittest.TestCase):
    """A run names the model that the agents of each role run on.

    Each Workflow input carries the model of each role, and close-task.js
    starts each agent on the model of its role. The agents of a role
    without a model follow the session model.
    """

    def test_input_names_model_of_each_role(self):
        # name: (the models of the init flags, the models field of each
        # Workflow input)
        cases = {
            "no flag": ({}, {}),
            "one role": ({"implementer": FABLE}, {"implementer": FABLE}),
            "every role": (
                {role: f"model-of-{role}" for role in ROLES},
                {role: f"model-of-{role}" for role in ROLES},
            ),
            "the default covers every role": (
                {DEFAULT_ROLE: OPUS},
                dict.fromkeys(ROLES, OPUS),
            ),
            "the default covers each role without a model of its own": (
                {"implementer": FABLE, DEFAULT_ROLE: OPUS},
                {**dict.fromkeys(ROLES, OPUS), "implementer": FABLE},
            ),
            "the default comes first": (
                {DEFAULT_ROLE: OPUS, "narrow": FABLE},
                {**dict.fromkeys(ROLES, OPUS), "narrow": FABLE},
            ),
            "an empty model names no model": ({"reviewer": ""}, {}),
            "the spaces around a model do not count": (
                {"reviewer": f" {OPUS} "},
                {"reviewer": OPUS},
            ),
        }
        for name, (flagged, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, launch = running_task(tmp, flags=model_flags(flagged))
                self.assertEqual(launch["models"], want)
                # A relaunch runs on the models of the run.
                relaunch = json.loads(ultraclose("args", REF, cwd=repo, env=env))
                self.assertEqual(relaunch["models"], want)

    def test_config_changes_models_of_later_launches(self):
        first = {"implementer": FABLE, DEFAULT_ROLE: OPUS}
        # name: (the config flags, the models field of the next Workflow
        # input)
        cases = {
            "no flag": ([], {**dict.fromkeys(ROLES, OPUS), "implementer": FABLE}),
            "a role gets a model of its own": (
                [f"--model=fixer={FABLE}"],
                {**dict.fromkeys(ROLES, OPUS), "implementer": FABLE, "fixer": FABLE},
            ),
            "a role without a model of its own takes the default": (
                ["--model=implementer="],
                dict.fromkeys(ROLES, OPUS),
            ),
            # A new default leaves each role with a model of its own on
            # that model. Arguments that name a model for every agent
            # also remove the model of each such role.
            "a new default": (
                [f"--model={DEFAULT_ROLE}={NEW}"],
                {**dict.fromkeys(ROLES, NEW), "implementer": FABLE},
            ),
            "a new default for every role": (
                [f"--model={DEFAULT_ROLE}={NEW}", "--model=implementer="],
                dict.fromkeys(ROLES, NEW),
            ),
            "a run without a default": (
                [f"--model={DEFAULT_ROLE}="],
                {"implementer": FABLE},
            ),
            "a run without a model": (
                ["--model=implementer=", f"--model={DEFAULT_ROLE}="],
                {},
            ),
        }
        for name, (flags, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, launch = running_task(tmp, flags=model_flags(first))
                ultraclose("config", *flags, cwd=repo, env=env)
                relaunch = json.loads(ultraclose("args", REF, cwd=repo, env=env))
                self.assertEqual(relaunch["models"], want)
                # The rest of the input matches the first launch.
                del relaunch["task"]["resume"]
                self.assertEqual({**relaunch, "models": launch["models"]}, launch)

    def test_refuses_model_flag_without_role_or_model_name(self):
        values = {
            "no model": "reviewer",
            "no role": f"={OPUS}",
            "a role that close-task.js does not have": f"review={OPUS}",
            "a role in capitals": f"Reviewer={OPUS}",
            "a model of two words": "reviewer=claude opus",
        }
        for name, value in values.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                run = Path(tmp, "run")
                flag = f"--model={value}"
                message = refused("init", "--run", str(run), flag, cwd=repo, env=env)
                self.assertIn("give ROLE=MODEL", message)
                self.assertFalse(run.exists())
                configured_run(repo, env, run)
                before = state_of(run)
                message = refused("config", flag, cwd=repo, env=env)
                self.assertIn("give ROLE=MODEL", message)
                # The message names each role that a flag can hold.
                for role in (*ROLES, DEFAULT_ROLE):
                    self.assertIn(role, message)
                self.assertEqual(state_of(run), before)

    def test_help_names_roles(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = new_repo(tmp)
            for command in ("init", "config"):
                with self.subTest(command):
                    text = ultraclose(command, "--help", cwd=repo, env=env)
                    text = " ".join(text.split())
                    self.assertIn("--model ROLE=MODEL", text)
                    self.assertIn(
                        f"ROLE is one of {', '.join(ROLES)}, or {DEFAULT_ROLE}", text
                    )
                    self.assertIn("follow the session model", text)

    def test_state_without_models_key(self):
        # A run can continue from a state file that init wrote without
        # the models key. That run names no model.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = launchable_repo(tmp)
            run = Path(tmp, "run")
            ultraclose(
                "init", "--run", str(run), f"--model=fixer={FABLE}", cwd=repo, env=env
            )
            triage(tmp, repo, env, {"ref": REF})
            st = state_of(run)
            del st["config"]["models"]
            (run / "state.json").write_text(json.dumps(st))
            lines = ultraclose("status", cwd=repo, env=env).splitlines()
            self.assertIn("models: none", lines)
            launch = json.loads(ultraclose("next", cwd=repo, env=env))[0]
            self.assertEqual(launch["models"], {})
            ultraclose("config", f"--model=reviewer={OPUS}", cwd=repo, env=env)
            self.assertEqual(state_of(run)["config"]["models"], {"reviewer": OPUS})
            relaunch = json.loads(ultraclose("args", REF, cwd=repo, env=env))
            self.assertEqual(relaunch["models"], {"reviewer": OPUS})


class DiskTest(unittest.TestCase):
    """A run watches the free space of the file systems of its disk paths."""

    def test_init_sets_floors(self):
        # Each flag names a path by its key in braces.
        # name: (the init flags, the floor in GB, the percent floor of each
        # path by its key, the disk paths by their keys)
        cases = {
            "no flag": ([], 30, {}, ["repo", "home"]),
            "both flags": (
                ["--disk-floor-gb", "12.5", "--disk-floor-percent", "{home}=25"],
                12.5,
                {"home": 25},
                ["repo", "home"],
            ),
            "a percent floor adds its path": (
                ["--disk-floor-percent", "{cache}=25"],
                30,
                {"cache": 25},
                ["repo", "home", "cache"],
            ),
            "a percent floor for each path": (
                ["--disk-floor-percent", "{home}=10"]
                + ["--disk-floor-percent", "{cache}=25"],
                30,
                {"home": 10, "cache": 25},
                ["repo", "home", "cache"],
            ),
            "the last flag of a path applies": (
                ["--disk-floor-percent", "{cache}=25"]
                + ["--disk-floor-percent", "{cache}=12.5"],
                30,
                {"cache": 12.5},
                ["repo", "home", "cache"],
            ),
            "a floor of 0 percent sets no floor and adds no path": (
                ["--disk-floor-percent", "{cache}=0"],
                30,
                {},
                ["repo", "home"],
            ),
            "the last = of a flag ends its path": (
                ["--disk-floor-percent", "{equals}=25"],
                30,
                {"equals": 25},
                ["repo", "home", "equals"],
            ),
        }
        for name, (flags, gb, percents, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, home = home_repo(tmp)
                paths = {"home": str(home)}
                for key, directory in (("cache", "cache"), ("equals", "a=b")):
                    paths[key] = str(Path(tmp, directory))
                    Path(paths[key]).mkdir()
                run = Path(tmp, "run")
                flags = [flag.format(**paths) for flag in flags]
                ultraclose("init", "--run", str(run), *flags, cwd=repo, env=env)
                config = state_of(run)["config"]
                paths["repo"] = config["repo"]
                self.assertEqual(config["diskFloorGb"], gb)
                self.assertEqual(
                    config["diskFloorPercent"],
                    {paths[key]: n for key, n in percents.items()},
                )
                self.assertEqual(config["diskPaths"], [paths[key] for key in want])

    def test_init_sets_disk_paths(self):
        # name: (the path of each --disk-path flag, the disk paths of the run)
        cases = {
            "no flag keeps the defaults": ([], ["repo", "home"]),
            "a flag adds to the defaults": (["cache"], ["repo", "home", "cache"]),
            "flags keep their order": (
                ["other", "cache"],
                ["repo", "home", "other", "cache"],
            ),
            "a default path appears once": (
                ["home", "cache"],
                ["repo", "home", "cache"],
            ),
            "a relative path": (["../cache"], ["repo", "home", "cache"]),
        }
        for name, (flags, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, home = home_repo(tmp)
                for directory in ("cache", "other"):
                    Path(tmp, directory).mkdir()
                run = Path(tmp, "run")
                args = ["init", "--run", str(run)]
                for flag in flags:
                    path = flag if flag.startswith(".") else str(Path(tmp, flag))
                    args += ["--disk-path", path]
                ultraclose(*args, cwd=repo, env=env)
                config = state_of(run)["config"]
                # The run names the repository by the path that git prints.
                root = Path(config["repo"]).parent
                paths = {"repo": config["repo"], "home": str(home)}
                paths |= {d: str(root / d) for d in ("cache", "other")}
                # A temporary directory can sit behind a symbolic link.
                self.assertEqual(
                    [os.path.realpath(p) for p in config["diskPaths"]],
                    [os.path.realpath(paths[key]) for key in want],
                )

    def test_init_without_home_directory(self):
        # The home directory of the environment does not exist, so the run
        # watches only the repository.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = new_repo(tmp)
            env["HOME"] = str(Path(tmp, "absent"))
            run = Path(tmp, "run")
            flags = ["--disk-floor-gb", "0"]
            ultraclose("init", "--run", str(run), *flags, cwd=repo, env=env)
            config = state_of(run)["config"]
            self.assertEqual(config["diskPaths"], [config["repo"]])
            # The watch command measures the repository, so it does not
            # report a path that it cannot measure.
            [text] = disk_texts(repo, env)
            found = DISK_TEXT.fullmatch(text)
            self.assertTrue(found, text)
            self.assertEqual(found["path"], config["repo"])
            self.assertEqual(watched(repo, env), [WATCH_ENDED])

    def test_init_refuses_bad_disk_flags(self):
        # Each flag names the home directory as {home}.
        # name: (the flags, a part of the message)
        cases = {
            "a disk path that does not exist": (
                ["--disk-path", "absent"],
                "absent does not exist",
            ),
            "a percent floor of a path that does not exist": (
                ["--disk-floor-percent", "absent=25"],
                "absent does not exist",
            ),
            "a percent floor without a path": (
                ["--disk-floor-percent", "25"],
                "give PATH=N",
            ),
            "a percent floor with an empty path": (
                ["--disk-floor-percent", "=25"],
                "give PATH=N",
            ),
            "a floor of more than 100 percent": (
                ["--disk-floor-percent", "{home}=100.5"],
                "between 0 and 100",
            ),
            "a floor of less than 0 percent": (
                ["--disk-floor-percent", "{home}=-1"],
                "between 0 and 100",
            ),
        }
        for name, (flags, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, home = home_repo(tmp)
                flags = [flag.format(home=home) for flag in flags]
                run = Path(tmp, "run")
                message = refused("init", "--run", str(run), *flags, cwd=repo, env=env)
                self.assertIn(want, message)
                self.assertFalse(run.exists())

    def disk_run(self, tmp):
        """Start a run that watches the default paths and a cache directory.

        Return the repository, the environment, the run directory, and
        each disk path by its name.
        """
        repo, env, home = home_repo(tmp)
        cache, run = Path(tmp, "cache"), Path(tmp, "run")
        cache.mkdir()
        ultraclose(
            "init",
            "--run",
            str(run),
            "--disk-floor-gb",
            "0",
            "--disk-path",
            str(cache),
            cwd=repo,
            env=env,
        )
        paths = {
            "repo": state_of(run)["config"]["repo"],
            "home": str(home),
            "cache": str(cache),
            "other": str(Path(tmp, "other")),
        }
        Path(paths["other"]).mkdir()
        return repo, env, run, paths

    def test_config_changes_disk_paths(self):
        # name: (the paths that --disk-path adds, the paths that
        # --no-disk-path removes, the disk paths of the run afterward)
        cases = {
            "--disk-path adds a path": (
                ["other"],
                [],
                ["repo", "home", "cache", "other"],
            ),
            "--disk-path keeps one copy of a path": (
                ["cache"],
                [],
                ["repo", "home", "cache"],
            ),
            "--no-disk-path removes an init path": ([], ["cache"], ["repo", "home"]),
            "--no-disk-path removes a default path": (
                [],
                ["home"],
                ["repo", "cache"],
            ),
            "both flags": (["other"], ["repo", "cache"], ["home", "other"]),
        }
        for name, (added, removed, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, run, paths = self.disk_run(tmp)
                before = state_of(run)
                # The command removes a path that no directory holds anymore.
                if "cache" in removed:
                    Path(paths["cache"]).rmdir()
                flags = []
                for key in added:
                    flags += ["--disk-path", paths[key]]
                for key in removed:
                    flags += ["--no-disk-path", paths[key]]
                printed = json.loads(ultraclose("config", *flags, cwd=repo, env=env))
                after = state_of(run)
                self.assertEqual(
                    after["config"],
                    {**before["config"], "diskPaths": [paths[key] for key in want]},
                )
                self.assertEqual(printed, after["config"])

    def test_config_changes_percent_floors(self):
        # Each case starts from a run with a percent floor of 25 for the
        # cache directory. Each flag names a path by its key in braces.
        # name: (the flags, the percent floor of each path by its key, the
        # disk paths by their keys)
        cases = {
            "a floor for another disk path": (
                ["--disk-floor-percent", "{home}=10"],
                {"cache": 25, "home": 10},
                ["repo", "home", "cache"],
            ),
            "a floor adds its path": (
                ["--disk-floor-percent", "{other}=10"],
                {"cache": 25, "other": 10},
                ["repo", "home", "cache", "other"],
            ),
            "a floor replaces the floor of its path": (
                ["--disk-floor-percent", "{cache}=12.5"],
                {"cache": 12.5},
                ["repo", "home", "cache"],
            ),
            "a floor of 0 percent removes the floor and keeps the path": (
                ["--disk-floor-percent", "{cache}=0"],
                {},
                ["repo", "home", "cache"],
            ),
            "a floor of 0 percent adds no path": (
                ["--disk-floor-percent", "{other}=0"],
                {"cache": 25},
                ["repo", "home", "cache"],
            ),
            "--no-disk-path removes the floor of its path": (
                ["--no-disk-path", "{cache}"],
                {},
                ["repo", "home"],
            ),
            "--no-disk-path wins over a floor of its path": (
                ["--disk-floor-percent", "{cache}=10", "--no-disk-path", "{cache}"],
                {},
                ["repo", "home"],
            ),
            "another flag keeps the floors": (
                ["--disk-floor-gb", "12.5"],
                {"cache": 25},
                ["repo", "home", "cache"],
            ),
        }
        for name, (flags, percents, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, run, paths = self.disk_run(tmp)
                floor = ["--disk-floor-percent", f"{paths['cache']}=25"]
                ultraclose("config", *floor, cwd=repo, env=env)
                before = state_of(run)
                self.assertEqual(
                    before["config"]["diskFloorPercent"], {paths["cache"]: 25}
                )
                flags = [flag.format(**paths) for flag in flags]
                printed = json.loads(ultraclose("config", *flags, cwd=repo, env=env))
                after = state_of(run)
                changed = {
                    "diskFloorPercent": {paths[key]: n for key, n in percents.items()},
                    "diskPaths": [paths[key] for key in want],
                }
                if "--disk-floor-gb" in flags:
                    changed["diskFloorGb"] = 12.5
                self.assertEqual(after["config"], {**before["config"], **changed})
                self.assertEqual(printed, after["config"])

    def test_config_refuses_bad_disk_flags(self):
        # name: (the flags, a part of the message)
        cases = {
            "a disk path that does not exist": (
                ["--disk-path", "absent"],
                "absent does not exist",
            ),
            "a path that the run does not watch": (
                ["--no-disk-path", "absent"],
                "no disk path",
            ),
            "a percent floor without a path": (
                ["--disk-floor-percent", "25"],
                "give PATH=N",
            ),
            "a floor of more than 100 percent": (
                ["--disk-floor-percent", "{cache}=100.5"],
                "between 0 and 100",
            ),
        }
        for name, (flags, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, run, paths = self.disk_run(tmp)
                flags = [flag.format(**paths) for flag in flags]
                before = state_of(run)
                message = refused("config", "--limit", "10", *flags, cwd=repo, env=env)
                self.assertIn(want, message)
                # The command changes nothing when it refuses one flag.
                self.assertEqual(state_of(run), before)

    def test_floors(self):
        with tempfile.TemporaryDirectory() as tmp:
            usage = shutil.disk_usage(tmp)
        size, free = usage.total / 2**30, usage.free / 2**30
        share = 100 * free / size
        # The free space changes during the test, and so does the size of
        # some file systems. Each floor that the free space meets is half
        # of the free space, and each other floor is far more than the
        # free space. The test allows 1 GB between each size that it
        # computes and the size that a command prints.
        met_gb, unmet_gb = free / 2, 2 * free + 1
        met_share, unmet_share = share / 2, (share + 100) / 2
        # name: (the floor in GB, the percent floor of the home directory,
        # whether the file system is low)
        cases = {
            "no floor": (0, 0, False),
            "a floor in GB that the free space meets": (met_gb, 0, False),
            "a floor in GB that the free space does not meet": (unmet_gb, 0, True),
            "a floor in percent that the free space meets": (0, met_share, False),
            "a floor in percent that the free space does not meet": (
                0,
                unmet_share,
                True,
            ),
            "two floors that the free space meets": (met_gb, met_share / 2, False),
            "the floor in GB is the higher floor": (unmet_gb, met_share, True),
            "the floor in percent is the higher floor": (met_gb, unmet_share, True),
        }
        for name, (gb, percent, low) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, home = home_repo(tmp)
                run = Path(tmp, "run")
                ultraclose(
                    "init",
                    "--run",
                    str(run),
                    "--disk-floor-gb",
                    repr(gb),
                    "--disk-floor-percent",
                    f"{home}={percent!r}",
                    cwd=repo,
                    env=env,
                )
                # The repository and the home directory share a file
                # system, so status prints one line. The line names the
                # home directory when its percent floor is the higher
                # floor. No case puts the two floors near each other.
                by = str(home) if size * percent / 100 > gb else None
                [text] = disk_texts(repo, env)
                found = DISK_TEXT.fullmatch(text)
                self.assertTrue(found, text)
                self.assertEqual(found["path"], state_of(run)["config"]["repo"])
                self.assertAlmostEqual(int(found["size"]), size, delta=1)
                floor = max(gb, size * percent / 100)
                self.assertAlmostEqual(int(found["floor"]), floor, delta=1)
                self.assertEqual(found["low"] == LOW, low)
                self.assertEqual(found["by"], by)
                # The run holds no task, so the watch command exits with
                # the ENDED line. The DISK line of a low file system
                # comes first.
                *reported, last = watched(repo, env)
                self.assertEqual(last, WATCH_ENDED)
                self.assertEqual(len(reported), low)
                for line in reported:
                    found = DISK_TEXT.fullmatch(line.removeprefix("DISK "))
                    self.assertTrue(line.startswith("DISK ") and found, line)
                    self.assertEqual(found["low"], LOW)
                    self.assertAlmostEqual(int(found["floor"]), floor, delta=1)
                    self.assertEqual(found["by"], by)

    def test_highest_percent_floor_of_file_system(self):
        # The home directory and the cache directory share the file system
        # of the temporary directory, and each has a percent floor.
        # name: (the percent floor of the home directory, the percent
        # floor of the cache directory, the key of the path whose floor
        # applies)
        cases = {
            "the later path has the higher floor": (1, 100, "cache"),
            "the earlier path has the higher floor": (100, 1, "home"),
        }
        for name, (home_percent, cache_percent, key) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, home = home_repo(tmp)
                paths = {"home": str(home), "cache": str(Path(tmp, "cache"))}
                Path(paths["cache"]).mkdir()
                flags = ["--disk-floor-gb", "0"]
                flags += ["--disk-floor-percent", f"{paths['home']}={home_percent}"]
                flags += ["--disk-floor-percent", f"{paths['cache']}={cache_percent}"]
                run = Path(tmp, "run")
                ultraclose("init", "--run", str(run), *flags, cwd=repo, env=env)
                [text] = disk_texts(repo, env)
                found = DISK_TEXT.fullmatch(text)
                self.assertTrue(found, text)
                # Some of the file system is in use, so its free space is
                # less than the floor of 100 percent.
                self.assertEqual(
                    (found["path"], found["low"], found["by"], found["percent"]),
                    (state_of(run)["config"]["repo"], LOW, paths[key], "100"),
                )
                self.assertAlmostEqual(int(found["floor"]), int(found["size"]), delta=1)

    def test_percent_floor_applies_to_its_file_system(self):
        # The home directory sits on the file system of the temporary
        # directory and has a percent floor that no file system meets. The
        # other path sits on another file system and has no percent floor.
        with tempfile.TemporaryDirectory() as tmp:
            other = other_file_system(tmp)
            if other is None:
                self.skipTest("no path sits on another file system")
            repo, env, home = home_repo(tmp)
            run = Path(tmp, "run")
            flags = ["--disk-floor-gb", "0", "--disk-path", other]
            flags += ["--disk-floor-percent", f"{home}=100"]
            ultraclose("init", "--run", str(run), *flags, cwd=repo, env=env)
            texts = disk_texts(repo, env)
            found = [DISK_TEXT.fullmatch(text) for text in texts]
            self.assertTrue(all(found), texts)
            self.assertEqual(
                [(f["path"], f["low"], f["by"], f["floor"]) for f in found[1:]],
                [(other, "floor", None, "0")],
            )
            self.assertEqual(
                (found[0]["path"], found[0]["low"], found[0]["by"]),
                (state_of(run)["config"]["repo"], LOW, str(home)),
            )
            self.assertEqual(watched(repo, env), [f"DISK {texts[0]}", WATCH_ENDED])

    def test_file_system_without_size(self):
        # The proc file system of Linux reports a size of 0.
        if not os.path.isdir("/proc") or shutil.disk_usage("/proc").total:
            self.skipTest("no /proc file system with a size of 0")
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, _ = home_repo(tmp)
            run = Path(tmp, "run")
            flags = ["--disk-floor-gb", "0", "--disk-path", "/proc"]
            ultraclose("init", "--run", str(run), *flags, cwd=repo, env=env)
            texts = disk_texts(repo, env)
            self.assertEqual(len(texts), 2, texts)
            found = DISK_TEXT.fullmatch(texts[0])
            self.assertTrue(found, texts)
            self.assertEqual(found["path"], state_of(run)["config"]["repo"])
            self.assertEqual(texts[1], "/proc: 0 GB free of 0 GB (0%), floor 0 GB")
            self.assertEqual(watched(repo, env), [WATCH_ENDED])

    def test_one_line_per_file_system(self):
        # The disk paths of the run share one file system.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, _, paths = self.disk_run(tmp)
            for first in ("repo", "home", "cache"):
                # The line names the first disk path of the file system.
                [text] = disk_texts(repo, env)
                self.assertEqual(DISK_TEXT.fullmatch(text)["path"], paths[first])
                ultraclose("config", "--no-disk-path", paths[first], cwd=repo, env=env)
            # A run without a disk path watches no file system.
            self.assertEqual(disk_texts(repo, env), [])
            self.assertEqual(watched(repo, env), [WATCH_ENDED])

    def test_one_line_per_file_system_on_each_device(self):
        # The repository and the home directory share the file system of
        # the temporary directory. The third disk path sits on another
        # file system, so status prints a line for each of the two.
        with tempfile.TemporaryDirectory() as tmp:
            other = other_file_system(tmp)
            if other is None:
                self.skipTest("no path sits on another file system")
            repo, env, _ = home_repo(tmp)
            run = Path(tmp, "run")
            flags = ["--disk-floor-gb", "0", "--disk-path", other]
            ultraclose("init", "--run", str(run), *flags, cwd=repo, env=env)
            texts = disk_texts(repo, env)
            found = [DISK_TEXT.fullmatch(text) for text in texts]
            self.assertTrue(all(found), texts)
            self.assertEqual(
                [(f["path"], f["low"]) for f in found],
                [(state_of(run)["config"]["repo"], "floor"), (other, "floor")],
            )
            self.assertEqual(watched(repo, env), [WATCH_ENDED])

    def test_reports_path_that_it_cannot_measure(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, _, paths = self.disk_run(tmp)
            Path(paths["cache"]).rmdir()
            measured, failed = disk_texts(repo, env)
            self.assertTrue(DISK_TEXT.fullmatch(measured), measured)
            start = f"{paths['cache']}: cannot measure the free space, "
            self.assertTrue(failed.startswith(start), failed)
            # The watch command reports the path, so it does not stay
            # silent about a file system that it cannot measure.
            self.assertEqual(watched(repo, env), [f"DISK {failed}", WATCH_ENDED])

    def test_state_without_disk_keys(self):
        # A run can continue from a state file that init wrote without
        # the percent floor and the disk paths.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, run, paths = self.disk_run(tmp)
            st = state_of(run)
            del st["config"]["diskFloorPercent"], st["config"]["diskPaths"]
            (run / "state.json").write_text(json.dumps(st))
            [text] = disk_texts(repo, env)
            found = DISK_TEXT.fullmatch(text)
            self.assertEqual(
                (found["path"], found["low"], found["floor"]),
                (paths["repo"], "floor", "0"),
            )
            self.assertEqual(watched(repo, env), [WATCH_ENDED])
            # The run watches the default paths with no percent floor, and
            # config adds to them.
            flags = ["--disk-path", paths["other"]]
            flags += ["--disk-floor-percent", f"{paths['home']}=25"]
            printed = json.loads(ultraclose("config", *flags, cwd=repo, env=env))
            self.assertEqual(
                printed["diskPaths"], [paths["repo"], paths["home"], paths["other"]]
            )
            self.assertEqual(printed["diskFloorPercent"], {paths["home"]: 25})


class WatchTest(unittest.TestCase):
    """The watch command runs until a file system is low or the run ends."""

    def start(self, tmp, task_state, *flags):
        """Start a run that has no floor and one task in the given state.

        The init command of the run takes the given flags as well.
        """
        repo, env, _ = home_repo(tmp)
        ultraclose(
            "init",
            "--run",
            str(Path(tmp, "run")),
            "--disk-floor-gb",
            "0",
            *flags,
            cwd=repo,
            env=env,
        )
        triage(tmp, repo, env, {"ref": REF})
        ultraclose("set", REF, task_state, cwd=repo, env=env)
        return repo, env

    def exit_after(self, repo, env, *command):
        """Start watch, and run a command when watch has kept running.

        Return the lines that watch prints before it exits.
        """
        with watching(repo, env) as p:
            try:
                with self.assertRaises(subprocess.TimeoutExpired):
                    p.wait(timeout=0.5)
                ultraclose(*command, cwd=repo, env=env)
                out, _ = p.communicate(timeout=60)
            finally:
                p.kill()
        self.assertEqual(p.returncode, 0)
        return out.splitlines()

    def test_exits_when_run_ends(self):
        # Each pair holds a state that keeps the run from its end, and a
        # state that ends the run.
        for unfinished, done in zip(UNFINISHED, itertools.cycle(FINISHED)):
            with self.subTest(unfinished), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start(tmp, unfinished)
                lines = self.exit_after(repo, env, "set", REF, done)
                self.assertEqual(lines, [WATCH_ENDED])

    def test_exits_when_file_system_runs_low(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start(tmp, "inflight")
            # No file system has this much free space.
            flags = ["--disk-floor-gb", "1e12"]
            [line] = self.exit_after(repo, env, "config", *flags)
            found = DISK_TEXT.fullmatch(line.removeprefix("DISK "))
            self.assertTrue(line.startswith("DISK ") and found, line)
            self.assertEqual(found["low"], LOW)

    def test_prints_line_for_each_low_file_system(self):
        # The repository and the other path sit on two file systems, and
        # both run low when config raises the floor.
        with tempfile.TemporaryDirectory() as tmp:
            other = other_file_system(tmp)
            if other is None:
                self.skipTest("no path sits on another file system")
            repo, env = self.start(tmp, "inflight", "--disk-path", other)
            flags = ["--disk-floor-gb", "1e12"]
            lines = self.exit_after(repo, env, "config", *flags)
            found = [DISK_TEXT.fullmatch(line.removeprefix("DISK ")) for line in lines]
            self.assertTrue(
                all(line.startswith("DISK ") for line in lines) and all(found), lines
            )
            self.assertEqual(
                [(f["path"], f["low"]) for f in found],
                [(state_of(Path(tmp, "run"))["config"]["repo"], LOW), (other, LOW)],
            )

    def test_refuses_interval_that_is_not_positive_and_finite(self):
        # A watch command that accepts a negative or an infinite interval
        # fails at its first wait. One that accepts an interval of 0
        # measures without a wait, and the run holds a pending task, so it
        # keeps running until the timeout.
        for flags in (
            ["--interval", "0"],
            ["--interval=-1"],
            ["--interval=inf"],
            ["--interval=nan"],
        ):
            with self.subTest(flags), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start(tmp, "pending")
                message = refused("watch", *flags, cwd=repo, env=env, timeout=60)
                self.assertIn("greater than 0", message)


def unmatched_globs(stderr):
    """The shared globs that the warnings on a standard error name."""
    start, end = "warning: the shared glob ", " matches no file of any task"
    return [
        line.removeprefix(start).removesuffix(end)
        for line in stderr.splitlines()
        if line.startswith(start) and line.endswith(end)
    ]


class SharedGlobTest(unittest.TestCase):
    """A shared glob matches "/" plus the path of a file."""

    def assert_warns(self, stderr, globs):
        """The standard error holds one warning per glob, and then the rule."""
        self.assertEqual(unmatched_globs(stderr), globs)
        if globs:
            self.assertIn(ROOT_GLOB, stderr.splitlines()[-1])
        else:
            self.assertEqual(stderr, "")

    def test_glob_matches_slash_plus_path(self):
        # Two tasks edit one file. They start together when the shared
        # glob matches the file, and triage warns about the glob when it
        # does not.
        refs = ["APP-0001", "APP-0002"]
        # name: (the shared glob, the file, whether the glob matches it)
        cases = {
            "a leading slash and the root file": ("/README.md", "README.md", True),
            "a leading slash and a nested file": (
                "/README.md",
                "docs/README.md",
                False,
            ),
            "a leading star and the root file": ("*/README.md", "README.md", True),
            "a leading star and a nested file": (
                "*/README.md",
                "docs/README.md",
                True,
            ),
            "a bare name and the root file": ("README.md", "README.md", False),
            "a bare path and a nested file": (
                "docs/README.md",
                "docs/README.md",
                False,
            ),
        }
        for name, (glob, file, matched) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = launchable_repo(tmp)
                run = str(Path(tmp, "run"))
                ultraclose("init", "--run", run, "--shared", glob, cwd=repo, env=env)
                stderr = triage(
                    tmp, repo, env, *({"ref": r, "files": [file]} for r in refs)
                )
                self.assert_warns(stderr, [] if matched else [glob])
                started = json.loads(ultraclose("next", cwd=repo, env=env))
                self.assertEqual(
                    [t["task"]["ref"] for t in started], refs if matched else refs[:1]
                )

    def test_triage_warns_about_unmatched_globs(self):
        # name: (the init --shared globs, the files of each task of each
        # triage, the globs that the last triage warns about)
        cases = {
            "no --shared flag": ([], [[["pkg/a.go"]]], []),
            "a default glob as a flag": (["*/tests/*"], [[["pkg/a.go"]]], []),
            "one glob of two matches no file": (
                ["README.md", "*/docs/*"],
                [[["docs/a.md"], ["README.md"]]],
                ["README.md"],
            ),
            "two globs match no file": (
                ["README.md", "docs/*"],
                [[["docs/a.md", "README.md"]]],
                ["README.md", "docs/*"],
            ),
            "a task without a file": (["/README.md"], [[[]]], ["/README.md"]),
            "a file of an earlier triage": (
                ["/README.md"],
                [[["README.md"]], [["pkg/a.go"]]],
                [],
            ),
            "a run without a task": (["README.md"], [[]], []),
        }
        for name, (globs, triages, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                args = ["init", "--run", str(Path(tmp, "run"))]
                for glob in globs:
                    args += ["--shared", glob]
                ultraclose(*args, cwd=repo, env=env)
                n = 0
                for tasks in triages:
                    rows = []
                    for files in tasks:
                        rows.append({"ref": f"APP-000{n}", "files": files})
                        n += 1
                    stderr = triage(tmp, repo, env, *rows)
                self.assert_warns(stderr, want)

    def test_config_warns_about_unmatched_glob_that_it_adds(self):
        # The shared glob of a configured run matches no file of the task.
        # name: (the files of the task, or None for a run without a task,
        # the config flags, the globs that config warns about)
        cases = {
            "--shared adds a glob that matches no file": (
                ["README.md"],
                ["--shared", "README.md"],
                ["README.md"],
            ),
            "--shared adds a glob that matches a file": (
                ["README.md"],
                ["--shared", "/README.md"],
                [],
            ),
            "--shared names a glob of init": (
                ["README.md"],
                ["--shared", GLOB],
                [GLOB],
            ),
            "--unshared removes the glob that --shared names": (
                ["README.md"],
                ["--shared", GLOB, "--unshared", GLOB],
                [],
            ),
            "no --shared flag": (["README.md"], ["--limit", "3"], []),
            "a run without a task": (None, ["--shared", "README.md"], []),
        }
        for name, (files, flags, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                run = Path(tmp, "run")
                configured_run(repo, env, run)
                if files is not None:
                    triage(tmp, repo, env, {"ref": REF, "files": files})
                p = finished("config", *flags, cwd=repo, env=env)
                self.assert_warns(p.stderr, want)
                # The standard output holds only the config.
                self.assertEqual(json.loads(p.stdout), state_of(run)["config"])

    def test_help_states_rule(self):
        for args in (["--help"], ["init", "--help"], ["config", "--help"]):
            with self.subTest(" ".join(args)), tempfile.TemporaryDirectory() as tmp:
                text = ultraclose(*args, cwd=tmp, env=clean_env())
                # The help wraps its lines at the width of the terminal.
                self.assertIn(ROOT_GLOB, " ".join(text.split()))


class DependencyTest(unittest.TestCase):
    """The triage command adds each open aven dependency to an after list."""

    def test_triage_adds_open_dependencies(self):
        # name: (the after list of the row, or None for a row without one,
        # the dependencies that aven reports, the after list of the task)
        cases = {
            "no dependency": (None, {}, []),
            "an open dependency": (
                None,
                {"unresolved": ["APP-0001"]},
                ["APP-0001"],
            ),
            "two open dependencies": (
                None,
                {"unresolved": ["APP-0002", "APP-0001"]},
                ["APP-0002", "APP-0001"],
            ),
            "a resolved dependency": (None, {"resolved": ["APP-0001"]}, []),
            "an open and a resolved dependency": (
                None,
                {"unresolved": ["APP-0001"], "resolved": ["APP-0002"]},
                ["APP-0001"],
            ),
            "a task that waits for the task": (None, {"blocks": ["APP-0001"]}, []),
            "an order that only the row names": (
                ["APP-0003"],
                {"unresolved": ["APP-0001"]},
                ["APP-0003", "APP-0001"],
            ),
            "a dependency that the row also names": (
                ["APP-0001"],
                {"unresolved": ["APP-0001"]},
                ["APP-0001"],
            ),
        }
        for name, (after, reported, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                run = Path(tmp, "run")
                ultraclose("init", "--run", str(run), cwd=repo, env=env)
                aven_dependencies(env, REF, **reported)
                row = {"ref": REF} if after is None else {"ref": REF, "after": after}
                triage(tmp, repo, env, row)
                self.assertEqual(state_of(run)["tasks"][REF]["after"], want)

    def test_later_triage_reads_dependencies_again(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = new_repo(tmp)
            run = Path(tmp, "run")
            ultraclose("init", "--run", str(run), cwd=repo, env=env)
            aven_dependencies(env, REF, unresolved=["APP-0001"])
            triage(tmp, repo, env, {"ref": REF})
            self.assertEqual(state_of(run)["tasks"][REF]["after"], ["APP-0001"])
            # The first dependency is done, and the task gains a second one.
            aven_dependencies(env, REF, unresolved=["APP-0002"], resolved=["APP-0001"])
            triage(tmp, repo, env, {"ref": REF})
            self.assertEqual(state_of(run)["tasks"][REF]["after"], ["APP-0002"])

    def test_next_waits_for_dependency(self):
        # The rows name no order. The first task depends on the second
        # task in aven, and the limit of the run leaves a slot for both.
        first, second = "APP-0001", "APP-0002"
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = launchable_repo(tmp)
            ultraclose("init", "--run", str(Path(tmp, "run")), cwd=repo, env=env)
            aven_dependencies(env, first, unresolved=[second])
            aven_dependencies(env, second, blocks=[first])
            triage(tmp, repo, env, {"ref": first}, {"ref": second})
            started = json.loads(ultraclose("next", cwd=repo, env=env))
            self.assertEqual([t["task"]["ref"] for t in started], [second])
            ultraclose("set", second, "landed", cwd=repo, env=env)
            started = json.loads(ultraclose("next", cwd=repo, env=env))
            self.assertEqual([t["task"]["ref"] for t in started], [first])

    def test_triage_loads_no_row_when_aven_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = new_repo(tmp)
            run = Path(tmp, "run")
            ultraclose("init", "--run", str(run), cwd=repo, env=env)
            triage(tmp, repo, env, {"ref": REF})
            before = state_of(run)
            # The stub fails for the ref of the second row.
            file = Path(tmp, "unknown.json")
            rows = [{"ref": r, "title": "t", "files": []} for r in (REF, "NOPE-0000")]
            file.write_text(json.dumps(rows))
            message = refused("triage", str(file), cwd=repo, env=env)
            self.assertIn("unknown-ref input=NOPE-0000", message)
            self.assertEqual(state_of(run), before)


class UntriagedTest(unittest.TestCase):
    """The untriaged command prints the open aven tasks outside the run."""

    def test_prints_open_tasks_that_run_does_not_hold(self):
        # The run holds one task. The row of a task holds its ref, its
        # title, and its priority.
        new = {"ref": "APP-0002", "title": "A new task", "priority": "high"}
        plain = {"ref": "APP-0003", "title": "t", "priority": "none"}
        # name: (the state of the task of the run, the tasks that aven
        # lists, the rows that the command prints)
        cases = {
            "no open task": ("pending", [], []),
            "a task outside the run": ("pending", [new], [new]),
            "two tasks outside the run": ("pending", [new, plain], [new, plain]),
            "an epic": ("pending", [{**new, "is_epic": True}], []),
            "a deleted task": ("pending", [{**new, "deleted": True}], []),
            "a done task beside an open task": (
                "pending",
                [{**new, "status": "done"}, plain],
                [plain],
            ),
        }
        cases |= {
            f"a task with status {s}": ("pending", [{**new, "status": s}], [new])
            for s in ("inbox", "backlog", "todo", "active")
        }
        cases |= {
            f"a task with status {s}": ("pending", [{**new, "status": s}], [])
            for s in ("done", "canceled")
        }
        cases |= {
            f"the {s} task of the run": (s, [{"ref": REF}, new], [new])
            for s in UNFINISHED + FINISHED
        }
        for name, (task_state, listed_tasks, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                ultraclose("init", "--run", str(Path(tmp, "run")), cwd=repo, env=env)
                triage(tmp, repo, env, {"ref": REF})
                ultraclose("set", REF, task_state, cwd=repo, env=env)
                aven_listed_tasks(env, *listed_tasks)
                rows = json.loads(ultraclose("untriaged", cwd=repo, env=env))
                self.assertEqual(rows, want)

    def test_triage_loads_rows_with_files(self):
        new = {"ref": "APP-0002", "title": "A new task", "priority": "high"}
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = new_repo(tmp)
            run = Path(tmp, "run")
            ultraclose("init", "--run", str(run), cwd=repo, env=env)
            aven_listed_tasks(env, new)
            rows = json.loads(ultraclose("untriaged", cwd=repo, env=env))
            file = Path(tmp, "rows.json")
            file.write_text(json.dumps([{**row, "files": ["a.go"]} for row in rows]))
            ultraclose("triage", str(file), cwd=repo, env=env)
            task = state_of(run)["tasks"][new["ref"]]
            self.assertEqual(
                {k: task[k] for k in ("title", "priority", "files", "state")},
                {
                    "title": new["title"],
                    "priority": new["priority"],
                    "files": ["a.go"],
                    "state": "pending",
                },
            )
            # The run holds the task, so the command prints it no more.
            rows = json.loads(ultraclose("untriaged", cwd=repo, env=env))
            self.assertEqual(rows, [])

    def test_passes_filters_to_aven_list(self):
        # name: (the filters of the command, the filters that aven list
        # gets after --json)
        cases = {
            # The command adds --open beside each filter that aven accepts
            # with it.
            "no filter": ([], ["--open"]),
            "a project and a label": (
                ["--project", "app", "--label", "bug"],
                ["--open", "--project", "app", "--label", "bug"],
            ),
            "a filter with an equals sign": (
                ["--metadata", "review=pending"],
                ["--open", "--metadata", "review=pending"],
            ),
            # The parser knows --help, which starts with the same letters.
            "a flag that starts as a flag of the parser": (
                ["--has-metadata", "review"],
                ["--open", "--has-metadata", "review"],
            ),
            # aven list refuses --open beside each of these filters.
            "a status": (
                ["--project", "app", "--status", "todo"],
                ["--project", "app", "--status", "todo"],
            ),
            "a status with its value attached": (["--status=todo"], ["--status=todo"]),
            "deleted tasks too": (["--all"], ["--all"]),
            "only deleted tasks": (["--deleted"], ["--deleted"]),
            "tasks that start later": (["--upcoming"], ["--upcoming"]),
            "open tasks": (["--project", "app", "--open"], ["--project", "app", "--open"]),
            # The command applies the limit itself.
            "a limit": (
                ["--project", "app", "--limit", "3"],
                ["--open", "--project", "app"],
            ),
            "a limit with its value attached": (
                ["--limit=3", "--project", "app"],
                ["--open", "--project", "app"],
            ),
            "a limit and a status": (
                ["--status", "todo", "--limit", "3"],
                ["--status", "todo"],
            ),
        }
        for name, (filters, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                ultraclose("init", "--run", str(Path(tmp, "run")), cwd=repo, env=env)
                aven_listed_tasks(env)
                ultraclose("untriaged", *filters, cwd=repo, env=env)
                args = Path(env["AVEN_ANSWERS"], "list.args").read_text().splitlines()
                self.assertEqual(args, ["list", "--json", *want])

    def test_limit_counts_printed_rows(self):
        # aven lists the task of the run, an epic, and a done task before
        # the new tasks. None of these tasks counts toward the limit.
        listed = [
            {"ref": REF},
            {"ref": "APP-0002", "is_epic": True},
            {"ref": "APP-0003", "status": "done"},
            {"ref": "APP-0004"},
            {"ref": "APP-0005"},
            {"ref": "APP-0006"},
        ]
        # name: (the filters of the command, the refs that it prints)
        cases = {
            "no limit": ([], ["APP-0004", "APP-0005", "APP-0006"]),
            "a limit": (["--limit", "2"], ["APP-0004", "APP-0005"]),
            "a limit with its value attached": (["--limit=2"], ["APP-0004", "APP-0005"]),
            "a limit beside another filter": (
                ["--project", "app", "--limit", "1"],
                ["APP-0004"],
            ),
            "a limit larger than the list": (
                ["--limit", "9"],
                ["APP-0004", "APP-0005", "APP-0006"],
            ),
        }
        for name, (filters, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                ultraclose("init", "--run", str(Path(tmp, "run")), cwd=repo, env=env)
                triage(tmp, repo, env, {"ref": REF})
                aven_listed_tasks(env, *listed)
                rows = json.loads(ultraclose("untriaged", *filters, cwd=repo, env=env))
                self.assertEqual([row["ref"] for row in rows], want)

    def test_refuses_limit_that_is_not_a_count(self):
        cases = {
            "zero": ["--limit", "0"],
            "zero attached": ["--limit=0"],
            "a negative number": ["--limit", "-1"],
            "a word": ["--limit", "some"],
            "no value": ["--project", "app", "--limit"],
            "another flag": ["--limit", "--project", "app"],
        }
        for name, filters in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                ultraclose("init", "--run", str(Path(tmp, "run")), cwd=repo, env=env)
                aven_listed_tasks(env)
                message = refused("untriaged", *filters, cwd=repo, env=env)
                self.assertIn("--limit takes a whole number of at least 1", message)
                self.assertFalse(Path(env["AVEN_ANSWERS"], "list.args").exists())

    def test_other_commands_refuse_filters(self):
        commands = (
            ["status"],
            ["next"],
            ["triage", "rows.json"],
            ["set", REF, "noted"],
        )
        for command in commands:
            with self.subTest(command[0]), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                run = Path(tmp, "run")
                ultraclose("init", "--run", str(run), cwd=repo, env=env)
                triage(tmp, repo, env, {"ref": REF})
                before = state_of(run)
                message = refused(*command, "--project", "app", cwd=repo, env=env)
                self.assertIn("unrecognized arguments: --project app", message)
                self.assertEqual(state_of(run), before)

    def test_help_names_filters(self):
        with tempfile.TemporaryDirectory() as tmp:
            text = ultraclose("untriaged", "--help", cwd=tmp, env=clean_env())
            # The help wraps its lines at the width of the terminal.
            text = " ".join(text.split())
            self.assertIn("untriaged [-h] [FILTER]...", text)
            self.assertIn("Each FILTER is an argument of aven list", text)


def stale(copy):
    copy.write_text("// an older close-task.js\n")


def hard_linked(copy):
    os.link(copy, copy.with_name("second-link.js"))


def symlinked(copy):
    target = copy.with_name("target.js")
    copy.rename(target)
    copy.symlink_to(target)


def task_copy(run, ref):
    """The copy of close-task.js that the Workflow of a task loads."""
    return Path(run, "tasks", ref, "close-task.js")


def copy_text(ref, title, source=WORKFLOW):
    """The text of the copy of close-task.js that names one task.

    The copy matches the source script, except for two lines of its meta.
    The name line holds the ref, and the description line holds the
    title. With the title None, the copy keeps the description of the
    source.
    """
    lines = source.read_text().splitlines(keepends=True)
    values = {"name": f"close {ref}", "description": title}
    for n, (key, value) in enumerate(values.items(), 1):
        if not lines[n].startswith(f"  {key}: "):
            raise AssertionError(f"line {n + 1} of {source} does not hold the {key}")
        if value is not None:
            lines[n] = f"  {key}: {json.dumps(value)},\n"
    return "".join(lines)


def meta_of(script):
    """The meta that Node reads from the text of a Workflow script.

    Node runs the meta without the rest of the script, so a meta that
    reads the input of the script fails.
    """
    meta, end, _ = script.partition("\n}\n")
    program = meta.replace("export const meta", "const meta", 1) + end
    p = subprocess.run(
        [NODE, "-"],
        input=program + "process.stdout.write(JSON.stringify(meta))\n",
        capture_output=True,
        encoding="utf-8",
        check=True,
    )
    return json.loads(p.stdout)


class WorkflowCopyTest(unittest.TestCase):
    """Each task launches from its own copy of close-task.js.

    The meta of the copy names the task, so the Workflow list shows which
    task each Workflow closes.
    """

    # The title that the triage helper gives a task.
    TITLE = "t"
    # A second task of the run.
    OTHER = "APP-0002"
    # The commands that print the Workflow input of the first task, each
    # with the function that reads the input from the output.
    LAUNCHES = {
        "next": (["next"], lambda out: json.loads(out)[0]),
        "args": (["args", REF], json.loads),
    }
    # Titles that must reach the description of the meta unchanged.
    TITLES = {
        "a plain title": "The list shows one name for every task",
        "quotes": """It's a "quoted" `title`""",
        "backslashes": "C:\\new\\table\\x41 and a last \\",
        "an interpolation": "${args.run} #{x} $(id) \\1 \\g<0>",
        "the end of a comment and of a script": "*/ }; // </script>",
        "several lines": "one\ntwo\r\nthree",
        "line separators": "one\u2028two\u2029three",
        "control characters": "a\x00b\x1bc",
        "characters outside ASCII": "na\u00efve \u65e5\u672c\u8a9e \U0001f600",
    }
    # Titles that hold no text. The Workflow tool refuses an empty
    # description, so each copy keeps the description of close-task.js.
    BLANK = {"an empty title": "", "a title of spaces": "   "}

    def start_run(self, tmp, script=SCRIPT):
        """Start a run with one pending task, and return the repo and the env."""
        repo, env = launchable_repo(tmp)
        run = Path(tmp, "run")
        ultraclose("init", "--run", str(run), cwd=repo, env=env, script=script)
        triage(tmp, repo, env, {"ref": REF})
        return repo, env

    def assert_loadable(self, copy, text):
        """The Workflow tool loads only a regular file with one hard link."""
        self.assertFalse(copy.is_symlink(), "the copy is a symlink")
        self.assertTrue(copy.is_file(), "the copy is not a regular file")
        self.assertEqual(copy.stat().st_nlink, 1)
        self.assertEqual(copy.read_text(), text)

    def installed_skill(self, tmp):
        """Lay the scripts out as home-manager installs them.

        The skill directory holds symlinks into a store, and each store
        file has a second hard link, as Nix store optimisation leaves it.
        """
        store, skill = Path(tmp, "store"), Path(tmp, "skill")
        store.mkdir()
        skill.mkdir()
        for source in (SCRIPT, WORKFLOW):
            stored = store / source.name
            shutil.copyfile(source, stored)
            os.link(stored, store / f"{source.name}.link")
            (skill / source.name).symlink_to(stored)
        return skill / SCRIPT.name

    def titled_copies(self, tmp):
        """Start one task per title of TITLES and BLANK in one run.

        Return the ref and the title of each task and the text of its
        copy, by the name of the title.
        """
        titles = {**self.TITLES, **self.BLANK}
        refs = {name: f"APP-{n:04}" for n, name in enumerate(titles)}
        rows = [{"ref": refs[name], "title": title} for name, title in titles.items()]
        repo, env = launchable_repo(tmp)
        run = Path(tmp, "run")
        limit = str(len(rows))
        ultraclose("init", "--run", str(run), "--limit", limit, cwd=repo, env=env)
        triage(tmp, repo, env, *rows)
        started = json.loads(ultraclose("next", cwd=repo, env=env))
        texts = {t["task"]["ref"]: Path(t["script"]).read_text() for t in started}
        return {
            name: (refs[name], title, texts[refs[name]])
            for name, title in titles.items()
        }

    def test_next_writes_copy_of_each_task(self):
        layouts = {
            "plain files": lambda tmp: SCRIPT,
            "symlinks to hard-linked store files": self.installed_skill,
        }
        for name, layout in layouts.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                script = layout(tmp)
                repo, env = self.start_run(tmp, script)
                triage(tmp, repo, env, {"ref": self.OTHER, "title": "u"})
                run = Path(tmp, "run").resolve()
                copies = {ref: task_copy(run, ref) for ref in (REF, self.OTHER)}
                started = json.loads(
                    ultraclose("next", cwd=repo, env=env, script=script)
                )
                # The script field of each input names the copy of its task.
                self.assertEqual(
                    {t["task"]["ref"]: Path(t["script"]) for t in started}, copies
                )
                self.assert_loadable(copies[REF], copy_text(REF, self.TITLE))
                self.assert_loadable(copies[self.OTHER], copy_text(self.OTHER, "u"))
                # The run holds no other copy of the script.
                self.assertEqual(
                    sorted(run.rglob("close-task.js*")), sorted(copies.values())
                )

    def test_launch_replaces_copy_that_workflow_refuses(self):
        damages = {
            "a missing copy": Path.unlink,
            "a stale copy": stale,
            "a copy with a second hard link": hard_linked,
            "a symlink to a file that holds the text of the copy": symlinked,
        }
        for command, (args, workflow_input) in self.LAUNCHES.items():
            for name, damage in damages.items():
                with (
                    self.subTest(f"{command} replaces {name}"),
                    tempfile.TemporaryDirectory() as tmp,
                ):
                    repo, env = self.start_run(tmp)
                    copy = task_copy(Path(tmp, "run").resolve(), REF)
                    copy.parent.mkdir()
                    copy.write_text(copy_text(REF, self.TITLE))
                    damage(copy)
                    task = workflow_input(ultraclose(*args, cwd=repo, env=env))
                    # SKILL.md names the copy by the script field of the input.
                    self.assertEqual(Path(task["script"]), copy)
                    self.assert_loadable(copy, copy_text(REF, self.TITLE))

    def test_launch_keeps_loadable_copy(self):
        # Workflow refuses a script that a command replaces during the load.
        # name: the commands that start the running task again
        relaunches = {
            "args": [["args", REF]],
            "next": [["set", REF, "noted"], ["set", REF, "pending"], ["next"]],
        }
        for name, commands in relaunches.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, launch = running_task(tmp)
                copy = Path(launch["script"])
                before = copy.stat()
                for command in commands:
                    ultraclose(*command, cwd=repo, env=env)
                after = copy.stat()
                self.assertEqual(
                    (after.st_ino, after.st_mtime_ns),
                    (before.st_ino, before.st_mtime_ns),
                )

    def test_relaunch_follows_script_and_title(self):
        # A later version of the skill changes close-task.js, and a later
        # triage changes the title of the task.
        with tempfile.TemporaryDirectory() as tmp:
            script = self.installed_skill(tmp)
            source = script.with_name("close-task.js")
            repo, env = self.start_run(tmp, script)
            launch = json.loads(ultraclose("next", cwd=repo, env=env, script=script))
            copy = Path(launch[0]["script"])
            self.assert_loadable(copy, copy_text(REF, self.TITLE))
            with source.open("a") as f:
                f.write("// a later line\n")
            triage(tmp, repo, env, {"ref": REF, "title": "a later title"})
            relaunch = json.loads(
                ultraclose("args", REF, cwd=repo, env=env, script=script)
            )
            self.assertEqual(Path(relaunch["script"]), copy)
            self.assertTrue(copy.read_text().endswith("// a later line\n"))
            self.assert_loadable(copy, copy_text(REF, "a later title", source))

    def test_launch_refuses_script_whose_meta_value_spans_lines(self):
        # Each copy differs from close-task.js in one line per value. So
        # the commands refuse a script whose value takes two lines.
        # name: the two lines that replace the line of the value
        splits = {
            "after the key": lambda head, value: f"{head}:\n    {value}",
            "inside the value": lambda head, value: f"{head}: 'a ' +\n    {value}",
        }
        # A task without a title keeps the description of close-task.js,
        # and the commands refuse its script all the same.
        titles = {"a title": self.TITLE, "an empty title": ""}
        # A titled task that the next command picks after the first task.
        later = "APP-9000"
        cases = itertools.product(
            self.LAUNCHES.items(),
            enumerate(("name", "description"), 1),
            splits.items(),
            titles.items(),
        )
        for (command, (args, _)), (n, key), (split, join), (named, title) in cases:
            with (
                self.subTest(f"{command} with a {key} split {split} for {named}"),
                tempfile.TemporaryDirectory() as tmp,
            ):
                script = self.installed_skill(tmp)
                source = script.with_name("close-task.js")
                lines = source.read_text().splitlines(keepends=True)
                head, value = lines[n].split(": ", 1)
                self.assertEqual(head, f"  {key}")
                lines[n] = join(head, value)
                source.write_text("".join(lines))
                repo, env = launchable_repo(tmp)
                run = Path(tmp, "run")
                ultraclose(
                    *("init", "--run", str(run), "--limit", "2"),
                    cwd=repo,
                    env=env,
                    script=script,
                )
                rows = [{"ref": REF, "title": title}, {"ref": later, "title": "u"}]
                triage(tmp, repo, env, *rows)
                message = refused(*args, cwd=repo, env=env, script=script)
                self.assertIn(
                    f"the meta of close-task.js must hold its {key} on one line",
                    message,
                )
                # The command fails before it starts any task.
                tasks = state_of(run)["tasks"]
                self.assertEqual(
                    {ref: t["state"] for ref, t in tasks.items()},
                    {REF: "pending", later: "pending"},
                )
                self.assertEqual(list(Path(repo, ".claude", "worktrees").glob("*")), [])
                self.assertEqual(list(run.rglob("close-task.js*")), [])

    def test_copy_differs_from_script_only_in_name_and_description(self):
        with tempfile.TemporaryDirectory() as tmp:
            for name, (ref, title, text) in self.titled_copies(tmp).items():
                with self.subTest(name):
                    kept = None if name in self.BLANK else title
                    self.assertEqual(text, copy_text(ref, kept))
                    # The Workflow tool refuses a script file that holds a
                    # control character.
                    self.assertNotRegex(text, r"[^\n -~]")

    @unittest.skipUnless(NODE, "node is not on PATH")
    def test_meta_of_copy_names_task(self):
        source = meta_of(WORKFLOW.read_text())
        with tempfile.TemporaryDirectory() as tmp:
            for name, (ref, title, text) in self.titled_copies(tmp).items():
                with self.subTest(name):
                    description = source["description"] if name in self.BLANK else title
                    self.assertEqual(
                        meta_of(text),
                        {**source, "name": f"close {ref}", "description": description},
                    )


class ArgsTest(unittest.TestCase):
    """The args command prints the Workflow input that relaunches a task."""

    def test_resume_carries_stored_summary_and_notes(self):
        both = {"summary": "a summary\n", "notes": "a note\n"}
        kept = {"summary": "a summary", "notes": "a note"}
        # name: (the text of each stored file, the commits of the task
        # branch, the flags, the resume field)
        cases = {
            "both files": (both, 1, [], {"round": 1, "lastRound": 6, **kept}),
            "no file": (
                {},
                1,
                [],
                {"round": 1, "lastRound": 6, "summary": "", "notes": ""},
            ),
            "only the summary": (
                {"summary": "a summary\n"},
                1,
                [],
                {"round": 1, "lastRound": 6, "summary": "a summary", "notes": ""},
            ),
            "an empty notes file": (
                {"summary": "a summary\n", "notes": ""},
                1,
                [],
                {"round": 1, "lastRound": 6, "summary": "a summary", "notes": ""},
            ),
            "text of several paragraphs": (
                {"summary": "one\n\ntwo\n", "notes": "\nthree\nfour\n\n"},
                1,
                [],
                {
                    "round": 1,
                    "lastRound": 6,
                    "summary": "one\n\ntwo",
                    "notes": "three\nfour",
                },
            ),
            "--final": (both, 1, ["--final"], {"round": 1, "lastRound": 1, **kept}),
            "--sync": (
                both,
                1,
                ["--sync", "the merge conflicts"],
                {
                    "round": 1,
                    "lastRound": 2,
                    "sync": "the merge conflicts",
                    **kept,
                },
            ),
            "--sync and a branch without a commit": (
                both,
                0,
                ["--sync", "the merge conflicts"],
                {
                    "round": 1,
                    "lastRound": 2,
                    "sync": "the merge conflicts",
                    **kept,
                },
            ),
            # The task starts again at the implementer. StoredTextTest
            # covers the files that this launch leaves.
            "a branch without a commit": (both, 0, [], None),
        }
        for name, (files, commits, flags, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, launch = running_task(tmp, commits)
                for key, text in files.items():
                    Path(launch["task"]["dir"], STORED_FILES[key]).write_text(text)
                relaunch = json.loads(
                    ultraclose("args", REF, *flags, cwd=repo, env=env)
                )
                self.assertEqual(relaunch["task"].get("resume"), want)
                # The rest of the input matches the first launch.
                relaunch["task"].pop("resume", None)
                self.assertEqual(relaunch, launch)


class StoredTextTest(unittest.TestCase):
    """A launch at the implementer starts with no stored summary or notes.

    The text of an earlier attempt describes work that the new implementer
    replaces. An implementer can commit and stop before it stores its own
    text, and the relaunch after it must not carry the earlier text.
    """

    def test_launch_at_implementer_removes_stored_text(self):
        earlier = {"summary": "an earlier attempt", "notes": "its caveat"}
        # name: (the commits of the task branch, the commands that launch
        # the task again, whether that launch starts at the implementer)
        cases = {
            "args on a branch without a commit": (0, [["args", REF]], True),
            "next after the task returns to pending": (
                1,
                [["set", REF, "noted"], ["set", REF, "pending"], ["next"]],
                True,
            ),
            "args on a branch with a commit": (1, [["args", REF]], False),
            "--sync on a branch without a commit": (
                0,
                [["args", REF, "--sync", "the merge conflicts"]],
                False,
            ),
        }
        for name, (commits, commands, fresh) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, launch = running_task(tmp, commits)
                d = Path(launch["task"]["dir"])
                for key, text in earlier.items():
                    (d / STORED_FILES[key]).write_text(text + "\n")
                for command in commands:
                    ultraclose(*command, cwd=repo, env=env)
                self.assertEqual(
                    {key: (d / file).exists() for key, file in STORED_FILES.items()},
                    dict.fromkeys(STORED_FILES, not fresh),
                )
                # The agent of that launch commits and stops before it
                # stores its text. The next relaunch resumes at a review
                # round.
                empty_commit(launch["task"]["wt"], env, "wip of the launch")
                relaunch = json.loads(ultraclose("args", REF, cwd=repo, env=env))
                resume = relaunch["task"]["resume"]
                self.assertEqual(
                    {key: resume[key] for key in STORED_FILES},
                    dict.fromkeys(STORED_FILES, "") if fresh else earlier,
                )


# The branch of the task, and the name that keeps the branch of an earlier
# run as a reference.
BRANCH = f"task/{REF}"
KEPT = f"kept/{REF}"
# The word that stands for the path of the task worktree in a test case.
WT = "WORKTREE"
# A second task, which no run has started.
OTHER = "APP-0001"
# What the refusal says about a run and the work of an earlier run.
OWN_WORK = "a run resumes only its own work"
# The command that the refusal names for the uncommitted changes of a
# worktree, so that the removal of the worktree keeps them.
COMMIT = "git commit --no-verify"
# The command that the refusal names to count the commits of a branch
# again after that commit.
RECOUNT = "git rev-list --count main..<branch>"
# The sentence of the refusal that names the command to run after the
# cleanup, with the words of that command.
RETRY = "Then run ultraclose.py {}."
# The line that set prints for the worktree of an earlier run, with the
# path of that worktree.
NOT_CREATED = "kept worktree {}, which this run did not create"


def noted(tmp, repo, env, wt):
    """The first run notes its task, which removes the clean worktree."""
    ultraclose("set", REF, "noted", cwd=repo, env=env)


def dirty(tmp, repo, env, wt):
    """The first run notes its task, and the worktree keeps a new file."""
    Path(wt, "work.txt").write_text("work\n")
    noted(tmp, repo, env, wt)


def lost(tmp, repo, env, wt):
    """The first run stops with its task running, and its directory is lost."""
    shutil.rmtree(Path(tmp, "run"))


def dirty_lost(tmp, repo, env, wt):
    """The first run stops before its task commits, and its directory is lost."""
    Path(wt, "work.txt").write_text("work\n")
    lost(tmp, repo, env, wt)


def renamed(tmp, repo, env, wt):
    """The first run is lost, and its task branch then takes another name."""
    lost(tmp, repo, env, wt)
    git("branch", "-m", BRANCH, KEPT, cwd=repo, env=env)


class LeftoverTest(unittest.TestCase):
    """A run resumes only the branches and the worktrees that it created.

    The set command also removes only those branches and those worktrees.
    """

    def ahead(self, repo, env, name):
        """The count of the commits of a branch that main lacks."""
        return int(git("rev-list", "--count", f"main..{name}", cwd=repo, env=env))

    def second_run(self, tmp, commits=1, end=noted, rows=({"ref": REF},)):
        """Start a second run after a first run left work for its task.

        The first run starts the task, and the task branch gets the given
        number of commits. The end argument ends the first run. The
        second run holds one task per row. Return the repository, the
        environment, the directory of the second run, and the path of the
        task worktree.
        """
        repo, env, launch = running_task(tmp, commits)
        wt = launch["task"]["wt"]
        end(tmp, repo, env, wt)
        second = Path(tmp, "second")
        ultraclose("init", "--run", str(second), cwd=repo, env=env)
        triage(tmp, repo, env, *rows)
        return repo, env, second, wt

    def test_refuses_work_of_earlier_run(self):
        # name: (the commits of the task branch, what ends the first run,
        # the line that names the leftover work, the repair, the commits
        # of the kept branch). The repair holds the git commands that
        # move the work out of the way. WT stands for the path of the
        # task worktree. The commits of the kept branch are None when no
        # kept branch remains.
        cases = {
            "the branch of a noted task": (
                1,
                noted,
                f"{REF}: the branch {BRANCH}, 1 commit ahead of main",
                [["branch", "-m", BRANCH, KEPT]],
                1,
            ),
            "a branch without a commit": (
                0,
                noted,
                f"{REF}: the branch {BRANCH}, 0 commits ahead of main",
                [["branch", "-D", BRANCH]],
                None,
            ),
            "the dirty worktree of a noted task": (
                1,
                dirty,
                f"{REF}: the branch {BRANCH}, 1 commit ahead of main,"
                f" and the worktree {WT}",
                [
                    ["-C", WT, "add", "-A"],
                    [*IDENTITY, "-C", WT, "commit", "-q", "--no-verify", "-m", "wip"],
                    ["worktree", "remove", WT],
                    ["branch", "-m", BRANCH, KEPT],
                ],
                2,
            ),
            # The refusal counts 0 commits, and the commit of the
            # uncommitted changes puts the branch 1 commit ahead. So the
            # repair renames the branch after it counts again.
            "the dirty worktree of a lost task without a commit": (
                0,
                dirty_lost,
                f"{REF}: the branch {BRANCH}, 0 commits ahead of main,"
                f" and the worktree {WT}",
                [
                    ["-C", WT, "add", "-A"],
                    [*IDENTITY, "-C", WT, "commit", "-q", "--no-verify", "-m", "wip"],
                    ["worktree", "remove", WT],
                    ["branch", "-m", BRANCH, KEPT],
                ],
                1,
            ),
            "the branch and the worktree of a lost run": (
                2,
                lost,
                f"{REF}: the branch {BRANCH}, 2 commits ahead of main,"
                f" and the worktree {WT}",
                [["worktree", "remove", WT], ["branch", "-m", BRANCH, KEPT]],
                2,
            ),
            "the worktree of a branch with another name": (
                2,
                renamed,
                f"{REF}: the worktree {WT}",
                [["worktree", "remove", WT]],
                2,
            ),
        }
        for name, (commits, end, line, repair, kept) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, second, wt = self.second_run(tmp, commits, end)
                before = state_of(second)
                # A refused command does not record the task as started,
                # so each later command refuses the task again. Each
                # refusal names the command that it refused.
                commands = (
                    (["next"], "next"),
                    (["args", REF], f"args {REF} again"),
                    (["next"], "next"),
                )
                for command, retry in commands:
                    message = refused(*command, cwd=repo, env=env)
                    self.assertIn(line.replace(WT, wt), message.splitlines())
                    self.assertIn(OWN_WORK, message)
                    self.assertIn(COMMIT, message)
                    self.assertIn(RECOUNT, message)
                    self.assertIn(RETRY.format(retry), message)
                self.assertEqual(state_of(second), before)
                for command in repair:
                    args = [wt if arg == WT else arg for arg in command]
                    git(*args, cwd=repo, env=env)
                # The task starts on a new branch from the base branch.
                launch = json.loads(ultraclose("next", cwd=repo, env=env))[0]
                self.assertEqual(launch["task"]["ref"], REF)
                self.assertEqual(self.ahead(repo, env, BRANCH), 0)
                self.assertEqual(
                    git("branch", "--show-current", cwd=wt, env=env), BRANCH
                )
                if kept is not None:
                    self.assertEqual(self.ahead(repo, env, KEPT), kept)

    def test_next_reports_work_for_waiting_task(self):
        # The task with the leftover work waits for the other task. The
        # first next reports the work, and it starts no task.
        with tempfile.TemporaryDirectory() as tmp:
            rows = ({"ref": OTHER}, {"ref": REF, "after": [OTHER]})
            repo, env, second, _ = self.second_run(tmp, rows=rows)
            before = state_of(second)
            message = refused("next", cwd=repo, env=env)
            self.assertEqual(
                [line for line in message.splitlines() if line.startswith("APP-")],
                [f"{REF}: the branch {BRANCH}, 1 commit ahead of main"],
            )
            self.assertEqual(state_of(second), before)
            self.assertEqual(
                git("branch", "--list", f"task/{OTHER}", cwd=repo, env=env), ""
            )
            # Only a pending task starts, so next ignores the leftover
            # work of a task in another state.
            ultraclose("set", REF, "noted", cwd=repo, env=env)
            started = json.loads(ultraclose("next", cwd=repo, env=env))
            self.assertEqual([t["task"]["ref"] for t in started], [OTHER])
            self.assertEqual(self.ahead(repo, env, BRANCH), 1)

    def test_args_checks_only_its_task(self):
        with tempfile.TemporaryDirectory() as tmp:
            rows = ({"ref": OTHER}, {"ref": REF})
            repo, env, _, _ = self.second_run(tmp, rows=rows)
            launch = json.loads(ultraclose("args", OTHER, cwd=repo, env=env))
            self.assertEqual(launch["task"]["ref"], OTHER)
            self.assertIn(OWN_WORK, refused("args", REF, cwd=repo, env=env))

    def test_run_resumes_its_own_work(self):
        # name: the commands that launch the task of the run again
        cases = {
            "next after a noted task returns to pending": [
                ["set", REF, "noted"],
                ["set", REF, "pending"],
                ["next"],
            ],
            "args of a noted task": [["set", REF, "noted"], ["args", REF]],
            "args of a running task": [["args", REF]],
        }
        for name, commands in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, launch = running_task(tmp, 2)
                for command in commands:
                    ultraclose(*command, cwd=repo, env=env)
                # The task keeps the commits of its branch.
                self.assertEqual(self.ahead(repo, env, BRANCH), 2)
                wt = launch["task"]["wt"]
                self.assertEqual(
                    git("branch", "--show-current", cwd=wt, env=env), BRANCH
                )
                state = state_of(Path(tmp, "run"))["tasks"][REF]["state"]
                self.assertEqual(state, "inflight")

    def test_next_starts_task_again_after_failed_start(self):
        # The first next creates the worktree, and then aven fails. The
        # command ends before it saves the state, so the state does not
        # show that the run created the worktree.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = launchable_repo(tmp)
            run = Path(tmp, "run")
            ultraclose("init", "--run", str(run), cwd=repo, env=env)
            triage(tmp, repo, env, {"ref": REF})
            failing = Path(env["AVEN_ANSWERS"], "edit.fails")
            failing.touch()
            self.assertIn("aven edit", refused("next", cwd=repo, env=env))
            self.assertEqual(state_of(run)["tasks"][REF]["state"], "pending")
            self.assertEqual(self.ahead(repo, env, BRANCH), 0)
            failing.unlink()
            launch = json.loads(ultraclose("next", cwd=repo, env=env))[0]
            self.assertEqual(launch["task"]["ref"], REF)
            self.assertTrue(Path(launch["task"]["wt"]).is_dir())

    def test_next_resumes_branch_of_failed_worktree_add(self):
        # git worktree add creates the task branch, and then it fails to
        # create the worktree. The run created that branch, so the next
        # call starts the task on it.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = launchable_repo(tmp)
            run = Path(tmp, "run")
            ultraclose("init", "--run", str(run), cwd=repo, env=env)
            triage(tmp, repo, env, {"ref": REF})
            # A file in place of the worktrees directory makes git fail.
            blocker = Path(repo, ".claude", "worktrees")
            blocker.parent.mkdir()
            blocker.touch()
            self.assertIn("git worktree add", refused("next", cwd=repo, env=env))
            self.assertEqual(self.ahead(repo, env, BRANCH), 0)
            blocker.unlink()
            launch = json.loads(ultraclose("next", cwd=repo, env=env))[0]
            self.assertEqual(launch["task"]["ref"], REF)
            self.assertEqual(
                git("branch", "--show-current", cwd=launch["task"]["wt"], env=env),
                BRANCH,
            )

    def test_set_keeps_work_of_earlier_run(self):
        # The second run does not start the task. A lost run leaves a
        # clean worktree, and a run that notes the task leaves only the
        # branch.
        # name: (what ends the first run, the state that the second run
        # sets, whether the first run leaves the worktree)
        cases = {
            "noted, and the worktree of a lost run": (lost, "noted", True),
            "closed, and the worktree of a lost run": (lost, "closed", True),
            "closed, and the branch of a noted task": (noted, "closed", False),
        }
        for name, (end, task_state, has_worktree) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, second, wt = self.second_run(tmp, 2, end)
                printed = ultraclose("set", REF, task_state, cwd=repo, env=env)
                self.assertEqual(Path(wt).is_dir(), has_worktree)
                self.assertEqual(self.ahead(repo, env, BRANCH), 2)
                kept = []
                if has_worktree:
                    self.assertEqual(
                        git("branch", "--show-current", cwd=wt, env=env), BRANCH
                    )
                    kept = [NOT_CREATED.format(wt)]
                self.assertEqual(printed.splitlines(), [*kept, f"{REF} {task_state}"])
                self.assertEqual(state_of(second)["tasks"][REF]["state"], task_state)
                # The command does not record the work as the work of the
                # run, so the run still refuses to resume it.
                ultraclose("set", REF, "pending", cwd=repo, env=env)
                self.assertIn(OWN_WORK, refused("next", cwd=repo, env=env))

    def test_set_removes_work_of_its_run(self):
        # name: (the commands that end the task of the run, the state that
        # the last command sets, whether the branch remains)
        cases = {
            "a noted task": ([["set", REF, "noted"]], "noted", True),
            "a closed task": ([["set", REF, "closed"]], "closed", False),
            # The worktree of the task is gone, so the command keeps the
            # branch.
            "a noted task that the run closes": (
                [["set", REF, "noted"], ["set", REF, "closed"]],
                "closed",
                True,
            ),
            "a noted task that the run starts again and closes": (
                [["set", REF, "noted"], ["args", REF], ["set", REF, "closed"]],
                "closed",
                False,
            ),
        }
        for name, (commands, task_state, keeps_branch) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, launch = running_task(tmp, 2)
                for command in commands:
                    printed = ultraclose(*command, cwd=repo, env=env)
                self.assertEqual(printed.splitlines(), [f"{REF} {task_state}"])
                self.assertFalse(Path(launch["task"]["wt"]).exists())
                branches = git("branch", "--list", BRANCH, cwd=repo, env=env)
                self.assertEqual(bool(branches), keeps_branch)

    def test_set_keeps_dirty_worktree_of_its_run(self):
        for task_state in ("noted", "closed"):
            with self.subTest(task_state), tempfile.TemporaryDirectory() as tmp:
                repo, env, launch = running_task(tmp, 2)
                wt = launch["task"]["wt"]
                Path(wt, "dirty.txt").write_text("dirty\n")
                printed = ultraclose("set", REF, task_state, cwd=repo, env=env)
                self.assertEqual(
                    printed.splitlines(),
                    [f"kept dirty worktree {wt}", f"{REF} {task_state}"],
                )
                self.assertTrue(Path(wt, "dirty.txt").is_file())
                self.assertEqual(self.ahead(repo, env, BRANCH), 2)

    def test_init_leaves_directory_that_holds_tasks(self):
        # The first run loses its state file, and its directory keeps its
        # tasks directory.
        # name: whether the first run started its task
        cases = {"the directory of a started task": True, "no task directory": False}
        for name, started in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = launchable_repo(tmp)
                first = Path(tmp, "first").resolve()
                ultraclose("init", "--run", str(first), cwd=repo, env=env)
                triage(tmp, repo, env, {"ref": REF})
                if started:
                    ultraclose("next", cwd=repo, env=env)
                (first / "state.json").unlink()
                out = ultraclose("init", "--run", str(first), cwd=repo, env=env)
                # The new run takes the next free directory, so it does
                # not treat the work of the first run as its own.
                second = first.with_name("first-2")
                self.assertIn(f"run={second} ", out)
                self.assertFalse((first / "state.json").exists())
                self.assertEqual(state_of(second)["tasks"], {})
                triage(tmp, repo, env, {"ref": REF})
                if started:
                    self.assertIn(OWN_WORK, refused("next", cwd=repo, env=env))
                else:
                    launched = json.loads(ultraclose("next", cwd=repo, env=env))
                    self.assertEqual([t["task"]["ref"] for t in launched], [REF])

    def test_run_after_lost_state_file(self):
        # The first run loses its state file while its task runs, and its
        # directory keeps the directory of the task.
        # name: (the directory of the second run, the flags of its init,
        # whether the second run resumes the work of the first run)
        cases = {
            "a new directory": ("second", [], False),
            "the directory of the first run with --force": ("first", ["--force"], True),
        }
        for name, (directory, flags, resumes) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, launch = running_task(tmp, 2)
                runs = {"first": Path(tmp, "run"), "second": Path(tmp, "second")}
                (runs["first"] / "state.json").unlink()
                run = runs[directory].resolve()
                ultraclose("init", "--run", str(run), *flags, cwd=repo, env=env)
                self.assertEqual(state_of(run)["tasks"], {})
                triage(tmp, repo, env, {"ref": REF})
                if resumes:
                    # The run treats the directory of the task as its own,
                    # so it resumes the branch and the worktree.
                    started = json.loads(ultraclose("next", cwd=repo, env=env))
                    self.assertEqual([t["task"]["ref"] for t in started], [REF])
                    self.assertEqual(started[0]["task"]["wt"], launch["task"]["wt"])
                else:
                    self.assertIn(OWN_WORK, refused("next", cwd=repo, env=env))
                self.assertEqual(self.ahead(repo, env, BRANCH), 2)


# A program that stands in for lefthook. It runs a process in a process
# group of its own, and it ends that process only after SIGINT. The
# process file of the environment records the ID of the process.
MANAGER = """\
import os
import signal
import subprocess
import sys

child = subprocess.Popen(["sleep", "30"], start_new_session=True)


def stop(signum, frame):
    child.kill()
    sys.exit(1)


signal.signal(signal.SIGINT, stop)
with open(os.environ["PROCESS"], "w") as f:
    f.write(f"{child.pid}\\n")
child.wait()
"""


# A shell script that hangs, and whose processes ignore SIGINT. After
# SIGTERM, its first process creates the stopped file of the environment
# and ends. That process waits for a second process, which ignores
# SIGTERM as well. The process file of the environment records the ID of
# the second process.
STUBBORN = """\
trap "" INT
trap 'touch "$STOPPED"; exit 1' TERM
(trap "" TERM; exec sleep 30) &
echo $! >"$PROCESS"
wait
"""


# A program that hangs. It keeps running after SIGINT, and it records
# each SIGINT that it gets in the interrupted file of the environment.
# The process file of the environment records the ID of its process.
OUTLASTS_SIGINT = """\
import os
import signal
import time


def interrupted(signum, frame):
    with open(os.environ["INTERRUPTED"], "a") as f:
        f.write("SIGINT\\n")


signal.signal(signal.SIGINT, interrupted)
with open(os.environ["PROCESS"], "w") as f:
    f.write(f"{os.getpid()}\\n")
time.sleep(30)
"""


def gone(process):
    """Whether a process ends within 10 seconds.

    A process that ended keeps its ID until its parent reads its exit
    status, so the function waits for the ID to leave.
    """
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        try:
            os.kill(process, 0)
        except ProcessLookupError:
            return True
        time.sleep(0.05)
    return False


class LandCase(unittest.TestCase):
    """The base of each test that lands a task."""

    SUBJECT = "feat: add a file"
    # The file that the task of start_task changes.
    FILE = "a.txt"
    # A second task, which is pending when the landing starts.
    OTHER = "APP-0002"
    # The engine file stands in for a service that the check and the
    # commit hook require. The environment of the test names the file.
    HEALTH = 'test -e "$ENGINE" || { echo no engine; exit 1; }'
    # A step that stops the service, prints the cause, and fails.
    CAUSE = "the step killed the engine"
    STOP = f'rm "$ENGINE"; echo "{CAUSE}"; exit 1'
    # A step that passes and records that it ran.
    RECORD = 'touch "$RAN"'
    # A step that passes, and whose first run stops the service.
    STOP_ONCE = 'test -e "$RAN" || { touch "$RAN"; rm "$ENGINE"; }'
    # A step that passes and records the golangci-lint cache that it got.
    # The format argument names the file of the record.
    SEEN_CACHE = 'printf %s "$GOLANGCI_LINT_CACHE" >"$SEEN.{}"'
    # A step that passes and adds an entry to its golangci-lint cache, as
    # golangci-lint does.
    FILL_CACHE = 'mkdir -p "$GOLANGCI_LINT_CACHE" && touch "$GOLANGCI_LINT_CACHE/entry"'
    # What a step that hangs prints before it hangs.
    WAITS = "the step waits for the engine"
    # A step that hangs. It prints a line and waits for a process that
    # it starts. The process file of the environment records the ID of
    # that process.
    HANG = f'echo "{WAITS}"; sh -c \'echo $$ >"$PROCESS"; exec sleep 30\''
    # A step whose first run hangs, and whose later runs pass.
    HANG_ONCE = f'test -e "$RAN" || {{ touch "$RAN"; {HANG}; }}'

    def start_task(self, tmp, *flags, hook="exit 0", checkout=None):
        """Start a run with one task that is ready to land.

        The flags go to init. The task branch holds one commit. The
        pre-commit hook of the repository runs the hook argument, and
        only the landing commits with hooks on. Return the repository
        and the environment.

        The main checkout has main checked out. The checkout argument
        names another branch, which the main checkout creates and checks
        out before the run starts. That branch holds one commit that main
        lacks.
        """
        repo, run = Path(tmp, "repo"), Path(tmp, "run")
        # The landing commits with the git config of its caller. This
        # file replaces the config of the user, so the hook path and the
        # signing key of the user do not reach the test repository.
        config = Path(tmp, "gitconfig")
        config.write_text("[user]\n\tname = test\n\temail = test@example.com\n")
        env = clean_env()
        stub_aven(tmp, env)
        env["GIT_CONFIG_GLOBAL"] = str(config)
        env["GIT_CONFIG_NOSYSTEM"] = "1"
        env["ENGINE"] = str(Path(tmp, "engine"))
        env["RAN"] = str(Path(tmp, "ran"))
        env["PROCESS"] = str(Path(tmp, "process"))
        git("init", "-q", "-b", "main", str(repo), cwd=tmp, env=env)
        git("commit", "-q", "--allow-empty", "-m", "init", cwd=repo, env=env)
        if checkout:
            git("switch", "-q", "-c", checkout, cwd=repo, env=env)
            git("commit", "-q", "--allow-empty", "-m", "branch", cwd=repo, env=env)
        ultraclose("init", "--run", str(run), *flags, cwd=repo, env=env)
        triage(tmp, repo, env, {"ref": REF})
        task = json.loads(ultraclose("next", cwd=repo, env=env))[0]["task"]
        Path(task["wt"], self.FILE).write_text("a\n")
        git("add", "-A", cwd=task["wt"], env=env)
        git("commit", "-q", "--no-verify", "-m", "wip", cwd=task["wt"], env=env)
        Path(task["dir"], "msg.txt").write_text(self.SUBJECT + "\n")
        hooks = Path(repo, ".git", "hooks")
        hooks.mkdir(exist_ok=True)
        (hooks / "pre-commit").write_text(f"#!/bin/sh\n{hook}\n")
        (hooks / "pre-commit").chmod(0o755)
        return repo, env

    def land(self, repo, env, *flags, stderr=subprocess.PIPE, timeout=None):
        """Land the task. Return the exit status and the printed lines.

        The flags go to the land command. The lines come from the standard
        output. Pass subprocess.STDOUT as stderr to add the lines of the
        standard error in the order that the command writes both. A
        timeout in seconds fails the test when the landing keeps running,
        such as a landing that does not stop a step that hangs.
        """
        p = subprocess.run(
            [sys.executable, str(SCRIPT), "land", REF, *flags],
            cwd=repo,
            env=env,
            check=False,
            stdout=subprocess.PIPE,
            stderr=stderr,
            text=True,
            timeout=timeout,
        )
        return p.returncode, p.stdout.splitlines()

    def hung_process(self, env):
        """The ID of the process that a step that hangs waits for.

        Wait until the step records the process.
        """
        record = Path(env["PROCESS"])
        deadline = time.monotonic() + 60
        while not (record.exists() and record.read_text().endswith("\n")):
            self.assertLess(time.monotonic(), deadline, "the step did not start")
            time.sleep(0.05)
        return int(record.read_text())

    def assert_ended(self, process):
        """The process that a step waited for ended with the step.

        The test kills a process that still runs, so that the process
        does not outlive the test.
        """
        if not gone(process):
            os.kill(process, signal.SIGKILL)
            self.fail(f"the process {process} of the step still runs")

    def add_other(self, tmp, repo, env, **row):
        """Add the second task to the run, with the fields of the row.

        The task of start_task takes the file that it changes, so the
        second task can wait for that file.
        """
        triage(
            tmp,
            repo,
            env,
            {"ref": REF, "files": [self.FILE]},
            {"ref": self.OTHER, **row},
        )

    def task_states(self, tmp):
        """The state of each task of the run, by ref."""
        tasks = state_of(Path(tmp, "run"))["tasks"]
        return {ref: t["state"] for ref, t in tasks.items()}

    def next_refs(self, repo, env):
        """Run the next command, and return the refs of the tasks that start."""
        started = json.loads(ultraclose("next", cwd=repo, env=env))
        return [t["task"]["ref"] for t in started]

    def assert_failed(self, repo, env, landing, result):
        """The landing printed the result last and left the base branch alone."""
        status, lines = landing
        self.assertEqual(status, 2, lines)
        self.assertEqual(lines[-1], f"RESULT {REF} {result}")
        self.assertEqual(git("log", "--format=%s", cwd=repo, env=env), "init")
        self.assertEqual(git("status", "--porcelain", cwd=repo, env=env), "")

    def assert_landed(self, repo, env, landing):
        status, lines = landing
        self.assertEqual(status, 0, lines)
        self.assertTrue(lines[-1].startswith(f"RESULT {REF} OK "), lines)
        self.assertEqual(
            git("log", "-1", "--format=%s", cwd=repo, env=env), self.SUBJECT
        )

    def after_result(self, lines):
        """The lines that follow the RESULT line of a landing with the result OK.

        The landing prints its TIMES line right before the RESULT line, so
        the RESULT line is the second line.
        """
        self.assertRegex(lines[0], r"^TIMES ", lines)
        self.assertTrue(lines[1].startswith(f"RESULT {REF} OK "), lines)
        return lines[2:]


class LandTest(LandCase):
    """What a landing prints, and how it tells a down service from a fault.

    A failed landing prints its result last. A landing with --next and the
    result OK then starts the next tasks. The check and the commit hooks
    of a landing do not read the golangci-lint cache that every checkout
    shares. A landing also records how long its check and its commit took.
    It stops a check or a commit that runs longer than the step timeout.
    """

    def test_result_line_comes_last(self):
        # name: (the init flags, the hook, the result, a line of the failing output)
        cases = {
            "a failed check": (
                ["--check", "echo 'test: FAIL'; exit 1"],
                "exit 0",
                "CHECKFAIL",
                "test: FAIL",
            ),
            "a failed commit hook": (
                [],
                "echo 'lint: FAIL'; exit 1",
                "COMMITFAIL",
                "lint: FAIL",
            ),
        }
        for name, (flags, hook, result, output) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(tmp, *flags, hook=hook)
                landing = self.land(repo, env)
                self.assert_failed(repo, env, landing, result)
                self.assertIn(output, landing[1][:-1])

    def test_lint_cache(self):
        # golangci-lint 2.12 reports the cached issues of another checkout
        # of the same module. So no step of a landing reads the cache that
        # every checkout shares.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(
                tmp,
                "--check",
                f"{self.SEEN_CACHE.format('check')} && {self.FILL_CACHE}",
                hook=self.SEEN_CACHE.format("hook"),
            )
            shared = Path(tmp, "shared")
            env["GOLANGCI_LINT_CACHE"] = str(shared)
            env["SEEN"] = str(Path(tmp, "seen"))
            wt = Path(repo, ".claude", "worktrees", f"t-{REF}")
            git_dir = git("rev-parse", "--absolute-git-dir", cwd=wt, env=env)
            # The check filled its cache, and the worktree stayed clean.
            self.assert_landed(repo, env, self.land(repo, env))
            seen = {
                step: Path(tmp, f"seen.{step}").read_text()
                for step in ("check", "hook")
            }
            # The cache of the check sits in the git directory of the
            # worktree. The commit hooks run in the main checkout, and
            # their cache sits in the run directory.
            self.assertEqual(
                seen,
                {
                    "check": str(Path(git_dir, LINT_CACHE)),
                    "hook": str(Path(tmp, "run").resolve() / LINT_CACHE),
                },
            )
            self.assertFalse(shared.exists())
            # git removed the cache of the check together with the worktree.
            self.assertFalse(Path(seen["check"]).exists())

    def test_next_starts_tasks_after_landing(self):
        # The second task cannot start while the first task runs.
        # name: (the init flags, the fields of the second task, or None
        # for a run without a second task)
        cases = {
            "a task that waits for the slot": (["--limit", "1"], {}),
            "a task that waits for a file of the landed task": (
                [],
                {"files": [self.FILE]},
            ),
            "a task that waits for the landed task": ([], {"after": [REF]}),
            "no pending task": ([], None),
        }
        for name, (flags, row) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(tmp, *flags)
                others = []
                if row is not None:
                    self.add_other(tmp, repo, env, **row)
                    others = [self.OTHER]
                self.assertEqual(self.next_refs(repo, env), [])
                status, lines = self.land(repo, env, "--next")
                self.assertEqual(status, 0, lines)
                listed = self.after_result(lines)
                self.assertEqual(
                    git("log", "-1", "--format=%s", cwd=repo, env=env),
                    self.SUBJECT,
                )
                # The list of Workflow inputs follows the RESULT line.
                started = json.loads("\n".join(listed))
                self.assertEqual([t["task"]["ref"] for t in started], others)
                self.assertEqual(
                    self.task_states(tmp),
                    {REF: "landed", **dict.fromkeys(others, "inflight")},
                )
                # Each input names a task that is ready for its Workflow,
                # as an input of the next command does.
                run = Path(tmp, "run").resolve()
                for t in started:
                    self.assertTrue(Path(t["task"]["wt"], ".git").exists())
                    self.assertTrue(Path(t["task"]["dir"], "task.md").exists())
                    self.assertEqual(Path(t["run"]), run)
                    # The landing writes the copy of close-task.js that the
                    # Workflow of the task loads.
                    copy = Path(t["script"])
                    self.assertEqual(copy, task_copy(run, self.OTHER))
                    self.assertEqual(copy.read_text(), copy_text(self.OTHER, "t"))

    def test_next_after_landing_names_models(self):
        # The input of a task that a landing starts names the models of
        # the run, as an input of the next command does.
        with tempfile.TemporaryDirectory() as tmp:
            flags = model_flags({"implementer": FABLE, DEFAULT_ROLE: OPUS})
            repo, env = self.start_task(tmp, *flags)
            self.add_other(tmp, repo, env, after=[REF])
            status, lines = self.land(repo, env, "--next")
            self.assertEqual(status, 0, lines)
            started = json.loads("\n".join(self.after_result(lines)))
            self.assertEqual([t["task"]["ref"] for t in started], [self.OTHER])
            self.assertEqual(
                started[0]["models"],
                {**dict.fromkeys(ROLES, OPUS), "implementer": FABLE},
            )

    def test_landing_starts_no_task(self):
        # A next command can start the second task after each landing.
        # name: (the check, the land flags, the result)
        cases = {
            "a landing without --next": ("exit 0", [], "OK"),
            "a failed landing with --next": ("exit 1", ["--next"], "CHECKFAIL"),
        }
        for name, (check, flags, result) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(tmp, "--check", check)
                self.add_other(tmp, repo, env)
                landing = self.land(repo, env, *flags)
                if result == "OK":
                    self.assert_landed(repo, env, landing)
                else:
                    self.assert_failed(repo, env, landing, result)
                self.assertEqual(self.task_states(tmp)[self.OTHER], "pending")
                self.assertEqual(self.next_refs(repo, env), [self.OTHER])

    def test_failed_next_keeps_result_of_landing(self):
        # The landing passes. Then the second task cannot start, because
        # aven does not print its task file.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp)
            self.add_other(tmp, repo, env)
            Path(tmp, "bin", "aven").write_text('#!/bin/sh\ntest "$1" != context\n')
            status, lines = self.land(repo, env, "--next", stderr=subprocess.STDOUT)
            self.assertEqual(status, 1, lines)
            # The error of next follows the RESULT line.
            after = self.after_result(lines)
            self.assertIn(f"ultraclose: aven context {self.OTHER} failed", after[0])
            self.assertEqual(
                git("log", "-1", "--format=%s", cwd=repo, env=env), self.SUBJECT
            )
            self.assertEqual(
                self.task_states(tmp), {REF: "landed", self.OTHER: "pending"}
            )

    def test_next_reports_leftover_work_after_landing(self):
        # An earlier run left the branch of the second task. The landing
        # passes, and its next starts no task.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp)
            self.add_other(tmp, repo, env)
            other = f"task/{self.OTHER}"
            git("branch", other, cwd=repo, env=env)
            status, lines = self.land(repo, env, "--next", stderr=subprocess.STDOUT)
            self.assertEqual(status, 1, lines)
            # The refusal of next follows the RESULT line.
            after = self.after_result(lines)
            self.assertIn(
                f"{self.OTHER}: the branch {other}, 0 commits ahead of main", after
            )
            message = "\n".join(after)
            self.assertIn(OWN_WORK, message)
            # The task of the landing has landed, so the refusal names next
            # and not the landing.
            self.assertIn(RETRY.format("next"), message)
            self.assertEqual(
                self.task_states(tmp), {REF: "landed", self.OTHER: "pending"}
            )
            # The refusal keeps the times that the landing recorded.
            [entry] = state_of(Path(tmp, "run"))["landings"]
            self.assertEqual((entry["ref"], entry["result"]), (REF, "OK"))
            git("branch", "-D", other, cwd=repo, env=env)
            self.assertEqual(self.next_refs(repo, env), [self.OTHER])

    def test_result_reaches_output_before_next(self):
        # The aven stub holds the start of the second task until the test
        # creates the release file. The output goes to a file, so Python
        # buffers it unless the landing flushes the RESULT line.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp)
            self.add_other(tmp, repo, env)
            waiting, release = Path(tmp, "waiting"), Path(tmp, "release")
            env = {k: v for k, v in env.items() if k != "PYTHONUNBUFFERED"}
            env.update(WAITING=str(waiting), RELEASE=str(release))
            Path(tmp, "bin", "aven").write_text(
                "#!/bin/sh\n"
                'if test "$1" = context; then\n'
                '  touch "$WAITING"\n'
                '  until test -e "$RELEASE"; do sleep 0.05; done\n'
                "fi\n"
                "echo stub\n"
            )
            output = Path(tmp, "output")
            with output.open("w") as f:
                p = subprocess.Popen(
                    [sys.executable, str(SCRIPT), "land", REF, "--next"],
                    cwd=repo,
                    env=env,
                    stdout=f,
                    stderr=subprocess.STDOUT,
                )
            try:
                deadline = time.monotonic() + 60
                while not waiting.exists():
                    self.assertIsNone(p.poll(), output.read_text())
                    self.assertLess(time.monotonic(), deadline, "next did not start")
                    time.sleep(0.05)
                # The next command is running, and the landing is done.
                self.assertEqual(
                    self.after_result(output.read_text().splitlines()), []
                )
            finally:
                release.touch()
                p.wait(timeout=60)
            self.assertEqual(p.returncode, 0, output.read_text())
            lines = output.read_text().splitlines()
            started = json.loads("\n".join(self.after_result(lines)))
            self.assertEqual([t["task"]["ref"] for t in started], [self.OTHER])

    def test_health(self):
        # name: (the service runs at the start, the check, the hook, the
        # result, when the health command failed)
        cases = {
            "the service is down before the merge": (
                False,
                self.RECORD,
                "exit 0",
                "HEALTHFAIL",
                "before the merge",
            ),
            "the service stops during the check": (
                True,
                self.STOP,
                "exit 0",
                "HEALTHFAIL",
                "after the check failed",
            ),
            "the service stops during the commit": (
                True,
                self.RECORD,
                self.STOP,
                "HEALTHFAIL",
                "after the commit failed",
            ),
            "the check fails and the service answers": (
                True,
                "exit 1",
                "exit 0",
                "CHECKFAIL",
                None,
            ),
            "the hook fails and the service answers": (
                True,
                self.RECORD,
                "exit 1",
                "COMMITFAIL",
                None,
            ),
        }
        for name, (up, check, hook, result, when) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(
                    tmp, "--check", check, "--health", self.HEALTH, hook=hook
                )
                if up:
                    Path(env["ENGINE"]).touch()
                landing = self.land(repo, env)
                self.assert_failed(repo, env, landing, result)
                output = landing[1][:-1]
                if when:
                    self.assertIn("no engine", output)
                    found = [line for line in output if when in line]
                    self.assertTrue(found, output)
                    # After a failed step, the output names the log of
                    # that step, which shows whether the step stopped the
                    # service.
                    if when.startswith("after"):
                        log = Path(found[0].split(", see ", 1)[1])
                        self.assertIn(self.CAUSE, log.read_text())
                else:
                    self.assertNotIn("no engine", output)
                # A landing that starts with the service down skips the check.
                self.assertEqual(Path(env["RAN"]).exists(), up and check == self.RECORD)

    def test_land_again_after_repair(self):
        # The check passes and the service stops. Then the hook cannot run.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(
                tmp,
                "--check",
                self.STOP_ONCE,
                "--health",
                self.HEALTH,
                hook=self.HEALTH,
            )
            Path(env["ENGINE"]).touch()
            landing = self.land(repo, env)
            self.assert_failed(repo, env, landing, "HEALTHFAIL")
            self.assertTrue(
                any("after the commit failed" in line for line in landing[1]), landing
            )
            # The task did not change. The service runs again.
            Path(env["ENGINE"]).touch()
            self.assert_landed(repo, env, self.land(repo, env))

    def test_config_changes_health(self):
        # The service is down. name: (the init flags, the config flags,
        # whether the landing ends in HEALTHFAIL)
        cases = {
            "config sets a health command": ([], ["--health", self.HEALTH], True),
            "config removes the health command": (
                ["--health", self.HEALTH],
                ["--health", ""],
                False,
            ),
        }
        for name, (flags, config, fails) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(tmp, *flags)
                ultraclose("config", *config, cwd=repo, env=env)
                landing = self.land(repo, env)
                if fails:
                    self.assert_failed(repo, env, landing, "HEALTHFAIL")
                else:
                    self.assert_landed(repo, env, landing)

    def test_state_without_health_key(self):
        # A run can continue from a state file that init wrote without
        # the health key.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp)
            state = Path(tmp, "run", "state.json")
            st = json.loads(state.read_text())
            del st["config"]["health"]
            state.write_text(json.dumps(st))
            self.assert_landed(repo, env, self.land(repo, env))

    def test_records_times(self):
        # A step that takes half a second.
        slow = "sleep 0.5"
        # name: (the init flags, the hook, the result, the timed steps
        # that ran, the step that takes half a second)
        cases = {
            "a slow check": (
                ["--check", slow],
                "exit 0",
                "OK",
                ["check", "commit"],
                "check",
            ),
            "a slow commit hook and no check": ([], slow, "OK", ["commit"], "commit"),
            "a failed check": (
                ["--check", "exit 1"],
                "exit 0",
                "CHECKFAIL",
                ["check"],
                None,
            ),
            "a failed commit hook": (
                ["--check", self.RECORD],
                "exit 1",
                "COMMITFAIL",
                ["check", "commit"],
                None,
            ),
            "a service that stops during the check": (
                ["--check", self.STOP, "--health", self.HEALTH],
                "exit 0",
                "HEALTHFAIL",
                ["check"],
                None,
            ),
        }
        for name, (flags, hook, result, steps, slow_step) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(tmp, *flags, hook=hook)
                Path(env["ENGINE"]).touch()
                _, lines = self.land(repo, env)
                # A run starts from a state file without the landings key.
                [entry] = state_of(Path(tmp, "run"))["landings"]
                self.assertEqual((entry["ref"], entry["result"]), (REF, result))
                self.assertEqual(sorted(entry["times"]), steps)
                if slow_step:
                    self.assertGreaterEqual(entry["times"][slow_step], 0.5)
                # The TIMES line comes after the failing output and before
                # the RESULT line. The first landing has no median.
                times = " ".join(rf"{step}=\S+" for step in steps)
                self.assertRegex(lines[-2], f"^TIMES {times}$")
                self.assertTrue(lines[-1].startswith(f"RESULT {REF} {result}"), lines)

    def test_records_time_of_both_commit_attempts(self):
        # The first run of the hook drops the file of the task from the
        # index, so the landing commits a second time. Each run of the
        # hook takes half a second.
        hook = 'test -e "$RAN" || { touch "$RAN"; git rm -q --cached a.txt; }; sleep 0.5'
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp, hook=hook)
            landing = self.land(repo, env)
            self.assert_landed(repo, env, landing)
            self.assertEqual(
                git("ls-tree", "--name-only", "HEAD", cwd=repo, env=env), "a.txt"
            )
            [entry] = state_of(Path(tmp, "run"))["landings"]
            self.assertEqual((entry["ref"], entry["result"]), (REF, "OK"))
            # The time of the commit covers both attempts.
            self.assertGreaterEqual(entry["times"]["commit"], 1.0)
            self.assertRegex(landing[1][-2], r"^TIMES commit=\S+$")

    def test_landing_that_stops_before_check_records_no_time(self):
        # name: (the init flags, whether the task has a commit message,
        # the result)
        cases = {
            "no commit message": ([], False, "NOMSG"),
            "the service is down before the merge": (
                ["--health", self.HEALTH],
                True,
                "HEALTHFAIL",
            ),
        }
        for name, (flags, has_message, result) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(tmp, "--check", self.RECORD, *flags)
                if not has_message:
                    Path(tmp, "run", "tasks", REF, "msg.txt").unlink()
                landing = self.land(repo, env)
                self.assert_failed(repo, env, landing, result)
                self.assertNotIn("landings", state_of(Path(tmp, "run")))
                self.assertFalse(
                    [line for line in landing[1] if line.startswith("TIMES")], landing
                )

    def test_result_line_follows_times_that_run_cannot_record(self):
        # The check fails, and it puts a directory in the place of the
        # file that the next write of the state takes.
        block = 'mkdir "$BLOCK"; exit 1'
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp, "--check", block)
            env["BLOCK"] = str(Path(tmp, "run", "state.json.tmp"))
            before = state_of(Path(tmp, "run"))
            landing = self.land(repo, env)
            self.assert_failed(repo, env, landing, "CHECKFAIL")
            lines = landing[1]
            self.assertTrue(
                lines[-3].startswith("the run did not record the times"), lines
            )
            self.assertRegex(lines[-2], r"^TIMES check=\S+$")
            before["tasks"][REF]["state"] = "approved"
            self.assertEqual(state_of(Path(tmp, "run")), before)

    def test_times_line_gives_medians_of_landed_tasks(self):
        def landing(result, **times):
            return {"ref": "APP-0001", "result": result, "times": times}

        # The medians count the landings that landed a task. The times
        # of their checks are 80 and 90 seconds, and the times of their
        # commits are 10, 20, and 710 seconds.
        earlier = [
            landing("OK", check=80, commit=10),
            landing("CHECKFAIL", check=9000),
            landing("OK", check=90, commit=20),
            landing("COMMITFAIL", check=9000, commit=9000),
            landing("OK", commit=710),
        ]
        medians = {"check": "1m25s", "commit": "20s"}
        # name: (the init flags, the hook, the result, the median of each
        # timed step that ran)
        cases = {
            "a landed task": (["--check", self.RECORD], "exit 0", "OK", medians),
            "a landed task of a run without a check": (
                [],
                "exit 0",
                "OK",
                {"commit": "20s"},
            ),
            "a failed commit hook": (
                ["--check", self.RECORD],
                "exit 1",
                "COMMITFAIL",
                medians,
            ),
        }
        for name, (flags, hook, result, want) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(tmp, *flags, hook=hook)
                state = Path(tmp, "run", "state.json")
                st = json.loads(state.read_text())
                st["landings"] = earlier
                state.write_text(json.dumps(st))
                _, lines = self.land(repo, env)
                times = " ".join(
                    rf"{step}=\S+ \(median {median}\)" for step, median in want.items()
                )
                self.assertRegex(lines[-2], f"^TIMES {times}$")
                self.assertTrue(lines[-1].startswith(f"RESULT {REF} {result}"), lines)
                # The run keeps the earlier landings and adds this one.
                landings = state_of(Path(tmp, "run"))["landings"]
                self.assertEqual(landings[:-1], earlier)
                self.assertEqual(
                    (landings[-1]["ref"], landings[-1]["result"]), (REF, result)
                )

    def test_stops_step_that_runs_longer_than_step_timeout(self):
        # The first run of the step hangs, and the step timeout of the run
        # is 2 seconds.
        # name: (the check, the hook, the step that hangs, the result)
        cases = {
            "a check that hangs": (self.HANG_ONCE, "exit 0", "check", "CHECKTIMEOUT"),
            "a commit hook that hangs": (
                "",
                self.HANG_ONCE,
                "commit",
                "COMMITTIMEOUT",
            ),
        }
        for name, (check, hook, step, result) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(
                    tmp, "--check", check, "--step-timeout", "2", hook=hook
                )
                landing = self.land(repo, env, timeout=60)
                # The step waited for a process that it started. The
                # landing ended that process with the step.
                self.assert_ended(self.hung_process(env))
                # After a stopped commit, the main checkout holds no
                # change of the task.
                self.assert_failed(repo, env, landing, result)
                run = Path(tmp, "run").resolve()
                log = run / "logs" / f"{REF}.{step}.log"
                lines = landing[1]
                self.assertIn(
                    f"the {step} ran longer than the step timeout of 2s, so the"
                    f" landing stopped it, see {log}",
                    lines,
                )
                # The end of the log shows where the step stopped.
                self.assertIn(self.WAITS, lines)
                self.assertIn(self.WAITS, log.read_text())
                [entry] = state_of(run)["landings"]
                self.assertEqual((entry["ref"], entry["result"]), (REF, result))
                self.assertGreaterEqual(entry["times"][step], 2)
                # The step ended on the SIGINT of the stop, and the landing
                # saw it end before the time of the next signal.
                self.assertLess(entry["times"][step], 2 + GRACE)
                self.assertRegex(lines[-2], rf"^TIMES (check=\S+ )?{step}=\S+$")
                # The landing lock is free, and the task lands when its
                # step ends in time.
                self.assert_landed(repo, env, self.land(repo, env, timeout=60))

    def test_stops_process_group_that_step_ends_after_sigint(self):
        # The hook stands in for lefthook, which runs some hook commands
        # in process groups of their own and ends them only after SIGINT.
        with tempfile.TemporaryDirectory() as tmp:
            manager = Path(tmp, "manager.py")
            manager.write_text(MANAGER)
            hook = f'exec "{sys.executable}" "{manager}"'
            repo, env = self.start_task(tmp, "--step-timeout", "2", hook=hook)
            landing = self.land(repo, env, timeout=60)
            self.assert_ended(self.hung_process(env))
            self.assert_failed(repo, env, landing, "COMMITTIMEOUT")
            # Every process of the commit ended on the SIGINT of the stop,
            # and the landing saw them end before the time of SIGTERM.
            [entry] = state_of(Path(tmp, "run"))["landings"]
            self.assertLess(entry["times"]["commit"], 2 + GRACE)

    def test_stops_processes_that_ignore_signals(self):
        # The landing sends SIGTERM to the processes that SIGINT did not
        # end, and it kills the processes that SIGTERM did not end.
        with tempfile.TemporaryDirectory() as tmp:
            script = Path(tmp, "stubborn.sh")
            script.write_text(STUBBORN)
            hook = f'exec sh "{script}"'
            repo, env = self.start_task(tmp, "--step-timeout", "2", hook=hook)
            stopped = Path(tmp, "stopped")
            env["STOPPED"] = str(stopped)
            landing = self.land(repo, env, timeout=60)
            self.assert_ended(self.hung_process(env))
            # The first process got SIGTERM before the landing killed the
            # processes of the step.
            self.assertTrue(stopped.exists())
            self.assert_failed(repo, env, landing, "COMMITTIMEOUT")

    def test_config_changes_step_timeout(self):
        # The run starts with the default step timeout, and the step
        # hangs for less time than that. So only the step timeout of the
        # config command stops the step.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp, "--check", self.HANG)
            ultraclose("config", "--step-timeout", "2", cwd=repo, env=env)
            landing = self.land(repo, env, timeout=60)
            self.assert_ended(self.hung_process(env))
            self.assert_failed(repo, env, landing, "CHECKTIMEOUT")

    def test_state_without_step_timeout_key(self):
        # A run can continue from a state file that init wrote without
        # the stepTimeout key. Its landings have the default step timeout.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp, "--check", self.RECORD)
            state = Path(tmp, "run", "state.json")
            st = json.loads(state.read_text())
            del st["config"]["stepTimeout"]
            state.write_text(json.dumps(st))
            self.assert_landed(repo, env, self.land(repo, env, timeout=60))

    def test_stopped_step_does_not_run_health_command(self):
        # The service is down when the landing stops the step. A failed
        # step with the service down ends the landing with HEALTHFAIL.
        stop = 'rm "$ENGINE"; '
        # name: (the check, the hook, the result)
        cases = {
            "a check that hangs": (
                stop + self.HANG,
                "exit 0",
                "CHECKTIMEOUT",
            ),
            "a commit hook that hangs": (
                "",
                stop + self.HANG,
                "COMMITTIMEOUT",
            ),
        }
        for name, (check, hook, result) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(
                    tmp,
                    "--check",
                    check,
                    "--health",
                    self.HEALTH,
                    "--step-timeout",
                    "2",
                    hook=hook,
                )
                Path(env["ENGINE"]).touch()
                landing = self.land(repo, env, timeout=60)
                self.assert_ended(self.hung_process(env))
                self.assert_failed(repo, env, landing, result)
                self.assertNotIn("no engine", landing[1])

    def test_result_line_follows_checkout_that_landing_cannot_restore(self):
        # The hook leaves the lock file of the index and hangs, as a git
        # process that the landing kills can. No git command can then
        # restore the main checkout.
        hook = f'touch "$LOCK"; {self.HANG_ONCE}'
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp, "--step-timeout", "2", hook=hook)
            lock = Path(repo, ".git", "index.lock")
            env["LOCK"] = str(lock)
            head = git("rev-parse", "HEAD", cwd=repo, env=env)
            status, lines = self.land(repo, env, timeout=60)
            self.assert_ended(self.hung_process(env))
            self.assertEqual(status, 2, lines)
            self.assertEqual(lines[-1], f"RESULT {REF} COMMITTIMEOUT")
            self.assertRegex(lines[-2], r"^TIMES commit=\S+$")
            # The output names the branch whose changes the main checkout
            # holds, the git command that failed, and the lock file.
            output = "\n".join(lines)
            self.assertIn(
                "the landing did not restore the main checkout, which still"
                f" holds the changes of task/{REF}:",
                lines,
            )
            reset = f"git reset -q --hard {head}"
            self.assertIn(f"ultraclose: {reset} failed in {repo}:", lines)
            self.assertIn(str(lock), output)
            self.assertNotEqual(git("status", "--porcelain", cwd=repo, env=env), "")
            # The task lands after the lock file leaves and the two
            # commands of the rollback run.
            lock.unlink()
            git(*reset.split()[1:], cwd=repo, env=env)
            git("clean", "-qfd", cwd=repo, env=env)
            self.assert_landed(repo, env, self.land(repo, env, timeout=60))

    def test_signal_during_restore_lets_git_command_finish(self):
        # The landing restores the main checkout after a commit that fails
        # and after a commit that it stops. The git wrapper sends a signal
        # to the land command when the landing runs the first git command
        # of the restore. The wrapper waits until the land command ends.
        # Then it runs that git command and records that the command
        # passed.
        # result: (the init flags, the commit hook)
        cases = {
            "COMMITFAIL": ([], "exit 1"),
            "COMMITTIMEOUT": (["--step-timeout", "2"], self.HANG),
        }
        for (result, (flags, hook)), sig in itertools.product(
            cases.items(), (signal.SIGTERM, signal.SIGHUP)
        ):
            with (
                self.subTest(result=result, signal=sig.name),
                tempfile.TemporaryDirectory() as tmp,
            ):
                # The land command of this test inherits the signals that
                # the caller of the test ignores, and it keeps them ignored.
                if signal.getsignal(sig) == signal.SIG_IGN:
                    self.skipTest(f"the caller of the test ignores {sig.name}")
                repo, env = self.start_task(tmp, *flags, hook=hook)
                wrapper = Path(tmp, "wrapper")
                wrapper.mkdir()
                real = shutil.which("git", path=env["PATH"])
                reset = Path(tmp, "reset")
                (wrapper / "git").write_text(
                    "#!/bin/sh\n"
                    'if [ "$1 $2 $3" = "reset -q --hard" ]; then\n'
                    f"  kill -{sig.name.removeprefix('SIG')} $PPID\n"
                    "  while kill -0 $PPID 2>/dev/null; do sleep 0.05; done\n"
                    f'  "{real}" "$@" && touch "{reset}"\n'
                    "  exit\n"
                    "fi\n"
                    f'exec "{real}" "$@"\n'
                )
                (wrapper / "git").chmod(0o755)
                env["PATH"] = f"{wrapper}{os.pathsep}{env['PATH']}"
                status, lines = self.land(repo, env, timeout=60)
                if result == "COMMITTIMEOUT":
                    self.assert_ended(self.hung_process(env))
                # The signal ended the land command before it printed a
                # result.
                self.assertEqual(status, -sig, lines)
                self.assertFalse(
                    [line for line in lines if line.startswith(f"RESULT {REF}")],
                    lines,
                )
                # The git command of the restore outlives the land command.
                deadline = time.monotonic() + 30
                while not reset.exists():
                    self.assertLess(
                        time.monotonic(), deadline, "the git reset did not pass"
                    )
                    time.sleep(0.05)
                self.assertEqual(git("status", "--porcelain", cwd=repo, env=env), "")

    def test_signal_stops_step_that_runs(self):
        for sig in (signal.SIGTERM, signal.SIGHUP, signal.SIGINT):
            with self.subTest(sig.name), tempfile.TemporaryDirectory() as tmp:
                # The land command of this test inherits the signals that
                # the caller of the test ignores, and it keeps them ignored.
                if signal.getsignal(sig) == signal.SIG_IGN:
                    self.skipTest(f"the caller of the test ignores {sig.name}")
                repo, env = self.start_task(tmp, "--check", self.HANG)
                p = subprocess.Popen(
                    [sys.executable, str(SCRIPT), "land", REF],
                    cwd=repo,
                    env=env,
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                )
                try:
                    process = self.hung_process(env)
                    p.send_signal(sig)
                    p.wait(timeout=60)
                finally:
                    p.kill()
                    p.wait()
                self.assertNotEqual(p.returncode, 0)
                self.assert_ended(process)

    def test_signal_during_stop_ends_command_after_step_stops(self):
        # The step keeps running after SIGINT, so the landing sends it
        # SIGTERM GRACE seconds after SIGINT. A signal comes in that time.
        # name: (the init flags, the signal that starts the stop, the
        # signal that comes during the stop, the exit status of the land
        # command)
        cases = {
            "a second SIGINT": ([], signal.SIGINT, signal.SIGINT, -signal.SIGINT),
            "SIGTERM after the step timeout": (
                ["--step-timeout", "1"],
                None,
                signal.SIGTERM,
                128 + signal.SIGTERM,
            ),
        }
        for name, (flags, first, second, status) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                # The land command of this test inherits the signals that
                # the caller of the test ignores, and it keeps them ignored.
                if signal.getsignal(second) == signal.SIG_IGN:
                    self.skipTest(f"the caller of the test ignores {second.name}")
                program = Path(tmp, "program.py")
                program.write_text(OUTLASTS_SIGINT)
                check = f'exec "{sys.executable}" "{program}"'
                repo, env = self.start_task(tmp, "--check", check, *flags)
                interrupted = Path(tmp, "interrupted")
                env["INTERRUPTED"] = str(interrupted)
                p = subprocess.Popen(
                    [sys.executable, str(SCRIPT), "land", REF],
                    cwd=repo,
                    env=env,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.DEVNULL,
                    text=True,
                )
                try:
                    process = self.hung_process(env)
                    if first:
                        p.send_signal(first)
                    # The step records the SIGINT of the stop.
                    deadline = time.monotonic() + 60
                    while not interrupted.exists():
                        self.assertLess(
                            time.monotonic(), deadline, "the stop did not start"
                        )
                        time.sleep(0.05)
                    p.send_signal(second)
                    output = p.communicate(timeout=60)[0]
                finally:
                    p.kill()
                    p.wait()
                self.assert_ended(process)
                # The signal ended the land command before it printed a
                # result.
                self.assertEqual(p.returncode, status)
                self.assertNotIn(f"RESULT {REF}", output)

    def test_keeps_signal_that_caller_ignores(self):
        # The land command inherits each signal that its caller ignores,
        # such as SIGHUP under nohup. That signal does not stop the step,
        # and the task lands.
        check = 'echo $$ >"$PROCESS"; sleep 3'
        for sig in (signal.SIGTERM, signal.SIGHUP, signal.SIGINT):
            with self.subTest(sig.name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(tmp, "--check", check)
                ignore = f'trap "" {sig.name.removeprefix("SIG")}; exec "$0" "$@"'
                p = subprocess.Popen(
                    ["sh", "-c", ignore, sys.executable, str(SCRIPT), "land", REF],
                    cwd=repo,
                    env=env,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                )
                try:
                    self.hung_process(env)
                    p.send_signal(sig)
                    output = p.communicate(timeout=60)[0]
                finally:
                    p.kill()
                    p.wait()
                self.assert_landed(repo, env, (p.returncode, output.splitlines()))


# A branch that a run lands on in place of main.
RELEASE = "release/v1"
# The branch that a landing names for the pull request of the task of
# LandCase.start_task. The name comes from LandCase.SUBJECT.
HEAD = "feat/add-a-file"
# The address of a pull request.
URL = "https://github.com/example/app/pull/7"
# What the merged command prints when the base branch main lacks the
# change of the task of LandCase.start_task.
LACKS = f"main lacks the change of {REF}"
# What status prints after "target: " for a run whose landings propose
# pull requests. The format arguments are the target and the base branch.
PR_TARGET = "{}, with a pull request against {} for each task"


def branches(repo, env):
    """The local branches of a repository."""
    names = "--format=%(refname:lstrip=2)"
    return git("for-each-ref", names, "refs/heads", cwd=repo, env=env).splitlines()


def branch_heads(repo, env):
    """The commit of each local branch of a repository, by the name of the branch."""
    return {b: git("rev-parse", b, cwd=repo, env=env) for b in branches(repo, env)}


class TargetTest(LandCase):
    """The target of a run names where its tasks land.

    A branch makes each landing commit on that branch in the main
    checkout, so init takes only a branch that the main checkout has
    checked out. The target pr makes each landing propose a pull request
    against the base branch, which the main checkout need not have
    checked out.
    """

    def test_init_records_target(self):
        # name: (the branch that the main checkout has checked out, the
        # init flags, the target, the base branch, what status prints
        # about the target)
        cases = {
            "the default": ("main", [], "main", "main", "main"),
            "a branch": (RELEASE, ["--target", RELEASE], RELEASE, RELEASE, RELEASE),
            "pull requests": (
                "main",
                ["--target", "pr"],
                "pr",
                "main",
                PR_TARGET.format("pr", "main"),
            ),
            "pull requests while the main checkout has another branch": (
                RELEASE,
                ["--target", "pr"],
                "pr",
                "main",
                PR_TARGET.format("pr", "main"),
            ),
            "pull requests against a branch": (
                "main",
                ["--target", f"pr:{RELEASE}"],
                f"pr:{RELEASE}",
                RELEASE,
                PR_TARGET.format(f"pr:{RELEASE}", RELEASE),
            ),
        }
        for name, (checkout, flags, target, base, text) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = launchable_repo(tmp)
                git("branch", RELEASE, cwd=repo, env=env)
                git("switch", "-q", checkout, cwd=repo, env=env)
                run = Path(tmp, "run").resolve()
                out = ultraclose("init", "--run", str(run), *flags, cwd=repo, env=env)
                self.assertIn(f" target={target} base={base} ", out)
                config = state_of(run)["config"]
                self.assertEqual((config["target"], config["base"]), (target, base))
                self.assertEqual(status_values(repo, env, "target"), [text])

    def test_init_refuses_target_that_run_cannot_land_on(self):
        # name: (the branch that the main checkout has checked out, or
        # None for a detached HEAD, the init flags, the texts of the
        # refusal)
        cases = {
            "the default while the main checkout has another branch": (
                "trunk",
                [],
                ["the branch main", "has trunk checked out", "--target"],
            ),
            "a branch that the main checkout has not checked out": (
                "trunk",
                ["--target", RELEASE],
                [f"the branch {RELEASE}", "has trunk checked out", "--target"],
            ),
            "a detached HEAD": (
                None,
                ["--target", RELEASE],
                [f"the branch {RELEASE}", "has a detached HEAD"],
            ),
            "pull requests against a branch that the repository lacks": (
                "trunk",
                ["--target", "pr"],
                ["the repository has no branch main", "--target pr:BRANCH"],
            ),
            "pull requests against a branch without a name": (
                "trunk",
                ["--target", "pr:"],
                ["give a branch, pr, or pr:BRANCH in place of 'pr:'"],
            ),
            "a target without a name": (
                "trunk",
                ["--target", ""],
                ["give a branch, pr, or pr:BRANCH in place of ''"],
            ),
        }
        for name, (checkout, flags, texts) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                # The repository has the branches trunk and RELEASE, and
                # no branch main.
                repo, env = launchable_repo(tmp)
                git("branch", "-m", "main", "trunk", cwd=repo, env=env)
                git("branch", RELEASE, cwd=repo, env=env)
                if not checkout:
                    git("switch", "-q", "--detach", cwd=repo, env=env)
                run = Path(tmp, "run")
                message = refused("init", "--run", str(run), *flags, cwd=repo, env=env)
                for text in texts:
                    self.assertIn(text, message)
                self.assertFalse(run.exists())
                self.assertIn("no run found", refused("status", cwd=repo, env=env))

    def test_help_names_targets(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = new_repo(tmp)
            text = " ".join(ultraclose("init", "--help", cwd=repo, env=env).split())
            self.assertIn("--target TARGET", text)
            self.assertIn("which the main checkout must have checked out", text)
            self.assertIn("pr:BRANCH as a pull request against BRANCH", text)
            self.assertIn("The default is main", text)

    def test_state_without_target_key(self):
        # A run can continue from a state file that init wrote without
        # the target key. Each landing of that run commits on the base
        # branch.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp)
            run = Path(tmp, "run")
            st = state_of(run)
            del st["config"]["target"]
            (run / "state.json").write_text(json.dumps(st))
            self.assertEqual(status_values(repo, env, "target"), ["main"])
            self.assert_landed(repo, env, self.land(repo, env))

    def test_lands_on_branch_target(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp, "--target", RELEASE, checkout=RELEASE)
            main = git("rev-parse", "main", cwd=repo, env=env)
            # The task branch starts from the target, which holds a commit
            # that main lacks.
            ahead = git("log", "--format=%s", f"main..task/{REF}", cwd=repo, env=env)
            self.assertEqual(ahead.splitlines(), ["wip", "branch"])
            self.add_other(tmp, repo, env, after=[REF])
            status, lines = self.land(repo, env, "--next")
            self.assertEqual(status, 0, lines)
            # The landing commits on the target, and main stays as it was.
            landed = git("log", "--format=%s", RELEASE, cwd=repo, env=env)
            self.assertEqual(landed.splitlines(), [self.SUBJECT, "branch", "init"])
            self.assertEqual(git("rev-parse", "main", cwd=repo, env=env), main)
            # The next task starts from the target as well, and its input
            # names the target as the base branch.
            [launch] = json.loads("\n".join(self.after_result(lines)))
            self.assertEqual((launch["base"], launch["pr"]), (RELEASE, False))
            self.assertEqual(launch["task"]["branch"], f"task/{self.OTHER}")
            self.assertEqual(
                git("rev-parse", "HEAD", cwd=launch["task"]["wt"], env=env),
                git("rev-parse", RELEASE, cwd=repo, env=env),
            )

    def test_landing_fails_while_main_checkout_has_another_branch(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp, "--target", RELEASE, checkout=RELEASE)
            release = git("rev-parse", RELEASE, cwd=repo, env=env)
            git("switch", "-q", "main", cwd=repo, env=env)
            landing = self.land(repo, env)
            self.assert_failed(repo, env, landing, "NOTBASE")
            self.assertIn(f"the main checkout is not on {RELEASE}", landing[1])
            self.assertEqual(git("rev-parse", RELEASE, cwd=repo, env=env), release)


class PullRequestTest(LandCase):
    """A run with the target pr proposes each task as a pull request.

    A landing of such a run runs the check and names the branch of the
    pull request. It records no commit on the base branch, and it leaves
    the main checkout alone. The proposed task waits for its pull request
    to merge, and the merged command lands it.
    """

    # The line that a landing prints last when it proposes the task of
    # start_task.
    RESULT = f"RESULT {REF} PR head={HEAD} base=main"

    def propose(self, tmp, *flags):
        """Start a run with the target pr, and land its task.

        The flags go to init. Return the repository and the environment.
        """
        repo, env = self.start_task(tmp, "--target", "pr", *flags)
        status, lines = self.land(repo, env)
        self.assertEqual((status, lines[-1]), (0, self.RESULT), lines)
        return repo, env

    def task(self, tmp):
        """The task of start_task, as the state of the run holds it."""
        return state_of(Path(tmp, "run"))["tasks"][REF]

    def worktree(self, repo):
        """The worktree of the task of start_task."""
        return Path(repo, ".claude", "worktrees", f"t-{REF}")

    def commit(self, cwd, env, file, text="b\n"):
        """Record one commit that writes the text to the file."""
        Path(cwd, file).write_text(text)
        git("add", "-A", cwd=cwd, env=env)
        git("commit", "-q", "--no-verify", "-m", f"write {file}", cwd=cwd, env=env)

    def merge(self, repo, env, how="squash"):
        """Bring the branch of the pull request into main, as the remote does.

        The how argument names the kind of the merge: squash for one
        squashed commit, merge for a merge commit, rebase for a copy of
        each commit of the branch, or ff for a fast-forward of main to the
        branch. The main checkout has main checked out.
        """
        if how == "squash":
            git("merge", "-q", "--squash", HEAD, cwd=repo, env=env)
            git("commit", "-q", "--no-verify", "-m", "squash", cwd=repo, env=env)
        elif how == "ff":
            git("merge", "-q", "--ff-only", HEAD, cwd=repo, env=env)
        elif how == "merge":
            merge = ("merge", "-q", "--no-verify", "--no-ff", "--no-edit")
            git(*merge, HEAD, cwd=repo, env=env)
        else:
            git("cherry-pick", f"main..{HEAD}", cwd=repo, env=env)

    def test_landing_proposes_pull_request(self):
        # The commit hook of the repository fails, so a landing that
        # commits fails as well.
        # name: the branch that the main checkout has checked out in
        # place of main, beside a file that git does not track
        cases = {"the main checkout on the base branch": None, "another branch": "work"}
        for name, checkout in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(
                    tmp, "--target", "pr", hook="exit 1", checkout=checkout
                )
                if checkout:
                    Path(repo, "untracked.txt").write_text("u\n")
                before = branch_heads(repo, env)
                change = before.pop(f"task/{REF}")
                status, lines = self.land(repo, env)
                self.assertEqual((status, lines), (0, [self.RESULT]))
                # The base branch did not move, so the landing commits on
                # no branch. It renames the task branch, which starts from
                # the base branch.
                self.assertEqual(branch_heads(repo, env), {**before, HEAD: change})
                ahead = git("log", "--format=%s", f"main..{HEAD}", cwd=repo, env=env)
                self.assertEqual(ahead, "wip")
                self.assertFalse(self.worktree(repo).exists())
                # The task waits as proposed, and it stays open in aven.
                task = self.task(tmp)
                self.assertEqual((task["state"], task["branch"]), ("proposed", HEAD))
                self.assertEqual(aven_edits(env), [f"{REF} --status active"])
                lines = ultraclose("status", cwd=repo, env=env).splitlines()
                waits = f"waits for the push of {HEAD} and its pull request"
                self.assertIn(f"proposed {REF} {waits} | t", lines)
                self.assertNotIn(ENDED, lines)

    def test_landing_runs_check(self):
        # name: (the check, the exit status, the result, the state of the
        # task, the branch of the task)
        cases = {
            "a check that passes": (self.RECORD, 0, self.RESULT, "proposed", HEAD),
            "a check that fails": (
                "exit 1",
                2,
                f"RESULT {REF} CHECKFAIL",
                "approved",
                f"task/{REF}",
            ),
        }
        for name, (check, status, result, task_state, branch) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(tmp, "--target", "pr", "--check", check)
                landing = self.land(repo, env)
                self.assertEqual((landing[0], landing[1][-1]), (status, result))
                self.assertRegex(landing[1][-2], r"^TIMES check=\S+$")
                self.assertEqual(self.task(tmp)["state"], task_state)
                self.assertIn(branch, branches(repo, env))
                # A failed landing keeps the worktree for the next one.
                self.assertEqual(self.worktree(repo).exists(), status == 2)
                # The medians count a landing that proposed its task.
                lines = ultraclose("status", cwd=repo, env=env).splitlines()
                medians = [x for x in lines if x.startswith("median landing: check=")]
                self.assertEqual(len(medians), 1 if status == 0 else 0)

    def test_branch_name_follows_commit_message(self):
        long = "fix: keep the summary and the notes of a relaunched task in its file\n"
        # name: (the commit message, the refs that the repository holds,
        # the name of the branch)
        cases = {
            "a type and a scope": (
                "fix(mcp): retry a timeout\n\nThe body.\n",
                [],
                "fix/retry-a-timeout",
            ),
            "a breaking change": (
                "feat!: drop the v1 API\n",
                [],
                "feat/drop-the-v1-api",
            ),
            "a subject without a type": ("Add a file\n", [], "feat/add-a-file"),
            "a type that opens no branch name": (
                "deps: bump the parser\n",
                [],
                "feat/deps-bump-the-parser",
            ),
            "a subject of more than 50 characters": (
                long,
                [],
                "fix/keep-the-summary-and-the-notes-of-a-relaunched",
            ),
            "a subject without a word": ("fix: ...\n", [], "fix/change"),
            "a blank line before the subject": ("\nfix: retry\n", [], "fix/retry"),
            "a branch with the name": (
                self.SUBJECT,
                [f"refs/heads/{HEAD}"],
                f"{HEAD}-2",
            ),
            "two branches with the name and the next name": (
                self.SUBJECT,
                [f"refs/heads/{HEAD}", f"refs/heads/{HEAD}-2"],
                f"{HEAD}-3",
            ),
            "a remote branch with the name": (
                self.SUBJECT,
                [f"refs/remotes/origin/{HEAD}"],
                f"{HEAD}-2",
            ),
            "a branch below the name": (
                self.SUBJECT,
                [f"refs/heads/{HEAD}/part"],
                f"{HEAD}-2",
            ),
        }
        for name, (message, held, branch) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(tmp, "--target", "pr")
                Path(tmp, "run", "tasks", REF, "msg.txt").write_text(message)
                for ref in held:
                    git("update-ref", ref, "main", cwd=repo, env=env)
                status, lines = self.land(repo, env)
                self.assertEqual(
                    (status, lines), (0, [f"RESULT {REF} PR head={branch} base=main"])
                )
                self.assertIn(branch, branches(repo, env))
                self.assertEqual(self.task(tmp)["branch"], branch)

    def test_branch_named_after_type_blocks_landing(self):
        # Git keeps no branch below the name of another branch, so a
        # branch named feat blocks every name that starts with feat/.
        for held in ("refs/heads/feat", "refs/remotes/origin/feat"):
            with self.subTest(held), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(
                    tmp, "--target", "pr", "--check", self.RECORD
                )
                git("update-ref", held, "main", cwd=repo, env=env)
                landing = self.land(repo, env)
                self.assert_failed(repo, env, landing, "BRANCHTAKEN")
                self.assertIn(
                    "a local branch or a remote branch has the name feat",
                    landing[1][0],
                )
                self.assertIn(f"So feat blocks the branch {HEAD}", landing[1][0])
                # The landing stops before the check, and the task keeps
                # its branch and its worktree for the next landing.
                self.assertFalse(Path(env["RAN"]).exists())
                self.assertEqual(self.task(tmp)["state"], "approved")
                self.assertIn(f"task/{REF}", branches(repo, env))
                self.assertTrue(self.worktree(repo).exists())
                git("update-ref", "-d", held, cwd=repo, env=env)
                status, lines = self.land(repo, env)
                self.assertEqual((status, lines[-1]), (0, self.RESULT), lines)

    def test_branch_taken_during_check(self):
        # The first check creates a branch with the name of the pull
        # request branch.
        take = f'test -e "$RAN" || {{ touch "$RAN"; git branch {HEAD}; }}'
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp, "--target", "pr", "--check", take)
            landing = self.land(repo, env)
            self.assert_failed(repo, env, landing, "BRANCHTAKEN")
            self.assertIn(f"a branch named '{HEAD}' already exists", landing[1][0])
            self.assertEqual(self.task(tmp)["state"], "approved")
            self.assertNotIn("branch", self.task(tmp))
            self.assertIn(f"task/{REF}", branches(repo, env))
            self.assertTrue(self.worktree(repo).exists())
            # The next landing names a free branch.
            status, lines = self.land(repo, env)
            result = f"RESULT {REF} PR head={HEAD}-2 base=main"
            self.assertEqual((status, lines[-1]), (0, result), lines)

    def test_landing_merges_base_branch(self):
        # The base branch gains a commit after the task starts.
        # name: (the file that the commit writes, the exit status, the
        # result)
        cases = {
            "another file": ("b.txt", 0, self.RESULT),
            "the file of the task": (self.FILE, 2, f"RESULT {REF} CONFLICT"),
        }
        for name, (file, status, result) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(tmp, "--target", "pr")
                self.commit(repo, env, file)
                main = git("rev-parse", "main", cwd=repo, env=env)
                landing = self.land(repo, env)
                self.assertEqual((landing[0], landing[1][-1]), (status, result))
                self.assertEqual(git("rev-parse", "main", cwd=repo, env=env), main)
                if status:
                    # The output names the file with the conflict.
                    self.assertIn(self.FILE, landing[1])
                    self.assertEqual(self.task(tmp)["state"], "approved")
                    self.assertIn(f"task/{REF}", branches(repo, env))
                else:
                    # The branch of the pull request holds the base branch.
                    # The message of the merge commit does not name the
                    # task branch, whose name holds the task ref.
                    git("merge-base", "--is-ancestor", "main", HEAD, cwd=repo, env=env)
                    merge = git("log", "-1", "--format=%B", HEAD, cwd=repo, env=env)
                    self.assertEqual(merge, "Merge branch 'main'")

    def test_task_ref_in_commit_message(self):
        # The task branch holds a commit whose message names the task.
        # name: (the init flags, the result)
        cases = {
            "a pull request publishes the commit": (["--target", "pr"], "LOGHASREF"),
            "a squashed commit drops the message": ([], "OK"),
        }
        for name, (flags, result) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(tmp, *flags, "--check", self.RECORD)
                wt = self.worktree(repo)
                commit = ("commit", "-q", "--no-verify", "--allow-empty")
                git(*commit, "-m", f"wip\n\nFor {REF}.", cwd=wt, env=env)
                named = git("log", "-1", "--format=%h %s", cwd=wt, env=env)
                landing = self.land(repo, env)
                if result == "OK":
                    self.assert_landed(repo, env, landing)
                    continue
                # The landing names the commit, and it stops before the
                # check.
                self.assert_failed(repo, env, landing, result)
                self.assertEqual(landing[1][:-1], [named])
                self.assertFalse(Path(env["RAN"]).exists())
                self.assertEqual(self.task(tmp)["state"], "approved")
                # One commit in place of the commits of the branch drops
                # the message.
                fork = git("merge-base", "main", "HEAD", cwd=wt, env=env)
                git("reset", "-q", "--soft", fork, cwd=wt, env=env)
                git("commit", "-q", "--no-verify", "-m", "wip", cwd=wt, env=env)
                status, lines = self.land(repo, env)
                self.assertEqual((status, lines[-1]), (0, self.RESULT), lines)

    def test_change_without_content_proposes_nothing(self):
        # The second commit of the task branch removes the file that the
        # first commit added.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp, "--target", "pr", "--check", self.RECORD)
            wt = self.worktree(repo)
            git("rm", "-q", self.FILE, cwd=wt, env=env)
            git("commit", "-q", "--no-verify", "-m", "undo", cwd=wt, env=env)
            landing = self.land(repo, env)
            self.assert_failed(repo, env, landing, "EMPTY")
            # The landing stops before the check.
            self.assertFalse(Path(env["RAN"]).exists())
            self.assertEqual(self.task(tmp)["state"], "approved")
            self.assertIn(f"task/{REF}", branches(repo, env))

    def test_next_starts_tasks_that_do_not_wait_for_merge(self):
        # name: (the init flags, the fields of the second task, whether
        # the second task starts after the landing)
        cases = {
            "a task that waits for the slot": (["--limit", "1"], {}, True),
            "a task that waits for a file of the proposed task": (
                [],
                {"files": [self.FILE]},
                False,
            ),
            "a task that waits for the proposed task": ([], {"after": [REF]}, False),
        }
        for name, (flags, row, starts) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_task(tmp, "--target", "pr", *flags)
                self.add_other(tmp, repo, env, **row)
                self.assertEqual(self.next_refs(repo, env), [])
                status, lines = self.land(repo, env, "--next")
                self.assertEqual((status, lines[0]), (0, self.RESULT), lines)
                # The list of Workflow inputs follows the RESULT line.
                started = json.loads("\n".join(lines[1:]))
                refs = [t["task"]["ref"] for t in started]
                self.assertEqual(refs, [self.OTHER] if starts else [])
                if starts:
                    continue
                # The second task waits until the pull request merges. It
                # then starts from the base branch, which holds the change.
                self.assertEqual(self.next_refs(repo, env), [])
                self.merge(repo, env)
                ultraclose("merged", REF, cwd=repo, env=env)
                [launch] = json.loads(ultraclose("next", cwd=repo, env=env))
                self.assertEqual(launch["task"]["ref"], self.OTHER)
                self.assertTrue(Path(launch["task"]["wt"], self.FILE).exists())

    def test_opened_records_pull_request(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp, "--target", "pr")
            # Only a proposed task has a pull request.
            message = refused("opened", REF, URL, cwd=repo, env=env)
            self.assertIn(f"{REF} is inflight", message)
            self.assertIn("the opened command takes only a proposed task", message)
            message = refused("opened", self.OTHER, URL, cwd=repo, env=env)
            self.assertIn(f"unknown task {self.OTHER}", message)
            status, lines = self.land(repo, env)
            self.assertEqual((status, lines), (0, [self.RESULT]))
            out = ultraclose("opened", REF, URL, cwd=repo, env=env)
            self.assertEqual(out, f"{REF} proposed {URL}\n")
            self.assertEqual(self.task(tmp)["pr"], URL)
            lines = ultraclose("status", cwd=repo, env=env).splitlines()
            self.assertIn(f"proposed {REF} waits for the merge of {URL} | t", lines)

    def test_merged_lands_task(self):
        # Each case names the kind of the merge that brings the branch of
        # the pull request into the base branch.
        for how in ("squash", "merge", "rebase"):
            with self.subTest(how), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.propose(tmp)
                # The base branch lacks the change of the task, and so does
                # the commit that it gains.
                for n in (1, 2):
                    message = refused("merged", REF, cwd=repo, env=env)
                    self.assertIn(LACKS, message)
                    self.assertIn("--force", message)
                    self.assertEqual(self.task(tmp)["state"], "proposed")
                    self.assertIn(HEAD, branches(repo, env))
                    self.assertEqual(aven_edits(env), [f"{REF} --status active"])
                    self.commit(repo, env, f"b{n}.txt")
                self.merge(repo, env, how)
                out = ultraclose("merged", REF, cwd=repo, env=env)
                self.assertEqual(out, f"{REF} landed\n")
                self.assertEqual(self.task(tmp)["state"], "landed")
                self.assertEqual(branches(repo, env), ["main"])
                self.assertEqual(aven_edits(env)[-1], f"{REF} --status done")
                lines = ultraclose("status", cwd=repo, env=env).splitlines()
                self.assertIn(ENDED, lines)

    def test_merged_lands_fast_forwarded_task(self):
        # The pull request merged as a fast-forward. So the base branch
        # gained no commit after the fork of the branch, and its last
        # commit is the last commit of the branch.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.propose(tmp)
            self.merge(repo, env, "ff")
            self.assertEqual(
                git("rev-parse", "main", cwd=repo, env=env),
                git("rev-parse", HEAD, cwd=repo, env=env),
            )
            out = ultraclose("merged", REF, cwd=repo, env=env)
            self.assertEqual(out, f"{REF} landed\n")
            self.assertEqual(self.task(tmp)["state"], "landed")
            self.assertEqual(branches(repo, env), ["main"])
            self.assertEqual(aven_edits(env)[-1], f"{REF} --status done")

    def test_merged_finds_change_under_later_commit(self):
        # A later commit of the base branch changes the lines of the
        # change, so a merge of the branch into the base branch has a
        # conflict. The commit of the merge still holds the change.
        for how in ("squash", "merge", "rebase"):
            with self.subTest(how), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.propose(tmp)
                self.merge(repo, env, how)
                self.commit(repo, env, self.FILE, "later\n")
                out = ultraclose("merged", REF, cwd=repo, env=env)
                self.assertEqual(out, f"{REF} landed\n")
                self.assertEqual(self.task(tmp)["state"], "landed")
                self.assertEqual(branches(repo, env), ["main"])

    def test_merged_without_change_in_base_branch(self):
        with tempfile.TemporaryDirectory() as tmp:
            # The pull request merged with other content than the branch
            # holds, so a merge of the branch has a conflict.
            repo, env = self.propose(tmp)
            self.commit(repo, env, self.FILE)
            message = refused("merged", REF, cwd=repo, env=env)
            self.assertIn(LACKS, message)
            ultraclose("merged", REF, "--force", cwd=repo, env=env)
            self.assertEqual(self.task(tmp)["state"], "landed")
            self.assertEqual(branches(repo, env), ["main"])
        with tempfile.TemporaryDirectory() as tmp:
            # A task whose branch is gone has no change to compare.
            repo, env = self.propose(tmp)
            git("branch", "-q", "-D", HEAD, cwd=repo, env=env)
            ultraclose("merged", REF, cwd=repo, env=env)
            self.assertEqual(self.task(tmp)["state"], "landed")
            self.assertEqual(aven_edits(env)[-1], f"{REF} --status done")

    def test_merged_changes_nothing_while_branch_is_checked_out(self):
        # The main checkout has the branch of the pull request checked
        # out, such as for a review, so git cannot delete the branch.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.propose(tmp)
            self.merge(repo, env)
            git("switch", "-q", HEAD, cwd=repo, env=env)
            message = refused("merged", REF, cwd=repo, env=env)
            self.assertIn(f"git cannot delete the branch {HEAD}", message)
            self.assertIn("the command changed nothing", message)
            self.assertIn("Check out another branch there", message)
            # The aven task stays open, and the run keeps the task.
            self.assertEqual(aven_edits(env), [f"{REF} --status active"])
            self.assertEqual(self.task(tmp)["state"], "proposed")
            self.assertIn(HEAD, branches(repo, env))
            git("switch", "-q", "main", cwd=repo, env=env)
            ultraclose("merged", REF, cwd=repo, env=env)
            self.assertEqual(self.task(tmp)["state"], "landed")
            self.assertEqual(aven_edits(env)[-1], f"{REF} --status done")

    def test_set_removes_work_of_relaunched_task(self):
        # The relaunch of a proposed task works on the branch of its pull
        # request. A closed task loses that branch, and a noted task
        # keeps it.
        for task_state in ("closed", "noted"):
            with self.subTest(task_state), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.propose(tmp)
                ultraclose("args", REF, "--sync", "a conflict", cwd=repo, env=env)
                self.assertTrue(self.worktree(repo).exists())
                out = ultraclose("set", REF, task_state, cwd=repo, env=env)
                self.assertEqual(out, f"{REF} {task_state}\n")
                self.assertFalse(self.worktree(repo).exists())
                kept = [HEAD, "main"] if task_state == "noted" else ["main"]
                self.assertEqual(branches(repo, env), kept)

    def test_proposed_task_takes_no_landing(self):
        # The landing that proposed the task removed its worktree.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.propose(tmp)
            before = self.task(tmp)
            message = refused("land", REF, cwd=repo, env=env)
            self.assertIn(f"{REF} is proposed", message)
            self.assertIn("merged command", message)
            self.assertIn("args command", message)
            self.assertEqual(self.task(tmp), before)

    def test_merged_takes_only_proposed_task(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp, "--target", "pr")
            message = refused("merged", REF, "--force", cwd=repo, env=env)
            self.assertIn(f"{REF} is inflight", message)
            self.assertIn("the merged command takes only a proposed task", message)
            message = refused("merged", self.OTHER, cwd=repo, env=env)
            self.assertIn(f"unknown task {self.OTHER}", message)
            self.assertEqual(self.task(tmp)["state"], "inflight")
            self.assertIn(f"task/{REF}", branches(repo, env))

    def test_relaunch_keeps_branch_and_pull_request(self):
        # The relaunch adds a commit to the branch of the pull request, so
        # the task waits for the push of that commit.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.propose(tmp)
            ultraclose("opened", REF, URL, cwd=repo, env=env)
            reason = "the pull request has a conflict"
            relaunch = ultraclose("args", REF, "--sync", reason, cwd=repo, env=env)
            launch = json.loads(relaunch)
            # The task starts again on the branch of the pull request.
            self.assertEqual((launch["pr"], launch["task"]["branch"]), (True, HEAD))
            wt = launch["task"]["wt"]
            self.assertEqual(git("branch", "--show-current", cwd=wt, env=env), HEAD)
            self.assertEqual(self.task(tmp)["state"], "inflight")
            # The sync agent adds a commit and changes the subject.
            self.commit(wt, env, "b.txt")
            change = git("rev-parse", "HEAD", cwd=wt, env=env)
            Path(launch["task"]["dir"], "msg.txt").write_text("fix: sync the file\n")
            status, lines = self.land(repo, env)
            # The landing keeps the name of the branch, and its result
            # names the pull request that the task records.
            self.assertEqual((status, lines), (0, [f"{self.RESULT} pr={URL}"]))
            self.assertEqual(branches(repo, env), [HEAD, "main"])
            self.assertEqual(git("rev-parse", HEAD, cwd=repo, env=env), change)
            task = self.task(tmp)
            self.assertEqual((task["state"], task["pr"]), ("proposed", URL))
            # The pull request lacks the commit of the sync agent until the
            # push, which opened records.
            proposed = f"proposed {REF} waits for the"
            lines = ultraclose("status", cwd=repo, env=env).splitlines()
            push = f"{proposed} push of {HEAD} to the pull request {URL} | t"
            self.assertIn(push, lines)
            ultraclose("opened", REF, URL, cwd=repo, env=env)
            lines = ultraclose("status", cwd=repo, env=env).splitlines()
            self.assertIn(f"{proposed} merge of {URL} | t", lines)
            self.assertNotIn(push, lines)

    def test_retry_of_noted_task_names_closed_pull_request(self):
        # The user closed the pull request, and the run noted the task.
        # A retry of the task names that pull request, so that the caller
        # reads its state and opens a new one.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.propose(tmp)
            ultraclose("opened", REF, URL, cwd=repo, env=env)
            ultraclose("set", REF, "noted", cwd=repo, env=env)
            launch = json.loads(ultraclose("args", REF, cwd=repo, env=env))
            self.assertEqual(launch["task"]["branch"], HEAD)
            self.commit(launch["task"]["wt"], env, "b.txt")
            status, lines = self.land(repo, env)
            self.assertEqual((status, lines[-1]), (0, f"{self.RESULT} pr={URL}"))
            # The new pull request replaces the closed one.
            new = URL.replace("/7", "/8")
            ultraclose("opened", REF, new, cwd=repo, env=env)
            self.assertEqual(self.task(tmp)["pr"], new)
            lines = ultraclose("status", cwd=repo, env=env).splitlines()
            self.assertIn(f"proposed {REF} waits for the merge of {new} | t", lines)

    def test_new_run_takes_proposed_task(self):
        # A session with another version of the skill starts a new run,
        # which takes the proposed task with its branch and its pull
        # request.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp, "--target", "pr")
            earlier = in_session(env, "first")
            self.land(repo, earlier)
            ultraclose("opened", REF, URL, cwd=repo, env=earlier)
            task = self.task(tmp)
            before = state_of(Path(tmp, "run"))
            later, script = in_session(env, "second"), earlier_skill(tmp)
            second = Path(tmp, "second").resolve()
            init = ("init", "--run", str(second))
            # A run whose landings commit leaves the pull request open
            # beside the commit of the task.
            for flags in ([], ["--target", "main"]):
                with self.subTest(flags=flags):
                    message = refused(*init, *flags, cwd=repo, env=later, script=script)
                    for text in (
                        "has unfinished tasks of the target pr",
                        "each landing of that run proposes its task as a pull"
                        " request. Each landing of the target main commits its"
                        " task on main instead",
                        "Name the target pr to move those tasks to the new run",
                        "--force",
                    ):
                        self.assertIn(text, message)
                    self.assertFalse(second.exists())
                    self.assertEqual(state_of(Path(tmp, "run")), before)
            # A target with the same base branch whose landings propose
            # pull requests moves the tasks, whatever branch the main
            # checkout has checked out.
            git("switch", "-q", "-c", "work", cwd=repo, env=env)
            out = ultraclose(
                *init, "--target", "pr:main", cwd=repo, env=later, script=script
            )
            self.assertIn(f"{REF} (proposed)", out.splitlines())
            self.assertEqual(state_of(second)["tasks"], {REF: task})
            lines = ultraclose("status", cwd=repo, env=later, script=script)
            waits = f"proposed {REF} waits for the merge of {URL} | t"
            self.assertIn(waits, lines.splitlines())

    def test_new_run_counts_branch_of_task_that_it_does_not_take_as_leftover_work(
        self,
    ):
        # A new run that does not take the task leaves it in the earlier
        # run, with the branch and the pull request of the task. A new
        # attempt at the task opens a second pull request, so the new run
        # counts the branch as leftover work.
        def opened(repo, env):
            """The run records the pull request of the proposed task."""
            ultraclose("opened", REF, URL, cwd=repo, env=env)

        def ended(repo, env):
            """A relaunch of the task ends noted, so the run ends."""
            opened(repo, env)
            ultraclose("args", REF, "--sync", "a conflict", cwd=repo, env=env)
            ultraclose("set", REF, "noted", cwd=repo, env=env)

        of_pr = f"the branch {HEAD} of the pull request {URL}, 1 commit ahead of main"
        # The orchestrator might never have pushed a branch whose task
        # records no pull request, so the line claims none.
        of_none = (
            f"the branch {HEAD} that a landing proposed, with no recorded pull"
            " request, 1 commit ahead of main"
        )
        # name: (what the first run does after the landing, the flags of
        # init, what init prints about the task, the leftover work of the
        # task, what the new run records)
        cases = {
            "init --force after a pull request opens": (
                opened,
                ["--force"],
                f"{REF} {HEAD} pr={URL}",
                of_pr,
                {"branch": HEAD, "pr": URL},
            ),
            "init --force before the push": (
                lambda repo, env: None,
                ["--force"],
                f"{REF} {HEAD}",
                of_none,
                {"branch": HEAD},
            ),
            "init after the run ended with the task noted": (
                ended,
                [],
                f"{REF} {HEAD} pr={URL}",
                of_pr,
                {"branch": HEAD, "pr": URL},
            ),
        }
        close = "close that pull request with mcp__github__update_pull_request"
        merged = "take the Merged pull request steps of the Leftover work section"
        # The refusal says how to find a pull request that the run did not
        # record.
        find = "list its pull requests with mcp__github__list_pull_requests"
        for name, (end, flags, printed, line, record) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.propose(tmp)
                end(repo, env)
                second = Path(tmp, "second").resolve()
                out = ultraclose(
                    "init", "--run", str(second), *flags, cwd=repo, env=env
                ).splitlines()
                at = out.index(
                    "left 1 task with the branch of a pull request in that run:"
                )
                self.assertEqual(out[at + 1], printed)
                self.assertEqual(state_of(second)["left"], {REF: record})
                # A second init keeps the record, and prints no task of
                # its own run.
                third = Path(tmp, "third").resolve()
                out = ultraclose(
                    "init", "--run", str(third), "--force", cwd=repo, env=env
                )
                self.assertNotIn("left", out)
                self.assertEqual(state_of(third)["left"], {REF: record})
                triage(tmp, repo, env, {"ref": REF})
                before = state_of(third)
                for command in (["next"], ["args", REF]):
                    message = refused(*command, cwd=repo, env=env)
                    self.assertIn(f"{REF}: {line}", message.splitlines())
                    self.assertIn(OWN_WORK, message)
                    self.assertIn(close, message)
                    self.assertIn(merged, message)
                    self.assertEqual(find in message, "pr" not in record)
                self.assertEqual(state_of(third), before)
                # After the move of the branch, the task starts on a new
                # branch from the base branch.
                git("branch", "-m", HEAD, KEPT, cwd=repo, env=env)
                launch = json.loads(ultraclose("next", cwd=repo, env=env))[0]
                self.assertEqual(launch["task"]["branch"], f"task/{REF}")
                self.assertEqual(branches(repo, env), [KEPT, "main", f"task/{REF}"])
                # A later init drops the record of a branch that is gone.
                fourth = Path(tmp, "fourth").resolve()
                ultraclose("init", "--run", str(fourth), "--force", cwd=repo, env=env)
                self.assertEqual(state_of(fourth)["left"], {})

    def test_new_run_that_takes_tasks_keeps_record(self):
        # The second run counts the branch of the pull request as leftover
        # work, and holds an unfinished task. A session with another
        # version of the skill starts a third run, which takes that task.
        # The third run keeps the record of the second run.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.propose(tmp)
            ultraclose("opened", REF, URL, cwd=repo, env=env)
            earlier = in_session(env, "first")
            second = Path(tmp, "second").resolve()
            init = ("init", "--run", str(second), "--force")
            ultraclose(*init, cwd=repo, env=earlier)
            triage(tmp, repo, earlier, {"ref": REF})
            record = {REF: {"branch": HEAD, "pr": URL}}
            self.assertEqual(state_of(second)["left"], record)
            later, script = in_session(env, "second"), earlier_skill(tmp)
            third = Path(tmp, "third").resolve()
            out = ultraclose(
                "init", "--run", str(third), cwd=repo, env=later, script=script
            )
            self.assertIn(f"{REF} (pending)", out.splitlines())
            # The record names no task of the second run, so init prints
            # no task.
            self.assertNotIn("left", out)
            self.assertEqual(state_of(third)["left"], record)
            message = refused("next", cwd=repo, env=later, script=script)
            line = f"the branch {HEAD} of the pull request {URL}"
            self.assertIn(line, message)

    def test_new_run_records_no_branch_of_closed_task(self):
        # The proposed task has no worktree, so set closed keeps its
        # branch. A new run counts no branch of a closed task as leftover
        # work.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.propose(tmp)
            ultraclose("opened", REF, URL, cwd=repo, env=env)
            ultraclose("set", REF, "closed", cwd=repo, env=env)
            self.assertIn(HEAD, branches(repo, env))
            second = Path(tmp, "second").resolve()
            out = ultraclose("init", "--run", str(second), "--force", cwd=repo, env=env)
            self.assertNotIn("left", out)
            self.assertEqual(state_of(second)["left"], {})

    def test_leftover_work_of_task_branch_names_no_pull_request(self):
        # The refusal names a pull request only for the branch of one.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env = self.start_task(tmp, "--target", "pr")
            second = Path(tmp, "second").resolve()
            ultraclose("init", "--run", str(second), "--force", cwd=repo, env=env)
            self.assertEqual(state_of(second)["left"], {})
            triage(tmp, repo, env, {"ref": REF})
            message = refused("next", cwd=repo, env=env)
            self.assertIn(OWN_WORK, message)
            self.assertNotIn("pull request", message)


# The Workflow tool runs the body of close-task.js inside an async
# function, and passes it the input and an agent function. The driver
# does the same under Node with scripted agents. It reads the input and
# one result per agent label from standard input. It prints the result of
# the script and each agent call.
DRIVER_HEAD = """\
const scenario = JSON.parse(require('node:fs').readFileSync(0, 'utf8'))
const calls = []
const scripted = async (prompt, opts) => {
  const { label, schema, model } = opts
  calls.push({ label, prompt, schema, model, options: Object.keys(opts).sort() })
  if (!(label in scenario.agents)) throw new Error(`no result for ${label}`)
  return scenario.agents[label]
}
const workflow = async (args, agent, log) => {
"""
DRIVER_TAIL = """
}
workflow(scenario.args, scripted, () => {}).then(result => {
  process.stdout.write(JSON.stringify({ result, calls }))
})
"""

FINDING = {
    "severity": "major",
    "location": "a.py:1",
    "problem": "p",
    "evidence": "e",
    "fix": "f",
    "regression": False,
}
APPROVE = {
    "verdict": "approve",
    "findings": [],
    "improves_base": True,
    "reject_reason": "",
}
CHANGES = {**APPROVE, "verdict": "changes", "findings": [FINDING]}
REJECT = {
    **CHANGES,
    "verdict": "reject",
    "improves_base": False,
    "reject_reason": "the approach cannot work",
}


def described(author):
    """The summary and the notes that one agent returns."""
    return {"summary": f"summary by {author}", "notes": f"notes by {author}"}


# The summary and the notes that the agents of an earlier launch stored,
# and the two values of a task whose agents stored nothing.
STORED = described("the earlier launch")
UNSTORED = {"summary": "", "notes": ""}
# The resume field of the input that `ultraclose.py args` prints for a
# task with one finished round, without and with --sync.
RELAUNCH = {"round": 2, "lastRound": 6, **STORED}
SYNC = {"round": 2, "lastRound": 3, "sync": "the merge conflicts", **STORED}


def defect(author):
    """The defect outside the task that one agent reports."""
    return {
        "title": f"defect by {author}",
        "description": f"## Observed\nthe fault that {author} found",
    }


def listed(*authors):
    """The defects of the authors as the prompt of a fixer or the narrowing
    agent lists them."""
    head = "The defects outside the task that the change reports so far:\n"
    if not authors:
        return head + "none\n"
    return head + "".join(
        f"{n}. defect by {author}\n"
        f"   ## Observed\n   the fault that {author} found\n"
        for n, author in enumerate(authors, 1)
    )


IMPLEMENTED = {
    "status": "implemented",
    **described("the implementer"),
    "defects": [defect("the implementer")],
}
SYNCED = {
    "status": "synced",
    "report": "report by the sync agent",
    **described("the sync agent"),
    "defects": [defect("the sync agent")],
}
NARROWED = {
    "followups": [{"title": "t", "description": "d"}],
    **described("the narrowing agent"),
    "defects": [defect("the narrowing agent")],
}


def fixed(n, gave_up=False, withdrawn=()):
    """The result of fixer n, which reports one defect of its own.

    The withdrawn argument holds the numbers of the defects so far that
    the fixer withdraws.
    """
    return {
        "responses": [{"finding": 1, "action": "fixed", "detail": "d"}],
        "gave_up": gave_up,
        **described(f"fixer {n}"),
        "defects": [defect(f"fixer {n}")],
        "withdrawn_defects": list(withdrawn),
    }


@unittest.skipUnless(NODE, "node is not on PATH")
class WorkflowCase(unittest.TestCase):
    """The base of each test that runs close-task.js under Node."""

    def label(self, short):
        """Turn a short label such as "fix r1" into the label of the script."""
        kind, _, rest = short.partition(" ")
        return f"{kind} {REF} {rest}".strip()

    def run_workflow(
        self, agents, rounds=6, resume=None, args=None, models=None, landing=None
    ):
        """Run close-task.js with scripted agents.

        The agents argument maps each short agent label to the result of
        that agent, in the order that the script must call them. The args
        argument holds the input of the script as `ultraclose.py` printed
        it, and the driver then runs the copy of the script that the input
        names. Without it, the input names a task outside any run, with
        the given rounds and resume field. That input holds the models
        field only when the models argument is not None. The landing
        argument holds the pr field of that input and the branch of its
        task, and the input holds neither field without it. Return the
        result of the script and each agent call by short label. Each call
        holds the label, the prompt, the schema, and the sorted names of
        the options of the agent. It holds the model only when the call
        has one.
        """
        task = {
            "ref": REF,
            "title": "t",
            "wt": "/repo/wt",
            "dir": f"/run/tasks/{REF}",
        }
        if resume:
            task["resume"] = resume
        outside = {
            "run": "/run",
            "repo": "/repo",
            "base": "main",
            "rounds": rounds,
            "task": task,
        }
        if models is not None:
            outside["models"] = models
        if landing is not None:
            outside["pr"], task["branch"] = landing
        scenario = {
            "args": args or outside,
            "agents": {self.label(short): r for short, r in agents.items()},
        }
        script = Path(args["script"]) if args else WORKFLOW
        body = script.read_text().replace("export const meta", "const meta", 1)
        with tempfile.TemporaryDirectory() as tmp:
            driver = Path(tmp, "driver.js")
            driver.write_text(DRIVER_HEAD + body + DRIVER_TAIL)
            p = subprocess.run(
                [NODE, str(driver)],
                input=json.dumps(scenario),
                capture_output=True,
                text=True,
                check=False,
            )
        self.assertEqual(p.returncode, 0, p.stderr)
        ran = json.loads(p.stdout)
        self.assertEqual([c["label"] for c in ran["calls"]], list(scenario["agents"]))
        return ran["result"], dict(zip(agents, ran["calls"]))

    def every_agent(self, models=None, landing=None):
        """Return one call of each agent that the script starts, by short
        label.

        The models argument holds the models field of the input, or None
        for an input without the field. The landing argument holds the pr
        field of the input and the branch of its task, or None for an
        input without the two fields.
        """
        # The first run reaches every agent except the sync agent.
        _, calls = self.run_workflow(
            {
                "impl": IMPLEMENTED,
                "review r1": CHANGES,
                "fix r1": fixed(1),
                "review final": CHANGES,
                "narrow": NARROWED,
            },
            rounds=1,
            models=models,
            landing=landing,
        )
        _, synced = self.run_workflow(
            {"sync": SYNCED, "review r2": APPROVE},
            resume=SYNC,
            models=models,
            landing=landing,
        )
        calls["sync"] = synced["sync"]
        return calls


class WorkflowRulesTest(WorkflowCase):
    """The ground rules that the prompt of every agent carries."""

    def test_every_agent_learns_who_handles_user_messages(self):
        calls = self.every_agent()
        self.assertEqual(
            set(calls),
            {"impl", "sync", "review r1", "fix r1", "review final", "narrow"},
        )
        # The rule says who handles a message of the user, and what a
        # relayed message does not change.
        rule = (
            "The orchestrator of this run reads and handles every message "
            "that the user sends during the run",
            "does not cancel, replace, or narrow your assignment",
        )
        for label, call in calls.items():
            with self.subTest(label):
                for sentence in rule:
                    self.assertIn(sentence, call["prompt"])

    def test_every_agent_learns_how_many_commands_to_run_at_once(self):
        calls = self.every_agent()
        self.assertEqual(
            set(calls),
            {"impl", "sync", "review r1", "fix r1", "review final", "narrow"},
        )
        # The agents of a run share the memory of one machine. The rule
        # limits the commands that one agent runs at the same time, and
        # forbids load that the agent creates. It also says how to
        # reproduce a failure that appears only under load.
        rule = (
            "Run at most two build, test, or probe commands at the same time.",
            "Never create load on the machine to reproduce a failure.",
            "do not run copies of a test at the same time with "
            "`rust-parallel`, `xargs -P`, or background jobs, and do not "
            "run a stress tool.",
            "repeat the test in sequence, or limit the test to one CPU",
        )
        for label, call in calls.items():
            with self.subTest(label):
                for sentence in rule:
                    self.assertIn(sentence, call["prompt"])

    def test_sync_agent_keeps_branch_name_out_of_merge_commit(self):
        # The default message of a merge names the task branch, whose
        # name holds the task ref. A pull request publishes the commit.
        _, calls = self.run_workflow(
            {"sync": SYNCED, "review r2": APPROVE}, resume=SYNC
        )
        self.assertIn(
            '`cd /repo/wt && git merge --no-verify -m "Merge main" main`',
            calls["sync"]["prompt"],
        )

    def test_every_agent_learns_its_branch_and_how_the_change_lands(self):
        commits = "The orchestrator writes the real commit on main later"
        proposes = "The orchestrator opens a pull request against main later."
        # name: (the landing argument of every_agent, the branch that the
        # rules name, the sentence that the rules hold, the sentence that
        # they leave out)
        cases = {
            "an input without the fields": (None, f"task/{REF}", commits, proposes),
            "a run that commits on its base branch": (
                (False, f"task/{REF}"),
                f"task/{REF}",
                commits,
                proposes,
            ),
            "a run that proposes pull requests": (
                (True, f"task/{REF}"),
                f"task/{REF}",
                proposes,
                commits,
            ),
            "a task whose landing named its branch": (
                (True, "fix/retry-a-timeout"),
                "fix/retry-a-timeout",
                proposes,
                commits,
            ),
        }
        for name, (landing, branch, held, left_out) in cases.items():
            calls = self.every_agent(landing=landing)
            self.assertEqual(
                set(calls),
                {"impl", "sync", "review r1", "fix r1", "review final", "narrow"},
            )
            for label, call in calls.items():
                with self.subTest(name, agent=label):
                    self.assertIn(
                        f"on branch {branch}, which was cut from main.",
                        call["prompt"],
                    )
                    self.assertIn(held, call["prompt"])
                    self.assertNotIn(left_out, call["prompt"])
                    # The message file holds the title and the body of
                    # the pull request.
                    if held == proposes:
                        self.assertIn(
                            f"The first line of the message in /run/tasks/{REF}"
                            "/msg.txt becomes its title, and the rest of the"
                            " message becomes its body.",
                            call["prompt"],
                        )


class WorkflowModelTest(WorkflowCase):
    """Each agent runs on the model that the input names for its role."""

    # The role of each agent, by the first word of its label.
    ROLE = {
        "impl": "implementer",
        "sync": "sync",
        "review": "reviewer",
        "fix": "fixer",
        "narrow": "narrow",
    }
    # The options that the call of every agent carries.
    OPTIONS = ["label", "phase", "schema"]

    def assert_models(self, calls, models):
        """Each call carries the model of its role, by short label.

        An agent call without a model option follows the session model.
        So the call of a role without a model carries no such option.
        """
        for label, call in calls.items():
            with self.subTest(label):
                want = models.get(self.ROLE[label.split()[0]])
                self.assertEqual(call.get("model"), want)
                named = ["model"] if want else []
                self.assertEqual(call["options"], sorted(self.OPTIONS + named))

    def test_each_agent_runs_on_model_of_its_role(self):
        # name: the models field of the input, or None for an input
        # without the field
        cases = {
            "an input without the field": None,
            "no role": {},
            "every role": {role: f"model-of-{role}" for role in ROLES},
            "the implementer": {"implementer": FABLE},
            "the reviewer and the fixer": {"reviewer": OPUS, "fixer": FABLE},
            "the sync agent and the narrowing agent": {"sync": OPUS, "narrow": FABLE},
        }
        for name, models in cases.items():
            with self.subTest(name):
                calls = self.every_agent(models)
                self.assertEqual(
                    set(calls),
                    {"impl", "sync", "review r1", "fix r1", "review final", "narrow"},
                )
                self.assert_models(calls, models or {})

    def test_retry_runs_on_model_of_its_role(self):
        models = {"implementer": FABLE, "reviewer": OPUS}
        _, calls = self.run_workflow(
            {
                "impl": None,
                "impl retry": IMPLEMENTED,
                "review r1": None,
                "review r1 retry": CHANGES,
                "fix r1": None,
                "fix r1 retry": fixed(1),
                "review r2": APPROVE,
            },
            models=models,
        )
        self.assert_models(calls, models)

    def test_agents_run_on_models_of_run(self):
        # The input that `ultraclose.py` prints carries the models of the
        # run config to the agents of the copy of the script. So a
        # relaunch runs on the models of the run, and on each model that
        # the config command changed.
        with tempfile.TemporaryDirectory() as tmp:
            flags = model_flags({"implementer": FABLE, DEFAULT_ROLE: OPUS})
            repo, env, launch = running_task(tmp, flags=flags)
            first = {**dict.fromkeys(ROLES, OPUS), "implementer": FABLE}
            agents = {"impl": IMPLEMENTED, "review r1": CHANGES, "fix r1": fixed(1)}
            _, calls = self.run_workflow({**agents, "review r2": APPROVE}, args=launch)
            self.assert_models(calls, first)
            ultraclose("config", "--model=reviewer=", cwd=repo, env=env)
            ultraclose("config", f"--model=fixer={FABLE}", cwd=repo, env=env)
            relaunch = json.loads(ultraclose("args", REF, cwd=repo, env=env))
            _, calls = self.run_workflow(
                {"review r1": CHANGES, "fix r1": fixed(1), "review r2": APPROVE},
                args=relaunch,
            )
            self.assert_models(calls, {**first, "fixer": FABLE})
            ultraclose("config", f"--model={DEFAULT_ROLE}=", cwd=repo, env=env)
            relaunch = json.loads(ultraclose("args", REF, cwd=repo, env=env))
            _, calls = self.run_workflow(
                {"review r1": CHANGES, "fix r1": fixed(1), "review r2": APPROVE},
                args=relaunch,
            )
            self.assert_models(calls, {"implementer": FABLE, "fixer": FABLE})

    def test_final_relaunch_runs_narrowing_agent_on_corrected_model(self):
        # A narrowing agent that returns nothing ends its task as
        # unconverged, not as error. After a config command corrects the
        # model of the narrow role, args --final relaunches the task. The
        # narrowing agent of the relaunch runs on the corrected model.
        with tempfile.TemporaryDirectory() as tmp:
            wrong = "claude-opsu-5-5"
            flags = ["--rounds=1", *model_flags({"narrow": wrong})]
            repo, env, launch = running_task(tmp, flags=flags)
            capped = {"review r1": CHANGES, "fix r1": fixed(1), "review final": CHANGES}
            result, calls = self.run_workflow(
                {"impl": IMPLEMENTED, **capped, "narrow": None, "narrow retry": None},
                args=launch,
            )
            self.assertEqual(result["status"], "unconverged")
            self.assertIn("the narrowing agent returned nothing", result["notes"])
            self.assert_models(calls, {"narrow": wrong})
            ultraclose("config", f"--model=narrow={OPUS}", cwd=repo, env=env)
            relaunch = json.loads(ultraclose("args", REF, "--final", cwd=repo, env=env))
            result, calls = self.run_workflow(
                {**capped, "narrow": NARROWED}, args=relaunch
            )
            self.assertEqual(result["status"], "landable")
            self.assert_models(calls, {"narrow": OPUS})

    def test_sync_relaunch_runs_sync_agent_on_corrected_model(self):
        # A sync agent that returns nothing ends its task as error. Then a
        # config command corrects the model of the sync role. Only a
        # relaunch with the --sync reason starts a sync agent, and that
        # agent runs on the corrected model. A relaunch without the reason
        # goes straight to the review of the unsynced branch.
        reason = "the merge conflicts"
        with tempfile.TemporaryDirectory() as tmp:
            wrong = "claude-opsu-5-5"
            repo, env, _ = running_task(tmp, flags=model_flags({"sync": wrong}))
            launch = json.loads(
                ultraclose("args", REF, "--sync", reason, cwd=repo, env=env)
            )
            result, calls = self.run_workflow(
                {"sync": None, "sync retry": None}, args=launch
            )
            self.assertEqual(result["status"], "error")
            self.assertIn("the sync agent returned nothing", result["notes"])
            self.assert_models(calls, {"sync": wrong})
            ultraclose("config", f"--model=sync={OPUS}", cwd=repo, env=env)
            relaunch = json.loads(ultraclose("args", REF, cwd=repo, env=env))
            _, calls = self.run_workflow({"review r1": APPROVE}, args=relaunch)
            self.assertNotIn("sync", calls)
            relaunch = json.loads(
                ultraclose("args", REF, "--sync", reason, cwd=repo, env=env)
            )
            result, calls = self.run_workflow(
                {"sync": SYNCED, "review r1": APPROVE}, args=relaunch
            )
            self.assertEqual(result["status"], "approved")
            self.assertIn(reason, calls["sync"]["prompt"])
            self.assert_models(calls, {"sync": OPUS})


class WorkflowRegressionTest(WorkflowCase):
    """What a reviewer reports as a regression."""

    def test_reviewers_learn_what_a_regression_is(self):
        calls = self.every_agent()
        # The scripted run names its base branch main. A fault in added
        # code is a regression only when it is a fault in behavior.
        rule = (
            "A finding is a regression only when it reports a fault in "
            "behavior that the change introduced",
            "A fault that main also has is not a regression.",
            "A missing or weak test leaves the behavior as it is, and so "
            "does a doc, comment, or convention fault.",
            "None of these faults is a regression, even in code that the "
            "change added.",
            "Text that an agent acts on is behavior, so a wrong instruction "
            "in a prompt, a skill, an agent definition, a CLAUDE.md file, or "
            "other such text is a fault in behavior.",
        )
        for label in ("review r1", "review final"):
            with self.subTest(label):
                call = calls[label]
                finding = call["schema"]["properties"]["findings"]["items"]
                field = finding["properties"]["regression"]["description"]
                for sentence in rule:
                    # The reviewer reads the rule in the prompt and beside
                    # the field that it sets.
                    self.assertIn(sentence, call["prompt"])
                    self.assertIn(sentence, field)


class WorkflowReviewScopeTest(WorkflowCase):
    """How much of the change each reviewer reviews.

    A reviewer of the loop reviews the whole diff. The final reviewer
    checks only the findings of the last round and their fixes.
    """

    def test_reviewers_learn_how_much_to_review(self):
        proof = "the proof that the finding is wrong"
        rebuts = {
            **fixed(1),
            "responses": [{"finding": 1, "action": "rebutted", "detail": proof}],
        }
        # The notes that the prompt of a reviewer can carry. The scripted
        # run names its base branch main.
        notes = {
            "history": (
                "Earlier review rounds already happened, and "
                f"/run/tasks/{REF}/history.md records each finding with the "
                "fixer's answer. Read it. Verify each claimed fix in the "
                "code. Do not raise a rebutted finding again unless the "
                "rebuttal is wrong, and then say why."
            ),
            "rebuttals": f"Rebuttals so far:\n- a.py:1: p\n  Rebuttal: {proof}\n",
            "afresh": (
                "Then review the whole diff afresh, because fixes introduce "
                "new faults."
            ),
            "sync": "Review the whole diff against main again.",
            "bounded": (
                "This review is narrow. A fixer applied only the exact fixes "
                "of the findings below. Do not review the whole diff again. "
                "In the review steps, check only these findings and what "
                "their fixes changed. Report a finding only when it is one "
                "of these findings and it is still open, or when one of "
                "these fixes introduced it.\n"
                "1. [major] a.py:1\n   Problem: p\n   Evidence: e\n   Fix: f\n"
            ),
        }
        # The final reviewer reads the history and the rebuttals, but no
        # sentence that asks for a review of the whole diff.
        loop = {"history", "rebuttals", "afresh"}
        final = {"history", "rebuttals", "bounded"}
        # name: (run_workflow arguments, label: the notes of the prompt)
        cases = {
            "a cap at the first round": (
                {
                    "agents": {
                        "impl": IMPLEMENTED,
                        "review r1": CHANGES,
                        "fix r1": fixed(1),
                        "review final": APPROVE,
                    },
                    "rounds": 1,
                },
                {"review r1": set(), "review final": {"history", "bounded"}},
            ),
            "a cap at a later round": (
                {
                    "agents": {
                        "impl": IMPLEMENTED,
                        "review r1": CHANGES,
                        "fix r1": rebuts,
                        "review r2": CHANGES,
                        "fix r2": fixed(2),
                        "review final": APPROVE,
                    },
                    "rounds": 2,
                },
                {"review r1": set(), "review r2": loop, "review final": final},
            ),
            "a cap after a sync": (
                {
                    "agents": {
                        "sync": SYNCED,
                        "review r2": CHANGES,
                        "fix r2": rebuts,
                        "review r3": CHANGES,
                        "fix r3": fixed(3),
                        "review final": APPROVE,
                    },
                    "resume": SYNC,
                },
                {
                    "review r2": {"history", "afresh", "sync"},
                    "review r3": loop | {"sync"},
                    "review final": final,
                },
            ),
        }
        for name, (kwargs, want) in cases.items():
            _, calls = self.run_workflow(**kwargs)
            for label, carried in want.items():
                prompt = calls[label]["prompt"]
                for note, text in notes.items():
                    with self.subTest(f"{name}, {label}, {note}"):
                        self.assertEqual(text in prompt, note in carried)


class WorkflowResultTest(WorkflowCase):
    """The summary, the notes, and the defects of a result.

    The summary and the notes follow the last change. A relaunch starts
    from the summary and the notes that the earlier launch stored. The
    defects add up over the agents of one launch.
    """

    def test_result_describes_last_change(self):
        first_fix = {"impl": IMPLEMENTED, "review r1": CHANGES, "fix r1": fixed(1)}
        rejected = {"summary": fixed(1)["summary"], "notes": REJECT["reject_reason"]}
        # name: (run_workflow arguments, status, the summary and the notes)
        cases = {
            "the first review approves": (
                {"agents": {"impl": IMPLEMENTED, "review r1": APPROVE}},
                "approved",
                IMPLEMENTED,
            ),
            "a fix round": (
                {"agents": {**first_fix, "review r2": APPROVE}},
                "approved",
                fixed(1),
            ),
            "two fix rounds": (
                {
                    "agents": {
                        **first_fix,
                        "review r2": CHANGES,
                        "fix r2": fixed(2),
                        "review r3": APPROVE,
                    }
                },
                "approved",
                fixed(2),
            ),
            "the final cycle approves": (
                {"agents": {**first_fix, "review final": APPROVE}, "rounds": 1},
                "approved",
                fixed(1),
            ),
            "the change lands with open findings": (
                {
                    "agents": {
                        **first_fix,
                        "review final": CHANGES,
                        "narrow": NARROWED,
                    },
                    "rounds": 1,
                },
                "landable",
                NARROWED,
            ),
            "a fixer gives up": (
                {
                    "agents": {
                        "impl": IMPLEMENTED,
                        "review r1": CHANGES,
                        "fix r1": fixed(1, gave_up=True),
                    }
                },
                "abandoned",
                fixed(1),
            ),
            "the implementer finds the task obsolete": (
                {"agents": {"impl": {**IMPLEMENTED, "status": "obsolete"}}},
                "obsolete",
                IMPLEMENTED,
            ),
            "a review rejects after a fix round": (
                {"agents": {**first_fix, "review r2": REJECT}},
                "rejected",
                rejected,
            ),
            "the final review leaves a change that cannot land": (
                {"agents": {**first_fix, "review final": REJECT}, "rounds": 1},
                "unconverged",
                rejected,
            ),
            "a reviewer returns nothing after a fix round": (
                {
                    "agents": {
                        **first_fix,
                        "review r2": None,
                        "review r2 retry": None,
                    }
                },
                "error",
                {
                    "summary": fixed(1)["summary"],
                    "notes": "the reviewer of round 2 returned nothing",
                },
            ),
            "a sync agent abandons the task": (
                {
                    "agents": {"sync": {**SYNCED, "status": "abandoned"}},
                    "resume": SYNC,
                },
                "abandoned",
                SYNCED,
            ),
            "a relaunch with a fix round": (
                {
                    "agents": {
                        "review r2": CHANGES,
                        "fix r2": fixed(2),
                        "review r3": APPROVE,
                    },
                    "resume": RELAUNCH,
                },
                "approved",
                fixed(2),
            ),
            "a relaunch that the first review approves": (
                {"agents": {"review r2": APPROVE}, "resume": RELAUNCH},
                "approved",
                STORED,
            ),
            "a relaunch of a task that stored nothing": (
                {
                    "agents": {"review r2": APPROVE},
                    "resume": {**RELAUNCH, **UNSTORED},
                },
                "approved",
                UNSTORED,
            ),
            "a relaunch that the first review rejects": (
                {"agents": {"review r2": REJECT}, "resume": RELAUNCH},
                "rejected",
                {"summary": STORED["summary"], "notes": REJECT["reject_reason"]},
            ),
            "a sync that the review approves": (
                {
                    "agents": {"sync": SYNCED, "review r2": APPROVE},
                    "resume": SYNC,
                },
                "approved",
                SYNCED,
            ),
            "a sync agent returns nothing": (
                {"agents": {"sync": None, "sync retry": None}, "resume": SYNC},
                "error",
                {
                    "summary": STORED["summary"],
                    "notes": "the sync agent returned nothing",
                },
            ),
        }
        for name, (kwargs, status, want) in cases.items():
            with self.subTest(name):
                result, _ = self.run_workflow(**kwargs)
                self.assertEqual(
                    (result["status"], result["summary"], result["notes"]),
                    (status, want["summary"], want["notes"]),
                )

    def test_relaunch_returns_what_the_agents_stored(self):
        # The implementer stores its summary and notes, and its launch
        # returns no result. The input that the args command prints
        # carries both to the next launch.
        with tempfile.TemporaryDirectory() as tmp:
            repo, env, launch = running_task(tmp)
            _, calls = self.run_workflow(
                {"impl": IMPLEMENTED, "review r1": APPROVE}, args=launch
            )
            self.assertIn(store_step(launch["task"]["dir"]), calls["impl"]["prompt"])
            for key, name in STORED_FILES.items():
                Path(launch["task"]["dir"], name).write_text(IMPLEMENTED[key] + "\n")
            relaunch = json.loads(ultraclose("args", REF, cwd=repo, env=env))
            result, _ = self.run_workflow({"review r1": APPROVE}, args=relaunch)
            self.assertEqual(
                (result["status"], result["summary"], result["notes"]),
                ("approved", IMPLEMENTED["summary"], IMPLEMENTED["notes"]),
            )

    def test_agents_receive_last_summary_and_notes(self):
        both, review = ["summary", "notes"], ["summary"]
        fix_round = {"review r2": CHANGES, "fix r2": fixed(2), "review r3": APPROVE}
        # name: (run_workflow arguments, label: (the author whose words
        # the prompt carries, the fields))
        cases = {
            "a first launch": (
                {
                    "agents": {
                        "impl": IMPLEMENTED,
                        "review r1": CHANGES,
                        "fix r1": fixed(1),
                        "review r2": CHANGES,
                        "fix r2": fixed(2),
                        "review final": CHANGES,
                        "narrow": NARROWED,
                    },
                    "rounds": 2,
                },
                {
                    "review r1": (IMPLEMENTED, review),
                    "fix r1": (IMPLEMENTED, both),
                    "review r2": (fixed(1), review),
                    "fix r2": (fixed(1), both),
                    "review final": (fixed(2), review),
                    "narrow": (fixed(2), both),
                },
            ),
            "a relaunch": (
                {"agents": fix_round, "resume": RELAUNCH},
                {
                    "review r2": (STORED, review),
                    "fix r2": (STORED, both),
                    "review r3": (fixed(2), review),
                },
            ),
            "a sync": (
                {"agents": {"sync": SYNCED, **fix_round}, "resume": SYNC},
                {
                    "sync": (STORED, both),
                    "review r2": (SYNCED, review),
                    "fix r2": (SYNCED, both),
                    "review r3": (fixed(2), review),
                },
            ),
        }
        authors = [STORED, IMPLEMENTED, SYNCED, fixed(1), fixed(2), NARROWED]
        for name, (kwargs, want) in cases.items():
            _, calls = self.run_workflow(**kwargs)
            for label, (author, fields) in want.items():
                with self.subTest(f"{name}, {label}"):
                    prompt = calls[label]["prompt"]
                    for field in fields:
                        self.assertIn(author[field], prompt)
                    # No prompt carries the words of an earlier or a later
                    # author.
                    for other in authors:
                        if other != author:
                            self.assertNotIn(other["summary"], prompt)
                            self.assertNotIn(other["notes"], prompt)

    def test_reviewer_receives_report_of_sync_agent(self):
        _, calls = self.run_workflow(
            {"sync": SYNCED, "review r2": APPROVE}, resume=SYNC
        )
        # The summary of the sync agent describes the whole change, so its
        # account of the merge takes a field of its own.
        sync, review = calls["sync"], calls["review r2"]
        self.assertIn("report", sync["schema"]["required"])
        self.assertIn("Return in report what the merge required", sync["prompt"])
        self.assertIn(f"Its report: {SYNCED['report']}\n", review["prompt"])

    def test_agents_that_change_the_work_return_and_store_summary_and_notes(self):
        calls = self.every_agent()
        # label: whether the result of the agent carries the summary and
        # the notes of the change
        want = {
            "impl": True,
            "sync": True,
            "fix r1": True,
            "narrow": True,
            "review r1": False,
            "review final": False,
        }
        self.assertEqual(set(calls), set(want))
        for label, returns in want.items():
            with self.subTest(label):
                schema, prompt = calls[label]["schema"], calls[label]["prompt"]
                self.assertEqual(
                    {"summary", "notes"} <= set(schema["required"]), returns
                )
                # An agent that returns both also stores both, in the files
                # that the args command reads.
                store = store_step(f"/run/tasks/{REF}")
                self.assertEqual("Stored summary and notes:" in prompt, returns)
                self.assertEqual(store in prompt, returns)
                if not returns:
                    continue
                # The agent stores both in the step right before it commits
                # the work in progress, the way it writes msg.txt before
                # that commit. So an agent that stops after its commit
                # leaves text that describes the commit.
                commit = "Commit the work in progress."
                self.assertEqual(prompt.count(commit), 1)
                self.assertEqual(prompt.count(store), 1)
                self.assertRegex(
                    prompt, re.escape(store) + r"\n\d+\. " + re.escape(commit)
                )

    def test_result_lists_defects_of_each_agent(self):
        first_fix = {"impl": IMPLEMENTED, "review r1": CHANGES, "fix r1": fixed(1)}
        second_fix = {**first_fix, "review r2": CHANGES}
        relaunched_fix = {"review r2": CHANGES, "fix r2": fixed(2)}
        # name: (run_workflow arguments, status, the agents whose defects
        # the result lists)
        cases = {
            "the first review approves": (
                {"agents": {"impl": IMPLEMENTED, "review r1": APPROVE}},
                "approved",
                ["the implementer"],
            ),
            "the implementer reports no defect": (
                {
                    "agents": {
                        "impl": {**IMPLEMENTED, "defects": []},
                        "review r1": APPROVE,
                    }
                },
                "approved",
                [],
            ),
            "a fixer adds a defect": (
                {"agents": {**first_fix, "review r2": APPROVE}},
                "approved",
                ["the implementer", "fixer 1"],
            ),
            "each fixer adds a defect": (
                {"agents": {**second_fix, "fix r2": fixed(2), "review r3": APPROVE}},
                "approved",
                ["the implementer", "fixer 1", "fixer 2"],
            ),
            "a fixer withdraws the defect of the implementer": (
                {
                    "agents": {
                        "impl": IMPLEMENTED,
                        "review r1": CHANGES,
                        "fix r1": fixed(1, withdrawn=[1]),
                        "review r2": APPROVE,
                    }
                },
                "approved",
                ["fixer 1"],
            ),
            "a fixer withdraws the defect of an earlier fixer": (
                {
                    "agents": {
                        **second_fix,
                        "fix r2": fixed(2, withdrawn=[2]),
                        "review r3": APPROVE,
                    }
                },
                "approved",
                ["the implementer", "fixer 2"],
            ),
            # Number 2 names no defect so far. The defect that the fixer
            # adds takes that number only in the next round.
            "a fixer withdraws a number that the list lacks": (
                {
                    "agents": {
                        "impl": IMPLEMENTED,
                        "review r1": CHANGES,
                        "fix r1": fixed(1, withdrawn=[0, 2]),
                        "review r2": APPROVE,
                    }
                },
                "approved",
                ["the implementer", "fixer 1"],
            ),
            "the change lands with open findings": (
                {
                    "agents": {
                        **first_fix,
                        "review final": CHANGES,
                        "narrow": NARROWED,
                    },
                    "rounds": 1,
                },
                "landable",
                ["the implementer", "fixer 1", "the narrowing agent"],
            ),
            "a fixer gives up": (
                {
                    "agents": {
                        "impl": IMPLEMENTED,
                        "review r1": CHANGES,
                        "fix r1": fixed(1, gave_up=True),
                    }
                },
                "abandoned",
                ["the implementer", "fixer 1"],
            ),
            "the implementer finds the task obsolete": (
                {"agents": {"impl": {**IMPLEMENTED, "status": "obsolete"}}},
                "obsolete",
                ["the implementer"],
            ),
            "the implementer abandons the task": (
                {"agents": {"impl": {**IMPLEMENTED, "status": "abandoned"}}},
                "abandoned",
                ["the implementer"],
            ),
            "a review rejects after a fix round": (
                {"agents": {**first_fix, "review r2": REJECT}},
                "rejected",
                ["the implementer", "fixer 1"],
            ),
            "the final review leaves a change that cannot land": (
                {"agents": {**first_fix, "review final": REJECT}, "rounds": 1},
                "unconverged",
                ["the implementer", "fixer 1"],
            ),
            "a fixer returns nothing": (
                {
                    "agents": {
                        "impl": IMPLEMENTED,
                        "review r1": CHANGES,
                        "fix r1": None,
                        "fix r1 retry": None,
                    }
                },
                "error",
                ["the implementer"],
            ),
            "a sync agent abandons the task": (
                {
                    "agents": {"sync": {**SYNCED, "status": "abandoned"}},
                    "resume": SYNC,
                },
                "abandoned",
                ["the sync agent"],
            ),
            "a sync that the review approves": (
                {
                    "agents": {"sync": SYNCED, "review r2": APPROVE},
                    "resume": SYNC,
                },
                "approved",
                ["the sync agent"],
            ),
            "a sync with a fix round": (
                {
                    "agents": {
                        "sync": SYNCED,
                        **relaunched_fix,
                        "review r3": APPROVE,
                    },
                    "resume": SYNC,
                },
                "approved",
                ["the sync agent", "fixer 2"],
            ),
            "a relaunch that the first review approves": (
                {"agents": {"review r2": APPROVE}, "resume": RELAUNCH},
                "approved",
                [],
            ),
            "a relaunch with a fix round": (
                {
                    "agents": {**relaunched_fix, "review r3": APPROVE},
                    "resume": RELAUNCH,
                },
                "approved",
                ["fixer 2"],
            ),
        }
        for name, (kwargs, status, authors) in cases.items():
            with self.subTest(name):
                result, _ = self.run_workflow(**kwargs)
                self.assertEqual(
                    (result["status"], result["defects"]),
                    (status, [defect(author) for author in authors]),
                )

    def test_agents_receive_defects_so_far(self):
        _, calls = self.run_workflow(
            {
                "impl": IMPLEMENTED,
                "review r1": CHANGES,
                "fix r1": fixed(1),
                "review r2": CHANGES,
                "fix r2": fixed(2, withdrawn=[1]),
                "review r3": CHANGES,
                "fix r3": fixed(3),
                "review final": CHANGES,
                "narrow": NARROWED,
            },
            rounds=3,
        )
        # label: the agents whose defects the prompt lists, by number
        want = {
            "fix r1": ["the implementer"],
            "fix r2": ["the implementer", "fixer 1"],
            "fix r3": ["fixer 1", "fixer 2"],
            "narrow": ["fixer 1", "fixer 2", "fixer 3"],
        }
        for label, authors in want.items():
            with self.subTest(label):
                self.assertIn(listed(*authors), calls[label]["prompt"])

    def test_agents_that_report_defects_learn_the_rule(self):
        calls = self.every_agent()
        # label: the fields for defects that the schema of the agent requires
        want = {
            "impl": {"defects"},
            "sync": {"defects"},
            "fix r1": {"defects", "withdrawn_defects"},
            "review r1": set(),
            "review final": set(),
            "narrow": {"defects"},
        }
        self.assertEqual(set(calls), set(want))
        for label, fields in want.items():
            with self.subTest(label):
                schema, prompt = calls[label]["schema"], calls[label]["prompt"]
                self.assertEqual(
                    {f for f in schema["properties"] if "defects" in f}, fields
                )
                self.assertLessEqual(fields, set(schema["required"]))
                # An agent learns the rule only when its result has the field.
                self.assertEqual("Defects outside the task:" in prompt, bool(fields))
                if fields:
                    self.assertEqual(
                        schema["properties"]["defects"]["items"]["required"],
                        ["title", "description"],
                    )

    def test_agents_after_relaunch_learn_what_exists(self):
        fix_round = {"review r2": CHANGES, "fix r2": fixed(2), "review r3": APPROVE}
        synced = {"sync": SYNCED, **fix_round}
        nothing = {"summary": "none", "notes": "none"}
        # name: (run_workflow arguments, the agent, the summary and the
        # notes that it receives, the agents whose defects it receives or
        # None for an agent that receives no defects)
        cases = {
            "the fixer of a relaunch": (
                {"agents": fix_round, "resume": RELAUNCH},
                "fix r2",
                STORED,
                [],
            ),
            "the fixer of a relaunch that stored nothing": (
                {"agents": fix_round, "resume": {**RELAUNCH, **UNSTORED}},
                "fix r2",
                nothing,
                [],
            ),
            "the sync agent": (
                {"agents": synced, "resume": SYNC},
                "sync",
                STORED,
                None,
            ),
            "the sync agent of a task that stored nothing": (
                {"agents": synced, "resume": {**SYNC, **UNSTORED}},
                "sync",
                nothing,
                None,
            ),
            "the fixer after a sync": (
                {"agents": synced, "resume": SYNC},
                "fix r2",
                SYNCED,
                ["the sync agent"],
            ),
        }
        for name, (kwargs, label, want, reporters) in cases.items():
            with self.subTest(name):
                _, calls = self.run_workflow(**kwargs)
                prompt = calls[label]["prompt"]
                self.assertIn(
                    f"The summary of the change so far:\n{want['summary']}\n", prompt
                )
                self.assertIn(
                    f"The notes of the change so far:\n{want['notes']}\n", prompt
                )
                if reporters is not None:
                    self.assertIn(listed(*reporters), prompt)


if __name__ == "__main__":
    unittest.main()
