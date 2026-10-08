package ui

import (
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
)

// branchSession is a working-tree session on a named branch at head. The
// branch matters: a detached HEAD has no anchor stable across a rewrite, so it
// deliberately does not carry forward.
func branchSession(t *testing.T, head string) *model.ReviewSession {
	t.Helper()
	branch := "main"
	return model.NewReviewSession(t.TempDir(), head, &branch, model.SourceWorkingTree)
}

// TestOpenSessionCarriesReviewAcrossAmend is the end-to-end behaviour this
// whole line of work is for: comment on the working tree, amend the commit,
// reopen, and the review is still there.
//
// The exact-context lookup deliberately misses across a HEAD change (see
// persistence.TestNotLoadWorktreeSessionAfterHeadAdvances); this is the
// composition that makes the miss recoverable.
func TestOpenSessionCarriesReviewAcrossAmend(t *testing.T) {
	store := &persistence.Store{ReviewsDir: t.TempDir()}
	m := testModel(t)

	// A branch, not a detached HEAD: carry-forward needs a stable anchor.
	lc1, session := openSession(store, branchSession(t, "abc1234567"), nil)
	session.ReviewComments = append(session.ReviewComments,
		model.NewComment("still relevant", model.CommentTypeFromID("note"), nil))
	m.App.Session = session
	m.session = lc1
	if err := lc1.save(m.App); err != nil {
		t.Fatal(err)
	}

	// The amend: same branch, same working-tree source, new HEAD.
	fresh := model.NewReviewSession(
		session.RepoPath, "9999999aaaaaaa", session.BranchName, session.DiffSource)
	lc2, resumed := openSession(store, fresh, nil)

	if lc2.wasCreated {
		t.Error("a session carried forward is not a fresh one")
	}
	if len(resumed.ReviewComments) != 1 || resumed.ReviewComments[0].Content != "still relevant" {
		t.Fatalf("the review did not survive the amend: %+v", resumed.ReviewComments)
	}
	if resumed.ID != session.ID {
		t.Errorf("session id = %q, want the original %q", resumed.ID, session.ID)
	}
	if resumed.BaseCommit != "9999999aaaaaaa" {
		t.Errorf("base commit = %q, want the new HEAD", resumed.BaseCommit)
	}
	if lc2.adoptedFrom != session.BaseCommit {
		t.Errorf("adoptedFrom = %q, want %q — the reviewer has to be told",
			lc2.adoptedFrom, session.BaseCommit)
	}
	if lc2.path == lc1.path {
		t.Error("the carried session must live at its new identity")
	}
}

// TestOpenSessionAtSameHeadIsNotAdoption keeps the ordinary resume path clear
// of carry-forward: an unmoved HEAD must resolve exactly, and say nothing.
func TestOpenSessionAtSameHeadIsNotAdoption(t *testing.T) {
	store := &persistence.Store{ReviewsDir: t.TempDir()}
	m := testModel(t)

	lc1, session := openSession(store, branchSession(t, "abc1234567"), nil)
	session.ReviewComments = append(session.ReviewComments,
		model.NewComment("keep me", model.CommentTypeFromID("note"), nil))
	m.App.Session = session
	m.session = lc1
	if err := lc1.save(m.App); err != nil {
		t.Fatal(err)
	}

	fresh := model.NewReviewSession(
		session.RepoPath, session.BaseCommit, session.BranchName, session.DiffSource)
	lc2, resumed := openSession(store, fresh, nil)

	if lc2.adoptedFrom != "" {
		t.Errorf("adoptedFrom = %q, want empty at an unmoved HEAD", lc2.adoptedFrom)
	}
	if len(resumed.ReviewComments) != 1 {
		t.Errorf("comments = %d, want 1", len(resumed.ReviewComments))
	}
}

