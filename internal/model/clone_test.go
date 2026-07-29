package model

import (
	"testing"

	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

func fullComment() *Comment {
	side := LineSideOld
	c := NewCommentWithRange("body", CommentTypeFromID("issue"), &side, NewLineRange(3, 9))
	newLine, oldLine := uint32(9), uint32(7)
	c.LineContext = &LineContext{NewLine: &newLine, OldLine: &oldLine, Content: "ctx"}
	rid, cid, commit := "rid", "cid", "sha"
	c.RemoteReviewID, c.RemoteCommentID, c.CommitID = &rid, &cid, &commit
	return c
}

func TestCommentCloneIsDeep(t *testing.T) {
	orig := fullComment()
	clone := orig.Clone()
	if !orig.Equal(clone) {
		t.Fatal("clone must equal original")
	}
	*clone.CommitID = "mutated"
	clone.LineRange.End = 99
	*clone.LineContext.NewLine = 1000
	if *orig.CommitID != "sha" || orig.LineRange.End != 9 || *orig.LineContext.NewLine != 9 {
		t.Fatal("clone must not alias original pointers")
	}
}

func TestCommentEqual(t *testing.T) {
	a, b := fullComment(), fullComment()
	b.ID = a.ID
	b.CreatedAt = a.CreatedAt
	if !a.Equal(b) {
		t.Fatal("identical comments must be equal")
	}
	b.Content = "changed"
	if a.Equal(b) {
		t.Fatal("content change must break equality")
	}
	b.Content = a.Content
	b.CommitID = nil
	if a.Equal(b) {
		t.Fatal("pointer nil-ness must break equality")
	}
	if a.Equal(nil) {
		t.Fatal("nil is never equal")
	}
	var nilA *Comment
	if !nilA.Equal(nil) {
		t.Fatal("nil equals nil")
	}
}

func TestFileReviewCloneIsDeep(t *testing.T) {
	f := NewFileReview("a.go", StatusModified, 42)
	f.Reviewed = true
	f.AddFileComment(fullComment())
	f.AddLineComment(9, fullComment())
	f.ReviewedHunks.Insert("hunk-content-v1:0000000000000001:0")

	clone := f.Clone()
	clone.FileComments[0].Content = "mutated"
	clone.LineComments[9][0].Content = "mutated"
	clone.ReviewedHunks.Insert("zzz")
	*clone.ContentHash = 0

	if f.FileComments[0].Content == "mutated" || f.LineComments[9][0].Content == "mutated" {
		t.Fatal("comments must be deep-copied")
	}
	if f.ReviewedHunks.Contains("zzz") || *f.ContentHash != 42 {
		t.Fatal("hunks/hash must be deep-copied")
	}
}

func TestReviewSessionCloneIsDeep(t *testing.T) {
	branch, notes := "main", "notes"
	s := NewReviewSession("/repo", "abc", &branch, SourcePullRequest)
	s.SessionNotes = &notes
	s.CommitRange = []string{"new", "old"}
	s.PrSessionKey = &forgetypes.PrSessionKey{
		Repository: forgetypes.Repository{Kind: forgetypes.KindGitHub, Host: "github.com", Owner: "o", Name: "r"},
		Number:     1, HeadSHA: "abc",
	}
	s.CommitSelectionRange = &IndexRange{0, 1}
	s.ReviewComments = append(s.ReviewComments, fullComment())
	s.AddFile("a.go", StatusModified, 1)

	clone := s.Clone()
	*clone.BranchName = "other"
	*clone.SessionNotes = "other"
	clone.CommitRange[0] = "other"
	clone.PrSessionKey.Number = 99
	clone.CommitSelectionRange[1] = 42
	clone.ReviewComments[0].Content = "mutated"
	clone.Files["a.go"].Reviewed = true

	if *s.BranchName != "main" || *s.SessionNotes != "notes" || s.CommitRange[0] != "new" {
		t.Fatal("scalars must be deep-copied")
	}
	if s.PrSessionKey.Number != 1 || s.CommitSelectionRange[1] != 1 {
		t.Fatal("key/range must be deep-copied")
	}
	if s.ReviewComments[0].Content == "mutated" || s.Files["a.go"].Reviewed {
		t.Fatal("comments/files must be deep-copied")
	}
}

func TestLineContextRoundTrip(t *testing.T) {
	orig := fullComment()
	clone := orig.Clone()
	if !ptrEq(orig.LineContext.NewLine, clone.LineContext.NewLine) {
		t.Fatal("line context values must match")
	}
}

func TestReviewedCountAndFileHelpers(t *testing.T) {
	s := testSession()
	s.AddFile("a.go", StatusModified, 1)
	s.AddFile("b.go", StatusModified, 1)
	s.File("a.go").Reviewed = true
	if s.ReviewedCount() != 1 {
		t.Fatalf("ReviewedCount = %d", s.ReviewedCount())
	}
	if s.File("missing.go") != nil {
		t.Fatal("missing file must be nil")
	}
	if s.IsFileReviewed("missing.go") || s.IsHunkReviewed("missing.go", "k") {
		t.Fatal("missing file is never reviewed")
	}
	f := s.File("a.go")
	if !f.ToggleHunkReviewed("key") || f.ToggleHunkReviewed("key") {
		t.Fatal("toggle must flip membership")
	}
	if f.CommentCount() != 0 {
		t.Fatal("no comments yet")
	}
}
