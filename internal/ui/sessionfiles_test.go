package ui

import (
	"bytes"
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/reviewcli"
)

// TestAgentCanCommentOnAFreshlyOpenedPrSession: `mrman pr X --auto=...`
// tells an agent it may now review, but the session was saved before its
// diff files were registered, so every `review add` with a file was refused
// ("file ... is not part of this review session") until something else made
// the TUI save. Seen live on github.com/infrashift/scratch#2.
func TestAgentCanCommentOnAFreshlyOpenedPrSession(t *testing.T) {
	store := &persistence.Store{ReviewsDir: t.TempDir()}
	files := testApp(t).DiffFiles
	details := &forge.PullRequestDetails{
		PullRequestSummary: forge.PullRequestSummary{Repository: testRepo(), Number: 7, State: "OPEN"},
		HeadSHA:            "headsha", BaseSHA: "basesha",
	}
	lc, _ := openPrSession(store, app.NewPrSession(details), files, []string{"comment"})

	err := reviewcli.Add(store, reviewcli.Options{
		Session: lc.path, Comment: "agent finding", Type: "issue",
		TargetFile: "src/x.go", Line: 2,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("review add on a file in the diff: %v", err)
	}
}

// TestLocalSessionIsSavedWithItsFiles is the same property for a local
// review, which agents also comment on.
func TestLocalSessionIsSavedWithItsFiles(t *testing.T) {
	store := &persistence.Store{ReviewsDir: t.TempDir()}
	files := testApp(t).DiffFiles
	fresh := model.NewReviewSession("/repo", "abc", nil, model.SourceWorkingTree)
	lc, _ := openSession(store, fresh, files)

	persisted, err := store.LoadSession(lc.path)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.File("src/x.go") == nil {
		t.Fatal("the session was saved without its diff files")
	}
}
