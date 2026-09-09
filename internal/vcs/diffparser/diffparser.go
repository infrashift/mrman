// Package diffparser parses unified diff output from CLI tools into
// model.DiffFile structures. It is a port of tuicr's diff_parser.rs and is
// used by text-based VCS backends (hg, jj, sparse Git) where a native diff
// library is unavailable or undesirable.
package diffparser

import (
	"strconv"
	"strings"

	"github.com/infrashift/mrman/internal/textsafe"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
)

// Format selects the diff header dialect to expect.
type Format int

// Diff format variants for different VCS tools.
const (
	// GitStyle expects "diff --git " file headers (used by jj, git patches).
	GitStyle Format = iota
	// Hg expects Mercurial's "diff -r "/"diff " headers; ---/+++ lines may
	// carry tab-separated timestamps.
	Hg
)

// Parse reads a complete unified diff. Highlighter h may be nil (no spans).
// It returns errs.ErrNoChanges when the text contains no file diffs.
func Parse(text string, format Format, h *syntax.Highlighter) ([]model.DiffFile, error) {
	lines := strings.Split(text, "\n")
	// strings.Split leaves a trailing empty element for newline-terminated
	// input; drop it to mirror Rust's str::lines.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	i := 0
	return ParseLines(func() (string, bool, error) {
		if i >= len(lines) {
			return "", false, nil
		}
		line := strings.TrimSuffix(lines[i], "\r")
		i++
		return line, true, nil
	}, format, h)
}

// ParseLines consumes a streaming line iterator (next returns line, ok; a
// returned error aborts the parse). Used by the git CLI backend to stream
// huge diffs without buffering. Highlighter h may be nil (no spans).
// It returns errs.ErrNoChanges when the stream contains no file diffs.
func ParseLines(next func() (string, bool, error), format Format, h *syntax.Highlighter) ([]model.DiffFile, error) {
	// Every diff mrman shows — from a forge, a patch file, git or jj —
	// enters here, so this is where terminal control sequences leave it.
	src := &lineSource{next: func() (string, bool, error) {
		line, ok, err := next()
		return textsafe.SanitizeLine(line), ok, err
	}}
	var files []model.DiffFile

	headerPrefix := "diff --git "
	if format == Hg {
		headerPrefix = "diff "
	}

	for {
		line, ok, err := src.advance()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		if !strings.HasPrefix(line, headerPrefix) {
			continue
		}
		file, err := parseFile(src, line, format, h)
		if err != nil {
			return nil, err
		}
		file.SourceIndex = len(files)
		files = append(files, file)
	}

	if len(files) == 0 {
		return nil, errs.ErrNoChanges
	}
	return files, nil
}

// parseFile parses one file, starting from its already-consumed "diff"
// header line: the metadata header, then either a binary marker or the
// hunks up to the next file.
func parseFile(src *lineSource, header string, format Format, h *syntax.Highlighter) (model.DiffFile, error) {
	// Keep the file's preamble verbatim — the header line just consumed
	// plus whatever parseFileHeader consumes below — so a review reply can
	// quote it back as the author wrote it.
	rawHeader := []string{header}
	src.record = &rawHeader
	oldPath, newPath, status, err := parseFileHeader(src, format)
	src.record = nil
	if err != nil {
		return model.DiffFile{}, err
	}

	// For git-style diffs (jj, git patches), if parseFileHeader didn't find
	// ---/+++ or rename/copy lines (e.g. empty new files, mode-only
	// changes), fall back to parsing paths from the "diff --git a/X b/X"
	// header.
	if oldPath == nil && newPath == nil {
		if a, b, ok := parseDiffGitHeader(header); ok {
			switch status {
			case model.StatusDeleted:
				oldPath = &a
			case model.StatusAdded:
				newPath = &b
			default:
				oldPath = &a
				newPath = &b
			}
		}
	}
	file := model.DiffFile{OldPath: oldPath, NewPath: newPath, Status: status, RawHeader: rawHeader}

	// Check if binary. `git diff --binary` can emit lowercase "GIT binary
	// patch", so keep this check explicit instead of a case-sensitive
	// substring search.
	peeked, peekOK, err := src.peek()
	if err != nil {
		return model.DiffFile{}, err
	}
	if peekOK && isBinaryPatchLine(peeked) {
		if _, _, err := src.advance(); err != nil { // consume binary message
			return model.DiffFile{}, err
		}
		file.IsBinary = true
		return file, nil
	}

	filePath := ""
	switch {
	case newPath != nil:
		filePath = *newPath
	case oldPath != nil:
		filePath = *oldPath
	}
	hunks, err := parseHunks(src, filePath, h)
	if err != nil {
		return model.DiffFile{}, err
	}
	file.Hunks = hunks
	file.ContentHash = model.ComputeContentHash(hunks)
	return file, nil
}

