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


class LandTest(unittest.TestCase):
    """A landing prints its result last and tells a down service from a fault."""

    REF = "APP-7KQ9"
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
        triage = Path(tmp, "triage.json")
        triage.write_text(json.dumps([{"ref": self.REF, "title": "t", "files": []}]))
        ultraclose("triage", str(triage), cwd=repo, env=env)
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
            [sys.executable, str(SCRIPT), "land", self.REF],
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
        self.assertEqual(lines[-1], f"RESULT {self.REF} {result}")
        self.assertEqual(self.git("log", "--format=%s", cwd=repo, env=env), "init")
        self.assertEqual(self.git("status", "--porcelain", cwd=repo, env=env), "")

    def assert_landed(self, repo, env, landing):
        status, lines = landing
        self.assertEqual(status, 0, lines)
        self.assertTrue(lines[-1].startswith(f"RESULT {self.REF} OK "), lines)
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
class WorkflowResultTest(unittest.TestCase):
    """The summary, the notes, and the defects of a result.

    The summary and the notes follow the last change. The defects add up
    over the agents of one launch.
    """

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
