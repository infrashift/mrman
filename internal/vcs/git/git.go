// Package git implements the vcs.Backend interface by shelling out to the
// git CLI. It is a port of tuicr's GitCliBackend (src/vcs/git/cli.rs):
// diffs are produced by `git diff` and parsed through
// internal/vcs/diffparser, untracked files are synthesized as
// addition-only diffs, and revision ranges support Git's REV, A..B, A..,
// ..B, and A...B forms.
package git

import (
	"errors"
	"strings"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/vcs"
)

// Untracked files larger than this are shown in the file list but their
// content is not parsed: they are likely logs, dumps, or build artefacts.
const maxUntrackedFileSize = 10 * 1024 * 1024

// emptyTreeOID is Git's well-known empty tree object, used as the old side
// when diffing a root commit.
const emptyTreeOID = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// commitFormat renders one NUL-separated commit record per commit,
// terminated by an ASCII record separator (0x1e).
const commitFormat = "--format=%H%x00%h%x00%an%x00%ct%x00%B%x1e"

// repoMode classifies how the repository's checkout is materialized.
type repoMode int

const (
	repoStandard repoMode = iota
	repoSparseCheckout
	repoSparseIndex
)

func (m repoMode) isSparseCheckout() bool { return m != repoStandard }

// Backend is the git CLI vcs.Backend implementation.
type Backend struct {
	run            vcs.Runner
	root           string
	info           vcs.Info
	repoMode       repoMode
	untrackedCache bool
	fsmonitor      bool
	whitespace     vcs.WhitespaceMode
}

var _ vcs.Backend = (*Backend)(nil)

// Discover locates the repository containing cwd and builds a Backend for
// it. It returns an error matching errs.ErrNotARepository when cwd is not
// inside a git work tree.
func Discover(cwd string, ws vcs.WhitespaceMode, run vcs.Runner) (*Backend, error) {
	out, err := gitRun(run, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, errs.ErrNotARepository
	}
	root := strings.TrimSpace(out)

	b := &Backend{run: run, root: root, whitespace: ws}
	b.repoMode = b.detectRepoMode()

	headCommit := "HEAD"
	if head, err := b.git("rev-parse", "HEAD"); err == nil {
		headCommit = strings.TrimSpace(head)
	}
	var branchName *string
	if branch, err := b.git("symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		if trimmed := strings.TrimSpace(branch); trimmed != "" {
			branchName = &trimmed
		}
	}
	b.untrackedCache, b.fsmonitor = b.detectRuntimeFlags()

	b.info = vcs.Info{
		RootPath:   root,
		HeadCommit: headCommit,
		BranchName: branchName,
		Type:       vcs.TypeGit,
	}
	return b, nil
}

// Info returns the discovered repository description.
func (b *Backend) Info() *vcs.Info { return &b.info }

// StartupWarnings reports sparse-checkout performance caveats.
func (b *Backend) StartupWarnings() []string {
	if !b.repoMode.isSparseCheckout() {
		return nil
	}

	warnings := []string{"Sparse checkout detected; using Git CLI backend."}
	if !b.untrackedCache {
		fsmonitorState := "not enabled"
		if b.fsmonitor {
			fsmonitorState = "enabled"
		}
		warnings = append(warnings,
			"Sparse checkout without core.untrackedCache can make untracked scans slow; "+
				"run `git update-index --test-untracked-cache` then "+
				"`git config core.untrackedCache true` if it passes (fsmonitor: "+
				fsmonitorState+").")
	}
	return warnings
}

// SupportsSparseCheckout is always true for the CLI backend.
func (b *Backend) SupportsSparseCheckout() bool { return true }

func (b *Backend) detectRepoMode() repoMode {
	out, err := b.git("config", "--get-regexp", `^(core\.sparsecheckout|index\.sparse)$`)
	if err != nil {
		out = ""
	}
	return repoModeFromConfig(out)
}

