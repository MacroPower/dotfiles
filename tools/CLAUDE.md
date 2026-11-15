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
- Docs should explain concepts and usage patterns; **do not enumerate exports**.

### Testing

- Use `github.com/stretchr/testify/assert` and `require`.
- Table-driven tests with `map[string]struct{}` format.
- Field names: prefer `want` for expected output, `err` for expected errors.
- For inputs, use clear contextual names (e.g., `before`/`after` for diffs, `line`/`col` for positions).
- Always use `t.Parallel()` in all tests.
- Use `require.ErrorIs` for sentinel error checking.
- Use `require.ErrorAs` for error type extraction.

## mcp-destiny Game Data

Destiny 2 is frozen, so the game data that `mcp-destiny` vendors does not go stale. Bungie shipped its final major patch, Update 9.7.0, on 2026-06-09. Bungie has shipped only maintenance hotfixes since that patch, and the last hotfix is Update 9.7.0.6 of 2026-10-20. Game data such as perks, weapon frames, armor archetypes, and stat formulas stays as that hotfix left it.

Bungie has announced plans for later Destiny 2 updates but has not set a release date for any of them. On 2026-09-17, Bungie announced work to bring vaulted campaigns, destinations, and raids back into the game. On 2026-10-29, Bungie said that the work stays in pre-production until about February 2027 and then needs about 12 months of production. Bungie also announced a Destiny 2 "Celebration" that brings new updates, and it plans to give more news about the Celebration in February 2027.

These rules cover game data from a source other than the Bungie API and Agg's Destiny Vault Manager, such as the weapon curves of D2Foundry's Oracle Engine.

- Do not count future patches as a cost when you choose a source of game data. Do not plan for balance changes. A source that describes an update before Update 9.7.0.6 still costs a check against the patch notes after that update. It also loses each value that those notes change without giving the new value. When two sources otherwise match, prefer the source that describes the later update.
- Prefer a vendored snapshot that the binary embeds over a download at run time.
- Check the snapshot against the patch notes of every update after the update that its source describes, up to and including Update 9.7.0.6. A community source can describe an earlier update, as Oracle Engine describes the game at Update 8.2.6.
- Record the source of each value and the last update of its check in the snapshot, as `mcp-destiny/curves/curves.json` does.
- When Bungie ships a later update, such as a hotfix, an update that restores vaulted content, or a Celebration update, check its patch notes against each snapshot. Weapons that a returned raid brings back might carry new item hashes and plug hashes, so also check whether each snapshot lists those hashes. Then name that update as the last update in each snapshot, in this section, in `mcp-destiny/doc.go` and `mcp-destiny/curves/doc.go`, and in the `lastUpdate` constant of `mcp-destiny/curves/curves_internal_test.go`.
