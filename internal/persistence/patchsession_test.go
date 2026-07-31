package persistence

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// makePatchSession builds a session shaped the way the patch backend builds
// one: the artifact's directory as the repo path, its filename stem as the
// anchor, and a hex content hash where a commit would go.
func makePatchSession(t *testing.T, dir, stem, contentHash string) *model.ReviewSession {
	t.Helper()
	branch := stem
	s := model.NewReviewSession(dir, contentHash, &branch, model.SourcePatch)
	s.AddFile("drivers/foo.c", model.StatusModified, 0)
	return s
}

// TestPatchSessionRoundTrips is the end-to-end identity contract: a patch
// review must save, be findable again, and come back with its comments.
//
// This is the assertion that would have caught a missing KindForSource arm in
// production rather than in a unit test — a session whose slug cannot be
// derived silently never persists, because openSession discards the save
// error.
func TestPatchSessionRoundTrips(t *testing.T) {
	store := newTestStore(t)
	dir := makeRepo(t)
	sess := makePatchSession(t, dir, "v2_net_fix", "9f3c1a2b7d4e5061")

	side := model.LineSideNew
	c := model.NewComment("wrong errno", model.CommentTypeFromID("issue"), &side)
	c.LineContext = &model.LineContext{Content: "\t\treturn -EINVAL;"}
	sess.Files["drivers/foo.c"].AddLineComment(103, c)

	path := mustSave(t, store, sess)
	if !fileExists(t, path) {
		t.Fatal("the patch session did not reach disk")
	}

	_, loaded, found, err := store.LoadLatestSessionForContext(
		dir, strp("v2_net_fix"), "9f3c1a2b7d4e5061", model.SourcePatch, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("the patch session did not resolve again")
	}
	if loaded.ID != sess.ID {
		t.Errorf("resolved %q, want %q", loaded.ID, sess.ID)
	}
	if got := loaded.Files["drivers/foo.c"].CommentCount(); got != 1 {
		t.Errorf("comment count = %d, want 1", got)
	}
	// The anchor snapshot is what lets a v2 of the same patch re-anchor the
	// comment rather than lose it.
	if got := loaded.Files["drivers/foo.c"].LineComments[103][0].LineContext.Content; got != "\t\treturn -EINVAL;" {
		t.Errorf("LineContext.Content = %q, want the tab-indented snapshot", got)
	}
}

// TestPatchSessionFilenameIsReadable checks the naming work applies here too:
// a directory of patch reviews should be legible and globbable.
func TestPatchSessionFilenameIsReadable(t *testing.T) {
	store := newTestStore(t)
	dir := makeRepo(t)
	path := mustSave(t, store, makePatchSession(t, dir, "v2_net_fix", "9f3c1a2b7d4e5061"))

	name := filepath.Base(path)
	assertHashSuffix(t, path)
	for _, want := range []string{"v2_net_fix", "patch", "9f3c1a2"} {
		if !strings.Contains(name, want) {
			t.Errorf("filename %s should carry %q", name, want)
		}
	}
	if strings.Contains(name, ":") {
		t.Errorf("filename %s must not contain a colon", name)
	}
}

// TestDifferentPatchesAreDifferentSessions is why the content hash is the
// identity. Two artifacts in one directory must not share a review.
func TestDifferentPatchesAreDifferentSessions(t *testing.T) {
	store := newTestStore(t)
	dir := makeRepo(t)

	a := mustSave(t, store, makePatchSession(t, dir, "series_v1", "1111111111111111"))
	b := mustSave(t, store, makePatchSession(t, dir, "series_v2", "2222222222222222"))
	if a == b {
		t.Fatal("two patch artifacts must not share a session file")
	}

	// And a resend of the same artifact under a new name is still a new review
	// — the bytes are the identity, but so is the name it was filed under.
	c := mustSave(t, store, makePatchSession(t, dir, "series_v1", "3333333333333333"))
	if c == a {
		t.Error("edited bytes must start a new review")
	}
}

// TestPatchSessionIsNeverCarriedForward keeps the amend carry-forward away
// from patches: adopting one onto a "new HEAD" is meaningless, because a
// different artifact is a different review rather than the same one moved.
func TestPatchSessionIsNeverCarriedForward(t *testing.T) {
	store := newTestStore(t)
	dir := makeRepo(t)

	sess := makePatchSession(t, dir, "series", "1111111111111111")
	side := model.LineSideNew
	sess.Files["drivers/foo.c"].AddLineComment(1,
		model.NewComment("keep me", model.CommentTypeFromID("note"), &side))
	mustSave(t, store, sess)

	if _, ok, err := store.AdoptSessionForNewHead(
		dir, strp("series"), "2222222222222222", model.SourcePatch); err != nil || ok {
		t.Errorf("a patch session must not carry forward (ok=%v, err=%v)", ok, err)
	}
}

// TestPatchSessionListsFromItsDirectory covers discovery: the review has no
// checkout, so it is findable from wherever the artifact lives.
func TestPatchSessionListsFromItsDirectory(t *testing.T) {
	store := newTestStore(t)
	dir := makeRepo(t)
	mustSave(t, store, makePatchSession(t, dir, "series", "1111111111111111"))

	rows, err := store.ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d sessions for %s, want 1", len(rows), dir)
	}
	if rows[0].Kind != SummaryKindLocal {
		t.Errorf("Kind = %q, want %q", rows[0].Kind, SummaryKindLocal)
	}
	if rows[0].Anchor != "series" {
		t.Errorf("Anchor = %q, want the patch stem", rows[0].Anchor)
	}
	if strings.Contains(rows[0].Slug, ":") {
		t.Errorf("slug %q must be addressable, so it must not contain a colon", rows[0].Slug)
	}
}
