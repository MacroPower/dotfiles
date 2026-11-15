#!/usr/bin/env python3
"""Tests for ultraclose.py and close-task.js.

Run with: python3 test_ultraclose.py. The close-task.js tests run the
script under Node, and they skip when node is not on PATH.
"""

import json
import os
import shutil
import subprocess
import sys
import tempfile
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


def refused(*args, cwd, env):
    """Run one ultraclose.py command that must fail, and return its message."""
    p = subprocess.run(
        [sys.executable, str(SCRIPT), *args],
        cwd=cwd,
        env=env,
        check=False,
        capture_output=True,
        text=True,
    )
    if not p.returncode:
        raise AssertionError(f"ultraclose.py {' '.join(args)} passed:\n{p.stdout}")
    return p.stderr


def new_repo(tmp):
    """Create an empty repository, and return it with a clean environment."""
    repo, env = Path(tmp, "repo"), clean_env()
    subprocess.run(["git", "init", "-q", "-b", "main", str(repo)], env=env, check=True)
    return repo, env


def launchable_repo(tmp):
    """Create a repository whose tasks can start, and return it with its env.

    The repository has one commit, so a task branch can start from it.
    A stub stands in for aven, which a task start calls.
    """
    repo, env = new_repo(tmp)
    bin_dir = Path(tmp, "bin")
    bin_dir.mkdir()
    aven = bin_dir / "aven"
    aven.write_text("#!/bin/sh\necho stub\n")
    aven.chmod(0o755)
    env["PATH"] = f"{bin_dir}{os.pathsep}{env['PATH']}"
    subprocess.run(
        [
            "git",
            "-c",
            "user.name=test",
            "-c",
            "user.email=test@example.com",
            "-c",
            "commit.gpgsign=false",
            "commit",
            "-q",
            "--no-verify",
            "--allow-empty",
            "-m",
            "init",
        ],
        cwd=repo,
        env=env,
        check=True,
    )
    return repo, env


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
# The line that status prints for a run that has ended.
ENDED = "ended: no task is pending, inflight, or approved"
# The states of a task that keep a run from its end, and the other states.
UNFINISHED = ("pending", "inflight", "approved")
FINISHED = ("landed", "noted", "closed")


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


def stale(copy):
    copy.write_text("// an older close-task.js\n")


def hard_linked(copy):
    os.link(copy, copy.with_name("second-link.js"))


def symlinked(copy):
    copy.unlink()
    copy.symlink_to(WORKFLOW)


class WorkflowCopyTest(unittest.TestCase):
    """The run directory holds a copy of close-task.js that Workflow loads."""

    def start_run(self, tmp):
        """Start a run with one pending task, and return the repo and the env."""
        repo, env = launchable_repo(tmp)
        ultraclose("init", "--run", str(Path(tmp, "run")), cwd=repo, env=env)
        triage(tmp, repo, env, {"ref": REF})
        return repo, env

    def assert_loadable(self, copy):
        """The Workflow tool loads only a regular file with one hard link."""
        self.assertFalse(copy.is_symlink(), "the copy is a symlink")
        self.assertTrue(copy.is_file(), "the copy is not a regular file")
        self.assertEqual(copy.stat().st_nlink, 1)
        self.assertEqual(copy.read_bytes(), WORKFLOW.read_bytes())

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

    def test_init_copies(self):
        layouts = {
            "plain files": lambda tmp: SCRIPT,
            "symlinks to hard-linked store files": self.installed_skill,
        }
        for name, layout in layouts.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                repo, env = new_repo(tmp)
                run = Path(tmp, "run")
                script = layout(tmp)
                ultraclose("init", "--run", str(run), cwd=repo, env=env, script=script)
                self.assert_loadable(run / "close-task.js")

    def test_launch_refreshes(self):
        # Each command prints the Workflow input of the pending task.
        commands = {
            "next": (["next"], lambda out: json.loads(out)[0]),
            "args": (["args", REF], json.loads),
        }
        damages = {
            "a missing copy": Path.unlink,
            "a stale copy": stale,
            "a copy with a second hard link": hard_linked,
            "a symlink into the skill directory": symlinked,
        }
        for command, (args, workflow_input) in commands.items():
            for name, damage in damages.items():
                with (
                    self.subTest(f"{command} replaces {name}"),
                    tempfile.TemporaryDirectory() as tmp,
                ):
                    repo, env = self.start_run(tmp)
                    damage(Path(tmp, "run", "close-task.js"))
                    task = workflow_input(ultraclose(*args, cwd=repo, env=env))
                    # SKILL.md names the copy by the run field of the input.
                    self.assert_loadable(Path(task["run"], "close-task.js"))

    def test_launch_keeps_loadable_copy(self):
        # Workflow refuses a script that a command replaces during the load.
        commands = {"next": ["next"], "args": ["args", REF]}
        for command, args in commands.items():
            with self.subTest(command), tempfile.TemporaryDirectory() as tmp:
                repo, env = self.start_run(tmp)
                copy = Path(tmp, "run", "close-task.js")
                before = copy.stat()
                ultraclose(*args, cwd=repo, env=env)
                after = copy.stat()
                self.assertEqual(
                    (after.st_ino, after.st_mtime_ns),
                    (before.st_ino, before.st_mtime_ns),
                )


