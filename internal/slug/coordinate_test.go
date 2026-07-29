package slug

import (
	"errors"
	"testing"
)

func coord(owner, repo string) RepoCoordinate {
	return RepoCoordinate{Owner: owner, Repo: repo}
}

func TestParseRepoCoordinateForms(t *testing.T) {
	for _, input := range []string{
		"slatedb/slatedb",
		"github.com/slatedb/slatedb",
		"forge:github.com/slatedb/slatedb",
		"gh:github.com/slatedb/slatedb",
		"https://github.com/slatedb/slatedb.git",
		"git@github.com:slatedb/slatedb.git",
		"ssh://git@github.com/slatedb/slatedb",
		"  slatedb/slatedb  ",
	} {
		got, err := ParseRepoCoordinate(input)
		if err != nil {
			t.Errorf("ParseRepoCoordinate(%q) error: %v", input, err)
			continue
		}
		if want := coord("slatedb", "slatedb"); got != want {
			t.Errorf("ParseRepoCoordinate(%q) = %+v, want %+v", input, got, want)
		}
	}
}

func TestParseRepoCoordinateForgePrefixes(t *testing.T) {
	tests := []struct {
		input string
		want  RepoCoordinate
	}{
		{"gl:gitlab.com/team/svc", coord("team", "svc")},
		{"ado:dev.azure.com/org/project/repo", coord("project", "repo")},
		{"fj:codeberg.org/owner/repo", coord("owner", "repo")},
	}
	for _, tt := range tests {
		got, err := ParseRepoCoordinate(tt.input)
		if err != nil {
			t.Errorf("ParseRepoCoordinate(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseRepoCoordinate(%q) = %+v, want %+v", tt.input, got, tt.want)
		}
	}
}

func TestParseRepoCoordinateBareRepoWithoutOwner(t *testing.T) {
	got, err := ParseRepoCoordinate("slatedb")
	if err != nil {
		t.Fatalf("ParseRepoCoordinate error: %v", err)
	}
	if want := coord("", "slatedb"); got != want {
		t.Errorf("ParseRepoCoordinate = %+v, want %+v", got, want)
	}
}

func TestParseRepoCoordinateNestedGroupsTakeLastTwoSegments(t *testing.T) {
	got, err := ParseRepoCoordinate("gitlab.com/org/team/svc")
	if err != nil {
		t.Fatalf("ParseRepoCoordinate error: %v", err)
	}
	if want := coord("team", "svc"); got != want {
		t.Errorf("ParseRepoCoordinate = %+v, want %+v", got, want)
	}
}

func TestParseRepoCoordinateRejectsEmptyInputs(t *testing.T) {
	for _, input := range []string{"", "forge:", "/", "github.com/owner/.git"} {
		if _, err := ParseRepoCoordinate(input); !errors.Is(err, ErrInvalidRepoCoordinate) {
			t.Errorf("ParseRepoCoordinate(%q) error = %v, want ErrInvalidRepoCoordinate", input, err)
		}
	}
}

func TestRepoCoordinateMatchesCaseInsensitively(t *testing.T) {
	if !coord("SlateDB", "SlateDB").Matches(coord("slatedb", "slatedb")) {
		t.Error("case-insensitive match failed")
	}
}

func TestRepoCoordinateMatchesWhenEitherSideHasNoOwner(t *testing.T) {
	// A no-remote checkout (no owner) still matches an owner/repo selector.
	if !coord("slatedb", "slatedb").Matches(coord("", "slatedb")) {
		t.Error("selector with owner did not match candidate without owner")
	}
	if !coord("", "slatedb").Matches(coord("slatedb", "slatedb")) {
		t.Error("selector without owner did not match candidate with owner")
	}
}

func TestRepoCoordinateDoesNotMatchDifferentOwnerOrRepo(t *testing.T) {
	if coord("a", "repo").Matches(coord("b", "repo")) {
		t.Error("different owners matched")
	}
	if coord("a", "repo").Matches(coord("a", "other")) {
		t.Error("different repos matched")
	}
}

func TestRepoCoordinateFromParsedSlugs(t *testing.T) {
	// The coordinate a slug belongs to, for both local and PR slugs.
	localParsed, err := Parse("agavra/tuicr@main/worktree/abc1234")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	local := localParsed.(LocalSlug)
	if got, want := coord(local.Owner, local.Repo), coord("agavra", "tuicr"); got != want {
		t.Errorf("local coordinate = %+v, want %+v", got, want)
	}

	prParsed, err := Parse("gh:github.com/slatedb/slatedb/pr/1745")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	pr := prParsed.(PrSlug)
	got := coord(pr.RepoPath[len(pr.RepoPath)-2], pr.RepoPath[len(pr.RepoPath)-1])
	if want := coord("slatedb", "slatedb"); got != want {
		t.Errorf("pr coordinate = %+v, want %+v", got, want)
	}
}
