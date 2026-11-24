"""Tests for compare.py.

Each test builds a git repository in a temporary directory and runs
compare.py there against a fake prose-lint. The fake prints a
ProseFoo finding for each line that holds `foo`, a ProseBar finding for
each line that holds `bar`, and a ProseBaz finding, whose kind holds a
space, for each line that holds `baz`. Like prose-lint, it skips a path
under harper/fixtures. Two more fakes wrap the first, and each drops
the findings of one rule. No test runs a real prose-lint.

Run with: python3 test_compare.py.
"""

import io
import os
import re
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parent))

import compare  # noqa: E402
import measure  # noqa: E402

FAKE = """\
#!/bin/sh
if [ "$1" = --commit ]; then
  path='<stdin>'
  message=$(mktemp)
  trap 'rm -f "$message"' EXIT
  cat >"$message"
  set -- "$message"
else
  path=$1
  case "$1" in */harper/fixtures/*) exit 0 ;; esac
fi
awk -v path="$path" '
  /foo/ { printf "%s:%d:%d: [Required] Style::ProseFoo: Foo.\\n", path, NR, index($0, "foo"); found = 1 }
  /bar/ { printf "%s:%d:%d: [Recommended] Style::ProseBar: Bar.\\n", path, NR, index($0, "bar"); found = 1 }
  /baz/ { printf "%s:%d:%d: [Required] Word Choice::ProseBaz: Baz.\\n", path, NR, index($0, "baz"); found = 1 }
  END { exit found }
' "$1"
"""

DROP_BAR = """\
#!/bin/sh
"$FAKE_DIR/fake" "$@" | grep -v ProseBar
exit 0
"""

DROP_BAZ = """\
#!/bin/sh
"$FAKE_DIR/fake" "$@" | grep -v ProseBaz
exit 0
"""

FAILS = """\
#!/bin/sh
echo broken >&2
exit 2
"""


