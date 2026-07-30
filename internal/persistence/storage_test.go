package persistence

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adrg/xdg"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/model"
)

// ---- Helpers ----

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return &Store{ReviewsDir: filepath.Join(t.TempDir(), "reviews")}
}

func strp(s string) *string { return &s }

func makeRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	return repo
}

// makeRepoWithOrigin creates a real checkout with an origin remote, so slug
// derivation resolves a genuine owner/repo instead of falling back to the
// directory basename.
func makeRepoWithOrigin(t *testing.T, originURL string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "checkout")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"remote", "add", "origin", originURL},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

func makeLocalSession(t *testing.T, repoPath, baseCommit string, branch *string,
	src model.SessionDiffSource, commitRange []string) *model.ReviewSession {
	t.Helper()
	s := model.NewReviewSession(repoPath, baseCommit, branch, src)
	s.CommitRange = commitRange
	s.AddFile("src/main.go", model.StatusModified, 0)
	return s
}

func makePrKey(number uint64, headSHA string) *forgetypes.PrSessionKey {
	return &forgetypes.PrSessionKey{
		Repository: forgetypes.Repository{
			Kind: forgetypes.KindGitHub, Host: "github.com", Owner: "agavra", Name: "tuicr",
		},
		Number:  number,
		HeadSHA: headSHA,
	}
}

func makePrSession(key *forgetypes.PrSessionKey) *model.ReviewSession {
	s := model.NewReviewSession(
		fmt.Sprintf("forge:%s/%s/%s", key.Repository.Host, key.Repository.Owner, key.Repository.Name),
		key.HeadSHA, strp("reviews"), model.SourcePullRequest)
	k := *key
	s.PrSessionKey = &k
	return s
}

func mustSave(t *testing.T, s *Store, sess *model.ReviewSession) string {
	t.Helper()
	path, err := s.SaveSession(sess)
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return path
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	exists, err := pathExists(path)
	if err != nil {
		t.Fatal(err)
	}
	return exists
}

// ---- Save/load round trips ----

func TestRoundtripLocalSession(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil)

	path := mustSave(t, store, sess)
	loaded, err := store.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	if loaded.ID != sess.ID || loaded.BaseCommit != sess.BaseCommit {
		t.Fatalf("loaded %q/%q, want %q/%q", loaded.ID, loaded.BaseCommit, sess.ID, sess.BaseCommit)
	}
}

func TestDeleteEmptySessionAndManifestEntry(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	sess := makeLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree, nil)
	path := mustSave(t, store, sess)

	deleted, err := store.DeleteSessionIfEmpty(path)
	if err != nil || !deleted {
		t.Fatalf("DeleteSessionIfEmpty = %v, %v; want true", deleted, err)
	}

	if fileExists(t, path) {
		t.Fatal("session file should be gone")
	}
	_, _, found, err := store.LoadLatestSessionForContext(repo, strp("main"), "abc1234", model.SourceWorkingTree, nil)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("manifest entry should be pruned with the file")
	}
}

func TestDeleteReviewedOnlySessionUnconditionally(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil)
	// Mark the file reviewed: DeleteSessionIfEmpty refuses this, but
	// DeleteSession discards it anyway.
	sess.Files["src/main.go"].Reviewed = true
	path := mustSave(t, store, sess)

	deleted, err := store.DeleteSessionIfEmpty(path)
	if err != nil || deleted {
		t.Fatalf("DeleteSessionIfEmpty = %v, %v; want false", deleted, err)
	}
	if !fileExists(t, path) {
		t.Fatal("reviewed session must survive empty-delete")
	}

	deleted, err = store.DeleteSession(path)
	if err != nil || !deleted {
		t.Fatalf("DeleteSession = %v, %v; want true", deleted, err)
	}
	if fileExists(t, path) {
		t.Fatal("reviewed-only session should be discarded")
	}
}

func TestDeleteMissingSessionReportsFalse(t *testing.T) {
	store := newTestStore(t)
	missing := filepath.Join(store.ReviewsDir, "does-not-exist.json")

	if deleted, err := store.DeleteSession(missing); err != nil || deleted {
		t.Fatalf("DeleteSession = %v, %v; want false, nil", deleted, err)
	}
	if deleted, err := store.DeleteSessionIfEmpty(missing); err != nil || deleted {
		t.Fatalf("DeleteSessionIfEmpty = %v, %v; want false, nil", deleted, err)
	}
}

