First, the hook reads the file. Then it runs the linter. Finally, it prints the findings.

The `--force` flag skips the check. The `--dry-run` flag prints the plan. The `--quiet` flag hides the summary.

First, the build copies the sources. Next, it compiles each package.

The loader reads `a.nix`. The loader reads `b.nix`. The loader reads `c.nix`.

The loader reads `a.nix`. `b.nix`. The loader reads `c.nix`. The loader reads `d.nix`.
