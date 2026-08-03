package slug

import (
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// diffPathsSession builds a session shaped the way the diff backend builds
// one: the new side's directory as the repo path, a sanitized "<old>-vs-<new>"
// anchor, and a hex hash of the two paths where a commit would go.
func diffPathsSession(repoPath, label, pairHash string) *model.ReviewSession {
	branch := label
	return model.NewReviewSession(repoPath, pairHash, &branch, model.SourceDiffPaths)
}

// TestDiffPathsSlugRoundTrips is the contract every persistence path depends
// on: a slug that cannot be parsed back is a session that cannot be found.
//
// Two of the sites this exercises fail *silently* when a source is missed —
// KindForSource makes saving fail in a way openSession swallows, and
// SlugSource.String falls through to "" — so the round trip catches both.
func TestDiffPathsSlugRoundTrips(t *testing.T) {
	swapForSessionRunner(t, originRunner)

	sl, err := ForSession(diffPathsSession("/tmp/new", "old-a.txt-vs-new-a.txt", "9f3c1a2b7d4e5061"))
	if err != nil {
		t.Fatalf("ForSession: %v", err)
	}

	want := "agavra/tuicr@old-a.txt-vs-new-a.txt/diff/9f3c1a2"
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
	if local.Source.Kind != SourceDiffPaths {
		t.Errorf("Source.Kind = %v, want SourceDiffPaths", local.Source.Kind)
	}
	if local.Source.Head != "9f3c1a2" {
		t.Errorf("Source.Head = %q, want the short pair hash", local.Source.Head)
	}
	if local.Anchor.Branch != "old-a.txt-vs-new-a.txt" {
		t.Errorf("Anchor.Branch = %q", local.Anchor.Branch)
	}
}

// TestDiffPathsSlugNeverContainsColon is the trap the bare-hex identity
// exists to avoid: Parse cuts on the first colon and then demands a known
// forge prefix, so a colon in the head token makes the slug unparseable and
// the session unfindable.
func TestDiffPathsSlugNeverContainsColon(t *testing.T) {
	swapForSessionRunner(t, originRunner)

	sl, err := ForSession(diffPathsSession("/tmp/new", "a-vs-b", "9f3c1a2b7d4e5061"))
	if err != nil {
		t.Fatalf("ForSession: %v", err)
	}
	source := sl.(LocalSlug).Source.String()
	if strings.Contains(source, ":") {
		t.Errorf("source segment %q must not contain a colon", source)
	}
}

// TestDiffPathsIsNotLive guards the carry-forward guard: a comparison has no
// HEAD to move, so AdoptSessionForNewHead must never treat one as a session
// to migrate.
func TestDiffPathsIsNotLive(t *testing.T) {
	if IsLiveSource(model.SourceDiffPaths) {
		t.Error("IsLiveSource(SourceDiffPaths) must be false")
	}
	if SourceDiffPaths.IsLive() {
		t.Error("SourceDiffPaths.IsLive() must be false")
	}
}

func TestKindForDiffPathsSource(t *testing.T) {
	kind, ok := KindForSource(model.SourceDiffPaths)
	if !ok {
		t.Fatal("KindForSource must recognise SourceDiffPaths")
	}
	if kind != SourceDiffPaths {
		t.Errorf("kind = %v, want SourceDiffPaths", kind)
	}
}
