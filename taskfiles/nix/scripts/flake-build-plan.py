#!/usr/bin/env python3
# /// script
# dependencies = []
# ///
"""Report what each host compiles locally instead of fetching from a cache.

Usage: flake-build-plan.py [OLD_REV] [--host SUBSTRING]... [--no-base]

For every host configuration in the working tree (darwin, NixOS, and
home-manager), the script evaluates the system derivation and runs
`nix build --dry-run` against it. Nix answers with the store paths it fetches
from a substituter and the derivations it builds on this machine. The
to-be-built derivations fall into three groups:

  - downloads: fixed-output derivations such as fetchurl. Network only.
  - always local: derivations with preferLocalBuild or
    allowSubstitutes = false (writeText, runCommandLocal, wrappers). A cache
    never carries them.
  - cache misses: everything else. Each one is a compile that the binary
    cache was expected to cover and does not.

The script compares cache misses by name with the same evaluation at OLD_REV
(a git rev, default HEAD) and flags a miss absent there as `new`. The flag
separates the misses a bump introduced from the ones this repo's overlays
always cause. A new miss on a compiler, a runtime, or a browser means Hydra
has not built it for that platform or its build failed, so the switch
compiles it for hours and might fail at the end.

The dry run skips store paths the local machine already holds. Run the script
where neither revision is built, such as the Lima VM, and the comparison is
complete. On a host that has switched to OLD_REV, the baseline shrinks to
nothing and every listed miss reads as new.

The script also checks for overlay drift. The flake's `overlayTargets` output
names every nixpkgs attribute that this repo's overlays modify in place. For
each one, the script reads the name, src, and patches of the plain nixpkgs
derivation at both revisions, before any overlay applies. A difference means
nixpkgs changed the package under the overlay, which a version diff cannot
see and which breaks an overlay that re-sources or patches it. The fabric
overlay failed that way when nixpkgs adopted the patch it carried. Pass
--overlay-target to check extra attributes.

Output is JSON:

  {"hosts": {"<host>": {"system": "aarch64-darwin",
                        "cache_misses": [{"name": "...", "drv": "/nix/store/...", "new": true}],
                        "downloads": N, "always_local": N, "fetched": N}},
   "overlay_drift": [{"attr": "python3Packages.fabric", "changed": ["patches"],
                      "old": {"name": "...", "src": "...", "patches": [...]},
                      "new": {...}}]}
"""

import argparse
import json
import re
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor

# Each host kind with the attribute that holds its system derivation.
HOST_KINDS = [
    ("darwinConfigurations", "config.system.build.toplevel"),
    ("nixosConfigurations", "config.system.build.toplevel"),
    ("homeConfigurations", "activationPackage"),
]

STORE_PATH_RE = re.compile(r"^\s+(/nix/store/\S+)")
NAME_RE = re.compile(r"^/nix/store/[a-z0-9]{32}-(.+?)(\.drv)?$")
VERSION_SUFFIX_RE = re.compile(r"-[0-9].*$")


def run(cmd: list[str]) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, capture_output=True, text=True, check=True)


def drv_name(path: str) -> str:
    match = NAME_RE.match(path)
    return match.group(1) if match else path


def pname(name: str) -> str:
    """Strip the version the way lib.getName does, so a bump of a recurring
    local build does not read as a new cache miss."""
    return VERSION_SUFFIX_RE.sub("", name)


def resolve(flake_ref: str) -> str:
    """Fetch a flake reference once and return its source tree in the store.

    Every later evaluation reads that copy, so git sees one fetch per revision
    instead of one per host. On the virtiofs mount inside the Lima VM, git
    sometimes reads an empty index (DTF-HHGG), which makes Nix report a tree
    without flake.nix. That failure is transient, so the fetch gets one retry.
    Evaluating a store copy leaves `self.rev` unset, which only blanks
    `system.configurationRevision`.
    """
    for attempt in range(2):
        proc = subprocess.run(["nix", "flake", "metadata", "--json", flake_ref], capture_output=True, text=True)
        if proc.returncode == 0:
            return json.loads(proc.stdout)["path"]
        if attempt or "does not contain a '/flake.nix' file" not in proc.stderr:
            raise subprocess.CalledProcessError(proc.returncode, proc.args, proc.stdout, proc.stderr)
    raise AssertionError("unreachable")


def list_hosts(flake_ref: str, filters: list[str]) -> list[tuple[str, str, str]]:
    """Return (host name, configuration kind, system derivation attribute) triples."""
    hosts = []
    for kind, suffix in HOST_KINDS:
        try:
            names = json.loads(run(["nix", "eval", "--json", f"{flake_ref}#{kind}", "--apply", "builtins.attrNames"]).stdout)
        except subprocess.CalledProcessError:
            continue
        for name in names:
            if filters and not any(f in name for f in filters):
                continue
            hosts.append((name, kind, suffix))
    return hosts


