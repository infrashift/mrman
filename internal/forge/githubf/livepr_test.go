package githubf_test

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/forge"
	_ "github.com/infrashift/mrman/internal/forge/drivers" // register drivers
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/model"
)

// TestLivePullRequest exercises every Forge method against a real GitHub
// pull request. Fakes cannot catch a driver that builds a valid-looking
// request GitHub then rejects, which is exactly the class of bug that
// survives to the first real review.
//
// Opt in with:
//
//	MRMAN_LIVE_PR=infrashift/scratch#1 go test ./internal/forge/githubf/
//
// It only reads, so it is safe against any pull request you can see;
// submitting is covered separately because it writes.
func TestLivePullRequest(t *testing.T) {
	target := os.Getenv("MRMAN_LIVE_PR")
	if target == "" {
		t.Skip("set MRMAN_LIVE_PR=owner/repo#N to run against a real pull request")
	}

	slug, numberText, ok := strings.Cut(target, "#")
	if !ok {
		t.Fatalf("MRMAN_LIVE_PR=%q, want owner/repo#N", target)
	}
	owner, name, ok := strings.Cut(slug, "/")
	if !ok {
		t.Fatalf("MRMAN_LIVE_PR=%q, want owner/repo#N", target)
	}
	number, err := strconv.ParseUint(numberText, 10, 64)
	if err != nil {
		t.Fatalf("bad pull request number in %q: %v", target, err)
	}

	repo := forgetypes.Repository{
		Kind: forgetypes.KindGitHub, Host: "github.com", Owner: owner, Name: name,
	}
	backend, err := forge.ForRepository(repo, config.Default().Forge)
	if err != nil {
		t.Fatalf("resolve driver: %v", err)
	}
	ctx := context.Background()

	caps := backend.Capabilities()
	t.Logf("capabilities: %+v", caps)

	// --- listing (the selector's Pull Requests tab) ---
	page, err := backend.ListPullRequests(ctx, forge.ListQuery{
		Repository: repo, Scope: forge.ScopeOpen, PageSize: 50,
	})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	found := false
	for _, row := range page.Items {
		if row.Number == number {
			found = true
			t.Logf("listing row: #%d %q by %s [%s]", row.Number, row.Title, row.Author, row.HeadRefName)
		}
	}
	if !found {
		t.Errorf("pull request #%d is open but absent from the listing", number)
	}

	// The review-requested scope must at least round-trip.
	if _, err := backend.ListPullRequests(ctx, forge.ListQuery{
		Repository: repo, Scope: forge.ScopeReviewRequested, PageSize: 50,
	}); err != nil {
		t.Errorf("ListPullRequests(review-requested): %v", err)
	}

	// --- opening ---
	details, err := backend.GetPullRequest(ctx, forge.Target{Repository: &repo, Number: number})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	t.Logf("pr #%d %q head=%.7s base=%.7s readonly=%v",
		details.Number, details.Title, details.HeadSHA, details.BaseSHA, details.IsReadOnly())

	patch, err := backend.GetDiff(ctx, details)
	if err != nil {
		t.Fatalf("GetDiff: %v", err)
	}
	if !strings.HasPrefix(patch, "diff --git") {
		t.Errorf("GetDiff returned something that is not a unified diff: %.60q", patch)
	}

	commits, err := backend.ListCommits(ctx, details)
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) == 0 {
		t.Fatal("ListCommits returned nothing")
	}
	t.Logf("commits (oldest first): %d", len(commits))
	for _, c := range commits {
		t.Logf("  %.7s %s", c.OID, c.Summary)
	}

	// --- context expansion (M8.3's forge-backed provider) ---
	path := firstChangedPath(t, patch)
	lineReq := forge.FileLinesRequest{
		Repository: repo, BaseSHA: details.BaseSHA, HeadSHA: details.HeadSHA,
		Path: path, Status: model.StatusModified, Side: forge.FileSideHead,
	}
	count, err := backend.FileLineCount(ctx, lineReq)
	if err != nil {
		t.Fatalf("FileLineCount(%s): %v", path, err)
	}
	if count == 0 {
		t.Fatalf("FileLineCount(%s) = 0", path)
	}
	full := lineReq
	full.StartLine, full.EndLine = 1, uint32(count)
	lines, err := backend.FetchFileLines(ctx, full)
	if err != nil {
		t.Fatalf("FetchFileLines(%s): %v", path, err)
	}
	if len(lines) != count {
		t.Errorf("FetchFileLines returned %d lines, FileLineCount said %d", len(lines), count)
	}
	t.Logf("context: %s has %d lines", path, count)

	// --- existing discussions (M8.2) ---
	threads, err := backend.ListReviewThreads(ctx, details)
	if err != nil {
		t.Fatalf("ListReviewThreads: %v", err)
	}
	t.Logf("threads: %d", len(threads))
	for i := range threads {
		th := &threads[i]
		line := "none"
		if th.Line != nil {
			line = strconv.FormatUint(uint64(*th.Line), 10)
		}
		root := ""
		if r := th.Root(); r != nil {
			root = r.Author + ": " + firstLine(r.Body)
		}
		t.Logf("  %s:%s resolved=%v outdated=%v — %s", th.Path, line, th.IsResolved, th.IsOutdated, root)
	}

	if caps.ReviewSummaries {
		summaries, err := backend.ListReviewSummaries(ctx, details)
		if err != nil {
			t.Fatalf("ListReviewSummaries: %v", err)
		}
		t.Logf("review summaries: %d", len(summaries))
		for _, s := range summaries {
			t.Logf("  %s [%s] %s", s.Author, s.State, firstLine(s.Body))
		}
	}

	if caps.CommitScopedReviews {
		meta, err := backend.ReviewMetadata(ctx, details)
		if err != nil {
			t.Fatalf("ReviewMetadata: %v", err)
		}
		t.Logf("viewer=%q reviews=%d", meta.ViewerLogin, len(meta.Reviews))
		for _, r := range meta.Reviews {
			t.Logf("  %s @ %.7s", r.Author, r.CommitOID)
		}
	}

	// --- per-commit range diff (M9's ( / ) cycling) ---
	if caps.CommitRangeDiff && len(commits) > 1 {
		// The newest commit alone: its parent is the one before it.
		startSHA := commits[len(commits)-2].OID
		endSHA := commits[len(commits)-1].OID
		rangePatch, err := backend.GetCommitRangeDiff(ctx, details, startSHA, endSHA)
		if err != nil {
			t.Fatalf("GetCommitRangeDiff(%.7s..%.7s): %v", startSHA, endSHA, err)
		}
		if len(rangePatch) >= len(patch) {
			t.Errorf("a single-commit range diff (%d bytes) is not smaller than the whole MR (%d)",
				len(rangePatch), len(patch))
		}
		t.Logf("range diff %.7s..%.7s: %d bytes vs %d for the whole MR",
			startSHA, endSHA, len(rangePatch), len(patch))
	}
}

// firstChangedPath pulls the first b/ path out of a unified diff.
func firstChangedPath(t *testing.T, patch string) string {
	t.Helper()
	for line := range strings.SplitSeq(patch, "\n") {
		if after, ok := strings.CutPrefix(line, "+++ b/"); ok {
			return after
		}
	}
	t.Fatal("no changed path in the patch")
	return ""
}

func firstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before + "…"
	}
	return s
}
