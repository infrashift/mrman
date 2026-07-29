package app

// commit_scoped_comment_test.go ports tuicr's
// src/app/tests/commit_scoped_comment_tests.rs: commit-scoped comment
// visibility and the commit-id stamp for new comments. The commit selector
// itself is M5; these tests drive the CommitSelectionRange/ReviewCommits
// stubs directly.

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/reviewcli"
	"github.com/infrashift/mrman/internal/vcs"
)

func buildAppWithReviewCommits(commits ...vcs.CommitInfo) *App {
	a := buildAppWithFiles(nil, 0)
	a.ReviewCommits = commits
	return a
}

func commit(id string) vcs.CommitInfo {
	return vcs.CommitInfo{
		ID:      id,
		ShortID: id,
		Summary: fmt.Sprintf("commit %s", id),
		Author:  "tester",
		Time:    time.Now().UTC(),
	}
}

func lineComment(content string, commitID *string) *model.Comment {
	side := model.LineSideNew
	c := model.NewComment(content, model.CommentTypeFromID("note"), &side)
	c.CommitID = commitID
	return c
}

func selection(start, end int) *model.IndexRange {
	r := model.IndexRange{start, end}
	return &r
}

func TestLegacyCommentWithNoCommitIDIsAlwaysVisible(t *testing.T) {
	a := buildAppWithReviewCommits(commit("aaa"), commit("bbb"))
	a.CommitSelectionRange = selection(0, 0) // only "aaa" selected
	if !a.CommentVisible(lineComment("old comment", nil)) {
		t.Fatal("legacy comment with CommitID nil must be visible regardless of selection")
	}
}

func TestCommentScopedToSelectedCommitIsVisible(t *testing.T) {
	a := buildAppWithReviewCommits(commit("aaa"), commit("bbb"))
	a.CommitSelectionRange = selection(0, 0) // only "aaa" selected
	if !a.CommentVisible(lineComment("on aaa", strPtr("aaa"))) {
		t.Fatal("comment scoped to the selected commit must be visible")
	}
}

func TestCommentScopedToUnselectedCommitIsHidden(t *testing.T) {
	a := buildAppWithReviewCommits(commit("aaa"), commit("bbb"))
	a.CommitSelectionRange = selection(0, 0) // only "aaa" selected
	if a.CommentVisible(lineComment("on bbb", strPtr("bbb"))) {
		t.Fatal("comment scoped to a commit outside the selection must be hidden")
	}
}

func TestFullRangeShowsAllCommitScopedComments(t *testing.T) {
	a := buildAppWithReviewCommits(commit("aaa"), commit("bbb"))
	a.CommitSelectionRange = selection(0, 1) // full range
	if !a.CommentVisible(lineComment("on aaa", strPtr("aaa"))) {
		t.Fatal("full range includes commit aaa")
	}
	if !a.CommentVisible(lineComment("on bbb", strPtr("bbb"))) {
		t.Fatal("full range includes commit bbb")
	}
}

func TestNoSelectorShowsAllComments(t *testing.T) {
	a := buildAppWithReviewCommits(commit("aaa"), commit("bbb"))
	// CommitSelectionRange is nil by default.
	if !a.CommentVisible(lineComment("on aaa", strPtr("aaa"))) {
		t.Fatal("no selector => all comments visible")
	}
	if !a.CommentVisible(lineComment("on bbb", strPtr("bbb"))) {
		t.Fatal("no selector => all comments visible")
	}
}

func TestCommitIDForNewCommentIsNilWithoutSelector(t *testing.T) {
	a := buildAppWithReviewCommits(commit("aaa"), commit("bbb"))
	if a.CommitIDForNewComment() != nil {
		t.Fatal("no selector => no commit-id stamp")
	}
}

func TestCommitIDForNewCommentIsNilForFullRange(t *testing.T) {
	a := buildAppWithReviewCommits(commit("aaa"), commit("bbb"))
	a.CommitSelectionRange = selection(0, 1)
	if a.CommitIDForNewComment() != nil {
		t.Fatal("full range => no commit-id stamp (comment is against cumulative diff)")
	}
}

func TestCommitIDForNewCommentIsNilForMultiCommitSubset(t *testing.T) {
	a := buildAppWithReviewCommits(commit("aaa"), commit("bbb"), commit("ccc"))
	a.CommitSelectionRange = selection(0, 1) // 2 of 3 commits
	if a.CommitIDForNewComment() != nil {
		t.Fatal("multi-commit subset => no commit-id stamp")
	}
}

