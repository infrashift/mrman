package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/theme"
	"github.com/infrashift/mrman/internal/vcs"
)

// selectionBgSGR is the truecolor background code the theme paints selected
// text with, in the form the emitter writes it.
func selectionBgSGR(t *theme.Theme) string {
	r, g, b, _ := t.VisualSelectionBg().RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

// highlightedText is the text of an emitted row that carries bg. Each style
// change emits a complete SGR sequence, so a run is selected exactly when its
// opening sequence names the selection background.
func highlightedText(line, bg string) string {
	var out strings.Builder
	active := false
	for len(line) > 0 {
		start := strings.Index(line, "\x1b[")
		if start < 0 {
			if active {
				out.WriteString(line)
			}
			break
		}
		if active {
			out.WriteString(line[:start])
		}
		end := strings.IndexByte(line[start:], 'm')
		if end < 0 {
			break
		}
		active = strings.Contains(line[start:start+end+1], bg)
		line = line[start+end+1:]
	}
	return out.String()
}

// annotationWithContent is the index of the first annotation whose new-side
// content matches.
func annotationWithContent(t *testing.T, a *app.App, content string) int {
	t.Helper()
	for i := range a.LineAnnotations {
		if got, ok := a.ContentForSide(i, model.LineSideNew); ok && got == content {
			return i
		}
	}
	t.Fatalf("no annotation carries %q", content)
	return 0
}

// rowsOf returns the drawn-row indices the annotation occupied in the last
// render.
func rowsOf(a *app.App, annIdx int) []int {
	var rows []int
	for row, idx := range a.DiffState.RowAnnotations {
		if idx == annIdx {
			rows = append(rows, row)
		}
	}
	return rows
}

func TestSelectionHighlightsExactlyWhatYankCopies(t *testing.T) {
	m := testModel(t)
	a := m.App
	idx := annotationWithContent(t, a, "var A = 10")
	// "var A = 10" → runes 4..10 are "A = 10".
	a.VisualSelection = &app.VisualSelection{
		Anchor: app.SelPoint{AnnotationIdx: idx, CharOffset: 4, Side: model.LineSideNew},
		Head:   app.SelPoint{AnnotationIdx: idx, CharOffset: 10, Side: model.LineSideNew},
	}
	want, _, err := a.CopyVisualSelection()
	if err != nil {
		t.Fatalf("copy: %v", err)
	}

	lines := m.diffPane.BuildLines(a, 80, 20)
	rows := rowsOf(a, idx)
	if len(rows) != 1 {
		t.Fatalf("the fixture line must occupy one row, got %d", len(rows))
	}
	got := highlightedText(lines[rows[0]], selectionBgSGR(m.Theme))
	if got != want {
		t.Errorf("highlight %q does not match the yanked text %q", got, want)
	}
}

// TestMultiRowSelectionHighlightsPartialEnds covers the shape a real drag
// makes: a partial first row, whole rows in between, and a partial last row.
func TestMultiRowSelectionHighlightsPartialEnds(t *testing.T) {
	m := testModel(t)
	a := m.App
	first := annotationWithContent(t, a, "package x")
	last := annotationWithContent(t, a, "var A = 10")
	if last-first != 2 {
		t.Fatalf("fixture rows must be adjacent, got %d..%d", first, last)
	}

	// "package x"[4:] through "var A = 10"[:3].
	a.VisualSelection = &app.VisualSelection{
		Anchor: app.SelPoint{AnnotationIdx: first, CharOffset: 4, Side: model.LineSideNew},
		Head:   app.SelPoint{AnnotationIdx: last, CharOffset: 3, Side: model.LineSideNew},
	}
	lines := m.diffPane.BuildLines(a, 80, 20)
	bg := selectionBgSGR(m.Theme)

	want := []string{"age x", "var A = 1", "var"}
	var joined []string
	for i, idx := range []int{first, first + 1, last} {
		rows := rowsOf(a, idx)
		if len(rows) != 1 {
			t.Fatalf("row %d must occupy one screen row, got %d", idx, len(rows))
		}
		got := highlightedText(lines[rows[0]], bg)
		if got != want[i] {
			t.Errorf("row %d highlighted %q, want %q", idx, got, want[i])
		}
		joined = append(joined, got)
	}

	text, _, err := a.CopyVisualSelection()
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if strings.Join(joined, "\n") != text {
		t.Errorf("highlight %q does not reassemble the yank %q",
			strings.Join(joined, "\n"), text)
	}
}

func TestSelectionSurvivesLineWrapping(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("wrapped ", 40))
	m := wrapModel(t, long)
	a := m.App
	idx := annotationWithContent(t, a, long)
	a.VisualSelection = &app.VisualSelection{
		Anchor: app.SelPoint{AnnotationIdx: idx, CharOffset: 0, Side: model.LineSideNew},
		Head:   app.SelPoint{AnnotationIdx: idx, CharOffset: len([]rune(long)), Side: model.LineSideNew},
	}

	lines := m.diffPane.BuildLines(a, m.diffInnerWidth(), 30)
	rows := rowsOf(a, idx)
	if len(rows) < 2 {
		t.Fatalf("the fixture must wrap, got %d rows", len(rows))
	}
	bg := selectionBgSGR(m.Theme)
	var got strings.Builder
	for _, row := range rows {
		text := highlightedText(lines[row], bg)
		if strings.TrimSpace(text) == "" {
			t.Errorf("wrapped row %d carries no highlight", row)
		}
		got.WriteString(text)
	}
	// Every row but the last runs the highlight out to the pane edge, the way
	// a soft-wrapped selection does, so the padding at each break is expected;
	// what matters is that no word is left unhighlighted.
	if normalized := strings.Join(strings.Fields(got.String()), " "); normalized != long {
		t.Errorf("the highlight across wrapped rows reassembled %q, want the whole line",
			normalized)
	}
	// The last row stops at the text, rather than running on to the edge.
	if tail := highlightedText(lines[rows[len(rows)-1]], bg); tail != strings.TrimSpace(tail) {
		t.Errorf("the final wrapped row highlights padding past the text: %q", tail)
	}
}

