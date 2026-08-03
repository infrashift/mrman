package ui

import (
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/output"
)

func TestDiffPathsSessionSourceMapping(t *testing.T) {
	got := sessionSource(app.DiffSource{Kind: app.DiffSourceDiffPaths})
	if got != model.SourceDiffPaths {
		t.Errorf("sessionSource = %q, want %q", got, model.SourceDiffPaths)
	}
}

func TestDiffPathsScopeLabel(t *testing.T) {
	if got := output.ScopeDiffPaths.Label(); got != "two paths" {
		t.Errorf("Label() = %q, want %q", got, "two paths")
	}
	// A comparison has no commit ids, so the generic scope line has nothing
	// to say; the pair reaches the export through DiffSourceLabel.
	if got := output.ScopeDiffPaths.ScopeLine(nil); got != "" {
		t.Errorf("ScopeLine() = %q, want empty", got)
	}
}

// TestDiffHeaderChunk covers the only place the reviewer can learn which of
// the two paths they are reading: OldPath is never rendered, and every
// display path is the new side.
func TestDiffHeaderChunk(t *testing.T) {
	a := testApp(t)
	a.DiffSource = app.DiffSource{Kind: app.DiffSourceDiffPaths}

	// Without a label there is still something honest to say.
	if got := headerSourceChunk(a); got != "two paths" {
		t.Errorf("unlabelled header = %q, want %q", got, "two paths")
	}

	a.ComparisonLabel = "old/a.txt → new/a.txt"
	if got := headerSourceChunk(a); got != a.ComparisonLabel {
		t.Errorf("header = %q, want the comparison label %q", got, a.ComparisonLabel)
	}
}

// TestDiffScopeLabelPrefersTheComparison guards the export wording: "two
// paths" is true but useless in a document handed to someone else, while
// the pair itself says exactly what was reviewed.
func TestDiffScopeLabelPrefersTheComparison(t *testing.T) {
	a := testApp(t)
	a.DiffSource = app.DiffSource{Kind: app.DiffSourceDiffPaths}
	scope := output.ScopeKind(int(a.DiffSource.Kind))

	if got := scopeLabel(a, scope); got != "two paths" {
		t.Errorf("unlabelled scope = %q, want the generic kind name", got)
	}

	a.ComparisonLabel = "staging/config.yaml → prod/config.yaml"
	if got := scopeLabel(a, scope); got != a.ComparisonLabel {
		t.Errorf("scope = %q, want %q", got, a.ComparisonLabel)
	}

	// Every other source keeps its kind name, label or not.
	a.DiffSource = app.DiffSource{Kind: app.DiffSourceWorkingTree}
	if got := scopeLabel(a, output.ScopeWorkingTree); got != "working tree changes" {
		t.Errorf("working-tree scope = %q, want it unaffected", got)
	}
}
