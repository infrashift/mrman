package slug

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/infrashift/mrman/internal/model"
)

// Derivation errors, mirroring tuicr's SlugDeriveError taxonomy. Match with
// errors.Is.
var (
	// ErrNilSession reports a nil session passed to ForSession.
	ErrNilSession = errors.New("cannot derive slug from nil session")
	// ErrNoRepoName reports a repo path with no usable directory basename.
	ErrNoRepoName = errors.New("cannot determine repo name from path")
	// ErrMissingCommitRange reports a range diff source with no commit range.
	ErrMissingCommitRange = errors.New("commit range required for diff source but missing")
	// ErrMissingPrSessionKey reports a pull-request session without its key.
	ErrMissingPrSessionKey = errors.New("merge request session has no session key")
	// ErrUnsupportedDiffSource reports a session diff source with no slug form.
	ErrUnsupportedDiffSource = errors.New("diff source has no slug form")
)

// Runner executes an external command in a directory and returns its output.
// It is the injection seam that keeps slug derivation testable without git.
type Runner interface {
	Run(dir string, name string, args ...string) (stdout, stderr []byte, err error)
}

// execRunner is the real subprocess-backed Runner used by ForSession.
type execRunner struct{}

// Run executes name with args in dir, capturing stdout and stderr.
func (execRunner) Run(dir string, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// forSessionRunner is the Runner ForSession resolves owner/repo with;
// swappable in tests.
var forSessionRunner Runner = execRunner{}

// ForSession derives the slug identifying a review session.
//
// Pull-request sessions map their PrSessionKey onto a host-qualified PrSlug.
// All other sessions produce a LocalSlug: owner/repo resolve from the
// session's repo path via ResolveOwnerRepo (falling back to the directory
// basename with no owner), the anchor is the sanitized branch name or the
// short base-commit SHA for detached heads, and the source segment encodes
// the diff source with the short HEAD SHA (live sources) or the commit
// range's short base/head SHAs.
func ForSession(s *model.ReviewSession) (Slug, error) {
	if s == nil {
		return nil, ErrNilSession
	}
	if s.DiffSource == model.SourcePullRequest {
		key := s.PrSessionKey
		if key == nil {
			return nil, ErrMissingPrSessionKey
		}
		return PrSlug{
			Forge:    key.Repository.Kind,
			Host:     key.Repository.Host,
			RepoPath: key.Repository.PathSegments(),
			Number:   key.Number,
		}, nil
	}

	owner, repo, err := ResolveOwnerRepo(s.RepoPath, forSessionRunner)
	if err != nil {
		return nil, err
	}
	var anchor SlugAnchor
	if s.BranchName != nil {
		anchor = SlugAnchor{Branch: sanitizeRef(*s.BranchName)}
	} else {
		anchor = SlugAnchor{ShortSHA: shortSHA(s.BaseCommit)}
	}
	source, err := sourceForSession(s)
	if err != nil {
		return nil, err
	}
	return LocalSlug{Owner: owner, Repo: repo, Anchor: anchor, Source: source}, nil
}

// KindForSource maps a session's diff source onto its slug source kind. ok is
// false for sources with no slug form (notably pull requests, which use PrSlug
// instead).
//
// This is the single place the model's diff sources and the slug's source
// kinds are lined up. IsLiveSource and sourceForSession both go through it, so
// a new source cannot be classified as live in one and not the other.
func KindForSource(src model.SessionDiffSource) (SourceKind, bool) {
	switch src {
	case model.SourceWorkingTree:
		return SourceWorktree, true
	case model.SourceStaged:
		return SourceStaged, true
	case model.SourceUnstaged:
		return SourceUnstaged, true
	case model.SourceStagedAndUnstaged:
		return SourceStagedAndUnstaged, true
	case model.SourcePristine:
		return SourcePristine, true
	case model.SourceCommitRange:
		return SourceCommits, true
	case model.SourceWorkingTreeAndCommits:
		return SourceWorktreeAndCommits, true
	case model.SourceStagedUnstagedAndCommits:
		return SourceStagedUnstagedAndCommits, true
	case model.SourcePatch:
		return SourcePatch, true
	}
	return 0, false
}

// IsLiveSource reports whether a session diff source is one of the live
// working-tree kinds, whose slug identity moves when HEAD moves.
//
// Answerable from the source alone, which matters: a range source cannot even
// have its slug derived without a commit range, so callers deciding whether a
// session is a carry-forward candidate must be able to rule it out first.
func IsLiveSource(src model.SessionDiffSource) bool {
	kind, ok := KindForSource(src)
	return ok && kind.IsLive()
}

// sourceForSession maps a session's diff source onto its slug source segment.
func sourceForSession(s *model.ReviewSession) (SlugSource, error) {
	kind, ok := KindForSource(s.DiffSource)
	if !ok {
		return SlugSource{}, fmt.Errorf("%w: %s", ErrUnsupportedDiffSource, s.DiffSource)
	}
	switch {
	case kind.IsLive():
		return SlugSource{Kind: kind, Head: liveHeadToken(s.BaseCommit)}, nil
	case kind == SourcePristine:
		return SlugSource{Kind: SourcePristine}, nil
	case kind == SourcePatch:
		// A patch's BaseCommit is a hash of the artifact, not a commit, so it
		// is shortened the same way but means something different: the same
		// bytes resume the same review, edited bytes start a new one.
		return SlugSource{Kind: SourcePatch, Head: liveHeadToken(s.BaseCommit)}, nil
	}
	return rangeSource(s, kind)
}

// rangeSource builds a range slug source from the session's commit range,
// which is stored newest-first: head is the first element, base the last.
func rangeSource(s *model.ReviewSession, kind SourceKind) (SlugSource, error) {
	if len(s.CommitRange) == 0 {
		return SlugSource{}, fmt.Errorf("%w: %s", ErrMissingCommitRange, s.DiffSource)
	}
	return SlugSource{
		Kind: kind,
		Head: shortSHA(s.CommitRange[0]),
		Base: shortSHA(s.CommitRange[len(s.CommitRange)-1]),
	}, nil
}

// liveHeadToken is the token used in the slug source segment to identify
// HEAD for live diff sources: the short SHA when available, "none" for an
// unborn HEAD (empty commit) so the slug remains well-formed.
func liveHeadToken(headCommit string) string {
	short := shortSHA(headCommit)
	if short == "" {
		return "none"
	}
	return short
}

// ResolveOwnerRepo resolves the (owner, repo) pair for a local checkout from
// its remote origin URL, obtained by running
// "git config --get remote.origin.url" in repoPath via run. It falls back to
// the directory basename with an empty owner when the origin URL is missing
// or unparseable, and fails only when the path has no usable basename.
func ResolveOwnerRepo(repoPath string, run Runner) (owner string, repo string, err error) {
	if run != nil {
		stdout, _, runErr := run.Run(repoPath, "git", "config", "--get", "remote.origin.url")
		if runErr == nil {
			if o, r, ok := parseRemoteOwnerRepo(strings.TrimSpace(string(stdout))); ok {
				return o, r, nil
			}
		}
	}
	name := filepath.Base(filepath.Clean(repoPath))
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return "", "", fmt.Errorf("%w: %s", ErrNoRepoName, repoPath)
	}
	return "", name, nil
}

