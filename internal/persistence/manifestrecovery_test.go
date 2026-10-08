package persistence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// corruptManifest overwrites index.json with bytes that are not JSON.
func corruptManifest(t *testing.T, store *Store) {
	t.Helper()
	writeTestFile(t, filepath.Join(store.ReviewsDir, ManifestFilename), "{not json")
}

// TestCorruptManifestKeepsEverySession: a corrupt index.json used to load as
// an empty manifest, and the next save wrote that back with one entry, so
// every other session vanished from listings and lookups. The index is
// derived data; the session files can rebuild it.
func TestCorruptManifestKeepsEverySession(t *testing.T) {
	store := newTestStore(t)
	mustSave(t, store, makeLocalSession(t, makeRepo(t), "aaa1111", new("main"), model.SourceWorkingTree, nil))
	mustSave(t, store, makeLocalSession(t, makeRepo(t), "bbb2222", new("main"), model.SourceWorkingTree, nil))
	corruptManifest(t, store)

	// A read sees every session while the file is still corrupt.
	if got := listCount(t, store); got != 2 {
		t.Fatalf("listing over a corrupt manifest = %d sessions, want 2", got)
	}

	// A write keeps them, and keeps the corrupt bytes for inspection.
	mustSave(t, store, makeLocalSession(t, makeRepo(t), "ccc3333", new("main"), model.SourceWorkingTree, nil))
	if got := listCount(t, store); got != 3 {
		t.Fatalf("after a save over a corrupt manifest: %d sessions, want 3", got)
	}
	backups, err := filepath.Glob(filepath.Join(store.ReviewsDir, ManifestFilename+".corrupt-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("corrupt manifest backups = %v, %v; want one", backups, err)
	}
	if data, _ := os.ReadFile(backups[0]); string(data) != "{not json" {
		t.Errorf("backup holds %q, want the corrupt bytes", data)
	}
}

// TestUnreadableManifestFailsInsteadOfEmptying: an I/O error is not
// corruption, and must not be papered over with an empty manifest either.
func TestUnreadableManifestFailsInsteadOfEmptying(t *testing.T) {
	store := newTestStore(t)
	mustSave(t, store, makeLocalSession(t, makeRepo(t), "aaa1111", new("main"), model.SourceWorkingTree, nil))
	index := filepath.Join(store.ReviewsDir, ManifestFilename)
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0o755); err != nil { // reading a directory fails with EISDIR
		t.Fatal(err)
	}

	_, err := store.SaveSession(makeLocalSession(t, makeRepo(t), "bbb2222", new("main"), model.SourceWorkingTree, nil))
	if err == nil || !strings.Contains(err.Error(), ManifestFilename) {
		t.Fatalf("save over an unreadable manifest: err = %v, want an error naming it", err)
	}
}

func listCount(t *testing.T, store *Store) int {
	t.Helper()
	all, err := store.ListAllSessions()
	if err != nil {
		t.Fatalf("ListAllSessions: %v", err)
	}
	return len(all)
}

func mustLoadManifest(t *testing.T, store *Store) *Manifest {
	t.Helper()
	m, err := store.loadManifest()
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	return m
}
