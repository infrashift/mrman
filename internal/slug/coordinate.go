package slug

import (
	"errors"
	"fmt"
	"strings"

	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// ErrInvalidRepoCoordinate reports a repo selector no coordinate can be
// parsed from.
var ErrInvalidRepoCoordinate = errors.New("invalid repo coordinate")

// RepoCoordinate is the repository half of a session identity: owner/repo,
// with the owner optional (empty) for local checkouts that have no origin
// remote.
//
// This is the unit "mrman review list --repo" matches on. Because every slug
// — local or PR — carries the same coordinate, one selector pulls in both a
// checkout's local sessions and the forge PR sessions for the same repo.
type RepoCoordinate struct {
	Owner string
	Repo  string
}

// adoGitMarker is the literal segment Azure DevOps puts between a project
// and its repository ({org}/{project}/_git/{repo}). It is a URL marker, not
// a namespace, so coordinate derivation skips it.
const adoGitMarker = "_git"

// ParseRepoCoordinate parses a user-supplied repo selector: "owner/repo",
// "host/owner/repo", "forge:host/owner/repo" (also accepting the gh/gl/ado/fj
// slug prefixes), or an HTTPS / HTTP / SSH / SCP URL. The last two path
// segments become owner/repo (so nested GitLab subgroups degrade
// gracefully); a lone segment yields a repo with no owner. A trailing .git
// is stripped.
//
// Azure DevOps' "_git" path marker is dropped first. Its clone URLs are
// {org}/{project}/_git/{repo}, so keeping the marker would make "_git" the
// owner and stop a checkout's coordinate ever matching the {project}/{repo}
// one derived from its PR slugs — which silently broke `mrman review list
// --repo <ado checkout>`.
func ParseRepoCoordinate(input string) (RepoCoordinate, error) {
	trimmed := strings.TrimSpace(input)
	withoutForge := stripForgePrefix(trimmed)
	withoutScheme := withoutForge
	if _, rest, ok := strings.Cut(withoutForge, "://"); ok {
		withoutScheme = rest
	}
	// SCP-style git@host:owner/repo: drop the user, treat ":" as a separator.
	normalized := withoutScheme
	if _, rest, ok := strings.Cut(withoutScheme, "@"); ok {
		normalized = strings.Replace(rest, ":", "/", 1)
	}
	var segments []string
	for _, seg := range strings.Split(strings.Trim(normalized, "/"), "/") {
		if seg != "" && seg != adoGitMarker {
			segments = append(segments, seg)
		}
	}
	if len(segments) == 0 {
		return RepoCoordinate{}, fmt.Errorf("%w: %s", ErrInvalidRepoCoordinate, input)
	}
	repo := strings.TrimSuffix(segments[len(segments)-1], ".git")
	if repo == "" {
		return RepoCoordinate{}, fmt.Errorf("%w: %s", ErrInvalidRepoCoordinate, input)
	}
	var owner string
	if len(segments) >= 2 {
		owner = segments[len(segments)-2]
	}
	return RepoCoordinate{Owner: owner, Repo: repo}, nil
}

// stripForgePrefix removes a leading "forge:" or known forge slug prefix
// ("gh:", "gl:", "ado:", "fj:") from a selector, leaving URL schemes
// (https://...) and SCP forms (git@host:path) untouched.
func stripForgePrefix(s string) string {
	prefix, rest, ok := strings.Cut(s, ":")
	if !ok || strings.HasPrefix(rest, "//") ||
		strings.ContainsAny(prefix, "/@") {
		return s
	}
	if prefix == "forge" {
		return rest
	}
	if _, known := forgetypes.KindFromSlugPrefix(prefix); known {
		return rest
	}
	return s
}

// Matches reports whether other belongs to the repo this coordinate names.
// Repo names compare case-insensitively; owners are compared only when both
// sides have one, so a checkout without a remote still matches an
// "owner/repo" selector by repo name.
func (c RepoCoordinate) Matches(other RepoCoordinate) bool {
	if !strings.EqualFold(c.Repo, other.Repo) {
		return false
	}
	if c.Owner != "" && other.Owner != "" {
		return strings.EqualFold(c.Owner, other.Owner)
	}
	return true
}
