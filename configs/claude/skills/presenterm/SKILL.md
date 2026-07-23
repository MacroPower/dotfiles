---
name: presenterm
description: >-
  Reference and guidance for writing presenterm presentations (terminal
  slideshows written in markdown) and using the `presenterm` CLI
  (https://mfontanini.github.io/presenterm/). Trigger when the user creates or
  edits a presenterm presentation, asks about slides in the terminal, uses
  presenterm comment commands like `<!-- end_slide -->` or `<!-- pause -->`,
  configures presenterm themes or config, or exports a presentation to
  PDF/HTML. SKIP for PowerPoint/Keynote/Google Slides, reveal.js and other
  browser-based slide tools, and generic markdown documents that aren't
  presentations.
---

# presenterm Reference

[presenterm](https://github.com/mfontanini/presenterm) renders slideshows in the terminal from a single markdown file. Slides are separated by `<!-- end_slide -->` comments, and HTML-comment "commands" control pauses, layouts, and styling that plain markdown can't express. This skill is a snapshot of the official docs (mdbook at <https://mfontanini.github.io/presenterm/>) as of v0.16.1.

## Quick Reference

| Topic | Reference |
| --- | --- |
| Writing presentations (front matter, slides, comment commands, column layouts, images) | [presentations.md](references/presentations.md) |
| Code snippets (highlighting, `+exec` and friends, mermaid/latex/typst/d2 rendering) | [code.md](references/code.md) |
| Themes (built-ins, front matter selection, full theme YAML schema) | [themes.md](references/themes.md) |
| Configuration (config file, `options:`, settings, key bindings, custom executors) | [configuration.md](references/configuration.md) |
| Presenting (key bindings, hot reload, PDF/HTML export, transitions, speaker notes) | [presenting.md](references/presenting.md) |

Open the matching reference file for full detail. Summaries below are for quick lookup.

## Presentation Basics

A minimal presentation:

```markdown
---
title: My presentation
sub_title: With a subtitle
author: Me
---

First slide
===

Some **markdown** content.

<!-- pause -->

More content, revealed on the next keypress.

<!-- end_slide -->

Second slide
===

* bullet
* points
```

Key facts:

- The front matter is optional; setting `title`/`sub_title`/`author` (or `authors:` for a list) generates an introduction slide.
- Setext headers (`Title\n===`) are slide titles, rendered centered and styled. Regular `#` headings are just headings.
- Slides end at `<!-- end_slide -->`, not at `---`. The `implicit_slide_ends` and `end_slide_shorthand` options relax this.
- `presenterm file.md` hot-reloads on save and jumps to the changed slide; `presenterm -p file.md` is presentation mode without reload.
- Images use normal markdown tags, resolve relative to the presentation file, and render natively on kitty/iterm2/sixel terminals. Size with `![image:width:50%](img.png)`.

## Comment Command Cheatsheet

Full detail in [presentations.md](references/presentations.md). `presenterm --list-comment-commands` prints this list.

```markdown
<!-- end_slide -->              end the current slide
<!-- pause -->                  reveal the rest of the slide on next keypress
<!-- new_line -->               explicit vertical spacing (markdown collapses blank lines)
<!-- new_lines: 2 -->
<!-- jump_to_middle -->         vertically center what follows (separator slides)
<!-- incremental_lists: true -->   bullets appear one at a time until slide end
<!-- list_item_newlines: 2 -->  spacing between list items
<!-- alignment: center -->      left|center|right for the rest of the slide
<!-- font_size: 2 -->           1-7, kitty terminal only
<!-- column_layout: [2, 1] -->  define proportional columns
<!-- column: 0 -->              start writing into column 0
<!-- reset_layout -->           back to full width
<!-- no_footer -->              hide the footer on this slide
<!-- skip_slide -->             exclude this slide
<!-- include: other.md -->      inline an external markdown file
<!-- speaker_note: text -->     only visible to --listen-speaker-notes instances
<!-- snippet_output: id -->     show output of the +exec snippet tagged +id:id
<!-- // free-form note -->      user comment, never rendered
```

Single-line HTML comments must be valid commands (or set the `command_prefix` option); multi-line comments are ignored as plain comments.

## CLI Cheatsheet

```bash
presenterm file.md               # develop: hot reload on save
presenterm -p file.md            # present: no reload
presenterm -t <theme> file.md    # pick a theme
presenterm --list-themes         # demo all built-in themes
presenterm --current-theme       # print active theme name
presenterm -x file.md            # allow +exec snippets (ctrl+e to run)
presenterm -X file.md            # also allow +exec_replace / +image auto-execution
presenterm --validate-snippets   # run all executable snippets, fail on errors
presenterm --validate-overflows  # error if any slide overflows the screen
presenterm -e file.md            # export PDF (needs weasyprint); -o sets output path
presenterm -E file.md            # export self-contained HTML (no dependencies)
presenterm --publish-speaker-notes file.md   # main instance
presenterm --listen-speaker-notes file.md    # notes-only follower instance
presenterm --image-protocol <p>  # override image protocol detection
presenterm --config-file <path>  # explicit config (also $PRESENTERM_CONFIG_FILE)
presenterm --list-comment-commands
```

Navigation while running: arrows/hjkl/page keys to move, `n`/`p` to skip pauses, `gg`/`G`/`<N>G` to jump, `ctrl+p` for the slide index, `?` for key bindings, `T` to toggle the column grid, `q` to quit.

## Common Patterns

### Two-column slide (text + code/image)

```markdown
<!-- column_layout: [2, 1] -->

<!-- column: 0 -->
Explanation text here.

<!-- column: 1 -->
![](diagram.png)

<!-- reset_layout -->
Full-width text below both columns.
```

A `[1, 3, 1]` layout with content only in column 1 centers content in the middle 60% of the screen.

### Section separator slide

```markdown
<!-- jump_to_middle -->

Part two
===
```

### Code snippets

Flags go after the language: ```` ```rust +line_numbers {1,3|5-7} ````.

- `{1,3,5-7}` highlights only those lines; `|` groups advance per keypress (dynamic highlighting).
- `+exec` makes a block runnable with `ctrl+e` (requires `-x`). `+exec_replace` runs automatically and shows only the output (requires `-X`).
- Hidden lines (executed but not shown): prefix `# ` in rust, `/// ` in python/bash/go/etc.
- ```` ```file ```` blocks include external source files (`path:`, `language:`, optional `start_line`/`end_line`).
- `mermaid`/`latex`/`typst`/`d2` blocks with `+render` become images; size with `+width:50%`. mermaid needs `mmdc`, latex/typst need `typst` (+ `pandoc` for latex), d2 needs `d2`.

Full detail in [code.md](references/code.md).

### Theme selection in front matter

```yaml
---
theme:
  name: catppuccin-mocha    # or light:/dark: pair, or path:, or override:
---
```

`theme.override` restyles a single presentation inline without a separate theme file; overrides hot-reload on save. Custom themes are YAML files in `<config-dir>/themes/` and can `extends:` a built-in. Full schema in [themes.md](references/themes.md).

### Per-presentation options

Options go in the front matter (or globally in `config.yaml`):

```yaml
---
options:
  implicit_slide_ends: true     # slide titles end the previous slide
  incremental_lists: true       # all lists reveal item by item
  auto_render_languages: [mermaid]
---
```

Everything else (default theme, key bindings, snippet executors, export dimensions, transitions) lives in the config file only: see [configuration.md](references/configuration.md).
