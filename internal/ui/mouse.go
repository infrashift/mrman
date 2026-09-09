// mouse.go implements tuicr's mouse support: wheel scrolling per pane,
// click-to-jump, and drag-to-select in the diff.
//
// Everything here depends on knowing where each pane landed on screen, which
// only View() knows. View records a paneRect per pane as it composes the
// frame; the handlers below hit-test against those. The rects describe the
// pane *body* — inside the borders — so a click on a border resolves to no
// pane rather than to the row next to it.
package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
)

// Wheel step sizes, matching tuicr's WHEEL_LINES / WHEEL_COLS.
const (
	wheelLines = 3
	wheelCols  = 4
)

// paneRect is a pane's body rectangle on screen, plus the scroll offset its
// first visible row corresponds to.
type paneRect struct {
	Panel      app.FocusedPanel
	X, Y, W, H int
	// Offset is the index of the content row drawn at Y, so a screen row
	// maps back to content as Offset + (row - Y).
	Offset int
}

// contains reports whether a screen position falls inside the pane body.
func (r paneRect) contains(x, y int) bool {
	return r.W > 0 && r.H > 0 && x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// rowAt maps a screen row to its content index.
func (r paneRect) rowAt(y int) int { return r.Offset + (y - r.Y) }

// layoutRects is the frame's hit-test map, rebuilt every View.
type layoutRects struct {
	panes []paneRect
}

func (l *layoutRects) reset() { l.panes = l.panes[:0] }

func (l *layoutRects) add(rect paneRect) { l.panes = append(l.panes, rect) }

// at returns the pane under a screen position.
func (l *layoutRects) at(x, y int) (paneRect, bool) {
	for _, rect := range l.panes {
		if rect.contains(x, y) {
			return rect, true
		}
	}
	return paneRect{}, false
}

// find returns a recorded pane by identity.
func (l *layoutRects) find(panel app.FocusedPanel) (paneRect, bool) {
	for _, rect := range l.panes {
		if rect.Panel == panel {
			return rect, true
		}
	}
	return paneRect{}, false
}

// handleMouse routes a mouse event. Mouse handling is skipped entirely when
// the config disables it, so the terminal's own selection keeps working.
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if !m.mouseEnabled {
		return nil
	}
	// Modal and full-screen states own the whole frame; clicking through
	// them would act on a diff the user cannot see.
	switch m.App.InputMode {
	case input.ModeHelp, input.ModeCommitSelect, input.ModeSubmitResolver,
		input.ModeSubmitConfirm, input.ModeSubmitActionPicker:
		return nil
	}

	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		m.handleWheel(msg)
	case tea.MouseClickMsg:
		return m.handleClick(msg)
	case tea.MouseMotionMsg:
		m.handleDrag(msg)
	case tea.MouseReleaseMsg:
		m.dragging = false
	}
	return nil
}

// handleWheel scrolls the pane under the pointer without moving its cursor,
// the way a wheel is expected to behave.
func (m *Model) handleWheel(msg tea.MouseWheelMsg) {
	mouse := msg.Mouse()
	rect, ok := m.layout.at(mouse.X, mouse.Y)
	if !ok {
		return
	}
	switch mouse.Button {
	case tea.MouseWheelUp:
		m.scrollPane(rect.Panel, -wheelLines)
	case tea.MouseWheelDown:
		m.scrollPane(rect.Panel, wheelLines)
	case tea.MouseWheelLeft:
		m.scrollPaneX(rect.Panel, -wheelCols)
	case tea.MouseWheelRight:
		m.scrollPaneX(rect.Panel, wheelCols)
	}
}

// scrollPane scrolls a pane vertically by lines, positive being downward.
func (m *Model) scrollPane(panel app.FocusedPanel, lines int) {
	a := m.App
	switch panel {
	case app.PanelDiff:
		if lines > 0 {
			a.ScrollViewDown(lines)
		} else {
			a.ScrollViewUp(-lines)
		}
	case app.PanelFileList:
		if lines > 0 {
			a.FileListDown(lines)
		} else {
			a.FileListUp(-lines)
		}
	case app.PanelComments:
		for i := 0; i < abs(lines); i++ {
			if lines > 0 {
				a.CommentNavDown()
			} else {
				a.CommentNavUp()
			}
		}
	case app.PanelCommitSelector:
		for i := 0; i < abs(lines); i++ {
			if lines > 0 {
				a.CommitSelectDown()
			} else {
				a.CommitSelectUp()
			}
		}
	}
}

// scrollPaneX scrolls a pane horizontally.
func (m *Model) scrollPaneX(panel app.FocusedPanel, cols int) {
	a := m.App
	switch panel {
	case app.PanelDiff:
		if cols > 0 {
			a.ScrollRight(cols)
		} else {
			a.ScrollLeft(-cols)
		}
	case app.PanelFileList:
		if cols > 0 {
			a.FileListState.ScrollRight(cols)
		} else {
			a.FileListState.ScrollLeft(-cols)
		}
	}
}

