#!/usr/bin/env python3
"""Scheduler and landing queue for the aven-ultraclose skill.

Run every command from inside the repository. The run directory holds
state.json, rules.md, notes.md, logs/, and one directory per task. The
repository's git config records its path as ultraclose.run, so a later
session finds the run without arguments.

  init --run DIR [--limit N] [--rounds N] [--check CMD]
       [--shared GLOB]... [--disk-floor-gb N] [--force]
  triage FILE            add or update tasks from a JSON list
  next                   start one task per free slot, print each Workflow input
  args REF [--round N] [--final] [--sync REASON]
                         print the Workflow input that relaunches REF
  set REF STATE [--flag TEXT]
  land REF               land REF on the base branch, one landing at a time
  status
  watch                  exit when free disk space runs low
"""

import argparse
import fcntl
import fnmatch
import json
import re
import shutil
import subprocess
import sys
import time
from contextlib import contextmanager
from pathlib import Path

STATES = ("pending", "inflight", "approved", "landed", "noted", "closed")
SETTLED = ("landed", "closed")
PRIORITY = {"urgent": 0, "high": 1, "medium": 2, "low": 3, "none": 4}
# Files that match a shared glob hold no lock, so two running tasks may
# both edit them. The scheduler matches each glob against "/" plus the
# path relative to the repository. The run config holds these globs
# plus the glob of each init --shared flag.
DEFAULT_SHARED = [
    "*_test.*",
    "*/test_*",
    "*.test.*",
    "*.spec.*",
    "*/tests/*",
    "*/testdata/*",
    "*/__tests__/*",
]


def die(msg):
    sys.exit(f"ultraclose: {msg}")


def sh(*cmd, cwd, check=True):
    p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, check=False)
    if check and p.returncode:
        die(f"{' '.join(cmd)} failed in {cwd}:\n{p.stdout}{p.stderr}")
    return p


def out(*cmd, cwd):
    return sh(*cmd, cwd=cwd).stdout.strip()


def main_checkout():
    p = subprocess.run(
        ["git", "worktree", "list", "--porcelain"],
        capture_output=True,
        text=True,
        check=False,
    )
    if p.returncode:
        die("run this from inside a git repository")
    return p.stdout.splitlines()[0].split(" ", 1)[1]


def run_dir(repo):
    p = sh("git", "config", "--get", "ultraclose.run", cwd=repo, check=False)
    path = p.stdout.strip()
    if not path or not Path(path, "state.json").exists():
        die("no run found for this repository, start one with init")
    return Path(path)


@contextmanager
def locked(path):
    with open(path, "w") as f:
        fcntl.flock(f, fcntl.LOCK_EX)
        yield


@contextmanager
def state(run):
    """Load state.json under a lock and save it when the block ends."""
    with locked(run / "state.lock"):
        st = json.loads((run / "state.json").read_text())
        yield st
        tmp = run / "state.json.tmp"
        tmp.write_text(json.dumps(st, indent=1, sort_keys=True) + "\n")
        tmp.replace(run / "state.json")


def worktree(cfg, ref):
    return f"{cfg['worktrees']}/t-{ref}"


def branch(ref):
    return f"task/{ref}"


def locks(cfg, task):
    return {
        f
        for f in task["files"]
        if not any(fnmatch.fnmatch("/" + f, g) for g in cfg["shared"])
    }


def blockers(st, task):
    """The tasks that must settle before this task starts."""
    tasks = st["tasks"]
    return [a for a in task["after"] if a in tasks and tasks[a]["state"] not in SETTLED]


def pick(st):
    cfg, tasks = st["config"], st["tasks"]
    busy = [r for r, t in tasks.items() if t["state"] in ("inflight", "approved")]
    free = cfg["limit"] - sum(1 for r in busy if tasks[r]["state"] == "inflight")
    held = set().union(*(locks(cfg, tasks[r]) for r in busy)) if busy else set()
    last_busy = any(tasks[r]["last"] for r in busy)
    others_open = any(
        not t["last"] and t["state"] in ("pending", "inflight", "approved")
        for t in tasks.values()
    )
    pending = sorted(
        (r for r, t in tasks.items() if t["state"] == "pending"),
        key=lambda r: (tasks[r]["last"], PRIORITY.get(tasks[r]["priority"], 9), r),
    )
    chosen = []
    for ref in pending:
        t = tasks[ref]
        if len(chosen) >= free:
            break
        if blockers(st, t):
            continue
        # A task marked last waits for every other task and runs alone.
        if t["last"] and (others_open or last_busy):
            continue
        mine = locks(cfg, t)
        if mine & held:
            continue
        held |= mine
        last_busy = last_busy or t["last"]
        chosen.append(ref)
    return chosen


