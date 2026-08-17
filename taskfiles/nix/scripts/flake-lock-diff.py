#!/usr/bin/env python3
"""Diff two flake.lock files and print changed direct inputs as JSON.

Usage: flake-lock-diff.py [OLD] [NEW]

OLD is a lock file path or a git rev whose flake.lock to read (default: HEAD).
NEW is a lock file path (default: ./flake.lock).

Only inputs of the root flake are reported; transitive nodes (flake-parts_2,
systems_3, ...) churn constantly and are not actionable. Output is a JSON
array, one object per changed input, with the upstream repo, old/new revs and
dates, and a compare URL when the host supports one.
"""

import json
import subprocess
import sys
from datetime import datetime, timezone


def load_lock(spec: str) -> dict:
    """Load a lock file from a path, or from a git rev's flake.lock."""
    try:
        with open(spec) as f:
            return json.load(f)
    except FileNotFoundError:
        out = subprocess.run(
            ["git", "show", f"{spec}:flake.lock"],
            capture_output=True,
            text=True,
            check=True,
        ).stdout
        return json.loads(out)


def node_for(lock: dict, name: str) -> dict | None:
    key = lock["nodes"]["root"]["inputs"].get(name)
    if not isinstance(key, str):  # lists are follows-references, not real nodes
        return None
    return lock["nodes"].get(key)


def date_of(node: dict) -> str | None:
    ts = node.get("locked", {}).get("lastModified")
    if ts is None:
        return None
    return datetime.fromtimestamp(ts, tz=timezone.utc).strftime("%Y-%m-%d")


def repo_of(node: dict) -> tuple[str, str | None]:
    """Return (repo description, compare-url host) for a node."""
    locked = node.get("locked", {})
    original = node.get("original", {})
    kind = locked.get("type") or original.get("type") or "?"
    if kind in ("github", "gitlab"):
        owner, repo = locked.get("owner"), locked.get("repo")
        return f"{kind}:{owner}/{repo}", kind
    return original.get("url") or locked.get("url") or kind, None


def compare_url(host: str | None, repo: str, old: str, new: str) -> str | None:
    if host == "github":
        return f"https://github.com/{repo.split(':', 1)[1]}/compare/{old}...{new}"
    if host == "gitlab":
        return f"https://gitlab.com/{repo.split(':', 1)[1]}/-/compare/{old}...{new}"
    return None


def main() -> int:
    if len(sys.argv) > 3 or sys.argv[1:2] in (["-h"], ["--help"]):
        print(__doc__.strip(), file=sys.stderr)
        return 2
    old_lock = load_lock(sys.argv[1] if len(sys.argv) > 1 else "HEAD")
    new_lock = load_lock(sys.argv[2] if len(sys.argv) > 2 else "flake.lock")

    changes = []
    for name in sorted(new_lock["nodes"]["root"]["inputs"]):
        new_node = node_for(new_lock, name)
        if new_node is None:
            continue
        old_node = node_for(old_lock, name)
        new_rev = new_node.get("locked", {}).get("rev") or new_node.get("locked", {}).get("narHash")
        old_rev = (old_node or {}).get("locked", {}).get("rev") or (old_node or {}).get("locked", {}).get("narHash")
        if old_rev == new_rev:
            continue

        repo, host = repo_of(new_node)
        entry = {
            "input": name,
            "repo": repo,
            "old_rev": old_rev,
            "new_rev": new_rev,
            "old_date": date_of(old_node) if old_node else None,
            "new_date": date_of(new_node),
            "flake": new_node.get("flake", True),
        }
        ref = new_node.get("original", {}).get("ref")
        if ref:
            entry["ref"] = ref
        if old_rev and new_rev:
            url = compare_url(host, repo, old_rev, new_rev)
            if url:
                entry["compare_url"] = url
        changes.append(entry)

    json.dump(changes, sys.stdout, indent=2)
    print()
    return 0


if __name__ == "__main__":
    sys.exit(main())
