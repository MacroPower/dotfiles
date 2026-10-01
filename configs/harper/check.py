"""Check every rule in the tier directories against its fixture pair.

Usage: check.py <weirpack> <fixtures dir> <rules dir>

A rule's tier is the directory under rules/ that holds it: required,
recommended, or optional. A .weir file is a Weir rule the weirpack
carries under the internal name <Rule>_<Tier>, an .awk file is a check
run with awk in place of Harper, and builtins.txt names the Harper
built-ins the tier enables. For each rule, fixtures/<tier>/<Rule>.bad.md
must produce a lint on every non-blank line, and neither
fixtures/<tier>/<Rule>.good.md nor the shared fixtures/common.good.md
may produce one. The shared file holds ordinary technical prose that
every rule must pass, so a line added there covers all the rules.

A rule without a fixture pair fails, so a typo in a built-in name
cannot pass silently. A fixture pair without a rule fails too, so a
rule moved between tiers takes its fixtures along. A built-in listed
under two tiers fails, and so does an entry under rules/ that is not a
tier directory. Exits 1 with one line per failure.
"""

import json
import subprocess
import sys
import zipfile
from dataclasses import dataclass
from pathlib import Path

TIERS = ("required", "recommended", "optional")
COMMON = "common.good.md"


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


def check_rule(rule: Rule, pack: Path, fixtures: Path) -> list[str]:
    bad = fixtures / rule.tier / f"{rule.name}.bad.md"
    good = fixtures / rule.tier / f"{rule.name}.good.md"
    failures: list[str] = []
    if not bad.is_file() or not good.is_file():
        failures.append(f"{rule.tier}/{rule.name}: missing fixture pair {bad.name} and {good.name}")
        return failures

    if rule.kind == "awk":

        def report(target: Path) -> set[int]:
            return awk_lines(rule.path, target)

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
    if len(argv) != 4:
        print(__doc__.strip(), file=sys.stderr)
        return 2
    pack = Path(argv[1])
    fixtures = Path(argv[2])
    rules_dir = Path(argv[3])

    if not (fixtures / COMMON).is_file():
        print(f"{fixtures / COMMON}: missing the shared good fixture")
        return 1
    rules, failures = collect_rules(rules_dir)
    packed = pack_names(pack)
    for rule in rules:
        if rule.kind == "weir" and rule.internal not in packed:
            failures.append(f"{rule.path}: not in the weirpack as {rule.internal}")
    failures += orphan_fixtures(fixtures, rules)
    for rule in rules:
        failures += check_rule(rule, pack, fixtures)

    for f in failures:
        print(f)
    if failures:
        print(f"{len(failures)} fixture failure(s) across {len(rules)} rules")
        return 1
    print(f"{len(rules)} rules pass their fixtures")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