def workflow_args(st, run, ref, resume=None):
    cfg, t = st["config"], st["tasks"][ref]
    task = {
        "ref": ref,
        "title": t["title"],
        "wt": worktree(cfg, ref),
        "dir": str(run / "tasks" / ref),
    }
    if resume:
        task["resume"] = resume
    return {
        "run": str(run),
        "repo": cfg["repo"],
        "base": cfg["base"],
        "rounds": cfg["rounds"],
        "task": task,
    }


def cmd_init(a):
    repo = main_checkout()
    old = sh("git", "config", "--get", "ultraclose.run", cwd=repo, check=False)
    old = old.stdout.strip()
    if old and Path(old, "state.json").exists() and not a.force:
        die(f"a run exists at {old}. Continue it, or pass --force to start over")
    base = out("git", "branch", "--show-current", cwd=repo)
    if not base:
        die("the main checkout has a detached HEAD")
    run = Path(a.run).resolve()
    for d in (run, run / "tasks", run / "logs"):
        d.mkdir(parents=True, exist_ok=True)
    ignored = sh(
        "git", "check-ignore", "-q", ".claude/worktrees/x", cwd=repo, check=False
    )
    if ignored.returncode:
        common = out(
            "git", "rev-parse", "--path-format=absolute", "--git-common-dir", cwd=repo
        )
        with open(Path(common, "info", "exclude"), "a") as f:
            f.write(".claude/worktrees/\n")
    cfg = {
        "repo": repo,
        "base": base,
        "worktrees": f"{repo}/.claude/worktrees",
        "limit": a.limit,
        "rounds": a.rounds,
        "check": a.check,
        "shared": list(dict.fromkeys(DEFAULT_SHARED + (a.shared or []))),
        "diskFloorGb": a.disk_floor_gb,
    }
    (run / "state.json").write_text(json.dumps({"config": cfg, "tasks": {}}))
    sh("git", "config", "--local", "ultraclose.run", str(run), cwd=repo)
    print(f"run={run} repo={repo} base={base} limit={a.limit}")


def cmd_triage(a):
    repo = main_checkout()
    run = run_dir(repo)
    rows = json.loads(Path(a.file).read_text())
    with state(run) as st:
        added = updated = 0
        for row in rows:
            ref = row["ref"]
            files = sorted(
                {f.removeprefix(repo + "/").removeprefix("./") for f in row["files"]}
            )
            new = {
                "title": row["title"],
                "priority": row.get("priority", "none"),
                "files": files,
                "after": row.get("after", []),
                "last": bool(row.get("last", False)),
            }
            old = st["tasks"].get(ref)
            if old:
                old.update(new)
                updated += 1
            else:
                st["tasks"][ref] = {
                    **new,
                    "state": "pending",
                    "startedAt": 0,
                    "flags": [],
                }
                added += 1
    print(f"added={added} updated={updated}")


def start(st, run, ref):
    """Give a task its worktree, its task file, and the active status."""
    cfg, t = st["config"], st["tasks"][ref]
    repo, wt = cfg["repo"], worktree(cfg, ref)
    if not Path(wt).exists():
        has_branch = not sh(
            "git",
            "rev-parse",
            "-q",
            "--verify",
            f"refs/heads/{branch(ref)}",
            cwd=repo,
            check=False,
        ).returncode
        if has_branch:
            sh("git", "worktree", "add", "-q", wt, branch(ref), cwd=repo)
        else:
            sh(
                "git",
                "worktree",
                "add",
                "-q",
                wt,
                "-b",
                branch(ref),
                cfg["base"],
                cwd=repo,
            )
    d = run / "tasks" / ref
    (d / "scratch").mkdir(parents=True, exist_ok=True)
    (d / "task.md").write_text(out("aven", "context", ref, cwd=repo) + "\n")
    sh("aven", "edit", ref, "--status", "active", cwd=repo)
    t["state"] = "inflight"
    t["startedAt"] = time.time()


def cmd_next(a):
    repo = main_checkout()
    run = run_dir(repo)
    with state(run) as st:
        launched = []
        for ref in pick(st):
            start(st, run, ref)
            launched.append(workflow_args(st, run, ref))
    print(json.dumps(launched, indent=1))


def cmd_args(a):
    repo = main_checkout()
    run = run_dir(repo)
    with state(run) as st:
        t = st["tasks"].get(a.ref) or die(f"unknown task {a.ref}")
        history = run / "tasks" / a.ref / "history.md"
        done = 0
        if history.exists():
            done = len(re.findall(r"^## Round \d+", history.read_text(), re.MULTILINE))
        rnd = a.round or done + 1
        if a.final:
            last = rnd
        elif a.sync:
            last = rnd + 1
        else:
            last = max(st["config"]["rounds"], rnd)
        resume = {"round": rnd, "lastRound": last}
        if a.sync:
            resume["sync"] = a.sync
        was = t["state"]
        start(st, run, a.ref)
        # A task with no commit of its own starts again at the implementer.
        ahead = out(
            "git",
            "rev-list",
            "--count",
            f"{st['config']['base']}..{branch(a.ref)}",
            cwd=repo,
        )
        if ahead == "0" and not a.sync:
            resume = None
        print(json.dumps(workflow_args(st, run, a.ref, resume), indent=1))
        print(f"{a.ref}: {was} to inflight", file=sys.stderr)


