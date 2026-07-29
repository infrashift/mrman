// Package prnoop provides the placeholder VCS backend used in PR diff mode,
// ported from tuicr's vcs/pr_noop.rs.
//
// When the app enters PR mode, the diff comes from the forge (`gh pr diff`),
// not from a local working tree. The vcs.Backend slot still needs to be
// filled because the app and a number of other call sites assume one is
// always present. Backend satisfies that requirement without doing any real
// work: every method either succeeds with an empty result or returns an
// errs.ErrUnsupported-wrapped error. PR-mode code paths route through the
// forge context provider for the operations that matter (context expansion);
// they never call into the VCS backend.
package prnoop

import (
	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
)

// Backend is the no-op VCS backend for PR mode.
type Backend struct {
	vcs.UnsupportedBase
	info vcs.Info
}

// New builds a no-op backend rooted at root. As in tuicr's PR mode, the
// backend reports vcs.TypeFile (PR diffs are forge-sourced, not local) with
// no head commit and no branch.
func New(root string) *Backend {
	return &Backend{info: vcs.Info{
		RootPath:   root,
		HeadCommit: "",
		BranchName: nil,
		Type:       vcs.TypeFile,
	}}
}

// Info returns the placeholder repository info.
func (b *Backend) Info() *vcs.Info { return &b.info }

// WorkingTreeDiff always fails: PR mode does not read from the local
// working tree.
func (b *Backend) WorkingTreeDiff(*syntax.Highlighter) ([]model.DiffFile, error) {
	return nil, errs.Unsupportedf("PR mode does not read from the local working tree")
}

// FetchContextLines returns no lines. PR-mode context expansion routes
// through the forge context provider; if anything reaches this backend it is
// a routing bug, so degrade gracefully instead of failing.
func (b *Backend) FetchContextLines(string, model.FileStatus, *string, uint32, uint32) ([]model.DiffLine, error) {
	return []model.DiffLine{}, nil
}

// FileLineCount reports zero lines; PR mode has no local files to measure.
func (b *Backend) FileLineCount(string, model.FileStatus, *string) (uint32, error) {
	return 0, nil
}
