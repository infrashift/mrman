// Package vcs defines the version-control abstraction mrman's review flows
// build on, ported from tuicr's VcsBackend trait. Backends live in
// subpackages (git, jj, filebackend, prnoop); operations a backend does not
// support return errors matching errs.ErrUnsupported, which callers use as
// control flow to degrade gracefully.
package vcs

import (
	"time"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
)

// Type identifies a backend implementation.
type Type string

// Backend types.
const (
	TypeGit     Type = "git"
	TypeJujutsu Type = "jj"
	TypeFile    Type = "file"
	// TypePatch is a standalone patch artifact reviewed without a repository.
	TypePatch Type = "patch"
	// TypeDiff is a comparison of two arbitrary paths, neither of which
	// need be in a repository.
	TypeDiff Type = "diff"
)

// Info describes the repository a backend operates on.
type Info struct {
	RootPath   string
	HeadCommit string
	BranchName *string
	Type       Type
}

// CommitInfo is one commit row in selectors and headers.
type CommitInfo struct {
	ID         string
	ShortID    string
	BranchName *string
	Summary    string
	Body       *string
	Author     string
	Time       time.Time
}

// WhitespaceMode controls whitespace handling in diffs.
type WhitespaceMode int

// Whitespace modes.
const (
	WhitespaceNormal WhitespaceMode = iota
	WhitespaceIgnoreAll
)

// ChangeStatus reports whether staged/unstaged changes exist.
type ChangeStatus struct {
	Staged   bool
	Unstaged bool
}

// ChangeKind selects a side for cheap changed-path probes.
type ChangeKind int

// Change kinds.
const (
	ChangeStaged ChangeKind = iota
	ChangeUnstaged
)

// RevisionDiffTarget says how a resolved revision range should be diffed:
// as the union of the commit list, or an explicit base..head pair.
type RevisionDiffTarget struct {
	Explicit bool
	Base     *string
	Head     string
}

// ResolvedRevisionRange is a resolved revset: commit ids OLDEST-FIRST plus
// the diff target.
type ResolvedRevisionRange struct {
	CommitIDs []string
	Target    RevisionDiffTarget
}

// Backend is the VCS abstraction. Optional operations return
// errs.ErrUnsupported-wrapped errors when a backend cannot provide them.
type Backend interface {
	Info() *Info
	StartupWarnings() []string
	SupportsSparseCheckout() bool

	WorkingTreeDiff(h *syntax.Highlighter) ([]model.DiffFile, error)
	StagedDiff(h *syntax.Highlighter) ([]model.DiffFile, error)
	UnstagedDiff(h *syntax.Highlighter) ([]model.DiffFile, error)
	ChangeStatus() (ChangeStatus, error)
	ListChangedPaths(kind ChangeKind) ([]string, error)

	// FetchContextLines and FileLineCount read the new side of the diff:
	// refCommit when set (IndexRef for the index), else the working tree.
	FetchContextLines(path string, status model.FileStatus, refCommit *string, start, end uint32) ([]model.DiffLine, error)
	FileLineCount(path string, status model.FileStatus, refCommit *string) (uint32, error)

	RecentCommits(offset, limit int) ([]CommitInfo, error)
	CommitsInfo(ids []string) ([]CommitInfo, error)
	ResolveRevisionRange(revset string) (ResolvedRevisionRange, error)
	CommitRangeDiff(rng ResolvedRevisionRange, h *syntax.Highlighter) ([]model.DiffFile, error)
	WorkingTreeWithCommitsDiff(ids []string, h *syntax.Highlighter) ([]model.DiffFile, error)

	StageFile(path string) error
}

// UnsupportedBase provides ErrUnsupported defaults for every optional
// Backend method; backends embed it and override what they support.
type UnsupportedBase struct{}

// StartupWarnings returns no warnings by default.
func (UnsupportedBase) StartupWarnings() []string { return nil }

// SupportsSparseCheckout is false by default.
func (UnsupportedBase) SupportsSparseCheckout() bool { return false }

// StagedDiff is unsupported by default.
func (UnsupportedBase) StagedDiff(*syntax.Highlighter) ([]model.DiffFile, error) {
	return nil, errs.Unsupportedf("staged diff")
}

// UnstagedDiff is unsupported by default.
func (UnsupportedBase) UnstagedDiff(*syntax.Highlighter) ([]model.DiffFile, error) {
	return nil, errs.Unsupportedf("unstaged diff")
}

// ChangeStatus is unsupported by default.
func (UnsupportedBase) ChangeStatus() (ChangeStatus, error) {
	return ChangeStatus{}, errs.Unsupportedf("change status")
}

// ListChangedPaths is unsupported by default.
func (UnsupportedBase) ListChangedPaths(ChangeKind) ([]string, error) {
	return nil, errs.Unsupportedf("list changed paths")
}

// RecentCommits returns no commits by default.
func (UnsupportedBase) RecentCommits(int, int) ([]CommitInfo, error) { return nil, nil }

// CommitsInfo returns no commits by default.
func (UnsupportedBase) CommitsInfo([]string) ([]CommitInfo, error) { return nil, nil }

// ResolveRevisionRange is unsupported by default.
func (UnsupportedBase) ResolveRevisionRange(string) (ResolvedRevisionRange, error) {
	return ResolvedRevisionRange{}, errs.Unsupportedf("revision ranges")
}

// CommitRangeDiff is unsupported by default.
func (UnsupportedBase) CommitRangeDiff(ResolvedRevisionRange, *syntax.Highlighter) ([]model.DiffFile, error) {
	return nil, errs.Unsupportedf("commit range diff")
}

// WorkingTreeWithCommitsDiff is unsupported by default.
func (UnsupportedBase) WorkingTreeWithCommitsDiff([]string, *syntax.Highlighter) ([]model.DiffFile, error) {
	return nil, errs.Unsupportedf("working tree with commits diff")
}

// StageFile is unsupported by default.
func (UnsupportedBase) StageFile(string) error {
	return errs.Unsupportedf("staging")
}

// IndexRef is the refCommit that reads the staged (index) version of a
// file: the new side of a staged-only review. Only git has an index, and
// only git offers a staged diff source.
const IndexRef = ":0"

// TooLargeDiffMarker is the line a synthesized diff carries in place of the
// hunks of a file the forge declined to diff because it is too large. The
// parser marks that file IsTooLarge, as it marks a binary file from git's
// "Binary files ... differ", so it renders as too large rather than as a
// file with no changes.
const TooLargeDiffMarker = "Diff too large to display"
