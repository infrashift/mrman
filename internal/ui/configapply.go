package ui

import (
	"fmt"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/risk"
)

// applyConfig wires loaded configuration onto the app state and model,
// mirroring tuicr's main.rs config-override pass.
func applyConfig(cfg config.Config, a *app.App, m *Model) {
	a.ShowOwnAuthor = cfg.ShowOwnAuthor
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
		m.SetLeader(leader[0])
	}
	m.CommentVimMode = cfg.CommentVim
	m.commentTabWidth = cfg.CommentTabWidth
	m.mouseEnabled = cfg.Mouse
	m.export.ShowLegend = cfg.ExportLegend
	m.export.IncludeDiff = cfg.ExportDiff
	m.export.TemplatePath = cfg.Templates.Notes
	m.export.ReviewBodyTemplatePath = cfg.Templates.ReviewBody
	m.export.PatchTemplatePath = cfg.Templates.PatchReply
	if m.session != nil && cfg.ReviewWatchIntervalMS >= 0 {
		if cfg.ReviewWatchIntervalMS == 0 {
			m.session.watchEvery = 0 // 0 disables via the poll guard below
			m.session.watchDisabled = true
		} else {
			m.session.watchEvery = msDuration(cfg.ReviewWatchIntervalMS)
		}
	}
}

// applyRisk turns on the opt-in risk score when [risk] enabled is set,
// resolving the policy from the repository under review first. It returns
// warnings about policies that were found but could not be used. With risk
// off it does nothing, and App.Risk stays nil.
func applyRisk(cfg config.Config, a *app.App) []string {
	if !cfg.Risk.Enabled {
		return nil
	}
	root := ""
	if a.VcsInfo != nil {
		root = a.VcsInfo.RootPath
	}
	policy, warnings := risk.ResolvePolicy(root, cfg.Risk.Policy)
	a.EnableRisk(policy)
	return warnings
}

// applyVizzini loads a --vizzini-report onto the review. An unreadable
// report is a warning rather than a failure: the review works without it.
// The policy that checks it for staleness is the one risk scoring uses.
func applyVizzini(cfg config.Config, a *app.App, path string) []string {
	if path == "" {
		return nil
	}
	r, err := risk.ReadReport(path)
	if err != nil {
		return []string{"Vizzini report not loaded: " + err.Error()}
	}
	var policy *risk.Policy
	var warnings []string
	if a.Risk != nil {
		policy = a.Risk.Policy
	} else {
		root := ""
		if a.VcsInfo != nil {
			root = a.VcsInfo.RootPath
		}
		policy, warnings = risk.ResolvePolicy(root, cfg.Risk.Policy)
	}
	a.LoadVizzini(path, r, policy)
	return append(warnings, vizziniWarnings(a.Vizzini)...)
}

// vizziniWarnings are the ways a loaded report may not describe the diff.
func vizziniWarnings(v *app.VizziniState) []string {
	var out []string
	if v.Stale {
		out = append(out, fmt.Sprintf(
			"Vizzini report looks stale: its diff score %d does not match this diff, so findings may point at the wrong lines",
			v.Report.BaseRiskScore))
	}
	if v.Unanchored > 0 {
		out = append(out, fmt.Sprintf("%d Vizzini finding(s) name files that are not in this diff", v.Unanchored))
	}
	return out
}
