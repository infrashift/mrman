package git

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/vcs"
)

// RecentCommits pages through `git log` from HEAD. An unborn HEAD (fresh
// `git init`) yields an empty list rather than an error so startup can fall
// through to the staged/unstaged paths.
func (b *Backend) RecentCommits(offset, limit int) ([]vcs.CommitInfo, error) {
	if _, err := b.git("rev-parse", "--verify", "HEAD"); err != nil {
		return nil, nil //nolint:nilerr // an unborn HEAD has no commits, which is not an error
	}
	branchTipNames := b.branchTipNames()
	output, err := b.git("log",
		fmt.Sprintf("--skip=%d", offset),
		fmt.Sprintf("--max-count=%d", limit),
		commitFormat)
	if err != nil {
		return nil, err
	}
	return parseCommitRecords(output, branchTipNames), nil
}

// CommitsInfo fetches commit metadata for the given ids via `git show -s`.
func (b *Backend) CommitsInfo(ids []string) ([]vcs.CommitInfo, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	branchTipNames := b.branchTipNames()
	args := append([]string{"show", "-s", commitFormat}, ids...)
	output, err := b.git(args...)
	if err != nil {
		return nil, err
	}
	return parseCommitRecords(output, branchTipNames), nil
}

// ResolveRevisionRange resolves a user-supplied revision expression (REV,
// A..B, A.., ..B, or A...B) into concrete commit ids (oldest first) and a
// diff target.
func (b *Backend) ResolveRevisionRange(revset string) (vcs.ResolvedRevisionRange, error) {
	expr, err := parseRevisionExpression(revset)
	if err != nil {
		return vcs.ResolvedRevisionRange{}, err
	}

	switch expr.kind {
	case revisionSingle:
		// `HEAD`
		head, err := b.resolveCommitID(expr.left)
		if err != nil {
			return vcs.ResolvedRevisionRange{}, err
		}
		base := b.parentRev(head)
		return vcs.ResolvedRevisionRange{
			CommitIDs: []string{head},
			Target:    vcs.RevisionDiffTarget{Explicit: true, Base: base, Head: head},
		}, nil
	case revisionRange:
		// `A..B`, `A..`, or `..B`
		base, err := b.resolveCommitID(expr.left)
		if err != nil {
			return vcs.ResolvedRevisionRange{}, err
		}
		head, err := b.resolveCommitID(expr.right)
		if err != nil {
			return vcs.ResolvedRevisionRange{}, err
		}
		commitIDs, err := b.revListRange(base, head)
		if err != nil {
			return vcs.ResolvedRevisionRange{}, err
		}
		return vcs.ResolvedRevisionRange{
			CommitIDs: commitIDs,
			Target:    vcs.RevisionDiffTarget{Explicit: true, Base: &base, Head: head},
		}, nil
	default:
		// `A...B`
		left, err := b.resolveCommitID(expr.left)
		if err != nil {
			return vcs.ResolvedRevisionRange{}, err
		}
		right, err := b.resolveCommitID(expr.right)
		if err != nil {
			return vcs.ResolvedRevisionRange{}, err
		}
		mergeBase, err := b.git("merge-base", left, right)
		if err != nil {
			return vcs.ResolvedRevisionRange{}, err
		}
		base := strings.TrimSpace(mergeBase)
		commitIDs, err := b.revListRange(base, right)
		if err != nil {
			return vcs.ResolvedRevisionRange{}, err
		}
		return vcs.ResolvedRevisionRange{
			CommitIDs: commitIDs,
			Target:    vcs.RevisionDiffTarget{Explicit: true, Base: &base, Head: right},
		}, nil
	}
}

// revisionExpression is Git's parsed view of a user-supplied revision
// string, a port of tuicr's RevisionExpression.
type revisionExpression struct {
	kind  revisionKind
	left  string
	right string
}

type revisionKind int

const (
	// revisionSingle is a single commit expression, e.g. `HEAD` (in left).
	revisionSingle revisionKind = iota
	// revisionRange is a two-dot range: base in left, head in right.
	// Open-ended forms are normalized to HEAD.
	revisionRange
	// revisionMergeBase is a three-dot range, e.g. `A...B`.
	revisionMergeBase
)

// parseRevisionExpression parses Git revision syntax accepted by the
// revision option: `REV`, `A..B`, `A..`, `..B`, and `A...B`.
func parseRevisionExpression(revisions string) (revisionExpression, error) {
	if left, right, found := strings.Cut(revisions, "..."); found {
		if left == "" || right == "" {
			return revisionExpression{}, &errs.VcsCommand{
				Detail: "Invalid revision range: missing endpoint"}
		}
		return revisionExpression{kind: revisionMergeBase, left: left, right: right}, nil
	}

	if base, head, found := strings.Cut(revisions, ".."); found {
		if base == "" {
			base = "HEAD"
		}
		if head == "" {
			head = "HEAD"
		}
		return revisionExpression{kind: revisionRange, left: base, right: head}, nil
	}

	return revisionExpression{kind: revisionSingle, left: revisions}, nil
}

