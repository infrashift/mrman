package ui

import (
	"errors"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/output"
	"github.com/infrashift/mrman/internal/slug"
)

// exportOptions carries the UI-level export configuration (template override
// and legend flag arrive from config in M5; defaults match tuicr).
type exportOptions struct {
	TemplatePath           string
	ReviewBodyTemplatePath string
	PatchTemplatePath      string
	ShowLegend             bool
	ToStdout               bool
	// PatchContext selects how much of the diff a reply quotes.
	PatchContext output.PatchContext
	// ReplyHeaders threads a reply into the conversation its patch was posted
	// in. Zero when the artifact carried no threading metadata, which is the
	// common case for a locally produced patch — plain `git format-patch`
	// writes no Message-Id.
	ReplyHeaders output.ReplyHeaders
}

// scopeLabel is the human scope description an export carries, preferring
// anything the review can say about itself over the generic kind name.
//
// A comparison is the case that needs it: "two paths" is true but useless
// to whoever reads the exported notes, while the pair itself says exactly
// what was reviewed. Every other source's kind name is already its best
// description.
func scopeLabel(a *app.App, scope output.ScopeKind) string {
	if a.DiffSource.Kind == app.DiffSourceDiffPaths && a.ComparisonLabel != "" {
		return a.ComparisonLabel
	}
	return scope.Label()
}

// renderExport builds the notes markdown for the current session.
func renderExport(a *app.App, opts exportOptions) (string, error) {
	scope := output.ScopeKind(int(a.DiffSource.Kind))
	data, err := output.BuildTemplateData(a.Session, scope.ScopeLine(a.DiffSource.Commits), output.ExportOptions{
		SessionSlug:     sessionSlugString(a),
		DiffSourceLabel: scopeLabel(a, scope),
		ShowLegend:      opts.ShowLegend,
		CommentTypes:    legendEntries(a.CommentTypes),
	})
	if err != nil {
		return "", err
	}
	tmpl, warnings := output.LoadNotesTemplate(opts.TemplatePath)
	for _, w := range warnings {
		a.SetWarning(w)
	}
	return output.RenderNotes(tmpl, data)
}

// renderPatchReply builds the quoted-diff mail reply for the current session.
//
// Unlike the notes export this needs the diff itself, which renderExport never
// did — a reply is the diff with comments interleaved, not a list of comments
// about it.
func renderPatchReply(a *app.App, opts exportOptions) (string, error) {
	scope := output.ScopeKind(int(a.DiffSource.Kind))
	data, err := output.BuildPatchData(a.Session, a.DiffFiles, output.PatchOptions{
		SessionSlug:     sessionSlugString(a),
		DiffSourceLabel: scopeLabel(a, scope),
		ShowLegend:      opts.ShowLegend,
		CommentTypes:    legendEntries(a.CommentTypes),
		Context:         opts.PatchContext,
		Reply:           opts.ReplyHeaders,
		// The anchor verdicts live on the app, and output must not import it,
		// so they cross as functions.
		Outdated:    a.HasOutdatedAnchor,
		AnchorLabel: func(id string) string { return a.AnchorVerdictFor(id).Label() },
	})
	if err != nil {
		return "", err
	}
	tmpl, warnings := output.LoadPatchReplyTemplate(opts.PatchTemplatePath)
	for _, w := range warnings {
		a.SetWarning(w)
	}
	return output.RenderPatchReply(tmpl, data)
}

// legendEntries converts the app's resolved comment types into the export's
// legend shape. Without this the "Comment types:" line never rendered at all —
// export_legend defaulted to on, but no caller supplied the types it needed,
// so every definition a user wrote in [[comment_types]] was dropped.
func legendEntries(defs []app.CommentTypeDef) []output.LegendEntry {
	entries := make([]output.LegendEntry, 0, len(defs))
	for _, d := range defs {
		entry := output.LegendEntry{ID: d.ID, Label: d.Label}
		if d.Definition != nil {
			entry.Definition = *d.Definition
		}
		entries = append(entries, entry)
	}
	return entries
}

func sessionSlugString(a *app.App) string {
	if s, err := slug.ForSession(a.Session); err == nil {
		return s.String()
	}
	return ""
}

// exportToClipboard renders and copies the review as markdown notes.
func (m *Model) exportToClipboard() (pendingStdout string) {
	return m.deliverExport(renderExport, "Review")
}

// patchReplyToClipboard renders and copies the review as a mail reply.
func (m *Model) patchReplyToClipboard() (pendingStdout string) {
	return m.deliverExport(renderPatchReply, "Reply")
}

// deliverExport renders with the given renderer and puts the result where the
// reviewer asked for it, setting the outcome message. pendingStdout is
// returned non-empty in --stdout mode: the caller prints it after the TUI
// exits, since Bubble Tea owns the terminal until then.
func (m *Model) deliverExport(
	render func(*app.App, exportOptions) (string, error), noun string,
) (pendingStdout string) {
	a := m.App
	text, err := render(a, m.export)
	if err != nil {
		if errors.Is(err, errs.ErrNoComments) {
			a.SetMessage("No comments to export - skipping copy")
		} else {
			a.SetError("Export failed: " + err.Error())
		}
		return ""
	}
	if m.export.ToStdout {
		a.SetMessage(noun + " will print to stdout on exit")
		return text
	}
	viaTerminal, err := output.CopyText(text)
	switch {
	case err != nil:
		a.SetError("Clipboard failed: " + err.Error())
	case viaTerminal:
		a.SetMessage(noun + " copied to clipboard (via terminal)")
	default:
		a.SetMessage(noun + " copied to clipboard")
	}
	return ""
}
