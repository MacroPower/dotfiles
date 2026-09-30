"""Check every rule in the tier manifest against its fixture pair.

Usage: check.py <weirpack> <fixtures dir> <heading.awk> <rules.toml>

For each name under a tier in rules.toml, fixtures/<Rule>.bad.md must
produce a <Rule> lint on every non-blank line and fixtures/<Rule>.good.md
must produce none. heading.awk checks ProseHeading in place of Harper.
A rule without a fixture pair fails, so a typo in a built-in name
cannot pass silently. A Weir rule in the weirpack that the manifest
does not list fails too, as does a name under two tiers or a tier key
the manifest does not define, so an untiered rule cannot ship. Exits 1
with one line per failure.
"""

import json
import subprocess
import sys
import tomllib
import zipfile
from pathlib import Path

HEADING_RULE = "ProseHeading"
TIERS = ("required", "recommended", "optional")


def rule_names(pack: Path) -> list[str]:
    with zipfile.ZipFile(pack) as zf:
        return sorted(Path(n).stem for n in zf.namelist() if n.endswith(".weir"))


def manifest_rules(manifest: Path) -> tuple[list[str], list[str]]:
    """Return every rule name in tier order and the manifest's failures."""
    data = tomllib.loads(manifest.read_text())
    names: list[str] = []
    failures: list[str] = []
    for key in data:
        if key not in TIERS:
            failures.append(f"{manifest}: unknown tier [{key}]")
    for tier in TIERS:
        for name in data.get(tier, {}).get("rules", []):
            if name in names:
                failures.append(f"{manifest}: {name} appears under two tiers")
                continue
            names.append(name)
    return names, failures


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
    manifest = Path(argv[4])

    rules, failures = manifest_rules(manifest)
    for weir in rule_names(pack):
        if weir not in rules:
            failures.append(f"{manifest}: {weir} has a .weir file but no tier")
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
