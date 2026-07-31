// Package slug implements deterministic identifiers for review changesets,
// ported from tuicr's src/slug.rs.
//
// A slug is the agent-facing identity for a review session. It is intended to
// be human-readable and computable from public information about the
// repository and the kind of review being performed.
//
// Grammar:
//
//   - Local: [<owner>/]<repo>@<anchor>/<source>
//   - PR:    <forge-prefix>:<host>/<repo-path...>/pr/<number>
//
// Where <anchor> is either a sanitized branch/bookmark name (no "/") or
// "~<short-sha>" for detached / anonymous heads, and <source> is one of the
// diff-source variants (worktree/<head>, staged/<head>, unstaged/<head>,
// staged-and-unstaged/<head>, pristine, commits/<base>..<head>,
// worktree-and-commits/<base>..<head>,
// staged-and-unstaged-and-commits/<base>..<head>).
//
// The "live" working-tree sources (worktree, staged, unstaged,
// staged-and-unstaged) embed the short SHA of the current HEAD, so a new
// commit on the same branch is a distinct identity rather than the same
// session at a new position. That distinction is deliberate and load-bearing:
// it is what makes the previous HEAD's session findable as a separate thing.
//
// It is not, on its own, what keeps stale comments from lying. Originally it
// was — a new HEAD simply lost the old session (tuicr #378) — but a review
// that discards your work on every amend is a poor trade. The persistence
// layer now carries a live session forward onto a new HEAD (see
// Store.AdoptSessionForNewHead) and every comment's anchor is re-validated
// against the new diff, so a comment that no longer describes its line says
// so instead of quietly pointing at the wrong code. SameLiveReview is the
// relation that carry-forward matches on.
//
// Unlike tuicr, PR slugs are host-qualified — for example
// gh:github.com/infrashift/mrman/pr/12 or
// ado:dev.azure.com/org/project/repo/pr/7 — so the same owner/repo on two
// hosts can never collide. The tuicr legacy host-less PR form is not
// accepted.
package slug

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// shortSHALen is the number of leading SHA characters embedded in slugs.
const shortSHALen = 7

// Parse errors, mirroring tuicr's SlugParseError taxonomy. Wrapped values
// carry the offending input; match with errors.Is.
var (
	// ErrEmpty reports an empty slug string.
	ErrEmpty = errors.New("empty slug")
	// ErrInvalidShape reports a slug that does not fit the grammar.
	ErrInvalidShape = errors.New("invalid slug shape")
	// ErrUnknownSource reports an unrecognized diff-source segment.
	ErrUnknownSource = errors.New("unknown diff source")
	// ErrInvalidPrNumber reports a non-numeric PR number segment.
	ErrInvalidPrNumber = errors.New("invalid PR number")
	// ErrUnknownForge reports an unrecognized forge prefix before ":".
	ErrUnknownForge = errors.New("unknown forge kind")
	// ErrMissingRange reports a missing or malformed <base>..<head> range.
	ErrMissingRange = errors.New("missing or malformed commit range")
)

// Slug is a review-session identifier: either a LocalSlug or a PrSlug.
type Slug interface {
	String() string
}

// Compile-time checks that both slug kinds satisfy Slug.
var (
	_ Slug = LocalSlug{}
	_ Slug = PrSlug{}
)

// SlugAnchor is the branch position a local slug is anchored to. Exactly one
// field is non-empty: Branch holds a sanitized branch/bookmark name (slashes
// replaced with "-" at construction time so the anchor segment never contains
// "/"); ShortSHA holds a short commit SHA for detached / anonymous heads and
// renders with a leading "~".
//
//nolint:revive // Name fixed by the cross-package slug API contract.
type SlugAnchor struct {
	Branch   string
	ShortSHA string
}

// String renders the anchor segment: the branch name, or "~<sha>".
func (a SlugAnchor) String() string {
	if a.ShortSHA != "" {
		return "~" + a.ShortSHA
	}
	return a.Branch
}

// SourceKind discriminates the diff-source variant of a SlugSource.
type SourceKind int

