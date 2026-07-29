package reviewcli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
)

// newTestStore builds a store and a saved session, returning the session
// file path for direct addressing.
func newTestStore(t *testing.T) (*persistence.Store, *model.ReviewSession, string) {
	t.Helper()
	store := &persistence.Store{ReviewsDir: t.TempDir()}
	branch := "main"
	session := model.NewReviewSession(t.TempDir(), "abc1234", &branch, model.SourceWorkingTree)
	session.AddFile("src/main.go", model.StatusModified, 42)
	path, err := store.SaveSession(session)
	if err != nil {
		t.Fatal(err)
	}
	return store, session, path
}

func TestListIntegration(t *testing.T) {
	store, session, _ := newTestStore(t)
	var buf bytes.Buffer
	if err := List(store, Options{Repo: session.RepoPath}, &buf); err != nil {
		t.Fatal(err)
	}
	var rows []SessionSummaryOutput
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("output not JSON: %v\n%s", err, buf.String())
	}
	if len(rows) != 1 || rows[0].Kind != "local" || rows[0].FileCount != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Error("output must end with newline")
	}

	// --all ignores the repo selector.
	buf.Reset()
	if err := List(store, Options{All: true, Repo: "/nowhere"}, &buf); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("all rows = %+v err=%v", rows, err)
	}
}

func TestAddAndCommentsIntegration(t *testing.T) {
	store, _, path := newTestStore(t)

	// Add a line comment addressed by direct path.
	var buf bytes.Buffer
	err := Add(store, Options{
		Session: path, Comment: "needs a nil check", Type: "issue",
		TargetFile: "src/main.go", Line: 10, Side: "new", Username: "Claude",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	var added CommentOutput
	if err := json.Unmarshal(buf.Bytes(), &added); err != nil {
		t.Fatalf("add output not JSON: %v\n%s", err, buf.String())
	}
	if added.Location != "src/main.go:10" || added.CommentType != "issue" ||
		added.LifecycleState != "local_draft" || added.Content != "needs a nil check" {
		t.Fatalf("added = %+v", added)
	}

	// Add a review comment via --input JSON.
	buf.Reset()
	err = Add(store, Options{Session: path, Input: `{"content": "overall LGTM"}`}, &buf)
	if err != nil {
		t.Fatal(err)
	}

	// Comments lists both, review scope first.
	buf.Reset()
	if err := Comments(store, Options{Session: path}, &buf); err != nil {
		t.Fatal(err)
	}
	var comments []CommentOutput
	if err := json.Unmarshal(buf.Bytes(), &comments); err != nil {
		t.Fatalf("comments output not JSON: %v\n%s", err, buf.String())
	}
	if len(comments) != 2 {
		t.Fatalf("got %d comments", len(comments))
	}
	if comments[0].Location != "review" || comments[1].Location != "src/main.go:10" {
		t.Fatalf("order wrong: %q, %q", comments[0].Location, comments[1].Location)
	}

	// The comment persisted to disk with the author stamped.
	reloaded, err := store.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	stored := reloaded.File("src/main.go").LineComments[10]
	if len(stored) != 1 || stored[0].Author != "Claude" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestAddResolvesSessionBySlug(t *testing.T) {
	store, session, _ := newTestStore(t)

	// Find the slug via the listing, then address the session by it.
	var buf bytes.Buffer
	if err := List(store, Options{Repo: session.RepoPath}, &buf); err != nil {
		t.Fatal(err)
	}
	var rows []SessionSummaryOutput
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	slugStr := rows[0].Slug

	buf.Reset()
	err := Add(store, Options{
		Session: slugStr, Repo: session.RepoPath, Comment: "by slug",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	var added CommentOutput
	if err := json.Unmarshal(buf.Bytes(), &added); err != nil {
		t.Fatal(err)
	}
	if added.Location != "review" || added.Content != "by slug" {
		t.Fatalf("added = %+v", added)
	}
}

func TestAddUnknownSessionErrors(t *testing.T) {
	store, session, _ := newTestStore(t)
	err := Add(store, Options{
		Session: "not-a-real-slug", Repo: session.RepoPath, Comment: "x",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("err = %v", err)
	}
}
