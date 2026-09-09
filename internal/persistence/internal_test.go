package persistence

// Targeted tests for internal branches the ported tuicr suites do not reach:
// slug down-casting edge cases, corruption fallbacks, and error paths.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/slug"
)

// fakeSlug is a Slug implementation of neither known concrete kind.
type fakeSlug struct{}

func (fakeSlug) String() string { return "fake" }

func TestSlugKindPartsHandlesPointersAndUnknown(t *testing.T) {
	local, pr := slugKindParts(&slug.LocalSlug{Repo: "r"})
	if local == nil || pr != nil {
		t.Fatal("pointer LocalSlug should down-cast to local")
	}
	local, pr = slugKindParts(&slug.PrSlug{Number: 7})
	if local != nil || pr == nil {
		t.Fatal("pointer PrSlug should down-cast to pr")
	}
	local, pr = slugKindParts(fakeSlug{})
	if local != nil || pr != nil {
		t.Fatal("unknown slug type should down-cast to neither kind")
	}
}

func TestManifestAnchorForUnknownSlugIsEmpty(t *testing.T) {
	if anchor := manifestAnchorFor(fakeSlug{}); anchor != "" {
		t.Fatalf("anchor = %q, want empty", anchor)
	}
}

func TestRelativePathRejectsUnknownSlugKind(t *testing.T) {
	sess := makeLocalSession(t, makeRepo(t), "abc", new("main"), model.SourceWorkingTree, nil)
	_, err := relativePathForSession(fakeSlug{}, sess)
	if _, ok := errors.AsType[*errs.CorruptedSession](err); !ok {
		t.Fatalf("err = %v, want *errs.CorruptedSession", err)
	}
}

func TestRelativePathRequiresPrSessionKey(t *testing.T) {
	sess := makePrSession(makePrKey(125, "abcdef"))
	sess.PrSessionKey = nil
	_, err := relativePathForSession(slug.PrSlug{Number: 125}, sess)
	if _, ok := errors.AsType[*errs.CorruptedSession](err); !ok {
		t.Fatalf("err = %v, want *errs.CorruptedSession", err)
	}
}

func TestSessionPathWrapsSlugDeriveError(t *testing.T) {
	store := newTestStore(t)
	sess := makePrSession(makePrKey(125, "abcdef"))
	sess.PrSessionKey = nil // PullRequest source with no key cannot derive a slug

	_, err := store.SessionPath(sess)
	if _, ok := errors.AsType[*errs.CorruptedSession](err); !ok {
		t.Fatalf("err = %v, want *errs.CorruptedSession", err)
	}
}

func TestMarkSessionActiveWrapsSlugDeriveError(t *testing.T) {
	store := newTestStore(t)
	sess := makePrSession(makePrKey(125, "abcdef"))
	sess.PrSessionKey = nil

	err := store.MarkSessionActive(sess, filepath.Join(store.ReviewsDir, "x.json"))
	if _, ok := errors.AsType[*errs.CorruptedSession](err); !ok {
		t.Fatalf("err = %v, want *errs.CorruptedSession", err)
	}
}

func TestSaveRecoversFromCorruptManifest(t *testing.T) {
	store := newTestStore(t)
	if err := os.MkdirAll(filepath.Join(store.ReviewsDir, SessionsDirname), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(store.ReviewsDir, ManifestFilename), "not json {")

	sess := makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil)
	path := mustSave(t, store, sess)

	if !fileExists(t, path) {
		t.Fatal("save should succeed despite a corrupt manifest")
	}
	manifest, err := LoadManifest(store.ReviewsDir)
	if err != nil {
		t.Fatalf("manifest should be rewritten cleanly: %v", err)
	}
	if manifest.Len() != 1 {
		t.Fatalf("manifest len = %d, want 1", manifest.Len())
	}
}

func TestGetPrMissesLocalOnlyBucketAndViceVersa(t *testing.T) {
	m := NewManifest()
	m.Upsert("slug-a", localEntry(time.Now(), "/a", "main"))
	m.Upsert("slug-b", prEntry(time.Now(), 125, "abc"))

	if m.GetPr("slug-a") != nil {
		t.Fatal("GetPr must not match a local entry")
	}
	if m.GetLocal("slug-b", "/a") != nil {
		t.Fatal("GetLocal must not match a PR entry")
	}
	if m.GetPr("missing") != nil || m.GetLocal("missing", "/a") != nil {
		t.Fatal("missing slugs must not match")
	}
}

