---
title: Templates
description: Override the exported review markdown and the review body posted with :submit, using Go text/template and a documented data model.
---

Three pieces of text mrman produces are templated, with embedded defaults you
can replace:

| Template | Renders |
|---|---|
| `notes` | The export from `y`, `:clip`, `:export` and `--stdout` |
| `review_body` | The body posted with `:submit` |
| `patch_reply` | The mail reply from `:patch` when reviewing a patch series |

```toml
[templates]
notes = "~/.config/mrman/templates/notes.md.tmpl"
review_body = "~/.config/mrman/templates/review_body.md.tmpl"
patch_reply = "~/.config/mrman/templates/patch_reply.txt.tmpl"
```

Both are Go [`text/template`](https://pkg.go.dev/text/template). A template that
fails to parse **falls back to its default with a warning** rather than losing
your review — an export is not the place to discover a syntax error.

## The notes template

### Data model

Every exported field and method below is stable; overrides may rely on them.

**Top level**

| Field | Type | Meaning |
|---|---|---|
| `.Slug` | string | Session slug, empty when the session has none |
| `.Repo` | string | Repository path |
| `.Branch` | string | Branch name, empty on a detached head |
| `.DiffSourceLabel` | string | Human scope description, e.g. `staged changes` |
| `.ScopeLine` | string | The full `Reviewing …` line; empty for working-tree reviews |
| `.SessionNotes` | string | Session summary, empty when unset |
| `.CommentTypes` | `[]LegendEntry` | Legend entries for types actually used |
| `.ShowLegend` | bool | Gated by `export_legend` |
| `.ReviewComments` | `[]TemplateComment` | Review-level comments, numbered first |
| `.Files` | `[]TemplateFile` | Files carrying comments, sorted by path |
| `.Counts` | struct | `.Files`, `.Reviewed`, `.Comments` totals |

**`LegendEntry`** — `.ID`, `.Label`, `.Definition` (falls back to `ID`).

**`TemplateFile`** — `.Path`, `.Status` (`added`, `modified`, …), `.Comments`
(file-level first, then line comments by line).

**`TemplateComment`**

| Field | Meaning |
|---|---|
| `.Type` | Uppercased export label (`ISSUE`); empty for untyped |
| `.Author` | Comment author (`user` for local drafts) |
| `.ShowAuthor` | Whether `.Author` should be badged for this reader — see below |
| `.Location` | The anchor without backticks — see below |
| `.CommitID` | Short SHA the comment is scoped to, empty when unscoped |
| `.Content` | Raw comment text, may span lines |
| `.Anchor` | The anchor as the diff parser saw it (`path:line` or `path:start-end`), for templates that quote hunks |
| `.Number` | Continuous 1-based sequence across the whole export |
| `.Diff` | Unified-diff text of the hunk the comment is anchored in, unfenced; empty unless `export_diff` is on — see below |
| `.Marker` | Method: the numbered-list marker, e.g. `3.` |
| `.Body` | Method: `.Content` with continuation lines indented under the marker |
| `.DiffBlock` | Method: `.Diff` as a fenced block with its own surrounding blank lines, or empty |
| `.AuthorTag` | Method: `@name`, or empty when the comment is not badged |
| `.Tag` | Method: the bracket contents — `.Type`, `.AuthorTag`, or both joined by a space |

`.Location` formats:

| Shape | Means |
|---|---|
| `src/x.go:42` | A line on the new side |
| `src/x.go:~42` | A line on the old (deleted) side |
| `src/x.go:10-15` | A range on the new side |
| `src/x.go:~10-~15` | A range on the old side |
| `src/x.go` | A file comment |
| `Review Comment (scope: …)` | A review-level comment |

Prefer `.Body` over `.Content` in a numbered list — it handles multi-line
comments and strips stray carriage returns. `.Content` is there when you are
building something other than a list.

### The author badge

`.Tag` is what the default template brackets, so a review written by more than
one person exports as `**[ISSUE @claude]**` and an untyped comment as
`**[@claude]**`. Use `.Tag` rather than `.Type` unless you want to drop
attribution.

Which comments carry a badge follows the same rule the TUI draws by:

- Your own comments are bare — the badge marks what *someone else* wrote. Set
  `show_own_author = true` to attribute every comment instead.
- A comment with no author is never badged. That is a session written before
  mrman stamped authors, and inventing one would be a guess.
- With no `username` configured, nothing matches you, so every comment badges
  as `@user`. Setting `username` is what quiets that.

`.ShowAuthor` is the resolved verdict, already folded into `.AuthorTag` and
`.Tag`; read it directly only if you are rendering the badge some other way.

### Quoted hunks

With `export_diff = true`, every line-anchored comment carries the hunk it sits
in. `.Diff` is the raw text — the `@@` header, then the hunk's lines with their
`+`/`-` prefixes — and `.DiffBlock` is that same text fenced and spaced ready to
drop straight above a list entry, which is what the default does.

Both are empty when there is nothing to quote: review and file comments are not
anchored to a hunk, and neither are comments on a binary file, an oversized one,
a file the current diff no longer carries, or a line that has since gone. A run
of comments inside one hunk quotes it once, on the first of them.

Use `.DiffBlock` unless you are placing the fence yourself:

```go-template
{{range .Comments}}{{if .Diff}}{{codefence "diff" .Diff}}
{{end}}- {{.Location}}: {{.Content}}
{{end}}
```

`codefence` widens the fence past any backtick run in the diff, so a hunk that
itself contains a code fence cannot break out of the block.

### Helper functions

| Function | Use |
|---|---|
| `upper` | `{{upper .Type}}` |
| `trunc` | `{{trunc 80 .Content}}` — at most N runes |
| `indent` | `{{indent 2 .Content}}` |
| `join` | `{{join ", " .Names}}` |
| `codefence` | Wraps text in a fence sized to avoid collisions |

### The default

Worth reading before you replace it — it defines an `entry` template and reuses
it for both review-level and per-file comments:

```go-template
{{define "entry"}}{{.DiffBlock}}{{.Marker}} {{if .Tag}}**[{{.Tag}}]** {{end}}`{{.Location}}`{{if .CommitID}} (commit {{.CommitID}}){{end}} - {{.Body}}
{{end}}{{if .Slug}}## Session: {{.Slug}}

{{end}}I reviewed your code and have the following comments. Please address them.

{{if .ScopeLine}}{{.ScopeLine}}

{{end}}{{if and .ShowLegend .CommentTypes}}Comment types: {{range $i, $t := .CommentTypes}}{{if $i}}, {{end}}{{upper $t.Label}} ({{$t.Definition}}){{end}}

{{end}}{{if .SessionNotes}}Summary: {{.SessionNotes}}

{{end}}{{if or .ReviewComments .Files}}## Local mrman Comments

{{range .ReviewComments}}{{template "entry" .}}{{end}}{{range .Files}}{{range .Comments}}{{template "entry" .}}{{end}}{{end}}{{end}}
```

### A worked override

Group by file with headings instead of one flat list:

```go-template
# Review: {{.Repo}}{{if .Branch}} ({{.Branch}}){{end}}

{{.Counts.Comments}} comment(s) across {{.Counts.Files}} file(s); {{.Counts.Reviewed}} reviewed.

{{range .ReviewComments}}> {{.Content}}
{{end}}
{{range .Files}}## `{{.Path}}` — {{.Status}}

{{range .Comments}}- {{if .Type}}**{{.Type}}** {{end}}`{{.Location}}` — {{.Content}}
{{end}}
{{end}}
```

## The review-body template

This one is deliberately small, because it is posted to a forge rather than read
locally.

| Field | Meaning |
|---|---|
| `.ReviewComments` | Review-level comments |
| `.MovedToSummary` | Comments that could not be anchored inline |

Each carries `.Type` (empty when untyped), `.Path` (set on
`MovedToSummary` items), `.Content`, `.Author`/`.ShowAuthor`, and the
`.AuthorTag`/`.Tag` methods described [above](#the-author-badge). `upper` is
available — `.Tag` uppercases the type for you.

The default:

```go-template
{{- range $i, $c := .ReviewComments}}{{if $i}}

{{end}}{{if $c.AuthorTag}}**[{{$c.AuthorTag}}]** {{end}}{{$c.Content}}{{end}}
{{- if .MovedToSummary}}{{if .ReviewComments}}

{{end}}## Unplaced comments

{{range .MovedToSummary}}- {{if .Tag}}[{{.Tag}}] {{end}}{{.Path}}: {{.Content}}
{{end}}{{end}}
```

This body is posted to a forge, where `@name` may resolve to a real mention.
Drop `.AuthorTag` from an override if that ping is unwanted — the badge in the
notes export is unaffected.

`MovedToSummary` is how mrman refuses to drop a comment it could not place —
see [preflight](../../guides/merge-requests/#preflight) for the reasons a comment
ends up there. **Keep that section in any override**, or you will silently lose
comments on submit.

## The patch-reply template

`:patch` quotes the diff with your comments interleaved, for replying on a
mailing list. Its data model (`PatchData`: the reply headers, one entry per
file and hunk with the quoted lines, and the comments that could not be
placed) is in `internal/output/patch.go`; the embedded default is
`internal/output/templates/patch_reply.txt.tmpl` and the same `upper`,
`trunc`, `indent`, `join` and `codefence` helpers apply. Start from the
default when overriding — the quoting rules it encodes are what make the
reply threadable.

## Testing an override

```sh
mrman -w --stdout
```

Prints the notes export to stdout instead of the clipboard, so you can iterate
on the template without leaving the shell. If you see the default output plus a
warning in the status bar, your template did not parse.
