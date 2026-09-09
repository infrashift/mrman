---
title: Themes
description: The 23 bundled themes, how light/dark pairing works, and the 41-slot format for writing your own.
---

## Bundled themes

Twenty-three, in display order:

| Family | Names |
|---|---|
| Built-in | `dark`, `light` |
| Ayu | `ayu-light`, `ayu-mirage` |
| One Dark | `onedark` |
| GitHub | `github-light`, `github-dark` |
| Catppuccin | `catppuccin-latte`, `catppuccin-frappe`, `catppuccin-macchiato`, `catppuccin-mocha` |
| Gruvbox | `gruvbox-dark`, `gruvbox-light` |
| Nord | `nord-dark`, `nord-light`, `nord-dark-high-contrast`, `nord-light-high-contrast` |
| Solarized | `solarized-light`, `solarized-dark` |
| Tokyo Night | `tokyo-night-storm`, `tokyo-night-day` |
| Everforest | `everforest-dark`, `everforest-light` |

## Selecting one

```toml
theme = "tokyo-night-storm"
```

Or pair a light and a dark variant and let appearance decide:

```toml
theme_dark = "catppuccin-mocha"
theme_light = "catppuccin-latte"
appearance = "system"          # dark | light | system
```

Per-invocation overrides:

```sh
mrman --theme gruvbox-dark
mrman --appearance light
```

### Resolution order

First hit wins:

1. `--theme`
2. `theme`
3. `theme_dark` / `theme_light`, chosen by appearance
4. Whichever of those two is set, if only one is
5. `--appearance`
6. `appearance`
7. The bundled default

An invalid `--theme` **exits** — you asked for it explicitly, so mrman does not
quietly substitute something else. An invalid *configured* theme warns and falls
through to the next step, because a typo in a config file should not stop you
reviewing code.

Bundled names win over local files of the same name.

## Transparency

```toml
transparent_background = true   # default
```

Lets your terminal background show through instead of painting the theme's
panel background. Set it to `false` if your theme's background is
load-bearing — a high-contrast theme over a busy terminal background is worse
than either alone.

## Writing your own

A local theme is a **flat TOML file** at `~/.config/mrman/themes/<name>.toml`
with all 41 color slots present. Each value is either `#RRGGBB` or a named
terminal color (`red`, `brightblue`, …). The file is validated against an
embedded CUE schema, so a missing or malformed slot tells you which one.

```toml
# ~/.config/mrman/themes/mine.toml
panel_bg     = "#1a1b26"
bg_highlight = "#292e42"
fg_primary   = "#c0caf5"
fg_secondary = "#9aa5ce"
fg_dim       = "#565f89"
# ... and the rest
syntax_style = "nord"
```

Then `theme = "mine"`.

### The 41 slots

| Group | Keys |
|---|---|
| Surfaces | `panel_bg`, `bg_highlight`, `fg_primary`, `fg_secondary`, `fg_dim` |
| Diff | `diff_add`, `diff_add_bg`, `diff_del`, `diff_del_bg`, `diff_context`, `diff_hunk_header`, `expanded_context_fg` |
| Syntax overlay | `syntax_add_bg`, `syntax_del_bg` |
| File status | `file_added`, `file_modified`, `file_deleted`, `file_renamed` |
| Review state | `reviewed`, `pending` |
| Comment types | `comment_note`, `comment_suggestion`, `comment_issue`, `comment_praise` |
| Chrome | `border_focused`, `border_unfocused`, `status_bar_bg`, `cursor_color`, `cursor_line_bg`, `branch_name`, `help_indicator` |
| Messages | `message_info_fg`, `message_info_bg`, `message_warning_fg`, `message_warning_bg`, `message_error_fg`, `message_error_bg`, `update_badge_fg`, `update_badge_bg` |
| Editor modes | `mode_fg`, `mode_bg` |

`syntax_add_bg` and `syntax_del_bg` are the backgrounds composited *under*
syntax-highlighted added and removed lines, which is why they are separate from
`diff_add_bg` / `diff_del_bg`.

`comment_*` slots color the four default comment types. If you define your own
[comment types](../configuration/#comment-types) they carry their own `color`,
and these remain the fallbacks.

### Syntax highlighting

Diff bodies are highlighted with [chroma](https://github.com/alecthomas/chroma).
Two optional keys:

```toml
syntax_style = "nord"                          # any registered chroma style
syntax_style_file = "./my-style.xml"           # a chroma XML style file
```

`syntax_style_file` is resolved relative to the themes directory and **wins**
when both are set — with a warning, so you know which one is in effect. An
unknown `syntax_style` name warns and falls back to a light or dark default
chosen by the lightness of `panel_bg`, which is nearly always what you wanted.

## Colors that look wrong

Almost always the terminal, not the theme. Check that you are in truecolor mode
(`echo $COLORTERM` should say `truecolor` or `24bit`). Named terminal colors
come from your terminal's palette by design, so a theme built from them will
follow your terminal's scheme rather than overriding it — that is the point of
allowing them, but it surprises people who expected `#RRGGBB` fidelity.