func TestUpsertMatchesNilCanonicalPaths(t *testing.T) {
	m := NewManifest()
	entry := localEntry(time.Now(), "/x", "main")
	entry.CanonicalRepoPath = nil
	m.Upsert("s", entry)
	entry.Display.CommentCount = 7
	m.Upsert("s", entry)

	if m.Len() != 1 {
		t.Fatalf("len = %d, want 1 (nil canonical paths must compare equal)", m.Len())
	}
	mixed := localEntry(time.Now(), "/y", "main")
	m.Upsert("s", mixed)
	if m.Len() != 2 {
		t.Fatalf("len = %d, want 2 (nil vs non-nil canonical paths differ)", m.Len())
	}
}

func TestSortSummariesBreaksTiesDeterministically(t *testing.T) {
	at := time.Now()
	summaries := []SessionSummary{
		{Slug: "b", Path: "2", UpdatedAt: at},
		{Slug: "a", Path: "9", UpdatedAt: at},
		{Slug: "a", Path: "1", UpdatedAt: at},
		{Slug: "c", Path: "0", UpdatedAt: at.Add(time.Second)},
	}
	sortSummaries(summaries)

	got := []string{
		summaries[0].Slug + summaries[0].Path,
		summaries[1].Slug + summaries[1].Path,
		summaries[2].Slug + summaries[2].Path,
		summaries[3].Slug + summaries[3].Path,
	}
	want := []string{"c0", "a1", "a9", "b2"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestListSessionsWithUnparseableSelectorMatchesNothing(t *testing.T) {
	store := newTestStore(t)
	mustSave(t, store, makePrSession(makePrKey(125, "abcdef0123456789")))

	listed, err := store.ListSessions("")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("listed %d sessions for empty selector, want 0", len(listed))
	}
}

func TestUpdateSessionMissingFileErrors(t *testing.T) {
	store := newTestStore(t)
	_, err := store.UpdateSession(filepath.Join(store.ReviewsDir, "missing.json"),
		func(*model.ReviewSession) error { return nil })
	if err == nil {
		t.Fatal("updating a missing session must error")
	}
}

func TestDeleteSessionCorruptFileErrors(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil)
	path := mustSave(t, store, sess)
	writeTestFile(t, path, "not json {")

	if _, err := store.DeleteSession(path); err == nil {
		t.Fatal("deleting a corrupt session must surface the corruption")
	}
	if _, err := store.DeleteSessionIfEmpty(path); err == nil {
		t.Fatal("empty-deleting a corrupt session must surface the corruption")
	}
}

func TestLoadPrSessionCorruptFileErrors(t *testing.T) {
	store := newTestStore(t)
	key := makePrKey(125, "abcdef0123456789")
	path := mustSave(t, store, makePrSession(key))
	writeTestFile(t, path, "not json {")

	if _, _, _, err := store.LoadPrSession(key); err == nil {
		t.Fatal("a corrupt PR session must surface the corruption")
	}
}

func TestLoadLatestSessionForContextCorruptFileErrors(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	path := mustSave(t, store, makeLocalSession(t, repo, "abc1234", new("main"), model.SourceWorkingTree, nil))
	writeTestFile(t, path, "not json {")

	_, _, _, err := store.LoadLatestSessionForContext(
		repo, new("main"), "abc1234", model.SourceWorkingTree, nil)
	if err == nil {
		t.Fatal("a corrupt local session must surface the corruption")
	}
}

func TestRemoveStaleLockMissingFileIsNotStale(t *testing.T) {
	removed, err := removeStaleLock(filepath.Join(t.TempDir(), "missing.lock"))
	if err != nil || removed {
		t.Fatalf("removeStaleLock = %v, %v; want false, nil", removed, err)
	}
}

func TestReadLockOwnerPidHandlesEmptyAndMissing(t *testing.T) {
	if _, ok := readLockOwnerPid(filepath.Join(t.TempDir(), "missing")); ok {
		t.Fatal("missing lock file must yield no pid")
	}
	empty := filepath.Join(t.TempDir(), "empty")
	writeTestFile(t, empty, "   \n")
	if _, ok := readLockOwnerPid(empty); ok {
		t.Fatal("empty lock body must yield no pid")
	}
}

func TestWriteAtomicFailsWhenParentIsAFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	writeTestFile(t, blocker, "")
	if err := writeAtomic(filepath.Join(blocker, "child", "x.json"), []byte("{}")); err == nil {
		t.Fatal("writeAtomic under a regular file must error")
	}
}

func TestSaveManifestFailsWhenDirIsAFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	writeTestFile(t, blocker, "")
	if err := SaveManifest(filepath.Join(blocker, "reviews"), NewManifest()); err == nil {
		t.Fatal("SaveManifest under a regular file must error")
	}
}

func TestMarshalPrettyShape(t *testing.T) {
	data, err := marshalPretty(map[string]string{"a": "<b>&c"})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": \"<b>&c\"\n}\n"
	if string(data) != want {
		t.Fatalf("marshalPretty = %q, want %q (2-space indent, no HTML escaping, trailing newline)", data, want)
	}
}
