"""Tests for the comment check and the CRLF check in check.py, and for
the lines that check.py expects a bad fixture to lint.

Run with: python3 test_check.py. The weirpack build runs the tests
before check.py reads the rule files. Every rule file follows the
comment layout, and every awk rule reads a file with CRLF line endings
the way it reads the file with LF endings. Only these tests therefore
show that a breach or a misread fails.
"""

import sys
import tempfile
import unittest
from pathlib import Path

# Importing check writes a __pycache__ directory beside it unless this
# flag is set, and git shows the directory as untracked.
sys.dont_write_bytecode = True

import check  # noqa: E402

OVERVIEW = """\
# Overview
#
# `and/or` leaves the reader to work out which combinations the
# sentence allows.
"""
RELATED_RULES = """\
# Related Rules
#
# ProseSlash skips `and/or`, so only this rule reports the phrase.
"""
MEASUREMENT = """\
# Measurement
#
# The rule hit 61 lines in the measure.py corpus, and in 20 of 20
# sampled hits `and/or` joined alternatives in a sentence.
"""
OUTPUT_FORMAT = """\
# Output format, one line per finding:
#   <file>:<line>:<col>: Readability::ProseAndOr: <message>
"""
WEIR_CODE = "expr main and/or\n"
AWK_CODE = '\n@include "markdown.awk"\n'


def comment(*parts: str) -> str:
    """Join the parts of a comment with one blank comment line between them."""
    return "#\n".join(parts)


class RequiredSectionsTest(unittest.TestCase):
    def failures(self, name: str, text: str) -> list[str]:
        """Return the failures of a rule file, with each path cut to the file name."""
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / name
            path.write_text(text)
            return [f.removeprefix(f"{tmp}/") for f in check.check_comment(path)]

    def test_weir(self):
        cases = {
            "the required sections alone pass": (
                comment(OVERVIEW, MEASUREMENT),
                [],
            ),
            "an optional section between them passes": (
                comment(OVERVIEW, RELATED_RULES, MEASUREMENT),
                [],
            ),
            "a comment without Measurement fails": (
                comment(OVERVIEW, RELATED_RULES),
                ["ProseAndOr.weir: the comment lacks the Measurement section"],
            ),
            "a comment without Overview fails": (
                comment(RELATED_RULES, MEASUREMENT),
                [
                    "ProseAndOr.weir:1: the comment does not open with the Overview heading",
                    "ProseAndOr.weir: the comment lacks the Overview section",
                ],
            ),
        }
        for label, (text, want) in cases.items():
            with self.subTest(label):
                self.assertEqual(self.failures("ProseAndOr.weir", text + WEIR_CODE), want)

    def test_awk(self):
        cases = {
            "the required sections alone pass": (
                comment(OVERVIEW, MEASUREMENT, OUTPUT_FORMAT),
                [],
            ),
            "a comment without Measurement fails": (
                comment(OVERVIEW, OUTPUT_FORMAT),
                ["ProseAndOr.awk: the comment lacks the Measurement section"],
            ),
        }
        for label, (text, want) in cases.items():
            with self.subTest(label):
                self.assertEqual(self.failures("ProseAndOr.awk", text + AWK_CODE), want)


# An awk check that reports each line that ends in a period.
PERIOD_CHECK = r'/\.$/ { printf "%s:%d:1: Style::ProsePeriod: Period.\n", FILENAME, FNR }' "\n"


class CrlfTest(unittest.TestCase):
    def failures(self, check_code: str) -> list[str]:
        """Return the CRLF failures of an awk check, with each path cut below the fixtures."""
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tier = root / "fixtures" / "required"
            tier.mkdir(parents=True)
            (tier / "ProsePeriod.bad.md").write_text("One sentence.\n")
            script = root / "ProsePeriod.awk"
            script.write_text(check_code)
            rule = check.Rule("ProsePeriod", "required", "awk", script)
            found = check.check_crlf(rule, root / "fixtures", root)
            return [f.removeprefix(f"{tmp}/fixtures/") for f in found]

    def test_crlf(self):
        cases = {
            "a check that keeps the \\r fails": (
                PERIOD_CHECK,
                [
                    "required/ProsePeriod.bad.md:1:1: Style::ProsePeriod: Period."
                    " (missing when the lines end in CRLF)"
                ],
            ),
            "a check that drops the \\r passes": (
                r'BEGIN { RS = "\r?\n" }' "\n" + PERIOD_CHECK,
                [],
            ),
            "a check that fires on the \\r fails": (
                r'/\r$/ { printf "%s:%d:1: Style::ProsePeriod: Period.\n", FILENAME, FNR }' "\n",
                [
                    "required/ProsePeriod.bad.md:1:1: Style::ProsePeriod: Period."
                    " (reported only when the lines end in CRLF)"
                ],
            ),
            "a check whose column moves fails on both sides": (
                r'{ printf "%s:%d:%d: Style::ProsePeriod: Period.\n", FILENAME, FNR, length($0) }'
                "\n",
                [
                    "required/ProsePeriod.bad.md:1:13: Style::ProsePeriod: Period."
                    " (missing when the lines end in CRLF)",
                    "required/ProsePeriod.bad.md:1:14: Style::ProsePeriod: Period."
                    " (reported only when the lines end in CRLF)",
                ],
            ),
        }
        for label, (code, want) in cases.items():
            with self.subTest(label):
                self.assertEqual(self.failures(code), want)


class ExpectedLinesTest(unittest.TestCase):
    def test_lines(self):
        lines = [
            "<!-- prettier-ignore-start -->",
            "| Key | Value | <!-- same finding -->",
            "> The proxy reads the route table and records the choice.",
            "<!-- prettier-ignore-end -->",
            "",
            ">",
            "  <!-- prettier-ignore -->  ",
            "The scheduler reads the queue. <!-- prettier-ignore -->",
            "<!-- a note -->",
        ]
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "ProseAndOr.bad.md"
            path.write_text("\n".join(lines) + "\n")
            self.assertEqual(check.expected_lines(path), {3, 8, 9})


if __name__ == "__main__":
    unittest.main()
