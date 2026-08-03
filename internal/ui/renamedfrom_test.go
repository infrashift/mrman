package ui

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// TestRenamedFrom covers the header text that makes a move visible at all:
// DisplayPath is the new path and OldPath is rendered nowhere else, so
// without this an R badge says a file moved without saying from where.
func TestRenamedFrom(t *testing.T) {
	str := func(s string) *string { return &s }
	tests := []struct {
		name string
		file model.DiffFile
		want string
	}{
		{"a rename names its old path", model.DiffFile{
			OldPath: str("src/foo.js"), NewPath: str("lib/foo.js"),
		}, "src/foo.js"},
		{"an unmoved file says nothing", model.DiffFile{
			OldPath: str("a.txt"), NewPath: str("a.txt"),
		}, ""},
		{"an addition has no old path", model.DiffFile{
			NewPath: str("new.txt"),
		}, ""},
		{"a deletion has no new path", model.DiffFile{
			OldPath: str("gone.txt"),
		}, ""},
		// Read from the paths, not the status, so a copy is described too.
		{"a copy is described as well", model.DiffFile{
			OldPath: str("orig.txt"), NewPath: str("copy.txt"), Status: model.StatusCopied,
		}, "orig.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renamedFrom(&tt.file); got != tt.want {
				t.Errorf("renamedFrom = %q, want %q", got, tt.want)
			}
		})
	}
}