func TestUnselectedRowsCarryNoHighlight(t *testing.T) {
	m := testModel(t)
	a := m.App
	idx := annotationWithContent(t, a, "var A = 10")
	a.VisualSelection = &app.VisualSelection{
		Anchor: app.SelPoint{AnnotationIdx: idx, CharOffset: 0, Side: model.LineSideNew},
		Head:   app.SelPoint{AnnotationIdx: idx, CharOffset: 10, Side: model.LineSideNew},
	}
	lines := m.diffPane.BuildLines(a, 80, 20)
	bg := selectionBgSGR(m.Theme)
	other := annotationWithContent(t, a, "package x")
	for _, row := range rowsOf(a, other) {
		if got := highlightedText(lines[row], bg); got != "" {
			t.Errorf("row outside the selection is highlighted: %q", got)
		}
	}
}

func TestColumnOffsetsAccountForWideRunes(t *testing.T) {
	const s = "a世b" // display widths 1, 2, 1

	for _, tc := range []struct{ off, col int }{{0, 0}, {1, 1}, {2, 3}, {3, 4}} {
		if got := runeOffsetToCol(s, tc.off); got != tc.col {
			t.Errorf("runeOffsetToCol(%d) = %d, want %d", tc.off, got, tc.col)
		}
	}
	// Column 2 lands in the second half of 世, which resolves to the offset
	// before it rather than splitting the rune.
	for _, tc := range []struct{ col, off int }{{0, 0}, {1, 1}, {2, 1}, {3, 2}, {4, 3}} {
		if got := colToRuneOffset(s, tc.col); got != tc.off {
			t.Errorf("colToRuneOffset(%d) = %d, want %d", tc.col, got, tc.off)
		}
	}
}

func TestClickWithoutDragLeavesYExportingTheReview(t *testing.T) {
	m := mouseModel(t)
	m.export.ToStdout = true
	addReviewComment(m, "something to export")
	diff := paneOf(t, m, app.PanelDiff)

	m.Update(tea.MouseClickMsg(mouse(diff.X+8, diff.Y+3, tea.MouseLeft)))
	m.Update(tea.MouseReleaseMsg(mouse(diff.X+8, diff.Y+3, tea.MouseLeft)))
	if m.App.VisualSelection != nil {
		t.Fatal("a click with no drag must not leave a selection behind")
	}

	pressRune(m, 'y')
	if m.PendingStdout == "" {
		t.Error("y after a plain click must still export the review")
	}
	if msg := m.App.Message; msg != nil && strings.Contains(msg.Content, "Yanked") {
		t.Errorf("a plain click must not turn y into a zero-character yank: %q", msg.Content)
	}
}

