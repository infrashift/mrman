package ui

import (
	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/config"
)

// applyConfig wires loaded configuration onto the app state and model,
// mirroring tuicr's main.rs config-override pass.
func applyConfig(cfg config.Config, a *app.App, m *Model) {
	if cfg.Username != "" {
		a.Username = cfg.Username
	}
	if len(cfg.CommentTypes) > 0 {
		defs := make([]app.CommentTypeDef, 0, len(cfg.CommentTypes))
		for _, ct := range cfg.CommentTypes {
			def := app.CommentTypeDef{ID: ct.ID, Label: ct.Label}
			if ct.Definition != "" {
				definition := ct.Definition
				def.Definition = &definition
			}
			if ct.Color != "" {
				color := ct.Color
				def.Color = &color
			}
			defs = append(defs, def)
		}
		a.SetCommentTypes(defs)
	}

	a.ShowFileList = cfg.ShowFileList
	a.ShowCommitSelector = cfg.ShowCommits
	if cfg.DiffView == "side-by-side" {
		a.DiffViewMode = app.ViewSideBySide
	}
	if cfg.CommitOrder == "ascending" {
		a.CommitOrder = app.CommitAscending
	}
	if cfg.InitialCommitSelection == "oldest" {
		a.CommitSelectionStart = app.CommitSelectionOldest
	}
	a.DiffState.WrapLines = cfg.Wrap
	a.CursorLineHighlight = cfg.CursorLine
	a.ScrollOffset = cfg.ScrollOffset
	a.IsSingleFileView = a.IsSingleFileView || cfg.SingleFileView

	if leader := []rune(cfg.Leader); len(leader) == 1 {
		m.leader = leader[0]
	}
	m.CommentVimMode = cfg.CommentVim
	m.export.ShowLegend = cfg.ExportLegend
	m.export.TemplatePath = cfg.Templates.Notes
	m.export.ReviewBodyTemplatePath = cfg.Templates.ReviewBody
	if m.session != nil && cfg.ReviewWatchIntervalMS >= 0 {
		if cfg.ReviewWatchIntervalMS == 0 {
			m.session.watchEvery = 0 // 0 disables via the poll guard below
			m.session.watchDisabled = true
		} else {
			m.session.watchEvery = msDuration(cfg.ReviewWatchIntervalMS)
		}
	}
}
