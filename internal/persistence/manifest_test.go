package persistence

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/model"
)

func localEntry(updatedAt time.Time, canonical, anchor string) ManifestEntry {
	c := canonical
	return ManifestEntry{
		Path:              "sessions/aaaabbbbccccdddd.json",
		Kind:              LocalManifestKind(),
		UpdatedAt:         updatedAt,
		CanonicalRepoPath: &c,
		Display: DisplayMetadata{
			CommentCount:  3,
			ReviewedCount: 1,
			FileCount:     4,
			Anchor:        anchor,
		},
	}
}

func prEntry(updatedAt time.Time, number uint64, headSHA string) ManifestEntry {
	return ManifestEntry{
		Path:      "sessions/eeeeffff00001111.json",
		Kind:      PrManifestKind(number, headSHA),
		UpdatedAt: updatedAt,
		Display: DisplayMetadata{
			Anchor: "pr/125",
		},
	}
}

func TestManifestStartsEmpty(t *testing.T) {
	m := NewManifest()
	if !m.IsEmpty() || m.Len() != 0 {
		t.Fatalf("new manifest should be empty, len=%d", m.Len())
	}
	if m.Version != ManifestVersion {
		t.Fatalf("version = %q, want %q", m.Version, ManifestVersion)
	}
}

func TestInsertAndRetrieveLocalEntry(t *testing.T) {
	m := NewManifest()
	e := localEntry(time.Now(), "/Users/agavra/tuicr", "main")
	m.Upsert("agavra/tuicr@main/worktree", e)

	if m.Len() != 1 {
		t.Fatalf("len = %d, want 1", m.Len())
	}
	got := m.GetLocal("agavra/tuicr@main/worktree", "/Users/agavra/tuicr")
	if got == nil || !reflect.DeepEqual(*got, e) {
		t.Fatalf("GetLocal = %+v, want %+v", got, e)
	}
	if m.GetLocal("agavra/tuicr@main/worktree", "/Users/agavra/other-checkout") != nil {
		t.Fatal("GetLocal must not match a different canonical path")
	}
}

func TestReplaceLocalEntryWithSameCanonicalPath(t *testing.T) {
	m := NewManifest()
	first := localEntry(time.Now(), "/Users/agavra/tuicr", "main")
	second := first
	second.Display.CommentCount = 99

	m.Upsert("agavra/tuicr@main/worktree", first)
	m.Upsert("agavra/tuicr@main/worktree", second)

	if m.Len() != 1 {
		t.Fatalf("len = %d, want 1", m.Len())
	}
	got := m.GetLocal("agavra/tuicr@main/worktree", "/Users/agavra/tuicr")
	if got == nil || got.Display.CommentCount != 99 {
		t.Fatalf("replacement did not stick: %+v", got)
	}
}

func TestStoreMultipleLocalEntriesForSameSlugDifferentCheckouts(t *testing.T) {
	m := NewManifest()
	m.Upsert("agavra/tuicr@main/worktree", localEntry(time.Now(), "/Users/agavra/work/tuicr", "main"))
	m.Upsert("agavra/tuicr@main/worktree", localEntry(time.Now(), "/Users/agavra/oss/tuicr", "main"))

	if m.Len() != 2 {
		t.Fatalf("len = %d, want 2", m.Len())
	}
	if m.GetLocal("agavra/tuicr@main/worktree", "/Users/agavra/work/tuicr") == nil {
		t.Fatal("work checkout entry missing")
	}
	if m.GetLocal("agavra/tuicr@main/worktree", "/Users/agavra/oss/tuicr") == nil {
		t.Fatal("oss checkout entry missing")
	}
}

func TestRemoveAllEntriesForSlug(t *testing.T) {
	m := NewManifest()
	m.Upsert("agavra/tuicr@main/worktree", localEntry(time.Now(), "/a", "main"))
	m.Upsert("agavra/tuicr@main/worktree", localEntry(time.Now(), "/b", "main"))

	removed := m.RemoveAll("agavra/tuicr@main/worktree")
	if len(removed) != 2 {
		t.Fatalf("removed %d entries, want 2", len(removed))
	}
	if !m.IsEmpty() {
		t.Fatal("manifest should be empty after RemoveAll")
	}
	if len(m.RemoveAll("missing")) != 0 {
		t.Fatal("RemoveAll of a missing slug should return no entries")
	}
}

func TestReplacePrEntryWhenHeadAdvances(t *testing.T) {
	m := NewManifest()
	m.Upsert("gh:github.com/agavra/tuicr/pr/125", prEntry(time.Now(), 125, "abc12345"))
	m.Upsert("gh:github.com/agavra/tuicr/pr/125", prEntry(time.Now(), 125, "def67890"))

	if m.Len() != 1 {
		t.Fatalf("len = %d, want 1", m.Len())
	}
	entry := m.GetPr("gh:github.com/agavra/tuicr/pr/125")
	if entry == nil {
		t.Fatal("PR entry missing")
	}
	_, headSHA, ok := entry.Kind.PrDetails()
	if !ok || headSHA != "def67890" {
		t.Fatalf("head = %q ok=%v, want def67890", headSHA, ok)
	}
}

