// Package ignore filters review files through the repo root's .gitignore
// and .mrmanignore, ported from tuicr's tuicrignore module. The .mrmanignore
// is loaded after .gitignore, so its patterns win — including `!` negations
// that un-ignore gitignored paths (e.g. reviewing a lockfile the repo
// ignores). Only the two root-level files are consulted; nested ignore files
// are not.
package ignore

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"

	"github.com/infrashift/mrman/internal/model"
)

// FileName is the mrman-specific ignore file.
const FileName = ".mrmanignore"

// Filter matches paths against the combined ignore rules.
type Filter struct {
	matcher  gitignore.Matcher
	hasRules bool
}

// Load builds a filter from root/.gitignore and root/.mrmanignore. Missing
// files simply contribute no patterns.
func Load(root string) *Filter {
	var patterns []gitignore.Pattern
	for _, name := range []string{".gitignore", FileName} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			patterns = append(patterns, gitignore.ParsePattern(trimmed, nil))
		}
	}
	return &Filter{
		matcher:  gitignore.NewMatcher(patterns),
		hasRules: len(patterns) > 0,
	}
}

// HasRules reports whether any ignore pattern was loaded.
func (f *Filter) HasRules() bool { return f.hasRules }

// Ignored reports whether the repo-relative path is excluded.
func (f *Filter) Ignored(relPath string) bool {
	if !f.hasRules {
		return false
	}
	return f.matcher.Match(strings.Split(filepath.ToSlash(relPath), "/"), false)
}

// FilterDiffFiles drops ignored files, matching on the display path so
// deleted files match on their old path.
func (f *Filter) FilterDiffFiles(files []model.DiffFile) []model.DiffFile {
	if !f.hasRules {
		return files
	}
	kept := files[:0]
	for i := range files {
		if !f.Ignored(files[i].DisplayPath()) {
			kept = append(kept, files[i])
		}
	}
	return kept
}

// FilterPaths drops ignored paths, for cheap change-status probes.
func (f *Filter) FilterPaths(paths []string) []string {
	if !f.hasRules {
		return paths
	}
	kept := paths[:0]
	for _, p := range paths {
		if !f.Ignored(p) {
			kept = append(kept, p)
		}
	}
	return kept
}
