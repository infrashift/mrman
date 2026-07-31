---
title: Keybindings
description: "Every key, chord, mouse action and : command in mrman, with the count prefixes each one accepts."
---

Press `?` in the app for the same list. `<leader>` is `;` by default and is
configurable to any single character (`leader` in the config).

Most navigation keys take a count prefix: `5j`, `3}`, `{N}G`. The help popup
takes one too (`20j`), where `{N}G` just means "to the end" — a popup has no
source lines to jump to.

## Navigation

| Key | Action |
|---|---|
| `j` `k` / `↓` `↑` | Move the cursor down / up |
| `h` `l` / `←` `→` | Scroll horizontally (4 columns) |
| `Ctrl-e` / `Ctrl-y` | Scroll the view without moving the cursor |
| `Ctrl-d` / `Ctrl-u` | Half page down / up |
| `Ctrl-f` / `Ctrl-b`, `PgDn` / `PgUp` | Full page down / up |
| `g` / `G` | First / last file |
| `{N}G` | Jump to source line N in the current file |
| `}` / `{` | Next / previous file |
| `]` / `[` | Next / previous hunk |
| `m` / `M` | Next / previous comment (including forge comments) |
| `(` / `)` | Previous / next commit in a multi-commit review |
| `zt` / `zz` / `zb` | Cursor to top / center / bottom |
| `Enter` or `Space` | Expand or collapse hidden context at the cursor |

`Enter` matches tuicr; `Space` is an mrman alias for the same action. In the
file tree they mean different things — see below — but the panes never
collide, because the focused pane handles its own keys first.

## Panels

| Key | Action |
|---|---|
| `Tab` / `Shift-Tab` | Cycle focus through the visible panes |
| `<leader>e` | Toggle the file list |
| `<leader>h` / `<leader>l` | Focus the file list / diff |
| `<leader>j` / `<leader>k` | Focus down / up the layout |
| `<leader>s` | Toggle the inline commit selector |
| `<leader>f` | Toggle single-file view |
| `<leader>t` | Back to the target selector |
| `<leader>p` | Back to the pull-request list |
| `Esc` | Back to the selector when no diff is loaded |

`<leader>t` and `<leader>p` are the way out of a review — the same places
`:commits` and `:prs` reach. Esc leaves a loaded review alone (it only
discards a half-typed count); it reopens the selector solely from the empty
state you land in by escaping the selector at startup, which would otherwise
have no key that led anywhere.

Tab skips panes that are not on screen. The comment navigator appears only
when there is a comment to navigate; the commit selector only on a
multi-commit review.

## File tree (when focused)

| Key | Action |
|---|---|
| `j` / `k` | Move |
| `Enter` | Open the file, or expand/collapse the directory |
| `Space` | Toggle the directory |
| `o` / `O` | Expand / collapse all directories |
| `←` / `→` | Scroll horizontally |

## Comment navigator (when focused)

| Key | Action |
|---|---|
| `j` / `k` | Move |
| `Enter` | Jump to the comment in the diff, or peek it when its file is folded |

The navigator lists every comment in the review, including those on files you
have marked reviewed — folding a file keeps your notes on it in the list. Those
rows have no diff row to jump to, so `Enter` opens a read-only **peek panel**
instead: the commented line with a few lines of context, and the comment
beneath it. The file stays folded.

The marker glyph shows scope (`★` review, `▣` file, `●` line, `◇` a forge
thread) and its colour shows the comment type.

## Comment peek panel

| Key | Action |
|---|---|
| `j` / `k` | Scroll |
| `Ctrl-d` / `Ctrl-u` | Half page down / up |
| `q` / `Esc` / `Enter` | Close |

## Commit selector (when focused)

| Key | Action |
|---|---|
| `j` / `k` | Move |
| `Space` / `Enter` | Toggle the commit and reload the diff |
| `Esc` | Back to the diff |

## Review

| Key | Action |
|---|---|
| `r` | Toggle the file reviewed |
| `R` | Toggle the hunk reviewed |
| `c` | Comment on the line at the cursor |
| `C` | Comment on the file |
| `<leader>c` | Comment on the whole review |
| `v` / `V` | Visual select |
| `i` / `A` | Edit the comment at the cursor (cursor at start / end) |
| `dd` | Delete the comment at the cursor |
| `y` | Yank the selection, or export the review when nothing is selected |

Comments already pushed to a forge are read-only, as are the forge's own
existing comments; mrman says so rather than silently doing nothing.

### Visual mode

| Key | Action |
|---|---|
| `j` / `k` | Extend the selection |
| `c` / `Enter` | Comment on the range |
| `y` | Copy the selection |
| `Esc` / `v` / `V` | Cancel |

## Comment box

| Key | Action |
|---|---|
| `Enter`, `Ctrl-Enter`, `Ctrl-s` | Save |
| `Shift-Enter`, `Alt-Enter`, `Ctrl-j`, `Ctrl-k` | Newline |
| `Tab` / `Shift-Tab` | Cycle the comment type |
| `Esc` | Cancel |
| `Ctrl-a` / `Ctrl-e`, `Home` / `End` | Line start / end |
| `Alt-b` / `Alt-f`, `Alt-←` / `Alt-→` | Word left / right |
| `Ctrl-w`, `Alt-Backspace` | Delete the previous word |
| `Ctrl-u` | Clear the line |