func TestEmptyManifestWhenFileMissing(t *testing.T) {
	m, err := LoadManifest(t.TempDir())
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if !m.IsEmpty() {
		t.Fatal("missing manifest file should load as empty")
	}
}

func TestRoundtripManifestThroughDisk(t *testing.T) {
	dir := t.TempDir()
	m := NewManifest()
	local := localEntry(time.Now().UTC().Truncate(time.Second), "/Users/agavra/tuicr", "main")
	m.Upsert("agavra/tuicr@main/worktree", local)
	m.Upsert("gh:github.com/agavra/tuicr/pr/148", prEntry(time.Now().UTC().Truncate(time.Second), 148, "abc12345"))

	if err := SaveManifest(dir, m); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	loaded, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	if loaded.Len() != 2 {
		t.Fatalf("len = %d, want 2", loaded.Len())
	}
	got := loaded.GetLocal("agavra/tuicr@main/worktree", "/Users/agavra/tuicr")
	if got == nil || !reflect.DeepEqual(*got, local) {
		t.Fatalf("local entry did not roundtrip: %+v", got)
	}
	if loaded.GetPr("gh:github.com/agavra/tuicr/pr/148") == nil {
		t.Fatal("PR entry did not roundtrip")
	}
}

func TestOverwriteManifestAtomically(t *testing.T) {
	dir := t.TempDir()
	first := NewManifest()
	first.Upsert("a", localEntry(time.Now(), "/a", "main"))
	if err := SaveManifest(dir, first); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	second := NewManifest()
	second.Upsert("a", localEntry(time.Now(), "/a", "main"))
	second.Upsert("b", localEntry(time.Now(), "/b", "feature"))
	if err := SaveManifest(dir, second); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	loaded, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded.Len() != 2 {
		t.Fatalf("len = %d, want 2", loaded.Len())
	}
	if _, err := os.Stat(filepath.Join(dir, ManifestFilename+".tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("tmp file should be cleaned up after rename")
	}
}

func TestCorruptedManifestSurfacesError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ManifestFilename), []byte("not json {"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(dir)
	var corrupted *errs.CorruptedSession
	if !errors.As(err, &corrupted) {
		t.Fatalf("err = %v, want *errs.CorruptedSession", err)
	}
}

func TestWalkOnlyJSONFilesExcludingManifestAndActive(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "local", "abcd")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(sub, "a.json"), "{}")
	writeTestFile(t, filepath.Join(sub, "b.txt"), "ignored")
	writeTestFile(t, filepath.Join(dir, ManifestFilename), "{}")
	writeTestFile(t, filepath.Join(dir, activeSessionsFilename), "{}")

	var visited []string
	if err := walkJSON(dir, func(path string) {
		visited = append(visited, filepath.Base(path))
	}); err != nil {
		t.Fatalf("walkJSON: %v", err)
	}

	if !reflect.DeepEqual(visited, []string{"a.json"}) {
		t.Fatalf("visited = %v, want [a.json]", visited)
	}
}

func TestRebuildManifestViaExtractor(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "local", "abcd")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(sub, "a.json"), "{}")
	writeTestFile(t, filepath.Join(sub, "b.json"), "{}")

	m, err := RebuildFromFiles(dir, func(path string) (string, ManifestEntry, bool) {
		name := filepath.Base(path)
		name = name[:len(name)-len(".json")]
		return "repo@" + name + "/worktree", localEntry(time.Now(), "/repos/"+name, name), true
	})
	if err != nil {
		t.Fatalf("RebuildFromFiles: %v", err)
	}

	if m.Len() != 2 {
		t.Fatalf("len = %d, want 2", m.Len())
	}
	if m.GetLocal("repo@a/worktree", "/repos/a") == nil {
		t.Fatal("entry for a missing")
	}
	if m.GetLocal("repo@b/worktree", "/repos/b") == nil {
		t.Fatal("entry for b missing")
	}
}

func TestRebuildSkipsFilesExtractorRejects(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.json"), "{}")
	writeTestFile(t, filepath.Join(dir, "skip.json"), "{}")

	m, err := RebuildFromFiles(dir, func(path string) (string, ManifestEntry, bool) {
		if filepath.Base(path) == "skip.json" {
			return "", ManifestEntry{}, false
		}
		return "kept", localEntry(time.Now(), "/kept", "main"), true
	})
	if err != nil {
		t.Fatalf("RebuildFromFiles: %v", err)
	}
	if m.Len() != 1 {
		t.Fatalf("len = %d, want 1", m.Len())
	}
}

func TestRebuildEmptyManifestWhenDirMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	m, err := RebuildFromFiles(missing, func(string) (string, ManifestEntry, bool) {
		t.Fatal("extractor should not be called")
		return "", ManifestEntry{}, false
	})
	if err != nil {
		t.Fatalf("RebuildFromFiles: %v", err)
	}
	if !m.IsEmpty() {
		t.Fatal("manifest should be empty when the dir is missing")
	}
}

func TestEntryFromSessionLocalMetadata(t *testing.T) {
	branch := "main"
	sess := model.NewReviewSession("/tmp/mrman-test-repo-that-does-not-exist", "abc1234def", &branch, model.SourceWorkingTree)
	sess.AddFile("src/a.go", model.StatusModified, 0)
	sess.AddFile("src/b.go", model.StatusModified, 0)
	sess.Files["src/a.go"].Reviewed = true

	entry := entryFromSession(sess, "sessions/deadbeefdeadbeef.json", "main")

	if entry.Path != "sessions/deadbeefdeadbeef.json" {
		t.Fatalf("path = %q", entry.Path)
	}
	if !entry.Kind.IsLocal() {
		t.Fatal("kind should be local")
	}
	if !entry.UpdatedAt.Equal(sess.UpdatedAt) {
		t.Fatalf("updated_at = %v, want %v", entry.UpdatedAt, sess.UpdatedAt)
	}
	if entry.CanonicalRepoPath == nil || *entry.CanonicalRepoPath != "/tmp/mrman-test-repo-that-does-not-exist" {
		t.Fatalf("canonical path = %v", entry.CanonicalRepoPath)
	}
	want := DisplayMetadata{CommentCount: 0, ReviewedCount: 1, FileCount: 2, Anchor: "main"}
	if entry.Display != want {
		t.Fatalf("display = %+v, want %+v", entry.Display, want)
	}
}

func TestEntryFromSessionPrMetadata(t *testing.T) {
	branch := "reviews"
	sess := model.NewReviewSession("forge:github.com/agavra/tuicr", "abcdef0123456789", &branch, model.SourcePullRequest)
	sess.PrSessionKey = &forgetypes.PrSessionKey{
		Repository: forgetypes.Repository{
			Kind: forgetypes.KindGitHub, Host: "github.com", Owner: "agavra", Name: "tuicr",
		},
		Number:  125,
		HeadSHA: "abcdef0123456789",
	}
	sess.ReviewComments = append(sess.ReviewComments,
		model.NewComment("hi", model.CommentTypeFromID("note"), nil))

	entry := entryFromSession(sess, "sessions/cafecafecafecafe.json", "pr/125")

	number, headSHA, ok := entry.Kind.PrDetails()
	if !ok || number != 125 || headSHA != "abcdef0123456789" {
		t.Fatalf("kind = %+v, want pr 125/abcdef0123456789", entry.Kind)
	}
	if entry.CanonicalRepoPath != nil {
		t.Fatalf("canonical path = %v, want nil for PR", *entry.CanonicalRepoPath)
	}
	if entry.Display.CommentCount != 1 {
		t.Fatalf("comment count = %d, want 1", entry.Display.CommentCount)
	}
}

func TestManifestKindMarshalsTaggedUnion(t *testing.T) {
	localJSON, err := json.Marshal(LocalManifestKind())
	if err != nil {
		t.Fatal(err)
	}
	if string(localJSON) != `{"type":"local"}` {
		t.Fatalf("local kind = %s", localJSON)
	}

	prJSON, err := json.Marshal(PrManifestKind(125, "abc123"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(prJSON, &decoded); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"type": "pr", "number": float64(125), "head_sha": "abc123"}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("pr kind = %v, want %v", decoded, want)
	}
}

func TestManifestKindUnmarshalsTaggedUnion(t *testing.T) {
	var local ManifestKind
	if err := json.Unmarshal([]byte(`{"type":"local"}`), &local); err != nil {
		t.Fatal(err)
	}
	if !local.IsLocal() || local.IsPr() {
		t.Fatalf("kind = %+v, want local", local)
	}

	var pr ManifestKind
	if err := json.Unmarshal([]byte(`{"type":"pr","number":42,"head_sha":"ff00"}`), &pr); err != nil {
		t.Fatal(err)
	}
	number, headSHA, ok := pr.PrDetails()
	if !ok || number != 42 || headSHA != "ff00" {
		t.Fatalf("kind = %+v, want pr 42/ff00", pr)
	}

	var bad ManifestKind
	if err := json.Unmarshal([]byte(`{"type":"mystery"}`), &bad); err == nil {
		t.Fatal("unknown kind tag should be an error")
	}
	if err := json.Unmarshal([]byte(`[]`), &bad); err == nil {
		t.Fatal("non-object kind should be an error")
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
