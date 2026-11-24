"""Compare the findings of two prose-lint binaries on the same inputs.

Usage:
  compare.py FIRST SECOND [RULE...] [--root DIR] [--jobs N]

FIRST and SECOND name two prose-lint binaries, such as the installed
prose-lint and a build of a branch. Run the script from the root of the
repository. It lints these inputs with both binaries:

- Each file under configs/harper/fixtures. Both binaries skip a path
  under harper/fixtures, so the script lints a copy of each file in a
  temporary directory, and it names the copy by the path of the file.
- Each file that git tracks. Each binary skips a file whose extension
  it does not know.
- The files of the measure.py corpus, for each source whose clone the
  root of `measure.py fetch` holds. --root names that root, and its
  default is the root of `measure.py fetch`.
- The last 200 commit messages, each read with --commit.

The script prints one row for each rule, with the count of findings
that only FIRST prints, that only SECOND prints, and that both print.
A finding counts as the same when both binaries print the same line
for it, message included. Each RULE, such as ProseHedge or
Style::ProseHedge, adds a list of each finding of that rule that only
one binary prints, each above the source line that it names.
A `<` marks a finding of FIRST, and a `>` marks a finding of SECOND.

The script runs the binaries in parallel, --jobs at a time, and exits
0 when the binaries differ. It warns on stderr for each input on which
a binary exits with a status other than 0 or 1. It exits 1 without a
report when it cannot run a binary.
"""

import argparse
import os
import re
import shutil
import subprocess
import sys
import tempfile
from collections import Counter, defaultdict
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path

# Importing measure writes a __pycache__ directory beside it unless this
# flag is set.
sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "configs" / "harper"))

import measure  # noqa: E402

FIXTURES = Path("configs/harper/fixtures")
COMMITS = 200

# The part of a finding after its path, such as
# `3:14: [Recommended] Style::ProseHedge: Hedge.` A harper kind can hold
# a space, as in `Word Choice::VeryUnique`.
FINDING = re.compile(r"^(?P<line>\d+):\d+: \[[^\]]*\] (?P<rule>[^:]+::\w+): ")


@dataclass(frozen=True)
class Input:
    """One lint of both binaries.

    label names the input in the report, and path names the file that
    the binaries lint. A commit message has no path, and message holds
    its text.
    """

    label: str
    path: Path | None = None
    message: str = ""

    def source_line(self, line: int) -> str:
        if self.path is None:
            text = self.message
        else:
            text = self.path.read_text(errors="replace")
        lines = text.splitlines()
        return lines[line - 1] if 0 < line <= len(lines) else ""


@dataclass(frozen=True)
class Finding:
    input: Input
    rest: str

    @property
    def rule(self) -> str:
        m = FINDING.match(self.rest)
        return m["rule"] if m else "(unparsed)"

    @property
    def line(self) -> int:
        m = FINDING.match(self.rest)
        return int(m["line"]) if m else 0


def git(*args: str) -> str:
    return subprocess.run(["git", *args], capture_output=True, text=True, check=True).stdout


def fixture_inputs(tmp: Path) -> list[Input]:
    inputs = []
    for path in sorted(p for p in FIXTURES.rglob("*") if p.is_file()):
        copy = tmp / "fixtures" / path.relative_to(FIXTURES)
        copy.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, copy)
        inputs.append(Input(str(path), copy))
    return inputs


def tracked_inputs() -> list[Input]:
    names = git("ls-files", "-z").split("\0")
    return [Input(n, Path(n)) for n in names if n and Path(n).is_file()]


def corpus_inputs(root: Path) -> tuple[list[Input], list[str]]:
    """Return the files of each source that root holds, and the missing sources."""
    sources, exclude, _ = measure.load_corpus()
    inputs, missing = [], []
    for src in sources:
        dest = root / src.repo
        if not (dest / ".git").exists():
            missing.append(src.repo)
            continue
        for path in measure.source_files(root, src, exclude):
            inputs.append(Input(f"{src.repo}/{path.relative_to(dest)}", path))
    return inputs, missing


