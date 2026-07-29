package git

import (
	"errors"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/errs"
)

const branchTipsKey = "git for-each-ref --format=%(objectname)%00%(refname:short) refs/heads"

const commitLogOutput = "id1\x00sha1\x00Alice\x001700000000\x00summary one\n\nbody first\nbody second\n\x1e\n" +
	"id2\x00sha2\x00Bob\x001699000000\x00initial\n\x1e\n"

func TestRecentCommits(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git rev-parse --verify HEAD": {stdout: "id1\n"},
		branchTipsKey:                 {stdout: "id1\x00main\nid1\x00dev\n"},
		"git log --skip=5 --max-count=10 " + commitFormat: {stdout: commitLogOutput},
	})

	commits, err := backend.RecentCommits(5, 10)
	if err != nil {
		t.Fatalf("RecentCommits failed: %v", err)
	}

	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
	first := commits[0]
	if first.ID != "id1" || first.ShortID != "sha1" || first.Author != "Alice" {
		t.Errorf("first = %+v", first)
	}
	if first.Summary != "summary one" {
		t.Errorf("summary = %q", first.Summary)
	}
	if first.Body == nil || *first.Body != "body first\nbody second" {
		t.Errorf("body = %v", first.Body)
	}
	if first.BranchName == nil || *first.BranchName != "dev" {
		t.Errorf("branch = %v, want dev (sorted first)", first.BranchName)
	}
	if !first.Time.Equal(time.Unix(1700000000, 0)) {
		t.Errorf("time = %v", first.Time)
	}

	second := commits[1]
	if second.ID != "id2" || second.Author != "Bob" || second.Summary != "initial" {
		t.Errorf("second = %+v", second)
	}
	if second.Body != nil {
		t.Errorf("second body = %v, want nil", second.Body)
	}
	if second.BranchName != nil {
		t.Errorf("second branch = %v, want nil", second.BranchName)
	}
}

func TestRecentCommitsUnbornHead(t *testing.T) {
	backend, runner := newTestBackend(t, "/repo", map[string]response{
		"git rev-parse --verify HEAD": {stderr: "fatal: needed a single revision", err: exitError(128)},
	})

	commits, err := backend.RecentCommits(0, 10)
	if err != nil {
		t.Fatalf("RecentCommits failed: %v", err)
	}

	if commits != nil {
		t.Errorf("commits = %v, want none", commits)
	}
	if len(runner.calls) != 1 {
		t.Errorf("calls = %v, want only the HEAD probe", runner.calls)
	}
}

func TestCommitsInfo(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		branchTipsKey: {},
		"git show -s " + commitFormat + " id1 id2": {stdout: commitLogOutput},
	})

	commits, err := backend.CommitsInfo([]string{"id1", "id2"})
	if err != nil {
		t.Fatalf("CommitsInfo failed: %v", err)
	}

	if len(commits) != 2 || commits[0].ID != "id1" || commits[1].ID != "id2" {
		t.Fatalf("commits = %+v", commits)
	}
}

func TestCommitsInfoEmpty(t *testing.T) {
	backend, runner := newTestBackend(t, "/repo", nil)

	commits, err := backend.CommitsInfo(nil)
	if err != nil {
		t.Fatalf("CommitsInfo failed: %v", err)
	}

	if commits != nil || len(runner.calls) != 0 {
		t.Errorf("commits = %v, calls = %v", commits, runner.calls)
	}
}

func TestParseCommitRecords(t *testing.T) {
	commits := parseCommitRecords("\n\x1e\nid-only\x1e", nil)
	if len(commits) != 0 {
		t.Errorf("records without short id should be skipped: %+v", commits)
	}

	commits = parseCommitRecords("id\x00short", nil)
	if len(commits) != 1 {
		t.Fatalf("commits = %+v", commits)
	}
	if commits[0].Author != "Unknown" {
		t.Errorf("author = %q, want Unknown fallback", commits[0].Author)
	}
	if commits[0].Summary != "(no message)" {
		t.Errorf("summary = %q, want (no message)", commits[0].Summary)
	}
	if !commits[0].Time.Equal(time.Unix(0, 0)) {
		t.Errorf("time = %v", commits[0].Time)
	}
}

func TestParseCommitMessage(t *testing.T) {
	summary, body := parseCommitMessage("")
	if summary != "(no message)" || body != nil {
		t.Errorf("empty = %q %v", summary, body)
	}

	summary, body = parseCommitMessage("only summary\n")
	if summary != "only summary" || body != nil {
		t.Errorf("summary-only = %q %v", summary, body)
	}

	summary, body = parseCommitMessage("summary\n\n\nbody a\nbody b\n")
	if summary != "summary" || body == nil || *body != "body a\nbody b" {
		t.Errorf("with body = %q %v", summary, body)
	}

	summary, body = parseCommitMessage("summary\n\n   \n")
	if summary != "summary" || body != nil {
		t.Errorf("blank body = %q %v", summary, body)
	}
}

