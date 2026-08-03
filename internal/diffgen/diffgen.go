// Package diffgen synthesizes git-style unified diff text from a pair of
// blobs. The line diff is a compact O(ND) greedy Myers algorithm (Myers
// 1986, "An O(ND) Difference Algorithm and Its Variations") over lines that
// keep their trailing newline, so a missing final newline diffs naturally
// and emits git's "\ No newline at end of file" marker.
//
// It is a standalone package rather than a detail of one caller because two
// unrelated parts of mrman need to diff content that git will not diff for
// them: the Azure DevOps driver, whose API exposes no text-diff endpoint at
// all, and the two-path review backend, which compares files that need not
// be in any repository. Neither has a layer in common with the other.
//
// The output is text, never model.DiffFile. Callers hand it to
// internal/vcs/diffparser like any other diff, so this package stays free
// of mrman's diff model and every consumer shares one parser.
package diffgen

import (
	"bytes"
	"fmt"
	"strings"
)

// diffContextLines is the number of unchanged lines kept around each hunk,
// matching git's default.
const diffContextLines = 3

// maxEditDistance bounds the Myers search depth. The trace kept for
// backtracking grows quadratically with the edit distance, so a
// pathological pair (two large, entirely different files) degrades to a
// whole-file delete+insert instead of exhausting memory. 2048 line edits
// per file is far beyond anything a human reviews line by line.
const maxEditDistance = 2048

// binarySniffLen is how many leading bytes are searched for a NUL byte to
// classify a blob as binary, mirroring git's heuristic.
const binarySniffLen = 8000

// Options tunes UnifiedFileDiff. The zero value reproduces the behaviour
// this code had while it lived inside the Azure DevOps driver, so a caller
// that wants git's defaults passes Options{} and gets identical bytes.
type Options struct {
	// Context is how many unchanged lines to keep around each hunk. Zero
	// means diffContextLines (3), git's default.
	Context int

	// MaxEditDistance bounds the Myers search depth; zero means
	// maxEditDistance.
	//
	// Raising it is not free. The backtracking trace holds d+1 ints for
	// every sweep d, so its memory is O(bound^2/2) — roughly 17 MB at the
	// default 2048 and 268 MB at 8192. A materially larger bound wants the
	// linear-space Myers refinement, not a larger constant here.
	MaxEditDistance int

	// LineKey maps a line to the value equality is decided on, leaving the
	// text that gets emitted untouched. nil compares lines verbatim.
	//
	// It is how a whitespace-insensitive diff is produced without a second
	// algorithm: key on the line with its whitespace stripped, and the
	// reviewer still reads the original.
	LineKey func(string) string

	// NoRenameHeaders suppresses the "rename from"/"rename to" block that
	// two differing non-empty paths would otherwise imply.
	//
	// It exists for comparisons where the two paths are simply the two
	// things being compared rather than one file's history. Calling
	// `old.txt` vs `new.txt` a rename makes diffparser report StatusRenamed,
	// which paints an R badge that means nothing to the reviewer.
	NoRenameHeaders bool
}

// context returns the effective context-line count.
func (o Options) context() int {
	if o.Context <= 0 {
		return diffContextLines
	}
	return o.Context
}

// editBound returns the effective Myers search bound.
func (o Options) editBound() int {
	if o.MaxEditDistance <= 0 {
		return maxEditDistance
	}
	return o.MaxEditDistance
}

// keys projects lines through LineKey, or returns lines unchanged when no
// key is set. The result is index-parallel to lines: the algorithm compares
// keys and emits the text at the same index.
func (o Options) keys(lines []string) []string {
	if o.LineKey == nil {
		return lines
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = o.LineKey(line)
	}
	return out
}

// editKind classifies one line of an edit script.
type editKind int8

// Edit kinds.
const (
	editEqual editKind = iota
	editDelete
	editInsert
)

// edit is one line of an edit script. text retains its trailing newline
// except possibly on the last line of a file.
type edit struct {
	kind editKind
	text string
}

// isBinaryContent reports whether content looks binary: a NUL byte within
// the first binarySniffLen bytes.
func isBinaryContent(content string) bool {
	sniff := content
	if len(sniff) > binarySniffLen {
		sniff = sniff[:binarySniffLen]
	}
	return bytes.IndexByte([]byte(sniff), 0) >= 0
}

