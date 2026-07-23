# Configuration

presenterm reads a `config.yaml` from its configuration directory:

- `$XDG_CONFIG_HOME/presenterm/` if set, otherwise
- `~/.config/presenterm/` on Linux
- `~/Library/Application Support/presenterm/` on macOS
- `~/AppData/Roaming/presenterm/config/` on Windows

A custom path can be given via `--config-file` or the `PRESENTERM_CONFIG_FILE` environment variable. A [sample config](https://github.com/mfontanini/presenterm/blob/master/config.sample.yaml) exists in the repo. Custom themes live in `themes/` next to the config file.

A JSON schema is available for YAML language servers; add this line at the top of the config file:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/mfontanini/presenterm/master/config-file-schema.json
```

# Options

Options can be set in the config file under `options:` OR in a presentation's front matter under the same key (per-presentation, travels with the file).

## implicit_slide_ends

A slide title implies the previous slide ended, removing the need for `<!-- end_slide -->` between slides:

```markdown
---
options:
  implicit_slide_ends: true
---

Tasty vegetables
================

* Potato

Awful vegetables
================

* Lettuce
```

## end_slide_shorthand

Thematic breaks (`---`) also act as slide terminators. `<!-- end_slide -->` still works.

## h1_slide_titles

The first `h1` heading in a slide becomes the slide title (instead of requiring setext headers).

## command_prefix

Single-line HTML comments are assumed to be commands, so `<!-- remember to say "potato" here -->` is an error by default (multi-line comments are always plain comments). Setting a prefix makes only prefixed comments commands:

```markdown
---
options:
  command_prefix: "cmd:"
---

<!-- remember to say "potato here" -->

<!-- cmd:pause -->
```

## incremental_lists

All bullet points in all lists show up with pauses in between. For specific lists only, use the `incremental_lists` comment command instead.

## strict_front_matter_parsing

Set to `false` to tolerate unknown keys in a presentation's front matter (useful for presentations written for other tools).

## image_attributes_prefix

Changes the image size attribute prefix (default `image:`). Set to `""` to write `![width:50%](path.png)`.

## auto_render_languages

Languages for which `+render` is implicit:

```yaml
options:
  auto_render_languages:
    - mermaid
```

## list_item_newlines

Number of newlines between list items (default 1). Also settable per-slide via the comment command.

# Settings

Everything below can **only** be set in the config file, not in front matter.

## Defaults

```yaml
defaults:
  # default theme; a plain string, or per terminal color scheme:
  theme:
    light: light
    dark: dark

  # only needed on Windows (or if images render at the wrong size):
  # used to compute the window size for image scaling
  terminal_font_size: 16

  # force an image protocol if auto-detection fails:
  # auto | kitty-local | kitty-remote | iterm2 | sixel
  image_protocol: kitty-local

  # cap presentation width/height; content is aligned within larger terminals
  max_columns: 100
  max_columns_alignment: center   # left | center | right
  max_rows: 100
  max_rows_alignment: center      # top | center | bottom

  # whether incremental lists pause before/after the list itself (both default true)
  incremental_lists:
    pause_before: true
    pause_after: true

  # check that no slide overflows the terminal:
  # never (default) | always | when_presenting | when_developing
  validate_overflows: never
```

## Slide transitions

Animations when moving between slides:

```yaml
transition:
  duration_millis: 750
  frames: 45
  animation:
    style: fade   # fade | slide_horizontal | collapse_horizontal
```

`fade` fades into the next slide; `slide_horizontal` slides horizontally; `collapse_horizontal` collapses into the center.

## Key bindings

Overrides under the `bindings` key. These are the defaults; an override replaces the whole list for that action:

```yaml
bindings:
  next: ["l", "j", "<right>", "<page_down>", "<down>", " "]
  previous: ["h", "k", "<left>", "<page_up>", "<up>"]
  # "fast" jumps skip pauses, dynamic code highlights, and transitions
  next_fast: ["n"]
  previous_fast: ["p"]
  first_slide: ["gg"]
  last_slide: ["G"]
  go_to_slide: ["<number>G"]
  execute_code: ["<c-e>"]
  reload: ["<c-r>"]
  toggle_slide_index: ["<c-p>"]
  toggle_bindings: ["?"]
  close_modal: ["<esc>"]
  exit: ["<c-c>", "q"]
  suspend: ["<c-z>"]
```

## Snippet execution

Disabled by default for security. Enabling globally (instead of passing `-x` / `-X` per run):

```yaml
snippet:
  exec:
    enable: true          # +exec snippets
  exec_replace:
    enable: true          # +exec_replace / +image snippets; runs code with NO user intervention
```

Use at your own risk, especially with presentations you didn't write.

## Custom snippet executors

Add or override how a language's snippets are compiled/run:

```yaml
snippet:
  exec:
    custom:
      c++:                            # the code block language identifier
        filename: "snippet.cpp"       # file the snippet is written to
        environment:
          MY_FAVORITE_ENVIRONMENT_VAR: foo
        hidden_line_prefix: "/// "    # lines starting with this run but aren't shown
        commands:                     # run in order, in the snippet's directory
          - ["g++", "-std=c++20", "snippet.cpp", "-o", "snippet"]
          - ["./snippet"]
```

All command output is included in the execution output, so mute compiler output if needed. Built-in executors are defined in [executors.yaml](https://github.com/mfontanini/presenterm/blob/master/executors.yaml) and can be overridden the same way.

## Snippet rendering threads

`+render` blocks (mermaid especially) render asynchronously:

```yaml
snippet:
  render:
    threads: 2
```

## Mermaid, d2, typst

```yaml
mermaid:
  scale: 2                                   # passed to the mermaid CLI
  config_file: /home/foo/my_config_file.yml  # custom mermaid config
  puppeteer_config_file: /home/foo/puppeteer.json
d2:
  scale: 2                                   # passed to d2 --scale
typst:
  ppi: 400                                   # PPI for latex/typst images, default 300
```

## Speaker notes

Always publish speaker notes so only `--listen-speaker-notes` is ever needed:

```yaml
speaker_notes:
  always_publish: true
```

## Exports

```yaml
export:
  # page size; defaults to the terminal's size at export time
  dimensions:
    columns: 80
    rows: 30

  # pauses are ignored in exports by default; this makes each pause a new page
  pauses: new_slide

  # snippets execute in parallel during export by default
  snippets: sequential

  # use specific fonts in the PDF
  pdf:
    fonts:
      normal: /usr/share/fonts/truetype/tlwg/TlwgMono.ttf
      italic: /usr/share/fonts/truetype/tlwg/TlwgMono-Oblique.ttf
      bold: /usr/share/fonts/truetype/tlwg/TlwgMono-Bold.ttf
      bold_italic: /usr/share/fonts/truetype/tlwg/TlwgMono-BoldOblique.ttf
```
