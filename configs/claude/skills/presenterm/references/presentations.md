# Writing Presentations

A presentation is a single markdown file. Slides are delimited by the `end_slide` comment command. Most common markdown elements work: ordered/unordered lists, headings, formatted text (bold, italics, strikethrough, inline code), code blocks, block quotes, tables, etc.

## Front matter and the introduction slide

A YAML front matter at the top of the file configures presentation metadata. Setting any of `title`, `sub_title`, or `author` causes presenterm to render an introduction slide:

```yaml
---
title: "My _first_ **presentation**"
sub_title: (in presenterm!)
author: Myself
---
```

All attributes are optional; omit them all if you don't want an introduction slide. `title` can contain arbitrary markdown (bold, italics, `<span>` tags, etc).

For multiple authors, use `authors` instead of `author`:

```yaml
---
title: Our first presentation
authors:
  - Me
  - You
---
```

The front matter can also carry:

- `theme:` selects or overrides the theme (see [themes.md](themes.md)).
- `options:` sets per-presentation options like `implicit_slide_ends` (see [configuration.md](configuration.md)).
- `event`, `location`, `date`: extra metadata referenceable from template footers.

## Slide titles

Any [setext header](https://spec.commonmark.org/0.30/#setext-headings) is treated as a slide title and rendered specially (by default: centered, vertically padded, distinct color):

```markdown
Hello
===
```

Regular `#` headings are just headings. The `h1_slide_titles` option makes the first `h1` in a slide act as the slide title instead.

## Ending slides

Slides end at an explicit `end_slide` comment:

```html
<!-- end_slide -->
```

Two options change this behavior (see [configuration.md](configuration.md)):

- `implicit_slide_ends: true`: a slide title implies the previous slide ended.
- `end_slide_shorthand: true`: thematic breaks (`---`) also delimit slides.

## Colored text

`span` HTML tags (and **only** `span` tags) can set foreground/background colors:

- Via `style`, using only the CSS attributes `color` and `background-color`. Colors can be hex or refer to theme palette colors via `palette:<name>` / `p:<name>`.
- Via `class`, pointing to a class defined in the theme palette (a foreground/background pair).

```markdown
<span style="color: #ff0000; background-color: palette:foo">colored text!</span>

<span class="my_class">colored text!</span>
```

## Font sizes

The kitty terminal (>= 0.40.0) supports a protocol for changing font size. presenterm uses it:

- In themes, via the `font_size` property on the intro slide title, slide titles, and headers (built-in themes use 2 for these; 1 is the default).
- Explicitly, via the `font_size` comment command (values 1-7):

```markdown
# Normal text

<!-- font_size: 2 -->

# Larger text
```

Terminal support is verified at startup; font size changes are ignored on unsupported terminals.

# Comment Commands

presenterm uses HTML comments as commands for behavior vanilla markdown can't express. `presenterm --list-comment-commands` prints all of them. A single-line HTML comment is assumed to be a command (unless `command_prefix` is configured); multi-line comments are treated as plain comments.

## pause

Content after a `pause` only shows up once you advance the presentation:

```html
<!-- pause -->
```

## font_size

Sets the font size (1-7, default 1) for the remainder of the slide. kitty-only, see above.

```html
<!-- font_size: 2 -->
```

## jump_to_middle

Jumps to the vertical center of the page. Combined with a slide title it makes a nice section-separator slide:

```markdown
<!-- jump_to_middle -->

Farming potatoes
===
```

## new_line / new_lines

Markdown collapses consecutive blank lines, so these create explicit vertical spacing. `newline`/`newlines` are accepted aliases.

```markdown
hi

<!-- new_lines: 10 -->

mom

<!-- new_line -->

bye
```

## incremental_lists

Instead of a `pause` between every bullet, make each list item appear one keypress at a time **until the end of the current slide**:

```markdown
<!-- incremental_lists: true -->

* this
* appears
* one after
* the other

<!-- incremental_lists: false -->

* this appears
* all at once
```

By default incremental lists also pause before and after the list itself; the `defaults.incremental_lists` config setting changes that. The `incremental_lists` option enables this for all lists.

## list_item_newlines

Number of new lines between list items for the remainder of the slide. Useful to "unpack" a short list so it fills more of the slide. Also available globally as the `list_item_newlines` option.

```markdown
<!-- list_item_newlines: 2 -->

* this
* is
* more
* spaced
```

## include

Includes an external markdown file as if it were part of the presentation:

```markdown
<!-- include: foo.md -->
```

Paths referenced by an included file resolve relative to that file. E.g. if you include `foo/bar.md` and it references image `tar.png`, the image is looked up at `foo/tar.png`.

## no_footer

Hides the footer on this slide:

```html
<!-- no_footer -->
```

## skip_slide

Excludes the slide from the presentation:

```html
<!-- skip_slide -->
```

## alignment

Text alignment for the remainder of the slide: `left` (default), `center`, or `right`.

```markdown
<!-- alignment: center -->

centered
```

## speaker_note

A note only shown in a `--listen-speaker-notes` instance (see [presenting.md](presenting.md)):

```markdown
<!-- speaker_note: this is a speaker note -->
```

Multiline notes use YAML block syntax:

```yaml
<!--
speaker_note: |
  something
  something else
-->
```

## snippet_output

Displays the output of an executable snippet defined elsewhere with `+id:<identifier>` (see [code.md](code.md)):

```html
<!-- snippet_output: identifier -->
```

## User comments

Comments ignored during rendering, for personal notes/TODOs:

```markdown
<!-- // This is a user comment -->
<!-- comment: This is also a user comment which won't be rendered -->
```

# Column Layouts

presenterm supports column layouts via comment commands (deliberately not HTML divs). Define a layout, then enter columns as you write.

## Defining layouts

```html
<!-- column_layout: [3, 2] -->
```

This defines 2 columns whose widths are proportional to their "size units": the total here is 5, so the first column takes 3/5 (60%) of the screen and the second 2/5 (40%). Any number of columns with any unit sizes works.

## Using columns

Enter a column (0-indexed) before writing content into it:

```html
<!-- column: 0 -->
```

Markdown goes into that column until you switch to another column, reset the layout with `<!-- reset_layout -->`, or the slide ends. Content after `reset_layout` spans the full width below the columns.

## Example

~~~markdown
Layout example
==============

<!-- column_layout: [2, 1] -->

<!-- column: 0 -->

This is some code I like:

```rust
fn potato() -> u32 {
    42
}
```

<!-- column: 1 -->

![](examples/doge.png)

<!-- reset_layout -->

Because we just reset the layout, this text is now below both of the columns.
~~~

## Centering content

A layout like `[1, 3, 1]` where you only write into column 1 centers your content in the middle 60% of the screen.

While running, press `T` to toggle a visual grid that shows column boundaries.

# Images

Image tags render natively in terminals supporting the iterm2 image protocol, the kitty graphics protocol, or sixel (kitty, iterm2, WezTerm, ghostty, foot, ...). Otherwise images fall back to ASCII blocks.

- Image paths are relative to the presentation file: `![](food/potato.png)` resolves to `$PRESENTATION_DIRECTORY/food/potato.png`.
- Images render at their original size, unless they don't fit, in which case they're resized preserving aspect ratio.
- Remote images are not supported by design.
- Under tmux, enable the `allow-passthrough` option for images to work.

## Image size

The `image:width` / `image:w` attribute sizes an image as a percentage of terminal width (aspect ratio preserved, no overflow allowed):

```markdown
![image:width:50%](image.png)
```

The `image:` prefix is configurable via the `image_attributes_prefix` option.

## Protocol detection

The image protocol is auto-detected. Override with `--image-protocol <protocol>` or the `defaults.image_protocol` config key. Values: `auto`, `iterm2`, `iterm2-multipart`, `kitty-local`, `kitty-remote`, `sixel`, `ascii-blocks`.
