"""Tests for the comment check, the CRLF check, and the locale check in
check.py, for the lines that check.py expects a bad fixture to lint, and
for the RAW and SHADOW that check.py and measure.py set for each awk
check.

Run with: python3 test_check.py. The weirpack build runs the tests
before check.py reads the rule files. Every rule file follows the
comment layout. Every awk rule reads a file with CRLF line endings the
way it reads the file with LF endings, and it prints the same findings
under the C locale as under a UTF-8 locale. Only these tests therefore
show that a breach or a misread fails.
"""

import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

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
# An awk BEGIN block that drops the `\r` of each line that ends in CRLF.
DROP_CR = r'BEGIN { RS = "\r?\n" }' "\n"


def awk_failures(run, check_code: str, fixture: str = "One sentence.\n") -> list[str]:
    """Return the failures that `run` reports for an awk check and its bad fixture.

    `run` is a function of check.py that takes a rule, the fixtures
    directory, and the awk lib directory. The function cuts each path in
    a failure below the fixtures directory.

    `run` starts each awk process with LC_ALL=C in its environment.
    Python sets LC_CTYPE to C.UTF-8 when it starts under the C locale,
    as in the sandbox of the weirpack build, and each awk process
    inherits that variable. LC_ALL=C overrides it, so the UTF-8 side of
    each check gets its locale only from the LC_ALL that awk_report()
    sets.
    """
    with mock.patch.dict(os.environ, {"LC_ALL": "C"}), tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        tier = root / "fixtures" / "required"
        tier.mkdir(parents=True)
        (tier / "ProsePeriod.bad.md").write_text(fixture, encoding="utf-8")
        script = root / "ProsePeriod.awk"
        script.write_text(check_code)
        rule = check.Rule("ProsePeriod", "required", "awk", script)
        found = run(rule, root / "fixtures", root)
        return [f.removeprefix(f"{tmp}/fixtures/") for f in found]


class CrlfTest(unittest.TestCase):
    def failures(self, check_code: str) -> list[str]:
        """Return the CRLF failures of an awk check, with each path cut below the fixtures."""
        return awk_failures(check.check_crlf, check_code)

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
                DROP_CR + PERIOD_CHECK,
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


# The fixture of the locale cases. Its `é` is one character under a
# UTF-8 locale and two bytes under the C locale.
CAFE = "Caf\xe9 closed.\n"
# An awk condition that holds under a UTF-8 locale alone. gawk reads
# the octal escapes as the two bytes of `é`.
UTF8 = r'length("\303\251") == 1'


class LocaleTest(unittest.TestCase):
    def failures(self, check_code: str) -> list[str]:
        """Return the locale failures of an awk check, with each path cut below the fixtures."""
        return awk_failures(check.check_locale, check_code, CAFE)

    def test_locale(self):
        finding = "required/ProsePeriod.bad.md:1:{}: Style::ProsePeriod: Period."
        quote = "required/ProsePeriod.bad.md:1:1: Style::ProsePeriod: Starts with `{}`."
        cases = {
            "a check that reads both locales alike passes": (
                DROP_CR + PERIOD_CHECK,
                [],
            ),
            "a check that needs a UTF-8 locale fails": (
                DROP_CR
                + f'{UTF8} && /\\.$/'
                + ' { printf "%s:%d:1: Style::ProsePeriod: Period.\\n", FILENAME, FNR }\n',
                [
                    finding.format(1) + " (missing under the C locale)",
                    finding.format(1)
                    + " (missing under the C locale when the lines end in CRLF)",
                ],
            ),
            "a check that drops the \\r under a UTF-8 locale alone fails": (
                f'BEGIN {{ RS = {UTF8} ? "\\r?\\n" : "\\n" }}\n' + PERIOD_CHECK,
                [
                    finding.format(1)
                    + " (missing under the C locale when the lines end in CRLF)",
                ],
            ),
            "a check whose column counts bytes fails on both sides": (
                DROP_CR + r'{ printf "%s:%d:%d: Style::ProsePeriod: Period.\n", FILENAME, FNR,'
                r' index($0, "closed") }'
                "\n",
                [
                    finding.format(6) + " (missing under the C locale)",
                    finding.format(7) + " (reported only under the C locale)",
                    finding.format(6)
                    + " (missing under the C locale when the lines end in CRLF)",
                    finding.format(7)
                    + " (reported only under the C locale when the lines end in CRLF)",
                ],
            ),
            # Under the C locale, the first four bytes of the line end
            # in the first byte of `é`, which is not UTF-8 on its own.
            "a check that splits a character under the C locale fails": (
                DROP_CR + r'{ printf "%s:%d:1: Style::ProsePeriod: Starts with `%s`.\n",'
                r" FILENAME, FNR, substr($0, 1, 4) }"
                "\n",
                [
                    quote.format("Caf\xe9") + " (missing under the C locale)",
                    quote.format(r"Caf\xc3") + " (reported only under the C locale)",
                    quote.format("Caf\xe9")
                    + " (missing under the C locale when the lines end in CRLF)",
                    quote.format(r"Caf\xc3")
                    + " (reported only under the C locale when the lines end in CRLF)",
                ],
            ),
        }
        for label, (code, want) in cases.items():
            with self.subTest(label):
                self.assertEqual(self.failures(code), want)