def dry_run(flake_ref: str, name: str, kind: str, suffix: str) -> tuple[str, list[str], list[str]]:
    """Evaluate one host and return (system drv, drvs to build, paths to fetch)."""
    # One evaluation yields the system derivation and the host's own cache
    # settings. The dry run then asks the caches that host uses, not the ones
    # configured on the machine running this script.
    info = json.loads(
        run(
            [
                "nix",
                "eval",
                "--json",
                f'{flake_ref}#{kind}."{name}"',
                "--apply",
                f"c: {{ drv = c.{suffix}.drvPath;"
                " substituters = c.config.nix.settings.substituters or [];"
                " keys = c.config.nix.settings.trusted-public-keys or []; }",
            ]
        ).stdout
    )
    drv = info["drv"]
    options = []
    if info["substituters"]:
        options += ["--option", "substituters", " ".join(dict.fromkeys(info["substituters"]))]
    if info["keys"]:
        options += ["--option", "trusted-public-keys", " ".join(dict.fromkeys(info["keys"]))]
    # Nix prints the plan on stderr and exits 0; nothing to do prints nothing.
    stderr = subprocess.run(["nix", "build", "--dry-run", *options, f"{drv}^*"], capture_output=True, text=True, check=True).stderr
    to_build, to_fetch, section = [], [], None
    for line in stderr.splitlines():
        if "will be built" in line:
            section = to_build
        elif "will be fetched" in line:
            section = to_fetch
        elif (match := STORE_PATH_RE.match(line)) and section is not None:
            section.append(match.group(1))
        else:
            section = None
    return drv, to_build, to_fetch


def derivation_attrs(drvs: list[str]) -> dict[str, dict]:
    """Return env attributes per derivation, with structured attrs merged in."""
    if not drvs:
        return {}
    shown = json.loads(run(["nix", "derivation", "show", *drvs]).stdout)
    attrs = {}
    for path, info in shown.items():
        env = dict(info.get("env", {}))
        if "__json" in env:
            env.update(json.loads(env.pop("__json")))
        env["__outputs"] = info.get("outputs", {})
        env["__system"] = info.get("system", "")
        attrs[path] = env
    return attrs


def classify(attrs: dict) -> str:
    if any("hash" in out for out in attrs["__outputs"].values()):
        return "download"
    if attrs.get("preferLocalBuild") in (True, "1"):
        return "local"
    if "allowSubstitutes" in attrs and attrs["allowSubstitutes"] in (False, ""):
        return "local"
    # Lix drops preferLocalBuild and allowSubstitutes from the stored
    # environment of a derivation with structured attributes. The
    # writeTextFile family (writeText, writeShellScript, and friends)
    # therefore carries neither flag here. Its `text` plus `buildCommand`
    # pair is distinctive.
    if "text" in attrs and "buildCommand" in attrs:
        return "local"
    return "miss"


def plan_host(flake_ref: str, name: str, kind: str, suffix: str) -> dict:
    drv, to_build, to_fetch = dry_run(flake_ref, name, kind, suffix)
    attrs = derivation_attrs([drv, *to_build])
    groups: dict[str, list[str]] = {"download": [], "local": [], "miss": []}
    for path in to_build:
        groups[classify(attrs[path])].append(path)
    return {
        "system": attrs[drv]["__system"],
        "system_drv": drv,
        "misses": sorted(groups["miss"], key=drv_name),
        "downloads": len(groups["download"]),
        "always_local": len(groups["local"]),
        "fetched": len(to_fetch),
    }


DERIVATION_FACTS = """
let
  pkgs = import {nixpkgs} {{
    system = "x86_64-linux";
    config = {{
      allowUnfree = true;
      allowBroken = true;
      allowUnsupportedSystem = true;
      allowInsecurePredicate = _: true;
    }};
  }};
  lib = pkgs.lib;
  targets = builtins.fromJSON ''{targets}'';
  get = path: lib.attrByPath (lib.splitString "." path) null pkgs;
  srcOf =
    p:
    let
      s = p.src or null;
    in
    if s == null then null else if lib.isDerivation s then s.outPath else toString s;
  patchName = x: if lib.isDerivation x then x.name else baseNameOf (toString x);
  facts =
    p:
    if p == null || !(lib.isDerivation p) then
      null
    else
      {{
        name = p.name;
        src = srcOf p;
        patches = map patchName (p.patches or [ ]);
      }};
in
lib.listToAttrs (map (t: lib.nameValuePair t (facts (get t))) targets)
"""


def nixpkgs_path(flake_src: str) -> str:
    """Return the store path of the nixpkgs input locked by a flake source tree."""
    expr = f'(builtins.getFlake "{flake_src}").inputs.nixpkgs.outPath'
    return run(["nix", "eval", "--raw", "--impure", "--expr", expr]).stdout.strip()