func TestKeepSessionWithCommentsWhenDeletingIfEmpty(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil)
	sess.ReviewComments = append(sess.ReviewComments,
		model.NewComment("keep me", model.CommentTypeFromID("note"), nil))
	path := mustSave(t, store, sess)

	deleted, err := store.DeleteSessionIfEmpty(path)
	if err != nil || deleted {
		t.Fatalf("DeleteSessionIfEmpty = %v, %v; want false", deleted, err)
	}
	if !fileExists(t, path) {
		t.Fatal("commented session must survive")
	}
}

func TestKeepSessionWithReviewedHunksWhenDeletingIfEmpty(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil)
	sess.Files["src/main.go"].ToggleHunkReviewed("stable-hunk")
	path := mustSave(t, store, sess)

	deleted, err := store.DeleteSessionIfEmpty(path)
	if err != nil || deleted {
		t.Fatalf("DeleteSessionIfEmpty = %v, %v; want false", deleted, err)
	}
	if !fileExists(t, path) {
		t.Fatal("hunk-reviewed session must survive")
	}
}

// assertHashSuffix checks the identity part of a session filename: the last
// "-<16hex>" before .json. The label in front is decoration and deliberately
// unpinned beyond the specific assertions at each call site.
func assertHashSuffix(t *testing.T, path string) {
	t.Helper()
	name := strings.TrimSuffix(filepath.Base(path), ".json")
	idx := strings.LastIndex(name, "-")
	if idx < 0 {
		t.Fatalf("no hash suffix in %s", name)
	}
	hash := name[idx+1:]
	if len(hash) != 16 {
		t.Fatalf("expected a 16-hex identity suffix, got %q in %s", hash, name)
	}
	for _, r := range hash {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("identity suffix %q is not hex", hash)
		}
	}
}

func TestSaveUnderFlatSessionsDirForLocal(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil)

	path := mustSave(t, store, sess)

	if filepath.Base(filepath.Dir(path)) != SessionsDirname {
		t.Fatalf("expected %s/ parent, got %s", SessionsDirname, path)
	}
	assertHashSuffix(t, path)
	// The label says what the session is without opening it: the test repo has
	// no origin, so the repo token is the bare directory name.
	name := filepath.Base(path)
	for _, want := range []string{"repo@", "main", "worktree", "abc1234"} {
		if !strings.Contains(name, want) {
			t.Errorf("filename %s should carry %q", name, want)
		}
	}
}

func TestSaveUnderFlatSessionsDirForPr(t *testing.T) {
	store := newTestStore(t)
	path := mustSave(t, store, makePrSession(makePrKey(125, "abcdef0123456789")))

	if filepath.Base(filepath.Dir(path)) != SessionsDirname {
		t.Fatalf("expected %s/ parent, got %s", SessionsDirname, path)
	}
	assertHashSuffix(t, path)
	// A PR session leads with the same repo token a local one would, so one
	// glob reaches both; the host and PR number follow.
	name := filepath.Base(path)
	for _, want := range []string{"agavra-tuicr@", "github.com", "pr-125"} {
		if !strings.Contains(name, want) {
			t.Errorf("filename %s should carry %q", name, want)
		}
	}
}

// TestLocalAndPrSessionsShareARepoGlob is the pruning contract: one pattern
// per repo has to reach everything mrman stored for it, or "delete this
// repo's reviews" means opening files to find out which they are.
func TestLocalAndPrSessionsShareARepoGlob(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepoWithOrigin(t, "https://github.com/agavra/tuicr.git")

	localPath := mustSave(t, store,
		makeLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree, nil))
	prPath := mustSave(t, store, makePrSession(makePrKey(125, "abcdef0123456789")))

	matches, err := filepath.Glob(
		filepath.Join(store.ReviewsDir, SessionsDirname, "agavra-tuicr@*"))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, m := range matches {
		found[m] = true
	}
	if !found[localPath] || !found[prPath] {
		t.Errorf("glob matched %v, want both %s and %s", matches, localPath, prPath)
	}
}

