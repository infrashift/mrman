package patch

import (
	"strings"

	"github.com/infrashift/mrman/internal/vcs"
)

// DefaultStripLevel is how many leading path components to drop, matching
// `patch -p1` and `git am -p1` — the convention every posted patch follows.
const DefaultStripLevel = 1

// splitBody separates a message body into its commit-message prose and its
// diff.
//
// `git format-patch` puts a "---" line between the two, but so does a mail
// signature and so does prose, and a bare diff has no separator at all. So
// the split is driven by where the diff demonstrably starts, not by a
// separator: everything before the first file header is prose.
//
// The diffstat that format-patch writes just above the diff is dropped. It is
// generated, it is not part of what the author wrote, and its lines begin
// with a space — which is a context line to a diff parser.
func splitBody(body string) (changelog, diff string) {
	lines := strings.Split(body, "\n")
	start := diffStart(lines)
	if start < 0 {
		return strings.TrimRight(body, "\n"), ""
	}

	head := lines[:start]
	// Trim the trailing "---" separator and the diffstat block above the diff.
	for len(head) > 0 {
		last := strings.TrimRight(head[len(head)-1], " \t")
		if last == "" || last == "---" || isDiffstatLine(last) {
			head = head[:len(head)-1]
			continue
		}
		break
	}

	return strings.TrimRight(strings.Join(head, "\n"), "\n"),
		strings.Join(lines[start:], "\n")
}

// diffStart returns the index of the first line that begins a file diff, or
// -1 when the body carries none.
func diffStart(lines []string) int {
	for i, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			return i
		}
		if isUnifiedFileStart(lines, i) {
			return i
		}
	}
	return -1
}

// isDiffstatLine reports whether a line belongs to a generated diffstat:
// " path/to/file.c | 12 ++++----" or " 3 files changed, 9 insertions(+)".
func isDiffstatLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.Contains(trimmed, "file changed") || strings.Contains(trimmed, "files changed") {
		return true
	}
	name, count, ok := strings.Cut(trimmed, "|")
	if !ok || strings.TrimSpace(name) == "" {
		return false
	}
	// The right side is a number then a run of +/- (or "Bin ... bytes").
	count = strings.TrimSpace(count)
	if strings.HasPrefix(count, "Bin") {
		return true
	}
	digits, rest, _ := strings.Cut(count, " ")
	if digits == "" || strings.TrimLeft(digits, "0123456789") != "" {
		return false
	}
	return strings.TrimLeft(strings.TrimSpace(rest), "+-") == ""
}