def overlay_targets(flake_src: str) -> list[str]:
    return json.loads(run(["nix", "eval", "--raw", f"{flake_src}#overlayTargets"]).stdout)


def derivation_facts(nixpkgs: str, targets: list[str]) -> dict[str, dict | None]:
    """Read name, src, and patches of plain nixpkgs derivations, before any overlay."""
    if not targets:
        return {}
    expr = DERIVATION_FACTS.format(nixpkgs=nixpkgs, targets=json.dumps(targets))
    return json.loads(run(["nix", "eval", "--json", "--impure", "--expr", expr]).stdout)


def overlay_drift(old_src: str, new_src: str, extra_targets: list[str]) -> list[dict]:
    targets = sorted(set(overlay_targets(new_src)) | set(extra_targets))
    with ThreadPoolExecutor(max_workers=2) as pool:
        old_fut = pool.submit(lambda: derivation_facts(nixpkgs_path(old_src), targets))
        new_fut = pool.submit(lambda: derivation_facts(nixpkgs_path(new_src), targets))
        old, new = old_fut.result(), new_fut.result()
    drift = []
    for attr in targets:
        before, after = old.get(attr), new.get(attr)
        if before == after:
            continue
        if before is None or after is None:
            changed = ["missing at old revision" if before is None else "missing at new revision"]
        else:
            changed = [key for key in ("name", "src", "patches") if before[key] != after[key]]
        drift.append({"attr": attr, "changed": changed, "old": before, "new": after})
    return drift


def plan_all(flake_ref: str, filters: list[str]) -> dict[str, dict]:
    hosts = list_hosts(flake_ref, filters)
    # Each evaluation holds one host configuration in memory, so run three
    # at a time.
    with ThreadPoolExecutor(max_workers=3) as pool:
        plans = pool.map(lambda h: plan_host(flake_ref, *h), hosts)
    return {name: plan for (name, _, _), plan in zip(hosts, plans, strict=True)}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("old_rev", nargs="?", default="HEAD", help="git rev to compare cache misses against (default HEAD)")
    parser.add_argument(
        "--host", action="append", default=[], metavar="SUBSTRING", help="only hosts whose name contains SUBSTRING (repeatable)"
    )
    parser.add_argument(
        "--no-base", action="store_true", help="skip the OLD_REV comparison; every miss is reported without a `new` flag"
    )
    parser.add_argument(
        "--overlay-target",
        action="append",
        default=[],
        metavar="ATTR",
        help="also diff this nixpkgs attribute path for overlay drift (repeatable)",
    )
    args = parser.parse_args()

    new_src = resolve(".")
    new_plans = plan_all(new_src, args.host)

    old_names: dict[str, set[str]] = {}
    drift: list[dict] = []
    if not args.no_base:
        # Pin the rev explicitly: on a dirty tree, a bare git+file:. resolves to
        # a copy of the working tree, which makes old and new identical.
        old_sha = run(["git", "rev-parse", args.old_rev]).stdout.strip()
        old_src = resolve(f"git+file:.?rev={old_sha}")
        old_plans = plan_all(old_src, args.host)
        old_names = {host: {pname(drv_name(p)) for p in plan["misses"]} for host, plan in old_plans.items()}
        drift = overlay_drift(old_src, new_src, args.overlay_target)

    result = {}
    new_misses: dict[str, list[str]] = {}
    for host, plan in new_plans.items():
        misses = []
        for path in plan["misses"]:
            entry = {"name": drv_name(path), "drv": path}
            if not args.no_base:
                entry["new"] = pname(entry["name"]) not in old_names.get(host, set())
                if entry["new"]:
                    new_misses.setdefault(host, []).append(entry["name"])
            misses.append(entry)
        result[host] = {
            "system": plan["system"],
            "system_drv": plan["system_drv"],
            "cache_misses": misses,
            "downloads": plan["downloads"],
            "always_local": plan["always_local"],
            "fetched": plan["fetched"],
        }

    json.dump({"hosts": result, "overlay_drift": drift}, sys.stdout, indent=2)
    print()
    if not args.no_base:
        total = sum(len(names) for names in new_misses.values())
        print(f"{total} new cache misses across {len(result)} hosts", file=sys.stderr)
        for host, names in new_misses.items():
            print(f"  {host}: {', '.join(names)}", file=sys.stderr)
        print(f"{len(drift)} overlay targets changed in nixpkgs", file=sys.stderr)
        for entry in drift:
            print(f"  {entry['attr']}: {', '.join(entry['changed'])}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except subprocess.CalledProcessError as e:
        print(f"command failed: {' '.join(e.cmd)}\n{e.stderr.strip()}", file=sys.stderr)
        sys.exit(1)