func TestDistinctPathsForDifferentPrHeads(t *testing.T) {
	store := newTestStore(t)
	pathA := mustSave(t, store, makePrSession(makePrKey(125, "abcdef0123456789")))
	pathB := mustSave(t, store, makePrSession(makePrKey(125, "9999999999999999")))

	if pathA == pathB {
		t.Fatal("PR sessions with different heads must hash to different files")
	}
}

func TestUpdateManifestOnSave(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	sess := makeLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree, nil)
	mustSave(t, store, sess)

	manifest, err := LoadManifest(store.ReviewsDir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	sl, err := sessionSlug(sess)
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.GetLocal(sl.String(), canonicalPath(repo))
	if entry == nil {
		t.Fatal("manifest entry missing after save")
	}
	if !entry.Kind.IsLocal() {
		t.Fatal("entry kind should be local")
	}
	if entry.Display.FileCount != 1 {
		t.Fatalf("file count = %d, want 1", entry.Display.FileCount)
	}
	if entry.Display.Anchor != "main" {
		t.Fatalf("anchor = %q, want main", entry.Display.Anchor)
	}
}

func TestSessionPathMatchesSaveLocation(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil)

	predicted, err := store.SessionPath(sess)
	if err != nil {
		t.Fatalf("SessionPath: %v", err)
	}
	saved := mustSave(t, store, sess)
	if predicted != saved {
		t.Fatalf("SessionPath %q != SaveSession %q", predicted, saved)
	}
}

// ---- Lookup ----

func TestReturnNotFoundForUnknownContext(t *testing.T) {
	store := newTestStore(t)
	_, _, found, err := store.LoadLatestSessionForContext(
		makeRepo(t), strp("main"), "head", model.SourceWorkingTree, nil)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("unknown context should not resolve a session")
	}
}

func TestLoadSessionWhenHeadMatches(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	sess := makeLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree, nil)
	mustSave(t, store, sess)

	_, loaded, found, err := store.LoadLatestSessionForContext(
		repo, strp("main"), "abc1234", model.SourceWorkingTree, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("session for current head should load")
	}
	if loaded.ID != sess.ID {
		t.Fatalf("loaded %q, want %q", loaded.ID, sess.ID)
	}
}

func TestNotLoadWorktreeSessionAfterHeadAdvances(t *testing.T) {
	// Regression parity with tuicr #378: a worktree session is keyed by
	// (branch, head), so this exact-context lookup does not resolve across a
	// HEAD change.
	//
	// This is no longer the whole story, and it is not what keeps stale
	// comments honest — AdoptSessionForNewHead deliberately carries such a
	// session forward, and anchor re-validation is what makes that safe. What
	// this test still pins is that the exact lookup stays exact: adoption must
	// remain a distinct, deliberate step rather than something a caller gets
	// by accident.
	store := newTestStore(t)
	repo := makeRepo(t)
	mustSave(t, store, makeLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree, nil))

	_, _, found, err := store.LoadLatestSessionForContext(
		repo, strp("main"), "new-head", model.SourceWorkingTree, nil)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("advancing HEAD must yield a fresh session, not the previous one")
	}
}

func TestIgnoreSessionsWithDifferentDiffSource(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	mustSave(t, store, makeLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree, nil))

	_, _, found, err := store.LoadLatestSessionForContext(
		repo, strp("main"), "abc1234", model.SourceStaged, nil)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("a different diff source must not resolve the session")
	}
}

func TestPullRequestSourceContextLookupReturnsNotFound(t *testing.T) {
	store := newTestStore(t)
	_, _, found, err := store.LoadLatestSessionForContext(
		makeRepo(t), strp("main"), "head", model.SourcePullRequest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("PullRequest source must yield not-found, not an error")
	}
}

func TestRequireCommitRangeMatch(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	rangeA := []string{"c1", "c0"}
	rangeB := []string{"c3", "c2"}

	sessA := makeLocalSession(t, repo, "c1", strp("main"), model.SourceCommitRange, rangeA)
	mustSave(t, store, sessA)
	sessB := makeLocalSession(t, repo, "c3", strp("main"), model.SourceCommitRange, rangeB)
	mustSave(t, store, sessB)

	_, loadedA, foundA, err := store.LoadLatestSessionForContext(
		repo, strp("main"), "c1", model.SourceCommitRange, rangeA)
	if err != nil || !foundA {
		t.Fatalf("range A lookup: found=%v err=%v", foundA, err)
	}
	if loadedA.ID != sessA.ID {
		t.Fatalf("loaded %q, want %q", loadedA.ID, sessA.ID)
	}

	_, loadedB, foundB, err := store.LoadLatestSessionForContext(
		repo, strp("main"), "c3", model.SourceCommitRange, rangeB)
	if err != nil || !foundB {
		t.Fatalf("range B lookup: found=%v err=%v", foundB, err)
	}
	if loadedB.ID != sessB.ID {
		t.Fatalf("loaded %q, want %q", loadedB.ID, sessB.ID)
	}
}