// splitLinesKeepEnds splits content into lines that keep their trailing
// newline; the final line lacks one when the content does. Empty content
// yields no lines.
func splitLinesKeepEnds(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.SplitAfter(content, "\n")
	if n := len(lines); lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// diffLines computes the line edit script between a and b: common
// prefix/suffix trimming around a bounded Myers core.
//
// Equality is decided on opts.LineKey's projection of each line while the
// emitted script carries the original text, so a whitespace-insensitive
// comparison still shows the reviewer what is really in the file. The key
// slices stay index-parallel to a and b, and every slice below is cut at
// the same offsets on both.
func diffLines(a, b []string, opts Options) []edit {
	ka, kb := opts.keys(a), opts.keys(b)

	// Trim the common prefix and suffix; most real diffs are tiny islands
	// of change in a sea of equality, and the Myers core is quadratic in
	// the edit distance only, so shrinking its input costs nothing.
	prefix := 0
	for prefix < len(a) && prefix < len(b) && ka[prefix] == kb[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix &&
		ka[len(a)-1-suffix] == kb[len(b)-1-suffix] {
		suffix++
	}
	middle := myersDiff(
		a[prefix:len(a)-suffix], b[prefix:len(b)-suffix],
		ka[prefix:len(ka)-suffix], kb[prefix:len(kb)-suffix],
		opts.editBound())

	edits := make([]edit, 0, prefix+len(middle)+suffix)
	for _, line := range a[:prefix] {
		edits = append(edits, edit{editEqual, line})
	}
	edits = append(edits, middle...)
	for _, line := range a[len(a)-suffix:] {
		edits = append(edits, edit{editEqual, line})
	}
	return edits
}

// myersDiff runs the greedy O(ND) Myers algorithm over the keys ka and kb,
// returning an edit script carrying the corresponding text from a and b.
// ka must be index-parallel to a and kb to b. When the edit distance
// exceeds maxDistance the result degrades to delete-all/insert-all.
func myersDiff(a, b, ka, kb []string, maxDistance int) []edit {
	n, m := len(a), len(b)
	switch {
	case n == 0 && m == 0:
		return nil
	case n == 0:
		return replaceAll(nil, b)
	case m == 0:
		return replaceAll(a, nil)
	}

	bound := n + m
	if bound > maxDistance {
		bound = maxDistance
	}

	// trace[d] holds the furthest-x frontier after sweep d, compacted to
	// the d+1 diagonals of matching parity: entry j is diagonal k = -d+2j.
	trace := make([][]int, 0, bound+1)
	prev := []int(nil)
	for d := 0; d <= bound; d++ {
		cur := make([]int, d+1)
		for k := -d; k <= d; k += 2 {
			j := (k + d) / 2
			var x int
			switch {
			case d == 0:
				x = 0
			case k == -d:
				x = prev[j] // move down from k+1
			case k == d:
				x = prev[j-1] + 1 // move right from k-1
			default:
				down, right := prev[j], prev[j-1]
				if right < down {
					x = down
				} else {
					x = right + 1
				}
			}
			y := x - k
			for x < n && y < m && ka[x] == kb[y] {
				x++
				y++
			}
			cur[j] = x
			if x >= n && y >= m {
				trace = append(trace, cur)
				return backtrack(trace, a, b)
			}
		}
		trace = append(trace, cur)
		prev = cur
	}
	// Bound exceeded: fall back to a whole replacement of the (already
	// prefix/suffix-trimmed) region.
	return replaceAll(a, b)
}

// replaceAll builds a delete-everything/insert-everything script.
func replaceAll(a, b []string) []edit {
	edits := make([]edit, 0, len(a)+len(b))
	for _, line := range a {
		edits = append(edits, edit{editDelete, line})
	}
	for _, line := range b {
		edits = append(edits, edit{editInsert, line})
	}
	return edits
}

// backtrack walks the Myers trace from the solution back to the origin,
// reconstructing the edit script in reverse.
func backtrack(trace [][]int, a, b []string) []edit {
	x, y := len(a), len(b)
	var reversed []edit
	for d := len(trace) - 1; d > 0; d-- {
		prev := trace[d-1]
		k := x - y
		// Index helpers into the compacted previous row (parity d-1).
		down := func() int { return prev[(k+d)/2] }    // value at k+1
		right := func() int { return prev[(k+d)/2-1] } // value at k-1
		var prevK int
		if k == -d || (k != d && right() < down()) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := prev[(prevK+d-1)/2]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			reversed = append(reversed, edit{editEqual, a[x-1]})
			x--
			y--
		}
		if prevK == k+1 {
			reversed = append(reversed, edit{editInsert, b[prevY]})
		} else {
			reversed = append(reversed, edit{editDelete, a[prevX]})
		}
		x, y = prevX, prevY
	}
	for x > 0 {
		reversed = append(reversed, edit{editEqual, a[x-1]})
		x--
	}
	edits := make([]edit, len(reversed))
	for i, e := range reversed {
		edits[len(reversed)-1-i] = e
	}
	return edits
}

// formatHunks renders the edit script as @@-delimited unified hunks with
// context lines of context, merging hunks whose gaps are 2*context or
// fewer unchanged lines (git's grouping rule). Returns "" for an all-equal
// script.
func formatHunks(edits []edit, context int) string {
	type span struct{ lo, hi int } // inclusive edit-index range of one hunk
	var spans []span
	for i := 0; i < len(edits); i++ {
		if edits[i].kind == editEqual {
			continue
		}
		lo := i - context
		if lo < 0 {
			lo = 0
		}
		hi := i + context
		if hi > len(edits)-1 {
			hi = len(edits) - 1
		}
		// Extend hi over the whole run of consecutive changes plus trail
		// context by scanning forward from i in the outer loop instead:
		// merge with the previous span when they touch or overlap.
		if len(spans) > 0 && lo <= spans[len(spans)-1].hi+1 {
			spans[len(spans)-1].hi = hi
		} else {
			spans = append(spans, span{lo, hi})
		}
	}
	if len(spans) == 0 {
		return ""
	}

	// Precompute, per edit index, the old/new line numbers consumed so
	// far, so hunk headers can be derived per span.
	oldBefore := make([]int, len(edits)+1)
	newBefore := make([]int, len(edits)+1)
	for i, e := range edits {
		oldBefore[i+1] = oldBefore[i]
		newBefore[i+1] = newBefore[i]
		if e.kind != editInsert {
			oldBefore[i+1]++
		}
		if e.kind != editDelete {
			newBefore[i+1]++
		}
	}

	var sb strings.Builder
	for _, sp := range spans {
		oldCount := oldBefore[sp.hi+1] - oldBefore[sp.lo]
		newCount := newBefore[sp.hi+1] - newBefore[sp.lo]
		oldStart := oldBefore[sp.lo] + 1
		if oldCount == 0 {
			oldStart = oldBefore[sp.lo]
		}
		newStart := newBefore[sp.lo] + 1
		if newCount == 0 {
			newStart = newBefore[sp.lo]
		}
		fmt.Fprintf(&sb, "@@ -%s +%s @@\n",
			formatHunkRange(oldStart, oldCount), formatHunkRange(newStart, newCount))
		for i := sp.lo; i <= sp.hi; i++ {
			e := edits[i]
			switch e.kind {
			case editEqual:
				sb.WriteByte(' ')
			case editDelete:
				sb.WriteByte('-')
			case editInsert:
				sb.WriteByte('+')
			}
			text, hadNewline := strings.CutSuffix(e.text, "\n")
			sb.WriteString(text)
			sb.WriteByte('\n')
			if !hadNewline {
				sb.WriteString("\\ No newline at end of file\n")
			}
		}
	}
	return sb.String()
}

// formatHunkRange renders one side of a hunk header, omitting the count
// when it is exactly 1 as git does.
func formatHunkRange(start, count int) string {
	if count == 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

// UnifiedFileDiff renders one file's git-style diff from its blob pair.
// oldPath "" means the file was added; newPath "" means it was deleted;
// differing non-empty paths mean a rename unless opts.NoRenameHeaders says
// otherwise. Paths are relative and carry no leading slash. Binary content
// on either side produces a binary stub. Returns "" when there is nothing
// to show (identical blobs that are not a rename).
func UnifiedFileDiff(oldPath, newPath, oldContent, newContent string, opts Options) string {
	headerOld, headerNew := oldPath, newPath
	if headerOld == "" {
		headerOld = newPath
	}
	if headerNew == "" {
		headerNew = oldPath
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "diff --git a/%s b/%s\n", headerOld, headerNew)

	renamed := !opts.NoRenameHeaders && oldPath != "" && newPath != "" && oldPath != newPath
	if renamed && oldContent == newContent {
		sb.WriteString("similarity index 100%\n")
	}
	if renamed {
		fmt.Fprintf(&sb, "rename from %s\n", oldPath)
		fmt.Fprintf(&sb, "rename to %s\n", newPath)
	}
	switch {
	case oldPath == "":
		sb.WriteString("new file mode 100644\n")
	case newPath == "":
		sb.WriteString("deleted file mode 100644\n")
	}

	if isBinaryContent(oldContent) || isBinaryContent(newContent) {
		leftLabel, rightLabel := "a/"+headerOld, "b/"+headerNew
		if oldPath == "" {
			leftLabel = "/dev/null"
		}
		if newPath == "" {
			rightLabel = "/dev/null"
		}
		fmt.Fprintf(&sb, "Binary files %s and %s differ\n", leftLabel, rightLabel)
		return sb.String()
	}

	edits := diffLines(splitLinesKeepEnds(oldContent), splitLinesKeepEnds(newContent), opts)
	hunks := formatHunks(edits, opts.context())
	if hunks == "" {
		if oldPath != "" && newPath != "" && !renamed {
			// Identical blobs on a plain edit entry: nothing to show.
			//
			// With NoRenameHeaders this also covers two differently-named
			// files whose contents match: there is genuinely no difference
			// to review, and saying so beats an empty file entry.
			return ""
		}
		// Empty add/delete or pure rename: headers alone carry the change.
		return sb.String()
	}

	leftLabel, rightLabel := "a/"+oldPath, "b/"+newPath
	if oldPath == "" {
		leftLabel = "/dev/null"
	}
	if newPath == "" {
		rightLabel = "/dev/null"
	}
	fmt.Fprintf(&sb, "--- %s\n", leftLabel)
	fmt.Fprintf(&sb, "+++ %s\n", rightLabel)
	sb.WriteString(hunks)
	return sb.String()
}
