#!/usr/bin/env python3
"""Tests for ultraclose.py and close-task.js.

Run with: python3 test_ultraclose.py. The close-task.js tests run the
script under Node, and they skip when node is not on PATH.
"""

import itertools
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("ultraclose.py")
WORKFLOW = Path(__file__).with_name("close-task.js")
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


def clean_env():
    """The environment without the GIT_* variables of a calling git hook."""
    return {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}


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
# edit.fails. Every other command prints one word.
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


def running_task(tmp, commits=1):
    """Start a run with one running task.

    The task branch holds the given number of commits. The args command
    resumes a task at a review round only when its branch holds a commit.
    Return the repository, the environment, and the Workflow input that
    next printed for the task.
    """
    repo, env = launchable_repo(tmp)
    ultraclose("init", "--run", str(Path(tmp, "run")), cwd=repo, env=env)
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


def configured_run(repo, env, run):
    """Start a run that sets each config value that a batch inherits.

    The run sets a limit, a check, a health command, and a shared glob.
    """
    ultraclose(
        "init",
        "--run",
        str(run),
        "--limit",
        "8",
        "--check",
        "sh check.sh",
        "--health",
        "docker version",
        "--shared",
        GLOB,
        cwd=repo,
        env=env,
    )


REF = "APP-7KQ9"
# The shared glob of a configured run, and a file that the glob covers.
GLOB = "*/tools/mcp/*"
COVERED = "tools/mcp/render.go"
# The sentence that names the glob of a file at the root of the repository.
# The help of each --shared flag prints it, and so does each warning about
# a shared glob that matches no file.
ROOT_GLOB = "/README.md matches the README.md at the root of the repository"
# The line that status prints for a run that has ended, and the line that
# watch prints for it.
ENDED = "ended: no task is pending, inflight, or approved"
WATCH_ENDED = "ENDED no task is pending, inflight, or approved"
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
UNFINISHED = ("pending", "inflight", "approved")
FINISHED = ("landed", "noted", "closed")
# The files of a task directory that hold the summary and the notes of the
# change, by the key of the resume field that carries each one. The agents
# of close-task.js write the files, and the args command reads them.
STORED_FILES = {"summary": "summary.txt", "notes": "notes.txt"}
# The directory that holds a golangci-lint cache of one checkout.
LINT_CACHE = "golangci-lint-cache"


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

    def first_run(self, tmp, task_state=None):
        """Start a configured run, and return the repo, the env, and the run.

        The run holds one task in the given state, or no task.
        """
        repo, env = new_repo(tmp)
        run = Path(tmp, "first").resolve()
        configured_run(repo, env, run)
        if task_state:
            triage(tmp, repo, env, {"ref": REF})
            ultraclose("set", REF, task_state, cwd=repo, env=env)
        return repo, env, run

    def test_replaces_ended_run(self):
        # name: (the state of the task of the first run, the flags of the
        # second init)
        cases = {"a run with no task": (None, [])}
        cases |= {f"a run with one {s} task": (s, []) for s in FINISHED}
        cases |= {f"--force and one {s} task": (s, ["--force"]) for s in UNFINISHED}
        for name, (task_state, flags) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, first = self.first_run(tmp, task_state)
                before = state_of(first)
                second = Path(tmp, "second").resolve()
                out = ultraclose(
                    "init", "--run", str(second), *flags, cwd=repo, env=env
                )
                self.assertIn(f"replaced the run at {first}\n", out)
                # The new run takes no task and no config from the first run.
                state = state_of(second)
                self.assertEqual(state["tasks"], {})
                keys = ("limit", "check", "health", "shared")
                self.assertEqual(
                    {k: state["config"][k] for k in keys},
                    {"limit": 5, "check": "", "health": "", "shared": DEFAULT_SHARED},
                )
                status = ultraclose("status", cwd=repo, env=env)
                self.assertEqual(status.splitlines()[0], f"run={second}")
                self.assertEqual(state_of(first), before)

    def test_keeps_run_that_has_not_ended(self):
        for task_state in UNFINISHED:
            with self.subTest(task_state), tempfile.TemporaryDirectory() as tmp:
                repo, env, first = self.first_run(tmp, task_state)
                before = state_of(first)
                second = Path(tmp, "second")
                message = refused("init", "--run", str(second), cwd=repo, env=env)
                self.assertIn(f"the run at {first} has not ended", message)
                self.assertFalse(second.exists())
                status = ultraclose("status", cwd=repo, env=env)
                self.assertEqual(status.splitlines()[0], f"run={first}")
                self.assertEqual(state_of(first), before)

    def test_refuses_directory_of_earlier_run(self):
        # The repository names the second run, and the first run is older.
        cases = {
            "the run that the repository names": ("second", []),
            "the same run with --force": ("second", ["--force"]),
            "an older run": ("first", []),
        }
        for name, (directory, flags) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env, first = self.first_run(tmp)
                second = Path(tmp, "second").resolve()
                ultraclose(
                    "init", "--run", str(second), "--limit", "9", cwd=repo, env=env
                )
                runs = {"first": first, "second": second}
                before = {n: state_of(r) for n, r in runs.items()}
                message = refused(
                    "init", "--run", str(runs[directory]), *flags, cwd=repo, env=env
                )
                self.assertIn(f"{runs[directory]} holds an earlier run", message)
                status = ultraclose("status", cwd=repo, env=env)
                self.assertEqual(status.splitlines()[0], f"run={second}")
                self.assertEqual({n: state_of(r) for n, r in runs.items()}, before)

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


