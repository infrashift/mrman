package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

const someHash uint64 = 0xdeadbeef

func testSession() *ReviewSession {
	return NewReviewSession("/repo", "abc123", nil, SourceWorkingTree)
}

func testHunk(start uint32, content string) DiffHunk {
	old, newL := start, start
	return DiffHunk{
		Header: fmt.Sprintf("@@ -%d,1 +%d,1 @@", start, start),
		Lines: []DiffLine{{
			Origin:    OriginContext,
			Content:   content,
			OldLineno: &old,
			NewLineno: &newL,
		}},
		OldStart: start, OldCount: 1, NewStart: start, NewCount: 1,
	}
}

func testDiffFile(path string, hunks []DiffHunk) *DiffFile {
	p := path
	return &DiffFile{
		NewPath:     &p,
		Status:      StatusModified,
		Hunks:       hunks,
		ContentHash: ComputeContentHash(hunks),
	}
}

func TestClearEmptySession(t *testing.T) {
	s := testSession()
	cleared, unreviewed := s.ClearComments(ClearCommentsAndReviewed)
	if cleared != 0 || unreviewed != 0 {
		t.Fatalf("got %d, %d", cleared, unreviewed)
	}
}

func TestClearReviewLevelComments(t *testing.T) {
	s := testSession()
	s.ReviewComments = append(s.ReviewComments,
		NewComment("note", CommentTypeFromID("note"), nil),
		NewComment("issue", CommentTypeFromID("issue"), nil))
	cleared, unreviewed := s.ClearComments(ClearCommentsAndReviewed)
	if cleared != 2 || unreviewed != 0 || len(s.ReviewComments) != 0 {
		t.Fatalf("cleared=%d unreviewed=%d remaining=%d", cleared, unreviewed, len(s.ReviewComments))
	}
}

func TestClearFileAndLineComments(t *testing.T) {
	s := testSession()
	s.AddFile("src/main.go", StatusModified, someHash)
	f := s.File("src/main.go")
	f.AddFileComment(NewComment("comment", CommentTypeFromID("note"), nil))
	f.AddLineComment(10, NewComment("line", CommentTypeFromID("note"), nil))

	cleared, _ := s.ClearComments(ClearCommentsAndReviewed)
	if cleared != 2 {
		t.Fatalf("cleared = %d", cleared)
	}
	if len(f.FileComments) != 0 || len(f.LineComments) != 0 {
		t.Fatal("comments must be cleared")
	}
}

func TestClearResetsReviewedAndCounts(t *testing.T) {
	s := testSession()
	s.AddFile("a.go", StatusModified, someHash)
	s.AddFile("b.go", StatusAdded, someHash)
	s.AddFile("pending.go", StatusModified, someHash)
	s.File("a.go").Reviewed = true
	s.File("b.go").Reviewed = true

	cleared, unreviewed := s.ClearComments(ClearCommentsAndReviewed)
	if cleared != 0 || unreviewed != 2 {
		t.Fatalf("cleared=%d unreviewed=%d", cleared, unreviewed)
	}
	if s.IsFileReviewed("a.go") || s.IsFileReviewed("b.go") {
		t.Fatal("reviewed flags must reset")
	}
}

func TestClearCommentsOnlyKeepsReviewed(t *testing.T) {
	s := testSession()
	s.AddFile("a.go", StatusModified, someHash)
	s.File("a.go").Reviewed = true
	s.File("a.go").AddFileComment(NewComment("c", CommentTypeFromID("note"), nil))

	cleared, unreviewed := s.ClearComments(ClearCommentsOnly)
	if cleared != 1 || unreviewed != 0 {
		t.Fatalf("cleared=%d unreviewed=%d", cleared, unreviewed)
	}
	if !s.IsFileReviewed("a.go") {
		t.Fatal("CommentsOnly must keep reviewed state")
	}
}

