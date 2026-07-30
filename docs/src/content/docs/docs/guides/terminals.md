---
title: Terminal Setup
description: Which terminals give mrman every key, which chords need the kitty keyboard protocol, and the aliases that work when they are unavailable.
sidebar:
  order: 1
---

mrman is a full-screen TUI that wants modifier keys terminals have historically
disagreed about. It works everywhere; it works *best* somewhere. This page is
what to do if a key does not do what the docs say.

## Recommended terminals

Best experience — full [kitty keyboard
protocol](https://sw.kovidgoyal.net/kitty/keyboard-protocol/), so every chord
including `Shift-Enter` arrives intact:

- [**Ghostty**](https://ghostty.org/) — the reference terminal for mrman
- [kitty](https://sw.kovidgoyal.net/kitty/)
- [WezTerm](https://wezterm.org/)
- Recent Alacritty and foot

Works fine, but some chords fall back to aliases: Terminal.app, GNOME
Terminal, Windows Terminal, iTerm2, and most browser-based terminals.

## Why there are several ways to insert a newline

The comment box needs "save" and "newline" on different keys, and the obvious
split — `Enter` saves, `Shift-Enter` newlines — is not portable. So mrman
accepts all of these:

| Key | Needs |
|---|---|
| `Shift-Enter` | The kitty keyboard protocol |
| `Alt-Enter` | Survives tmux |
| `Ctrl-j` / `Ctrl-k` | Works without either |

Same for saving: `Enter`, `Ctrl-Enter` and `Ctrl-s` all save. If `Shift-Enter`
inserts nothing in your terminal, use `Ctrl-j` and nothing else changes.

In [vim comment mode](../../reference/keybindings/#vim-comment-editing-comment_vim--true-or-vim),
`Alt-Enter` saves and `Alt-Esc` cancels without the double press, because Alt
on `Enter` and `Esc` is the one modifier combination that survives every
terminal mrman targets, browser terminals included.

## tmux

mrman runs fine under tmux. Two things to know:

- **`Alt-` chords are the reliable ones.** tmux does not pass the kitty
  keyboard protocol through by default, so prefer `Alt-Enter` over
  `Shift-Enter`.
- If you want the full protocol, tmux 3.4+ can pass it through with
  `set -g extended-keys on` and `set -as terminal-features 'xterm*:extkeys'`
  in your `tmux.conf`, provided the outer terminal supports it.

Pane navigation while mrman is open: `Ctrl-b` then arrows, `Ctrl-b z` to zoom.

## zellij

Also fine. `Alt` plus arrows switches panes, `Alt-f` floats the pane.

## Mouse

Mouse support is on by default: the wheel scrolls the pane under the pointer
without moving its cursor, clicks jump, and dragging in the diff selects a
range that `y` then copies.

This means the terminal's own text selection is taken over. Two ways out:

- Hold your terminal's bypass modifier — usually **Shift**, or **Option** on
  macOS — for native selection.
- Turn it off entirely: `mouse = false` in your config.

## Colors and transparency

`transparent_background = true` (the default) lets your terminal background
show through, which keeps mrman consistent with a translucent or themed
terminal. Set it to `false` if your theme's background is load-bearing.

Themes pick their own syntax highlighting style; if colors look wrong, your
terminal is probably not in truecolor mode. See [Themes](../../reference/themes/).

## Things that do not work

**Piping mrman's stdin.** `script -qec "mrman pr 1" /dev/null` hangs: Bubble
Tea queries the terminal for capabilities at startup and a piped stdin never
answers. If you want mrman's output without a terminal, use `--json` or the
[`mrman review`](../agents/) commands, which are built for exactly that.

**`--auto` without a terminal.** It is refused on purpose — it authorizes an
agent to submit reviews, so it has to come from a person. See the [submit
interlock](../agents/#the-submit-interlock).

## Recording a demo

For screenshots and asciicasts, run mrman in a real terminal and capture the
pane rather than driving it through a pipe:

```sh
tmux new-session -d -x 200 -y 50 'mrman -w'
tmux capture-pane -p -e     # -e keeps the ANSI colors
```

This is also how mrman's own screen-by-screen test sweeps are done.
