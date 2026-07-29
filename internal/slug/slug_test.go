package slug

import (
	"errors"
	"slices"
	"testing"

	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// ---------- Display ----------

func TestRenderLocalSlug(t *testing.T) {
	tests := []struct {
		name string
		slug LocalSlug
		want string
	}{
		{
			name: "with owner",
			slug: LocalSlug{
				Owner:  "agavra",
				Repo:   "tuicr",
				Anchor: SlugAnchor{Branch: "main"},
				Source: SlugSource{Kind: SourceWorktree, Head: "abc1234"},
			},
			want: "agavra/tuicr@main/worktree/abc1234",
		},
		{
			name: "without owner",
			slug: LocalSlug{
				Repo:   "tuicr",
				Anchor: SlugAnchor{Branch: "main"},
				Source: SlugSource{Kind: SourceWorktree, Head: "abc1234"},
			},
			want: "tuicr@main/worktree/abc1234",
		},
		{
			name: "anonymous anchor",
			slug: LocalSlug{
				Owner:  "agavra",
				Repo:   "tuicr",
				Anchor: SlugAnchor{ShortSHA: "abc1234"},
				Source: SlugSource{Kind: SourceWorktree, Head: "abc1234"},
			},
			want: "agavra/tuicr@~abc1234/worktree/abc1234",
		},
		{
			name: "commits source",
			slug: LocalSlug{
				Owner:  "agavra",
				Repo:   "tuicr",
				Anchor: SlugAnchor{Branch: "main"},
				Source: SlugSource{Kind: SourceCommits, Base: "abc1234", Head: "def5678"},
			},
			want: "agavra/tuicr@main/commits/abc1234..def5678",
		},
		{
			name: "pristine source",
			slug: LocalSlug{
				Repo:   "tuicr",
				Anchor: SlugAnchor{Branch: "main"},
				Source: SlugSource{Kind: SourcePristine},
			},
			want: "tuicr@main/pristine",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.slug.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderPrSlug(t *testing.T) {
	tests := []struct {
		name string
		slug PrSlug
		want string
	}{
		{
			name: "github",
			slug: PrSlug{
				Forge:    forgetypes.KindGitHub,
				Host:     "github.com",
				RepoPath: []string{"agavra", "tuicr"},
				Number:   125,
			},
			want: "gh:github.com/agavra/tuicr/pr/125",
		},
		{
			name: "gitlab subgroup",
			slug: PrSlug{
				Forge:    forgetypes.KindGitLab,
				Host:     "gitlab.example.com",
				RepoPath: []string{"group", "subgroup", "proj"},
				Number:   34,
			},
			want: "gl:gitlab.example.com/group/subgroup/proj/pr/34",
		},
		{
			name: "azure devops org/project/repo",
			slug: PrSlug{
				Forge:    forgetypes.KindAzureDevOps,
				Host:     "dev.azure.com",
				RepoPath: []string{"org", "project", "repo"},
				Number:   7,
			},
			want: "ado:dev.azure.com/org/project/repo/pr/7",
		},
		{
			name: "forgejo",
			slug: PrSlug{
				Forge:    forgetypes.KindForgejo,
				Host:     "codeberg.org",
				RepoPath: []string{"owner", "repo"},
				Number:   9,
			},
			want: "fj:codeberg.org/owner/repo/pr/9",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.slug.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------- Round-trip ----------

func assertRoundtrip(t *testing.T, s string) {
	t.Helper()
	parsed, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", s, err)
	}
	if got := parsed.String(); got != s {
		t.Errorf("round-trip failed for %q: got %q", s, got)
	}
}

func TestRoundtripLocalSlugVariants(t *testing.T) {
	for _, s := range []string{
		"agavra/tuicr@main/worktree/abc1234",
		"agavra/tuicr@main/staged/abc1234",
		"agavra/tuicr@main/unstaged/abc1234",
		"agavra/tuicr@feature-login/staged-and-unstaged/abc1234",
		"agavra/tuicr@main/pristine",
		"agavra/tuicr@~abc1234/worktree/abc1234",
		"agavra/tuicr@main/commits/abc1234..def5678",
		"agavra/tuicr@main/worktree-and-commits/abc1234..def5678",
		"agavra/tuicr@main/staged-and-unstaged-and-commits/abc1234..def5678",
		"tuicr@main/worktree/abc1234",
		"tuicr@main/worktree/none",
	} {
		assertRoundtrip(t, s)
	}
}

func TestRoundtripPrSlugVariants(t *testing.T) {
	for _, s := range []string{
		"gh:github.com/agavra/tuicr/pr/125",
		"gh:github.com/org/svc/pr/9999",
		"gl:gitlab.com/org/svc/pr/12",
		"gl:gitlab.example.com/group/subgroup/proj/pr/34",
		"ado:dev.azure.com/org/project/repo/pr/7",
		"fj:codeberg.org/owner/repo/pr/9",
	} {
		assertRoundtrip(t, s)
	}
}

func TestParsePrSlugFields(t *testing.T) {
	parsed, err := Parse("gl:gitlab.example.com/group/subgroup/proj/pr/34")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	pr, ok := parsed.(PrSlug)
	if !ok {
		t.Fatalf("Parse returned %T, want PrSlug", parsed)
	}
	if pr.Forge != forgetypes.KindGitLab {
		t.Errorf("Forge = %q, want %q", pr.Forge, forgetypes.KindGitLab)
	}
	if pr.Host != "gitlab.example.com" {
		t.Errorf("Host = %q, want gitlab.example.com", pr.Host)
	}
	if want := []string{"group", "subgroup", "proj"}; !slices.Equal(pr.RepoPath, want) {
		t.Errorf("RepoPath = %v, want %v", pr.RepoPath, want)
	}
	if pr.Number != 34 {
		t.Errorf("Number = %d, want 34", pr.Number)
	}
}

func TestParseLocalSlugFields(t *testing.T) {
	parsed, err := Parse("agavra/tuicr@~abc1234/commits/abc1234..def5678")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	local, ok := parsed.(LocalSlug)
	if !ok {
		t.Fatalf("Parse returned %T, want LocalSlug", parsed)
	}
	want := LocalSlug{
		Owner:  "agavra",
		Repo:   "tuicr",
		Anchor: SlugAnchor{ShortSHA: "abc1234"},
		Source: SlugSource{Kind: SourceCommits, Base: "abc1234", Head: "def5678"},
	}
	if local != want {
		t.Errorf("Parse = %+v, want %+v", local, want)
	}
}

func TestParseAcceptsMrAliasAndEmitsPr(t *testing.T) {
	parsed, err := Parse("gl:gitlab.com/owner/repo/mr/5")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if got, want := parsed.String(), "gl:gitlab.com/owner/repo/pr/5"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestPrSlugHostsDoNotCollide(t *testing.T) {
	saas := PrSlug{
		Forge:    forgetypes.KindGitHub,
		Host:     "github.com",
		RepoPath: []string{"owner", "repo"},
		Number:   1,
	}
	onPrem := PrSlug{
		Forge:    forgetypes.KindGitHub,
		Host:     "ghe.corp.example",
		RepoPath: []string{"owner", "repo"},
		Number:   1,
	}
	if saas.String() == onPrem.String() {
		t.Errorf("same owner/repo on different hosts collided: %q", saas.String())
	}
}

// ---------- Parse errors ----------

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{"empty slug", "", ErrEmpty},
		{"pr non-numeric number", "gh:github.com/agavra/tuicr/pr/notanumber", ErrInvalidPrNumber},
		{"pr empty number", "gh:github.com/agavra/tuicr/pr/", ErrInvalidPrNumber},
		{"pr number overflow", "gh:github.com/o/r/pr/99999999999999999999", ErrInvalidPrNumber},
		{"unknown forge prefix", "xy:agavra/tuicr/pr/1", ErrUnknownForge},
		{"missing pr keyword", "gh:github.com/agavra/tuicr/notpr/1", ErrInvalidShape},
		{"legacy hostless pr slug", "gh:agavra/tuicr/pr/1", ErrInvalidShape},
		{"pr single repo segment", "gh:github.com/tuicr/pr/1", ErrInvalidShape},
		{"pr empty host", "gh:/agavra/tuicr/pr/1", ErrInvalidShape},
		{"pr empty repo segment", "gh:github.com//tuicr/pr/1", ErrInvalidShape},
		{"pr trailing segment", "gh:github.com/o/r/pr/1/extra", ErrInvalidShape},
		{"pr empty rest", "gh:", ErrInvalidShape},
		{"local without at separator", "agavra/tuicr/worktree/abc1234", ErrInvalidShape},
		{"unknown diff source", "agavra/tuicr@main/blarghhh", ErrUnknownSource},
		{"empty anchor", "agavra/tuicr@/worktree/abc1234", ErrInvalidShape},
		{"empty anonymous anchor", "agavra/tuicr@~/worktree/abc1234", ErrInvalidShape},
		{"empty project", "@main/worktree/abc1234", ErrInvalidShape},
		{"owner with extra slash", "a/b/c@main/worktree/abc1234", ErrInvalidShape},
		{"missing source separator", "tuicr@main", ErrInvalidShape},
		{"bare worktree source", "agavra/tuicr@main/worktree", ErrUnknownSource},
		{"bare staged source", "agavra/tuicr@main/staged", ErrUnknownSource},
		{"bare unstaged source", "agavra/tuicr@main/unstaged", ErrUnknownSource},
		{"bare staged-and-unstaged source", "agavra/tuicr@main/staged-and-unstaged", ErrUnknownSource},
		{"worktree with empty head", "agavra/tuicr@main/worktree/", ErrUnknownSource},
		{"worktree with extra segment", "agavra/tuicr@main/worktree/abc/def", ErrUnknownSource},
		{"commits empty range", "agavra/tuicr@main/commits/", ErrMissingRange},
		{"commits without separator", "agavra/tuicr@main/commits/abc1234", ErrMissingRange},
		{"commits empty base", "agavra/tuicr@main/commits/..abc1234", ErrMissingRange},
		{"commits empty head", "agavra/tuicr@main/commits/abc1234..", ErrMissingRange},
		{"worktree-and-commits missing range", "agavra/tuicr@main/worktree-and-commits/abc", ErrMissingRange},
		{"compound commits missing range", "agavra/tuicr@main/staged-and-unstaged-and-commits/abc", ErrMissingRange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.input)
			if err == nil {
				t.Fatalf("Parse(%q) succeeded, want error %v", tt.input, tt.want)
			}
			if !errors.Is(err, tt.want) {
				t.Errorf("Parse(%q) error = %v, want errors.Is(%v)", tt.input, err, tt.want)
			}
		})
	}
}

// ---------- Sanitization ----------

func TestSanitizeRefReplacesSlashes(t *testing.T) {
	tests := []struct{ in, want string }{
		{"feature/login", "feature-login"},
		{"dependabot/foo/bar", "dependabot-foo-bar"},
		{"main", "main"},
	}
	for _, tt := range tests {
		if got := sanitizeRef(tt.in); got != tt.want {
			t.Errorf("sanitizeRef(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestShortSHATruncates(t *testing.T) {
	tests := []struct{ in, want string }{
		{"abcdef1234567890", "abcdef1"},
		{"abc", "abc"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := shortSHA(tt.in); got != tt.want {
			t.Errorf("shortSHA(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
