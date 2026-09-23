"""Check every Prose rule and enabled built-in rule against its fixture pair.

Usage: check.py <weirpack> <fixtures dir> <heading.awk> <builtin-rules.txt>

For each rule in the weirpack and each name in builtin-rules.txt,
fixtures/<Rule>.bad.md must produce a <Rule> lint on every non-blank
line and fixtures/<Rule>.good.md must produce none. heading.awk checks
ProseHeading in place of Harper. A rule without a fixture pair fails, so
a typo in a rule name cannot pass silently. Exits 1 with one line per
failure.
"""

import json
import subprocess
import sys
import zipfile
from pathlib import Path

HEADING_RULE = "ProseHeading"


def rule_names(pack: Path) -> list[str]:
    with zipfile.ZipFile(pack) as zf:
        return sorted(Path(n).stem for n in zf.namelist() if n.endswith(".weir"))


def builtin_names(listing: Path) -> list[str]:
    names = []
    for line in listing.read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#"):
            names.append(line)
    return names


def harper_lines(pack: Path, rule: str, target: Path) -> set[int]:
    """Return the 1-based lines on which Harper reports `rule` in `target`."""
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
    lines: set[int] = set()
    for file_report in json.loads(proc.stdout):
        for lint in file_report["lints"]:
            if lint["rule"] == rule:
                lines.add(int(lint["line"]))
    return lines


def awk_lines(awk: Path, target: Path) -> set[int]:
    proc = subprocess.run(
        ["awk", "-f", str(awk), str(target)],
        capture_output=True,
        text=True,
        check=True,
    )
    lines: set[int] = set()
    for line in proc.stdout.splitlines():
        lines.add(int(line.split(":", 3)[1]))
    return lines


def expected_lines(target: Path) -> set[int]:
    return {
        i for i, line in enumerate(target.read_text().splitlines(), start=1) if line.strip()
    }


def check_rule(
    rule: str, fixtures: Path, report: "callable"
) -> list[str]:
    bad = fixtures / f"{rule}.bad.md"
    good = fixtures / f"{rule}.good.md"
    failures: list[str] = []
    if not bad.is_file() or not good.is_file():
        failures.append(f"{rule}: missing fixture pair {bad.name} and {good.name}")
        return failures

    want = expected_lines(bad)
    got = report(bad)
    for line in sorted(want - got):
        failures.append(f"{bad}:{line}: expected a {rule} lint")
    for line in sorted(report(good)):
        failures.append(f"{good}:{line}: unexpected {rule} lint")
    return failures


def main(argv: list[str]) -> int:
    if len(argv) != 5:
        print(__doc__.strip(), file=sys.stderr)
        return 2
    pack = Path(argv[1])
    fixtures = Path(argv[2])
    awk = Path(argv[3])
    builtins = Path(argv[4])

    rules = rule_names(pack) + [HEADING_RULE] + builtin_names(builtins)
    failures: list[str] = []
    for rule in rules:
        if rule == HEADING_RULE:
            failures += check_rule(rule, fixtures, lambda t: awk_lines(awk, t))
        else:
            failures += check_rule(rule, fixtures, lambda t, r=rule: harper_lines(pack, r, t))

    for f in failures:
        print(f)
    if failures:
        print(f"{len(failures)} fixture failure(s) across {len(rules)} rules")
        return 1
    print(f"{len(rules)} rules pass their fixtures")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
