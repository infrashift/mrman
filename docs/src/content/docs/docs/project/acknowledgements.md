---
title: Acknowledgements
description: The projects mrman is built on, ported from, and inspired by — and the licences that come with them.
---

mrman is not a from-scratch idea. It is a port of one project, shaped by
another, and standing on a stack of libraries that do the genuinely hard parts.
This page says who did what.

## tuicr — the thing mrman is a port of

[**tuicr**](https://github.com/agavra/tuicr) by
[agavra](https://github.com/agavra) — *"A code review TUI with vim keybindings.
Export to GitHub, GitLab, or clipboard."* Rust, MIT.

This is the largest debt by a wide margin, and "inspired by" undersells it.
mrman is a Go reimplementation of tuicr: the review model, the keybindings, the
continuous-diff presentation, the comment scopes, the slug-addressed session
storage and the persisted JSON format all originate there. The session format
is deliberately kept structurally identical to tuicr's v1.3. tuicr's own test
suite is ported throughout as the behavioural spec, and a fair number of
mrman's tests still cite tuicr issue numbers as the reason a rule exists — the
comment in `internal/slug` explaining why live sessions embed a HEAD SHA points
at tuicr #378, because that is where the bug was found and fixed.

If you want the Rust original, use it. It is a good tool, and mrman exists
because it was worth having in Go with a four-forge API layer rather than
because anything was wrong with it. [mrman vs tuicr](../comparison/#mrman-vs-tuicr)
is an honest account of where they differ.

:::note[Licence]
tuicr is MIT, © 2025 tuicr contributors. mrman is Apache-2.0. The MIT licence
requires its copyright and permission notice to travel with substantial
portions of the work, so mrman carries it in the
[`NOTICE`](https://github.com/infrashift/mrman/blob/main/NOTICE) file at the
repository root, shipped in every release archive. It is reproduced under
[Licensing](#licensing) below.
:::

## Hunk — a different answer to the same question

[**Hunk**](https://github.com/modem-dev/hunk) by [Modem](https://github.com/modem-dev)
— *"Review-first terminal diff viewer for agent-authored changesets, built on
OpenTUI and Pierre diffs."* TypeScript, MIT.

Hunk takes the same starting position — review belongs in the terminal, and a
growing share of the diffs under review were written by an agent — and answers
it differently at almost every turn: OpenTUI rather than Bubble Tea, inline
agent annotations rather than a JSON session CLI, a responsive split/stack
layout rather than a fixed pane arrangement. Its watch mode, its multi-file
sidebar, and its insistence that review is a first-class activity rather than a
`git diff` with colours are all worth taking seriously.

No code is shared between the two. The debt is one of framing: a project that
treats agent-authored changesets as the normal case rather than the exception
is a clarifying thing to have read.

## Charm — the terminal stack

Almost everything you actually look at is
[Charm](https://charm.sh)'s work:

| | |
|---|---|
| [**Bubble Tea**](https://github.com/charmbracelet/bubbletea) | The Elm-architecture runtime the whole app is a `Model` inside |
| [**Lip Gloss**](https://github.com/charmbracelet/lipgloss) | Styling and layout |
| **`charmbracelet/x/*`** | `ansi`, `term`, `termios`, `windows`, `colorprofile`, `ultraviolet` — the parts that make a terminal behave |

Kitty keyboard-protocol support is why mrman can tell a key release from a key
press, which is what makes the two-press file walk safe. That is Bubble Tea's
doing, not mrman's.

## Everything else mrman leans on

| | |
|---|---|
| [**Chroma**](https://github.com/alecthomas/chroma) | Syntax highlighting, by Alec Thomas |
| [**CUE**](https://cuelang.org) | Validates user-written TOML config and themes, so mistakes become warnings instead of crashes |
| [**go-git**](https://github.com/go-git/go-git) | Git plumbing |
| [**BurntSushi/toml**](https://github.com/BurntSushi/toml) | Config parsing |
| [**Cobra**](https://github.com/spf13/cobra) | The CLI surface |
| [**adrg/xdg**](https://github.com/adrg/xdg) | Config and data directories that respect the spec |
| [**google/uuid**](https://github.com/google/uuid) | Session and comment identifiers |
| [**go-shellquote**](https://github.com/kballard/go-shellquote) | Parsing `token_cmd` and editor commands the way a shell would |

And the four forge clients, without which the submit path would be a pile of
hand-rolled HTTP:

| | |
|---|---|
| [**go-github**](https://github.com/google/go-github) + [**githubv4**](https://github.com/shurcooL/githubv4) | GitHub REST and GraphQL |
| [**gitlab-org/api/client-go**](https://gitlab.com/gitlab-org/api/client-go) | GitLab |
| [**azure-devops-go-api**](https://github.com/microsoft/azure-devops-go-api) | Azure DevOps |
| [**forgejo-sdk**](https://codeberg.org/mvdkleijn/forgejo-sdk) | Forgejo and Gitea |

## Theme authors

Every bundled theme except mrman's own `dark` and `light` is a port of someone
else's colour scheme. The palettes are theirs:

| Theme | Original |
|---|---|
| `tokyo-night-storm`, `tokyo-night-day` | [Tokyo Night](https://github.com/enkia/tokyo-night-vscode-theme) |
| `catppuccin-latte`, `-frappe`, `-macchiato`, `-mocha` | [Catppuccin](https://github.com/catppuccin/catppuccin) |
| `gruvbox-dark`, `gruvbox-light` | [Gruvbox](https://github.com/morhetz/gruvbox) |
| `nord-dark`, `nord-light` (+ high-contrast) | [Nord](https://www.nordtheme.com) |
| `everforest-dark`, `everforest-light` | [Everforest](https://github.com/sainnhe/everforest) |
| `solarized-dark`, `solarized-light` | [Solarized](https://ethanschoonover.com/solarized/) |
| `ayu-light`, `ayu-mirage` | [Ayu](https://github.com/ayu-theme/ayu-colors) |
| `onedark` | [One Dark](https://github.com/atom/atom) (Atom) |
| `github-light`, `github-dark` | GitHub's editor palettes |

Syntax highlighting inside those themes is Chroma's, mapped onto each palette —
see [Themes](../../reference/themes/) for how to write your own.

## Licensing

mrman is Apache-2.0. Its dependencies carry their own licences, which travel
with them; `go mod download` and your tooling of choice will enumerate them
precisely, and this page is a courtesy rather than a substitute for that.

The one that needs stating explicitly is tuicr's, because mrman is a port of it
rather than merely a user of it. It lives in
[`NOTICE`](https://github.com/infrashift/mrman/blob/main/NOTICE) — the
canonical copy, alongside `LICENSE` in every release archive — and is
reproduced here for convenience:

```text
MIT License

Copyright (c) 2025 tuicr contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## And you

If you filed an issue, argued with a default, or told us a keybinding was
wrong: that is the part of this that does not fit in a table. Thank you.
[Contributing](../contributing/) if you want to do more of it.
