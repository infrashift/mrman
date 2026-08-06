package ui

// hunkcomment_test.go drives the hunk-comment keys as a user presses them, so
// the keymap → dispatch → app wiring is covered end to end and not just the
// app methods underneath it.

import (
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/theme"
	"github.com/infrashift/mrman/internal/vcs"
)

// moveToHunkHeader parks the cursor on the given hunk's @@ row.
func moveToHunkHeader(t *testing.T, m *Model, hunkIdx int) {
	t.Helper()
	idx, ok := m.App.HunkHeaderLine(0, hunkIdx)
	if !ok {
		t.Fatalf("no header for hunk %d", hunkIdx)
	}
	m.App.MoveCursorToAnnotation(idx)
}

// twoHunkModel is testModel with a second hunk, so ] has somewhere to go.
func twoHunkModel(t *testing.T) *Model {
	t.Helper()
	path := "src/x.go"
	hunk := func(start uint32) model.DiffHunk {
		return model.DiffHunk{
			Header: "@@ hunk @@",
			Lines: []model.DiffLine{
				{Origin: model.OriginContext, Content: "ctx", OldLineno: lineno(start), NewLineno: lineno(start)},
				{Origin: model.OriginDeletion, Content: "gone", OldLineno: lineno(start + 1)},
				{Origin: model.OriginAddition, Content: "new", NewLineno: lineno(start + 1)},
			},
			OldStart: start, OldCount: 2, NewStart: start, NewCount: 2,
		}
	}
	files := []model.DiffFile{{
		NewPath: &path,
		Status:  model.StatusModified,
		Hunks:   []model.DiffHunk{hunk(1), hunk(20)},
	}}
	files[0].ContentHash = model.ComputeContentHash(files[0].Hunks)
	session := model.NewReviewSession("/repo", "abc", nil, model.SourceWorkingTree)
	backend := &stubBackend{info: vcs.Info{RootPath: "/repo", HeadCommit: "abc", Type: vcs.TypeGit}}
	a := app.NewApp(backend, backend.Info(), files, session, app.DiffSource{Kind: app.DiffSourceWorkingTree})

	m := NewModel(a, theme.TokyoNightStorm())
	m.width, m.height = 100, 30
	m.syncViewport()
	return m
}

func TestPressingCOnAHunkHeaderAnchorsTheHunk(t *testing.T) {
	m := testModel(t)
	moveToHunkHeader(t, m, 0)

	pressRune(m, 'c')

	if m.App.InputMode != input.ModeComment {
		t.Fatal("c on a hunk header must open the comment box")
	}
	if m.App.CommentLineRange == nil {
		t.Fatal("a hunk comment must carry a range")
	}
	// The fixture hunk's new side is lines 1..3; its deleted old line 2 is
	// not part of the range.
	if got := m.App.CommentLineRange.Range; got != model.NewLineRange(1, 3) {
		t.Errorf("range: got %+v, want {1 3}", got)
	}
	if m.App.CommentLineRange.Side != model.LineSideNew {
		t.Errorf("side: got %q, want new", m.App.CommentLineRange.Side)
	}
}

// TestHunkCommentTitlesItselfAsARange checks the box header a user actually
// reads, which is the only feedback that c grabbed the whole hunk.
func TestHunkCommentTitlesItselfAsARange(t *testing.T) {
	m := testModel(t)
	moveToHunkHeader(t, m, 0)

	pressRune(m, 'c')

	if view := plainView(m); !contains(view, "L1-L3") {
		t.Errorf("comment box should title itself L1-L3, got:\n%s", view)
	}
}

func TestVisualHunkKeysExtendTheSelection(t *testing.T) {
	m := testModel(t)
	moveToHunkHeader(t, m, 0)

	pressRune(m, 'v')
	if m.App.InputMode != input.ModeVisualSelect {
		t.Fatal("v must enter visual mode")
	}
	pressRune(m, ']')
	pressRune(m, 'c')

	if m.App.CommentLineRange == nil {
		t.Fatal("expected a range anchor")
	}
	if got := m.App.CommentLineRange.Range; got != model.NewLineRange(1, 3) {
		t.Errorf("range: got %+v, want {1 3}", got)
	}
}

// TestVisualHunkKeysWalkAcrossHunks is the documented [ v ] ] c flow.
func TestVisualHunkKeysWalkAcrossHunks(t *testing.T) {
	m := twoHunkModel(t)
	moveToHunkHeader(t, m, 0)

	pressRune(m, 'v')
	pressRune(m, ']') // to the end of hunk 0
	pressRune(m, ']') // through hunk 1
	pressRune(m, 'c')

	if m.App.CommentLineRange == nil {
		t.Fatal("expected a range anchor")
	}
	if got := m.App.CommentLineRange.Range; got != model.NewLineRange(1, 21) {
		t.Errorf("range: got %+v, want {1 21} spanning both hunks", got)
	}
}

func TestVisualPrevHunkKeyShrinksTheSelection(t *testing.T) {
	m := twoHunkModel(t)
	moveToHunkHeader(t, m, 1)

	pressRune(m, 'v')
	pressRune(m, '[') // back through hunk 1's own start, then into hunk 0
	pressRune(m, '[')
	pressRune(m, 'c')

	if m.App.CommentLineRange == nil {
		t.Fatal("expected a range anchor")
	}
	if got := m.App.CommentLineRange.Range; got != model.NewLineRange(1, 20) {
		t.Errorf("range: got %+v, want {1 20}", got)
	}
}

// TestBracketKeysStillJumpInNormalMode guards the binding they share: outside
// visual mode ] must go on moving the cursor, not extending anything.
func TestBracketKeysStillJumpInNormalMode(t *testing.T) {
	m := twoHunkModel(t)
	moveToHunkHeader(t, m, 0)

	pressRune(m, ']')

	want, ok := m.App.HunkHeaderLine(0, 1)
	if !ok {
		t.Fatal("missing second hunk header")
	}
	if m.App.DiffState.CursorLine != want {
		t.Errorf("cursor: got %d, want %d (the next hunk header)",
			m.App.DiffState.CursorLine, want)
	}
	if m.App.VisualSelection != nil {
		t.Error("] in normal mode must not create a selection")
	}
}