def cmd_set(a):
    repo = main_checkout()
    run = run_dir(repo)
    with state(run) as st:
        cfg = st["config"]
        t = st["tasks"].get(a.ref) or die(f"unknown task {a.ref}")
        t["state"] = a.state
        if a.flag:
            t["flags"].append(a.flag)
        wt = worktree(cfg, a.ref)
        if a.state in ("noted", "closed") and Path(wt).exists():
            if out("git", "status", "--porcelain", cwd=wt):
                print(f"kept dirty worktree {wt}")
            else:
                sh("git", "worktree", "remove", wt, cwd=repo)
                if a.state == "closed":
                    sh("git", "branch", "-q", "-D", branch(a.ref), cwd=repo)
    print(f"{a.ref} {a.state}")


def free_gb(path):
    return shutil.disk_usage(path).free / 2**30


def cmd_status(a):
    repo = main_checkout()
    run = run_dir(repo)
    with state(run) as st:
        cfg, tasks = st["config"], st["tasks"]
    now = time.time()
    counts = {s: sum(1 for t in tasks.values() if t["state"] == s) for s in STATES}
    print(f"run={run}")
    print(" ".join(f"{s}={n}" for s, n in counts.items()), f"limit={cfg['limit']}")
    for ref, t in sorted(tasks.items()):
        if t["state"] == "inflight":
            hours = (now - t["startedAt"]) / 3600
            print(f"inflight {ref} {hours:.1f}h | {t['title']}")
        elif t["state"] == "approved":
            print(f"approved {ref} waits for landing | {t['title']}")
        elif t["state"] == "pending" and (b := blockers(st, t)):
            noted = [r for r in b if tasks[r]["state"] == "noted"]
            if noted:
                print(f"stuck {ref} waits for noted {' '.join(noted)} | {t['title']}")
    for ref, t in sorted(tasks.items()):
        for flag in t["flags"]:
            print(f"flag {ref} {t['state']}: {flag}")
    print(f"free disk: {free_gb(repo):.0f} GB (floor {cfg['diskFloorGb']} GB)")


def cmd_watch(a):
    repo = main_checkout()
    run = run_dir(repo)
    while True:
        st = json.loads((run / "state.json").read_text())
        cfg = st["config"]
        gb = free_gb(repo)
        if gb < cfg["diskFloorGb"]:
            print(f"DISK free={gb:.0f}GB floor={cfg['diskFloorGb']}GB")
            return
        time.sleep(60)


def tail(path, n=40):
    return "\n".join(Path(path).read_text(errors="replace").splitlines()[-n:])


