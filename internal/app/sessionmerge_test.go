package app

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

const someHash uint64 = 0xabc

const mainFile = "src/main.go"

func mergeTestSession() *model.ReviewSession {
	branch := "main"
	s := model.NewReviewSession("/repo", "abc1234", &branch, model.SourceWorkingTree)
	s.AddFile(mainFile, model.StatusModified, someHash)
	return s
}

func namedComment(id, content string) *model.Comment {
	c := model.NewComment(content, model.CommentTypeFromID("note"), nil)
	c.ID = id
	return c
}

func pushFileComment(s *model.ReviewSession, id, content string) {
	s.File(mainFile).FileComments = append(s.File(mainFile).FileComments, namedComment(id, content))
}

func fileCommentIDs(s *model.ReviewSession) []string {
	var ids []string
	for _, c := range s.File(mainFile).FileComments {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestMergeExternalCommentWithoutLosingLocal(t *testing.T) {
	base := mergeTestSession()
	current := base.Clone()
	latest := base.Clone()

	pushFileComment(current, "local", "from tui")
	pushFileComment(latest, "external", "from cli")

	changed := MergeExternalSessionChanges(current, base, latest)
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	ids := fileCommentIDs(current)
	if len(ids) != 2 || ids[0] != "local" || ids[1] != "external" {
		t.Fatalf("ids = %v", ids)
	}
}

func TestMergeDoesNotResurrectLocallyDeletedComment(t *testing.T) {
	base := mergeTestSession()
	pushFileComment(base, "deleted", "old")
	current := base.Clone()
	current.File(mainFile).FileComments = nil
	latest := base.Clone()

	changed := MergeExternalSessionChanges(current, base, latest)
	if changed != 0 {
		t.Fatalf("changed = %d, want 0", changed)
	}
	if len(fileCommentIDs(current)) != 0 {
		t.Fatal("deleted comment must stay deleted")
	}
}

func TestMergeAppliesExternalEditWhenUnchangedLocally(t *testing.T) {
	base := mergeTestSession()
	pushFileComment(base, "same", "old")
	current := base.Clone()
	latest := base.Clone()
	latest.File(mainFile).FileComments[0].Content = "new"

	changed := MergeExternalSessionChanges(current, base, latest)
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if got := current.File(mainFile).FileComments[0].Content; got != "new" {
		t.Fatalf("content = %q, want new", got)
	}
}

func TestMergeLocalEditWinsOverExternalEdit(t *testing.T) {
	base := mergeTestSession()
	pushFileComment(base, "contested", "base")
	current := base.Clone()
	current.File(mainFile).FileComments[0].Content = "local edit"
	latest := base.Clone()
	latest.File(mainFile).FileComments[0].Content = "external edit"

	changed := MergeExternalSessionChanges(current, base, latest)
	if changed != 0 {
		t.Fatalf("changed = %d, want 0", changed)
	}
	if got := current.File(mainFile).FileComments[0].Content; got != "local edit" {
		t.Fatalf("content = %q, local edit must win", got)
	}
}

func TestMergeLocalDeletionWinsOverExternalEdit(t *testing.T) {
	base := mergeTestSession()
	pushFileComment(base, "contested", "base")
	current := base.Clone()
	current.File(mainFile).FileComments = nil
	latest := base.Clone()
	latest.File(mainFile).FileComments[0].Content = "external edit"

	changed := MergeExternalSessionChanges(current, base, latest)
	if changed != 0 || len(fileCommentIDs(current)) != 0 {
		t.Fatalf("changed=%d ids=%v; local deletion must win", changed, fileCommentIDs(current))
	}
}

func TestMergeAppliesExternalDeleteWhenUnmodifiedLocally(t *testing.T) {
	base := mergeTestSession()
	pushFileComment(base, "gone", "delete me")
	current := base.Clone()
	latest := base.Clone()
	latest.File(mainFile).FileComments = nil

	changed := MergeExternalSessionChanges(current, base, latest)
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if len(fileCommentIDs(current)) != 0 {
		t.Fatal("external delete must apply to unmodified comment")
	}
}

func TestMergeKeepsExternalDeleteAwayFromLocalEdit(t *testing.T) {
	base := mergeTestSession()
	pushFileComment(base, "kept", "base")
	current := base.Clone()
	current.File(mainFile).FileComments[0].Content = "local edit"
	latest := base.Clone()
	latest.File(mainFile).FileComments = nil

	changed := MergeExternalSessionChanges(current, base, latest)
	if changed != 0 || len(fileCommentIDs(current)) != 1 {
		t.Fatal("locally edited comment must survive external delete")
	}
}

func TestMergeAdoptsNewFilesWholesale(t *testing.T) {
	base := mergeTestSession()
	current := base.Clone()
	latest := base.Clone()
	latest.AddFile("src/new.go", model.StatusAdded, someHash)
	latest.File("src/new.go").AddFileComment(namedComment("n1", "external file"))
	latest.File("src/new.go").AddLineComment(4, namedComment("n2", "external line"))

	changed := MergeExternalSessionChanges(current, base, latest)
	if changed != 2 {
		t.Fatalf("changed = %d, want 2 (comment count of adopted file)", changed)
	}
	adopted := current.File("src/new.go")
	if adopted == nil || adopted.CommentCount() != 2 {
		t.Fatal("new file must be adopted with its comments")
	}
	// Deep copy: mutating latest must not affect current.
	latest.File("src/new.go").FileComments[0].Content = "mutated"
	if adopted.FileComments[0].Content == "mutated" {
		t.Fatal("adopted file must be deep-copied")
	}
}

func TestMergeReviewedFlagFollowsLatestOnlyWhenLocalAgreesWithBase(t *testing.T) {
	// Local untouched: external reviewed flip is adopted.
	base := mergeTestSession()
	current := base.Clone()
	latest := base.Clone()
	latest.File(mainFile).Reviewed = true
	if changed := MergeExternalSessionChanges(current, base, latest); changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if !current.File(mainFile).Reviewed {
		t.Fatal("external reviewed flip must apply")
	}

	// Local diverged from base: external flip is ignored.
	base2 := mergeTestSession()
	current2 := base2.Clone()
	current2.File(mainFile).Reviewed = true // local change
	latest2 := base2.Clone()                // external kept it unreviewed
	if changed := MergeExternalSessionChanges(current2, base2, latest2); changed != 0 {
		t.Fatalf("changed = %d, want 0", changed)
	}
	if !current2.File(mainFile).Reviewed {
		t.Fatal("local reviewed change must win")
	}
}

func TestMergeExternalLineAndReviewComments(t *testing.T) {
	base := mergeTestSession()
	current := base.Clone()
	latest := base.Clone()
	latest.ReviewComments = append(latest.ReviewComments, namedComment("r1", "review scope"))
	latest.File(mainFile).AddLineComment(12, namedComment("l1", "line scope"))

	changed := MergeExternalSessionChanges(current, base, latest)
	if changed != 2 {
		t.Fatalf("changed = %d, want 2", changed)
	}
	if len(current.ReviewComments) != 1 || current.ReviewComments[0].ID != "r1" {
		t.Fatal("review comment must merge")
	}
	if got := current.File(mainFile).LineComments[12]; len(got) != 1 || got[0].ID != "l1" {
		t.Fatal("line comment must merge")
	}
}

func TestSessionCommentCount(t *testing.T) {
	s := mergeTestSession()
	if SessionCommentCount(s) != 0 {
		t.Fatal("empty session must count 0")
	}
	s.ReviewComments = append(s.ReviewComments, namedComment("a", "x"))
	pushFileComment(s, "b", "y")
	s.File(mainFile).AddLineComment(3, namedComment("c", "z"))
	if got := SessionCommentCount(s); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}
}

// TestMergeAppliesExternalLineAndReviewDeletes covers the deletion branches
// the file-comment test does not reach: a line keeps its other comments
// when one goes, a line whose last comment goes disappears from the map,
// and a review-level comment goes too.
func TestMergeAppliesExternalLineAndReviewDeletes(t *testing.T) {
	base := mergeTestSession()
	review := base.File(mainFile)
	review.AddLineComment(3, namedComment("keep", "stays"))
	review.AddLineComment(3, namedComment("drop-one", "goes"))
	review.AddLineComment(9, namedComment("drop-last", "goes, and the line with it"))
	base.ReviewComments = append(base.ReviewComments, namedComment("drop-review", "goes"))

	current := base.Clone()
	latest := base.Clone()
	latestReview := latest.File(mainFile)
	latestReview.LineComments[3] = latestReview.LineComments[3][:1] // "keep"
	delete(latestReview.LineComments, 9)
	latest.ReviewComments = nil

	if changed := MergeExternalSessionChanges(current, base, latest); changed != 3 {
		t.Fatalf("changed = %d, want 3", changed)
	}
	got := current.File(mainFile).LineComments
	if len(got[3]) != 1 || got[3][0].ID != "keep" {
		t.Errorf("line 3 = %+v, want only the kept comment", got[3])
	}
	if _, ok := got[9]; ok {
		t.Error("a line whose last comment was deleted must leave the map")
	}
	if len(current.ReviewComments) != 0 {
		t.Errorf("review comments = %+v, want none", current.ReviewComments)
	}
}
