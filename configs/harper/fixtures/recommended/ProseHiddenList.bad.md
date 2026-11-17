First, the hook reads the file. Then it runs the linter. Finally, it prints the findings.

The `--force` flag skips the check. The `--dry-run` flag prints the plan. The `--quiet` flag hides the summary.

First, the build copies the sources. Next, it compiles each package.

The loader reads `a.nix`. The loader reads `b.nix`. The loader reads `c.nix`.

The loader reads `a.nix`. `b.nix`. The loader reads `c.nix`. The loader reads `d.nix`.

The loader reads https://example.com/a. The loader reads https://example.com/b. The loader reads https://example.com/c.

The guide for the loader lives at
https://example.com/guide: read it first. The loader reads `a.nix`. The loader reads `b.nix`. The loader reads `c.nix`. <!-- same finding -->

The loader reads the file that the guide at
https://example.com/guide <!-- same finding -->
names. The loader reads `b.nix`. The loader reads `c.nix`. <!-- same finding -->

The proxy says “retry now.” The proxy says ‘wait now.’ The proxy says “stop now.”

The loader reads "`a.nix`." The loader reads "`b.nix`." The loader reads "`c.nix`."

"Save changes?" appears when you close the editor. "Save changes?" shows when you quit the app. "Save changes?" pops up when the session ends.

“Ready?” the wizard asks before the build. “Ready?” the prompt asks after the tests. “Ready?” the dialog asks at the end.

"A lease?" the guide asks before the build. "A lease?" the guide asks after the tests. "A lease?" the guide asks at the end.

“An entry?” the guide asks before the build. “An entry?” the guide asks after the tests. “An entry?” the guide asks at the end.

The loader reads `a.nix`. The loader reads `b.nix`.
####### The loader reads `c.nix`. <!-- same finding -->
