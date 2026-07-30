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
	ShowLegend             bool
	ToStdout               bool
}

// renderExport builds the notes markdown for the current session.
func renderExport(a *app.App, opts exportOptions) (string, error) {
	scope := output.ScopeKind(int(a.DiffSource.Kind))
	data, err := output.BuildTemplateData(a.Session, scope.ScopeLine(a.DiffSource.Commits), output.ExportOptions{
		SessionSlug:     sessionSlugString(a),
		DiffSourceLabel: scope.Label(),
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

// exportToClipboard renders and copies the review, setting the outcome
// message. pendingStdout is returned non-empty in --stdout mode: the caller
// prints it after the TUI exits.
func (m *Model) exportToClipboard() (pendingStdout string) {
	a := m.App
	text, err := renderExport(a, m.export)
	if err != nil {
		if errors.Is(err, errs.ErrNoComments) {
			a.SetMessage("No comments to export - skipping copy")
		} else {
			a.SetError("Export failed: " + err.Error())
		}
		return ""
	}
	if m.export.ToStdout {
		a.SetMessage("Review will print to stdout on exit")
		return text
	}
	viaTerminal, err := output.CopyText(text)
	switch {
	case err != nil:
		a.SetError("Clipboard failed: " + err.Error())
	case viaTerminal:
		a.SetMessage("Review copied to clipboard (via terminal)")
	default:
		a.SetMessage("Review copied to clipboard")
	}
	return ""
}
