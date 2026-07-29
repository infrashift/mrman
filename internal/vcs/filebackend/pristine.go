package filebackend

// Pristine review path enumeration, ported from tuicr's vcs/pristine.rs.
//
// Whole-repo review mode (`--all-files`) needs a list of every file the user
// can annotate. CollectTrackedPaths shells out to `git ls-files -z` to
// enumerate the tracked set, so untracked build artifacts (target/,
// node_modules/, ...) are excluded without maintaining a deny-list and
// without depending on the absence of a .gitignore. The MVP is git-only; jj
// and mercurial support is deferred to a future follow-up that hoists this
// responsibility into the vcs.Backend interface.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/vcs"
)

// noHeadSentinel is the literal used in place of a HEAD SHA when the
// repository has no commits yet. Keeps the "pristine:HEAD:hash" session key
// well-formed in the empty-repo case.
const noHeadSentinel = "none"

// CollectTrackedPaths enumerates every tracked file in the git repository
// rooted at repoRoot, returning sorted absolute paths.
//
// The list is taken from `git ls-files -z`, so it reflects exactly what git
// considers tracked (post-.gitignore, untracked files excluded).
// Deleted-but-tracked entries are filtered out at the boundary: a path that
// no longer exists on disk is dropped.
//
// It returns errs.ErrNotARepository if `git ls-files` fails (no repository
// at repoRoot, or git not on PATH) and errs.ErrNoChanges when the
// repository exists but has no tracked files on disk.
func CollectTrackedPaths(repoRoot string, run vcs.Runner) ([]string, error) {
	stdout, _, err := run.Run(repoRoot, "git", "ls-files", "-z")
	if err != nil {
		return nil, errs.ErrNotARepository
	}

	var paths []string
	for _, part := range strings.Split(string(stdout), "\x00") {
		if part == "" {
			continue
		}
		path := filepath.Join(repoRoot, part)
		if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
			continue
		}
		paths = append(paths, path)
	}

	sort.Strings(paths)

	if len(paths) == 0 {
		return nil, errs.ErrNoChanges
	}
	return paths, nil
}

// HeadShortSHA returns the short SHA of HEAD for the git repo at repoRoot,
// or the "none" sentinel if HEAD is unborn (e.g. a freshly-initialized repo
// with no commits) or any subprocess error occurs.
//
// The result is used as a component of pristine session keys; an advancing
// HEAD changes the key but the persistence-layer prefix-match keeps
// comments attached across `git pull`.
func HeadShortSHA(repoRoot string, run vcs.Runner) string {
	stdout, _, err := run.Run(repoRoot, "git", "rev-parse", "--short", "HEAD")
	if err != nil {
		return noHeadSentinel
	}
	trimmed := strings.TrimSpace(string(stdout))
	if trimmed == "" {
		return noHeadSentinel
	}
	return trimmed
}
