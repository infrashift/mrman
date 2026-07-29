package cellrender

import "image/color"

// RowKind tags a LogicalLine with its semantic role in the diff stream. It
// replaces tuicr's zero-width-marker content sniffing: the compositor and
// navigation code branch on the tag, never on the rendered text.
type RowKind uint8

// Row kinds for the diff stream, in tuicr's build order.
const (
	// RowDiffLine is a context/add/del source line.
	RowDiffLine RowKind = iota
	// RowFileHeader is a per-file "═══ path ═══" section header.
	RowFileHeader
	// RowHunkHeader is an "@@ -a,b +c,d @@" hunk header.
	RowHunkHeader
	// RowExpander is a "… N hidden lines …" context-expansion control row.
	RowExpander
	// RowHiddenStub is the collapsed stand-in for a hidden (reviewed/ignored)
	// file or hunk body.
	RowHiddenStub
	// RowCommentTop is the top border row of an inline comment box.
	RowCommentTop
	// RowCommentMiddle is a content row inside an inline comment box.
	RowCommentMiddle
	// RowCommentDivider is a divider row between comment threads in a box.
	RowCommentDivider
	// RowCommentBottom is the bottom border row of an inline comment box.
	RowCommentBottom
	// RowBlank is an intentional empty spacer row.
	RowBlank
	// RowPlaceholder is an off-viewport stand-in row that has not been built.
	RowPlaceholder
)

// LogicalLine is one pre-wrap row of the diff stream: styled content plus the
// semantic metadata the compositor needs to paint overlays without sniffing
// the text.
type LogicalLine struct {
	// Kind is the row's semantic role.
	Kind RowKind
	// Spans is the row's styled content.
	Spans []Span
	// Ann is the annotation index this row belongs to, or -1 when the row is
	// not part of an annotation.
	Ann int
	// BaseBg is the add/del row background, nil for none.
	BaseBg color.Color
	// CommentFg is the comment border/author accent color, nil for default.
	CommentFg color.Color
}
