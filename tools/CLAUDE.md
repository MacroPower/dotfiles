# Tools

This file provides guidance to Claude Code (claude.ai/code) when working with tools in this repository.

## Code Style

### Go Conventions

- Document all exported items with doc comments.
- Package documentation in `doc.go` files.
- Wrap errors with `fmt.Errorf("context: %w", err)`, or `fmt.Errorf("%w: %w", ErrSentinel, err)`.
- Avoid using "failed" or "error" in library error messages.
- Use global error variables for common errors.
- Use constructors with functional options.
- Accept interfaces, return concrete types.
- Prefer consistency over performance, avoid "fast paths" that could lead to unpredictable behavior.

### Documentation

- Use `[Name]` syntax for Go doc links. Use `[*Name]` for pointer types.
- Functional option types should have a list linking to all functions of that type.
- Functional options should always have a link to their type.
- Docs should explain concepts and usage patterns. **Do not enumerate exports**.

### Testing

- Use `github.com/stretchr/testify/assert` and `require`.
- Table-driven tests with `map[string]struct{}` format.
- Field names: prefer `want` for expected output, `err` for expected errors.
- For inputs, use clear contextual names, such as `before` and `after` for diffs, or `line` and `col` for positions.
- Always use `t.Parallel()` in all tests.
- Use `require.ErrorIs` for sentinel error checking.
- Use `require.ErrorAs` for error type extraction.

## mcp-git Tests

Keep the count of git processes low when you add a test to `mcp-git`. Process starts take most of the run time of the suite. When every test case built its repositories with git, the suite ran for more than ten minutes on a Mac that scans each new process. The Nix build of the package then failed.

- Take each repository from a helper of `mcp-git/fixture_test.go`, such as `initRepos`, `initFeatureRepos`, or `initSubmoduleRepos`. Each helper copies a template that the test binary builds one time. When no helper fits, add a template to that file, and add its helper to `TestFixturesStartNoGit`.
- Write the git config of a repository with `setConfig`, `appendGitConfig`, `setOriginURL`, and `unsetOriginURL`. Do not run `git config` in the setup of a test.
- Give the remote of a clone a later commit with `pushRemoteCommit`. Do not commit and push from a second clone.
- Each call of a tool starts several git processes. `TestRemoteToolsGitCommands` lists them for `git_fetch`, `git_pull`, `git_push`, and a `git_clone` call that pulls. Cover each value of an input with a test of the function that reads the value. When one call of a tool can show several values, such as every config key that the tool refuses, set the values in one case.

To count the git processes of one run, put a `git` script that records each start first on `PATH`:

```sh
dir=$(mktemp -d)
mkdir -p "$dir/shim"
printf '#!/bin/sh\necho x >> "%s"\nexec %s "$@"\n' "$dir/count" "$(command -v git)" > "$dir/shim/git"
chmod +x "$dir/shim/git"
cd tools/mcp-git
: > "$dir/count"
PATH="$dir/shim:$PATH" go test -count=1 ./...
wc -l < "$dir/count"
```

Some tests start git with an environment of their own. For example, `gitBackend` in `mcp-git/clone_test.go` returns a `cgi.Handler` that starts `git http-backend` with its own `Env`. A variable that the run sets does not reach those git processes, so the `printf` writes the path of the count file into the script.

The count was about 2,700 at the commit that added this section. It was about 2,300 at the commit that made `runRemote` read every path of a repository with one git command.

## mcp-destiny Game Data

Destiny 2 is frozen, so the game data that `mcp-destiny` vendors does not go stale. Bungie shipped its final major patch, Update 9.7.0, on 2026-06-09. Bungie has shipped only maintenance hotfixes since that patch, and the last hotfix is Update 9.7.0.6 of 2026-10-20. Game data such as perks, weapon frames, armor archetypes, and stat formulas stays as that hotfix left it.

Bungie has announced plans for later Destiny 2 updates but has not set a release date for any of them. On 2026-09-17, Bungie announced work to bring vaulted campaigns, destinations, and raids back into the game. On 2026-10-29, Bungie said that the work stays in pre-production until about February 2027 and then needs about 12 months of production. Bungie also announced a Destiny 2 "Celebration" that brings new updates, and it plans to give more news about the Celebration in February 2027.

These rules cover game data from a source other than the Bungie API and Agg's Destiny Vault Manager, such as the weapon curves of D2Foundry's Oracle Engine.

- Do not count future patches as a cost when you choose a source of game data. Do not plan for balance changes. A source that describes an update before Update 9.7.0.6 still costs a check against the patch notes after that update. It also loses each value that those notes change without giving the new value. When two sources otherwise match, prefer the source that describes the later update.
- Prefer a vendored snapshot that the binary embeds over a download at run time.
- Check the snapshot against the patch notes of every update after the update that its source describes, up to and including Update 9.7.0.6. A community source can describe an earlier update, as Oracle Engine describes the game at Update 8.2.6.
- Record the source of each value and the last update of its check in the snapshot, as `mcp-destiny/curves/curves.json` does.
- When Bungie ships a later update, such as a hotfix, an update that restores vaulted content, or a Celebration update, check its patch notes against each snapshot. Weapons that a returned raid brings back might carry new item hashes and plug hashes, so also check whether each snapshot lists those hashes. Then name that update as the last update in each snapshot, in this section, in `mcp-destiny/doc.go`, `mcp-destiny/curves/doc.go`, and `mcp-destiny/damage/doc.go`, and in the `lastUpdate` constants of `mcp-destiny/curves/curves_internal_test.go` and `mcp-destiny/damage/damage_internal_test.go`. Also name it in the doc comment of `RaidSpoils` in `mcp-destiny/inventory/patterns.go`.