// Diff-source variants, one per local source form.
const (
	// SourceWorktree is a live working-tree diff (worktree/<head>).
	SourceWorktree SourceKind = iota
	// SourceStaged is a live staged diff (staged/<head>).
	SourceStaged
	// SourceUnstaged is a live unstaged diff (unstaged/<head>).
	SourceUnstaged
	// SourceStagedAndUnstaged is a live combined diff
	// (staged-and-unstaged/<head>).
	SourceStagedAndUnstaged
	// SourcePristine is a whole-tree annotation surface (pristine).
	SourcePristine
	// SourceCommits is a commit-range diff (commits/<base>..<head>).
	SourceCommits
	// SourceWorktreeAndCommits combines worktree and commits
	// (worktree-and-commits/<base>..<head>).
	SourceWorktreeAndCommits
	// SourceStagedUnstagedAndCommits combines staged, unstaged and commits
	// (staged-and-unstaged-and-commits/<base>..<head>).
	SourceStagedUnstagedAndCommits
	// SourcePatch is a standalone patch artifact — a .patch file, a mail
	// message, or an mbox holding a series (patch/<content-hash>). Its head
	// token hashes the artifact's bytes rather than a commit, because a patch
	// has no position in any history: re-send it edited and it is a different
	// review.
	SourcePatch
)

// IsLive reports whether the kind is one of the live working-tree sources,
// whose slug segment carries a bare HEAD token rather than a commit range.
// These are the only sources whose identity moves when HEAD moves.
func (k SourceKind) IsLive() bool {
	switch k {
	case SourceWorktree, SourceStaged, SourceUnstaged, SourceStagedAndUnstaged:
		return true
	case SourcePristine, SourceCommits, SourceWorktreeAndCommits,
		SourceStagedUnstagedAndCommits, SourcePatch:
		return false
	}
	return false
}

// SlugSource is the diff-source segment of a local slug. Live kinds
// (worktree, staged, unstaged, staged-and-unstaged) carry the short HEAD SHA
// in Head (or "none" for an unborn HEAD) and leave Base empty. Range kinds
// (commits and the *-and-commits compounds) carry short SHAs in both Base and
// Head. Pristine carries neither.
//
//nolint:revive // Name fixed by the cross-package slug API contract.
type SlugSource struct {
	Kind SourceKind
	Head string
	Base string
}

// String renders the source segment of a local slug.
func (s SlugSource) String() string {
	switch s.Kind {
	case SourceWorktree:
		return "worktree/" + s.Head
	case SourceStaged:
		return "staged/" + s.Head
	case SourceUnstaged:
		return "unstaged/" + s.Head
	case SourceStagedAndUnstaged:
		return "staged-and-unstaged/" + s.Head
	case SourcePristine:
		return "pristine"
	case SourcePatch:
		return "patch/" + s.Head
	case SourceCommits:
		return "commits/" + s.Base + ".." + s.Head
	case SourceWorktreeAndCommits:
		return "worktree-and-commits/" + s.Base + ".." + s.Head
	case SourceStagedUnstagedAndCommits:
		return "staged-and-unstaged-and-commits/" + s.Base + ".." + s.Head
	}
	return ""
}

// LocalSlug identifies a local (non-PR) review session:
// [<owner>/]<repo>@<anchor>/<source>. Owner is empty for checkouts without a
// resolvable origin remote.
type LocalSlug struct {
	Owner  string
	Repo   string
	Anchor SlugAnchor
	Source SlugSource
}

// SameLiveReview reports whether other is the same live review as s at a
// possibly different HEAD: same repo, same branch, same live diff source,
// with only the HEAD token free to differ.
//
// This is the relation that lets a review survive an amend or a rebase. Two
// conditions keep it narrow. The source must be live, because a commit range
// names its own endpoints — a different range is a different review by
// construction, not the same one moved along. And the anchor must be a
// branch: a detached HEAD's anchor is itself derived from the commit, so
// there is no stable identity to carry, and treating two detached checkouts
// as one review would resurrect comments across an unrelated jump.
func (s LocalSlug) SameLiveReview(other LocalSlug) bool {
	if !s.Source.Kind.IsLive() || s.Source.Kind != other.Source.Kind {
		return false
	}
	if s.Anchor.Branch == "" || s.Anchor.Branch != other.Anchor.Branch {
		return false
	}
	return s.Owner == other.Owner && s.Repo == other.Repo
}

