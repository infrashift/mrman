package diffparser

import (
	"testing"

	"github.com/infrashift/mrman/internal/vcs"
)

func TestTooLargeMarkerMarksTheFile(t *testing.T) {
	text := "diff --git a/big.json b/big.json\n" + vcs.TooLargeDiffMarker + "\n" +
		"diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n"
	files, err := Parse(text, GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	if !files[0].IsTooLarge || files[0].DisplayPath() != "big.json" || len(files[0].Hunks) != 0 {
		t.Errorf("first file = %+v, want big.json marked too large", files[0])
	}
	if files[1].IsTooLarge || len(files[1].Hunks) != 1 {
		t.Errorf("the next file must parse normally: %+v", files[1])
	}
}