func TestAddFileInvalidation(t *testing.T) {
	s := testSession()
	// New file: not invalidated.
	if s.AddFile("x.go", StatusModified, 100) {
		t.Fatal("fresh add must not invalidate")
	}
	s.File("x.go").Reviewed = true
	// Same hash: reviewed survives.
	if s.AddFile("x.go", StatusModified, 100) {
		t.Fatal("same hash must not invalidate")
	}
	if !s.IsFileReviewed("x.go") {
		t.Fatal("reviewed must survive same hash")
	}
	// Changed hash: reviewed resets, invalidation reported.
	if !s.AddFile("x.go", StatusModified, 200) {
		t.Fatal("changed hash must invalidate")
	}
	if s.IsFileReviewed("x.go") {
		t.Fatal("reviewed must reset on content change")
	}
}

func TestAddFileLegacyNilHashAlwaysInvalidates(t *testing.T) {
	s := testSession()
	s.AddFile("legacy.go", StatusModified, 100)
	s.File("legacy.go").Reviewed = true
	s.File("legacy.go").ContentHash = nil // legacy session entry without hash
	if !s.AddFile("legacy.go", StatusModified, 100) {
		t.Fatal("nil legacy hash must invalidate")
	}
	if s.IsFileReviewed("legacy.go") {
		t.Fatal("reviewed must reset")
	}
}

func TestHunkReviewKeys(t *testing.T) {
	// Unique hunks get content-only keys.
	f := testDiffFile("src/main.go", []DiffHunk{testHunk(10, "alpha"), testHunk(50, "beta")})
	keys := f.HunkReviewKeys()
	if len(keys) != 2 {
		t.Fatalf("got %d keys", len(keys))
	}
	for _, k := range keys {
		if !strings.HasPrefix(k, "hunk-content-v1:") || !strings.HasSuffix(k, ":0") {
			t.Errorf("unique hunk key %q must be content-keyed", k)
		}
	}

	// Duplicate hunks fall back to span keys.
	dup := testDiffFile("src/dup.go", []DiffHunk{testHunk(10, "same"), testHunk(50, "same")})
	dupKeys := dup.HunkReviewKeys()
	if dupKeys[0] == dupKeys[1] {
		t.Fatal("duplicate hunks must get distinct keys")
	}
	for _, k := range dupKeys {
		if !strings.HasPrefix(k, "hunk-span-v1:") {
			t.Errorf("duplicate hunk key %q must be span-keyed", k)
		}
	}

	// Content key is stable across line-number shifts (the whole point).
	shifted := testDiffFile("src/main.go", []DiffHunk{testHunk(99, "alpha")})
	shiftedKey, _ := shifted.HunkReviewKey(0)
	origKey, _ := f.HunkReviewKey(0)
	if shiftedKey != origKey {
		t.Fatalf("content key must ignore line numbers: %q vs %q", shiftedKey, origKey)
	}

	if _, ok := f.HunkReviewKey(5); ok {
		t.Fatal("out-of-range index must return ok=false")
	}
}

func TestAddDiffFilePrunesStaleHunkKeys(t *testing.T) {
	s := testSession()
	f := testDiffFile("src/main.go", []DiffHunk{testHunk(10, "same")})
	key, _ := f.HunkReviewKey(0)
	s.AddDiffFile(f)
	s.File("src/main.go").ToggleHunkReviewed(key)
	if !s.IsHunkReviewed("src/main.go", key) {
		t.Fatal("hunk must be reviewed")
	}

	// Re-register with different content: the old key is pruned.
	changed := testDiffFile("src/main.go", []DiffHunk{testHunk(10, "different")})
	s.AddDiffFile(changed)
	if s.IsHunkReviewed("src/main.go", key) {
		t.Fatal("stale hunk key must be pruned")
	}

	// Preserving variant keeps foreign keys.
	s.File("src/main.go").ToggleHunkReviewed(key)
	s.AddDiffFilePreservingHunks(testDiffFile("src/main.go", []DiffHunk{testHunk(10, "third")}))
	if !s.IsHunkReviewed("src/main.go", key) {
		t.Fatal("preserving variant must keep hunk keys")
	}
}

func TestHasReviewedStateAndCounts(t *testing.T) {
	s := testSession()
	if s.HasReviewedState() || s.HasComments() || s.ReviewedCount() != 0 {
		t.Fatal("empty session must report nothing")
	}
	s.AddFile("a.go", StatusModified, someHash)
	s.File("a.go").ReviewedHunks.Insert("hunk-content-v1:0000000000000001:0")
	if !s.HasReviewedState() {
		t.Fatal("hunk review counts as reviewed state")
	}
	s.File("a.go").AddLineComment(3, NewComment("c", CommentTypeFromID("note"), nil))
	if !s.HasComments() {
		t.Fatal("line comment counts as comments")
	}
}