func TestClickingTheDiffWhileComposingKeepsTheComment(t *testing.T) {
	m := mouseModel(t)
	a := m.App
	a.DiffState.CursorLine = annotationWithContent(t, a, "var A = 10")
	pressRune(m, 'c')
	if a.InputMode != input.ModeComment {
		t.Fatalf("c must open the comment box, got %v", a.InputMode)
	}
	a.CommentBuffer = "half-written"

	diff := paneOf(t, m, app.PanelDiff)
	m.Update(tea.MouseClickMsg(mouse(diff.X+8, diff.Y+3, tea.MouseLeft)))

	if a.InputMode != input.ModeComment {
		t.Errorf("a click must not leave the comment box, got %v", a.InputMode)
	}
	if a.CommentBuffer != "half-written" {
		t.Errorf("a click must not discard the comment buffer, got %q", a.CommentBuffer)
	}
}

func TestVisualModeSelectionIsHighlighted(t *testing.T) {
	m := testModel(t)
	a := m.App
	a.DiffState.CursorLine = annotationWithContent(t, a, "var A = 10")
	a.EnterVisualModeAtCursor()

	want, _, err := a.CopyVisualSelection()
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	lines := m.diffPane.BuildLines(a, 80, 20)
	rows := rowsOf(a, a.DiffState.CursorLine)
	if len(rows) != 1 {
		t.Fatalf("the fixture line must occupy one row, got %d", len(rows))
	}
	if got := highlightedText(lines[rows[0]], selectionBgSGR(m.Theme)); got != want {
		t.Errorf("visual mode highlight %q does not match its yank %q", got, want)
	}
}

func TestSideBySideDragReadsTheColumnItLandedIn(t *testing.T) {
	m := mouseModel(t)
	a := m.App
	a.ToggleDiffViewMode()
	m.syncViewport()
	_ = m.View()

	paired := -1
	for i := range a.LineAnnotations {
		ann := &a.LineAnnotations[i]
		if ann.Kind == app.AnnSideBySideLine && ann.AddLineIdx != nil && ann.DelLineIdx != nil {
			paired = i
			break
		}
	}
	if paired < 0 {
		t.Fatal("side-by-side must pair the fixture's add and del lines")
	}

	rect := paneOf(t, m, app.PanelDiff)
	rows := rowsOf(a, paired)
	if len(rows) == 0 {
		t.Fatal("the paired row must be on screen")
	}
	y := rect.Y + rows[0]
	lw := a.LinenoWidth()

	for _, tc := range []struct {
		name string
		x    int
		want model.LineSide
	}{
		{"left column reads the old side", rect.X + app.SbsLeftGutter(lw) + 1, model.LineSideOld},
		{"right column reads the new side", rect.X + app.SbsRightGutter(lw, rect.W) + 1, model.LineSideNew},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.Update(tea.MouseClickMsg(mouse(tc.x, y, tea.MouseLeft)))
			if m.dragAnchor == nil {
				t.Fatal("a press in the diff must arm the drag")
			}
			if m.dragAnchor.Side != tc.want {
				t.Errorf("side = %v, want %v", m.dragAnchor.Side, tc.want)
			}
			m.Update(tea.MouseReleaseMsg(mouse(tc.x, y, tea.MouseLeft)))
		})
	}
}

func TestSideBySideDragCopiesTheColumnItLandedIn(t *testing.T) {
	m := mouseModel(t)
	a := m.App
	a.ToggleDiffViewMode()
	m.syncViewport()
	_ = m.View()

	idx := annotationWithContent(t, a, "var A = 10")
	rows := rowsOf(a, idx)
	if len(rows) == 0 {
		t.Fatal("the added line must be on screen")
	}
	rect := paneOf(t, m, app.PanelDiff)
	y := rect.Y + rows[0]
	right := rect.X + app.SbsRightGutter(a.LinenoWidth(), rect.W)

	m.Update(tea.MouseClickMsg(mouse(right, y, tea.MouseLeft)))
	m.Update(tea.MouseMotionMsg(mouse(right+len("var A = 10"), y, tea.MouseLeft)))
	m.Update(tea.MouseReleaseMsg(mouse(right+len("var A = 10"), y, tea.MouseLeft)))

	text, _, err := a.CopyVisualSelection()
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if text != "var A = 10" {
		t.Errorf("dragging the right column copied %q, want the new-side content", text)
	}
}

