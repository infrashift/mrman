package persistence

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/model"
)

// commentedLocalSession is a local session carrying one line comment, so
// adoption has something worth carrying.
func commentedLocalSession(t *testing.T, repo, head string, branch *string,
	src model.SessionDiffSource) *model.ReviewSession {
	t.Helper()
	sess := makeLocalSession(t, repo, head, branch, src, nil)
	side := model.LineSideNew
	c := model.NewComment("needs a guard", model.CommentTypeFromID("issue"), &side)
	c.LineContext = &model.LineContext{Content: "if x != nil {"}
	sess.Files["src/main.go"].AddLineComment(4, c)
	return sess
}

func commentCount(sess *model.ReviewSession) int {
	n := len(sess.ReviewComments)
	for _, f := range sess.Files {
		n += f.CommentCount()
	}
	return n
}

// TestAdoptCarriesReviewForwardOntoNewHead is the headline behaviour: amending
// a commit no longer throws the review away.
func TestAdoptCarriesReviewForwardOntoNewHead(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	original := commentedLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree)
	oldPath := mustSave(t, store, original)

	adopted, ok, err := store.AdoptSessionForNewHead(
		repo, strp("main"), "9999999aaa", model.SourceWorkingTree)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("a commented worktree session must carry forward onto a new HEAD")
	}
	if adopted.Session.ID != original.ID {
		t.Errorf("session id = %q, want the original %q — this is the same review continuing",
			adopted.Session.ID, original.ID)
	}
	if commentCount(adopted.Session) != 1 {
		t.Errorf("comment count = %d, want 1", commentCount(adopted.Session))
	}
	if adopted.Session.BaseCommit != "9999999aaa" {
		t.Errorf("base commit = %q, want the new HEAD", adopted.Session.BaseCommit)
	}
	if adopted.FromHead != "abc1234" {
		t.Errorf("FromHead = %q, want abc1234", adopted.FromHead)
	}

	// Carried, not copied: the old identity is gone from both disk and the
	// manifest, so the review does not appear twice.
	if adopted.Path == oldPath {
		t.Fatal("the adopted session must move to its new identity")
	}
	if fileExists(t, oldPath) {
		t.Error("the previous HEAD's session file must be removed, not left as a duplicate")
	}
	manifest := loadManifestOrDefault(store.ReviewsDir)
	if _, still := manifest.Entries[adopted.FromSlug]; still {
		t.Errorf("the previous slug %q must be gone from the manifest", adopted.FromSlug)
	}

	// And the review now resolves at the new HEAD by the ordinary exact lookup.
	_, loaded, found, err := store.LoadLatestSessionForContext(
		repo, strp("main"), "9999999aaa", model.SourceWorkingTree, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !found || loaded.ID != original.ID {
		t.Error("after adoption the session must resolve at the new HEAD")
	}
}

// TestAdoptSkipsEmptySession keeps adoption from churning files for reviewers
// who have not written anything yet.
func TestAdoptSkipsEmptySession(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	oldPath := mustSave(t, store,
		makeLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree, nil))

	_, ok, err := store.AdoptSessionForNewHead(
		repo, strp("main"), "9999999aaa", model.SourceWorkingTree)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("an untouched session is not worth carrying forward")
	}
	if !fileExists(t, oldPath) {
		t.Error("declining to adopt must not delete anything")
	}
}

// TestAdoptCarriesReviewedOnlySession covers reviewed marks with no comments:
// working out which files you had already been through is worth keeping too.
func TestAdoptCarriesReviewedOnlySession(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	sess := makeLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree, nil)
	sess.Files["src/main.go"].Reviewed = true
	mustSave(t, store, sess)

	adopted, ok, err := store.AdoptSessionForNewHead(
		repo, strp("main"), "9999999aaa", model.SourceWorkingTree)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("reviewed state must carry forward")
	}
	if !adopted.Session.IsFileReviewed("src/main.go") {
		t.Error("the reviewed mark was lost in the carry-forward")
	}
}

