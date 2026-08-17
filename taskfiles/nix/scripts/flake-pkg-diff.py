#!/usr/bin/env python3
# /// script
# dependencies = ["certifi"]
# ///
"""Diff installed package versions between two states of this flake.

Usage: flake-pkg-diff.py [OLD_REV]

Evaluates the flake's `inventory` output at OLD_REV (a git rev, default HEAD)
and in the working tree, then reports version changes for exactly the packages
the host configurations install - regardless of which flake input provides
them (nixpkgs, NURs, custom pkgs/). Homebrew casks and brews declared by the
darwin hosts are diffed against the old/new tap revisions in flake.lock by
fetching their definitions from GitHub.

This narrows a many-thousand-commit nixpkgs bump down to the version changes
that can actually affect these machines. Output is JSON:

  {"packages": {"changed": [...], "added": [...], "removed": [...]},
   "casks": {"changed": [...], "unresolved": [...]},
   "brews": {"changed": [...], "unresolved": [...]}}
"""

import json
import os
import re
import ssl
import subprocess
import sys
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor

import certifi

VERSION_RE = re.compile(r'^\s*version "([^"]+)"', re.MULTILINE)

# Nix-provided Pythons ship without a default CA bundle path.
SSL_CONTEXT = ssl.create_default_context(cafile=os.environ.get("SSL_CERT_FILE") or certifi.where())


def run(cmd: list[str]) -> str:
    return subprocess.run(cmd, capture_output=True, text=True, check=True).stdout


def eval_inventory(flake_ref: str) -> dict:
    return json.loads(run(["nix", "eval", "--raw", f"{flake_ref}#inventory"]))


def package_versions(inventory: dict) -> dict[str, str]:
    """Merge nixPackages and programs across hosts into name -> version."""
    versions: dict[str, set[str]] = {}
    for host in inventory.values():
        for entry in host.get("nixPackages", []) + host.get("programs", []):
            if entry.get("version"):
                versions.setdefault(entry["name"], set()).add(entry["version"])
    return {name: "|".join(sorted(vs)) for name, vs in versions.items()}


def diff_versions(old: dict[str, str], new: dict[str, str]) -> dict:
    return {
        "changed": [
            {"name": n, "old": old[n], "new": new[n]}
            for n in sorted(old.keys() & new.keys())
            if old[n] != new[n]
        ],
        "added": [{"name": n, "new": new[n]} for n in sorted(new.keys() - old.keys())],
        "removed": [{"name": n, "old": old[n]} for n in sorted(old.keys() - new.keys())],
    }


def tap_rev(lock: dict, input_name: str) -> str | None:
    key = lock["nodes"]["root"]["inputs"].get(input_name)
    if not isinstance(key, str):
        return None
    return lock["nodes"].get(key, {}).get("locked", {}).get("rev")


def fetch_version(repo: str, rev: str, paths: list[str]) -> str | None:
    """Fetch a formula/cask definition at a rev and extract its version."""
    for path in paths:
        url = f"https://raw.githubusercontent.com/{repo}/{rev}/{path}"
        try:
            with urllib.request.urlopen(url, timeout=15, context=SSL_CONTEXT) as resp:
                match = VERSION_RE.search(resp.read().decode())
                return match.group(1) if match else None
        except urllib.error.HTTPError as e:
            if e.code == 404:
                continue
            raise
    return None


def diff_brew_items(names: set[str], repo: str, path_fmt: str, old_rev: str, new_rev: str) -> dict:
    """Diff versions of casks/brews between two tap revisions."""

    def lookup(name: str) -> dict:
        short = name.rsplit("/", 1)[-1]
        paths = [path_fmt.format(shard=short[0], name=short), path_fmt.format(shard="", name=short).replace("//", "/")]
        try:
            old = fetch_version(repo, old_rev, paths)
            new = fetch_version(repo, new_rev, paths)
        except (urllib.error.URLError, OSError):
            old = new = None
        return {"name": name, "old": old, "new": new}

    with ThreadPoolExecutor(max_workers=16) as pool:
        results = list(pool.map(lookup, sorted(names)))
    return {
        "changed": [r for r in results if r["old"] and r["new"] and r["old"] != r["new"]],
        "unresolved": [r["name"] for r in results if r["old"] is None and r["new"] is None],
    }


def main() -> int:
    if len(sys.argv) > 2 or sys.argv[1:2] in (["-h"], ["--help"]):
        print(__doc__.strip(), file=sys.stderr)
        return 2
    old_sha = run(["git", "rev-parse", sys.argv[1] if len(sys.argv) > 1 else "HEAD"]).strip()

    # Pin the rev explicitly: on a dirty tree, a bare git+file:. resolves to a
    # copy of the working tree (dirtyRev), which would make old == new.
    with ThreadPoolExecutor(max_workers=2) as pool:
        old_fut = pool.submit(eval_inventory, f"git+file:.?rev={old_sha}")
        new_fut = pool.submit(eval_inventory, ".")
        old_inv, new_inv = old_fut.result(), new_fut.result()

    result = {"packages": diff_versions(package_versions(old_inv), package_versions(new_inv))}

    old_lock = json.loads(run(["git", "show", f"{old_sha}:flake.lock"]))
    with open("flake.lock") as f:
        new_lock = json.load(f)

    casks = {c for host in new_inv.values() for c in host.get("homebrewCasks", [])}
    brews = {b for host in new_inv.values() for b in host.get("homebrewBrews", [])}
    for section, names, input_name, repo, path_fmt in [
        ("casks", casks, "homebrew-cask", "Homebrew/homebrew-cask", "Casks/{shard}/{name}.rb"),
        ("brews", brews, "homebrew-core", "Homebrew/homebrew-core", "Formula/{shard}/{name}.rb"),
    ]:
        old_rev, new_rev = tap_rev(old_lock, input_name), tap_rev(new_lock, input_name)
        if not names or not old_rev or not new_rev or old_rev == new_rev:
            result[section] = {"changed": [], "unresolved": []}
            continue
        result[section] = diff_brew_items(names, repo, path_fmt, old_rev, new_rev)

    json.dump(result, sys.stdout, indent=2)
    print()
    return 0


if __name__ == "__main__":
    sys.exit(main())
