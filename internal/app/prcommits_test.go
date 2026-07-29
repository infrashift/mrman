package app

import (
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/model"
)

// indexRange is a terser literal for the selector's inclusive range.
func indexRange(start, end int) *model.IndexRange {
	return &model.IndexRange{start, end}
}

// prCommit builds a forge commit; callers pass them oldest first, the order
// ListCommits returns.
func prCommit(oid, summary string) forge.Commit {
	return forge.Commit{OID: oid, ShortOID: oid, Summary: summary, Author: "ana"}
}

func TestPrCommitsBecomeNewestFirstSelectorRows(t *testing.T) {
	a := prModeApp(t)
	a.ShowCommitSelector = true
	a.SetupPrCommitSelector([]forge.Commit{
		prCommit("c1", "oldest"), prCommit("c2", "middle"), prCommit("c3", "newest"),
	})

	if len(a.ReviewCommits) != 3 {
		t.Fatalf("rows = %d, want 3", len(a.ReviewCommits))
	}
	if a.ReviewCommits[0].ID != "c3" || a.ReviewCommits[2].ID != "c1" {
		t.Errorf("selector rows must be newest first, got %s..%s",
			a.ReviewCommits[0].ID, a.ReviewCommits[2].ID)
	}
	if !a.HasInlineCommitSelector() {
		t.Error("a multi-commit PR must show the strip")
	}
}

func TestSingleCommitPrHidesTheStrip(t *testing.T) {
	a := prModeApp(t)
	a.ShowCommitSelector = true
	a.SetupPrCommitSelector([]forge.Commit{prCommit("c1", "only")})
	if a.HasInlineCommitSelector() {
		t.Error("one commit is not worth a selector")
	}
}

func TestPrCommitRangeResolvesBoundarySHAs(t *testing.T) {
	a := prModeApp(t)
	a.Pr.Details.BaseSHA = "base"
	a.SetupPrCommitSelector([]forge.Commit{
		prCommit("c1", "oldest"), prCommit("c2", "middle"), prCommit("c3", "newest"),
	})
	// Rows are newest first: [c3, c2, c1].

	// The whole PR needs no range call — the cumulative diff already is it.
	a.CommitSelectionRange = nil
	if _, _, full, ok := a.PrCommitRange(); !ok || !full {
		t.Error("selecting everything must report the full range")
	}

	// Just the newest commit: parent is c2.
	a.CommitSelectionRange = indexRange(0, 0)
	start, end, full, ok := a.PrCommitRange()
	if !ok || full || start != "c2" || end != "c3" {
		t.Errorf("newest-only range = (%s, %s) full=%v ok=%v; want (c2, c3)", start, end, full, ok)
	}

	// The oldest commit's parent is the PR base, not another commit.
	a.CommitSelectionRange = indexRange(2, 2)
	start, end, _, ok = a.PrCommitRange()
	if !ok || start != "base" || end != "c1" {
		t.Errorf("oldest-only range = (%s, %s); want (base, c1)", start, end)
	}

	// A middle span spans from its parent to its newest member.
	a.CommitSelectionRange = indexRange(0, 1)
	start, end, _, ok = a.PrCommitRange()
	if !ok || start != "c1" || end != "c3" {
		t.Errorf("two-commit range = (%s, %s); want (c1, c3)", start, end)
	}
}

func TestPrCommitRangeRejectsNonsense(t *testing.T) {
	a := prModeApp(t)
	if _, _, _, ok := a.PrCommitRange(); ok {
		t.Error("no commits means no range")
	}
	a.SetupPrCommitSelector([]forge.Commit{prCommit("c1", "a"), prCommit("c2", "b")})
	a.CommitSelectionRange = indexRange(1, 0) // inverted
	if _, _, _, ok := a.PrCommitRange(); ok {
		t.Error("an inverted selection must be rejected")
	}
}

