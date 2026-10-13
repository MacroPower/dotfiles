"""Count a rule's hits on a pinned corpus and sample them for precision.

Usage:
  measure.py fetch [--root DIR]
  measure.py run [RULE...] [--weir FILE | --awk FILE] [--corpus docs|go|all]
                 [--sample N] [--seed N] [--root DIR]

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

run takes each rule by its file name (ProseHedge), or as <tier>/<Rule>
(optional/ProseHedge) for one tier. A name that no tier holds runs as a
Harper built-in. --weir and --awk add a scratch rule from a file that
no tier holds. The Weir rules run from a scratch weirpack that holds
every tier's rules under their internal names and the scratch rule as
<stem>_Scratch. Go comments reach the rules through the shadow that
`prose-lint --shadow` prints, which the hook lints too. Set PROSE_LINT
to run a prose-lint other than the one on PATH. The awk checks read
markdown only, so they skip the go sources.

run prints the harper version, a hash of corpus.json, the hits and the
lines with a hit for each source, and a seeded sample of hits. Each
sampled hit shows its location, the text the rule matched, and the line
around it.
"""

import argparse
import fnmatch
import hashlib
import json
import os
import random
import re
import shutil
import subprocess
import sys
import tempfile
import zipfile
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path

# Importing check writes a __pycache__ directory beside it unless this
# flag is set, and git shows the directory as untracked.
sys.dont_write_bytecode = True

from check import TIERS, Rule, collect_rules  # noqa: E402

HERE = Path(__file__).resolve().parent
RULES = HERE / "rules"
LIB = HERE / "lib"
CORPUS = HERE / "corpus.json"

# harper-cli lints this many files per run. A run of about 400 files
# takes under a second.
BATCH = 400

# The extension Harper reads a shadow as, keyed by the source kind.
# Rust strips `//` from every line of a comment run.
SHADOW_EXT = {"go": ".rs"}

# The extensions each corpus kind reads.
EXTENSIONS = {"docs": (".md",), "go": (".go",)}

AWK_LINE = re.compile(r"^(?P<path>.*?):(?P<line>\d+):(?P<col>\d+): \w+::(?P<rule>\w+): (?P<message>.*)$")

MANIFEST = {
    "author": "Jacob Colvin",
    "version": "0.1.0",
    "description": "Scratch pack for measure.py",
    "license": "Apache-2.0",
}


@dataclass(frozen=True)
class Source:
    repo: str
    corpus: str
    commit: str
    paths: tuple[str, ...]


@dataclass(frozen=True)
class Doc:
    """One linted file: where Harper reads it and how a hit names it."""

    source: str
    name: str
    lint_path: Path


@dataclass(frozen=True)
class Hit:
    source: str
    name: str
    line: int
    col: int
    rule: str
    text: str
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


def run_tool(cmd: list[str], env: dict[str, str] | None = None) -> str:
    """Return the command's stdout, or exit with its stderr when it fails."""
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True, env=env)
    except OSError as err:
        sys.exit(f"measure.py: cannot run {cmd[0]}: {err.strerror}")
    if proc.returncode != 0:
        sys.exit(f"measure.py: {' '.join(cmd)} exited {proc.returncode}: {proc.stderr.strip()}")
    return proc.stdout


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


def resolve(names: list[str]) -> tuple[list[Rule], list[str]]:
    """Return the tier rules the names pick and the names left for built-ins."""
    rules, _ = collect_rules(RULES)
    picked: list[Rule] = []
    builtins: list[str] = []
    for name in names:
        tier = None
        if "/" in name:
            tier, name = name.split("/", 1)
            if tier not in TIERS:
                sys.exit(f"measure.py: {tier} is not a tier")
        hits = [r for r in rules if r.name == name and tier in (None, r.tier)]
        if hits:
            picked += hits
        elif tier is None:
            builtins.append(name)
        else:
            sys.exit(f"measure.py: no {name} rule under rules/{tier}")
    return picked, builtins