The several newline aliases exist because terminals disagree: `Shift-Enter`
needs the kitty keyboard protocol, `Alt-Enter` survives tmux, and `Ctrl-j`
works without it.

### Vim comment editing (`comment_vim = true`, or `:vim`)

Modes Normal / Insert / Visual / Visual-Line. Motions `h j k l w b e 0 ^ $
gg G` with counts; entry `i a I A o O`; edits `x s dd D C cc d{motion}
c{motion} y{motion} ciw diw`; registers `yy p P`; undo `u` / `Ctrl-r`.

From Normal: `:w` (or `Enter` twice) saves, `:q` (or `Esc`/`q` twice)
cancels. `Alt-Enter` saves and `Alt-Esc` cancels without the double press —
Alt is the one modifier on Enter and Esc that survives every terminal we
care about, browser terminals included. `Tab` cycles the comment type in
Normal mode and inserts `comment_tab_width` spaces in Insert mode.

## Search

| Key | Action |
|---|---|
| `/` | Search (the diff, or the help popup when it is open) |
| `n` / `N` | Next / previous match |

## Target selector

| Key | Action |
|---|---|
Three tabs: **Local**, **Pull Requests** and **Patches**.

| Key | Action |
|---|---|
| `Tab` / `Shift-Tab` | Next / previous tab |
| `j` / `k` | Move |
| `Space` | Toggle a commit (Local tab) |
| `Enter` | Confirm the range, open the pull request, or open the patch |
| `/` | Filter the loaded rows (Pull Requests, Patches) |
| `r` | Toggle all-open vs review-requested (PRs) · rescan (Patches) |
| `Esc` | Back to Local, or leave the selector |
| `q` | Quit |

The **Patches** tab is the inbox: it lists the `.patch`, `.diff`, `.mbox` and
`.eml` files in a directory, one level deep, newest first, showing each one's
subject, series size and author. A file it cannot read as a patch is still
listed, with the reason — a patch you expected to see and cannot open is worth
knowing about. It starts in the review's own directory, which for a patch
review is where the artifact lives, so its siblings are already there.

## Mouse

On by default; set `mouse = false` to leave the terminal's own selection
alone.

| Action | Effect |
|---|---|
| Wheel | Scroll the pane under the pointer, without moving its cursor |
| Click a file | Jump to it |
| Click a directory | Expand or collapse it |
| Click a diff line | Place the cursor there |
| Click a commit | Toggle it and reload the diff |
| Click a navigator row | Jump to that comment |
| Drag in the diff | Select; `y` then copies it |

Hold your terminal's bypass modifier (usually Shift or Option) for native
selection while the mouse is enabled.

## Commands

| Command | Action |
|---|---|
| `:q` `:quit` | Quit (unsaved comments block it) |
| `:q!` `:quit!` | Quit without saving |
| `:w` `:write` | Save the session |
| `:x` `:wq` | Save and quit |
| `:e` `:reload` | Re-read the diff; refetches in pull-request mode |
| `:edit` | Open the focused file in `$EDITOR` |
| `:clip` `:export` | Copy the review markdown to the clipboard |
| `:patch` `:reply` | Copy the review as a quoted-diff mail reply |
| `:clear` | Clear comments and reviewed marks |
| `:clearc` | Clear comments only |
| `:diff` | Toggle unified / side-by-side |
| `:wrap` `:set wrap` `:set wrap!` | Line wrap |
| `:focus` `:f` | Toggle single-file view |
| `:stage` | Stage the reviewed files (git) |
| `:commits` `:targets` | Open the target selector |
| `:prs` | Open it on the Pull Requests tab |
| `:patches` | Open it on the Patches tab |
| `:set commits` `:set nocommits` `:set commits!` | Commit selector visibility |
| `:comments unresolved\|all\|hide` | Existing forge comments |
| `:submit` | Submit picker |
| `:submit comment\|approve\|request-changes\|draft` | Submit directly |
| `:vim` `:novim` `:set vim` `:set novim` `:set vim!` | Vim comment box |
| `:{N}` / `:o{N}` | Jump to line N on the new / old side |
| `:help` `:h` | Help |
| `:version` | Show the build |
| `:agent` `:agent off` | Show / revoke the agent-submit grant |

Tab completes commands; repeated presses cycle the matches.

## Quitting

| Key | Action |
|---|---|
| `q` | Quit |
| `ZZ` / `ZQ` | Save and quit / quit without saving |
| `Ctrl-C` twice | Force quit |

## Differences from tuicr

- `Space` also expands context gaps; tuicr uses `Enter` alone. Both work.
- `Tab` in the comment box cycles four built-in comment types (NOTE, ISSUE,
  SUGGESTION, PRAISE) and then the untyped entry. tuicr ships only the untyped
  type, so `Tab` does nothing there until types are configured.
- No `:update` — mrman has no self-updater by design. Use your package
  manager or `go install`.
- No Mercurial backend. git, Jujutsu, `--file` and `-A` are supported.