def commit_inputs() -> list[Input]:
    records = git("log", "-z", f"-{COMMITS}", "--format=%H%n%B").split("\0")
    inputs = []
    for record in records:
        if record:
            sha, _, message = record.partition("\n")
            inputs.append(Input(f"commit {sha[:12]}", message=message))
    return inputs


def lint(binary: str, item: Input) -> Counter[Finding]:
    if item.path is None:
        cmd, stdin, prefix = [binary, "--commit"], item.message, "<stdin>:"
    else:
        cmd, stdin, prefix = [binary, str(item.path)], "", f"{item.path}:"
    proc = subprocess.run(cmd, input=stdin, capture_output=True, text=True)
    if proc.returncode not in (0, 1):
        print(
            f"compare.py: {binary} exited {proc.returncode} on {item.label}: {proc.stderr.strip()}",
            file=sys.stderr,
        )
    return Counter(Finding(item, line.removeprefix(prefix)) for line in proc.stdout.splitlines() if line)


def print_table(first: Counter[Finding], second: Counter[Finding]) -> None:
    counts: dict[str, list[int]] = defaultdict(lambda: [0, 0, 0])
    for column, found in enumerate((first - second, second - first, first & second)):
        for finding, n in found.items():
            counts[finding.rule][column] += n
    rows = [("rule", "only first", "only second", "both")]
    rows += [(rule, *map(str, counts[rule])) for rule in sorted(counts)]
    width = max(len(r[0]) for r in rows)
    for rule, *cells in rows:
        print(f"{rule:<{width}}  {cells[0]:>10}  {cells[1]:>11}  {cells[2]:>6}")


def print_rule(name: str, first: Counter[Finding], second: Counter[Finding]) -> None:
    print(f"\n{name}:")
    marked = [(f, "<") for f in first - second] + [(f, ">") for f in second - first]
    marked = [m for m in marked if name in (m[0].rule, m[0].rule.split("::")[-1])]
    if not marked:
        print("  no finding that only one binary prints")
    for finding, mark in sorted(marked, key=lambda m: (m[0].input.label, m[0].line, m[1], m[0].rest)):
        print(f"{mark} {finding.input.label}:{finding.rest}")
        print(f"    {finding.input.source_line(finding.line)}")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("first", help="the first prose-lint binary")
    parser.add_argument("second", help="the second prose-lint binary")
    parser.add_argument("rules", nargs="*", metavar="RULE", help="list the differences of this rule")
    parser.add_argument("--root", type=Path, default=measure.default_root(), help="root of the corpus clones")
    parser.add_argument("--jobs", type=int, default=os.cpu_count() or 1, help="binaries to run at a time")
    args = parser.parse_args(argv)
    for binary in (args.first, args.second):
        if shutil.which(binary) is None:
            sys.exit(f"compare.py: cannot run {binary}")

    with tempfile.TemporaryDirectory(prefix="prose-compare-") as tmp:
        fixtures = fixture_inputs(Path(tmp))
        tracked = tracked_inputs()
        corpus, missing = corpus_inputs(args.root)
        commits = commit_inputs()
        inputs = fixtures + tracked + corpus + commits
        jobs = [(b, item) for item in inputs for b in (args.first, args.second)]
        with ThreadPoolExecutor(max_workers=args.jobs) as pool:
            results = list(pool.map(lambda job: lint(*job), jobs))

        print(
            f"Linted {len(fixtures)} fixtures, {len(tracked)} tracked files, "
            f"{len(corpus)} corpus files, and {len(commits)} commit messages."
        )
        if missing:
            print(f"{args.root} does not hold the corpus clones of {', '.join(missing)}.")
        print()
        # jobs alternates the binaries, so every even result is FIRST's.
        first, second = Counter(), Counter()
        for i, found in enumerate(results):
            (first if i % 2 == 0 else second).update(found)
        print_table(first, second)
        for name in args.rules:
            print_rule(name, first, second)
    return 0


if __name__ == "__main__":
    sys.exit(main())
