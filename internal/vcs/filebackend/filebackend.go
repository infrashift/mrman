// Package filebackend reviews files outside of a VCS repository (the
// `--file` entry point) and provides the whole-repo pristine review surface
// (`--all-files`). Ported from tuicr's vcs/file.rs and vcs/pristine.rs.
//
// Single and Directory modes render each line with model.OriginAddition and
// a "@@ -0,0 +1,N @@" hunk header (new-file diff shape). Pristine mode
// renders each line with model.OriginContext and a "@@ -1,N +1,N @@" hunk
// header.
//
// Directory mode walks the tree with a minimal .gitignore-aware walker (see
// gitignore.go), skipping hidden entries, symlinks, and binary or unreadable
// files. Pristine mode skips the walker and accepts a pre-enumerated path
// list (typically from CollectTrackedPaths).
package filebackend

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
)

// Mode says how a Backend was constructed. Determines rendering semantics.
type Mode int

// Backend construction modes.
const (
	// Single is a single file passed via `--file <path>`. Renders as a
	// new-file addition diff ("@@ -0,0 +1,N @@", model.OriginAddition).
	Single Mode = iota
	// Directory is a directory walked from `--file <dir>`. Same rendering
	// as Single; only the entry point differs.
	Directory
	// Pristine is the whole-repo annotation surface from `--all-files`.
	// Renders as context ("@@ -1,N +1,N @@", model.OriginContext) so the
	// user reads pristine source instead of a synthetic addition diff.
	Pristine
)

// maxFileBytes caps the size of files rendered in full. Larger files are
// added to the diff list as IsTooLarge so the UI can show a placeholder
// instead of loading them. Matches tuicr's MAX_FILE_BYTES (10 MiB).
const maxFileBytes = 10 * 1024 * 1024

// binarySniffBytes is how many leading bytes are inspected when classifying
// a file as text vs. binary.
const binarySniffBytes = 8192

// fileEntry pairs an absolute path with the file size recorded at discovery
// time so buildDiffFileForPath does not need to re-stat each entry.
type fileEntry struct {
	path string
	size int64
}

// Backend reviews plain files without a VCS.
type Backend struct {
	vcs.UnsupportedBase
	info  vcs.Info
	files []fileEntry
	mode  Mode
}

// Compile-time interface check.
var _ vcs.Backend = (*Backend)(nil)

// New creates a Backend for the given file or directory path (the
// `--file <path>` entry point).
//
// It returns a wrapped I/O error if path cannot be resolved, an
// errs.InvalidInput if it is neither a file nor a directory, and
// errs.ErrNoChanges when a directory walk surfaces zero text files after
// binary filtering.
func New(path string) (*Backend, error) {
	canonical, err := canonicalize(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open %q: %w", path, err)
	}

	st, err := os.Stat(canonical)
	if err != nil {
		return nil, fmt.Errorf("cannot open %q: %w", path, err)
	}

	var (
		rootPath string
		files    []fileEntry
		mode     Mode
	)
	switch {
	case st.Mode().IsRegular():
		rootPath = filepath.Dir(canonical)
		files = []fileEntry{{path: canonical, size: st.Size()}}
		mode = Single
	case st.IsDir():
		rootPath = canonical
		files = collectTextFiles(canonical)
		if len(files) == 0 {
			return nil, errs.ErrNoChanges
		}
		mode = Directory
	default:
		return nil, &errs.InvalidInput{Detail: fmt.Sprintf("%q is not a file or directory", path)}
	}

	return &Backend{
		info:  fileInfo(rootPath),
		files: files,
		mode:  mode,
	}, nil
}

