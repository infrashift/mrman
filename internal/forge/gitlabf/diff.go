package gitlabf

import (
	"fmt"
	"strings"

	"github.com/infrashift/mrman/internal/vcs"
)

// defaultFileMode is used when GitLab omits a file mode on a new or
// deleted file entry.
const defaultFileMode = "100644"

// fileDiff is one per-file diff entry as GitLab serves it: metadata plus
// the raw hunk body (usually `@@ …` hunks without git file headers).
type fileDiff struct {
	OldPath string
	NewPath string
	AMode   string
	BMode   string
	Body    string
	New     bool
	Renamed bool
	Deleted bool
	// TooLarge marks an entry GitLab sent without its diff because the file
	// exceeds the instance's diff limits (too_large, or collapsed with no
	// body).
	TooLarge bool
}

// synthesizeUnifiedDiff builds a git-style unified diff from GitLab's
// per-file diff entries. GitLab's `diff` field carries only the `@@` hunks,
// so the `diff --git` line, rename/new/deleted headers, and the
// `---`/`+++` pair are synthesized here — the Go port of tuicr's
// inject_git_diff_headers, adapted from a flat diff stream to per-file
// entries.
func synthesizeUnifiedDiff(files []fileDiff) string {
	var b strings.Builder
	for i := range files {
		appendFileDiff(&b, &files[i])
	}
	return b.String()
}

// appendFileDiff writes one file's git-style diff section.
func appendFileDiff(b *strings.Builder, f *fileDiff) {
	oldPath := f.OldPath
	if oldPath == "" {
		oldPath = f.NewPath
	}
	newPath := f.NewPath
	if newPath == "" {
		newPath = oldPath
	}
	if oldPath == "" {
		return // entry without any path is unusable
	}
	fmt.Fprintf(b, "diff --git a/%s b/%s\n", oldPath, newPath)
	switch {
	case f.New:
		fmt.Fprintf(b, "new file mode %s\n", modeOrDefault(f.BMode))
	case f.Deleted:
		fmt.Fprintf(b, "deleted file mode %s\n", modeOrDefault(f.AMode))
	case f.Renamed:
		fmt.Fprintf(b, "rename from %s\nrename to %s\n", oldPath, newPath)
	}
	body := f.Body
	if body == "" {
		if f.TooLarge {
			b.WriteString(vcs.TooLargeDiffMarker + "\n")
		}
		// Rename-only and binary entries carry no hunks.
		return
	}
	// Defensive: some GitLab versions already include the ---/+++ pair in
	// the diff body; only synthesize it when absent.
	if !strings.HasPrefix(body, "--- ") {
		oldHeader := "a/" + oldPath
		if f.New {
			oldHeader = "/dev/null"
		}
		newHeader := "b/" + newPath
		if f.Deleted {
			newHeader = "/dev/null"
		}
		fmt.Fprintf(b, "--- %s\n+++ %s\n", oldHeader, newHeader)
	}
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteByte('\n')
	}
}

// modeOrDefault falls back to the regular-file mode when GitLab omits one.
func modeOrDefault(mode string) string {
	if mode == "" || mode == "0" {
		return defaultFileMode
	}
	return mode
}