func TestDisambiguateTwoCheckoutsWithSameRepoName(t *testing.T) {
	store := newTestStore(t)
	base := t.TempDir()
	repoA := filepath.Join(base, "a", "same-repo")
	repoB := filepath.Join(base, "b", "same-repo")
	for _, repo := range []string{repoA, repoB} {
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Same branch *and* same HEAD in both checkouts: this is the collision
	// the manifest's canonical_repo_path must break, since the slug itself
	// is identical.
	sessA := makeLocalSession(t, repoA, "head-x", strp("main"), model.SourceWorkingTree, nil)
	sessB := makeLocalSession(t, repoB, "head-x", strp("main"), model.SourceWorkingTree, nil)
	mustSave(t, store, sessA)
	mustSave(t, store, sessB)

	slugA, err := sessionSlug(sessA)
	if err != nil {
		t.Fatal(err)
	}
	slugB, err := sessionSlug(sessB)
	if err != nil {
		t.Fatal(err)
	}
	if slugA.String() != slugB.String() {
		t.Fatalf("slugs should collide: %q vs %q", slugA, slugB)
	}

	_, loadedA, foundA, err := store.LoadLatestSessionForContext(
		repoA, strp("main"), "head-x", model.SourceWorkingTree, nil)
	if err != nil || !foundA {
		t.Fatalf("repo A lookup: found=%v err=%v", foundA, err)
	}
	if loadedA.ID != sessA.ID {
		t.Fatalf("repo A resolved %q, want %q", loadedA.ID, sessA.ID)
	}

	_, loadedB, foundB, err := store.LoadLatestSessionForContext(
		repoB, strp("main"), "head-x", model.SourceWorkingTree, nil)
	if err != nil || !foundB {
		t.Fatalf("repo B lookup: found=%v err=%v", foundB, err)
	}
	if loadedB.ID != sessB.ID {
		t.Fatalf("repo B resolved %q, want %q", loadedB.ID, sessB.ID)
	}
}

func TestLoadSessionCorruptReportsCorrupted(t *testing.T) {
	store := newTestStore(t)
	path := filepath.Join(t.TempDir(), "bad.json")
	writeTestFile(t, path, "not json {")

	_, err := store.LoadSession(path)
	var corrupted *errs.CorruptedSession
	if !errors.As(err, &corrupted) {
		t.Fatalf("err = %v, want *errs.CorruptedSession", err)
	}
}

// ---- PR sessions ----

func TestRoundtripPrSession(t *testing.T) {
	store := newTestStore(t)
	key := makePrKey(125, "abcdef0123456789")
	path := mustSave(t, store, makePrSession(key))

	loadedPath, loaded, found, err := store.LoadPrSession(key)
	if err != nil || !found {
		t.Fatalf("LoadPrSession: found=%v err=%v", found, err)
	}
	if loadedPath != path {
		t.Fatalf("path %q, want %q", loadedPath, path)
	}
	if loaded.PrSessionKey == nil || *loaded.PrSessionKey != *key {
		t.Fatalf("key %+v, want %+v", loaded.PrSessionKey, key)
	}
}

func TestReturnNotFoundWhenHeadChangesForPr(t *testing.T) {
	store := newTestStore(t)
	mustSave(t, store, makePrSession(makePrKey(125, "abcdef0123456789")))

	_, _, found, err := store.LoadPrSession(makePrKey(125, "9999999999999999"))
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("a changed head must not surface the old session")
	}
}