// resolveCommitID peels a revision to a commit id via
// `git rev-parse --verify REV^{commit}`.
func (b *Backend) resolveCommitID(revision string) (string, error) {
	out, err := b.git("rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// parentRev resolves a commit's first parent. Single-revision reviews diff
// the commit against its first parent; root commits have no parent, so the
// old side is represented as the empty tree with nil.
func (b *Backend) parentRev(commitID string) *string {
	out, err := b.git("rev-parse", commitID+"^")
	if err != nil {
		return nil
	}
	parent := strings.TrimSpace(out)
	return &parent
}

// parentRevOrEmpty resolves a commit's first parent, falling back to the
// empty tree for root commits.
func (b *Backend) parentRevOrEmpty(commitID string) string {
	if parent := b.parentRev(commitID); parent != nil {
		return *parent
	}
	return emptyTreeOID
}

// revListRange lists base..head oldest-first; an empty selection is
// errs.ErrNoChanges.
func (b *Backend) revListRange(base, head string) ([]string, error) {
	output, err := b.git("rev-list", "--topo-order", "--reverse", base+".."+head)
	if err != nil {
		return nil, err
	}

	var commitIDs []string
	for line := range strings.SplitSeq(output, "\n") {
		if line != "" {
			commitIDs = append(commitIDs, line)
		}
	}
	if len(commitIDs) == 0 {
		return nil, errs.ErrNoChanges
	}
	return commitIDs, nil
}

// branchTipNames maps commit ids to the sorted short names of local
// branches whose tip they are. Failures degrade to no decoration.
func (b *Backend) branchTipNames() map[string][]string {
	output, err := b.git("for-each-ref",
		"--format=%(objectname)%00%(refname:short)", "refs/heads")
	if err != nil {
		output = ""
	}

	namesByTip := make(map[string][]string)
	for line := range strings.SplitSeq(output, "\n") {
		if oid, name, found := strings.Cut(line, "\x00"); found {
			namesByTip[oid] = append(namesByTip[oid], name)
		}
	}
	for _, names := range namesByTip {
		sort.Strings(names)
	}
	return namesByTip
}

// parseCommitRecords parses commitFormat output: records separated by
// 0x1e, fields separated by NUL.
func parseCommitRecords(output string, branchTipNames map[string][]string) []vcs.CommitInfo {
	var commits []vcs.CommitInfo
	for record := range strings.SplitSeq(output, "\x1e") {
		if commit, ok := parseCommitRecord(record, branchTipNames); ok {
			commits = append(commits, commit)
		}
	}
	return commits
}

func parseCommitRecord(record string, branchTipNames map[string][]string) (vcs.CommitInfo, bool) {
	record = strings.Trim(record, "\n")
	if record == "" {
		return vcs.CommitInfo{}, false
	}

	fields := strings.SplitN(record, "\x00", 5)
	if len(fields) < 2 {
		return vcs.CommitInfo{}, false
	}
	id := fields[0]
	shortID := fields[1]
	author := "Unknown"
	if len(fields) > 2 {
		author = fields[2]
	}
	var timestamp int64
	if len(fields) > 3 {
		if parsed, err := strconv.ParseInt(fields[3], 10, 64); err == nil {
			timestamp = parsed
		}
	}
	fullMessage := "(no message)"
	if len(fields) > 4 {
		fullMessage = fields[4]
	}
	summary, body := parseCommitMessage(fullMessage)

	var branchName *string
	if names := branchTipNames[id]; len(names) > 0 {
		branchName = &names[0]
	}

	return vcs.CommitInfo{
		ID:         id,
		ShortID:    shortID,
		BranchName: branchName,
		Summary:    summary,
		Body:       body,
		Author:     author,
		Time:       time.Unix(timestamp, 0).UTC(),
	}, true
}

// parseCommitMessage splits a full commit message into its summary line and
// optional body (leading blank lines stripped).
func parseCommitMessage(message string) (string, *string) {
	lines := splitLines(message)
	if len(lines) == 0 {
		return "(no message)", nil
	}
	summary := lines[0]
	rest := lines[1:]
	for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
		rest = rest[1:]
	}
	body := strings.Join(rest, "\n")
	if strings.TrimSpace(body) == "" {
		return summary, nil
	}
	return summary, &body
}
