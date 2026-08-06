// Package output renders review sessions to markdown and copies the result
// to the system clipboard, ported from tuicr's src/output/markdown.rs. The
// markdown shape is driven by a user-overridable text/template; the embedded
// default template reproduces tuicr's export byte-for-byte (with the tool
// word renamed: "## Local mrman Comments").
package output

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"text/template"
)

// defaultNotesTemplate is the embedded default for the notes export. It
// reproduces tuicr's generate_export_content output exactly.
//
//go:embed templates/notes.md.tmpl
var defaultNotesTemplate string

// defaultPatchReplyTemplate is the embedded default for the review-reply
// export: the diff quoted with "> " and comments interleaved beneath the
// lines they refer to.
//
//go:embed templates/patch_reply.txt.tmpl
var defaultPatchReplyTemplate string

// LegendEntry is one comment-type definition as it appears in the export
// legend. Label is uppercased by the template; Definition falls back to ID
// when the configuration has none.
type LegendEntry struct {
	ID         string
	Label      string
	Definition string
}

// TemplateComment is one comment as seen by templates. It is part of the
// stable, documented template data model.
type TemplateComment struct {
	// Type is the uppercased export label ("ISSUE"); empty for the typeless
	// default, which suppresses the **[TYPE]** marker in the default
	// template.
	Type string
	// Author is the comment author ("user" for local drafts).
	Author string
	// Location is the anchor without backticks: "src/x.go:42" for a line,
	// "src/x.go:~42" on the old (deleted) side, "src/x.go:10-15" /
	// "src/x.go:~10-~15" for ranges, "src/x.go" for a file comment, and
	// "Review Comment (scope: ...)" for review-level comments.
	Location string
	// CommitID is the short SHA the comment is scoped to, empty when
	// unscoped.
	CommitID string
	// Content is the raw comment text (may span multiple lines).
	Content string
	// Number is the comment's continuous 1-based sequence number across the
	// whole export.
	Number int
	// Anchor is a badge for a comment whose anchor moved since it was
	// written ("moved"), empty when the anchor still checks out. Only the
	// patch-reply export sets it; the notes template ignores it, which is
	// why adding it here is backward compatible for existing overrides.
	Anchor string
	// Diff is the unified-diff text of the hunk this comment is anchored in:
	// the "@@" header followed by the hunk's lines, prefixes included, no
	// fence. It is empty unless the export asked for diffs
	// (ExportOptions.IncludeDiff, from the export_diff setting), and stays
	// empty for review and file comments, for files the diff does not carry,
	// and when a run of comments shares one hunk and an earlier one already
	// carried it.
	Diff string
}

// Marker returns the numbered-list marker for this comment, e.g. "3.".
func (c TemplateComment) Marker() string {
	return fmt.Sprintf("%d.", c.Number)
}