// parseHunks reads hunks until the next file header or the end of the
// stream, skipping lines that are neither.
func parseHunks(src *lineSource, filePath string, h *syntax.Highlighter) ([]model.DiffHunk, error) {
	var hunks []model.DiffHunk
	for {
		line, ok, err := src.peek()
		if err != nil {
			return nil, err
		}
		if !ok || strings.HasPrefix(line, "diff ") {
			return hunks, nil
		}
		if !strings.HasPrefix(line, "@@") {
			if _, _, err := src.advance(); err != nil { // skip non-hunk, non-diff lines
				return nil, err
			}
			continue
		}
		hunk, ok, err := parseHunk(src, filePath, h)
		if err != nil {
			return nil, err
		}
		if ok {
			hunks = append(hunks, hunk)
		}
	}
}

// lineSource wraps the streaming iterator with single-line lookahead so the
// parser can peek at file/hunk boundaries without consuming them.
type lineSource struct {
	next   func() (string, bool, error)
	peeked string
	hasPk  bool
	done   bool
	// record accumulates every line advance() hands out while it is non-nil.
	// It exists so a file's verbatim header can be kept without threading a
	// return value through each of parseFileHeader's ten branches.
	record *[]string
}

func (s *lineSource) peek() (string, bool, error) {
	if s.hasPk {
		return s.peeked, true, nil
	}
	if s.done {
		return "", false, nil
	}
	line, ok, err := s.next()
	if err != nil {
		return "", false, err
	}
	if !ok {
		s.done = true
		return "", false, nil
	}
	s.peeked = line
	s.hasPk = true
	return line, true, nil
}

func (s *lineSource) advance() (string, bool, error) {
	line, ok, err := s.peek()
	s.hasPk = false
	if ok && s.record != nil {
		*s.record = append(*s.record, line)
	}
	return line, ok, err
}

// isBinaryPatchLine reports whether line is a binary-diff marker. A loose
// substring match on "Binary" also matches an ordinary `@@ ... @@` hunk
// header whose context text contains "Binary", so require the real marker
// shape instead: markers always start the line (git's "Binary files ...
// differ", hg's "Binary file ... has changed", or "GIT binary patch").
func isBinaryPatchLine(line string) bool {
	return strings.HasPrefix(line, "Binary file") || strings.HasPrefix(line, "GIT binary patch")
}

// fileHeader accumulates what the metadata lines between "diff --git" and
// the first hunk say about a file.
type fileHeader struct {
	oldPath, newPath *string
	status           model.FileStatus
}

// headerRule handles one kind of metadata line by prefix. done marks the
// line that ends the header (+++); the others keep reading.
type headerRule struct {
	prefix string
	apply  func(h *fileHeader, line string, format Format)
	done   bool
}

// headerPath strips the diff prefix ("--- a/", "+++ b/") and, for Hg, the
// timestamp after the tab. nil for /dev/null.
func headerPath(line, marker, treePrefix string, format Format) *string {
	pathStr := strings.TrimPrefix(strings.TrimPrefix(line, marker), treePrefix)
	if pathStr == "/dev/null" {
		return nil
	}
	if format == Hg {
		pathStr, _, _ = strings.Cut(pathStr, "\t")
	}
	return &pathStr
}

