package app

import (
	"fmt"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// benchApp builds a review of files×hunks×lines diff lines with one comment
// on every second hunk: 200×5×50 is a 50k-line diff with 500 comments.
func benchApp(b *testing.B, files, hunks, lines int) *App {
	b.Helper()
	diff := make([]model.DiffFile, 0, files)
	for f := range files {
		hs := make([]model.DiffHunk, 0, hunks)
		for h := range hunks {
			hs = append(hs, makeHunk(uint32(1+h*(lines+20)), uint32(lines)))
		}
		diff = append(diff, makeFileWithHunks(fmt.Sprintf("pkg%03d/file%03d.go", f/10, f), hs))
	}
	a := buildAppWithFiles(diff, uint32(hunks*(lines+20)))
	side := model.LineSideNew
	for f := range diff {
		review := a.Session.File(diff[f].DisplayPath())
		for h := 0; h < hunks; h += 2 {
			review.AddLineComment(uint32(1+h*(lines+20)),
				model.NewComment("finding", model.CommentTypeFromID("issue"), &side))
		}
	}
	a.RebuildAnnotations()
	return a
}

func BenchmarkRebuildAnnotations(b *testing.B) {
	a := benchApp(b, 200, 5, 50)
	b.ResetTimer()
	for b.Loop() {
		a.RebuildAnnotations()
	}
}

// BenchmarkBuildCommentNavigatorItems measures the walk itself; the public
// method serves it from a cache between rebuilds.
func BenchmarkBuildCommentNavigatorItems(b *testing.B) {
	a := benchApp(b, 200, 5, 50)
	b.ResetTimer()
	for b.Loop() {
		_ = a.buildCommentNavigatorItems()
	}
}