func TestSeparatePrSessionsByNumber(t *testing.T) {
	store := newTestStore(t)
	keyA := makePrKey(125, "abcdef0123456789")
	keyB := makePrKey(148, "abcdef0123456789")
	mustSave(t, store, makePrSession(keyA))
	mustSave(t, store, makePrSession(keyB))

	_, loadedA, foundA, err := store.LoadPrSession(keyA)
	if err != nil || !foundA {
		t.Fatalf("PR 125 lookup: found=%v err=%v", foundA, err)
	}
	_, loadedB, foundB, err := store.LoadPrSession(keyB)
	if err != nil || !foundB {
		t.Fatalf("PR 148 lookup: found=%v err=%v", foundB, err)
	}
	if *loadedA.PrSessionKey != *keyA || *loadedB.PrSessionKey != *keyB {
		t.Fatal("PR sessions crossed numbers")
	}
}

func TestSkipPrFilesInLocalContextLookup(t *testing.T) {
	store := newTestStore(t)
	mustSave(t, store, makePrSession(makePrKey(125, "abcdef0123456789")))

	_, _, found, err := store.LoadLatestSessionForContext(
		makeRepo(t), strp("main"), "head", model.SourceWorkingTree, nil)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("PR sessions must not satisfy local context lookups")
	}
}

// ---- Repo-coordinate selector ----

func TestFindPrSessionByRepoCoordinate(t *testing.T) {
	store := newTestStore(t)
	key := makePrKey(125, "abcdef0123456789")
	mustSave(t, store, makePrSession(key))

	listed, err := store.ListSessions("agavra/tuicr")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed %d sessions, want 1", len(listed))
	}
	if listed[0].Slug != "gh:github.com/agavra/tuicr/pr/125" {
		t.Fatalf("slug = %q", listed[0].Slug)
	}
	if listed[0].Kind != SummaryKindPr {
		t.Fatalf("kind = %q, want pr", listed[0].Kind)
	}
	if listed[0].Anchor != "pr/125" {
		t.Fatalf("anchor = %q, want pr/125", listed[0].Anchor)
	}
}

func TestNotMatchPrSessionForUnrelatedCoordinate(t *testing.T) {
	store := newTestStore(t)
	mustSave(t, store, makePrSession(makePrKey(125, "abcdef0123456789")))

	listed, err := store.ListSessions("other/project")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("listed %d sessions, want 0", len(listed))
	}
}

func TestMatchLocalAndPrSessionsSharingRepoNameByCoordinate(t *testing.T) {
	store := newTestStore(t)

	// A local checkout whose directory name is the repo name (no origin, so
	// the local slug carries no owner) plus a PR session for the same repo.
	// An "agavra/tuicr" coordinate should surface both.
	localRepo := filepath.Join(t.TempDir(), "tuicr")
	if err := os.MkdirAll(localRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustSave(t, store, makeLocalSession(t, localRepo, "abc1234", strp("main"), model.SourceWorkingTree, nil))
	mustSave(t, store, makePrSession(makePrKey(125, "abcdef0123456789")))

	listed, err := store.ListSessions("agavra/tuicr")
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	var hasPrSlug, hasLocalSlug bool
	for _, summary := range listed {
		kinds = append(kinds, summary.Kind)
		if summary.Slug == "gh:github.com/agavra/tuicr/pr/125" {
			hasPrSlug = true
		}
		if strings.HasPrefix(summary.Slug, "tuicr@main/worktree") {
			hasLocalSlug = true
		}
	}
	if len(listed) != 2 || !hasPrSlug || !hasLocalSlug {
		t.Fatalf("listed kinds %v, want one local and one pr", kinds)
	}
}

func TestMatchLocalSessionByCanonicalPathInPathMode(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)
	mustSave(t, store, makeLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree, nil))
	// A second checkout's session must not leak into the listing.
	mustSave(t, store, makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil))

	listed, err := store.ListSessions(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed %d sessions, want 1", len(listed))
	}
	if listed[0].Kind != SummaryKindLocal {
		t.Fatalf("kind = %q, want local", listed[0].Kind)
	}
}

