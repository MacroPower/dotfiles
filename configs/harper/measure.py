"""Count a rule's hits on a pinned corpus and sample them for precision.

Usage:
  measure.py fetch [--root DIR]
  measure.py run RULE... [--corpus docs|go|all] [--sample N] [--seed N]
                 [--root DIR]

corpus.json pins each source to a repository, a commit, and the paths
to read. A docs source contributes its markdown files, and a go source
contributes the comments of its Go files. The exclude list drops every
file that has a path part matching one of its globs.

fetch sparse-clones each source at its pinned commit into
$XDG_CACHE_HOME/prose-corpus, or ~/.cache/prose-corpus when the
variable is unset. --root names a directory that already holds the
clones as <owner>/<repo>, such as /tmp/git, and run reads from the
same place. run warns when a clone sits at another commit, because its
counts then differ from the ones a rule comment cites.

run lints each file of the corpus with `prose-lint-rs --only RULES
--json` and reads the hits from its output. PROSE_LINT names the
binary, and run looks for prose-lint-rs on PATH when the variable is
unset. Build the binary with
`nix build --no-link --print-out-paths .#prose-lint-rs` from the
checkout that holds the rules to measure, because the binary embeds
the rules when it builds.

The flake build reads only the files that git tracks, so git add a
new rule file first. The .#prose-lint-rs build also runs the rule
tests. They fail on a rule without a fixture pair, without the
Overview and Measurement sections in its comment, or with a finding on
fixtures/common.good.md. For a draft rule or a candidate built-in,
build the binary without the tests. The Measurement section of
configs/harper/CLAUDE.md gives the command, and so does the message
that run prints when it cannot find the binary.

The binary runs the Weir rules, the Harper built-ins that the
builtins.txt of a tier lists, and the native checks of
tools/prose-lint/src/checks. It never runs an awk rule. To measure a
changed awk rule, make the same change in its native check. To
measure a Harper built-in that no tier lists, add it to the
builtins.txt of a tier on a branch, and build the binary there.

run passes each rule to --only as given. A bare name (ProseHedge) runs
the rule in every tier, and the binary then drops a hit of a lower
tier when a higher tier reports the same line with the same message.
<tier>/<Rule> (optional/ProseHedge) runs the rule of one tier and
counts each of its hits. The binary fails on a name that no rule of
the tier has.

run prints the harper-core version, a hash of corpus.json, and a table
of the files, hits, and lines with a hit for each source. The `top`
column gives the share of the source's hits that its busiest file
holds. When run names two or more rules, it then prints the hits of
each name, zero included. Last, it prints a seeded sample of hits.
Each sampled hit shows its location, its message, and the line around
it.
"""

import argparse
import fnmatch
import hashlib
import json
import os
import random
import shutil
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path

HERE = Path(__file__).resolve().parent
CORPUS = HERE / "corpus.json"

# The extensions each corpus kind reads.
EXTENSIONS = {"docs": (".md",), "go": (".go",)}


@dataclass(frozen=True)
class Source:
    repo: str
    corpus: str
    commit: str
    paths: tuple[str, ...]


@dataclass(frozen=True)
class Hit:
    source: str
    name: str
    line: int
    col: int
    tier: str
    rule: str
    message: str
    context: str


def load_corpus() -> tuple[list[Source], list[str], str]:
    raw = CORPUS.read_bytes()
    data = json.loads(raw)
    sources = [
        Source(s["repo"], s["corpus"], s["commit"], tuple(s["paths"])) for s in data["sources"]
    ]
    return sources, data["exclude"], hashlib.sha256(raw).hexdigest()[:12]


def default_root() -> Path:
    cache = os.environ.get("XDG_CACHE_HOME") or str(Path.home() / ".cache")
    return Path(cache) / "prose-corpus"


def git(dest: Path, *args: str) -> str:
    proc = subprocess.run(
        ["git", "-C", str(dest), *args], capture_output=True, text=True, check=True
    )
    return proc.stdout.strip()


def head(dest: Path) -> str | None:
    if not (dest / ".git").exists():
        return None
    try:
        return git(dest, "rev-parse", "HEAD")
    except subprocess.CalledProcessError:
        return None