def build_pack(tmp: Path, scratch: Path | None) -> Path:
    """Zip every tier's Weir rules and the scratch rule the way the build does."""
    pack_dir = tmp / "pack"
    pack_dir.mkdir()
    rules, _ = collect_rules(RULES)
    for rule in rules:
        if rule.kind == "weir":
            shutil.copy(rule.path, pack_dir / f"{rule.internal}.weir")
    if scratch is not None:
        shutil.copy(scratch, pack_dir / f"{scratch.stem}_Scratch.weir")
    (pack_dir / "manifest.json").write_text(json.dumps(MANIFEST))
    pack = tmp / "scratch.weirpack"
    with zipfile.ZipFile(pack, "w") as zf:
        for path in sorted(pack_dir.iterdir()):
            zf.write(path, path.name)
    return pack


def harper(*args: str, env: dict[str, str]) -> subprocess.CompletedProcess:
    return subprocess.run(["harper-cli", *args], capture_output=True, text=True, env=env)


def make_shadows(docs: list[tuple[Source, Path, Path]], tmp: Path) -> list[Doc]:
    """Write each Go file's comment shadow to tmp and return the linted files."""
    prose_lint = os.environ.get("PROSE_LINT", "prose-lint")
    shadow_dir = tmp / "shadow"
    shadow_dir.mkdir()

    def one(item: tuple[int, tuple[Source, Path, Path]]) -> Doc:
        i, (src, dest, path) = item
        out = shadow_dir / f"{i}{SHADOW_EXT[src.corpus]}"
        out.write_text(run_tool([prose_lint, "--shadow", str(path)]))
        return Doc(src.repo, str(path.relative_to(dest)), out)

    with ThreadPoolExecutor() as pool:
        return list(pool.map(one, enumerate(docs)))


def read_line(path: Path, line: int) -> str:
    lines = path.read_text(errors="replace").splitlines()
    return lines[line - 1] if 0 < line <= len(lines) else ""


def lint_weir(docs: list[Doc], pack: Path, only: list[str], env: dict[str, str]) -> list[Hit]:
    hits: list[Hit] = []
    for start in range(0, len(docs), BATCH):
        batch = docs[start : start + BATCH]
        proc = harper(
            "lint",
            "--weirpacks",
            str(pack),
            "--only",
            ",".join(only),
            "-o",
            "--format",
            "json",
            "--quiet",
            *(str(d.lint_path) for d in batch),
            env=env,
        )
        if proc.returncode not in (0, 1):
            sys.exit(f"harper-cli exited {proc.returncode}: {proc.stderr}")
        reports = json.loads(proc.stdout) if proc.stdout.strip() else []
        # harper-cli names each file by its basename only, in input
        # order, so a report maps to its input by position.
        if len(reports) != len(batch):
            sys.exit(f"harper-cli returned {len(reports)} reports for {len(batch)} files")
        for doc, report in zip(batch, reports):
            for lint in report["lints"]:
                hits.append(
                    Hit(
                        doc.source,
                        doc.name,
                        int(lint["line"]),
                        int(lint["column"]),
                        lint["rule"],
                        lint.get("matched_text", ""),
                        read_line(doc.lint_path, int(lint["line"])),
                    )
                )
    return hits


def lint_awk(docs: list[Doc], checks: list[tuple[str, Path]]) -> list[Hit]:
    env = {**os.environ, "AWKPATH": str(LIB)}

    def one(item: tuple[Doc, tuple[str, Path]]) -> list[Hit]:
        doc, (internal, script) = item
        found = []
        for out in run_tool(["awk", "-f", str(script), str(doc.lint_path)], env).splitlines():
            m = AWK_LINE.match(out)
            if m:
                line = int(m["line"])
                found.append(
                    Hit(
                        doc.source,
                        doc.name,
                        line,
                        int(m["col"]),
                        internal,
                        m["message"],
                        read_line(doc.lint_path, line),
                    )
                )
        return found

    work = [(doc, check) for doc in docs for check in checks]
    with ThreadPoolExecutor() as pool:
        return [hit for found in pool.map(one, work) for hit in found]


