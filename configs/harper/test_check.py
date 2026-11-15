"""Tests for the comment check in check.py.

Run with: python3 test_check.py. The weirpack build runs the tests
before check.py reads the rule files. Every rule file follows the
comment layout, so only these tests show that a breach fails.
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


if __name__ == "__main__":
    unittest.main()
