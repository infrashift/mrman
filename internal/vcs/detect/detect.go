// Package detect auto-detects the VCS backend for a working directory,
// ported from tuicr's detect_vcs.
package detect

import (
	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/vcs"
	"github.com/infrashift/mrman/internal/vcs/git"
	"github.com/infrashift/mrman/internal/vcs/jj"
)

// Detect probes cwd for a supported repository and returns its backend.
//
// Jujutsu is tried first because jj repos are Git-backed and contain a .git
// directory; probing git first would misclassify them. Git is tried next.
// Mercurial is deliberately not ported from tuicr (user decision); an hg
// backend would slot in here after git.
//
// It returns errs.ErrNotARepository when no backend claims cwd.
func Detect(cwd string, ws vcs.WhitespaceMode, run vcs.Runner) (vcs.Backend, error) {
	if backend, err := jj.Discover(cwd, ws, run); err == nil {
		return backend, nil
	}
	if backend, err := git.Discover(cwd, ws, run); err == nil {
		return backend, nil
	}
	return nil, errs.ErrNotARepository
}
