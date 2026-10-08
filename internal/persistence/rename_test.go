package persistence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// hashedNameStore writes a session under the old bare-hash filename and leaves
// a v2.0 manifest pointing at it, i.e. exactly what an existing install looks
// like before first open on the new layout.
func hashedNameStore(t *testing.T, sessions ...*model.ReviewSession) (*Store, []string) {
	t.Helper()
	store := newTestStore(t)
	sessionsDir := filepath.Join(store.ReviewsDir, SessionsDirname)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := NewManifest()
	manifest.Version = manifestVersionHashedNames
	var paths []string
	for i, sess := range sessions {
		// The old scheme: hash only, no label.
		name := "abcdef012345678" + string(rune('a'+i)) + ".json"
		relative := filepath.Join(SessionsDirname, name)
		full := filepath.Join(store.ReviewsDir, relative)
		data, err := json.Marshal(sess)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sl, err := sessionSlug(sess)
		if err != nil {
			t.Fatal(err)
		}
		manifest.Upsert(sl.String(), entryFromSession(sess, relative, manifestAnchorFor(sl)))
		paths = append(paths, full)
	}
	if err := SaveManifest(store.ReviewsDir, manifest); err != nil {
		t.Fatal(err)
	}
	return store, paths
}

// TestMigrateRenamesHashedSessionsKeepingThem is the contract that matters to
// anyone already using mrman: a layout change must not cost them their
// reviews. Unlike the pre-sessions/ migration, nothing is moved aside.
func TestMigrateRenamesHashedSessionsKeepingThem(t *testing.T) {
	repo := makeRepo(t)
	sess := makeLocalSession(t, repo, "abc1234", new("main"), model.SourceWorkingTree, nil)
	side := model.LineSideNew
	sess.Files["src/main.go"].AddLineComment(4,
		model.NewComment("keep me", model.CommentTypeFromID("issue"), &side))

	store, oldPaths := hashedNameStore(t, sess)

	// Any operation triggers the migration.
	if err := store.maybeMigrate(); err != nil {
		t.Fatal(err)
	}

	if fileExists(t, oldPaths[0]) {
		t.Error("the bare-hash file should have been renamed, not left behind")
	}

	// The session still resolves, with its comment.
	_, loaded, found, err := store.LoadLatestSessionForContext(
		repo, new("main"), "abc1234", model.SourceWorkingTree, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("the migrated session must still resolve")
	}
	if loaded.ID != sess.ID {
		t.Errorf("resolved %q, want %q", loaded.ID, sess.ID)
	}
	if got := loaded.Files["src/main.go"].CommentCount(); got != 1 {
		t.Errorf("comment count = %d, want 1 — migration must not lose content", got)
	}

	// And it now sits under a readable name.
	entries, err := os.ReadDir(filepath.Join(store.ReviewsDir, SessionsDirname))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one session file, got %d", len(entries))
	}
	if name := entries[0].Name(); !strings.Contains(name, "@") {
		t.Errorf("migrated file %s carries no label", name)
	}
}

// TestMigrateStampsVersionAndIsIdempotent keeps the rename from re-running on
// every store operation.
func TestMigrateStampsVersionAndIsIdempotent(t *testing.T) {
	store, _ := hashedNameStore(t,
		makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil))

	if err := store.maybeMigrate(); err != nil {
		t.Fatal(err)
	}
	manifest := mustLoadManifest(t, store)
	if manifest.Version != ManifestVersion {
		t.Fatalf("manifest version = %q, want %q", manifest.Version, ManifestVersion)
	}
	before, err := os.ReadDir(filepath.Join(store.ReviewsDir, SessionsDirname))
	if err != nil {
		t.Fatal(err)
	}

	if err := store.maybeMigrate(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(filepath.Join(store.ReviewsDir, SessionsDirname))
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Errorf("second migration changed the file count: %d then %d", len(before), len(after))
	}
	if before[0].Name() != after[0].Name() {
		t.Errorf("second migration renamed again: %s then %s", before[0].Name(), after[0].Name())
	}
}

// TestMigrateRewritesManifestPaths covers the half that would otherwise leave
// every session unreachable: renaming files without updating the manifest.
func TestMigrateRewritesManifestPaths(t *testing.T) {
	store, _ := hashedNameStore(t,
		makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil))

	if err := store.maybeMigrate(); err != nil {
		t.Fatal(err)
	}

	manifest := mustLoadManifest(t, store)
	if manifest.Len() != 1 {
		t.Fatalf("manifest entries = %d, want 1", manifest.Len())
	}
	manifest.each(func(_ string, entry ManifestEntry) {
		full := filepath.Join(store.ReviewsDir, entry.Path)
		if !fileExists(t, full) {
			t.Errorf("manifest points at %s, which does not exist", entry.Path)
		}
		if !strings.Contains(filepath.Base(entry.Path), "@") {
			t.Errorf("manifest still records the old name %s", entry.Path)
		}
	})
}