def excerpt(context: str, col: int, width: int = 160) -> str:
    text = context.strip()
    if len(text) <= width:
        return text
    offset = len(context) - len(context.lstrip())
    start = max(0, col - 1 - offset - width // 2)
    end = start + width
    return ("..." if start else "") + text[start:end] + ("..." if end < len(text) else "")


def run(args: argparse.Namespace, sources: list[Source], exclude: list[str], digest: str) -> int:
    if not (args.rules or args.weir or args.awk):
        sys.exit("measure.py: name a rule, or pass --weir or --awk")
    picked, builtins = resolve(args.rules)
    root = Path(args.root) if args.root else default_root()

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

    weir_only = [r.internal for r in picked if r.kind != "awk"] + builtins
    checks = [(f"{r.name}_{r.tier.capitalize()}", r.path) for r in picked if r.kind == "awk"]
    scratch_weir = Path(args.weir) if args.weir else None
    if scratch_weir:
        weir_only.append(f"{scratch_weir.stem}_Scratch")
    if args.awk:
        checks.append((f"{Path(args.awk).stem}_Scratch", Path(args.awk)))

    with tempfile.TemporaryDirectory() as tmp_name:
        tmp = Path(tmp_name)
        env = {**os.environ, "HOME": str(tmp)}
        version = harper("--version", env=env).stdout.strip()
        pack = build_pack(tmp, scratch_weir)
        if scratch_weir:
            proc = harper("test", "--no-color", str(scratch_weir), env=env)
            if proc.returncode != 0:
                print(proc.stdout + proc.stderr, end="")
                return 1

        files = {src.repo: source_files(root, src, exclude) for src in chosen}
        md_docs = [
            Doc(src.repo, str(path.relative_to(root / src.repo)), path)
            for src in chosen
            if src.corpus == "docs"
            for path in files[src.repo]
        ]
        go_docs = make_shadows(
            [
                (src, root / src.repo, path)
                for src in chosen
                if src.corpus != "docs"
                for path in files[src.repo]
            ],
            tmp,
        )

        hits: list[Hit] = []
        if weir_only:
            hits += lint_weir(md_docs + go_docs, pack, weir_only, env)
        if checks:
            hits += lint_awk(md_docs, checks)

    names = weir_only + [c[0] for c in checks]
    print(f"{version}, corpus {digest}")
    print(f"rules: {', '.join(names)}")
    print()
    rows = [("source", "files", "hits", "lines")]
    for src in chosen:
        mine = [h for h in hits if h.source == src.repo]
        lines = {(h.name, h.line) for h in mine}
        rows.append((src.repo, str(len(files[src.repo])), str(len(mine)), str(len(lines))))
    total_lines = {(h.source, h.name, h.line) for h in hits}
    rows.append(
        ("total", str(sum(len(f) for f in files.values())), str(len(hits)), str(len(total_lines)))
    )
    widths = [max(len(row[i]) for row in rows) for i in range(4)]
    for row in rows:
        print(
            row[0].ljust(widths[0]),
            *(cell.rjust(width) for cell, width in zip(row[1:], widths[1:])),
            sep="  ",
        )
    if len(names) > 1:
        print()
        for name in names:
            count = sum(1 for h in hits if h.rule == name)
            print(f"{name}: {count} {'hit' if count == 1 else 'hits'}")

    if args.sample and hits:
        sample = random.Random(args.seed).sample(hits, min(args.sample, len(hits)))
        sample.sort(key=lambda h: (h.source, h.name, h.line, h.col))
        print()
        print(f"Sample of {len(sample)} of {len(hits)} hits (seed {args.seed}):")
        for h in sample:
            print()
            print(f"{h.source}/{h.name}:{h.line}:{h.col}  {h.rule}")
            print(f"  matched: {h.text}")
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
    run_cmd.add_argument("rules", nargs="*", metavar="RULE")
    scratch = run_cmd.add_mutually_exclusive_group()
    scratch.add_argument("--weir", metavar="FILE", help="also run this Weir rule")
    scratch.add_argument("--awk", metavar="FILE", help="also run this awk check")
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