// NewPristine creates a Backend in pristine mode from a pre-enumerated list
// of absolute paths under root (the `--all-files` entry point).
//
// It skips the directory walker entirely: the caller has already decided
// what should be included (typically from CollectTrackedPaths). Each path is
// stat-ed for its size, binary files are dropped via isProbablyBinary, and
// the remaining set is stored sorted. Returns errs.ErrNoChanges when every
// path was filtered out (all binary, all unreadable, or the input list was
// empty).
func NewPristine(paths []string, root string) (*Backend, error) {
	files := make([]fileEntry, 0, len(paths))
	for _, path := range paths {
		if isProbablyBinary(path) {
			continue
		}
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		files = append(files, fileEntry{path: path, size: st.Size()})
	}

	if len(files) == 0 {
		return nil, errs.ErrNoChanges
	}

	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })

	return &Backend{
		info:  fileInfo(root),
		files: files,
		mode:  Pristine,
	}, nil
}

func fileInfo(root string) vcs.Info {
	return vcs.Info{
		RootPath:   root,
		HeadCommit: "file",
		BranchName: nil,
		Type:       vcs.TypeFile,
	}
}

// Mode reports which entry point built this backend.
func (b *Backend) Mode() Mode { return b.mode }

// Info returns the pseudo-repository info (HeadCommit "file", no branch).
func (b *Backend) Info() *vcs.Info { return &b.info }

// WorkingTreeDiff renders every discovered file as one DiffFile. Returns
// errs.ErrNoChanges when all files were filtered (binary or unreadable).
func (b *Backend) WorkingTreeDiff(h *syntax.Highlighter) ([]model.DiffFile, error) {
	var diffFiles []model.DiffFile
	for _, entry := range b.files {
		if f := b.buildDiffFileForPath(h, entry.path, entry.size); f != nil {
			diffFiles = append(diffFiles, *f)
		}
	}
	if len(diffFiles) == 0 {
		return nil, errs.ErrNoChanges
	}
	return diffFiles, nil
}

// FetchContextLines reads [start, end] (1-indexed, inclusive) from the file
// at path relative to the backend root. The resolved path is confined to the
// root: without this guard a malformed session pointing at ../../etc/passwd
// (or a renamed repo where a stored path now escapes the new root) could
// read arbitrary files. Out-of-root, missing, or non-regular paths yield an
// empty result.
func (b *Backend) FetchContextLines(path string, _ model.FileStatus, _ *string, start, end uint32) ([]model.DiffLine, error) {
	if start > end || start == 0 {
		return nil, nil
	}

	joined := filepath.Join(b.info.RootPath, path)
	canonical, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return nil, nil
	}
	rel, err := filepath.Rel(b.info.RootPath, canonical)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, nil
	}
	st, err := os.Stat(canonical)
	if err != nil || !st.Mode().IsRegular() {
		return nil, nil
	}

	data, err := os.ReadFile(canonical)
	if err != nil {
		return nil, err
	}
	return vcs.SliceContextLines(string(data), start, end), nil
}

// ChangeStatus reports no staged or unstaged changes, because a --file or
// pristine review has no index to compare against.
//
// Answering plainly matters: when a backend cannot answer, resolveChangeStatus
// falls back to UnstagedDiff and then to WorkingTreeDiff, which returns this
// backend's own files and so reports them as "unstaged changes". The target
// selector then offers an UNSTAGED row that fails with "unsupported operation"
// the moment it is chosen.
func (b *Backend) ChangeStatus() (vcs.ChangeStatus, error) {
	return vcs.ChangeStatus{}, nil
}

// FileLineCount returns the number of lines in root/path.
func (b *Backend) FileLineCount(path string, _ model.FileStatus, _ *string) (uint32, error) {
	data, err := os.ReadFile(filepath.Join(b.info.RootPath, path))
	if err != nil {
		return 0, err
	}
	return countLines(string(data)), nil
}

