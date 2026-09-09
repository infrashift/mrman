package filebackend

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Directory-mode walking.
//
// tuicr walks directories with the Rust `ignore` crate (ripgrep's walker),
// which honors .gitignore files, hidden-entry filtering, and symlink
// boundaries. Go has no equivalent in the standard library and this module
// deliberately avoids both a new dependency and shelling out to git (the
// `--file <dir>` entry point must work outside any repository), so the walk
// is implemented directly: recursive directory traversal that skips hidden
// entries (dotfiles, matching the ignore crate's default) and symlinks, and
// honors nested .gitignore files via a minimal matcher.
//
// The matcher supports the common .gitignore subset: comments and blank
// lines, `!` negation with last-match-wins ordering, trailing-`/`
// directory-only patterns, patterns containing `/` anchored to the
// .gitignore's directory, basename matching for slash-free patterns, `*`/`?`
// globs per path segment, and `**` spanning segments. Exotic features
// (character-class escapes, core.excludesfile, .git/info/exclude) are out of
// scope.

// ignoreRule is one parsed .gitignore pattern.
type ignoreRule struct {
	pattern  string // normalized: no '!' prefix, no trailing '/', no leading '/'
	negate   bool   // pattern started with '!'
	dirOnly  bool   // pattern ended with '/'
	anchored bool   // pattern contains '/' → match against the full relative path
}

// ignoreScope is the rule set of one .gitignore, applying to everything
// under its directory.
type ignoreScope struct {
	dir   string // absolute directory containing the .gitignore
	rules []ignoreRule
}

// FileEntry is one file found by CollectTextFiles.
type FileEntry struct {
	// Path is absolute.
	Path string
	// RelPath is Path relative to the root that was walked, and is what a
	// caller should show and key state on.
	RelPath string
	// Size was recorded at discovery time, so a caller deciding whether a
	// file is too large to render need not stat it again.
	Size int64
}

// CollectTextFiles walks root and returns every non-hidden, non-ignored,
// non-binary regular file under it, sorted by path.
//
// It is exported so review sources outside this package can walk a
// directory the way `--file <dir>` does. That consistency is the point: a
// second walker with different ignore semantics would mean one mode
// silently reviewing a node_modules tree that another correctly skips.
func CollectTextFiles(root string) []FileEntry {
	entries := collectTextFiles(root)
	out := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, FileEntry{
			Path:    e.path,
			RelPath: relativeTo(root, e.path),
			Size:    e.size,
		})
	}
	return out
}

// collectTextFiles walks root and returns every non-hidden, non-ignored,
// non-binary regular file with its size, sorted by path.
func collectTextFiles(root string) []fileEntry {
	var entries []fileEntry
	walkCollect(root, nil, &entries)
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return entries
}

func walkCollect(dir string, scopes []ignoreScope, entries *[]fileEntry) {
	if rules := parseGitignoreFile(filepath.Join(dir, ".gitignore")); len(rules) > 0 {
		scopes = append(scopes, ignoreScope{dir: dir, rules: rules})
	}

	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range dirEntries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue // hidden entries are skipped, like the ignore crate's default
		}
		full := filepath.Join(dir, name)
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			continue // do not follow symlinks
		case entry.IsDir():
			if ignoredBy(scopes, full, true) {
				continue
			}
			walkCollect(full, scopes, entries)
		case entry.Type().IsRegular():
			if ignoredBy(scopes, full, false) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if isProbablyBinary(full) {
				continue
			}
			*entries = append(*entries, fileEntry{path: full, size: info.Size()})
		}
	}
}

// parseGitignoreFile reads and parses one .gitignore, returning nil when the
// file is absent or unreadable.
func parseGitignoreFile(path string) []ignoreRule {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var rules []ignoreRule
	for raw := range strings.SplitSeq(string(data), "\n") {
		line := strings.TrimRight(strings.TrimSuffix(raw, "\r"), " ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			rule.negate = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			rule.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if strings.HasPrefix(line, "/") {
			rule.anchored = true
			line = strings.TrimPrefix(line, "/")
		}
		if strings.Contains(line, "/") {
			rule.anchored = true
		}
		if line == "" {
			continue
		}
		rule.pattern = line
		rules = append(rules, rule)
	}
	return rules
}

// ignoredBy reports whether absPath is ignored under the accumulated scopes.
// Scopes are ordered outermost-first and rules within a scope keep file
// order, so iterating in order and letting later matches win implements
// git's last-match-wins semantics with deeper .gitignore files overriding
// shallower ones.
func ignoredBy(scopes []ignoreScope, absPath string, isDir bool) bool {
	ignored := false
	for _, scope := range scopes {
		rel, err := filepath.Rel(scope.dir, absPath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		rel = filepath.ToSlash(rel)
		for _, rule := range scope.rules {
			if rule.dirOnly && !isDir {
				continue
			}
			if rule.matches(rel) {
				ignored = !rule.negate
			}
		}
	}
	return ignored
}

// matches reports whether the rule matches the slash-separated path rel
// (relative to the rule's .gitignore directory).
func (r ignoreRule) matches(rel string) bool {
	if r.anchored {
		return globMatch(r.pattern, rel)
	}
	return globMatch(r.pattern, path.Base(rel))
}

// globMatch matches pattern against p segment-by-segment, with `**`
// spanning any number of segments.
func globMatch(pattern, p string) bool {
	return segsMatch(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

func segsMatch(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for skip := 0; skip <= len(segs); skip++ {
			if segsMatch(pat[1:], segs[skip:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	ok, err := path.Match(pat[0], segs[0])
	if err != nil || !ok {
		return false
	}
	return segsMatch(pat[1:], segs[1:])
}
