"""Check every rule in the tier directories against its fixture pair.

Usage: check.py <weirpack> <fixtures dir> <rules dir> <awk lib dir>

A rule's tier is the directory under rules/ that holds it: required,
recommended, or optional. A .weir file is a Weir rule the weirpack
carries under the internal name <Rule>_<Tier>, an .awk file is a check
run with awk in place of Harper, and builtins.txt names the Harper
built-ins the tier enables. The awk lib dir goes on AWKPATH, so a check
can `@include` the libraries in lib/, such as lib/markdown.awk. For
each rule, fixtures/<tier>/<Rule>.bad.md must produce a lint on every
non-blank line, and both fixtures/<tier>/<Rule>.good.md and the shared
fixtures/common.good.md must produce none. A line that holds only `>`
marks is a blank line inside a blockquote, so it counts as blank. A bad
line that ends in an HTML comment that holds `same finding` does not
need a lint of its own, because a finding on a nearby line covers it,
such as the heading after it that skips a level. A line that holds only
a prettier-ignore comment does not need one either, so a fixture can
keep prettier from adding a blank line between two blocks.
A rule that reports one line for a span of several lines, such as a run
of paragraphs, marks every line after the first. An awk rule can also
have fixtures/<tier>/<Rule>.raw.txt, which it reads with RAW=1 the way
prose-lint reads a source file, and which must produce a lint on every
non-blank line. The shared file holds
ordinary technical prose that every rule must pass, so a line added
there covers all the rules.

Each awk rule also reads a copy of each of its fixtures and of the
shared file, with every line ending in CRLF. It must print the same
findings there, at the same lines and columns. A file with CRLF line
endings then draws the findings of its copy with LF endings.

Each plain phrase a Weir rule lists in its main expression, directly
or through a referenced list, must also fire on its own in a probe
sentence. List entries that a guard or a typo has broken therefore
fail the build. Entries that contain a shorter entry of the same rule
fail too, because the shorter one already matches every sentence they
do.

Two tiers of one rule must not report the same phrase. For each rule
name in more than one tier, each tier's bad fixture runs through the
other tiers' rules, and a hit on one of its lines fails, unless the
fixture's own tier reports that line with the same message. prose-lint
keeps only the higher tier of two such findings, so that case shows
one finding.

The comment that opens each .weir and .awk file must follow the layout
in the Comments section of CLAUDE.md. SECTIONS lists the section names
in order, and REQUIRED_SECTIONS lists the sections that every comment
carries.

A heading is a comment line that holds only a section name. A comment
line that is a paragraph of its own, is not indented past the one space
after `#`, and does not end in a period counts as a heading too, so a
misspelled or invented section name fails. A line is a paragraph of its
own when a blank comment line or an end of the comment sits on each
side of it. The first line of the comment and the line directly above
the code can therefore count. In an .awk file, the `Output format`
lines are the paragraph that opens with those words. Each of these
breaches fails with a line of its own:

- The file does not open with a comment.
- The comment does not open with the first section.
- A heading is not in SECTIONS, repeats, or comes after a section that
  SECTIONS puts later.
- A heading lacks a blank comment line before it or after it.
- A section holds no text.
- The comment lacks a required section.
- An awk file has no `Output format` lines, or the lines do not end
  the comment.

A rule without a fixture pair fails, so a typo in a built-in name
cannot pass silently. A fixture pair without a rule fails too, so a
rule moved between tiers takes its fixtures along. A built-in listed
under two tiers fails, and so does an entry under rules/ that is not a
tier directory. Exits 1 with one line per failure.
"""

import json
import os
import re
import subprocess
import sys
import tempfile
import zipfile
from dataclasses import dataclass
from pathlib import Path

TIERS = ("required", "recommended", "optional")
COMMON = "common.good.md"

# A list entry made only of words, which a probe sentence can quote.
# An uppercase tag such as DET or PROG names a word class instead.
PLAIN_ENTRY = re.compile(r"[A-Za-z']+( [A-Za-z']+)*")
TAGS = frozenset(
    "ADJ ADP ADV AUX CCONJ DET INTJ NOUN NUM PART PRON PROPN PUNCT SCONJ SYM VERB X PROG".split()
)
EXPR_LINE = re.compile(r"expr (\w+) (.*)")