class LandTest(unittest.TestCase):
    """A landing prints its result last and tells a down service from a fault."""

    SUBJECT = "feat: add a file"
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

    def start_task(self, tmp, *flags, hook="exit 0"):
        """Start a run with one task that is ready to land.

        The flags go to init. The task branch holds one commit. The
        pre-commit hook of the repository runs the hook argument, and
        only the landing commits with hooks on. Return the repository
        and the environment.
        """
        repo, run, bin_dir = Path(tmp, "repo"), Path(tmp, "run"), Path(tmp, "bin")
        bin_dir.mkdir()
        aven = bin_dir / "aven"
        aven.write_text("#!/bin/sh\necho stub\n")
        aven.chmod(0o755)
        # The landing commits with the git config of its caller. This
        # file replaces the config of the user, so the hook path and the
        # signing key of the user do not reach the test repository.
        config = Path(tmp, "gitconfig")
        config.write_text("[user]\n\tname = test\n\temail = test@example.com\n")
        env = clean_env()
        env["PATH"] = f"{bin_dir}{os.pathsep}{env['PATH']}"
        env["GIT_CONFIG_GLOBAL"] = str(config)
        env["GIT_CONFIG_NOSYSTEM"] = "1"
        env["ENGINE"] = str(Path(tmp, "engine"))
        env["RAN"] = str(Path(tmp, "ran"))
        self.git("init", "-q", "-b", "main", str(repo), cwd=tmp, env=env)
        self.git("commit", "-q", "--allow-empty", "-m", "init", cwd=repo, env=env)
        ultraclose("init", "--run", str(run), *flags, cwd=repo, env=env)
        triage(tmp, repo, env, {"ref": REF})
        task = json.loads(ultraclose("next", cwd=repo, env=env))[0]["task"]
        Path(task["wt"], "a.txt").write_text("a\n")
        self.git("add", "-A", cwd=task["wt"], env=env)
        self.git("commit", "-q", "--no-verify", "-m", "wip", cwd=task["wt"], env=env)
        Path(task["dir"], "msg.txt").write_text(self.SUBJECT + "\n")
        hooks = Path(repo, ".git", "hooks")
        hooks.mkdir(exist_ok=True)
        (hooks / "pre-commit").write_text(f"#!/bin/sh\n{hook}\n")
        (hooks / "pre-commit").chmod(0o755)
        return repo, env

    def git(self, *args, cwd, env):
        """Run one git command and return its standard output."""
        p = subprocess.run(
            ["git", *args], cwd=cwd, env=env, check=True, capture_output=True, text=True
        )
        return p.stdout.strip()

    def land(self, repo, env):
        """Land the task. Return the exit status and the printed lines."""
        p = subprocess.run(
            [sys.executable, str(SCRIPT), "land", REF],
            cwd=repo,
            env=env,
            check=False,
            capture_output=True,
            text=True,
        )
        return p.returncode, p.stdout.splitlines()

    def assert_failed(self, repo, env, landing, result):
        """The landing printed the result last and left the base branch alone."""
        status, lines = landing
        self.assertEqual(status, 2, lines)
        self.assertEqual(lines[-1], f"RESULT {REF} {result}")
        self.assertEqual(self.git("log", "--format=%s", cwd=repo, env=env), "init")
        self.assertEqual(self.git("status", "--porcelain", cwd=repo, env=env), "")

    def assert_landed(self, repo, env, landing):
        status, lines = landing
        self.assertEqual(status, 0, lines)
        self.assertTrue(lines[-1].startswith(f"RESULT {REF} OK "), lines)
        self.assertEqual(
            self.git("log", "-1", "--format=%s", cwd=repo, env=env), self.SUBJECT
        )

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
# The resume field of the input that `ultraclose.py args` prints for a
# task with one finished round, without and with --sync.
RELAUNCH = {"round": 2, "lastRound": 6}
SYNC = {"round": 2, "lastRound": 3, "sync": "the merge conflicts"}