// String renders the local slug.
func (s LocalSlug) String() string {
	if s.Owner != "" {
		return s.Owner + "/" + s.Repo + "@" + s.Anchor.String() + "/" + s.Source.String()
	}
	return s.Repo + "@" + s.Anchor.String() + "/" + s.Source.String()
}

// PrSlug identifies a forge pull-request review session:
// <forge-prefix>:<host>/<repo-path...>/pr/<number>. RepoPath holds the path
// segments between host and the pr suffix — owner/repo for GitHub and
// Forgejo, group[/subgroup...]/project for GitLab, org/project/repo for Azure
// DevOps.
type PrSlug struct {
	Forge    forgetypes.Kind
	Host     string
	RepoPath []string
	Number   uint64
}

// String renders the host-qualified PR slug.
func (s PrSlug) String() string {
	return s.Forge.SlugPrefix() + ":" + s.Host + "/" + strings.Join(s.RepoPath, "/") +
		"/pr/" + strconv.FormatUint(s.Number, 10)
}

// Parse parses a slug string into a LocalSlug or PrSlug.
//
// A PR slug starts with a forge prefix followed by ":"; a local slug never
// contains ":" because "[<owner>/]<repo>" is alphanumeric/dash and git ref
// names cannot contain ":". The "mr" segment is accepted as an input alias
// for "pr"; rendering always emits "pr".
func Parse(s string) (Slug, error) {
	if s == "" {
		return nil, ErrEmpty
	}
	if prefix, rest, ok := strings.Cut(s, ":"); ok {
		forge, known := forgetypes.KindFromSlugPrefix(prefix)
		if !known {
			return nil, fmt.Errorf("%w: %s", ErrUnknownForge, prefix)
		}
		return parsePr(forge, rest)
	}
	return parseLocal(s)
}

// parsePr parses the host-qualified remainder of a PR slug:
// <host>/<repo-path...>/pr/<number> with at least two repo path segments.
func parsePr(forge forgetypes.Kind, rest string) (PrSlug, error) {
	parts := strings.Split(rest, "/")
	// host + >=2 repo segments + "pr" + number.
	if len(parts) < 5 {
		return PrSlug{}, fmt.Errorf("%w: %s", ErrInvalidShape, rest)
	}
	prKeyword := parts[len(parts)-2]
	if prKeyword != "pr" && prKeyword != "mr" {
		return PrSlug{}, fmt.Errorf("%w: %s", ErrInvalidShape, rest)
	}
	for _, part := range parts[:len(parts)-1] {
		if part == "" {
			return PrSlug{}, fmt.Errorf("%w: %s", ErrInvalidShape, rest)
		}
	}
	number, err := strconv.ParseUint(parts[len(parts)-1], 10, 64)
	if err != nil {
		return PrSlug{}, fmt.Errorf("%w: %s", ErrInvalidPrNumber, parts[len(parts)-1])
	}
	return PrSlug{
		Forge:    forge,
		Host:     parts[0],
		RepoPath: parts[1 : len(parts)-2],
		Number:   number,
	}, nil
}