// TestOpenSessionDoesNotCarryDetachedHead covers the deliberate exclusion. A
// detached anchor is derived from the commit, so two detached checkouts are
// unrelated positions rather than one review that moved — carrying comments
// across would put them somewhere they were never written.
func TestOpenSessionDoesNotCarryDetachedHead(t *testing.T) {
	store := &persistence.Store{ReviewsDir: t.TempDir()}
	m := testModel(t)
	repo := t.TempDir()

	lc1, session := openSession(store,
		model.NewReviewSession(repo, "abc1234567", nil, model.SourceWorkingTree), nil)
	session.ReviewComments = append(session.ReviewComments,
		model.NewComment("about the old commit", model.CommentTypeFromID("note"), nil))
	m.App.Session = session
	m.session = lc1
	if err := lc1.save(m.App); err != nil {
		t.Fatal(err)
	}

	lc2, resumed := openSession(store,
		model.NewReviewSession(repo, "9999999aaaaaaa", nil, model.SourceWorkingTree), nil)
	if !lc2.wasCreated || lc2.adoptedFrom != "" {
		t.Error("a detached HEAD must start a fresh review, not adopt")
	}
	if len(resumed.ReviewComments) != 0 {
		t.Errorf("comments leaked across detached checkouts: %+v", resumed.ReviewComments)
	}
}

// TestOpenSessionWithoutStoreStillOpens covers the no-persistence path, where
// neither lookup nor adoption is available.
func TestOpenSessionWithoutStoreStillOpens(t *testing.T) {
	m := testModel(t)
	lc, session := openSession(nil, m.App.Session, nil)
	if session == nil || !lc.wasCreated || lc.adoptedFrom != "" {
		t.Errorf("a storeless open must yield the fresh session: %+v", lc)
	}
}

// TestReportSessionResumeWording pins what the reviewer is told. A carried
// session with a stale comment in it is the one combination that has to warn:
// these comments are older than this diff, and some no longer fit it.
func TestReportSessionResumeWording(t *testing.T) {
	cases := []struct {
		name      string
		adopted   string
		stats     app.AnchorStats
		wantKind  app.MessageType
		wantParts []string
		wantNone  bool
	}{
		{
			name: "carried clean", adopted: "abc1234567",
			stats: app.AnchorStats{Checked: 2}, wantKind: app.MessageInfo,
			wantParts: []string{"Resumed this review from a previous HEAD", "abc1234"},
		},
		{
			name: "carried with moves", adopted: "abc1234567",
			stats: app.AnchorStats{Checked: 2, Moved: 1}, wantKind: app.MessageInfo,
			wantParts: []string{"previous HEAD", "re-anchored"},
		},
		{
			name: "carried with stale", adopted: "abc1234567",
			stats: app.AnchorStats{Checked: 2, Outdated: 1}, wantKind: app.MessageWarning,
			wantParts: []string{"previous HEAD", "outdated"},
		},
		{
			name: "not carried, clean", adopted: "",
			stats: app.AnchorStats{Checked: 2}, wantNone: true,
		},
		{
			name: "not carried, stale", adopted: "",
			stats: app.AnchorStats{Checked: 2, Outdated: 1}, wantKind: app.MessageWarning,
			wantParts: []string{"outdated"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			a.AnchorStats = tc.stats
			reportSessionResume(a, &sessionLifecycle{adoptedFrom: tc.adopted})

			if tc.wantNone {
				if a.Message != nil {
					t.Fatalf("expected silence, got %+v", a.Message)
				}
				return
			}
			if a.Message == nil {
				t.Fatal("expected a message")
			}
			if a.Message.Type != tc.wantKind {
				t.Errorf("message type = %v, want %v", a.Message.Type, tc.wantKind)
			}
			for _, part := range tc.wantParts {
				if !strings.Contains(a.Message.Content, part) {
					t.Errorf("message %q missing %q", a.Message.Content, part)
				}
			}
		})
	}
	// A nil lifecycle must not panic — the storeless path passes one through.
	a := testApp(t)
	a.AnchorStats = app.AnchorStats{Checked: 1, Outdated: 1}
	reportSessionResume(a, nil)
	if a.Message == nil || a.Message.Type != app.MessageWarning {
		t.Errorf("a nil lifecycle must still report anchors, got %+v", a.Message)
	}
}