func TestDirectorySelectorUsesOriginCoordinate(t *testing.T) {
	store := newTestStore(t)
	repo := makeRepo(t)

	oldOrigin := originRemoteURL
	originRemoteURL = func(dir string) (string, error) {
		if canonicalPath(dir) != canonicalPath(repo) {
			return "", errors.New("no origin")
		}
		return "git@github.com:agavra/tuicr.git", nil
	}
	t.Cleanup(func() { originRemoteURL = oldOrigin })

	mustSave(t, store, makeLocalSession(t, repo, "abc1234", strp("main"), model.SourceWorkingTree, nil))
	mustSave(t, store, makePrSession(makePrKey(125, "abcdef0123456789")))

	listed, err := store.ListSessions(repo)
	if err != nil {
		t.Fatal(err)
	}
	// The checkout's own session matches by canonical path; the PR session
	// matches through the coordinate derived from the origin remote.
	if len(listed) != 2 {
		t.Fatalf("listed %d sessions, want 2 (local by path + pr by origin coordinate)", len(listed))
	}
}

func TestListAllSessionsNewestFirstWithActiveFlag(t *testing.T) {
	store := newTestStore(t)
	older := makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil)
	older.UpdatedAt = time.Now().Add(-time.Hour)
	mustSave(t, store, older)

	newer := makePrSession(makePrKey(125, "abcdef0123456789"))
	newer.UpdatedAt = time.Now()
	newerPath := mustSave(t, store, newer)

	if err := store.MarkSessionActive(newer, newerPath); err != nil {
		t.Fatalf("MarkSessionActive: %v", err)
	}

	listed, err := store.ListAllSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed %d sessions, want 2", len(listed))
	}
	if listed[0].Kind != SummaryKindPr || listed[1].Kind != SummaryKindLocal {
		t.Fatalf("order = [%s, %s], want newest (pr) first", listed[0].Kind, listed[1].Kind)
	}
	if !listed[0].Active {
		t.Fatal("marked session should list as active")
	}
	if listed[1].Active {
		t.Fatal("unmarked session should not list as active")
	}
}

// ---- Update / merge-on-write ----

func TestUpdateSession(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil)
	path := mustSave(t, store, sess)

	updated, err := store.UpdateSession(path, func(s *model.ReviewSession) error {
		s.SessionNotes = strp("updated notes")
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	if updated.SessionNotes == nil || *updated.SessionNotes != "updated notes" {
		t.Fatal("returned session missing the update")
	}

	loaded, err := store.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionNotes == nil || *loaded.SessionNotes != "updated notes" {
		t.Fatal("persisted session missing the update")
	}
}

func TestUpdateSessionPropagatesCallbackError(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil)
	path := mustSave(t, store, sess)

	boom := errors.New("boom")
	if _, err := store.UpdateSession(path, func(*model.ReviewSession) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if !fileExists(t, filepath.Join(store.ReviewsDir, lockFilename)) {
		return // lock released, as expected
	}
	t.Fatal("lock must be released after a failed update")
}

func TestSaveSessionByIdentityMergesPersisted(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil)

	// First save: nothing persisted yet.
	path1, saved1, err := store.SaveSessionByIdentity(sess, func(persisted *model.ReviewSession) (*model.ReviewSession, error) {
		if persisted != nil {
			t.Fatal("first save must see no persisted session")
		}
		return sess, nil
	})
	if err != nil {
		t.Fatalf("SaveSessionByIdentity: %v", err)
	}
	if saved1.ID != sess.ID {
		t.Fatalf("saved %q, want %q", saved1.ID, sess.ID)
	}

	// Second save: the persisted session is loaded and offered for merging.
	path2, saved2, err := store.SaveSessionByIdentity(sess, func(persisted *model.ReviewSession) (*model.ReviewSession, error) {
		if persisted == nil {
			t.Fatal("second save must see the persisted session")
		}
		if persisted.ID != sess.ID {
			t.Fatalf("persisted %q, want %q", persisted.ID, sess.ID)
		}
		persisted.SessionNotes = strp("merged")
		return persisted, nil
	})
	if err != nil {
		t.Fatalf("SaveSessionByIdentity: %v", err)
	}
	if path1 != path2 {
		t.Fatalf("identity paths differ: %q vs %q", path1, path2)
	}
	if saved2.SessionNotes == nil || *saved2.SessionNotes != "merged" {
		t.Fatal("merge result not returned")
	}

	loaded, err := store.LoadSession(path2)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionNotes == nil || *loaded.SessionNotes != "merged" {
		t.Fatal("merge result not persisted")
	}
}