// buildDiffFileForPath renders one file, or nil when it must be skipped
// (binary or unreadable or empty).
func (b *Backend) buildDiffFileForPath(h *syntax.Highlighter, absPath string, size int64) *model.DiffFile {
	// Binary check first so a too-large binary is skipped (not surfaced as a
	// misleading IsTooLarge text placeholder), and so single-file mode (which
	// never went through collectTextFiles) is also guarded.
	if isProbablyBinary(absPath) {
		return nil
	}

	relPath := relativeTo(b.info.RootPath, absPath)

	if size > maxFileBytes {
		return &model.DiffFile{
			SourceIndex: -1,
			NewPath:     &relPath,
			Status:      model.StatusAdded,
			IsTooLarge:  true,
			ContentHash: model.ComputeContentHash(nil),
		}
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}
	lines := splitLines(string(data))
	if len(lines) == 0 {
		return nil
	}

	renderOrigin := model.OriginAddition
	if b.mode == Pristine {
		renderOrigin = model.OriginContext
	}

	lineContents := make([]string, len(lines))
	for i, l := range lines {
		lineContents[i] = vcs.Tabify(l)
	}

	// The origin marker a diff would carry, so Raw reads as a diff line.
	rawPrefix := "+"
	if b.mode == Pristine {
		rawPrefix = " "
	}

	var highlighted [][]syntax.Span
	highlightOK := false
	if h != nil {
		highlighted, highlightOK = h.HighlightFileLines(absPath, lineContents)
	}

	diffLines := make([]model.DiffLine, 0, len(lineContents))
	for i, content := range lineContents {
		lineNum := uint32(i + 1)
		raw := rawPrefix + lines[i]

		var spans []syntax.Span
		if highlightOK && i < len(highlighted) && len(highlighted[i]) > 0 {
			spans = h.ApplyDiffBackground(highlighted[i], renderDiffOrigin(renderOrigin))
		}

		// Pristine context lines need both OldLineno and NewLineno populated
		// so the side-by-side and unified renderers walk the gutter math
		// correctly.
		var oldLineno *uint32
		if b.mode == Pristine {
			n := lineNum
			oldLineno = &n
		}
		newLineno := lineNum

		diffLines = append(diffLines, model.DiffLine{
			Origin:           renderOrigin,
			Content:          content,
			Raw:              raw,
			OldLineno:        oldLineno,
			NewLineno:        &newLineno,
			HighlightedSpans: spans,
		})
	}

	totalLines := uint32(len(lines))
	var (
		hunkHeader string
		oldStart   uint32
		oldCount   uint32
		fileStatus model.FileStatus
	)
	if b.mode == Pristine {
		hunkHeader = fmt.Sprintf("@@ -1,%d +1,%d @@", totalLines, totalLines)
		oldStart, oldCount = 1, totalLines
		fileStatus = model.StatusModified
	} else {
		hunkHeader = fmt.Sprintf("@@ -0,0 +1,%d @@", totalLines)
		oldStart, oldCount = 0, 0
		fileStatus = model.StatusAdded
	}

	hunks := []model.DiffHunk{{
		Header:   hunkHeader,
		Lines:    diffLines,
		OldStart: oldStart,
		OldCount: oldCount,
		NewStart: 1,
		NewCount: totalLines,
	}}

	return &model.DiffFile{
		SourceIndex: -1,
		NewPath:     &relPath,
		Status:      fileStatus,
		Hunks:       hunks,
		ContentHash: model.ComputeContentHash(hunks),
	}
}

// renderDiffOrigin maps a rendering origin to the syntax package's mirror.
func renderDiffOrigin(origin model.LineOrigin) syntax.DiffOrigin {
	if origin == model.OriginAddition {
		return syntax.DiffOriginAddition
	}
	return syntax.DiffOriginContext
}

// canonicalize resolves path to an absolute, symlink-free form.
func canonicalize(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// relativeTo strips root from abs (mirrors Rust strip_prefix with the
// absolute path itself as fallback). In single-file mode this is just the
// filename.
func relativeTo(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return abs
	}
	return rel
}

// splitLines splits content like Rust's str::lines: the trailing newline
// does not produce a final empty line, and "" yields no lines.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// countLines counts lines with str::lines semantics.
func countLines(content string) uint32 {
	return uint32(len(splitLines(content)))
}

// isProbablyBinary reports whether the first binarySniffBytes bytes of the
// file contain a NUL byte. Unreadable files classify as binary so they are
// skipped silently.
func isProbablyBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, binarySniffBytes)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return true
	}
	for _, b := range buf[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}
