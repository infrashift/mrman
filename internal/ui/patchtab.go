package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	render "github.com/infrashift/mrman/charmkit/cellrender"
	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/patch"
	"github.com/infrashift/mrman/internal/vcs/patchbackend"
)

// patchListResultMsg carries a finished directory scan back to the model.
type patchListResultMsg struct {
	Gen  uint64
	Dir  string
	Rows []app.PatchRow
	Err  error
}

// patchExtensions are the file names worth reading as review artifacts.
// Extension matching keeps the scan cheap; a file that looks like a patch but
// is named otherwise can still be opened with --patch.
var patchExtensions = map[string]bool{
	".patch": true,
	".diff":  true,
	".mbox":  true,
	".eml":   true,
}

// drainPatchTabLoad turns a pending scan request into a command, so walking
// the directory and parsing headers happens off the render loop.
func (m *Model) drainPatchTabLoad() tea.Cmd {
	req, ok := m.App.TakePatchTabLoad()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		rows, err := scanPatchDir(req.Dir)
		return patchListResultMsg{Gen: req.Gen, Dir: req.Dir, Rows: rows, Err: err}
	}
}

// scanPatchDir lists the artifacts in dir, one level deep.
//
// It does not recurse: a patch inbox is a flat directory, and walking a whole
// tree would turn opening a tab into an unbounded amount of work on a repo
// that happens to contain test fixtures.
func scanPatchDir(dir string) ([]app.PatchRow, error) {
	if dir == "" {
		return nil, fmt.Errorf("no directory to list")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var rows []app.PatchRow
	for _, entry := range entries {
		if entry.IsDir() || !patchExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		row := app.PatchRow{Path: path, Name: entry.Name()}
		if info, statErr := entry.Info(); statErr == nil {
			row.Modified = info.ModTime()
		}
		describePatch(&row, path)
		rows = append(rows, row)
	}
	return rows, nil
}

// describePatch fills in what the artifact says about itself. A file that
// cannot be read as a patch is still listed, with the reason — a file the
// reviewer expected to see and cannot open is worth showing.
func describePatch(row *app.PatchRow, path string) {
	series, err := patch.LoadFile(path, patch.Options{})
	if err != nil {
		row.Err = err.Error()
		return
	}
	if n := series.Len(); n > 1 {
		row.Series = fmt.Sprintf("%d patches", n)
	}
	for i := range series.Patches {
		if p := &series.Patches[i]; p.Subject != "" {
			row.Subject = p.Subject
			row.Author = p.Author
			return
		}
	}
}

// handlePatchListResult applies a finished scan.
func (m *Model) handlePatchListResult(msg patchListResultMsg) {
	if msg.Err != nil {
		m.App.SetPatchTabError(msg.Gen, msg.Err.Error())
		return
	}
	m.App.ApplyPatchTabRows(msg.Gen, msg.Rows)
}

// dispatchPatchTab handles a key while the Patches tab has focus.
func (m *Model) dispatchPatchTab(action input.Action) (quit bool, cmd tea.Cmd) {
	a := m.App
	switch action.Kind {
	case input.Quit:
		return true, nil
	case input.CommitSelectDown:
		a.PatchTabDown()
	case input.CommitSelectUp:
		a.PatchTabUp()
	case input.ConfirmCommitSelect, input.ToggleCommitSelect:
		m.openSelectedPatch()
	case input.BeginTargetFilter:
		a.BeginPatchTabFilter()
	case input.TogglePrReviewRequestedFilter:
		// On this tab the same key rescans, which is the analogous "get me
		// the current set" action.
		a.ReloadPatchTab()
		return false, m.drainPatchTabLoad()
	}
	return false, nil
}

// dispatchPatchTabFilter handles a key while the "/" prompt is open.
func (m *Model) dispatchPatchTabFilter(action input.Action) {
	a := m.App
	switch action.Kind {
	case input.ExitMode:
		a.CancelPatchTabFilter()
	case input.SubmitInput:
		a.CommitPatchTabFilter()
	case input.InsertChar:
		a.InsertPatchTabFilterChar(action.Ch)
	case input.DeleteChar:
		a.DeletePatchTabFilterChar()
	case input.DeleteWord:
		a.DeletePatchTabFilterWord()
	case input.ClearLine:
		a.ClearPatchTabFilter()
	}
}

// openSelectedPatch opens the artifact under the cursor as a review.
//
// This follows the Local tab's shape rather than the PR tab's, and is
// synchronous for the same reason that one is: reading a local file needs no
// forge round trip and no remote discussion fetch, so returning a command
// would only add a frame of latency to something already done.
func (m *Model) openSelectedPatch() {
	a := m.App
	row, ok := a.SelectedPatchRow()
	if !ok {
		return
	}
	if !row.Reviewable() {
		a.SetError("Cannot review " + row.Name + ": " + row.Err)
		return
	}

	backend, err := patchbackend.New(row.Path, patch.Options{})
	if err != nil {
		a.SetError("Cannot open " + row.Name + ": " + err.Error())
		return
	}
	files, err := backend.WorkingTreeDiff(m.Theme.Highlighter())
	if err != nil {
		a.SetError("Cannot read " + row.Name + ": " + err.Error())
		return
	}

	info := backend.Info()
	fresh := model.NewReviewSession(info.RootPath, info.HeadCommit, info.BranchName, model.SourcePatch)
	if m.session != nil {
		m.shutdown(a)
	}
	lifecycle, session := openSession(m.store, fresh, files)
	m.session = lifecycle

	// The backend changes with the target, unlike a Local-tab retarget where
	// the repository stays put.
	a.VCS = backend
	a.VcsInfo = info
	m.localCheckout = info.RootPath
	m.export.ReplyHeaders = replyHeadersFor(backend.Series())

	a.ApplyLoadedSelection(files, session, app.DiffSource{Kind: app.DiffSourcePatch})
	if commits, cErr := backend.RecentCommits(0, 0); cErr == nil && len(commits) > 0 {
		a.InstallReviewCommits(commits)
	}
	m.syncViewport()
	a.SetMessage("Reviewing " + row.Name)
	reportAnchorValidation(a)
}

// selectorPatchRows renders the Patches tab body.
func (m *Model) selectorPatchRows(height int) []string {
	a := m.App
	pt := a.PatchTab
	emitter := &render.Emitter{}
	t := m.Theme

	if pt != nil && pt.Err != "" {
		return []string{
			emitter.Line([]render.Span{{Text: "  " + pt.Err, Style: render.Style{Fg: t.MessageErrorFg}}}),
			emitter.Line([]render.Span{{Text: "  r retries", Style: render.Style{Fg: t.FgDim}}}),
		}
	}

	rows := a.PatchTabFilteredRows()
	if len(rows) == 0 {
		return []string{emitter.Line([]render.Span{
			{Text: "  " + m.patchTabEmptyMessage(), Style: render.Style{Fg: t.FgDim}},
		})}
	}

	lines := make([]string, 0, len(rows))
	for i, row := range rows {
		lines = append(lines, m.patchRow(emitter, row, pt != nil && i == pt.Cursor))
	}

	offset := 0
	if pt != nil {
		offset = pt.ScrollOffset
	}
	if offset >= len(lines) {
		offset = 0
	}
	end := min(offset+height, len(lines))
	return lines[offset:end]
}

// patchTabEmptyMessage explains an empty list in the terms that produced it.
func (m *Model) patchTabEmptyMessage() string {
	pt := m.App.PatchTab
	switch {
	case pt == nil || pt.Loading:
		return "Scanning for patches…"
	case pt.Filter != "":
		return "No patches match " + pt.Filter
	}
	return "No .patch, .diff or .mbox files in " + m.App.PatchTabDir()
}

// patchRow renders one artifact.
func (m *Model) patchRow(emitter *render.Emitter, row app.PatchRow, isCursor bool) string {
	t := m.Theme
	cursor := "  "
	nameStyle := render.Style{Fg: t.FgPrimary}
	if isCursor {
		cursor = "▸ "
		nameStyle = render.Style{Fg: t.FgPrimary, Bold: true}
	}

	spans := []render.Span{
		{Text: cursor, Style: render.Style{Fg: t.CursorColor}},
		{Text: render.TruncateOrPad(row.Name, 34) + " ", Style: nameStyle},
	}

	if !row.Reviewable() {
		spans = append(spans, render.Span{
			Text:  render.TruncateOrPad("unreadable: "+row.Err, 44),
			Style: render.Style{Fg: t.MessageErrorFg},
		})
		return emitter.Line(spans)
	}

	subject := row.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	spans = append(spans,
		render.Span{Text: render.TruncateOrPad(subject, 44) + " ", Style: render.Style{Fg: t.FgSecondary}},
		render.Span{Text: render.TruncateOrPad(row.Series, 11) + " ", Style: render.Style{Fg: t.FgDim}},
		render.Span{Text: render.TruncateOrPad(shortAuthor(row.Author), 14) + " ", Style: render.Style{Fg: t.FgDim}},
		render.Span{Text: relativeTime(row.Modified), Style: render.Style{Fg: t.FgDim}},
	)
	return emitter.Line(spans)
}

// shortAuthor reduces a From: line to its display name, or the address when
// there is none.
func shortAuthor(author string) string {
	if name, _, ok := strings.Cut(author, "<"); ok {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			return trimmed
		}
	}
	return strings.Trim(author, "<>")
}