def fetch(root: Path, sources: list[Source]) -> int:
    for src in sources:
        dest = root / src.repo
        if head(dest) == src.commit:
            print(f"{src.repo}: at {src.commit[:8]}")
            continue
        if not (dest / ".git").exists():
            dest.mkdir(parents=True, exist_ok=True)
            git(dest, "init", "-q")
            git(dest, "remote", "add", "origin", f"https://github.com/{src.repo}.git")
        if src.paths:
            git(dest, "sparse-checkout", "set", *src.paths)
        else:
            git(dest, "sparse-checkout", "disable")
        print(f"{src.repo}: fetching {src.commit[:8]}")
        git(dest, "fetch", "-q", "--depth", "1", "origin", src.commit)
        git(dest, "checkout", "-q", "--detach", "FETCH_HEAD")
    return 0


def excluded(rel: Path, exclude: list[str]) -> bool:
    return any(fnmatch.fnmatch(part, glob) for part in rel.parts for glob in exclude)


def source_files(root: Path, src: Source, exclude: list[str]) -> list[Path]:
    dest = root / src.repo
    tops = [dest / p for p in src.paths] or [dest]
    found: set[Path] = set()
    for top in tops:
        for path in top.rglob("*"):
            rel = path.relative_to(dest)
            if (
                path.is_file()
                and path.suffix in EXTENSIONS[src.corpus]
                and ".git" not in rel.parts
                and not excluded(rel, exclude)
            ):
                found.add(path)
    return sorted(found)


def run_tool(cmd: list[str], ok: tuple[int, ...] = (0,)) -> str:
    """Return the command's stdout, or exit with its stderr when its status is not in ok."""
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True)
    except OSError as err:
        sys.exit(f"measure.py: cannot run {cmd[0]}: {err.strerror}")
    if proc.returncode not in ok:
        sys.exit(f"measure.py: {' '.join(cmd)} exited {proc.returncode}: {proc.stderr.strip()}")
    return proc.stdout


def read_line(path: Path, line: int) -> str:
    lines = path.read_text(errors="replace").splitlines()
    return lines[line - 1] if 0 < line <= len(lines) else ""


def lint(binary: str, rules: list[str], src: str, dest: Path, path: Path) -> list[Hit]:
    """Return the hits of the rules on the file at path, which sits in the clone at dest."""
    out = run_tool([binary, "--only", ",".join(rules), "--json", str(path)], ok=(0, 1))
    hits = []
    for line in out.splitlines():
        f = json.loads(line)
        hits.append(
            Hit(
                src,
                str(path.relative_to(dest)),
                f["line"],
                f["col"],
                f["tier"],
                f["rule"],
                f["message"],
                read_line(path, f["line"]),
            )
        )
    return hits


