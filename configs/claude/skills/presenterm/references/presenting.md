# Running and Presenting

## Development mode vs presentation mode

By default (`presenterm file.md`) presenterm runs in development mode: it reloads the presentation every time the file is saved, detects which slide changed, and jumps to it. Passing `-p` / `--present` enables presentation mode, which disables hot reload.

Overflow validation (`--validate-overflows` or `defaults.validate_overflows`) checks that every slide fits the current terminal size on load, reload, and resize.

## Default key bindings

Press `?` at any time to see all configured bindings.

| Action | Keys |
| --- | --- |
| Next slide | `l`, `j`, right, down, page down, space |
| Previous slide | `h`, `k`, left, up, page up |
| Next/previous, skipping pauses and animations | `n` / `p` |
| First / last slide | `gg` / `G` |
| Jump to slide N | `<N>G` |
| Execute code snippet | `ctrl+e` |
| Reload presentation | `ctrl+r` |
| Slide index modal | `ctrl+p` |
| Key bindings modal | `?` |
| Toggle column layout grid | `T` |
| Close modal | `esc` |
| Exit | `ctrl+c`, `q` |
| Suspend | `ctrl+z` |

All of these can be overridden in the config file (see [configuration.md](configuration.md)).

# Exports

Presentations can be exported to PDF and HTML.

## PDF

Requires [weasyprint](https://pypi.org/project/weasyprint/) (a Python package; follow its install instructions, and activate its virtualenv if it lives in one):

```bash
presenterm --export-pdf demo.md            # writes demo.pdf
presenterm --export-pdf demo.md -o out.pdf

# one-shot with uv:
uv run --with weasyprint presenterm --export-pdf demo.md
```

## HTML

No extra dependencies; produces a single self-contained HTML file with images and styles embedded:

```bash
presenterm --export-html demo.md           # writes demo.html
```

## Export behavior

Configured under the `export` config key (see [configuration.md](configuration.md)): page dimensions (defaults to the terminal size at export time), whether pauses become separate pages, sequential vs parallel snippet execution, and PDF fonts. `--export-temporary-path` sets where temporary export files are stored.

# Slide Transitions

Transitions animate moving between slides. Enable by setting the `transition` config key:

```yaml
transition:
  duration_millis: 750
  frames: 45
  animation:
    style: fade
```

Supported styles:

- `fade`: fade the current slide into the next one.
- `slide_horizontal`: slide horizontally to the next/previous slide.
- `collapse_horizontal`: collapse the current slide into the center of the screen.

# Speaker Notes

Speaker notes are defined with the `speaker_note` comment command (see [presentations.md](presentations.md)) and displayed by a second presenterm instance:

```bash
# main instance, presents as usual and publishes slide-change events
presenterm demo.md --publish-speaker-notes

# in another shell/terminal: shows only the speaker notes,
# follows the main instance's slide changes automatically
presenterm demo.md --listen-speaker-notes
```

Setting `speaker_notes.always_publish: true` in the config file removes the need for `--publish-speaker-notes`.

Instances communicate over localhost UDP. On Linux and Windows multiple publisher/listener pairs can run at once (each keyed to its presentation file); on macOS only a single listener is supported.