var headerRules = []headerRule{
	{prefix: "---", apply: func(h *fileHeader, line string, f Format) {
		if p := headerPath(line, "--- ", "a/", f); p != nil {
			h.oldPath = p
		}
	}},
	{prefix: "+++", done: true, apply: func(h *fileHeader, line string, f Format) {
		if p := headerPath(line, "+++ ", "b/", f); p != nil {
			h.newPath = p
		}
	}},
	{prefix: "new file", apply: func(h *fileHeader, _ string, _ Format) { h.status = model.StatusAdded }},
	{prefix: "deleted file", apply: func(h *fileHeader, _ string, _ Format) { h.status = model.StatusDeleted }},
	{prefix: "rename from ", apply: func(h *fileHeader, line string, _ Format) {
		h.status = model.StatusRenamed
		p := strings.TrimPrefix(line, "rename from ")
		h.oldPath = &p
	}},
	{prefix: "rename to ", apply: func(h *fileHeader, line string, _ Format) {
		p := strings.TrimPrefix(line, "rename to ")
		h.newPath = &p
	}},
	{prefix: "copy from ", apply: func(h *fileHeader, line string, _ Format) {
		h.status = model.StatusCopied
		p := strings.TrimPrefix(line, "copy from ")
		h.oldPath = &p
	}},
	{prefix: "copy to ", apply: func(h *fileHeader, line string, _ Format) {
		p := strings.TrimPrefix(line, "copy to ")
		h.newPath = &p
	}},
}

// applyLine folds one metadata line into the header. Lines no rule claims
// (index, similarity, modes) are skipped. done reports the +++ line.
func (h *fileHeader) applyLine(line string, format Format) (done bool) {
	for _, rule := range headerRules {
		if strings.HasPrefix(line, rule.prefix) {
			rule.apply(h, line, format)
			return rule.done
		}
	}
	return false
}

// applyBinary takes the paths from a binary marker when nothing earlier in
// the header named them:
// Hg: "Binary file <path> has changed"; git: "Binary files a/<old> and b/<new> differ".
func (h *fileHeader) applyBinary(line string) {
	old, newer, ok := parseBinaryFileLine(line)
	if !ok {
		return
	}
	if h.oldPath == nil {
		h.oldPath = old
	}
	if h.newPath == nil {
		h.newPath = newer
	}
}

// parseFileHeader consumes ---/+++ and metadata lines (rename/copy, new
// file/deleted file, index, similarity, modes), returning paths and status.
// It stops after the +++ line, or before an @@ hunk header, "diff " header,
// binary marker, or end of stream.
func parseFileHeader(src *lineSource, format Format) (oldPath, newPath *string, status model.FileStatus, err error) {
	h := fileHeader{status: model.StatusModified}
	for {
		line, ok, err := src.peek()
		if err != nil {
			return nil, nil, h.status, err
		}
		if !ok || strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "diff ") {
			break
		}
		if isBinaryPatchLine(line) {
			h.applyBinary(line)
			break
		}
		done := h.applyLine(line, format)
		if _, _, err := src.advance(); err != nil {
			return nil, nil, h.status, err
		}
		if done {
			break
		}
	}
	return h.oldPath, h.newPath, deriveStatus(h.oldPath, h.newPath, h.status), nil
}

// deriveStatus infers Added/Deleted from missing paths when no metadata line
// already set an explicit status.
func deriveStatus(oldPath, newPath *string, status model.FileStatus) model.FileStatus {
	if status != model.StatusModified {
		return status
	}
	if oldPath == nil && newPath != nil {
		return model.StatusAdded
	}
	if oldPath != nil && newPath == nil {
		return model.StatusDeleted
	}
	return status
}