def excerpt(context: str, col: int, width: int = 160) -> str:
    text = context.strip()
    if len(text) <= width:
        return text
    offset = len(context) - len(context.lstrip())
    start = max(0, col - 1 - offset - width // 2)
    end = start + width
    return ("..." if start else "") + text[start:end] + ("..." if end < len(text) else "")


def run(args: argparse.Namespace, sources: list[Source], exclude: list[str], digest: str) -> int:
    root = Path(args.root) if args.root else default_root()
    binary = os.environ.get("PROSE_LINT", "prose-lint-rs")

    chosen = [s for s in sources if args.corpus in ("all", s.corpus)]
    for src in chosen:
        at = head(root / src.repo)
        if at is None:
            sys.exit(f"measure.py: no clone of {src.repo} under {root}; run measure.py fetch")
        if at != src.commit:
            print(
                f"warning: {src.repo} is at {at[:8]}, not the pinned {src.commit[:8]}",
                file=sys.stderr,
            )

    if shutil.which(binary) is None:
        sys.exit(
            f"measure.py: cannot find {binary}. Build the binary with"
            " `nix build --no-link --print-out-paths .#prose-lint-rs`, and set"
            " PROSE_LINT to bin/prose-lint-rs under the path that the build prints."
            " The build runs the rule tests, so for a draft rule, build with"
            " `nix build --no-link --print-out-paths --impure --expr 'let f ="
            ' builtins.getFlake "git+file://${toString ./.}"; in'
            " f.packages.${builtins.currentSystem}.prose-lint-rs.overrideAttrs"
            " { doCheck = false; }'` instead. Run either build from the"
            " repository root after you git add a new rule file."
        )
    version = run_tool([binary, "--version"]).strip()
    files = {src.repo: source_files(root, src, exclude) for src in chosen}
    work = [(src.repo, root / src.repo, path) for src in chosen for path in files[src.repo]]
    with ThreadPoolExecutor() as pool:
        found = pool.map(lambda w: lint(binary, args.rules, *w), work)
        hits = [h for file_hits in found for h in file_hits]

    print(f"harper-core {version}, corpus {digest}")
    print(f"rules: {', '.join(args.rules)}")
    print()

    def top(found: list[Hit]) -> str:
        if not found:
            return "-"
        per_file: dict[tuple[str, str], int] = {}
        for h in found:
            per_file[(h.source, h.name)] = per_file.get((h.source, h.name), 0) + 1
        return f"{100 * max(per_file.values()) // len(found)}%"

    rows = [("source", "files", "hits", "lines", "top")]
    for src in chosen:
        mine = [h for h in hits if h.source == src.repo]
        lines = {(h.name, h.line) for h in mine}
        rows.append(
            (src.repo, str(len(files[src.repo])), str(len(mine)), str(len(lines)), top(mine))
        )
    total_lines = {(h.source, h.name, h.line) for h in hits}
    rows.append(
        (
            "total",
            str(sum(len(f) for f in files.values())),
            str(len(hits)),
            str(len(total_lines)),
            top(hits),
        )
    )
    widths = [max(len(row[i]) for row in rows) for i in range(len(rows[0]))]
    for row in rows:
        print(
            row[0].ljust(widths[0]),
            *(cell.rjust(width) for cell, width in zip(row[1:], widths[1:])),
            sep="  ",
        )
    entries = [e for rule in args.rules for e in rule.split(",")]
    if len(entries) > 1:
        print()
        for entry in entries:
            count = sum(entry in (h.rule, f"{h.tier.lower()}/{h.rule}") for h in hits)
            print(f"{entry}: {count} {'hit' if count == 1 else 'hits'}")

    if args.sample and hits:
        sample = random.Random(args.seed).sample(hits, min(args.sample, len(hits)))
        sample.sort(key=lambda h: (h.source, h.name, h.line, h.col))
        print()
        print(f"Sample of {len(sample)} of {len(hits)} hits (seed {args.seed}):")
        for h in sample:
            print()
            print(f"{h.source}/{h.name}:{h.line}:{h.col}  [{h.tier}] {h.rule}")
            print(f"  message: {h.message}")
            print(f"  context: {excerpt(h.context, h.col)}")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(
        description="Count a rule's hits on a pinned corpus and sample them."
    )
    sub = parser.add_subparsers(dest="command", required=True)
    fetch_cmd = sub.add_parser("fetch", help="clone the corpus at its pinned commits")
    fetch_cmd.add_argument("--root", help="directory that holds the clones as <owner>/<repo>")
    run_cmd = sub.add_parser("run", help="lint the corpus with the named rules")
    run_cmd.add_argument("rules", nargs="+", metavar="RULE")
    run_cmd.add_argument("--corpus", choices=("docs", "go", "all"), default="all")
    run_cmd.add_argument("--sample", type=int, default=10, help="hits to print (default 10)")
    run_cmd.add_argument("--seed", type=int, default=1, help="seed for the sample (default 1)")
    run_cmd.add_argument("--root", help="directory that holds the clones as <owner>/<repo>")
    args = parser.parse_args(argv[1:])

    sources, exclude, digest = load_corpus()
    if args.command == "fetch":
        return fetch(Path(args.root) if args.root else default_root(), sources)
    return run(args, sources, exclude, digest)


if __name__ == "__main__":
    sys.exit(main(sys.argv))