def described(author):
    """The summary and the notes that one agent returns."""
    return {"summary": f"summary by {author}", "notes": f"notes by {author}"}


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

    def run_workflow(self, agents, rounds=6, resume=None):
        """Run close-task.js with scripted agents.

        The agents argument maps each short agent label to the result of
        that agent, in the order that the script must call them. Return
        the result of the script and each agent call by short label.
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
            "args": {
                "run": "/run",
                "repo": "/repo",
                "base": "main",
                "rounds": rounds,
                "task": task,
            },
            "agents": {self.label(short): r for short, r in agents.items()},
        }
        body = WORKFLOW.read_text().replace("export const meta", "const meta", 1)
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


class WorkflowResultTest(WorkflowCase):
    """The summary, the notes, and the defects of a result.

    The summary and the notes follow the last change. The defects add up
    over the agents of one launch.
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
                {"summary": "", "notes": ""},
            ),
            "a sync that the review approves": (
                {
                    "agents": {"sync": SYNCED, "review r2": APPROVE},
                    "resume": SYNC,
                },
                "approved",
                {"summary": "", "notes": SYNCED["notes"]},
            ),
        }
        for name, (kwargs, status, want) in cases.items():
            with self.subTest(name):
                result, _ = self.run_workflow(**kwargs)
                self.assertEqual(
                    (result["status"], result["summary"], result["notes"]),
                    (status, want["summary"], want["notes"]),
                )

    def test_agents_receive_last_summary_and_notes(self):
        _, calls = self.run_workflow(
            {
                "impl": IMPLEMENTED,
                "review r1": CHANGES,
                "fix r1": fixed(1),
                "review r2": CHANGES,
                "fix r2": fixed(2),
                "review final": CHANGES,
                "narrow": NARROWED,
            },
            rounds=2,
        )
        authors = [IMPLEMENTED, fixed(1), fixed(2), NARROWED]
        # label: (the agent whose words the prompt carries, the fields)
        want = {
            "review r1": (IMPLEMENTED, ["summary"]),
            "fix r1": (IMPLEMENTED, ["summary", "notes"]),
            "review r2": (fixed(1), ["summary"]),
            "fix r2": (fixed(1), ["summary", "notes"]),
            "review final": (fixed(2), ["summary"]),
            "narrow": (fixed(2), ["summary", "notes"]),
        }
        for label, (author, fields) in want.items():
            with self.subTest(label):
                prompt = calls[label]["prompt"]
                for field in fields:
                    self.assertIn(author[field], prompt)
                # No prompt carries the words of an earlier or a later agent.
                for other in authors:
                    if other != author:
                        self.assertNotIn(other["summary"], prompt)
                        self.assertNotIn(other["notes"], prompt)

    def test_agents_that_change_the_work_return_summary_and_notes(self):
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
        for label in ("impl", "fix r1", "narrow"):
            with self.subTest(label):
                self.assertLessEqual(
                    {"summary", "notes"}, set(calls[label]["schema"]["required"])
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

    def test_fixer_after_relaunch_learns_what_exists(self):
        fix_round = {"review r2": CHANGES, "fix r2": fixed(2), "review r3": APPROVE}
        # name: (run_workflow arguments, the notes that the fixer receives,
        # the agents whose defects it receives)
        cases = {
            "a relaunch": ({"agents": fix_round, "resume": RELAUNCH}, "none", []),
            "a sync": (
                {"agents": {"sync": SYNCED, **fix_round}, "resume": SYNC},
                SYNCED["notes"],
                ["the sync agent"],
            ),
        }
        for name, (kwargs, notes, reporters) in cases.items():
            with self.subTest(name):
                _, calls = self.run_workflow(**kwargs)
                prompt = calls["fix r2"]["prompt"]
                self.assertIn("The summary of the change so far:\nnone\n", prompt)
                self.assertIn(f"The notes of the change so far:\n{notes}\n", prompt)
                self.assertIn(listed(*reporters), prompt)


if __name__ == "__main__":
    unittest.main()