// parseLocal parses [<owner>/]<repo>@<anchor>/<source>.
func parseLocal(s string) (LocalSlug, error) {
	project, rest, ok := strings.Cut(s, "@")
	if !ok {
		return LocalSlug{}, fmt.Errorf("%w: %s", ErrInvalidShape, s)
	}

	var owner, repo string
	if o, r, split := strings.Cut(project, "/"); split {
		if strings.Contains(r, "/") || o == "" || r == "" {
			return LocalSlug{}, fmt.Errorf("%w: %s", ErrInvalidShape, s)
		}
		owner, repo = o, r
	} else if project == "" {
		return LocalSlug{}, fmt.Errorf("%w: %s", ErrInvalidShape, s)
	} else {
		repo = project
	}

	anchorStr, sourceStr, ok := strings.Cut(rest, "/")
	if !ok {
		return LocalSlug{}, fmt.Errorf("%w: %s", ErrInvalidShape, s)
	}

	var anchor SlugAnchor
	if sha, isAnon := strings.CutPrefix(anchorStr, "~"); isAnon {
		if sha == "" {
			return LocalSlug{}, fmt.Errorf("%w: %s", ErrInvalidShape, s)
		}
		anchor = SlugAnchor{ShortSHA: sha}
	} else {
		if anchorStr == "" {
			return LocalSlug{}, fmt.Errorf("%w: %s", ErrInvalidShape, s)
		}
		anchor = SlugAnchor{Branch: anchorStr}
	}

	source, err := parseSource(sourceStr)
	if err != nil {
		return LocalSlug{}, err
	}

	return LocalSlug{Owner: owner, Repo: repo, Anchor: anchor, Source: source}, nil
}

// parseSource parses the diff-source segment of a local slug.
//
// The compound *-and-commits/<range> sources must be matched before the
// simple worktree/<head> / staged-and-unstaged/<head> variants because their
// prefixes overlap (e.g. staged-and-unstaged-and-commits/ starts with
// staged-and-unstaged/).
func parseSource(s string) (SlugSource, error) {
	if rangeStr, ok := strings.CutPrefix(s, "commits/"); ok {
		return parseRange(rangeStr, SourceCommits)
	}
	if rangeStr, ok := strings.CutPrefix(s, "worktree-and-commits/"); ok {
		return parseRange(rangeStr, SourceWorktreeAndCommits)
	}
	if rangeStr, ok := strings.CutPrefix(s, "staged-and-unstaged-and-commits/"); ok {
		return parseRange(rangeStr, SourceStagedUnstagedAndCommits)
	}
	if s == "pristine" {
		return SlugSource{Kind: SourcePristine}, nil
	}
	if head, ok := strings.CutPrefix(s, "patch/"); ok {
		return tokenSource(head, s, SourcePatch)
	}
	if head, ok := strings.CutPrefix(s, "worktree/"); ok {
		return tokenSource(head, s, SourceWorktree)
	}
	if head, ok := strings.CutPrefix(s, "staged-and-unstaged/"); ok {
		return tokenSource(head, s, SourceStagedAndUnstaged)
	}
	if head, ok := strings.CutPrefix(s, "staged/"); ok {
		return tokenSource(head, s, SourceStaged)
	}
	if head, ok := strings.CutPrefix(s, "unstaged/"); ok {
		return tokenSource(head, s, SourceUnstaged)
	}
	return SlugSource{}, fmt.Errorf("%w: %s", ErrUnknownSource, s)
}

// tokenSource validates the single token of a source segment that carries one
// — the HEAD SHA of a live source, or the content hash of a patch. It only
// checks the token is present and does not itself contain a "/", which would
// make the segment ambiguous.
func tokenSource(head, full string, kind SourceKind) (SlugSource, error) {
	if head == "" || strings.Contains(head, "/") {
		return SlugSource{}, fmt.Errorf("%w: %s", ErrUnknownSource, full)
	}
	return SlugSource{Kind: kind, Head: head}, nil
}

// parseRange parses a <base>..<head> commit range.
func parseRange(s string, kind SourceKind) (SlugSource, error) {
	base, head, ok := strings.Cut(s, "..")
	if !ok || base == "" || head == "" {
		return SlugSource{}, ErrMissingRange
	}
	return SlugSource{Kind: kind, Base: base, Head: head}, nil
}

// sanitizeRef sanitizes a branch/bookmark name for use as a slug anchor
// segment, replacing "/" with "-". Lossy: feature/login and feature-login
// collapse to the same anchor; in practice collisions are rare.
func sanitizeRef(name string) string {
	return strings.ReplaceAll(name, "/", "-")
}

// shortSHA takes the first shortSHALen characters of a SHA. Shorter inputs
// pass through.
func shortSHA(sha string) string {
	if len(sha) <= shortSHALen {
		return sha
	}
	return sha[:shortSHALen]
}
