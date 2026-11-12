#!/usr/bin/env python3
"""Tests for ultraclose.py. Run with: python3 test_ultraclose.py"""

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("ultraclose.py")
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
                args = [sys.executable, str(SCRIPT), "init", "--run", str(run)]
                for glob in flags:
                    args += ["--shared", glob]
                subprocess.run(
                    args, cwd=repo, env=env, check=True, capture_output=True
                )
                cfg = json.loads((run / "state.json").read_text())["config"]
                self.assertEqual(cfg["shared"], want)


if __name__ == "__main__":
    unittest.main()