func TestSaveSessionByIdentityPropagatesCallbackError(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil)

	boom := errors.New("boom")
	_, _, err := store.SaveSessionByIdentity(sess, func(*model.ReviewSession) (*model.ReviewSession, error) {
		return nil, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

// ---- Default store ----

func TestNewDefaultStore(t *testing.T) {
	tmp := t.TempDir()
	oldValue, hadValue := os.LookupEnv("XDG_DATA_HOME")
	if err := os.Setenv("XDG_DATA_HOME", tmp); err != nil {
		t.Fatal(err)
	}
	xdg.Reload()
	t.Cleanup(func() {
		if hadValue {
			_ = os.Setenv("XDG_DATA_HOME", oldValue)
		} else {
			_ = os.Unsetenv("XDG_DATA_HOME")
		}
		xdg.Reload()
	})

	store, err := NewDefaultStore()
	if err != nil {
		t.Fatalf("NewDefaultStore: %v", err)
	}
	want := filepath.Join(tmp, "mrman", "reviews")
	if store.ReviewsDir != want {
		t.Fatalf("ReviewsDir = %q, want %q", store.ReviewsDir, want)
	}
	if !fileExists(t, store.ReviewsDir) {
		t.Fatal("reviews dir should exist")
	}
}

// ---- Migration ----

func TestMigratePreFlatLayoutOnFirstRun(t *testing.T) {
	store := newTestStore(t)
	if err := os.MkdirAll(store.ReviewsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-flat artifacts: a top-level *.json from the original flat layout
	// and a tree-layout subdir from the intermediate layout.
	stray := filepath.Join(store.ReviewsDir, "old_session.json")
	writeTestFile(t, stray, `{"legacy":true}`)
	treeSubdir := filepath.Join(store.ReviewsDir, "local", "abcd")
	if err := os.MkdirAll(treeSubdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(treeSubdir, "foo.json"), "{}")

	mustSave(t, store, makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil))

	if fileExists(t, stray) {
		t.Fatal("pre-flat artifacts should have moved during migration")
	}
	if !fileExists(t, filepath.Join(store.ReviewsDir, SessionsDirname)) {
		t.Fatal("sessions/ should exist after migration")
	}
	backup := store.ReviewsDir + ".bak1"
	if !fileExists(t, backup) {
		t.Fatalf("expected backup dir %s", backup)
	}
	if !fileExists(t, filepath.Join(backup, "old_session.json")) {
		t.Fatal("backup should carry the pre-flat artifacts")
	}
}

func TestMigrationPicksNextFreeBackupSuffix(t *testing.T) {
	store := newTestStore(t)
	if err := os.MkdirAll(store.ReviewsDir+".bak1", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.ReviewsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(store.ReviewsDir, "old.json"), "{}")

	if err := store.maybeMigrate(); err != nil {
		t.Fatalf("maybeMigrate: %v", err)
	}
	if !fileExists(t, filepath.Join(store.ReviewsDir+".bak2", "old.json")) {
		t.Fatal("migration should fall through to .bak2")
	}
}

func TestNoMigrationWhenSessionsDirAlreadyPresent(t *testing.T) {
	store := newTestStore(t)
	if err := os.MkdirAll(filepath.Join(store.ReviewsDir, SessionsDirname), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveManifest(store.ReviewsDir, NewManifest()); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(store.ReviewsDir, "stray.json")
	writeTestFile(t, stray, "{}")

	mustSave(t, store, makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil))

	if !fileExists(t, stray) {
		t.Fatal("stray .json must survive when sessions/ already exists")
	}
}

func TestNoMigrationWhenReviewsDirEmpty(t *testing.T) {
	store := newTestStore(t)
	if err := os.MkdirAll(store.ReviewsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := store.maybeMigrate(); err != nil {
		t.Fatalf("maybeMigrate: %v", err)
	}

	if !fileExists(t, store.ReviewsDir) {
		t.Fatal("empty reviews dir should be left in place")
	}
	if fileExists(t, store.ReviewsDir+".bak1") {
		t.Fatal("empty reviews dir must not be backed up")
	}
}

func TestNoMigrationWhenReviewsDirMissing(t *testing.T) {
	store := newTestStore(t)
	if err := store.maybeMigrate(); err != nil {
		t.Fatalf("maybeMigrate: %v", err)
	}
	if fileExists(t, store.ReviewsDir) {
		t.Fatal("maybeMigrate must not create the reviews dir")
	}
}