// TestAdoptRefusesNonLiveSources guards the narrow definition of "the same
// review moved along". A commit range names its own endpoints, so a different
// range is a different review; pristine has no HEAD in its identity at all.
func TestAdoptRefusesNonLiveSources(t *testing.T) {
	for _, src := range []model.SessionDiffSource{
		model.SourceCommitRange, model.SourcePristine, model.SourcePullRequest,
	} {
		t.Run(string(src), func(t *testing.T) {
			store := newTestStore(t)
			repo := makeRepo(t)
			sess := commentedLocalSession(t, repo, "abc1234", strp("main"), src)
			sess.CommitRange = []string{"abc1234", "0000111"}
			if src != model.SourcePullRequest {
				mustSave(t, store, sess)
			}

			if _, ok, err := store.AdoptSessionForNewHead(
				repo, strp("main"), "9999999aaa", src); err != nil || ok {
				t.Errorf("source %s must not carry forward (ok=%v, err=%v)", src, ok, err)
			}
		})
	}
}

// TestAdoptRefusesDetachedHead covers the case with no stable identity to
// carry: a detached anchor is derived from the commit itself, so two detached
// checkouts are not the same review at two positions.
func TestAdoptRefusesDetachedHead(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	mustSave(t, store, commentedLocalSession(t, repo, "abc1234", nil, model.SourceWorkingTree))

	if _, ok, err := store.AdoptSessionForNewHead(
		repo, nil, "9999999aaa", model.SourceWorkingTree); err != nil || ok {
		t.Errorf("a detached HEAD must not carry forward (ok=%v, err=%v)", ok, err)
	}
}

// TestAdoptDoesNotCrossBranches keeps a review on one branch from being
// resurrected on another.
func TestAdoptDoesNotCrossBranches(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	mustSave(t, store, commentedLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree))

	if _, ok, err := store.AdoptSessionForNewHead(
		repo, strp("feature"), "9999999aaa", model.SourceWorkingTree); err != nil || ok {
		t.Errorf("another branch must not adopt this review (ok=%v, err=%v)", ok, err)
	}
}

// TestAdoptDoesNotCrossDiffSources keeps a staged review from being adopted by
// a worktree one: they are different views of different content.
func TestAdoptDoesNotCrossDiffSources(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	mustSave(t, store, commentedLocalSession(t, repo, "abc1234", strp("main"), model.SourceStaged))

	if _, ok, err := store.AdoptSessionForNewHead(
		repo, strp("main"), "9999999aaa", model.SourceWorkingTree); err != nil || ok {
		t.Errorf("a staged review must not be adopted as a worktree one (ok=%v, err=%v)", ok, err)
	}
}

// TestAdoptDoesNotCrossCheckouts covers two clones of the same repo on the
// same branch. Their slugs are identical, which is exactly why the manifest
// disambiguates by canonical path — adoption has to honour that too.
func TestAdoptDoesNotCrossCheckouts(t *testing.T) {
	store := newTestStore(t)
	mine, theirs := makeRepo(t), makeRepo(t)
	mustSave(t, store, commentedLocalSession(t, theirs, "abc1234", strp("main"), model.SourceWorkingTree))

	if _, ok, err := store.AdoptSessionForNewHead(
		mine, strp("main"), "9999999aaa", model.SourceWorkingTree); err != nil || ok {
		t.Errorf("another checkout's review must not be adopted (ok=%v, err=%v)", ok, err)
	}
}

// TestAdoptPicksMostRecentlyUpdated settles ambiguity when a branch has
// accumulated sessions at several previous HEADs.
func TestAdoptPicksMostRecentlyUpdated(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)

	older := commentedLocalSession(t, repo, "aaaaaaa1", strp("main"), model.SourceWorkingTree)
	older.UpdatedAt = time.Now().Add(-2 * time.Hour)
	mustSave(t, store, older)

	newer := commentedLocalSession(t, repo, "bbbbbbb2", strp("main"), model.SourceWorkingTree)
	newer.UpdatedAt = time.Now().Add(-1 * time.Minute)
	mustSave(t, store, newer)

	adopted, ok, err := store.AdoptSessionForNewHead(
		repo, strp("main"), "9999999aaa", model.SourceWorkingTree)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected an adoption")
	}
	if adopted.Session.ID != newer.ID {
		t.Errorf("adopted the wrong session: got %q, want the most recent %q",
			adopted.Session.ID, newer.ID)
	}
	// The one not adopted is left alone rather than cleaned up: adoption moves
	// one review, it does not garbage-collect the reviewer's history.
	olderPath, err := store.SessionPath(older)
	if err != nil {
		t.Fatal(err)
	}
	if !fileExists(t, olderPath) {
		t.Error("the session that was not adopted must be left on disk")
	}
}

