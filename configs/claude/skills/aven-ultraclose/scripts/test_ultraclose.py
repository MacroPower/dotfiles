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


# The Workflow tool runs the body of close-task.js inside an async
# function, and passes it the input and an agent function. The driver
# does the same under Node with scripted agents. It reads the input and
# one result per agent label from standard input. It prints the result of
# the script and each agent call.
DRIVER_HEAD = """\
const scenario = JSON.parse(require('node:fs').readFileSync(0, 'utf8'))
const calls = []
const scripted = async (prompt, opts) => {
  calls.push({ label: opts.label, prompt, required: opts.schema.required })
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


IMPLEMENTED = {"status": "implemented", **described("the implementer")}
SYNCED = {"status": "synced", **described("the sync agent")}
NARROWED = {
    "followups": [{"title": "t", "description": "d"}],
    **described("the narrowing agent"),
}


def fixed(n, gave_up=False):
    return {
        "responses": [{"finding": 1, "action": "fixed", "detail": "d"}],
        "gave_up": gave_up,
        **described(f"fixer {n}"),
    }


@unittest.skipUnless(NODE, "node is not on PATH")
class WorkflowResultTest(unittest.TestCase):
    """The summary and the notes of a result follow the last change."""

    REF = "APP-7KQ9"

    def label(self, short):
        """Turn a short label such as "fix r1" into the label of the script."""
        kind, _, rest = short.partition(" ")
        return f"{kind} {self.REF} {rest}".strip()

    def run_workflow(self, agents, rounds=6, resume=None):
        """Run close-task.js with scripted agents.

        The agents argument maps each short agent label to the result of
        that agent, in the order that the script must call them. Return
        the result of the script and each agent call by short label.
        """
        task = {
            "ref": self.REF,
            "title": "t",
            "wt": "/repo/wt",
            "dir": f"/run/tasks/{self.REF}",
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
                    {"summary", "notes"}, set(calls[label]["required"])
                )

    def test_fixer_after_relaunch_learns_what_exists(self):
        fix_round = {"review r2": CHANGES, "fix r2": fixed(2), "review r3": APPROVE}
        # name: (run_workflow arguments, the notes that the fixer receives)
        cases = {
            "a relaunch": ({"agents": fix_round, "resume": RELAUNCH}, "none"),
            "a sync": (
                {"agents": {"sync": SYNCED, **fix_round}, "resume": SYNC},
                SYNCED["notes"],
            ),
        }
        for name, (kwargs, notes) in cases.items():
            with self.subTest(name):
                _, calls = self.run_workflow(**kwargs)
                prompt = calls["fix r2"]["prompt"]
                self.assertIn("The summary of the change so far:\nnone\n", prompt)
                self.assertIn(f"The notes of the change so far:\n{notes}\n", prompt)


if __name__ == "__main__":
    unittest.main()
