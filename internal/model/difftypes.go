package model

import (
	"fmt"
	"hash/fnv"

	"github.com/infrashift/mrman/internal/syntax"
)

// FileStatus is the change status of a file in a diff.
type FileStatus string

// File statuses, serialized lowercase as in tuicr sessions.
const (
	StatusAdded    FileStatus = "added"
	StatusModified FileStatus = "modified"
	StatusDeleted  FileStatus = "deleted"
	StatusRenamed  FileStatus = "renamed"
	StatusCopied   FileStatus = "copied"
)

// Char returns the single-letter marker shown in the file list.
func (s FileStatus) Char() byte {
	switch s {
	case StatusAdded:
		return 'A'
	case StatusModified:
		return 'M'
	case StatusDeleted:
		return 'D'
	case StatusRenamed:
		return 'R'
	case StatusCopied:
		return 'C'
	}
	return '?'
}

// LineOrigin classifies a diff line. Not serialized.
type LineOrigin int

// Line origins.
const (
	OriginContext LineOrigin = iota
	OriginAddition
	OriginDeletion
)

// DiffLine is one line of a hunk. HighlightedSpans is nil when the line uses
// default diff coloring.
//
// Content and Raw carry the same text for different purposes, and the split is
// load-bearing:
//
//   - Content is display text. Tabs are expanded to spaces (vcs.Tabify) so
//     highlighted spans line up with what is rendered. It is the only field
//     hashes, anchors, search and syntax spans may read.
//   - Raw is source text: the line exactly as the diff carried it, prefix and
//     tabs included. It exists to be emitted, and nothing else.
//
// Content cannot simply be un-tabified. Comment.LineContext.Content persists a
// snapshot of it into the session file, and anchor validation compares against
// that snapshot — so changing what Content holds would make every comment in
// every existing session report its anchor as gone. Hence a parallel field
// rather than a fix in place. The cost is near zero for tab-free code, because
// Tabify returns its input unchanged when there is no tab and both fields then
// share one string.
type DiffLine struct {
	Origin  LineOrigin
	Content string
	// Raw is the verbatim source line including its +/-/space prefix, empty
	// for lines mrman synthesized rather than read from a diff.
	Raw              string
	OldLineno        *uint32
	NewLineno        *uint32
	HighlightedSpans []syntax.Span
}

// DiffHunk is one @@-delimited hunk.
type DiffHunk struct {
	Header   string
	Lines    []DiffLine
	OldStart uint32
	OldCount uint32
	NewStart uint32
	NewCount uint32
}

func (h *DiffHunk) reviewContentHash() uint64 {
	hasher := fnv.New64a()
	writeHunkContentHash(hasher, h.Lines)
	return hasher.Sum64()
}

// CommentSpan returns the line range a whole-hunk comment anchors to and the
// side it sits on: the new-side span when the hunk carries any context or
// addition lines, otherwise the old-side span of a pure-deletion hunk.
//
// A hunk spans both sides, but a forge range cannot — GitHub, GitLab and
// Azure DevOps each anchor a multi-line comment to one side, and submit
// rejects a range that straddles them. New wins because it is what a
// reviewer is commenting on in everything but a pure deletion.
//
// The span is read off Lines rather than the @@ header's counts so it only
// ever names lines the diff actually carries; submit requires both endpoints
// to be present on the side it anchors to.
func (h *DiffHunk) CommentSpan() (LineRange, LineSide, bool) {
	if span, ok := lineSpan(h.Lines, LineSideNew); ok {
		return span, LineSideNew, true
	}
	if span, ok := lineSpan(h.Lines, LineSideOld); ok {
		return span, LineSideOld, true
	}
	return LineRange{}, LineSideNew, false
}

// lineSpan is the lowest and highest line number the lines carry on side,
// counting only lines that originate there: context and additions on New,
// deletions on Old. This is the same origin filter submit anchors ranges
// with, so a span it returns is always mappable.
func lineSpan(lines []DiffLine, side LineSide) (LineRange, bool) {
	var lo, hi uint32
	found := false
	for i := range lines {
		var lineno *uint32
		switch side {
		case LineSideNew:
			if lines[i].Origin == OriginContext || lines[i].Origin == OriginAddition {
				lineno = lines[i].NewLineno
			}
		case LineSideOld:
			if lines[i].Origin == OriginDeletion {
				lineno = lines[i].OldLineno
			}
		}
		if lineno == nil {
			continue
		}
		if !found || *lineno < lo {
			lo = *lineno
		}
		if !found || *lineno > hi {
			hi = *lineno
		}
		found = true
	}
	if !found {
		return LineRange{}, false
	}
	return NewLineRange(lo, hi), true
}

// DiffFile is one file's parsed diff. In-memory only, never serialized.
type DiffFile struct {
	OldPath         *string
	NewPath         *string
	Status          FileStatus
	Hunks           []DiffHunk
	IsBinary        bool
	IsTooLarge      bool
	IsCommitMessage bool
	ContentHash     uint64
	// RawHeader is the file's verbatim preamble — the "diff --git" line and
	// any index, mode, rename or ---/+++ lines under it. Emitted when quoting
	// a diff back; empty for synthesized files.
	RawHeader []string
	// CommitID names the commit or patch this file's diff belongs to, empty
	// when the diff is not attributable to one — a working tree, or a commit
	// range git already squashed into a single diff.
	//
	// It exists because a patch series is *not* squashed: two patches touching
	// one path produce two entries with the same DisplayPath. Session state is
	// keyed by path, so without this they share one FileReview and a single
	// comment renders under both. This is what tells them apart.
	CommitID string
	// SourceIndex is the file's position in the diff it was read from, or -1
	// when synthesized. It is unique across a whole series, not per patch.
	//
	// It exists because NewApp sorts DiffFiles by directory for display, which
	// destroys the order the patch author chose. A review reply quoted out of
	// that order is hard to follow against the original mail, so the exporter
	// sorts back by this.
	SourceIndex int
}