// Body returns Content formatted for a numbered list entry: trailing "\r"
// stripped from every line and continuation lines indented one column past
// the marker, so multi-line comments align under the list text.
func (c TemplateComment) Body() string {
	indent := strings.Repeat(" ", len(c.Marker())+1)
	lines := strings.Split(c.Content, "\n")
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if i > 0 {
			line = indent + line
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// DiffBlock returns Diff as a fenced code block ready to sit above the
// comment's list entry, or "" when there is no diff to show.
//
// The surrounding blank lines are part of the block rather than the template
// because they are what makes it render: a fence needs a blank line before it
// and the preceding entry ends with a single newline, so the block supplies
// its own separation on both sides. That also keeps the no-diff case emitting
// nothing at all, byte for byte.
func (c TemplateComment) DiffBlock() string {
	if c.Diff == "" {
		return ""
	}
	return "\n" + codefenceFn("diff", c.Diff) + "\n\n"
}

// TemplateFile is one reviewed file carrying comments, as seen by templates.
type TemplateFile struct {
	// Path is the display path of the file.
	Path string
	// Status is the diff status ("added", "modified", ...).
	Status string
	// Comments holds the file's comments: file-level comments first, then
	// line comments ordered by line key.
	Comments []TemplateComment
}

// TemplateData is the stable, documented data model handed to the notes
// template. User template overrides may rely on every exported field and
// method here.
type TemplateData struct {
	// Slug is the session slug, empty when the session has none.
	Slug string
	// Repo is the repository path.
	Repo string
	// Branch is the branch name, empty on detached heads.
	Branch string
	// DiffSourceLabel is the human scope description, e.g. "staged changes".
	DiffSourceLabel string
	// ScopeLine is the full "Reviewing ..." line, empty for working-tree
	// reviews which render no scope line.
	ScopeLine string
	// SessionNotes is the session summary, empty when unset.
	SessionNotes string
	// CommentTypes lists the legend entries for comment types actually used
	// in the session; the typeless "none" default never appears.
	CommentTypes []LegendEntry
	// ShowLegend gates the "Comment types:" line.
	ShowLegend bool
	// ReviewComments are the review-level comments, numbered first.
	ReviewComments []TemplateComment
	// Files lists files that carry comments, sorted by path.
	Files []TemplateFile
	// Counts holds session totals: all session files (with or without
	// comments), files marked reviewed, and total comments.
	Counts struct{ Files, Reviewed, Comments int }
}

// funcMap returns the helper functions available to notes templates.
func funcMap() template.FuncMap {
	return template.FuncMap{
		"upper":     strings.ToUpper,
		"trunc":     truncFn,
		"indent":    indentFn,
		"join":      joinFn,
		"codefence": codefenceFn,
	}
}

// truncFn shortens s to at most n runes.
func truncFn(n int, s string) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// indentFn prefixes every line of s with n spaces.
func indentFn(n int, s string) string {
	if n <= 0 {
		return s
	}
	pad := strings.Repeat(" ", n)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

// joinFn joins elems with sep.
func joinFn(sep string, elems []string) string {
	return strings.Join(elems, sep)
}

// codefenceFn wraps s in a fenced code block tagged with lang. The fence is
// extended past the longest backtick run inside s so embedded fences cannot
// break out.
func codefenceFn(lang, s string) string {
	width := 3
	if run := longestBacktickRun(s); run >= width {
		width = run + 1
	}
	fence := strings.Repeat("`", width)
	return fence + lang + "\n" + s + "\n" + fence
}

// longestBacktickRun returns the length of the longest consecutive backtick
// sequence in s.
func longestBacktickRun(s string) int {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	return longest
}

// LoadNotesTemplate loads the notes template. An empty overridePath returns
// the embedded default. A non-empty path is read and parsed; on read or
// parse errors the default is returned together with a warning, matching
// mrman's config philosophy of never failing startup on bad user input.
func LoadNotesTemplate(overridePath string) (*template.Template, []string) {
	var warnings []string
	if overridePath != "" {
		raw, err := os.ReadFile(overridePath)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf(
				"notes template override %s: %v; using embedded default", overridePath, err))
		} else {
			tmpl, parseErr := template.New("notes").Funcs(funcMap()).Parse(string(raw))
			if parseErr != nil {
				warnings = append(warnings, fmt.Sprintf(
					"notes template override %s: %v; using embedded default", overridePath, parseErr))
			} else {
				return tmpl, warnings
			}
		}
	}
	return template.Must(template.New("notes").Funcs(funcMap()).Parse(defaultNotesTemplate)), warnings
}

// RenderNotes executes tmpl against data and returns the rendered markdown.
func RenderNotes(tmpl *template.Template, data *TemplateData) (string, error) {
	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		return "", fmt.Errorf("render notes template: %w", err)
	}
	return sb.String(), nil
}

// LoadPatchReplyTemplate loads the review-reply template, degrading to the
// embedded default with a warning exactly as LoadNotesTemplate does.
func LoadPatchReplyTemplate(overridePath string) (*template.Template, []string) {
	var warnings []string
	if overridePath != "" {
		raw, err := os.ReadFile(overridePath)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf(
				"patch template override %s: %v; using embedded default", overridePath, err))
		} else {
			tmpl, parseErr := template.New("patch").Funcs(funcMap()).Parse(string(raw))
			if parseErr != nil {
				warnings = append(warnings, fmt.Sprintf(
					"patch template override %s: %v; using embedded default", overridePath, parseErr))
			} else {
				return tmpl, warnings
			}
		}
	}
	return template.Must(template.New("patch").Funcs(funcMap()).Parse(defaultPatchReplyTemplate)), warnings
}

// RenderPatchReply executes tmpl against data and returns the reply text.
func RenderPatchReply(tmpl *template.Template, data *PatchData) (string, error) {
	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		return "", err
	}
	return sb.String(), nil
}
