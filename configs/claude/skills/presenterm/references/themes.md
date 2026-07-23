# Themes

presenterm ships with built-in themes and supports fully custom ones defined in YAML.

## Setting themes

- CLI: `presenterm --theme <name>`.
- Config file default: `defaults.theme` (see [configuration.md](configuration.md)).
- Presentation front matter, in 3 flavors:

By name (overrides the default and `--theme`):

```yaml
---
theme:
  name: dark
---
```

Separate light/dark variants (presenterm detects the terminal's color scheme on launch):

```yaml
---
theme:
  light: light
  dark: dark
---
```

By path to a theme file:

```yaml
---
theme:
  path: /home/me/Documents/epic-theme.yaml
---
```

Overrides, partially or completely overriding the active theme inline (reloaded on save, great for iterating):

```yaml
---
theme:
  override:
    default:
      colors:
        foreground: "beeeff"
---
```

## Built-in themes

- `catppuccin-latte`, `catppuccin-frappe`, `catppuccin-macchiato`, `catppuccin-mocha`
- `dark`, `light`
- `gruvbox-dark`
- `terminal-dark`, `terminal-light`: use the terminal's own colors, so a transparent/imaged terminal background carries over. Pick the variant matching the terminal's color scheme.
- `tokyonight-moon`, `tokyonight-day`, `tokyonight-night`, `tokyonight-storm`

`presenterm --list-themes` renders sample content in every theme. `presenterm --current-theme` prints the theme in use.

## Loading custom themes

On startup presenterm loads every `.yaml` file in the `themes` directory under the config directory (e.g. `~/.config/presenterm/themes` on Linux) and makes it available like a built-in: usable via `--theme`, `theme.name`, etc.

# Theme Definition

Theme files are YAML. Root attributes either style a specific element type (slide title, headings, footer, ...) or set defaults. See the [built-in themes](https://github.com/mfontanini/presenterm/tree/master/themes) for complete examples.

## Extending themes

A custom theme can extend a built-in or custom theme and override only what it wants:

```yaml
extends: dark
default:
  colors:
    background: "000000"
```

## Alignment

Elements supporting alignment: code blocks, slide titles, the intro slide's title/subtitle/author, and tables.

Left/right alignment takes a `margin` (columns between the text and the screen border), either fixed or percent. Percent tends to survive terminal resizes better:

```yaml
alignment: left
margin:
  percent: 8   # or fixed: 5
```

Center alignment takes:

- `minimum_size`: minimum element width. Useful for code blocks so the background extends past the code.
- `minimum_margin`: minimum margin, same structure as `margin`.

## Colors

Every element can set foreground/background colors in hex:

```yaml
default:
  colors:
    foreground: ff0000
    background: 00ff00
```

## Default style

```yaml
default:
  margin:
    percent: 8
  colors:
    foreground: "e6e6e6"
    background: "040312"
```

## Intro slide

```yaml
intro_slide:
  title:
    alignment: left
    margin:
      percent: 8
  author:
    colors:
      foreground: black
    positioning: below_title   # or page_bottom
```

## Footer

Three styles: `template`, `progress_bar`, `empty`.

### Template footers

Text (arbitrary markdown, including `span` tags) on the left/center/right. Templates can reference `{current_slide}` and `{total_slides}` plus any front matter attribute: `{title}`, `{sub_title}`, `{event}`, `{location}`, `{date}`, `{author}`.

```yaml
footer:
  style: template
  left: "My **name** is {author}"
  center: "_@myhandle_"
  right: "{current_slide} / {total_slides}"
  height: 3
```

- Referencing an unset front matter attribute or an unsupported variable is an error. Escape literal braces by doubling: `{{potato}}` renders as `{potato}`.
- `height` is in terminal rows (default 2); text is vertically centered within it.

Each position can be an image instead of text:

```yaml
footer:
  style: template
  left:
    image: potato.png
  height: 5
```

Footer images are looked up relative to the presentation first, then relative to the themes directory. They preserve aspect ratio and expand vertically to `footer.height` rows.

### Progress bar footers

```yaml
footer:
  style: progress_bar
  character: X   # optional, defaults to a block character
```

### No footer

```yaml
footer:
  style: empty
```

## Slide title

```yaml
slide_title:
  prefix: "XX"          # prefix before the title text
  font_size: 2          # kitty-only font size
  padding_top: 1
  padding_bottom: 1
  separator: true       # horizontal line after the title
  bold: true
  underlined: true
  italics: true
  colors:
    foreground: beeeff
    background: feeedd
```

## Headings

Each of h1-h6 can be styled independently:

```yaml
headings:
  h1:
    prefix: "XX"
    colors:
      foreground: beeeff
    bold: true
    underlined: true
    italics: true
  h2:
    prefix: "XXX"
    colors:
      foreground: feeedd
```

## Code blocks

Syntax highlighting uses [syntect](https://github.com/trishume/syntect). Available highlighting themes:

base16-ocean.dark, base16-eighties.dark, base16-mocha.dark, base16-ocean.light, Catppuccin, Coldark, DarkNeon, InspiredGitHub, Nord-sublime, Solarized, Solarized (dark), Solarized (light), TwoDark, dracula-sublime, github-sublime-theme, gruvbox, onehalf, sublime-monokai-extended, sublime-snazzy, visual-studio-dark-plus, zenburn.

```yaml
code:
  theme_name: base16-eighties.dark
  padding:
    horizontal: 2
    vertical: 1
  background: false     # whether to draw the theme's background behind code
  line_numbers: false   # line numbers in all snippets by default
```

Custom `.tmTheme` highlighting themes dropped in `themes/highlighting` under the config directory are loaded automatically.

## Block quotes

```yaml
block_quote:
  prefix: "| "
```

## Mermaid and d2

```yaml
mermaid:
  background: transparent
  theme: dark
d2:
  theme: 1
```

## Alerts

GitHub-style markdown alerts (`> [!note]` etc):

```yaml
alert:
  base_colors:
    foreground: red
    background: black
  prefix: "| "
  styles:
    note:
      color: blue
      title: Note
      icon: I
    tip: { color: green, title: Tip, icon: T }
    important: { color: cyan, title: Important, icon: I }
    warning: { color: orange, title: Warning, icon: W }
    caution: { color: red, title: Caution, icon: C }
```

## Color palette

A palette defines named colors and named foreground/background pairs ("classes"):

```yaml
palette:
  colors:
    red: "f78ca2"
    purple: "986ee2"
  classes:
    foo:
      foreground: "ff0000"
      background: "00ff00"
```

Palette colors are referenced as `palette:<name>` or `p:<name>` anywhere a color is required, inside the theme itself and in presentation `span` tags:

```html
<span style="color: palette:red">this is red</span>
<span class="foo">this is foo-colored</span>
```

## Bold/italics styling

Bold and italics text has no colors by default; the top-level `bold` and `italics` keys assign some:

```yaml
bold:
  colors:
    foreground: red
italics:
  colors:
    background: blue
```

## typst rendering style

```yaml
typst:
  colors:
    background: ff0000
    foreground: 00ff00
  horizontal_margin: 2   # in points
  vertical_margin: 2
```