// CommentBelongsTo reports whether a comment should be shown against this
// diff entry.
//
// It only ever excludes anything when one display path appears more than once,
// which happens for a patch series: two patches touching one file produce two
// entries with the same path, and session state is keyed by path, so both
// would otherwise carry every comment written on either.
//
// An unstamped comment belongs everywhere. That covers ordinary diffs, where
// there is nothing to disambiguate, and comments written before the entry
// carried an id — hiding those would lose them.
func (f *DiffFile) CommentBelongsTo(c *Comment) bool {
	if f == nil || f.CommitID == "" || c == nil || c.CommitID == nil {
		return true
	}
	return *c.CommitID == f.CommitID
}

// DisplayPath returns the new path, falling back to the old path. A DiffFile
// always has at least one; the empty string signals a construction bug.
func (f *DiffFile) DisplayPath() string {
	if f.NewPath != nil {
		return *f.NewPath
	}
	if f.OldPath != nil {
		return *f.OldPath
	}
	return ""
}

// HunkReviewKey returns the stable persisted key identifying hunk i for
// reviewed-state tracking, or false when i is out of range.
//
// The key hashes only hunk content (not header line numbers) so unrelated
// edits above a hunk keep its reviewed state; identical duplicate hunks fall
// back to a line-aware span key so reviewed state cannot migrate between
// duplicates. Format strings are persisted in sessions — never change them.
func (f *DiffFile) HunkReviewKey(i int) (string, bool) {
	if i < 0 || i >= len(f.Hunks) {
		return "", false
	}
	counts := f.hunkContentHashCounts()
	return f.hunkReviewKeyWithCounts(&f.Hunks[i], counts), true
}

// HunkReviewKeys returns the review key of every hunk in order.
func (f *DiffFile) HunkReviewKeys() []string {
	counts := f.hunkContentHashCounts()
	keys := make([]string, len(f.Hunks))
	for i := range f.Hunks {
		keys[i] = f.hunkReviewKeyWithCounts(&f.Hunks[i], counts)
	}
	return keys
}

func (f *DiffFile) hunkContentHashCounts() map[uint64]int {
	counts := make(map[uint64]int, len(f.Hunks))
	for i := range f.Hunks {
		counts[f.Hunks[i].reviewContentHash()]++
	}
	return counts
}

func (f *DiffFile) hunkReviewKeyWithCounts(h *DiffHunk, counts map[uint64]int) string {
	hash := h.reviewContentHash()
	if counts[hash] > 1 {
		return fmt.Sprintf("hunk-span-v1:%016x:%d:%d:%d:%d",
			hash, h.OldStart, h.OldCount, h.NewStart, h.NewCount)
	}
	return fmt.Sprintf("hunk-content-v1:%016x:0", hash)
}

// ComputeContentHash hashes all hunk contents for change detection.
func ComputeContentHash(hunks []DiffHunk) uint64 {
	hasher := fnv.New64a()
	for i := range hunks {
		writeHunkContentHash(hasher, hunks[i].Lines)
	}
	return hasher.Sum64()
}

// MaxLineno returns the highest line number reachable from hunk headers on
// either side; used to size the line-number gutter.
func (f *DiffFile) MaxLineno() uint32 {
	var maxN uint32
	for i := range f.Hunks {
		h := &f.Hunks[i]
		if n := h.OldStart + h.OldCount; n > maxN {
			maxN = n
		}
		if n := h.NewStart + h.NewCount; n > maxN {
			maxN = n
		}
	}
	return maxN
}

// FirstValidLine returns the first line number in display order carrying a
// value on side: the first context/addition new-lineno on New, or the first
// deletion old-lineno on Old. Used to anchor file-level comments on submit.
// Returns false for binary, too-large, or sideless files.
func (f *DiffFile) FirstValidLine(side LineSide) (uint32, bool) {
	if f.IsBinary || f.IsTooLarge {
		return 0, false
	}
	for hi := range f.Hunks {
		for li := range f.Hunks[hi].Lines {
			line := &f.Hunks[hi].Lines[li]
			var candidate *uint32
			switch side {
			case LineSideNew:
				if line.Origin == OriginContext || line.Origin == OriginAddition {
					candidate = line.NewLineno
				}
			case LineSideOld:
				if line.Origin == OriginDeletion {
					candidate = line.OldLineno
				}
			}
			if candidate != nil {
				return *candidate, true
			}
		}
	}
	return 0, false
}

// Stat returns (additions, deletions) for this file.
func (f *DiffFile) Stat() (adds, dels int) {
	for hi := range f.Hunks {
		for li := range f.Hunks[hi].Lines {
			switch f.Hunks[hi].Lines[li].Origin {
			case OriginAddition:
				adds++
			case OriginDeletion:
				dels++
			case OriginContext:
			}
		}
	}
	return adds, dels
}

type fnvWriter interface{ Write(p []byte) (int, error) }

// writeHunkContentHash feeds origin marker + content + newline per line;
// hash.Hash writes never fail, so errors are discarded.
func writeHunkContentHash(hasher fnvWriter, lines []DiffLine) {
	for i := range lines {
		switch lines[i].Origin {
		case OriginAddition:
			_, _ = hasher.Write([]byte("+"))
		case OriginDeletion:
			_, _ = hasher.Write([]byte("-"))
		case OriginContext:
			_, _ = hasher.Write([]byte(" "))
		}
		_, _ = hasher.Write([]byte(lines[i].Content))
		_, _ = hasher.Write([]byte("\n"))
	}
}
