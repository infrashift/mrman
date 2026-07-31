package slug

import (
	"errors"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// patchSession builds a session shaped the way the patch backend will build
// one: the artifact's directory as the repo path, the file's stem as the
// anchor, and a hex content hash where a commit would go.
//
//nolint:unparam // repoPath is fixed today; kept so the helper reads at its call sites
func patchSession(repoPath, stem, contentHash string) *model.ReviewSession {
	branch := stem
	return model.NewReviewSession(repoPath, contentHash, &branch, model.SourcePatch)
}

// TestPatchSlugRoundTrips is the contract every persistence path depends on:
// a slug that cannot be parsed back is a session that cannot be found.
//
// Two of the sites this exercises fail *silently* when a source is missed —
// KindForSource makes saving fail in a way openSession swallows, and
// SlugSource.String falls through to "" — so the round trip is the assertion
// that catches both.
func TestPatchSlugRoundTrips(t *testing.T) {
	swapForSessionRunner(t, originRunner)

	sl, err := ForSession(patchSession("/home/r/mail", "v2_net_fix", "9f3c1a2b7d4e5061"))
	if err != nil {
		t.Fatalf("ForSession: %v", err)
	}

	want := "agavra/tuicr@v2_net_fix/patch/9f3c1a2"
	if got := sl.String(); got != want {
		t.Fatalf("slug = %q, want %q", got, want)
	}

	parsed, err := Parse(sl.String())
	if err != nil {
		t.Fatalf("Parse(%q): %v", sl.String(), err)
	}
	local, ok := parsed.(LocalSlug)
	if !ok {
		t.Fatalf("parsed as %T, want LocalSlug", parsed)
	}
	if local.Source.Kind != SourcePatch {
		t.Errorf("Source.Kind = %v, want SourcePatch", local.Source.Kind)
	}
	if local.Source.Head != "9f3c1a2" {
		t.Errorf("Source.Head = %q, want the short content hash", local.Source.Head)
	}
	if local.Anchor.Branch != "v2_net_fix" {
		t.Errorf("Anchor.Branch = %q", local.Anchor.Branch)
	}
}

// TestPatchSlugNeverContainsColon is the trap this design exists to avoid.
//
// Parse cuts on the first ":" and demands a known forge prefix, so a colon
// anywhere in a local slug makes it unparseable. The anchor is the danger:
// with no branch it becomes ~shortSHA(BaseCommit), and shortSHA takes 7
// characters — "pristine:..." survives only because "pristine" happens to be
// 8 long. A patch anchor therefore comes from the filename, and the filename
// has to be sanitized before it gets there.
func TestPatchSlugNeverContainsColon(t *testing.T) {
	swapForSessionRunner(t, originRunner)

	for _, stem := range []string{"plain", "v2_fix", "with.dots", "with-dashes"} {
		t.Run(stem, func(t *testing.T) {
			sl, err := ForSession(patchSession("/home/r/mail", stem, "abcdef0123456789"))
			if err != nil {
				t.Fatalf("ForSession: %v", err)
			}
			if strings.Contains(sl.String(), ":") {
				t.Fatalf("slug %q contains a colon and will not parse back", sl.String())
			}
			if _, err := Parse(sl.String()); err != nil {
				t.Errorf("Parse(%q): %v", sl.String(), err)
			}
		})
	}
}

// TestPatchSlugWithColonInStemFailsLoudly documents what happens when a
// caller forgets to sanitize. It is a guard on the contract, not on
// behaviour we want: the backend must not hand an unsanitized stem through.
func TestPatchSlugWithColonInStemFailsLoudly(t *testing.T) {
	swapForSessionRunner(t, originRunner)

	sl, err := ForSession(patchSession("/home/r/mail", "bad:stem", "abcdef0123456789"))
	if err != nil {
		t.Fatalf("ForSession: %v", err)
	}
	if _, err := Parse(sl.String()); err == nil {
		t.Errorf("Parse(%q) succeeded; an unsanitized stem should be caught, "+
			"not silently produce an unfindable session", sl.String())
	}
}

// TestPatchSlugDetachedAnchorStaysParseable covers the fallback path: a
// session with no branch anchors on ~shortSHA(BaseCommit). Because a patch's
// BaseCommit is bare hex, that is safe — but only because it is bare hex.
func TestPatchSlugDetachedAnchorStaysParseable(t *testing.T) {
	swapForSessionRunner(t, originRunner)

	sess := model.NewReviewSession("/home/r/mail", "9f3c1a2b7d4e5061", nil, model.SourcePatch)
	sl, err := ForSession(sess)
	if err != nil {
		t.Fatalf("ForSession: %v", err)
	}
	if got, want := sl.String(), "agavra/tuicr@~9f3c1a2/patch/9f3c1a2"; got != want {
		t.Errorf("slug = %q, want %q", got, want)
	}
	if _, err := Parse(sl.String()); err != nil {
		t.Errorf("Parse(%q): %v", sl.String(), err)
	}
}

// TestPatchSourceNeedsNoCommitRange guards the arm ordering in
// sourceForSession: without an explicit patch case it falls through to
// rangeSource, which errors on the empty CommitRange a patch always has.
func TestPatchSourceNeedsNoCommitRange(t *testing.T) {
	swapForSessionRunner(t, originRunner)

	sess := patchSession("/home/r/mail", "stem", "abcdef0123456789")
	if len(sess.CommitRange) != 0 {
		t.Fatal("precondition: a patch session carries no commit range")
	}
	if _, err := ForSession(sess); err != nil {
		t.Fatalf("ForSession: %v", err)
	}
	if _, err := ForSession(sess); errors.Is(err, ErrMissingCommitRange) {
		t.Error("a patch source must not be routed through rangeSource")
	}
}

// TestPatchSourceIsNotLive keeps carry-forward away from patches. Adopting
// one onto a "new HEAD" is meaningless — a different artifact is a different
// review, not the same one moved along.
func TestPatchSourceIsNotLive(t *testing.T) {
	if IsLiveSource(model.SourcePatch) {
		t.Error("a patch source must not be live")
	}
	if SourcePatch.IsLive() {
		t.Error("SourcePatch.IsLive() must be false")
	}

	a := LocalSlug{
		Owner: "agavra", Repo: "tuicr",
		Anchor: SlugAnchor{Branch: "series"},
		Source: SlugSource{Kind: SourcePatch, Head: "aaaaaaa"},
	}
	b := a
	b.Source.Head = "bbbbbbb"
	if a.SameLiveReview(b) {
		t.Error("two different patch artifacts are two different reviews")
	}
}

// TestParsePatchSourceRejectsMalformed covers the token validation shared
// with the live sources.
func TestParsePatchSourceRejectsMalformed(t *testing.T) {
	for _, s := range []string{"repo@main/patch/", "repo@main/patch/a/b"} {
		if _, err := Parse(s); !errors.Is(err, ErrUnknownSource) {
			t.Errorf("Parse(%q) error = %v, want ErrUnknownSource", s, err)
		}
	}
}