func TestCommitIDForNewCommentIsShaForSingleCommit(t *testing.T) {
	a := buildAppWithReviewCommits(commit("aaa"), commit("bbb"))
	a.CommitSelectionRange = selection(1, 1) // only "bbb"
	got := a.CommitIDForNewComment()
	if got == nil || *got != "bbb" {
		t.Fatalf("single commit selection must stamp that commit's SHA, got %v", got)
	}
}

func TestSpecialCommitsNeverStampOrMatch(t *testing.T) {
	staged := commit(StagedSelectionID)
	a := buildAppWithReviewCommits(staged, commit("aaa"))
	a.CommitSelectionRange = selection(0, 0) // synthetic staged row only
	if a.CommitIDForNewComment() != nil {
		t.Fatal("synthetic staged row must not stamp a commit id")
	}
	if a.CommentVisible(lineComment("on aaa", strPtr("aaa"))) {
		t.Fatal("selection of only the synthetic row hides commit-scoped comments")
	}
}

func TestAddCommentToSessionStampsCommitIDWhenProvided(t *testing.T) {
	session := model.NewReviewSession("/repo", "head", strPtr("main"), model.SourceWorkingTree)
	session.AddFile("src/lib.rs", model.StatusModified, 0)

	comment, err := reviewcli.AddCommentToSession(session, reviewcli.AddCommentRequest{
		Target: reviewcli.CommentTarget{
			Kind: reviewcli.TargetLine, Path: "src/lib.rs",
			Line: 10, Side: model.LineSideNew,
		},
		Content:     "scoped note",
		CommentType: model.CommentTypeFromID("note"),
		Author:      "user",
		CommitID:    strPtr("abc123"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comment.CommitID == nil || *comment.CommitID != "abc123" {
		t.Fatal("AddCommentToSession must stamp the provided commit id")
	}
	stored := session.File("src/lib.rs").LineComments[10][0]
	if stored.CommitID == nil || *stored.CommitID != "abc123" {
		t.Fatal("stored comment must carry the commit id")
	}
}

func TestAddCommentToSessionLeavesCommitIDNilWhenNotProvided(t *testing.T) {
	session := model.NewReviewSession("/repo", "head", strPtr("main"), model.SourceWorkingTree)
	session.AddFile("src/lib.rs", model.StatusModified, 0)

	comment, err := reviewcli.AddCommentToSession(session, reviewcli.AddCommentRequest{
		Target: reviewcli.CommentTarget{
			Kind: reviewcli.TargetLine, Path: "src/lib.rs",
			Line: 10, Side: model.LineSideNew,
		},
		Content:     "unscoped note",
		CommentType: model.CommentTypeFromID("note"),
		Author:      "user",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comment.CommitID != nil {
		t.Fatal("commit id must stay nil when not provided")
	}
}

func TestLegacySessionJSONDeserializesCommentWithoutCommitID(t *testing.T) {
	raw := `{
		"id": "test-id",
		"content": "old comment",
		"comment_type": "note",
		"created_at": "2024-01-01T00:00:00Z",
		"line_context": null,
		"side": null,
		"line_range": null,
		"author": "user",
		"lifecycle_state": "local_draft",
		"remote_review_id": null,
		"remote_comment_id": null
	}`
	var comment model.Comment
	if err := json.Unmarshal([]byte(raw), &comment); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if comment.CommitID != nil {
		t.Fatal("legacy JSON without commit_id must deserialize as nil")
	}
}

func TestCommitScopedCommentHiddenFromAnnotations(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.ReviewCommits = []vcs.CommitInfo{commit("aaa"), commit("bbb")}
	review := a.Session.File("test.rs")
	review.AddLineComment(2, lineComment("on bbb", strPtr("bbb")))
	a.RebuildAnnotations()

	if !anyAnnotation(a, func(ann *AnnotatedLine) bool { return ann.Kind == AnnLineComment }) {
		t.Fatal("comment visible with no selector")
	}
	assertEq(t, a.TotalLines(), len(a.LineAnnotations), "heights stay in lockstep (no selector)")

	a.CommitSelectionRange = selection(0, 0) // only "aaa": comment hidden
	a.RebuildAnnotations()
	if anyAnnotation(a, func(ann *AnnotatedLine) bool { return ann.Kind == AnnLineComment }) {
		t.Fatal("comment scoped to unselected commit must not be annotated")
	}
	assertEq(t, a.TotalLines(), len(a.LineAnnotations), "heights stay in lockstep (filtered)")

	// The cursor on stale rows resolves to no comment under the filter.
	if _, ok := a.FindCommentAtCursor(); ok {
		t.Fatal("no comment expected at cursor")
	}
}
