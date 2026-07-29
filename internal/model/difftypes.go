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
type DiffLine struct {
	Origin           LineOrigin
	Content          string
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
