# Working with huge / unknown directories

Reference for TB-scale filesystems and million-file trees. Read SKILL.md's
"Scale safety" section first.

## Preflight: estimate scale

Always run before any recursive command:

```bash
df -h <dir>                                          # filesystem capacity
fd --max-depth 1 -t f . <dir> | wc -l                # top-level file count
fd --max-depth 1 -t d . <dir> | wc -l                # top-level dir count
dust -d 1 -n 10 <dir>                                # bytes per top-level entry
fd --max-depth 2 -t f . <dir> | wc -l                # depth-2 fanout
```

The depth-2 fanout is the slowest probe; pass `timeout: 30000` to the
Bash tool for any deeper-than-default walk (depth-2 or below) when the
tree shape is unknown.

Rough thresholds:

| Top-level files (depth 1) | Strategy |
| --- | --- |
| < 100k | Run anything, but still pass `--max-depth` defensively |
| 100k - 1M | Always cap; size floor on dedupe (`-s 1M`) |
| 1M - 10M | Segment by top-level subfolder; size floor mandatory |
| > 10M files or > 10TB | Ask the user before launching a multi-hour job |

A million-file fanout or > 1 TB capacity means segment by top-level
subfolder rather than running dedupe / similarity / optimization across
the whole tree. See ["Segmenting a TB tree"](#segmenting-a-tb-tree)
below for the loop pattern.

## Per-tool limit flags

Worked examples for `fd`, `eza`, `dust`, `jq`, and moreutils live in
[inspect.md](inspect.md).

| Tool | Depth flag | Count / size flag | Notes |
| --- | --- | --- | --- |
| `fd` | `--max-depth N` | `--max-results N`, `--size +1M` | `--max-results` stops dispatching new entries (preferred over `\| head`); parallel walkers may run briefly |
| `eza -lT` | `--level=N` | -- | depth is mandatory at scale |
| `dust` | `-d N` | `-n N` (top-N), `-z 1M` (min size, takes a string) | `-z` wants `1M`, not a bare integer |
| `fclones group` | `--depth N` | `-s 1M` (size floor; **accepts suffixes**) | size floor often dominates; hashing 4KB files is wasted work |
| `czkawka_cli image` / `dup` | `-R` (top-level only) | `-m <bytes>` / `-i <bytes>` (**raw bytes only**) | no `--depth`. Asymmetry vs `fclones`: `fclones -s 1M` works, `czkawka_cli -m 1M` does not -- use `-m 1048576` |
| `czkawka_cli big` | -- | `-n N`, `-m`/`-i` (raw bytes) | already a flat top-N report |
| `find` | `-maxdepth N` | `-size +X` | discouraged anyway; use `fd` |
| `du` | `-d N` | -- | prefer `dust -d N` |

## Bounding long commands

In Claude Code, the Bash tool already enforces a timeout: 120000 ms
(2 min) default, tunable up to 600000 ms (10 min) via the `timeout`
parameter. For jobs that may run longer than 10 min, set
`run_in_background: true` and use the `Read` tool to fetch captured
stdout. The session stays unblocked and you're notified on completion.

The shell's `timeout` utility is redundant under this harness; do not
wrap commands with it.

**Non-Claude-Code fallback.** If this skill is loaded by a harness
without the Bash-tool `timeout`/`run_in_background` parameters, fall
back to the shell `timeout` utility:

```bash
timeout 30 <cmd>                       # 30 seconds
timeout 5m <cmd>                       # 5 minutes; units s/m/h/d
timeout --kill-after=10s 30 <cmd>      # SIGTERM at 30s, SIGKILL 10s later
```

Exit codes: `124` = SIGTERM honored within the window; `137` = SIGKILL
(either via `--kill-after` or external).

## Streaming and sampling patterns

```bash
fd -0 -t f . src/ | head -zn 1000 | xargs -0 cmd       # NUL-delimited sample
fd --max-results 100 -t f . src/                       # bounded walk, fast
fd -t f . src/ | head -n 100                           # newline-delimited (avoid at scale)
```

`fd --max-results N` is the right tool when you want N results from a
huge tree -- it stops dispatching new directory entries to walkers,
unlike `| head` which keeps `fd` running until SIGPIPE.

## Segmenting a TB tree

Run dedupe and similarity per top-level subfolder, not over the whole
root. Especially important for `czkawka_cli image`/`dup` (no depth flag
-- the only way to bound them is the input directory).

```bash
for sub in src/*/; do
  fclones group -s 1M "$sub" > "reports/$(basename "$sub").txt"
done

# Then merge / inspect reports/*.txt by hand.
```

For `czkawka_cli`:

```bash
for sub in src/*/; do
  czkawka_cli image -d "$sub" -m 1048576 -f "reports/$(basename "$sub").txt"
done
```

## Memory-heavy tools

`fclones`, `czkawka_cli image`, and `magick mogrify` over millions of
files build in-memory tables that scale with file count. Mitigations:

- Size floor: `fclones -s 1M`, `czkawka_cli -m 1048576`. Filters out the
  long tail of small files that dominate count but rarely matter.
- Chunked batching: `fd --max-depth N -X cmd` runs `cmd` once per
  chunk of matches rather than holding all paths in memory.
- Segment by subfolder (above) instead of running over the whole tree.

## Monitoring progress

Long ops on a TB tree are blind by default. Pick the wrapper that fits
the shape of the work:

- **Tool built-ins, no wrapper needed:**
  - `fclones group` -- progress bar + ETA on stderr (suppress with
    `--no-progress`).
  - `czkawka_cli` -- per-stage progress on stderr.
  - `7zz` -- per-file progress to stdout.
  - `magick mogrify -monitor` -- per-file progress.
  - `tar --checkpoint=1000 --checkpoint-action=dot` -- one dot per
    1000 records.
  - `rsync --info=progress2` -- file-by-file progress + total ETA.

- **Pipe throughput** -- `pv` shows bytes/sec, ETA, and percent on a
  stream:

  ```bash
  tar -cf - src/ | pv -s $(du -sb src/ | cut -f1) > src.tar
  pv huge.zst | zstd -d > huge
  fd -e jpg . src/ | pv -l > /tmp/jpgs.list                 # -l counts newlines
  fd -0 -e jpg . src/ | tr '\0' '\n' | pv -l > /tmp/jpgs.list  # NUL-safe variant
  ```

- **Peek at a running cp / mv / dd / tar** -- `progress` attaches to an
  already-running coreutils process. Start the cp/mv first, then run
  `progress` in another terminal -- with no matching process it exits
  immediately:

  ```bash
  progress -mw                         # follow all known progs, throughput + ETA
  progress -p $(pgrep -nf rsync)       # specific PID
  ```

- **Watch the destination grow** -- `viddy` re-runs the command every
  refresh (default 2s), so the inner command must be cheap and bounded:

  ```bash
  viddy 'dust -d 1 dst/'                              # cheap, bounded
  viddy 'du -sh dst/'                                 # total size, single number
  viddy 'fd --max-depth 4 -t f . dst/ | wc -l'        # file count, depth-capped
  ```

- **Timestamp output** -- pipe noisy stderr through `ts` (moreutils) so
  "is it stuck?" becomes a glance:

  ```bash
  fclones group -s 1M src/ 2>&1 | ts '[%H:%M:%S]'
  ```

- **Async + log file** -- set `run_in_background: true` on the Bash tool
  call and use the `Read` tool to fetch the captured output. Pair with
  `ts` in the wrapped command for time-stamped lines.

## When to refuse / escalate

If preflight shows > 10M files or > 10TB and the user has not given a
concrete strategy, stop. Report the scale numbers and ask. A multi-hour
dedupe started silently is a worse outcome than the question.

## When the user insists on running across the full tree

Pick one pattern based on expected runtime:

- **Synchronous (under 10 min)** -- set the Bash tool's `timeout`
  parameter (max 600000 ms = 10 min). Session blocks until done. Set the
  value to ~10x the probe estimate.

  ```
  # Bash tool call: timeout: 600000
  fclones group -s 1M src/ > /tmp/dups.txt 2>&1
  ```

- **Asynchronous (any duration)** -- set `run_in_background: true`.
  Stdout is captured; fetch it with the `Read` tool. The session stays
  unblocked and you're notified on completion. Required for jobs that
  may exceed 10 min. Keep `2>&1` on the command so progress messages on
  stderr land in the captured stream.

  ```
  # Bash tool call: run_in_background: true
  fclones group -s 1M src/ 2>&1
  ```
