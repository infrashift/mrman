package slug

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

func liveSlug(owner, repo, branch, head string, kind SourceKind) LocalSlug {
	return LocalSlug{
		Owner:  owner,
		Repo:   repo,
		Anchor: SlugAnchor{Branch: branch},
		Source: SlugSource{Kind: kind, Head: head},
	}
}

// TestSameLiveReviewAcrossHeads is the relation carry-forward matches on: the
// same branch of the same repo at a different HEAD is the same review moved
// along, which is what survives an amend.
func TestSameLiveReviewAcrossHeads(t *testing.T) {
	before := liveSlug("agavra", "tuicr", "main", "abcdef0", SourceWorktree)
	after := liveSlug("agavra", "tuicr", "main", "9999999", SourceWorktree)
	if !before.SameLiveReview(after) {
		t.Error("the same branch at a new HEAD must be the same live review")
	}
	if !after.SameLiveReview(before) {
		t.Error("the relation must be symmetric")
	}
	// It is still a distinct identity — that is what makes the previous HEAD's
	// session findable as a separate thing in the first place.
	if before.String() == after.String() {
		t.Error("slugs must still differ per HEAD")
	}
}

// TestSameLiveReviewRejectsDifferentIdentities pins every axis that must break
// the relation. Each of these is a review of different content, and adopting
// across one would resurrect comments somewhere they were never written.
func TestSameLiveReviewRejectsDifferentIdentities(t *testing.T) {
	base := liveSlug("agavra", "tuicr", "main", "abcdef0", SourceWorktree)
	cases := map[string]LocalSlug{
		"different branch": liveSlug("agavra", "tuicr", "feature", "9999999", SourceWorktree),
		"different repo":   liveSlug("agavra", "other", "main", "9999999", SourceWorktree),
		"different owner":  liveSlug("someone", "tuicr", "main", "9999999", SourceWorktree),
		"different source": liveSlug("agavra", "tuicr", "main", "9999999", SourceStaged),
	}
	for name, other := range cases {
		if base.SameLiveReview(other) {
			t.Errorf("%s must not be the same live review", name)
		}
	}
}

// TestSameLiveReviewRejectsDetachedAnchors covers the case with no stable
// identity: a detached anchor is derived from the commit, so it moves with the
// very thing carry-forward is trying to look past.
func TestSameLiveReviewRejectsDetachedAnchors(t *testing.T) {
	detached := LocalSlug{
		Owner: "agavra", Repo: "tuicr",
		Anchor: SlugAnchor{ShortSHA: "abcdef0"},
		Source: SlugSource{Kind: SourceWorktree, Head: "abcdef0"},
	}
	other := LocalSlug{
		Owner: "agavra", Repo: "tuicr",
		Anchor: SlugAnchor{ShortSHA: "9999999"},
		Source: SlugSource{Kind: SourceWorktree, Head: "9999999"},
	}
	if detached.SameLiveReview(other) {
		t.Error("two detached checkouts must not be treated as one review")
	}
	if detached.SameLiveReview(liveSlug("agavra", "tuicr", "main", "9999999", SourceWorktree)) {
		t.Error("a detached anchor must not match a branch anchor")
	}
}

// TestSameLiveReviewRejectsNonLiveSources keeps ranges out: a range names its
// own endpoints, so a different range is a different review by construction.
func TestSameLiveReviewRejectsNonLiveSources(t *testing.T) {
	for _, kind := range []SourceKind{
		SourcePristine, SourceCommits, SourceWorktreeAndCommits, SourceStagedUnstagedAndCommits,
	} {
		a := LocalSlug{
			Owner: "agavra", Repo: "tuicr",
			Anchor: SlugAnchor{Branch: "main"},
			Source: SlugSource{Kind: kind, Base: "aaaaaaa", Head: "bbbbbbb"},
		}
		b := a
		b.Source.Head = "ccccccc"
		if a.SameLiveReview(b) {
			t.Errorf("source kind %d must not carry forward", kind)
		}
	}
}

// TestIsLiveSourceMatchesKindLiveness guards the one thing KindForSource
// exists to prevent: the model-level and slug-level notions of "live" drifting
// apart as sources are added.
func TestIsLiveSourceMatchesKindLiveness(t *testing.T) {
	all := []model.SessionDiffSource{
		model.SourceWorkingTree, model.SourceStaged, model.SourceUnstaged,
		model.SourceStagedAndUnstaged, model.SourcePristine, model.SourceCommitRange,
		model.SourceWorkingTreeAndCommits, model.SourceStagedUnstagedAndCommits,
		model.SourcePullRequest, model.SourcePatch,
	}
	for _, src := range all {
		kind, ok := KindForSource(src)
		want := ok && kind.IsLive()
		if got := IsLiveSource(src); got != want {
			t.Errorf("IsLiveSource(%s) = %v, want %v", src, got, want)
		}
	}
	// And the classification itself, spelled out so a change has to be
	// deliberate rather than incidental.
	live := map[model.SessionDiffSource]bool{
		model.SourceWorkingTree:           true,
		model.SourceStaged:                true,
		model.SourceUnstaged:              true,
		model.SourceStagedAndUnstaged:     true,
		model.SourcePristine:              false,
		model.SourceCommitRange:           false,
		model.SourcePullRequest:           false,
		model.SourceWorkingTreeAndCommits: false,
		// A patch is not live: its identity is the artifact's bytes, so there
		// is no HEAD for carry-forward to move it along.
		model.SourcePatch: false,
	}
	for src, want := range live {
		if got := IsLiveSource(src); got != want {
			t.Errorf("IsLiveSource(%s) = %v, want %v", src, got, want)
		}
	}
}

// TestKindForSourceRejectsPullRequests covers the one source with no local
// slug form; PR sessions are identified by PrSlug instead.
func TestKindForSourceRejectsPullRequests(t *testing.T) {
	if _, ok := KindForSource(model.SourcePullRequest); ok {
		t.Error("a pull request has no local slug source kind")
	}
	if _, ok := KindForSource(model.SessionDiffSource("nonsense")); ok {
		t.Error("an unknown source must not map to a kind")
	}
}
