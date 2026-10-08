package vcs

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/infrashift/mrman/internal/textsafe"

	"github.com/infrashift/mrman/internal/model"
)

// BatchBoundary separates files in batched `jj file show` (and future
// `hg cat`) output. The random suffix makes collision with real source
// content effectively impossible. Kept identical to tuicr's marker so
// template snippets port directly.
const BatchBoundary = "@@TUICR_BATCH_BOUNDARY_e97f2d44_8b1a@@"

// Tabify expands tabs to four spaces so highlighted spans line up with the
// rendered text.
func Tabify(s string) string {
	return strings.ReplaceAll(s, "\t", "    ")
}

// SliceContextLines slices [startLine, endLine] (1-indexed, inclusive) of
// content into context DiffLines with both linenos set.
//
// A range past the end of the file stops at its last line, and an empty
// file has no lines (not one empty line).
func SliceContextLines(content string, startLine, endLine uint32) []model.DiffLine {
	if startLine > endLine || startLine == 0 || content == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	var result []model.DiffLine
	for n := startLine; n <= endLine; n++ {
		idx := int(n - 1)
		if idx >= len(lines) {
			break
		}
		lineNo := n
		oldNo, newNo := lineNo, lineNo
		// File contents are untrusted the moment they come from a forge or
		// a patch someone else wrote; scrub them like every other ingress.
		text := textsafe.SanitizeLine(lines[idx])
		result = append(result, model.DiffLine{
			Origin:  model.OriginContext,
			Content: Tabify(text),
			// The source line with a context line's leading space, kept
			// untabified so anything quoting this reads as the file does.
			Raw:       " " + text,
			OldLineno: &oldNo,
			NewLineno: &newNo,
		})
	}
	return result
}

// ReadWorkdirFile reads root/rel, returning ok=false on any error.
func ReadWorkdirFile(root, rel string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// ParseBatchedFiles parses batched file-show output where each file is
// prefixed with "\n{BatchBoundary}\n{path}\n" before its data.
func ParseBatchedFiles(output string) map[string]string {
	sep := "\n" + BatchBoundary + "\n"
	files := make(map[string]string)
	for block := range strings.SplitSeq(output, sep) {
		if block == "" {
			continue
		}
		path, data, found := strings.Cut(block, "\n")
		if !found {
			data = ""
		}
		if path == "" {
			continue
		}
		files[path] = data
	}
	return files
}

// ContainerFilePaths collects the unique paths of files needing full-file
// syntax highlighting on the given side, skipping binary/too-large/empty
// entries. Used by batch-fetching backends.
func ContainerFilePaths(files []model.DiffFile, side model.LineSide, needsFullFile func(string) bool) []string {
	var paths []string
	for i := range files {
		f := &files[i]
		if f.IsBinary || f.IsTooLarge || len(f.Hunks) == 0 {
			continue
		}
		syntaxPath := f.DisplayPath()
		if syntaxPath == "" || !needsFullFile(syntaxPath) {
			continue
		}
		var sidePath *string
		switch side {
		case model.LineSideOld:
			sidePath = f.OldPath
		case model.LineSideNew:
			sidePath = f.NewPath
		}
		if sidePath != nil {
			paths = append(paths, *sidePath)
		}
	}
	return paths
}