// tuicr's LEGACY_SESSION_JSON (v1.2, pre-PR-3): every newer field must
// default correctly.
func TestLegacySessionDeserializes(t *testing.T) {
	legacy := `{
		"id": "abc-uuid",
		"version": "1.2",
		"repo_path": "/tmp/test-repo",
		"branch_name": "main",
		"base_commit": "deadbeef",
		"diff_source": "working_tree",
		"created_at": "2026-05-01T12:00:00Z",
		"updated_at": "2026-05-01T12:00:00Z",
		"review_comments": [],
		"files": {},
		"session_notes": null
	}`
	var s ReviewSession
	if err := json.Unmarshal([]byte(legacy), &s); err != nil {
		t.Fatal(err)
	}
	if s.ID != "abc-uuid" || s.BaseCommit != "deadbeef" || s.DiffSource != SourceWorkingTree {
		t.Fatalf("identity lost: %+v", s)
	}
	if s.PrSessionKey != nil || s.CommitRange != nil || s.CommitSelectionRange != nil {
		t.Fatal("newer optionals must default nil")
	}
	if s.RemoteCommentsVisibility != forgetypes.VisibilityUnresolved {
		t.Fatalf("visibility = %q, want unresolved", s.RemoteCommentsVisibility)
	}
}

func TestPrSessionRoundTrip(t *testing.T) {
	branch := "reviews"
	s := NewReviewSession("forge:github.com/agavra/tuicr", "abcdef0123456789", &branch, SourcePullRequest)
	key := forgetypes.PrSessionKey{
		Repository: forgetypes.Repository{
			Kind: forgetypes.KindGitHub, Host: "github.com", Owner: "agavra", Name: "tuicr",
		},
		Number:  125,
		HeadSHA: "abcdef0123456789",
	}
	s.PrSessionKey = &key
	s.RemoteCommentsVisibility = forgetypes.VisibilityAll
	s.CommitSelectionRange = &IndexRange{1, 3}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"commit_selection_range":[1,3]`) {
		t.Fatalf("tuple range must serialize as array: %s", data)
	}
	var restored ReviewSession
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.PrSessionKey == nil || *restored.PrSessionKey != key {
		t.Fatalf("pr key lost: %+v", restored.PrSessionKey)
	}
	if restored.DiffSource != SourcePullRequest ||
		restored.RemoteCommentsVisibility != forgetypes.VisibilityAll {
		t.Fatalf("got %+v", restored)
	}
	if restored.CommitSelectionRange == nil || *restored.CommitSelectionRange != (IndexRange{1, 3}) {
		t.Fatalf("selection range lost: %+v", restored.CommitSelectionRange)
	}
}

func TestCommentCommitIDRoundTripInSession(t *testing.T) {
	s := testSession()
	s.AddFile("src/lib.go", StatusModified, someHash)
	side := LineSideNew
	f := s.File("src/lib.go")
	f.AddFileComment(NewComment("file note", CommentTypeFromID("note"), nil).WithCommitID("aaa111"))
	f.AddLineComment(42, NewComment("line note", CommentTypeFromID("issue"), &side).WithCommitID("bbb222"))

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	// Integer-keyed line comment maps serialize with string keys, as serde.
	if !strings.Contains(string(data), `"42":[`) {
		t.Fatalf("line comments must be string-keyed: %s", data)
	}
	var restored ReviewSession
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	rf := restored.File("src/lib.go")
	if rf == nil || len(rf.FileComments) != 1 || *rf.FileComments[0].CommitID != "aaa111" {
		t.Fatal("file comment commit_id must round-trip")
	}
	line := rf.LineComments[42]
	if len(line) != 1 || *line[0].CommitID != "bbb222" {
		t.Fatal("line comment commit_id must round-trip")
	}
}

func TestLegacyFileReviewDefaultsReviewedHunks(t *testing.T) {
	legacy := `{
		"path": "src/main.rs",
		"reviewed": false,
		"status": "modified",
		"file_comments": [],
		"line_comments": {},
		"content_hash": 123
	}`
	var f FileReview
	if err := json.Unmarshal([]byte(legacy), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.ReviewedHunks) != 0 {
		t.Fatal("reviewed_hunks must default empty")
	}
	if f.ContentHash == nil || *f.ContentHash != 123 {
		t.Fatalf("content_hash lost: %v", f.ContentHash)
	}
}

func TestReviewedHunksRoundTripSorted(t *testing.T) {
	s := testSession()
	f := testDiffFile("src/main.go", []DiffHunk{testHunk(10, "one"), testHunk(50, "two")})
	s.AddDiffFile(f)
	for _, k := range f.HunkReviewKeys() {
		s.File("src/main.go").ToggleHunkReviewed(k)
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var restored ReviewSession
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	hunks := restored.File("src/main.go").ReviewedHunks
	if len(hunks) != 2 {
		t.Fatalf("got %d hunks", len(hunks))
	}
	for i := 1; i < len(hunks); i++ {
		if hunks[i-1] > hunks[i] {
			t.Fatal("reviewed_hunks must stay sorted")
		}
	}
	for _, k := range f.HunkReviewKeys() {
		if !restored.IsHunkReviewed("src/main.go", k) {
			t.Errorf("key %q lost in round trip", k)
		}
	}
}

func TestStringSetOperations(t *testing.T) {
	var s StringSet
	if !s.Insert("b") || !s.Insert("a") || !s.Insert("c") {
		t.Fatal("inserts must report true")
	}
	if s.Insert("a") {
		t.Fatal("duplicate insert must report false")
	}
	if got := []string(s); got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("not sorted: %v", got)
	}
	if !s.Remove("b") || s.Remove("zz") {
		t.Fatal("remove results wrong")
	}
	s.Retain(func(v string) bool { return v == "c" })
	if len(s) != 1 || s[0] != "c" {
		t.Fatalf("retain failed: %v", s)
	}
}

func TestDiffFileHelpers(t *testing.T) {
	old, newP := "old.go", "new.go"
	f := &DiffFile{OldPath: &old, NewPath: &newP}
	if f.DisplayPath() != "new.go" {
		t.Fatal("DisplayPath must prefer new path")
	}
	f.NewPath = nil
	if f.DisplayPath() != "old.go" {
		t.Fatal("DisplayPath must fall back to old path")
	}

	oldNo, newNo := uint32(5), uint32(7)
	file := &DiffFile{
		Status: StatusModified,
		Hunks: []DiffHunk{{
			Lines: []DiffLine{
				{Origin: OriginDeletion, Content: "gone", OldLineno: &oldNo},
				{Origin: OriginAddition, Content: "here", NewLineno: &newNo},
			},
			OldStart: 5, OldCount: 1, NewStart: 7, NewCount: 1,
		}},
	}
	if n, ok := file.FirstValidLine(LineSideNew); !ok || n != 7 {
		t.Fatalf("FirstValidLine(New) = %d, %v", n, ok)
	}
	if n, ok := file.FirstValidLine(LineSideOld); !ok || n != 5 {
		t.Fatalf("FirstValidLine(Old) = %d, %v", n, ok)
	}
	adds, dels := file.Stat()
	if adds != 1 || dels != 1 {
		t.Fatalf("Stat = %d, %d", adds, dels)
	}
	if file.MaxLineno() != 8 {
		t.Fatalf("MaxLineno = %d", file.MaxLineno())
	}

	binary := &DiffFile{IsBinary: true}
	if _, ok := binary.FirstValidLine(LineSideNew); ok {
		t.Fatal("binary files have no valid line")
	}
}

func TestFileStatusChars(t *testing.T) {
	cases := map[FileStatus]byte{
		StatusAdded: 'A', StatusModified: 'M', StatusDeleted: 'D',
		StatusRenamed: 'R', StatusCopied: 'C', FileStatus("weird"): '?',
	}
	for status, want := range cases {
		if got := status.Char(); got != want {
			t.Errorf("%s.Char() = %c, want %c", status, got, want)
		}
	}
}
