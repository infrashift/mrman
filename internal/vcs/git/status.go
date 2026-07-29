package git

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// ChangeStatus probes for staged and unstaged changes. Tracked changes have
// cheap exact probes; untracked files require a working-tree scan, so that
// cost is only paid when tracked unstaged changes have not already proven
// the "unstaged" row should be shown.
func (b *Backend) ChangeStatus() (vcs.ChangeStatus, error) {
	staged, err := b.hasDiffChanges("diff", "--quiet", "--cached", "--")
	if err != nil {
		return vcs.ChangeStatus{}, err
	}
	trackedUnstaged, err := b.hasDiffChanges("diff", "--quiet", "--")
	if err != nil {
		return vcs.ChangeStatus{}, err
	}
	unstaged := trackedUnstaged
	if !trackedUnstaged {
		pathspecs, err := b.sparseCheckoutUntrackedPathspecs()
		if err != nil {
			return vcs.ChangeStatus{}, err
		}
		unstaged, err = b.hasUntrackedChanges(pathspecs)
		if err != nil {
			return vcs.ChangeStatus{}, err
		}
	}
	return vcs.ChangeStatus{Staged: staged, Unstaged: unstaged}, nil
}

// ListChangedPaths lists the paths with staged or unstaged changes;
// unstaged includes untracked files.
func (b *Backend) ListChangedPaths(kind vcs.ChangeKind) ([]string, error) {
	switch kind {
	case vcs.ChangeStaged:
		return b.listDiffPaths("diff", "--cached", "--name-only", "-z", "--")
	default:
		paths, err := b.listDiffPaths("diff", "--name-only", "-z", "--")
		if err != nil {
			return nil, err
		}
		pathspecs, err := b.sparseCheckoutUntrackedPathspecs()
		if err != nil {
			return nil, err
		}
		untracked, err := b.listUntrackedPaths(pathspecs)
		if err != nil {
			return nil, err
		}
		return append(paths, untracked...), nil
	}
}

// FetchContextLines reads [start, end] (1-indexed, inclusive) of the file
// for gap expansion: from refCommit's tree when set, from HEAD for deleted
// files, from the working tree otherwise.
func (b *Backend) FetchContextLines(path string, status model.FileStatus, refCommit *string, start, end uint32) ([]model.DiffLine, error) {
	if start > end || start == 0 {
		return nil, nil
	}

	content, err := b.readFileContent(path, status, refCommit)
	if err != nil {
		return nil, err
	}
	return vcs.SliceContextLines(content, start, end), nil
}

// FileLineCount returns the total number of lines in the file, read from
// the same source FetchContextLines uses.
func (b *Backend) FileLineCount(path string, status model.FileStatus, refCommit *string) (uint32, error) {
	content, err := b.readFileContent(path, status, refCommit)
	if err != nil {
		return 0, err
	}
	return countLines(content), nil
}

// StageFile stages a single path via `git add -- <path>`.
func (b *Backend) StageFile(path string) error {
	_, stderr, err := b.run.Run(b.root, "git", "add", "--", path)
	if err != nil {
		if _, exited := exitCode(err); exited {
			return &errs.VcsCommand{Detail: strings.TrimSpace(string(stderr))}
		}
		return &errs.VcsCommand{Detail: "Failed to run git: " + err.Error()}
	}
	return nil
}

func (b *Backend) readFileContent(path string, status model.FileStatus, refCommit *string) (string, error) {
	if refCommit != nil {
		content, ok := b.readGitObject(*refCommit + ":" + path)
		if !ok {
			return "", &errs.VcsCommand{Detail: "failed to read " + path + " at " + *refCommit}
		}
		return content, nil
	}
	if status == model.StatusDeleted {
		content, ok := b.readGitObject("HEAD:" + path)
		if !ok {
			return "", &errs.VcsCommand{Detail: "failed to read deleted file from HEAD"}
		}
		return content, nil
	}
	data, err := os.ReadFile(filepath.Join(b.root, path))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// countLines counts lines like Rust's str::lines: a trailing newline does
// not add an empty final line.
func countLines(content string) uint32 {
	if content == "" {
		return 0
	}
	n := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		n++
	}
	return uint32(n)
}

// hasDiffChanges runs a `git diff --quiet` probe: exit 0 means no changes,
// exit 1 means changes, anything else is an error.
func (b *Backend) hasDiffChanges(args ...string) (bool, error) {
	_, stderr, err := b.run.Run(b.root, "git", args...)
	if err == nil {
		return false, nil
	}
	if code, exited := exitCode(err); exited && code == 1 {
		return true, nil
	}
	return false, &errs.VcsCommand{Detail: strings.TrimSpace(string(stderr))}
}

// listDiffPaths lists NUL-separated paths from a `git diff --name-only -z`
// invocation.
func (b *Backend) listDiffPaths(args ...string) ([]string, error) {
	out, err := b.git(args...)
	if err != nil {
		return nil, err
	}
	return splitNulPaths(out), nil
}

// listUntrackedPaths lists untracked, non-ignored files, optionally
// narrowed to the given pathspecs.
func (b *Backend) listUntrackedPaths(pathspecs []string) ([]string, error) {
	args := []string{"ls-files", "--others", "--exclude-standard", "-z"}
	args = appendPathspecs(args, pathspecs)
	out, err := b.git(args...)
	if err != nil {
		return nil, err
	}
	return splitNulPaths(out), nil
}

// hasUntrackedChanges reports whether any untracked file or directory
// exists (the --directory flag lets git stop descending into fully
// untracked directories).
func (b *Backend) hasUntrackedChanges(pathspecs []string) (bool, error) {
	args := []string{"ls-files", "--others", "--exclude-standard", "-z", "--directory"}
	args = appendPathspecs(args, pathspecs)
	out, err := b.git(args...)
	if err != nil {
		return false, err
	}
	return len(splitNulPaths(out)) > 0, nil
}

// sparseCheckoutUntrackedPathspecs narrows untracked scans to the
// checked-out cones when the sparse patterns are simple paths. Complex
// patterns fall back to Git's full scan so valid untracked files are not
// accidentally hidden. A repo without sparse checkout yields no pathspecs.
func (b *Backend) sparseCheckoutUntrackedPathspecs() ([]string, error) {
	stdout, stderr, err := b.run.Run(b.root, "git", "sparse-checkout", "list")
	if err != nil {
		if _, exited := exitCode(err); exited {
			return nil, nil
		}
		return nil, gitCommandError(stdout, stderr, err)
	}
	out := string(stdout)

	var pathspecs []string
	for _, line := range strings.Split(out, "\n") {
		pattern := strings.TrimSpace(line)
		if pattern == "" {
			continue
		}
		if !isSimpleSparsePath(pattern) {
			return nil, nil
		}
		pathspec := strings.Trim(pattern, "/")
		if pathspec != "" {
			pathspecs = append(pathspecs, pathspec)
		}
	}
	return pathspecs, nil
}

func isSimpleSparsePath(pattern string) bool {
	return !strings.HasPrefix(pattern, "!") && !strings.ContainsAny(pattern, `*?[\`)
}

func appendPathspecs(args, pathspecs []string) []string {
	if len(pathspecs) == 0 {
		return args
	}
	return append(append(args, "--"), pathspecs...)
}

// splitNulPaths parses a NUL-separated path stream (e.g. the output of
// `git diff -z --name-only`), skipping empty entries.
func splitNulPaths(out string) []string {
	var paths []string
	for _, chunk := range strings.Split(out, "\x00") {
		if chunk != "" {
			paths = append(paths, chunk)
		}
	}
	return paths
}