// TestAdoptAtExistingHeadResolvesWithoutCarrying covers the locked recheck.
// A session already at the requested HEAD is returned as-is — that is what
// stops a second mrman, whose unlocked lookup missed by a hair, from saving a
// fresh session over one this instance just carried forward. Nothing is
// carried, so FromHead stays empty and the reviewer is told nothing.
func TestAdoptAtExistingHeadResolvesWithoutCarrying(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	sess := commentedLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree)
	path := mustSave(t, store, sess)

	adopted, ok, err := store.AdoptSessionForNewHead(
		repo, strp("main"), "abc1234", model.SourceWorkingTree)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("an existing session at this HEAD must be returned, not lost")
	}
	if adopted.Path != path {
		t.Errorf("path = %q, want the unchanged %q", adopted.Path, path)
	}
	if adopted.FromHead != "" || adopted.FromSlug != "" {
		t.Errorf("nothing was carried, so FromHead/FromSlug must be empty: %+v", adopted)
	}
	if adopted.Session.ID != sess.ID || commentCount(adopted.Session) != 1 {
		t.Error("the existing session must come back intact")
	}
	if !fileExists(t, path) {
		t.Error("the session file must be untouched")
	}
}

// TestAdoptTwiceIsIdempotent is the race in sequence: the second call finds
// the work already done and must not carry anything a second time, nor report
// that it did.
func TestAdoptTwiceIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	original := commentedLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree)
	mustSave(t, store, original)

	first, ok, err := store.AdoptSessionForNewHead(
		repo, strp("main"), "9999999aaa", model.SourceWorkingTree)
	if err != nil || !ok {
		t.Fatalf("first adoption failed (ok=%v, err=%v)", ok, err)
	}
	second, ok, err := store.AdoptSessionForNewHead(
		repo, strp("main"), "9999999aaa", model.SourceWorkingTree)
	if err != nil || !ok {
		t.Fatalf("second call must still resolve the session (ok=%v, err=%v)", ok, err)
	}
	if second.FromHead != "" {
		t.Errorf("the second call carried nothing, got FromHead %q", second.FromHead)
	}
	if second.Path != first.Path || second.Session.ID != original.ID {
		t.Errorf("second call resolved elsewhere: %q vs %q", second.Path, first.Path)
	}
	if commentCount(second.Session) != 1 {
		t.Errorf("comment count = %d, want 1 — no duplication", commentCount(second.Session))
	}
}

// TestAdoptSurvivesAMissingCandidateFile covers a manifest entry pointing at a
// file that is gone. Adoption is best-effort: the open falls back to a fresh
// review rather than failing.
func TestAdoptSurvivesAMissingCandidateFile(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	path := mustSave(t, store,
		commentedLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := store.AdoptSessionForNewHead(
		repo, strp("main"), "9999999aaa", model.SourceWorkingTree); err != nil || ok {
		t.Errorf("a vanished candidate must not fail the open (ok=%v, err=%v)", ok, err)
	}
}

// TestRemoveManifestEntryKeepsSiblings covers the two-checkouts bucket: only
// the named entry goes.
func TestRemoveManifestEntryKeepsSiblings(t *testing.T) {
	store := newTestStore(t)
	mine, theirs := makeRepo(t), makeRepo(t)
	minePath := mustSave(t, store,
		commentedLocalSession(t, mine, "abc1234", strp("main"), model.SourceWorkingTree))
	mustSave(t, store,
		commentedLocalSession(t, theirs, "abc1234", strp("main"), model.SourceWorkingTree))

	manifest := loadManifestOrDefault(store.ReviewsDir)
	var slugStr string
	for s, bucket := range manifest.Entries {
		if len(bucket) == 2 {
			slugStr = s
		}
	}
	if slugStr == "" {
		t.Fatal("expected one slug shared by two checkouts")
	}
	rel, err := filepath.Rel(store.ReviewsDir, minePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.removeManifestEntry(slugStr, rel); err != nil {
		t.Fatal(err)
	}
	if got := len(loadManifestOrDefault(store.ReviewsDir).Entries[slugStr]); got != 1 {
		t.Errorf("bucket size = %d, want 1 — the sibling checkout must survive", got)
	}
}