func TestReviewMetadataPreselectsCommitsSinceTheLastReview(t *testing.T) {
	a := prModeApp(t)
	a.SetupPrCommitSelector([]forge.Commit{
		prCommit("c1", "one"), prCommit("c2", "two"),
		prCommit("c3", "three"), prCommit("c4", "four"),
	})
	// Rows newest first: [c4, c3, c2, c1]. Last review covered c2.
	earlier := time.Now().Add(-2 * time.Hour)
	later := time.Now().Add(-time.Hour)
	a.ApplyPrReviewMetadata(&forge.ReviewMetadata{
		ViewerLogin: "me",
		Reviews: []forge.ReviewRecord{
			{Author: "me", CommitOID: "c1", SubmittedAt: &earlier},
			{Author: "me", CommitOID: "c2", SubmittedAt: &later},
			{Author: "someone-else", CommitOID: "c4", SubmittedAt: &later},
		},
	})

	// c4 and c3 landed after the review: indices 0..1.
	r := a.CommitSelectionRange
	if r == nil || r[0] != 0 || r[1] != 1 {
		t.Fatalf("selection = %+v, want commits newer than c2", r)
	}
	for _, oid := range []string{"c1", "c2"} {
		if !a.IsCommitReviewed(oid) {
			t.Errorf("%s was covered by the last review and must be marked", oid)
		}
	}
	for _, oid := range []string{"c3", "c4"} {
		if a.IsCommitReviewed(oid) {
			t.Errorf("%s landed after the review and must not be marked", oid)
		}
	}
}

func TestReviewMetadataIsIgnoredWhenItCannotHelp(t *testing.T) {
	commits := []forge.Commit{prCommit("c1", "one"), prCommit("c2", "two")}

	for _, tc := range []struct {
		name string
		meta *forge.ReviewMetadata
	}{
		{"nil metadata", nil},
		{"no viewer login", &forge.ReviewMetadata{Reviews: []forge.ReviewRecord{{Author: "me", CommitOID: "c1"}}}},
		{"only other reviewers", &forge.ReviewMetadata{
			ViewerLogin: "me",
			Reviews:     []forge.ReviewRecord{{Author: "you", CommitOID: "c1"}},
		}},
		{"forge does not scope reviews to commits", &forge.ReviewMetadata{
			ViewerLogin: "me",
			Reviews:     []forge.ReviewRecord{{Author: "me"}},
		}},
		{"reviewed commit was rebased away", &forge.ReviewMetadata{
			ViewerLogin: "me",
			Reviews:     []forge.ReviewRecord{{Author: "me", CommitOID: "gone"}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := prModeApp(t)
			a.SetupPrCommitSelector(commits)
			before := a.CommitSelectionRange
			a.ApplyPrReviewMetadata(tc.meta)
			if a.CommitSelectionRange != before {
				t.Error("an unusable metadata response must leave the selection alone")
			}
			if a.IsCommitReviewed("c1") {
				t.Error("nothing should be marked reviewed")
			}
		})
	}
}

func TestReviewMetadataSaysSoWhenNothingIsNew(t *testing.T) {
	a := prModeApp(t)
	a.SetupPrCommitSelector([]forge.Commit{prCommit("c1", "one"), prCommit("c2", "two")})
	a.ApplyPrReviewMetadata(&forge.ReviewMetadata{
		ViewerLogin: "me",
		Reviews:     []forge.ReviewRecord{{Author: "me", CommitOID: "c2"}},
	})
	if a.Message == nil || a.Message.Content != "No new commits since your last review" {
		t.Errorf("message = %+v", a.Message)
	}
}

func TestSelectedCommitIDs(t *testing.T) {
	a := prModeApp(t)
	a.SetupPrCommitSelector([]forge.Commit{
		prCommit("c1", "one"), prCommit("c2", "two"), prCommit("c3", "three"),
	})

	a.CommitSelectionRange = nil
	if got := a.SelectedCommitIDs(); len(got) != 3 {
		t.Errorf("no selection means all commits, got %v", got)
	}
	a.CommitSelectionRange = indexRange(0, 1)
	got := a.SelectedCommitIDs()
	if len(got) != 2 || got[0] != "c3" || got[1] != "c2" {
		t.Errorf("selected ids = %v, want newest-first c3,c2", got)
	}
}
