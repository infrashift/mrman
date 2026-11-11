package app

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/risk"
)

func TestRiskOffByDefault(t *testing.T) {
	a := buildAppWithFiles([]model.DiffFile{diffFile("internal/auth/a.go", "if x {")}, 0)
	if a.Risk != nil {
		t.Fatal("risk must stay nil until enabled")
	}
	if _, ok := a.FileRisk(0); ok {
		t.Error("FileRisk must report nothing while risk is off")
	}
	a.ApplyReloadedDiff([]model.DiffFile{diffFile("b.go", "x")})
	if a.Risk != nil {
		t.Error("a reload must not turn risk on")
	}
}

// The per-file scores are aligned with DiffFiles as displayed, which NewApp
// and every reload re-sort by directory. A score computed before the sort
// would land on the wrong row.
func TestRiskFollowsSortAndReload(t *testing.T) {
	// Given out of display order: the root file sorts first.
	a := buildAppWithFiles([]model.DiffFile{
		diffFile("internal/auth/login.go", "if a && b {"),
		diffFile("README.md", "hello"),
	}, 0)
	a.EnableRisk(risk.DefaultPolicy())

	assertAligned := func(when string) {
		t.Helper()
		if len(a.Risk.Assessment.Files) != len(a.DiffFiles) {
			t.Fatalf("%s: %d scores for %d files", when, len(a.Risk.Assessment.Files), len(a.DiffFiles))
		}
		for i := range a.DiffFiles {
			s, ok := a.FileRisk(i)
			if !ok || s.Path != a.DiffFiles[i].DisplayPath() {
				t.Errorf("%s: row %d is %q, score is for %q (counted %v)", when, i, a.DiffFiles[i].DisplayPath(), s.Path, ok)
			}
		}
	}
	assertAligned("open")
	if a.Risk.Assessment.Score == 0 || a.Risk.Policy.Source != risk.SourceDefault {
		t.Errorf("assessment %+v", a.Risk.Assessment)
	}
	auth := -1
	for i := range a.DiffFiles {
		if a.DiffFiles[i].DisplayPath() == "internal/auth/login.go" {
			auth = i
		}
	}
	if s, _ := a.FileRisk(auth); !s.Sensitive {
		t.Errorf("auth file not marked sensitive: %+v", s)
	}

	before := a.Risk.Assessment.Score
	a.ApplyReloadedDiff([]model.DiffFile{
		diffFile("z/docs.md", "a"),
		diffFile("internal/auth/login.go", "if a && b {", "if c || d {"),
		diffFile("a.go", "x"),
	})
	assertAligned("reload")
	if a.Risk.Assessment.Score <= before {
		t.Errorf("score %d after adding auth complexity, was %d", a.Risk.Assessment.Score, before)
	}

	a.ApplyInlineSelectionDiff([]model.DiffFile{diffFile("go.sum", "h1:x")})
	if s, ok := a.FileRisk(0); ok || s.Counted || a.Risk.Assessment.Score != 0 {
		t.Errorf("go.sum must not count: %+v score %d", s, a.Risk.Assessment.Score)
	}
	if _, ok := a.FileRisk(5); ok {
		t.Error("out-of-range index reported a score")
	}
}