# The sections of a rule comment, in the order that the Comments section
# of CLAUDE.md gives them. Change that section and these names together.
SECTIONS = ("Overview", "Expression", "False Positives", "Related Rules", "Measurement")
REQUIRED_SECTIONS = frozenset({"Overview", "Measurement"})
# The words that open the last lines of an awk rule's comment.
OUTPUT_FORMAT = "Output format"


@dataclass(frozen=True)
class Rule:
    name: str
    tier: str
    kind: str  # "weir", "awk", or "builtin"
    path: Path | None = None

    @property
    def internal(self) -> str:
        """The name Harper knows the rule by."""
        if self.kind == "weir":
            return f"{self.name}_{self.tier.capitalize()}"
        return self.name


def pack_names(pack: Path) -> set[str]:
    with zipfile.ZipFile(pack) as zf:
        return {Path(n).stem for n in zf.namelist() if n.endswith(".weir")}


def read_builtins(listing: Path) -> list[str]:
    names = []
    for line in listing.read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#"):
            names.append(line)
    return names


def collect_rules(rules_dir: Path) -> tuple[list[Rule], list[str]]:
    """Walk the tier directories and return every rule with the layout failures."""
    rules: list[Rule] = []
    failures: list[str] = []
    for entry in sorted(rules_dir.iterdir()):
        if entry.name not in TIERS:
            failures.append(f"{entry}: not a tier directory")
    seen_builtins: dict[str, str] = {}
    for tier in TIERS:
        tier_dir = rules_dir / tier
        if not tier_dir.is_dir():
            continue
        for path in sorted(tier_dir.glob("*.weir")):
            rules.append(Rule(path.stem, tier, "weir", path))
        for path in sorted(tier_dir.glob("*.awk")):
            rules.append(Rule(path.stem, tier, "awk", path))
        listing = tier_dir / "builtins.txt"
        if listing.is_file():
            for name in read_builtins(listing):
                if name in seen_builtins:
                    failures.append(
                        f"{listing}: {name} is already listed under {seen_builtins[name]}"
                    )
                    continue
                seen_builtins[name] = tier
                rules.append(Rule(name, tier, "builtin"))
    return rules, failures


def harper_lines(pack: Path, rule: str, target: Path) -> set[int]:
    """Return the 1-based lines on which Harper reports `rule` in `target`."""
    return {line for line, _ in harper_findings(pack, rule, target)}


