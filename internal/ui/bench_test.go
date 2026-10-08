package ui

import (
	"fmt"
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/theme"
	"github.com/infrashift/mrman/internal/vcs"
)

// benchModel is a 160×50 model over a diff of files×hunks×lines diff lines,
// with a comment on every second hunk.
func benchModel(b *testing.B, files, hunks, lines int) *Model {
	b.Helper()
	diff := make([]model.DiffFile, 0, files)
	for f := range files {
		path := fmt.Sprintf("pkg%03d/file%03d.go", f/10, f)
		hs := make([]model.DiffHunk, 0, hunks)
		for h := range hunks {
			start := uint32(1 + h*(lines+20))
			dl := make([]model.DiffLine, 0, lines)
			for i := range uint32(lines) {
				dl = append(dl, model.DiffLine{Origin: model.OriginContext, Content: fmt.Sprintf("line %d", start+i),
					OldLineno: new(start + i), NewLineno: new(start + i)})
			}
			hs = append(hs, model.DiffHunk{Lines: dl, OldStart: start, OldCount: uint32(lines),
				NewStart: start, NewCount: uint32(lines)})
		}
		f := model.DiffFile{NewPath: &path, Status: model.StatusModified, Hunks: hs}
		f.ContentHash = model.ComputeContentHash(hs)
		diff = append(diff, f)
	}
	session := model.NewReviewSession("/repo", "abc", nil, model.SourceWorkingTree)
	backend := &stubBackend{info: vcs.Info{RootPath: "/repo", HeadCommit: "abc", Type: vcs.TypeGit}}
	a := app.NewApp(backend, backend.Info(), diff, session, app.DiffSource{Kind: app.DiffSourceWorkingTree})
	side := model.LineSideNew
	for f := range diff {
		review := a.Session.File(diff[f].DisplayPath())
		for h := 0; h < hunks; h += 2 {
			review.AddLineComment(uint32(1+h*(lines+20)),
				model.NewComment("finding", model.CommentTypeFromID("issue"), &side))
		}
	}
	a.RebuildAnnotations()
	m := NewModel(a, theme.TokyoNightStorm())
	m.width, m.height = 160, 50
	m.syncViewport()
	return m
}

// BenchmarkIdleFrame is one redraw with nothing changed, which the 100 ms
// tick causes ten times a second.
func BenchmarkIdleFrame(b *testing.B) {
	m := benchModel(b, 200, 5, 50)
	b.ResetTimer()
	for b.Loop() {
		_ = m.View()
	}
}
