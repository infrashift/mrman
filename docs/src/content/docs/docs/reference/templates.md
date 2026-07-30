---
title: Templates
description: Override the exported review markdown and the review body posted with :submit, using Go text/template and a documented data model.
---

Two pieces of markdown mrman produces are templated, with embedded defaults you
can replace:

| Template | Renders |
|---|---|
| `notes` | The export from `y`, `:clip`, `:export` and `--stdout` |
| `review_body` | The body posted with `:submit` |

```toml
[templates]
notes = "~/.config/mrman/templates/notes.md.tmpl"
review_body = "~/.config/mrman/templates/review_body.md.tmpl"
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
| `.Location` | The anchor without backticks — see below |
| `.CommitID` | Short SHA the comment is scoped to, empty when unscoped |
| `.Content` | Raw comment text, may span lines |
| `.Number` | Continuous 1-based sequence across the whole export |
| `.Marker` | Method: the numbered-list marker, e.g. `3.` |
| `.Body` | Method: `.Content` with continuation lines indented under the marker |

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
{{define "entry"}}{{.Marker}} {{if .Type}}**[{{.Type}}]** {{end}}`{{.Location}}`{{if .CommitID}} (commit {{.CommitID}}){{end}} - {{.Body}}
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
`MovedToSummary` items) and `.Content`. `upper` is available.

The default:

```go-template
{{- range $i, $c := .ReviewComments}}{{if $i}}

{{end}}{{$c.Content}}{{end}}
{{- if .MovedToSummary}}{{if .ReviewComments}}

{{end}}## Unplaced comments

{{range .MovedToSummary}}- {{if .Type}}[{{upper .Type}}] {{end}}{{.Path}}: {{.Content}}
{{end}}{{end}}
```

`MovedToSummary` is how mrman refuses to drop a comment it could not place —
see [preflight](../../guides/pull-requests/#preflight) for the reasons a comment
ends up there. **Keep that section in any override**, or you will silently lose
comments on submit.

## Testing an override

```sh
mrman -w --stdout
```

Prints the notes export to stdout instead of the clipboard, so you can iterate
on the template without leaving the shell. If you see the default output plus a
warning in the status bar, your template did not parse.