// handleClick focuses the clicked pane and acts on the row under the
// pointer: jump to a file, toggle a directory, place the diff cursor, or
// select a commit.
func (m *Model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	mouse := msg.Mouse()
	if mouse.Button != tea.MouseLeft {
		return nil
	}
	rect, ok := m.layout.at(mouse.X, mouse.Y)
	if !ok {
		return nil
	}
	a := m.App
	a.FocusPanel(rect.Panel)
	row := rect.rowAt(mouse.Y)

	switch rect.Panel {
	case app.PanelFileList:
		m.clickFileList(row)
	case app.PanelComments:
		if row >= 0 && row < len(a.BuildCommentNavigatorItems()) {
			a.CommentNav.Cursor = row
			a.CommentNavSelect()
		}
	case app.PanelCommitSelector:
		if row >= 0 && row < a.CommitSelectRowCount() {
			a.CommitListCursor = row
			a.ToggleCommitSelection()
			return m.reloadInlineSelection()
		}
	case app.PanelDiff:
		m.clickDiff(rect, row, mouse.X)
	}
	return nil
}

// clickFileList opens a file or toggles a directory, lazygit-style.
func (m *Model) clickFileList(row int) {
	a := m.App
	items := a.BuildVisibleItems()
	if row < 0 || row >= len(items) {
		return
	}
	a.FileListState.Select(row)
	item := items[row]
	if item.IsDir {
		a.ToggleDirectory(item.Path)
		return
	}
	a.JumpToFile(item.FileIdx)
	a.FocusPanel(app.PanelDiff)
}

// clickDiff places the cursor and starts a drag selection.
func (m *Model) clickDiff(rect paneRect, row, screenX int) {
	a := m.App
	if row < 0 || row >= len(a.LineAnnotations) {
		return
	}
	if a.LineAnnotations[row].IsDecoration() {
		return // spacing and file headers are not cursor targets
	}
	a.MoveCursorToAnnotation(row)

	// A press begins a drag: motion extends it, release ends it, and the
	// selection survives so y can copy it.
	m.dragging = true
	point := app.SelPoint{
		AnnotationIdx: row,
		CharOffset:    diffCharOffset(a, rect, row, screenX),
		Side:          model.LineSideNew,
	}
	a.VisualSelection = &app.VisualSelection{Anchor: point, Head: point}
}

// handleDrag extends a drag selection to the pointer.
func (m *Model) handleDrag(msg tea.MouseMotionMsg) {
	if !m.dragging || m.App.VisualSelection == nil {
		return
	}
	mouse := msg.Mouse()
	rect, ok := m.layout.find(app.PanelDiff)
	if !ok {
		return
	}
	a := m.App

	// Dragging past the top or bottom edge scrolls, the way every editor
	// does — otherwise a selection cannot exceed one screenful.
	switch {
	case mouse.Y < rect.Y:
		a.ScrollViewUp(1)
	case mouse.Y >= rect.Y+rect.H:
		a.ScrollViewDown(1)
	}

	row := min(max(rect.rowAt(clampInt(mouse.Y, rect.Y, rect.Y+rect.H-1)), 0),
		max(len(a.LineAnnotations)-1, 0))
	a.VisualSelection.Head = app.SelPoint{
		AnnotationIdx: row,
		CharOffset:    diffCharOffset(a, rect, row, mouse.X),
		Side:          model.LineSideNew,
	}
	a.DiffState.CursorLine = row
}

// diffCharOffset maps a screen column to a character offset in the diff
// row's content, so a drag selects characters rather than whole lines.
func diffCharOffset(a *app.App, rect paneRect, row, screenX int) int {
	gutter := app.UnifiedGutter(a.LinenoWidth())
	if a.DiffViewMode == app.ViewSideBySide {
		gutter = app.SbsLeftGutter(a.LinenoWidth())
	}
	offset := screenX - rect.X - gutter + a.DiffState.ScrollX
	if offset < 0 {
		offset = 0
	}
	total := a.AnnotationContentLen(row, model.LineSideNew)
	return min(offset, total)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func clampInt(v, lo, hi int) int { return min(max(v, lo), hi) }

// recordLeftColumnRects records the file tree and comment navigator
// rectangles, mirroring leftColumn's split arithmetic exactly. flWidth is
// the column's full width including borders; top is its first screen row.
func (m *Model) recordLeftColumnRects(flWidth, top, innerH int) {
	a := m.App
	navH := commentNavHeight(a, innerH)
	if navH == 0 {
		m.layout.add(paneRect{
			Panel: app.PanelFileList, X: 1, Y: top + 1,
			W: flWidth - 2, H: innerH, Offset: a.FileListState.Offset(),
		})
		return
	}
	treeBody := innerH - navH
	m.layout.add(paneRect{
		Panel: app.PanelFileList, X: 1, Y: top + 1,
		W: flWidth - 2, H: treeBody, Offset: a.FileListState.Offset(),
	})
	m.layout.add(paneRect{
		Panel: app.PanelComments, X: 1, Y: top + treeBody + 3,
		W: flWidth - 2, H: navH - 2, Offset: a.CommentNav.Offset,
	})
}

// yankMouseSelection copies a drag selection to the clipboard, reporting
// whether there was one. A selection made with the mouse never enters
// visual mode, so normal-mode y has to look for it explicitly.
func (m *Model) yankMouseSelection() bool {
	a := m.App
	if a.VisualSelection == nil {
		return false
	}
	text, chars, err := a.CopyVisualSelection()
	a.ExitVisualMode()
	if err != nil {
		a.SetError("Copy failed: " + err.Error())
		return true
	}
	if _, copyErr := copyText(text); copyErr != nil {
		a.SetError("Clipboard failed: " + copyErr.Error())
	} else {
		a.SetMessage(fmt.Sprintf("Yanked %d character(s)", chars))
	}
	return true
}