// Normalize rewrites a diff into the shape diffparser expects: every file
// introduced by a "diff --git a/X b/X" header with a/ and b/ prefixes.
//
// This is the whole reason the diff parser needs no knowledge of patches. It
// recognises files by their "diff --git " header, which a quilt or `diff -u`
// patch does not have, and it strips a/ and b/ prefixes unconditionally,
// which corrupts a real path named "a/thing" under -p0. Emitting the prefixes
// ourselves means we control both ends of that assumption instead of working
// around it.
//
// stripLevel is how many leading components to remove from the paths a patch
// declares; pass DefaultStripLevel unless the caller knows better.
func Normalize(diff string, stripLevel int) string {
	if diff == "" {
		return ""
	}
	lines := strings.Split(diff, "\n")

	starts := fileStarts(lines)
	var out []string
	for i, start := range starts {
		end := len(lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		out = append(out, normalizeFile(lines[start:end], stripLevel)...)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// fileStarts returns the index of every line that begins a file diff.
//
// A "diff --git" line always starts one. A ---/+++/@@ triple starts one only
// when no git header is waiting for it: a git-format file carries both, and
// counting its own ---/+++ pair as a second file would split every file in
// two. The pending flag is consumed rather than latched so that a patch
// mixing both shapes still splits correctly.
func fileStarts(lines []string) []int {
	var starts []int
	pendingGitHeader := false
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			starts = append(starts, i)
			pendingGitHeader = true
		case isUnifiedFileStart(lines, i):
			if pendingGitHeader {
				pendingGitHeader = false
				continue
			}
			starts = append(starts, i)
		}
	}
	return starts
}

// normalizeFile emits one file's normalised block.
func normalizeFile(body []string, stripLevel int) []string {
	oldPath, newPath, rest := fileHeaderPaths(body, stripLevel)
	if oldPath == "" && newPath == "" {
		// Nothing recognisable; pass through untouched rather than guess.
		return body
	}

	// A /dev/null side keeps its marker so the parser derives add/delete.
	oldSide, newSide := "a/"+oldPath, "b/"+newPath
	if oldPath == devNull {
		oldSide = devNull
	}
	if newPath == devNull {
		newSide = devNull
	}
	// The "diff --git" line always names real paths, even for an add or a
	// delete, so fall back to the side that exists.
	gitOld, gitNew := oldPath, newPath
	if gitOld == devNull {
		gitOld = newPath
	}
	if gitNew == devNull {
		gitNew = oldPath
	}

	block := []string{"diff --git a/" + gitOld + " b/" + gitNew}
	block = append(block, rest...)
	block = append(block, "--- "+oldSide, "+++ "+newSide)
	block = append(block, hunkLines(body)...)
	return block
}

const devNull = "/dev/null"

// fileHeaderPaths pulls the old and new paths out of a file's header lines and
// returns the metadata worth keeping between the "diff --git" line and the
// ---/+++ pair (mode changes, rename and copy records, similarity, index).
func fileHeaderPaths(body []string, stripLevel int) (oldPath, newPath string, meta []string) {
	for _, line := range body {
		switch {
		case strings.HasPrefix(line, "--- "):
			oldPath = stripPath(strings.TrimPrefix(line, "--- "), stripLevel)
		case strings.HasPrefix(line, "+++ "):
			newPath = stripPath(strings.TrimPrefix(line, "+++ "), stripLevel)
		case strings.HasPrefix(line, "diff --git "):
			if oldPath == "" && newPath == "" {
				// Only a fallback: the ---/+++ pair is authoritative when present.
				if a, b, ok := gitHeaderPaths(line); ok {
					oldPath, newPath = a, b
				}
			}
		case strings.HasPrefix(line, "old mode "), strings.HasPrefix(line, "new mode "),
			strings.HasPrefix(line, "new file mode "), strings.HasPrefix(line, "deleted file mode "),
			strings.HasPrefix(line, "similarity index "), strings.HasPrefix(line, "dissimilarity index "),
			strings.HasPrefix(line, "index "),
			strings.HasPrefix(line, "GIT binary patch"),
			strings.HasPrefix(line, "Binary file"):
			meta = append(meta, line)
		case strings.HasPrefix(line, "rename from "), strings.HasPrefix(line, "rename to "),
			strings.HasPrefix(line, "copy from "), strings.HasPrefix(line, "copy to "):
			meta = append(meta, line)
			// A pure rename carries no ---/+++ pair, so take its paths here.
			if p, ok := strings.CutPrefix(line, "rename from "); ok && oldPath == "" {
				oldPath = stripPath(p, 0)
			}
			if p, ok := strings.CutPrefix(line, "rename to "); ok && newPath == "" {
				newPath = stripPath(p, 0)
			}
			if p, ok := strings.CutPrefix(line, "copy from "); ok && oldPath == "" {
				oldPath = stripPath(p, 0)
			}
			if p, ok := strings.CutPrefix(line, "copy to "); ok && newPath == "" {
				newPath = stripPath(p, 0)
			}
		case strings.HasPrefix(line, "@@"):
			return oldPath, newPath, meta
		}
	}
	return oldPath, newPath, meta
}

// gitHeaderPaths parses "diff --git a/X b/Y". Paths may contain spaces, so
// the " b/" separator anchors the split, matching diffparser's own reading.
//
// The a/ and b/ here are git's own markers on this one line, not part of the
// path, so they are removed literally rather than by strip level — a patch
// generated with --no-prefix has no " b/" to split on and falls back to the
// ---/+++ pair, which is authoritative anyway.
func gitHeaderPaths(line string) (oldPath, newPath string, ok bool) {
	rest, found := strings.CutPrefix(line, "diff --git ")
	if !found {
		return "", "", false
	}
	a, b, found := strings.Cut(rest, " b/")
	if !found {
		return "", "", false
	}
	return vcs.UnquoteGitPath(strings.TrimPrefix(strings.TrimSpace(a), "a/")), vcs.UnquoteGitPath(b), true
}

// hunkLines returns everything from a file's first hunk header onward,
// verbatim. Hunk bodies are the parser's business, not ours.
func hunkLines(body []string) []string {
	for i, line := range body {
		if strings.HasPrefix(line, "@@") {
			return body[i:]
		}
	}
	return nil
}

// stripPath cleans one side of a file header: it drops a trailing timestamp,
// unquotes a C-quoted path, and removes stripLevel leading components.
//
// Timestamps are tab-separated in quilt and `diff -u` output ("a/foo.c
// 2026-01-01 12:00:00.000000000 +0000"); diffparser only handles that for
// Mercurial, so it is handled here for everyone.
func stripPath(raw string, stripLevel int) string {
	path, _, _ := strings.Cut(strings.TrimSpace(raw), "\t")
	path = vcs.UnquoteGitPath(path)
	if path == devNull {
		return devNull
	}
	for range stripLevel {
		_, rest, ok := strings.Cut(path, "/")
		if !ok || rest == "" {
			// Stripping would leave nothing; keep what we have. A patch whose
			// paths are shallower than the strip level is likelier to be -p0
			// than to be about the filesystem root.
			return path
		}
		path = rest
	}
	return path
}