def cmd_land(a):
    repo = main_checkout()
    run = run_dir(repo)
    ref = a.ref
    with state(run) as st:
        cfg = st["config"]
        if ref not in st["tasks"]:
            die(f"unknown task {ref}")
        # A task that waits for its landing frees its slot and keeps its
        # file locks.
        st["tasks"][ref]["state"] = "approved"
        prefixes = sorted({r.split("-")[0] for r in st["tasks"]})
    refs = re.compile(rf"\b(?:{'|'.join(map(re.escape, prefixes))})-[0-9A-Z]{{4,}}\b")
    wt, br, base = worktree(cfg, ref), branch(ref), cfg["base"]
    msg = run / "tasks" / ref / "msg.txt"
    logs = run / "logs"

    def fail(code, detail=""):
        print(f"RESULT {ref} {code}")
        if detail:
            print(detail)
        sys.exit(2)

    def logged(name, *cmd, cwd):
        path = logs / f"{ref}.{name}.log"
        with open(path, "w") as f:
            rc = subprocess.run(
                cmd, cwd=cwd, stdout=f, stderr=subprocess.STDOUT, check=False
            )
        return rc.returncode, path

    with locked(run / "land.lock"):
        if out("git", "branch", "--show-current", cwd=repo) != base:
            fail("NOTBASE", f"the main checkout is not on {base}")
        if dirty := out("git", "status", "--porcelain", cwd=repo):
            fail("DIRTY", dirty)
        if not Path(wt).exists():
            fail("NOWORKTREE", wt)
        if dirty := out("git", "status", "--porcelain", cwd=wt):
            fail("WTDIRTY", dirty)
        if not msg.exists() or not msg.read_text().strip():
            fail("NOMSG", str(msg))
        if found := refs.findall(msg.read_text()):
            fail("MSGHASREF", " ".join(sorted(set(found))))

        # Bring the base branch into the task branch, so that the check
        # runs on the tree that the commit records.
        behind = sh(
            "git", "merge-base", "--is-ancestor", base, br, cwd=repo, check=False
        )
        if behind.returncode:
            rc, log = logged(
                "merge", "git", "merge", "--no-verify", "--no-edit", base, cwd=wt
            )
            if rc:
                files = out("git", "diff", "--name-only", "--diff-filter=U", cwd=wt)
                sh("git", "merge", "--abort", cwd=wt, check=False)
                fail("CONFLICT", files or tail(log))
        added = [
            line
            for line in out(
                "git", "diff", "-U0", f"{base}...{br}", cwd=repo
            ).splitlines()
            if line.startswith("+") and not line.startswith("+++") and refs.search(line)
        ]
        if added:
            fail("REFINDIFF", "\n".join(added[:20]))
        if cfg["check"]:
            rc, log = logged("check", "sh", "-c", cfg["check"], cwd=wt)
            if rc:
                fail("CHECKFAIL", tail(log))
            if dirty := out("git", "status", "--porcelain", cwd=wt):
                fail("WTDIRTY", "the check changed files:\n" + dirty)

        head = out("git", "rev-parse", "HEAD", cwd=repo)

        def rollback():
            sh("git", "reset", "-q", "--hard", head, cwd=repo)
            sh("git", "clean", "-qfd", cwd=repo)

        rc, log = logged("squash", "git", "merge", "--squash", br, cwd=repo)
        if rc:
            rollback()
            fail("CONFLICT", tail(log))
        if not sh(
            "git", "diff", "--cached", "--quiet", cwd=repo, check=False
        ).returncode:
            fail("EMPTY", f"{br} holds no change against {base}")
        want = out("git", "write-tree", cwd=repo)
        # A hook once emptied the index of the main checkout during a
        # commit. The commit must record the staged tree, so compare the
        # two and retry one time.
        for attempt in (1, 2):
            rc, log = logged("commit", "git", "commit", "-F", str(msg), cwd=repo)
            if rc:
                rollback()
                fail("COMMITFAIL", tail(log))
            if out("git", "rev-parse", "HEAD^{tree}", cwd=repo) == want:
                break
            if out("git", "rev-parse", "HEAD~1", cwd=repo) != head or attempt == 2:
                rollback()
                fail("TREEMISMATCH", f"the commit did not record tree {want}")
            sh("git", "reset", "-q", "--soft", head, cwd=repo)
            sh("git", "read-tree", want, cwd=repo)
        if dirty := out("git", "status", "--porcelain", cwd=repo):
            fail("POSTDIRTY", dirty)
        sh("git", "worktree", "remove", "--force", wt, cwd=repo)
        sh("git", "branch", "-q", "-D", br, cwd=repo)
        sh("aven", "edit", ref, "--status", "done", cwd=repo)
        with state(run) as st:
            st["tasks"][ref]["state"] = "landed"
        print(f"RESULT {ref} OK {out('git', 'log', '-1', '--format=%h %s', cwd=repo)}")


def main():
    p = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    sub = p.add_subparsers(dest="cmd", required=True)

    s = sub.add_parser("init")
    s.add_argument("--run", required=True)
    s.add_argument("--limit", type=int, default=5)
    s.add_argument("--rounds", type=int, default=6)
    s.add_argument("--check", default="")
    s.add_argument(
        "--shared",
        action="append",
        metavar="GLOB",
        help="add a glob to the default test-file globs",
    )
    s.add_argument("--disk-floor-gb", type=float, default=30)
    s.add_argument("--force", action="store_true")
    s.set_defaults(fn=cmd_init)

    s = sub.add_parser("triage")
    s.add_argument("file")
    s.set_defaults(fn=cmd_triage)

    sub.add_parser("next").set_defaults(fn=cmd_next)

    s = sub.add_parser("args")
    s.add_argument("ref")
    s.add_argument("--round", type=int)
    s.add_argument("--final", action="store_true")
    s.add_argument("--sync")
    s.set_defaults(fn=cmd_args)

    s = sub.add_parser("set")
    s.add_argument("ref")
    s.add_argument("state", choices=STATES)
    s.add_argument("--flag")
    s.set_defaults(fn=cmd_set)

    s = sub.add_parser("land")
    s.add_argument("ref")
    s.set_defaults(fn=cmd_land)

    sub.add_parser("status").set_defaults(fn=cmd_status)
    sub.add_parser("watch").set_defaults(fn=cmd_watch)

    a = p.parse_args()
    a.fn(a)


if __name__ == "__main__":
    main()
