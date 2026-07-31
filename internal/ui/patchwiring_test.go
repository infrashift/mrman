package ui

import (
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/output"
	"github.com/infrashift/mrman/internal/vcs"
)

// TestScopeKindMirrorsDiffSourceKind guards the one coupling in this codebase
// that fails silently and invisibly.
//
// internal/ui/export.go converts app.DiffSourceKind to output.ScopeKind with a
// plain int cast, so the two enums must stay index-parallel. Insert a variant
// into either and every exported review is relabelled with someone else's
// scope, with nothing to notice it — no compile error, no test failure
// anywhere else, just a wrong word in a document handed to a colleague.
func TestScopeKindMirrorsDiffSourceKind(t *testing.T) {
	pairs := []struct {
		diff  app.DiffSourceKind
		scope output.ScopeKind
		name  string
	}{
		{app.DiffSourceWorkingTree, output.ScopeWorkingTree, "working tree"},
		{app.DiffSourceStaged, output.ScopeStaged, "staged"},
		{app.DiffSourceUnstaged, output.ScopeUnstaged, "unstaged"},
		{app.DiffSourceStagedAndUnstaged, output.ScopeStagedAndUnstaged, "staged + unstaged"},
		{app.DiffSourceCommitRange, output.ScopeCommitRange, "commit range"},
		{app.DiffSourceStagedUnstagedAndCommits, output.ScopeStagedUnstagedAndCommits, "combined"},
		{app.DiffSourcePullRequest, output.ScopePullRequest, "pull request"},
		{app.DiffSourcePatch, output.ScopePatch, "patch"},
	}
	for _, p := range pairs {
		if output.ScopeKind(int(p.diff)) != p.scope {
			t.Errorf("%s: DiffSourceKind %d casts to ScopeKind %d, want %d",
				p.name, p.diff, output.ScopeKind(int(p.diff)), p.scope)
		}
	}
	// And the last pair must really be last, so a future variant is appended
	// rather than inserted.
	if int(app.DiffSourcePatch) != len(pairs)-1 {
		t.Errorf("DiffSourcePatch = %d, want %d — new kinds must be appended",
			app.DiffSourcePatch, len(pairs)-1)
	}
}

func TestPatchScopeLabel(t *testing.T) {
	if got := output.ScopePatch.Label(); got != "patch file" {
		t.Errorf("Label() = %q, want %q", got, "patch file")
	}
	// A patch has no commit ids, so the generic scope line has nothing to
	// say; the artifact's name reaches the export through DiffSourceLabel.
	if got := output.ScopePatch.ScopeLine(nil); got != "" {
		t.Errorf("ScopeLine() = %q, want empty", got)
	}
}

func TestPatchSessionSourceMapping(t *testing.T) {
	got := sessionSource(app.DiffSource{Kind: app.DiffSourcePatch})
	if got != model.SourcePatch {
		t.Errorf("sessionSource = %q, want %q", got, model.SourcePatch)
	}
}

// TestPatchHeaderChunk covers what the reviewer reads at the top of the
// screen: a series should say how many patches it holds, since that is the
// thing they will be walking through.
func TestPatchHeaderChunk(t *testing.T) {
	a := testApp(t)
	a.DiffSource = app.DiffSource{Kind: app.DiffSourcePatch}

	if got := headerSourceChunk(a); got != "patch" {
		t.Errorf("single patch header = %q, want %q", got, "patch")
	}

	a.InstallReviewCommits([]vcs.CommitInfo{
		{ID: "a", Summary: "one"}, {ID: "b", Summary: "two"}, {ID: "c", Summary: "three"},
	})
	if got := headerSourceChunk(a); !strings.Contains(got, "3 patches") {
		t.Errorf("series header = %q, want it to name the patch count", got)
	}
}