// wrapModel builds a model whose first diff line is long enough to wrap.
func wrapModel(t *testing.T, content string) *Model {
	t.Helper()
	path := "src/long.go"
	files := []model.DiffFile{{
		NewPath: &path,
		Status:  model.StatusModified,
		Hunks: []model.DiffHunk{{
			Header: "@@ -1,2 +1,2 @@",
			Lines: []model.DiffLine{
				{Origin: model.OriginAddition, Content: content, NewLineno: new(uint32(1))},
				{Origin: model.OriginContext, Content: "tail", OldLineno: new(uint32(1)), NewLineno: new(uint32(2))},
			},
			OldStart: 1, OldCount: 2, NewStart: 1, NewCount: 2,
		}},
	}}
	files[0].ContentHash = model.ComputeContentHash(files[0].Hunks)
	session := model.NewReviewSession("/repo", "abc", nil, model.SourceWorkingTree)
	backend := &stubBackend{info: vcs.Info{RootPath: "/repo", HeadCommit: "abc", Type: vcs.TypeGit}}
	a := app.NewApp(backend, backend.Info(), files, session, app.DiffSource{Kind: app.DiffSourceWorkingTree})
	a.DiffState.WrapLines = true

	m := NewModel(a, theme.TokyoNightStorm())
	m.width, m.height = 100, 40
	m.mouseEnabled = true
	m.syncViewport()
	_ = m.View()
	return m
}

func TestClickingBelowAWrappedLineLandsOnTheNextAnnotation(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("wrapped ", 40))
	m := wrapModel(t, long)
	a := m.App

	idx := annotationWithContent(t, a, long)
	rows := rowsOf(a, idx)
	if len(rows) < 2 {
		t.Fatalf("the fixture must wrap over several rows, got %d", len(rows))
	}

	rect := paneOf(t, m, app.PanelDiff)
	after := rows[len(rows)-1] + 1
	if after >= len(a.DiffState.RowAnnotations) {
		t.Fatal("the wrapped line must not be the last thing drawn")
	}
	want := a.DiffState.RowAnnotations[after]
	if want == idx {
		t.Fatal("the row after the wrapped line must belong to another annotation")
	}
	// The flat arithmetic the other panes use would have picked this instead.
	if naive := rect.Offset + after; naive == want {
		t.Skip("this fixture does not distinguish the wrap-aware lookup")
	}

	m.Update(tea.MouseClickMsg(mouse(rect.X+8, rect.Y+after, tea.MouseLeft)))
	if got := a.DiffState.CursorLine; got != want {
		t.Errorf("click landed on annotation %d, want %d", got, want)
	}
}

func TestSetMouseTogglesTrackingAndDropsTheSelection(t *testing.T) {
	m := mouseModel(t)
	diff := paneOf(t, m, app.PanelDiff)
	m.Update(tea.MouseClickMsg(mouse(diff.X+8, diff.Y+3, tea.MouseLeft)))
	m.Update(tea.MouseMotionMsg(mouse(diff.X+8, diff.Y+4, tea.MouseLeft)))
	if m.App.VisualSelection == nil {
		t.Fatal("the drag must have selected something to begin with")
	}

	m.runCommand(input.ParseCommand("set mouse!"))
	if m.mouseEnabled {
		t.Error(":set mouse! must turn tracking off")
	}
	if m.View().MouseMode != tea.MouseModeNone {
		t.Error("tracking off must stop requesting a mouse mode")
	}
	if m.App.VisualSelection != nil {
		t.Error("turning the mouse off must drop the selection it made")
	}
	if m.dragging || m.dragAnchor != nil {
		t.Error("turning the mouse off must abandon any drag in flight")
	}

	m.runCommand(input.ParseCommand("set mouse!"))
	if !m.mouseEnabled || m.View().MouseMode != tea.MouseModeCellMotion {
		t.Error(":set mouse! must turn tracking back on")
	}
}