def harper_findings(pack: Path, rule: str, target: Path) -> set[tuple[int, str]]:
    """Return the line and message of each `rule` finding in `target`."""
    proc = subprocess.run(
        [
            "harper-cli",
            "lint",
            "--weirpacks",
            str(pack),
            "--only",
            rule,
            "-o",
            "--format",
            "json",
            "--quiet",
            str(target),
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    if proc.returncode not in (0, 1):
        raise RuntimeError(f"harper-cli exited {proc.returncode} on {target}: {proc.stderr}")
    if not proc.stdout.strip():
        return set()
    found: set[tuple[int, str]] = set()
    for file_report in json.loads(proc.stdout):
        for lint in file_report["lints"]:
            if lint["rule"] == rule:
                found.add((int(lint["line"]), lint["message"]))
    return found


def awk_lines(awk: Path, lib: Path, target: Path, raw: bool = False) -> set[int]:
    return {line for line, _ in awk_findings(awk, lib, target, raw)}


def awk_report(awk: Path, lib: Path, target: Path, raw: bool = False) -> list[str]:
    """Return each finding the check prints, without the file name before its line."""
    env = {**os.environ, "AWKPATH": str(lib)}
    if raw:
        env["RAW"] = "1"
    # A finding can quote a `\r` from the line, so the output splits at
    # `\n` alone. Text mode and splitlines() also split at `\r`.
    proc = subprocess.run(
        ["awk", "-f", str(awk), str(target)],
        capture_output=True,
        check=True,
        env=env,
    )
    lines = proc.stdout.decode().split("\n")
    return [line.split(":", 1)[1] for line in lines if line]


def awk_findings(
    awk: Path, lib: Path, target: Path, raw: bool = False
) -> set[tuple[int, str]]:
    """Return the line and message of each finding the check prints."""
    found: set[tuple[int, str]] = set()
    for line in awk_report(awk, lib, target, raw):
        number, _, rest = line.split(":", 2)
        found.add((int(number), rest.split(": ", 1)[1]))
    return found


def check_crlf(rule: Rule, fixtures: Path, lib: Path) -> list[str]:
    """Fail each finding of an awk rule that a fixture with CRLF line endings changes.

    The rule reads a copy of each fixture whose lines end in CRLF, and
    it must print the findings of the fixture at the same lines and
    columns.
    """
    targets = (
        (fixtures / rule.tier / f"{rule.name}.bad.md", False),
        (fixtures / rule.tier / f"{rule.name}.good.md", False),
        (fixtures / COMMON, False),
        (fixtures / rule.tier / f"{rule.name}.raw.txt", True),
    )
    failures: list[str] = []
    with tempfile.TemporaryDirectory() as tmp:
        for target, raw in targets:
            if not target.is_file():
                continue
            copy = Path(tmp) / target.name
            copy.write_bytes(target.read_bytes().replace(b"\n", b"\r\n"))
            lf = set(awk_report(rule.path, lib, target, raw))
            crlf = set(awk_report(rule.path, lib, copy, raw))
            for finding in sorted(lf - crlf):
                failures.append(f"{target}:{finding} (missing when the lines end in CRLF)")
            for finding in sorted(crlf - lf):
                failures.append(f"{target}:{finding} (reported only when the lines end in CRLF)")
    return failures


SAME_FINDING = "<!-- same finding -->"
PRETTIER_IGNORE = re.compile(r"<!--\s*prettier-ignore(-start|-end)?\s*-->")


def expected_lines(target: Path) -> set[int]:
    """Return the number of each line of a bad fixture that needs a lint."""
    return {
        i
        for i, line in enumerate(target.read_text().splitlines(), start=1)
        if line.replace(">", "").strip()
        and not line.rstrip().endswith(SAME_FINDING)
        and not PRETTIER_IGNORE.fullmatch(line.strip())
    }


def check_rule(rule: Rule, pack: Path, fixtures: Path, lib: Path) -> list[str]:
    bad = fixtures / rule.tier / f"{rule.name}.bad.md"
    good = fixtures / rule.tier / f"{rule.name}.good.md"
    failures: list[str] = []
    if not bad.is_file() or not good.is_file():
        failures.append(f"{rule.tier}/{rule.name}: missing fixture pair {bad.name} and {good.name}")
        return failures

    if rule.kind == "awk":

        def report(target: Path) -> set[int]:
            return awk_lines(rule.path, lib, target)

    else:

        def report(target: Path) -> set[int]:
            return harper_lines(pack, rule.internal, target)

    want = expected_lines(bad)
    got = report(bad)
    for line in sorted(want - got):
        failures.append(f"{bad}:{line}: expected a {rule.name} lint")
    for target in (good, fixtures / COMMON):
        for line in sorted(report(target)):
            failures.append(f"{target}:{line}: unexpected {rule.name} lint")
    raw = fixtures / rule.tier / f"{rule.name}.raw.txt"
    if rule.kind == "awk" and raw.is_file():
        got = awk_lines(rule.path, lib, raw, raw=True)
        for line in sorted(expected_lines(raw) - got):
            failures.append(f"{raw}:{line}: expected a {rule.name} lint with RAW=1")
    return failures


def findings(rule: Rule, pack: Path, lib: Path, target: Path) -> set[tuple[int, str]]:
    if rule.kind == "awk":
        return awk_findings(rule.path, lib, target)
    return harper_findings(pack, rule.internal, target)


def check_tiers(rules: list[Rule], pack: Path, fixtures: Path, lib: Path) -> list[str]:
    """Fail each bad line that another tier of the same rule also reports."""
    by_name: dict[str, list[Rule]] = {}
    for rule in rules:
        by_name.setdefault(rule.name, []).append(rule)
    failures: list[str] = []
    for tiers in by_name.values():
        for own in tiers:
            bad = fixtures / own.tier / f"{own.name}.bad.md"
            if not bad.is_file():
                continue
            want = expected_lines(bad)
            mine = findings(own, pack, lib, bad)
            for other in tiers:
                if other is own:
                    continue
                for line, message in sorted(findings(other, pack, lib, bad)):
                    if line in want and (line, message) not in mine:
                        failures.append(
                            f"{bad}:{line}: the {other.tier} {other.name} also reports this line"
                        )
    return failures


def split_alternatives(body: str) -> list[str]:
    """Split a `[a, (b c), [d, e]]` list into its top-level alternatives."""
    inner = body.strip()[1:-1]
    parts: list[str] = []
    depth = 0
    current = ""
    for ch in inner:
        if ch in "([<":
            depth += 1
        elif ch in ")]>":
            depth -= 1
        if ch == "," and depth == 0:
            parts.append(current.strip())
            current = ""
        else:
            current += ch
    if current.strip():
        parts.append(current.strip())
    return parts


def plain_entries(weir: Path) -> list[str]:
    """Return the word-only phrases the rule's main expression lists.

    An `@name` alternative that refers to a list contributes that
    list's phrases, because each one matches on its own. A bare word or
    a parenthesized sequence of bare words counts as a phrase, and so
    does a main expression that is one such sequence.
    """
    exprs: dict[str, str] = {}
    for line in weir.read_text().splitlines():
        m = EXPR_LINE.fullmatch(line.strip())
        if m:
            exprs[m.group(1)] = m.group(2)

    def plain(alt: str) -> bool:
        return bool(PLAIN_ENTRY.fullmatch(alt)) and not TAGS & set(alt.split())

    def walk(body: str, seen: frozenset[str]) -> list[str]:
        body = body.strip()
        if not (body.startswith("[") and body.endswith("]")):
            return [body] if plain(body) else []
        found: list[str] = []
        for alt in split_alternatives(body):
            if alt.startswith("@") and alt[1:] in exprs and alt[1:] not in seen:
                found += walk(exprs[alt[1:]], seen | {alt[1:]})
                continue
            if alt.startswith("(") and alt.endswith(")"):
                alt = alt[1:-1].strip()
            if plain(alt):
                found.append(alt)
        return found

    return walk(exprs.get("main", ""), frozenset({"main"}))


def check_entries(rule: Rule, pack: Path) -> list[str]:
    """Probe each plain entry of a Weir rule and compare it with the others."""
    entries = plain_entries(rule.path)
    if not entries:
        return []
    failures: list[str] = []
    with tempfile.TemporaryDirectory() as tmp:
        probe = Path(tmp) / "probe.md"
        probe.write_text("\n\n".join(f"Then {e} happens here." for e in entries) + "\n")
        got = harper_lines(pack, rule.internal, probe)
    for i, entry in enumerate(entries):
        if 2 * i + 1 not in got:
            failures.append(f"{rule.path}: entry `{entry}` never fires on its own")
    words = [f" {e.lower()} " for e in entries]
    for long, long_words in zip(entries, words):
        for short, short_words in zip(entries, words):
            if short != long and short_words in long_words:
                failures.append(
                    f"{rule.path}: entry `{long}` repeats `{short}`, which already matches it"
                )
    return failures


def check_comment(path: Path) -> list[str]:
    """Check the comment that opens a rule file against the section layout."""
    comment: list[str] = []
    for line in path.read_text().splitlines():
        if not line.startswith("#"):
            break
        comment.append(line[1:].rstrip())
    if not any(comment):
        return [f"{path}: the file does not open with a comment"]

    awk = path.suffix == ".awk"

    def opens_output_format(i: int) -> bool:
        """Say whether line `i` opens the `Output format` lines of an awk rule.

        The lines are a paragraph of their own, so a wrapped line of
        prose that starts with the same words does not count.
        """
        return (
            awk
            and comment[i].strip().startswith(OUTPUT_FORMAT)
            and (i == 0 or not comment[i - 1])
        )

    failures: list[str] = []
    if comment[0].strip() != SECTIONS[0]:
        failures.append(f"{path}:1: the comment does not open with the {SECTIONS[0]} heading")
    seen: list[str] = []
    output_format = False
    for i, text in enumerate(comment):
        where = f"{path}:{i + 1}"
        name = text.strip()
        first = i == 0
        last = i == len(comment) - 1
        blank_before = not first and not comment[i - 1]
        blank_after = not last and not comment[i + 1]
        if opens_output_format(i):
            output_format = True
            # A heading after the lines gets a failure of its own below.
            rest = comment[i + 1 :]
            if "" in rest and not any(t.strip() in SECTIONS for t in rest):
                failures.append(f"{where}: the `{OUTPUT_FORMAT}` lines do not end the comment")
        elif name in SECTIONS:
            if output_format:
                failures.append(
                    f"{where}: the {name} heading comes after the `{OUTPUT_FORMAT}` lines"
                )
            later = [s for s in seen if SECTIONS.index(s) > SECTIONS.index(name)]
            if name in seen:
                failures.append(f"{where}: the {name} heading repeats")
            elif later:
                failures.append(
                    f"{where}: the {name} heading must come before {max(later, key=SECTIONS.index)}"
                )
            seen.append(name)
            if not first and not blank_before:
                failures.append(f"{where}: the {name} heading needs a blank comment line before it")
            if not blank_after:
                failures.append(f"{where}: the {name} heading needs a blank comment line after it")
            body = next((j for j in range(i + 1, len(comment)) if comment[j]), None)
            if body is None or comment[body].strip() in SECTIONS or opens_output_format(body):
                failures.append(f"{where}: the {name} section holds no text")
        elif (
            name
            and not text.startswith(("  ", "\t"))
            and not name.endswith(".")
            and (first or blank_before)
            and (last or blank_after)
        ):
            failures.append(f"{where}: `{name}` is not a section heading that CLAUDE.md names")
    for name in SECTIONS:
        if name in REQUIRED_SECTIONS and name not in seen:
            failures.append(f"{path}: the comment lacks the {name} section")
    if awk and not output_format:
        failures.append(
            f"{path}: the comment has no `{OUTPUT_FORMAT}` lines after a blank comment line"
        )
    return failures


def orphan_fixtures(fixtures: Path, rules: list[Rule]) -> list[str]:
    """Return a failure per fixture file whose rule is not in its tier."""
    known = {(r.tier, r.name) for r in rules}
    failures: list[str] = []
    for tier in TIERS:
        tier_dir = fixtures / tier
        if not tier_dir.is_dir():
            continue
        for path in sorted(tier_dir.glob("*.md")):
            name = path.name.removesuffix(".bad.md").removesuffix(".good.md")
            if (tier, name) not in known:
                failures.append(f"{path}: no {name} rule under rules/{tier}")
    return failures


def main(argv: list[str]) -> int:
    if len(argv) != 5:
        print(__doc__.strip(), file=sys.stderr)
        return 2
    pack = Path(argv[1])
    fixtures = Path(argv[2])
    rules_dir = Path(argv[3])
    lib = Path(argv[4])

    if not (fixtures / COMMON).is_file():
        print(f"{fixtures / COMMON}: missing the shared good fixture")
        return 1
    rules, failures = collect_rules(rules_dir)
    packed = pack_names(pack)
    for rule in rules:
        if rule.kind == "weir" and rule.internal not in packed:
            failures.append(f"{rule.path}: not in the weirpack as {rule.internal}")
    failures += orphan_fixtures(fixtures, rules)
    failures += check_tiers(rules, pack, fixtures, lib)
    for rule in rules:
        if rule.path is not None:
            failures += check_comment(rule.path)
        failures += check_rule(rule, pack, fixtures, lib)
        if rule.kind == "awk":
            failures += check_crlf(rule, fixtures, lib)
        if rule.kind == "weir":
            failures += check_entries(rule, pack)

    for f in failures:
        print(f)
    if failures:
        print(f"{len(failures)} failure(s) across {len(rules)} rules")
        return 1
    print(f"{len(rules)} rules pass their fixtures and the comment layout")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