// TestMigrateHandlesMultipleSessions covers a realistic store rather than a
// single file, including a PR session alongside local ones.
func TestMigrateHandlesMultipleSessions(t *testing.T) {
	repoA, repoB := makeRepo(t), makeRepo(t)
	store, _ := hashedNameStore(t,
		makeLocalSession(t, repoA, "abc1234", new("main"), model.SourceWorkingTree, nil),
		makeLocalSession(t, repoB, "def5678", new("feature"), model.SourceStaged, nil),
		makePrSession(makePrKey(125, "abcdef0123456789")),
	)

	if err := store.maybeMigrate(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(store.ReviewsDir, SessionsDirname))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 session files, got %d", len(entries))
	}
	for _, entry := range entries {
		if !strings.Contains(entry.Name(), "@") {
			t.Errorf("file %s was not renamed", entry.Name())
		}
	}
	if got := mustLoadManifest(t, store).Len(); got != 3 {
		t.Errorf("manifest entries = %d, want 3", got)
	}
}

// TestMigrateSkipsUnreadableFiles keeps one bad file from stranding the rest.
func TestMigrateSkipsUnreadableFiles(t *testing.T) {
	store, _ := hashedNameStore(t,
		makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil))
	junk := filepath.Join(store.ReviewsDir, SessionsDirname, "0000000000000000.json")
	writeTestFile(t, junk, "not json at all")

	if err := store.maybeMigrate(); err != nil {
		t.Fatalf("one corrupt file must not fail the migration: %v", err)
	}
	if !fileExists(t, junk) {
		t.Error("an unreadable file should be left alone, not deleted")
	}
	if got := mustLoadManifest(t, store).Len(); got != 1 {
		t.Errorf("manifest entries = %d, want 1 (the readable session)", got)
	}
}

// TestFreshStoreNeedsNoMigration guards the common path: a new install must
// not pay for, or be confused by, a migration that has nothing to do.
func TestFreshStoreNeedsNoMigration(t *testing.T) {
	store := newTestStore(t)
	path := mustSave(t, store,
		makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil))

	manifest := mustLoadManifest(t, store)
	if manifest.Version != ManifestVersion {
		t.Errorf("a fresh store should write the current version, got %q", manifest.Version)
	}
	if !strings.Contains(filepath.Base(path), "@") {
		t.Errorf("a fresh store should write labelled names, got %s", filepath.Base(path))
	}
}

// ---- Filename sanitation ----

func TestSanitizeFilenamePart(t *testing.T) {
	cases := []struct{ in, want string }{
		{"infrashift-mrman@main-worktree-abc1234", "infrashift-mrman@main-worktree-abc1234"},
		{"owner/repo@feat/x", "owner-repo@feat-x"},
		{"gh:github.com/o/r", "gh-github.com-o-r"},
		{"a///b", "a-b"},                  // runs collapse
		{"--lead--trail--", "lead-trail"}, // including internal runs
		{".hidden", "hidden"},             // never a dotfile
		{"", "session"},                   // never empty
		{"///", "session"},
		{"sp ace\ttab", "sp-ace-tab"},
		{"emoji-🙂-here", "emoji-here"},
		{"~abc1234-worktree-abc1234", "~abc1234-worktree-abc1234"},
	}
	for _, tc := range cases {
		if got := sanitizeFilenamePart(tc.in); got != tc.want {
			t.Errorf("sanitizeFilenamePart(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSanitizeFilenamePartTruncates keeps a pathological branch name from
// exceeding the filesystem's name limit; the identity hash the caller appends
// is what actually distinguishes the file.
func TestSanitizeFilenamePartTruncates(t *testing.T) {
	got := sanitizeFilenamePart(strings.Repeat("x", 500))
	if len(got) > sessionLabelMaxLen {
		t.Errorf("length = %d, want <= %d", len(got), sessionLabelMaxLen)
	}
}

// TestLongBranchNameStillProducesUsableFilename is the end-to-end version:
// a 500-character branch must still save.
func TestLongBranchNameStillProducesUsableFilename(t *testing.T) {
	store := newTestStore(t)
	branch := strings.Repeat("very-long-branch-", 30)
	path := mustSave(t, store,
		makeLocalSession(t, makeRepo(t), "abc1234", &branch, model.SourceWorkingTree, nil))

	if name := filepath.Base(path); len(name) > 255 {
		t.Errorf("filename is %d bytes, too long for common filesystems: %s", len(name), name)
	}
	if !fileExists(t, path) {
		t.Error("the session did not save")
	}
}

// TestDistinctSessionsWithIdenticalLabels covers labels that sanitize alike:
// the identity hash has to keep them apart. Two checkouts of the same repo on
// the same branch are exactly this case.
func TestDistinctSessionsWithIdenticalLabels(t *testing.T) {
	store := newTestStore(t)
	a := mustSave(t, store,
		makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil))
	b := mustSave(t, store,
		makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil))

	if a == b {
		t.Fatal("two checkouts of the same repo must not share a session file")
	}
	if filepath.Base(a) == filepath.Base(b) {
		t.Error("identical labels must still yield distinct filenames")
	}
}