func repoModeFromConfig(output string) repoMode {
	sparseCheckout := false
	sparseIndex := false
	for key, value := range configLines(output) {
		switch key {
		case "core.sparsecheckout":
			sparseCheckout = gitBoolConfigEnabled(value)
		case "index.sparse":
			sparseIndex = gitBoolConfigEnabled(value)
		}
	}
	switch {
	case sparseIndex:
		return repoSparseIndex
	case sparseCheckout:
		return repoSparseCheckout
	default:
		return repoStandard
	}
}

func (b *Backend) detectRuntimeFlags() (untrackedCache, fsmonitor bool) {
	out, err := b.git("config", "--get-regexp",
		`^(core\.untrackedcache|core\.fsmonitor|feature\.manyfiles)$`)
	if err != nil {
		out = ""
	}
	return parseRuntimeFlags(out)
}

func parseRuntimeFlags(output string) (untrackedCache, fsmonitor bool) {
	var untracked *bool
	manyFiles := false
	for key, value := range configLines(output) {
		switch key {
		case "core.untrackedcache":
			enabled := gitBoolConfigEnabled(value)
			untracked = &enabled
		case "core.fsmonitor":
			fsmonitor = gitFsmonitorConfigEnabled(value)
		case "feature.manyfiles":
			manyFiles = gitBoolConfigEnabled(value)
		}
	}
	// `feature.manyFiles` makes core.untrackedCache default to true, but
	// `git config --get core.untrackedCache` does not print that implied
	// value.
	if untracked == nil {
		return manyFiles, fsmonitor
	}
	return *untracked, fsmonitor
}

// configLines iterates `git config --get-regexp` output as (key, value)
// pairs; the value is everything after the first whitespace run.
func configLines(output string) func(yield func(string, string) bool) {
	return func(yield func(string, string) bool) {
		for line := range strings.SplitSeq(output, "\n") {
			if line == "" {
				continue
			}
			key, value := line, ""
			if idx := strings.IndexAny(line, " \t"); idx >= 0 {
				key, value = line[:idx], line[idx+1:]
			}
			if key == "" {
				continue
			}
			if !yield(key, value) {
				return
			}
		}
	}
}

func gitBoolConfigEnabled(value string) bool {
	switch strings.TrimSpace(value) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

func gitFsmonitorConfigEnabled(value string) bool {
	value = strings.TrimSpace(value)
	if gitBoolConfigEnabled(value) {
		return true
	}
	switch value {
	case "", "false", "0", "no", "off":
		return false
	}
	return true
}

// git runs a git command in the repository root, returning raw stdout.
func (b *Backend) git(args ...string) (string, error) {
	return gitRun(b.run, b.root, args...)
}

// gitRun runs a git command in dir, mapping failures to *errs.VcsCommand
// (stderr for command failures, a spawn message otherwise), mirroring
// tuicr's git_command_error.
func gitRun(run vcs.Runner, dir string, args ...string) (string, error) {
	stdout, stderr, err := run.Run(dir, "git", args...)
	if err != nil {
		return "", gitCommandError(stdout, stderr, err)
	}
	return string(stdout), nil
}

func gitCommandError(stdout, stderr []byte, err error) error {
	detail := strings.TrimSpace(string(stderr))
	if _, exited := exitCode(err); exited {
		if detail == "" {
			detail = strings.TrimSpace(string(stdout))
		}
		if detail == "" {
			detail = err.Error()
		}
		return &errs.VcsCommand{Detail: detail}
	}
	msg := "Failed to run git: " + err.Error()
	if detail != "" {
		msg += ": " + detail
	}
	return &errs.VcsCommand{Detail: msg}
}

// exitCode extracts a process exit code from a Runner error; ok is false
// when the command did not run at all (spawn failure).
func exitCode(err error) (code int, ok bool) {
	var exitErr interface{ ExitCode() int }
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}
