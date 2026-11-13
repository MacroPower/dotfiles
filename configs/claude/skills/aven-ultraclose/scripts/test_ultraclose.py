#!/usr/bin/env python3
"""Tests for ultraclose.py. Run with: python3 test_ultraclose.py"""

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


def ultraclose(*args, cwd, env, script=SCRIPT):
    """Run one ultraclose.py command and return its standard output."""
    p = subprocess.run(
        [sys.executable, str(script), *args],
        cwd=cwd,
        env=env,
        check=True,
        capture_output=True,
        text=True,
    )
    return p.stdout


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
                repo, run = Path(tmp, "repo"), Path(tmp, "run")
                env = clean_env()
                subprocess.run(
                    ["git", "init", "-q", "-b", "main", str(repo)],
                    env=env,
                    check=True,
                )
                args = ["init", "--run", str(run)]
                for glob in flags:
                    args += ["--shared", glob]
                ultraclose(*args, cwd=repo, env=env)
                cfg = json.loads((run / "state.json").read_text())["config"]
                self.assertEqual(cfg["shared"], want)


def stale(copy):
    copy.write_text("// an older close-task.js\n")


def hard_linked(copy):
    os.link(copy, copy.with_name("second-link.js"))


def symlinked(copy):
    copy.unlink()
    copy.symlink_to(WORKFLOW)


class WorkflowCopyTest(unittest.TestCase):
    """The run directory holds a copy of close-task.js that Workflow loads."""

    REF = "APP-7KQ9"

    def start_run(self, tmp):
        """Start a run with one pending task, and return the repo and the env.

        The repository has one commit, so a task branch can start from it.
        A stub stands in for aven, which a task start calls.
        """
        repo, run, bin_dir = Path(tmp, "repo"), Path(tmp, "run"), Path(tmp, "bin")
        bin_dir.mkdir()
        aven = bin_dir / "aven"
        aven.write_text("#!/bin/sh\necho stub\n")
        aven.chmod(0o755)
        env = clean_env()
        env["PATH"] = f"{bin_dir}{os.pathsep}{env['PATH']}"
        subprocess.run(
            ["git", "init", "-q", "-b", "main", str(repo)], env=env, check=True
        )
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
        ultraclose("init", "--run", str(run), cwd=repo, env=env)
        triage = Path(tmp, "triage.json")
        triage.write_text(json.dumps([{"ref": self.REF, "title": "t", "files": []}]))
        ultraclose("triage", str(triage), cwd=repo, env=env)
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
                repo, run = Path(tmp, "repo"), Path(tmp, "run")
                env = clean_env()
                subprocess.run(
                    ["git", "init", "-q", "-b", "main", str(repo)],
                    env=env,
                    check=True,
                )
                script = layout(tmp)
                ultraclose("init", "--run", str(run), cwd=repo, env=env, script=script)
                self.assert_loadable(run / "close-task.js")

    def test_launch_refreshes(self):
        # Each command prints the Workflow input of the pending task.
        commands = {
            "next": (["next"], lambda out: json.loads(out)[0]),
            "args": (["args", self.REF], json.loads),
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
        commands = {"next": ["next"], "args": ["args", self.REF]}
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


if __name__ == "__main__":
    unittest.main()
