package forge

import (
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/model"
)

func TestPullRequestDetailsReadOnly(t *testing.T) {
	open := &PullRequestDetails{}
	if open.IsReadOnly() || open.ReadOnlyReason() != "" {
		t.Fatal("open PR must not be read-only")
	}

	closed := &PullRequestDetails{Closed: true}
	if !closed.IsReadOnly() || closed.ReadOnlyReason() != "closed" {
		t.Fatalf("closed: %v %q", closed.IsReadOnly(), closed.ReadOnlyReason())
	}

	now := time.Now()
	merged := &PullRequestDetails{Closed: true, MergedAt: &now}
	if !merged.IsReadOnly() || merged.ReadOnlyReason() != "merged" {
		t.Fatalf("merged: %v %q", merged.IsReadOnly(), merged.ReadOnlyReason())
	}
}

func TestSideForStatus(t *testing.T) {
	if SideForStatus(model.StatusDeleted) != FileSideBase {
		t.Fatal("deleted must read the base side")
	}
	for _, status := range []model.FileStatus{
		model.StatusAdded, model.StatusModified, model.StatusRenamed, model.StatusCopied,
	} {
		if SideForStatus(status) != FileSideHead {
			t.Fatalf("%s must read the head side", status)
		}
	}
}

func TestPathForSide(t *testing.T) {
	oldPath, newPath := "old.go", "new.go"
	if p, ok := PathForSide(FileSideBase, &oldPath, &newPath); !ok || p != "old.go" {
		t.Fatalf("base = %q %v", p, ok)
	}
	if p, ok := PathForSide(FileSideHead, &oldPath, &newPath); !ok || p != "new.go" {
		t.Fatalf("head = %q %v", p, ok)
	}
	// Fallback to the other side when one is absent.
	if p, ok := PathForSide(FileSideBase, nil, &newPath); !ok || p != "new.go" {
		t.Fatalf("base fallback = %q %v", p, ok)
	}
	if p, ok := PathForSide(FileSideHead, &oldPath, nil); !ok || p != "old.go" {
		t.Fatalf("head fallback = %q %v", p, ok)
	}
	if _, ok := PathForSide(FileSideHead, nil, nil); ok {
		t.Fatal("no path must fail")
	}
}

func TestFileLinesRequestSHA(t *testing.T) {
	req := FileLinesRequest{BaseSHA: "base", HeadSHA: "head"}
	req.Side = FileSideBase
	if req.SHA() != "base" {
		t.Fatalf("base sha = %q", req.SHA())
	}
	req.Side = FileSideHead
	if req.SHA() != "head" {
		t.Fatalf("head sha = %q", req.SHA())
	}
}

func TestListScopeToggledAndLabel(t *testing.T) {
	if ScopeOpen.Toggled() != ScopeReviewRequested || ScopeReviewRequested.Toggled() != ScopeOpen {
		t.Fatal("Toggled mismatch")
	}
	if ScopeOpen.Label() != "all" || ScopeReviewRequested.Label() != "requested" {
		t.Fatal("Label mismatch")
	}
}

func TestSubmitEventHumanLabels(t *testing.T) {
	cases := map[SubmitEvent]string{
		SubmitComment:        "Comment",
		SubmitApprove:        "Approve",
		SubmitRequestChanges: "Request changes",
		SubmitDraft:          "Draft (pending review)",
		SubmitEvent(99):      "Comment",
	}
	for event, want := range cases {
		if got := event.HumanLabel(); got != want {
			t.Errorf("HumanLabel(%d) = %q, want %q", event, got, want)
		}
	}
}

func TestParseReviewState(t *testing.T) {
	cases := map[string]ReviewState{
		"APPROVED":          ReviewApproved,
		"approved":          ReviewApproved,
		"CHANGES_REQUESTED": ReviewChangesRequested,
		"DISMISSED":         ReviewDismissed,
		"PENDING":           ReviewPending,
		"COMMENTED":         ReviewCommented,
		"":                  ReviewCommented,
		"weird":             ReviewCommented,
		"  pending  ":       ReviewPending,
	}
	for input, want := range cases {
		if got := ParseReviewState(input); got != want {
			t.Errorf("ParseReviewState(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestReviewStateBadgeLabels(t *testing.T) {
	cases := map[ReviewState]string{
		ReviewCommented:        "",
		ReviewApproved:         "approved",
		ReviewChangesRequested: "changes requested",
		ReviewDismissed:        "dismissed",
		ReviewPending:          "pending",
		ReviewState("other"):   "",
	}
	for state, want := range cases {
		if got := state.BadgeLabel(); got != want {
			t.Errorf("BadgeLabel(%q) = %q, want %q", state, got, want)
		}
	}
}

func TestRemoteReviewThreadHelpers(t *testing.T) {
	empty := &RemoteReviewThread{}
	if empty.Root() != nil || empty.Replies() != nil {
		t.Fatal("empty thread helpers")
	}
	if !empty.IsActive() {
		t.Fatal("unresolved+current must be active")
	}

	thread := &RemoteReviewThread{
		Comments: []RemoteReviewComment{
			{ID: "root"}, {ID: "r1"}, {ID: "r2"},
		},
	}
	if thread.Root().ID != "root" {
		t.Fatalf("root = %+v", thread.Root())
	}
	replies := thread.Replies()
	if len(replies) != 2 || replies[0].ID != "r1" || replies[1].ID != "r2" {
		t.Fatalf("replies = %+v", replies)
	}

	resolved := &RemoteReviewThread{IsResolved: true}
	outdated := &RemoteReviewThread{IsOutdated: true}
	if resolved.IsActive() || outdated.IsActive() {
		t.Fatal("resolved/outdated threads are not active")
	}
}
