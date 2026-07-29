// Package forgetypes holds the plain-data forge types that persisted
// sessions reference. It is a leaf package with no client dependencies so
// internal/model can import it; the forge drivers build on top.
package forgetypes

import "strings"

// Kind identifies a forge as an open string, so sessions from forges this
// build does not know still deserialize and fail politely.
type Kind string

// Known forge kinds.
const (
	KindGitHub      Kind = "github"
	KindGitLab      Kind = "gitlab"
	KindAzureDevOps Kind = "azure_devops"
	KindForgejo     Kind = "forgejo"
)

// SlugPrefix returns the short prefix used in PR slugs.
func (k Kind) SlugPrefix() string {
	switch k {
	case KindGitHub:
		return "gh"
	case KindGitLab:
		return "gl"
	case KindAzureDevOps:
		return "ado"
	case KindForgejo:
		return "fj"
	}
	return string(k)
}

// KindFromSlugPrefix resolves a slug prefix back to a Kind, ok=false for
// unknown prefixes.
func KindFromSlugPrefix(prefix string) (Kind, bool) {
	switch prefix {
	case "gh":
		return KindGitHub, true
	case "gl":
		return KindGitLab, true
	case "ado":
		return KindAzureDevOps, true
	case "fj":
		return KindForgejo, true
	}
	return "", false
}

// Repository identifies a repository on a forge host. Owner may contain
// slashes on GitLab (subgroups); Project is set only for Azure DevOps
// (org/project/repo coordinates) and omitted from JSON elsewhere.
type Repository struct {
	Kind    Kind   `json:"kind"`
	Host    string `json:"host"`
	Owner   string `json:"owner"`
	Project string `json:"project,omitempty"`
	Name    string `json:"name"`
}

// PathSegments returns the repo path between host and the pr/<n> suffix in
// slugs and URLs: owner[/project]/name split on owner slashes.
func (r Repository) PathSegments() []string {
	segs := strings.Split(r.Owner, "/")
	if r.Project != "" {
		segs = append(segs, r.Project)
	}
	return append(segs, r.Name)
}

// Slug returns the display coordinate owner[/project]/name without the host.
func (r Repository) Slug() string {
	return strings.Join(r.PathSegments(), "/")
}

// PrSessionKey is the identity of a PR review session: repository, PR
// number, and the head SHA the session was opened at.
type PrSessionKey struct {
	Repository Repository `json:"repository"`
	Number     uint64     `json:"number"`
	HeadSHA    string     `json:"head_sha"`
}

// ShortHead returns the first 8 characters of the head SHA for display.
func (k PrSessionKey) ShortHead() string {
	if len(k.HeadSHA) <= 8 {
		return k.HeadSHA
	}
	return k.HeadSHA[:8]
}

// PrCommentsVisibility controls which existing remote comments render in PR
// mode.
type PrCommentsVisibility string

// Visibility modes.
const (
	VisibilityUnresolved PrCommentsVisibility = "unresolved"
	VisibilityAll        PrCommentsVisibility = "all"
	VisibilityHide       PrCommentsVisibility = "hide"
)