// parseHunk consumes one @@-delimited hunk. ok is false when the consumed
// header line is not a valid hunk header.
func parseHunk(src *lineSource, filePath string, h *syntax.Highlighter) (model.DiffHunk, bool, error) {
	headerLine, hasHeader, err := src.advance()
	if err != nil || !hasHeader {
		return model.DiffHunk{}, false, err
	}

	// Parse @@ -oldStart,oldCount +newStart,newCount @@
	bounds, ok := parseHunkHeader(headerLine)
	if !ok {
		return model.DiffHunk{}, false, nil
	}

	var lineContents []string
	// lineRaws keeps each body line exactly as it arrived, prefix and tabs
	// included, so a review reply can quote the diff back verbatim.
	var lineRaws []string
	var lineOrigins []model.LineOrigin
	var oldLinenos, newLinenos []*uint32

	oldLineno := bounds.OldStart
	newLineno := bounds.NewStart

	// oldSeen/newSeen count the body lines consumed on each side, so the hunk
	// ends where its header said it would.
	var oldSeen, newSeen uint32

	// Collect lines until the hunk's declared length is spent, or the next
	// hunk or file begins.
	//
	// The length budget is what keeps everything *after* a hunk out of it.
	// A `git format-patch` signature ("-- \n2.43.0"), an mbox's next-message
	// preamble, a diffstat line, a changelog bullet — all of them start with
	// "-", "+" or " " and would otherwise be read as diff rows: the "---"
	// guard below does not catch "-- ", and a leading space is a context
	// line. That corrupts the hunk's line numbering and, through
	// ComputeContentHash and HunkReviewKey, the reviewed state keyed off it.
	for !bounds.spent(oldSeen, newSeen) {
		line, hasLine, err := src.peek()
		if err != nil {
			return model.DiffHunk{}, false, err
		}
		if !hasLine || strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "diff ") {
			break
		}
		if _, _, err := src.advance(); err != nil {
			return model.DiffHunk{}, false, err
		}

		if strings.HasPrefix(line, `\`) {
			// "\ No newline at end of file" - skip.
			continue
		}

		var origin model.LineOrigin
		var content string
		var oldLn, newLn *uint32

		switch {
		case strings.HasPrefix(line, "+"):
			if strings.HasPrefix(line, "+++") {
				continue // skip +++ header lines
			}
			ln := newLineno
			newLineno++
			newSeen++
			origin, content, newLn = model.OriginAddition, line[1:], &ln
		case strings.HasPrefix(line, "-"):
			if strings.HasPrefix(line, "---") {
				continue // skip --- header lines
			}
			ln := oldLineno
			oldLineno++
			oldSeen++
			origin, content, oldLn = model.OriginDeletion, line[1:], &ln
		case strings.HasPrefix(line, " "), line == "":
			// Empty lines are context lines whose trailing space was trimmed.
			oldLn2, newLn2 := oldLineno, newLineno
			oldLineno++
			newLineno++
			oldSeen++
			newSeen++
			origin, content, oldLn, newLn = model.OriginContext, strings.TrimPrefix(line, " "), &oldLn2, &newLn2
		default:
			continue // unknown format, skip
		}

		lineContents = append(lineContents, vcs.Tabify(content))
		lineRaws = append(lineRaws, line)
		lineOrigins = append(lineOrigins, origin)
		oldLinenos = append(oldLinenos, oldLn)
		newLinenos = append(newLinenos, newLn)
	}

	// Apply syntax highlighting by side-specific sequence to keep lexer state
	// valid across interleaved add/del runs. Container grammars skip per-hunk
	// highlighting (they need full-file context; a post-pass owns those).
	seq := splitDiffLinesForHighlighting(lineContents, lineOrigins)
	var oldHL, newHL [][]syntax.Span
	var oldOK, newOK bool
	if h != nil && filePath != "" && !syntax.NeedsFullFileHighlight(filePath) {
		oldHL, oldOK = h.HighlightFileLines(filePath, seq.oldLines)
		newHL, newOK = h.HighlightFileLines(filePath, seq.newLines)
	}

	diffLines := make([]model.DiffLine, 0, len(lineContents))
	for idx, content := range lineContents {
		origin := lineOrigins[idx]
		diffLines = append(diffLines, model.DiffLine{
			Origin:    origin,
			Content:   content,
			Raw:       lineRaws[idx],
			OldLineno: oldLinenos[idx],
			NewLineno: newLinenos[idx],
			HighlightedSpans: highlightedLineForDiff(
				h, oldHL, oldOK, newHL, newOK,
				seq.oldLineIndices[idx], seq.newLineIndices[idx], origin,
			),
		})
	}

	return model.DiffHunk{
		Header:   headerLine,
		Lines:    diffLines,
		OldStart: bounds.OldStart,
		OldCount: bounds.OldCount,
		NewStart: bounds.NewStart,
		NewCount: bounds.NewCount,
	}, true, nil
}

// diffHighlightSequences carries the reconstructed old-side and new-side line
// sequences of a hunk plus, per display line, its index into each sequence
// (-1 when the line does not exist on that side).
type diffHighlightSequences struct {
	oldLines       []string
	newLines       []string
	oldLineIndices []int
	newLineIndices []int
}

// splitDiffLinesForHighlighting rebuilds the old and new file line sequences
// from a hunk's display lines so each side can be highlighted as contiguous
// source (port of tuicr's split_diff_lines_for_highlighting).
func splitDiffLinesForHighlighting(lineContents []string, lineOrigins []model.LineOrigin) diffHighlightSequences {
	seq := diffHighlightSequences{
		oldLineIndices: make([]int, 0, len(lineOrigins)),
		newLineIndices: make([]int, 0, len(lineOrigins)),
	}
	for i, content := range lineContents {
		switch lineOrigins[i] {
		case model.OriginContext:
			seq.oldLineIndices = append(seq.oldLineIndices, len(seq.oldLines))
			seq.oldLines = append(seq.oldLines, content)
			seq.newLineIndices = append(seq.newLineIndices, len(seq.newLines))
			seq.newLines = append(seq.newLines, content)
		case model.OriginAddition:
			seq.oldLineIndices = append(seq.oldLineIndices, -1)
			seq.newLineIndices = append(seq.newLineIndices, len(seq.newLines))
			seq.newLines = append(seq.newLines, content)
		case model.OriginDeletion:
			seq.oldLineIndices = append(seq.oldLineIndices, len(seq.oldLines))
			seq.oldLines = append(seq.oldLines, content)
			seq.newLineIndices = append(seq.newLineIndices, -1)
		}
	}
	return seq
}

// highlightedLineForDiff picks the highlighted spans for one display line
// (new side for additions and context, old side for deletions) and stamps
// the diff row background for add/del rows. Returns nil when highlighting is
// unavailable for the line's side.
func highlightedLineForDiff(
	h *syntax.Highlighter,
	oldHL [][]syntax.Span, oldOK bool,
	newHL [][]syntax.Span, newOK bool,
	oldIdx, newIdx int,
	origin model.LineOrigin,
) []syntax.Span {
	var spans []syntax.Span
	switch origin {
	case model.OriginDeletion:
		if !oldOK || oldIdx < 0 || oldIdx >= len(oldHL) {
			return nil
		}
		spans = oldHL[oldIdx]
	case model.OriginAddition, model.OriginContext:
		if !newOK || newIdx < 0 || newIdx >= len(newHL) {
			return nil
		}
		spans = newHL[newIdx]
	default:
		return nil
	}
	return h.ApplyDiffBackground(spans, diffOrigin(origin))
}

// diffOrigin maps model.LineOrigin to syntax.DiffOrigin.
func diffOrigin(origin model.LineOrigin) syntax.DiffOrigin {
	switch origin {
	case model.OriginAddition:
		return syntax.DiffOriginAddition
	case model.OriginDeletion:
		return syntax.DiffOriginDeletion
	default:
		return syntax.DiffOriginContext
	}
}

// hunkBounds is a parsed "@@ -oldStart,oldCount +newStart,newCount @@" header.
//
// Counted reports whether both ranges parsed cleanly. It gates using the
// counts as a body-length budget: parseU32Or1 falls back to 1 for anything
// unparsable, and budgeting a malformed header to one line would truncate a
// hunk that today parses in full. A header we did not really understand is
// better read to the next structural marker than trusted.
type hunkBounds struct {
	OldStart, OldCount uint32
	NewStart, NewCount uint32
	Counted            bool
}

// spent reports whether a body of oldSeen/newSeen lines has consumed
// everything the header declared. Always false for an uncounted header, so a
// hunk we could not measure is read to the next structural marker instead.
func (b hunkBounds) spent(oldSeen, newSeen uint32) bool {
	return b.Counted && oldSeen >= b.OldCount && newSeen >= b.NewCount
}

// parseHunkHeader parses "@@ -oldStart,oldCount +newStart,newCount @@ ctx"
// (counts default to 1 when omitted). ok is false for malformed headers.
func parseHunkHeader(line string) (hunkBounds, bool) {
	parts := strings.Fields(line)
	if len(parts) < 3 || parts[0] != "@@" {
		return hunkBounds{}, false
	}
	oldStart, oldCount, oldOK := parseRange(strings.TrimPrefix(parts[1], "-"))
	newStart, newCount, newOK := parseRange(strings.TrimPrefix(parts[2], "+"))
	return hunkBounds{
		OldStart: oldStart, OldCount: oldCount,
		NewStart: newStart, NewCount: newCount,
		Counted: oldOK && newOK,
	}, true
}

// parseRange parses "start,count" or "start" (count defaults to 1); invalid
// numbers fall back to 1. ok reports whether every number present parsed.
func parseRange(s string) (start, count uint32, ok bool) {
	startStr, countStr, hasComma := strings.Cut(s, ",")
	start, ok = parseU32Or1(startStr)
	count = 1
	if hasComma {
		var countOK bool
		count, countOK = parseU32Or1(countStr)
		ok = ok && countOK
	}
	return start, count, ok
}

func parseU32Or1(s string) (uint32, bool) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 1, false
	}
	return uint32(n), true
}

// parseDiffGitHeader parses paths from a "diff --git a/X b/X" header line,
// returning the paths with their a/ and b/ prefixes stripped. Paths may
// contain spaces, so the " b/" separator anchors the split.
func parseDiffGitHeader(line string) (oldPath, newPath string, ok bool) {
	rest, found := strings.CutPrefix(line, "diff --git ")
	if !found {
		return "", "", false
	}
	pos := strings.Index(rest, " b/")
	if pos < 0 {
		return "", "", false
	}
	oldPart := rest[:pos]
	newPart := rest[pos+1:]
	return strings.TrimPrefix(oldPart, "a/"), strings.TrimPrefix(newPart, "b/"), true
}

// parseBinaryFileLine parses paths from a binary file marker.
// Git format: "Binary files a/<old> and b/<new> differ" (either side may be
// /dev/null, yielding a nil path). Hg format: "Binary file <path> has
// changed" (same path on both sides).
func parseBinaryFileLine(line string) (oldPath, newPath *string, ok bool) {
	if content, found := strings.CutPrefix(line, "Binary files "); found {
		content, found = strings.CutSuffix(content, " differ")
		if !found {
			return nil, nil, false
		}
		oldPart, newPart, found := strings.Cut(content, " and ")
		if !found {
			return nil, nil, false
		}
		if oldPart != "/dev/null" {
			p := strings.TrimPrefix(oldPart, "a/")
			oldPath = &p
		}
		if newPart != "/dev/null" {
			p := strings.TrimPrefix(newPart, "b/")
			newPath = &p
		}
		return oldPath, newPath, true
	}

	if content, found := strings.CutPrefix(line, "Binary file "); found {
		path, found := strings.CutSuffix(content, " has changed")
		if !found {
			return nil, nil, false
		}
		oldP, newP := path, path
		return &oldP, &newP, true
	}

	return nil, nil, false
}