class StatusTest(unittest.TestCase):
    """The status command shows what a later batch of tasks inherits."""

    def test_prints_config(self):
        # name: (the init flags, the shared globs, the check, the health
        # command)
        cases = {
            "the defaults": ([], DEFAULT_SHARED, "none", "none"),
            "a shared glob, a check, and a health command": (
                ["--shared", GLOB, "--check", "sh check.sh", "--health", "true"],
                DEFAULT_SHARED + [GLOB],
                "sh check.sh",
                "true",
            ),
        }
        for name, (flags, shared, check, health) in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                ultraclose(
                    "init", "--run", str(Path(tmp, "run")), *flags, cwd=repo, env=env
                )
                lines = ultraclose("status", cwd=repo, env=env).splitlines()
                self.assertIn(f"shared: {' '.join(shared)}", lines)
                self.assertIn(f"check: {check}", lines)
                self.assertIn(f"health: {health}", lines)

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
            "--check": (["--check", "sh other.sh"], {"check": "sh other.sh"}),
            "an empty --check": (["--check", ""], {"check": ""}),
            "--health": (["--health", "true"], {"health": "true"}),
            "an empty --health": (["--health", ""], {"health": ""}),
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
        for unfinished, done in zip(UNFINISHED, FINISHED):
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

    def test_refuses_interval_that_is_not_positive(self):
        # A watch command that accepts a negative interval fails at its
        # first wait. One that accepts an interval of 0 measures without a
        # wait, and the run holds a pending task, so it keeps running until
        # the timeout.
        for flags in (["--interval", "0"], ["--interval=-1"]):
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
# What init says about a directory that holds the tasks directory of an
# earlier run.
HOLDS_TASKS = "holds the tasks directory of an earlier run"


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

    def test_init_refuses_directory_that_holds_tasks(self):
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
                message = refused("init", "--run", str(first), cwd=repo, env=env)
                self.assertIn(f"{first} {HOLDS_TASKS}", message)
                self.assertIn("Pass a new directory", message)
                self.assertIn("With --force, init takes this directory", message)
                self.assertFalse((first / "state.json").exists())
                self.assertIn("no run found", refused("status", cwd=repo, env=env))

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


class LandTest(unittest.TestCase):
    """What a landing prints, and how it tells a down service from a fault.

    A failed landing prints its result last. A landing with --next and the
    result OK then starts the next tasks. The check and the commit hooks
    of a landing do not read the golangci-lint cache that every checkout
    shares. A landing also records how long its check and its commit took.
    """

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

    def start_task(self, tmp, *flags, hook="exit 0"):
        """Start a run with one task that is ready to land.

        The flags go to init. The task branch holds one commit. The
        pre-commit hook of the repository runs the hook argument, and
        only the landing commits with hooks on. Return the repository
        and the environment.
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
        git("init", "-q", "-b", "main", str(repo), cwd=tmp, env=env)
        git("commit", "-q", "--allow-empty", "-m", "init", cwd=repo, env=env)
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

    def land(self, repo, env, *flags, stderr=subprocess.PIPE):
        """Land the task. Return the exit status and the printed lines.

        The flags go to the land command. The lines come from the standard
        output. Pass subprocess.STDOUT as stderr to add the lines of the
        standard error in the order that the command writes both.
        """
        p = subprocess.run(
            [sys.executable, str(SCRIPT), "land", REF, *flags],
            cwd=repo,
            env=env,
            check=False,
            stdout=subprocess.PIPE,
            stderr=stderr,
            text=True,
        )
        return p.returncode, p.stdout.splitlines()

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


# The Workflow tool runs the body of close-task.js inside an async
# function, and passes it the input and an agent function. The driver
# does the same under Node with scripted agents. It reads the input and
# one result per agent label from standard input. It prints the result of
# the script and each agent call.
DRIVER_HEAD = """\
const scenario = JSON.parse(require('node:fs').readFileSync(0, 'utf8'))
const calls = []
const scripted = async (prompt, opts) => {
  calls.push({ label: opts.label, prompt, schema: opts.schema })
  if (!(opts.label in scenario.agents)) throw new Error(`no result for ${opts.label}`)
  return scenario.agents[opts.label]
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

    def run_workflow(self, agents, rounds=6, resume=None, args=None):
        """Run close-task.js with scripted agents.

        The agents argument maps each short agent label to the result of
        that agent, in the order that the script must call them. The args
        argument holds the input of the script as `ultraclose.py` printed
        it, and the driver then runs the copy of the script that the input
        names. Without it, the input names a task outside any run, with
        the given rounds and resume field. Return the result of the script
        and each agent call by short label.
        """
        task = {
            "ref": REF,
            "title": "t",
            "wt": "/repo/wt",
            "dir": f"/run/tasks/{REF}",
        }
        if resume:
            task["resume"] = resume
        scenario = {
            "args": args
            or {
                "run": "/run",
                "repo": "/repo",
                "base": "main",
                "rounds": rounds,
                "task": task,
            },
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

    def every_agent(self):
        """Return one call of each agent that the script starts, by short
        label."""
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
        )
        _, synced = self.run_workflow(
            {"sync": SYNCED, "review r2": APPROVE}, resume=SYNC
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
