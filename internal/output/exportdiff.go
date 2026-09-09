package output

import (
	"strings"

	"github.com/infrashift/mrman/internal/model"
)

// hunkQuoter resolves the diff hunk a line comment sits in and renders it as
// unified-diff text for the notes export.
//
// The notes export is organised by path and line, not by diff order, so unlike
// the patch reply it cannot walk the diff and hang comments off it. It has to
// go the other way: from a comment back to the hunk that contains its anchor.
// That is all this type does; the placement rules themselves are shared with
// the reply through lineLocation, rawLine and skipReason.
//
// The zero value quotes nothing, which is what an export with IncludeDiff off
// wants — no lookup, no allocation, every Diff field left empty.
type hunkQuoter struct {
	// byPath groups the diff by display path. It is a slice per path because
	// a patch series can touch one file in several patches, and each of those
	// is a separate entry; CommentBelongsTo picks the right one.
	byPath map[string][]*model.DiffFile
	// last is the hunk the previous comment quoted, so a run of comments
	// inside one hunk does not repeat it.
	last hunkRef
}

// hunkRef identifies a hunk well enough to spot a repeat: the path plus the
// index of the hunk within that path's diff. found distinguishes "hunk 0 of
// foo.go" from "nothing quoted yet".
type hunkRef struct {
	path  string
	index int
	found bool
}

// newHunkQuoter indexes the diff by display path. It returns the zero quoter
// when the caller did not ask for diffs, so the index is only built when
// something will read it.
func newHunkQuoter(files []model.DiffFile, include bool) *hunkQuoter {
	q := &hunkQuoter{}
	if !include {
		return q
	}
	q.byPath = make(map[string][]*model.DiffFile, len(files))
	for i := range files {
		path := files[i].DisplayPath()
		q.byPath[path] = append(q.byPath[path], &files[i])
	}
	return q
}

// reset forgets the last quoted hunk. Callers use it between files so the
// repeat suppression never reaches across a file boundary.
func (q *hunkQuoter) reset() {
	q.last = hunkRef{}
}

// quote returns the unified-diff text of the hunk containing the comment's
// anchor, or "" when there is nothing to quote: diffs were not requested, the
// file is absent, binary or too large, the anchored line is no longer in the
// diff, or the immediately preceding comment already quoted this same hunk.
func (q *hunkQuoter) quote(path string, line uint32, c *model.Comment) string {
	if q.byPath == nil {
		return ""
	}
	diff := q.fileFor(path, c)
	if skipReason(diff) != "" {
		return ""
	}
	index, ok := hunkIndexFor(diff, model.SideOf(c), line)
	if !ok {
		return ""
	}
	ref := hunkRef{path: path, index: index, found: true}
	if ref == q.last {
		return ""
	}
	q.last = ref
	return renderHunk(&diff.Hunks[index])
}

// fileFor picks the diff entry a comment belongs to, which only matters when
// one path appears more than once — a patch series. An unstamped comment
// belongs to the first entry, matching CommentBelongsTo's "belongs everywhere"
// rule for comments written before entries carried an id.
func (q *hunkQuoter) fileFor(path string, c *model.Comment) *model.DiffFile {
	for _, diff := range q.byPath[path] {
		if diff.CommentBelongsTo(c) {
			return diff
		}
	}
	return nil
}

// hunkIndexFor finds the hunk carrying the given line on the given side.
//
// It looks the line up on the side the comment was written on rather than
// trusting the "@@" header's counts, so a comment only ever resolves to a hunk
// that really contains its anchor. This is the same (side, line) identity
// placeComments anchors the patch reply with, so both exports agree on which
// line a comment sits on — including a range comment, which is keyed by the
// end of its range.
func hunkIndexFor(diff *model.DiffFile, side model.LineSide, line uint32) (int, bool) {
	for hi := range diff.Hunks {
		for _, l := range diff.Hunks[hi].Lines {
			lineno := l.NewLineno
			if side == model.LineSideOld {
				lineno = l.OldLineno
			}
			if lineno != nil && *lineno == line {
				return hi, true
			}
		}
	}
	return 0, false
}

// renderHunk writes the hunk back out as unified diff: the header it was
// parsed from, then every line verbatim through rawLine, which keeps the
// +/-/space prefixes and the source's own tabs.
func renderHunk(h *model.DiffHunk) string {
	var sb strings.Builder
	sb.WriteString(h.Header)
	for _, l := range h.Lines {
		sb.WriteString("\n")
		sb.WriteString(rawLine(l))
	}
	return sb.String()
}