class CompareTest(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = Path(tmp.name)
        self.repo = self.tmp / "repo"
        self.fakes = self.tmp / "fakes"
        self.root = self.tmp / "corpus"
        self.fakes.mkdir()
        self.root.mkdir()
        for name, text in {"fake": FAKE, "drop-bar": DROP_BAR, "drop-baz": DROP_BAZ, "fails": FAILS}.items():
            path = self.fakes / name
            path.write_text(text)
            path.chmod(0o755)
        os.environ["FAKE_DIR"] = str(self.fakes)

        self.write("configs/harper/fixtures/required/ProseFoo.bad.md", "a foo\n")
        self.write("docs/a.md", "one foo\ntwo bar\n")
        self.write("docs/b.go", "// bar\n")
        self.git("init", "-q")
        self.git("add", ".")
        self.commit("docs: add foo and bar")
        self.commit("docs: plain")
        self.write("untracked.md", "foo bar\n")

        cwd = os.getcwd()
        os.chdir(self.repo)
        self.addCleanup(os.chdir, cwd)

    def write(self, rel: str, text: str) -> None:
        path = self.repo / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def git(self, *args: str) -> None:
        subprocess.run(["git", "-C", str(self.repo), *args], check=True, capture_output=True)

    def commit(self, message: str) -> None:
        self.git(
            "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", message
        )

    def run_compare(self, first: str, second: str, *args: str) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        argv = [str(self.fakes / first), str(self.fakes / second), *args, "--root", str(self.root)]
        with redirect_stdout(out), redirect_stderr(err):
            code = compare.main(argv)
        return code, out.getvalue(), err.getvalue()

    def rows(self, out: str) -> dict[str, list[int]]:
        rows = {}
        for line in out.splitlines():
            m = re.fullmatch(r"(\S.*::\w+) +(\d+) +(\d+) +(\d+)", line)
            if m:
                rows[m[1]] = [int(n) for n in m.groups()[1:]]
        return rows

    def test_same_binary_shows_no_difference(self) -> None:
        code, out, err = self.run_compare("fake", "fake")
        self.assertEqual(code, 0)
        self.assertEqual(err, "")
        # ProseFoo: the fixture, docs/a.md, and the first commit.
        # ProseBar: docs/a.md, docs/b.go, and the first commit.
        self.assertEqual(self.rows(out), {"Style::ProseBar": [0, 0, 3], "Style::ProseFoo": [0, 0, 3]})
        self.assertIn("Linted 1 fixtures, 3 tracked files, 0 corpus files, and 2 commit messages.", out)

    def test_dropped_rule_shows_in_its_row(self) -> None:
        code, out, _ = self.run_compare("fake", "drop-bar")
        self.assertEqual(code, 0)
        self.assertEqual(self.rows(out), {"Style::ProseBar": [3, 0, 0], "Style::ProseFoo": [0, 0, 3]})

        code, out, _ = self.run_compare("drop-bar", "fake")
        self.assertEqual(self.rows(out), {"Style::ProseBar": [0, 3, 0], "Style::ProseFoo": [0, 0, 3]})

    def test_rule_lists_each_difference_with_its_source_line(self) -> None:
        _, out, _ = self.run_compare("fake", "drop-bar", "ProseBar", "Style::ProseFoo")
        sha = subprocess.run(
            ["git", "rev-parse", "HEAD~1"], capture_output=True, text=True, check=True
        ).stdout[:12]
        listing = out.split("\nProseBar:\n", 1)[1]
        self.assertIn(
            f"< commit {sha}:1:19: [Recommended] Style::ProseBar: Bar.\n    docs: add foo and bar\n",
            listing,
        )
        self.assertIn("< docs/a.md:2:5: [Recommended] Style::ProseBar: Bar.\n    two bar\n", listing)
        self.assertIn("< docs/b.go:1:4: [Recommended] Style::ProseBar: Bar.\n    // bar\n", listing)
        self.assertIn("\nStyle::ProseFoo:\n  no finding that only one binary prints\n", out)

    def test_kind_with_a_space_gets_its_own_row(self) -> None:
        self.write("docs/c.md", "one baz\n")
        self.git("add", "docs/c.md")
        _, out, _ = self.run_compare("fake", "drop-baz", "ProseBaz")
        self.assertEqual(self.rows(out)["Word Choice::ProseBaz"], [1, 0, 0])
        self.assertNotIn("(unparsed)", out)
        self.assertIn("< docs/c.md:1:5: [Required] Word Choice::ProseBaz: Baz.\n    one baz\n", out)

    def test_fixture_finding_names_the_fixture(self) -> None:
        self.write("configs/harper/fixtures/required/ProseFoo.bad.md", "a foo\nb bar\n")
        _, out, _ = self.run_compare("fake", "drop-bar", "ProseBar")
        self.assertIn(
            "< configs/harper/fixtures/required/ProseFoo.bad.md:2:3: [Recommended] Style::ProseBar: Bar.\n    b bar\n",
            out,
        )

    def test_corpus_clone_joins_the_inputs(self) -> None:
        sources, _, _ = measure.load_corpus()
        src = next(s for s in sources if s.corpus == "docs" and s.paths)
        clone = self.root / src.repo
        (clone / ".git").mkdir(parents=True)
        (clone / src.paths[0]).mkdir(parents=True)
        (clone / src.paths[0] / "page.md").write_text("some bar\n")
        _, out, _ = self.run_compare("fake", "drop-bar", "ProseBar")
        self.assertIn("1 corpus files", out)
        self.assertEqual(self.rows(out)["Style::ProseBar"], [4, 0, 0])
        self.assertIn(f"< {src.repo}/{src.paths[0]}/page.md:1:6: [Recommended] Style::ProseBar: Bar.\n", out)
        missing = next(line for line in out.splitlines() if "does not hold" in line)
        self.assertNotIn(src.repo, missing)

    def test_failed_binary_warns_and_exits_zero(self) -> None:
        code, out, err = self.run_compare("fake", "fails")
        self.assertEqual(code, 0)
        self.assertIn("exited 2 on docs/a.md: broken", err)
        self.assertEqual(self.rows(out), {"Style::ProseBar": [3, 0, 0], "Style::ProseFoo": [3, 0, 0]})

    def test_missing_binary_stops_before_any_lint(self) -> None:
        with self.assertRaises(SystemExit) as caught:
            self.run_compare("fake", "missing")
        self.assertEqual(caught.exception.code, f"compare.py: cannot run {self.fakes / 'missing'}")


if __name__ == "__main__":
    unittest.main()