// parseRemoteOwnerRepo is a forge-agnostic remote-URL parser. It handles
// HTTPS, HTTP, SSH scheme URLs and SCP-style SSH (git@host:path), always
// taking the last two path segments as owner/repo so nested groupings
// (GitLab subgroups, etc.) degrade gracefully. A trailing .git is stripped.
func parseRemoteOwnerRepo(remoteURL string) (owner, repo string, ok bool) {
	url := strings.TrimSpace(remoteURL)
	for _, scheme := range []string{"https://", "http://", "ssh://"} {
		if rest, found := strings.CutPrefix(url, scheme); found {
			return parsePathSegments(rest)
		}
	}
	if _, rest, found := strings.Cut(url, "@"); found {
		return parsePathSegments(strings.Replace(rest, ":", "/", 1))
	}
	return parsePathSegments(url)
}

// parsePathSegments drops the host (everything before the first "/") and
// returns the last two remaining path segments as owner/repo.
func parsePathSegments(s string) (owner, repo string, ok bool) {
	_, path, found := strings.Cut(s, "/")
	if !found {
		return "", "", false
	}
	var segments []string
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		if seg != "" {
			segments = append(segments, seg)
		}
	}
	if len(segments) < 2 {
		return "", "", false
	}
	repoSeg := strings.TrimSuffix(segments[len(segments)-1], ".git")
	ownerSeg := segments[len(segments)-2]
	if repoSeg == "" {
		return "", "", false
	}
	return ownerSeg, repoSeg, true
}