# A markdown file with an `e.g.` in prose, in a fence, and in indented
# code. With RAW set, lib/markdown.awk reads the fence and the indented
# code as prose. With SHADOW set, it reads the indented code as prose.
MODES_FILE = "Build it, e.g. with make.\n\n```sh\nmake e.g.\n```\n\nThen run it.\n\n    run e.g.\n"
# An awk check that reports the first `e.g.` of each line that reaches
# prose().
EG_CHECK = (
    '@include "markdown.awk"\n'
    "function BEGIN_CHECK() {}\n"
    'function prose(text) { if (index(text, "e.g.")) printf "%s:%d:%d: Style::ProseEg: E.g.\\n",'
    ' FILENAME, FNR, index(text, "e.g.") }\n'
)


class ModeTest(unittest.TestCase):
    def report(self, exported: dict[str, str], raw: bool) -> list[str]:
        """Return the findings of EG_CHECK on MODES_FILE with `exported` in the environment."""
        lib = Path(check.__file__).resolve().parent / "lib"
        with mock.patch.dict(os.environ), tempfile.TemporaryDirectory() as tmp:
            for name in ("RAW", "SHADOW"):
                os.environ.pop(name, None)
            os.environ.update(exported)
            script = Path(tmp) / "ProseEg.awk"
            script.write_text(EG_CHECK)
            target = Path(tmp) / "modes.md"
            target.write_text(MODES_FILE)
            return check.awk_report(script, lib, target, raw)

    def test_modes(self):
        markdown = ["1:11: Style::ProseEg: E.g."]
        whole = markdown + ["4:6: Style::ProseEg: E.g.", "9:9: Style::ProseEg: E.g."]
        for exported in ({}, {"RAW": "1"}, {"SHADOW": "1"}, {"RAW": "1", "SHADOW": "1"}):
            with self.subTest(exported=exported):
                self.assertEqual(self.report(exported, raw=False), markdown)
                self.assertEqual(self.report(exported, raw=True), whole)

    def test_env(self):
        with mock.patch.dict(os.environ, {"RAW": "1", "SHADOW": "1", "KEEP": "1"}):
            cases = {
                "": {},
                "RAW": {"RAW": "1"},
                "SHADOW": {"SHADOW": "1"},
            }
            for mode, want in cases.items():
                with self.subTest(mode=mode):
                    env = check.awk_env(Path("/lib"), mode)
                    self.assertEqual(
                        {k: v for k, v in env.items() if k in ("RAW", "SHADOW")}, want
                    )
                    self.assertEqual((env["AWKPATH"], env["KEEP"]), ("/lib", "1"))


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
            "***",
            "> > - - -",
            "    ___",
            "*** <!-- a note -->",
            "**",
        ]
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "ProseAndOr.bad.md"
            path.write_text("\n".join(lines) + "\n")
            self.assertEqual(check.expected_lines(path), {3, 8, 9, 13, 14})


if __name__ == "__main__":
    unittest.main()