func TestResolveRevisionRangeSingle(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git rev-parse --verify HEAD^{commit}": {stdout: "c2\n"},
		"git rev-parse c2^":                    {stdout: "c1\n"},
	})

	rng, err := backend.ResolveRevisionRange("HEAD")
	if err != nil {
		t.Fatalf("ResolveRevisionRange failed: %v", err)
	}

	if len(rng.CommitIDs) != 1 || rng.CommitIDs[0] != "c2" {
		t.Errorf("commit ids = %v", rng.CommitIDs)
	}
	if !rng.Target.Explicit || rng.Target.Head != "c2" {
		t.Errorf("target = %+v", rng.Target)
	}
	if rng.Target.Base == nil || *rng.Target.Base != "c1" {
		t.Errorf("base = %v", rng.Target.Base)
	}
}

func TestResolveRevisionRangeSingleRootCommit(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git rev-parse --verify root^{commit}": {stdout: "c0\n"},
		"git rev-parse c0^":                    {stderr: "fatal: bad revision", err: exitError(128)},
	})

	rng, err := backend.ResolveRevisionRange("root")
	if err != nil {
		t.Fatalf("ResolveRevisionRange failed: %v", err)
	}

	if rng.Target.Base != nil {
		t.Errorf("base = %v, want nil for root commit", rng.Target.Base)
	}
}

func TestResolveRevisionRangeTwoDot(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git rev-parse --verify A^{commit}":          {stdout: "ca\n"},
		"git rev-parse --verify B^{commit}":          {stdout: "cb\n"},
		"git rev-list --topo-order --reverse ca..cb": {stdout: "c1\nc2\n"},
	})

	rng, err := backend.ResolveRevisionRange("A..B")
	if err != nil {
		t.Fatalf("ResolveRevisionRange failed: %v", err)
	}

	if len(rng.CommitIDs) != 2 || rng.CommitIDs[0] != "c1" || rng.CommitIDs[1] != "c2" {
		t.Errorf("commit ids = %v, want oldest-first [c1 c2]", rng.CommitIDs)
	}
	if !rng.Target.Explicit || rng.Target.Base == nil || *rng.Target.Base != "ca" || rng.Target.Head != "cb" {
		t.Errorf("target = %+v", rng.Target)
	}
}

func TestResolveRevisionRangeOpenEnded(t *testing.T) {
	responses := map[string]response{
		"git rev-parse --verify A^{commit}":          {stdout: "ca\n"},
		"git rev-parse --verify HEAD^{commit}":       {stdout: "ch\n"},
		"git rev-list --topo-order --reverse ca..ch": {stdout: "c1\n"},
		"git rev-list --topo-order --reverse ch..ca": {stdout: "c1\n"},
	}

	backend, _ := newTestBackend(t, "/repo", responses)
	rng, err := backend.ResolveRevisionRange("A..")
	if err != nil {
		t.Fatalf("A.. failed: %v", err)
	}
	if rng.Target.Head != "ch" || *rng.Target.Base != "ca" {
		t.Errorf("A.. target = %+v", rng.Target)
	}

	backend, _ = newTestBackend(t, "/repo", responses)
	rng, err = backend.ResolveRevisionRange("..A")
	if err != nil {
		t.Fatalf("..A failed: %v", err)
	}
	if rng.Target.Head != "ca" || *rng.Target.Base != "ch" {
		t.Errorf("..A target = %+v", rng.Target)
	}
}

func TestResolveRevisionRangeMergeBase(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git rev-parse --verify A^{commit}":          {stdout: "ca\n"},
		"git rev-parse --verify B^{commit}":          {stdout: "cb\n"},
		"git merge-base ca cb":                       {stdout: "mb\n"},
		"git rev-list --topo-order --reverse mb..cb": {stdout: "c1\nc2\n"},
	})

	rng, err := backend.ResolveRevisionRange("A...B")
	if err != nil {
		t.Fatalf("ResolveRevisionRange failed: %v", err)
	}

	if len(rng.CommitIDs) != 2 {
		t.Errorf("commit ids = %v", rng.CommitIDs)
	}
	if !rng.Target.Explicit || rng.Target.Base == nil || *rng.Target.Base != "mb" || rng.Target.Head != "cb" {
		t.Errorf("target = %+v", rng.Target)
	}
}

func TestResolveRevisionRangeMissingEndpoint(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", nil)

	for _, revset := range []string{"...B", "A...", "..."} {
		_, err := backend.ResolveRevisionRange(revset)
		var vcsErr *errs.VcsCommand
		if !errors.As(err, &vcsErr) || vcsErr.Detail != "Invalid revision range: missing endpoint" {
			t.Errorf("ResolveRevisionRange(%q) err = %v", revset, err)
		}
	}
}

func TestResolveRevisionRangeEmptySelection(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git rev-parse --verify A^{commit}":          {stdout: "ca\n"},
		"git rev-parse --verify B^{commit}":          {stdout: "cb\n"},
		"git rev-list --topo-order --reverse ca..cb": {stdout: "\n"},
	})

	_, err := backend.ResolveRevisionRange("A..B")

	if !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("err = %v, want ErrNoChanges", err)
	}
}

func TestResolveRevisionRangeUnknownRevision(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git rev-parse --verify nope^{commit}": {stderr: "fatal: needed a single revision", err: exitError(128)},
	})

	_, err := backend.ResolveRevisionRange("nope")

	var vcsErr *errs.VcsCommand
	if !errors.As(err, &vcsErr) || vcsErr.Detail != "fatal: needed a single revision" {
		t.Fatalf("err = %v", err)
	}
}
