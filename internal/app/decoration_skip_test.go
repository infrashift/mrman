package app

// decoration_skip_test.go ports tuicr's src/app/tests/decoration_skip_tests.rs
// in full.

import "testing"

func decoDiffLine(fileIdx int, newLineno uint32) AnnotatedLine {
	return AnnotatedLine{Kind: AnnDiffLine, FileIdx: fileIdx, NewLineno: new(newLineno)}
}

// decoFixture is two files: header, content, spacing, header, content.
func decoFixture() []AnnotatedLine {
	return []AnnotatedLine{
		{Kind: AnnFileHeader, FileIdx: 0}, // 0
		decoDiffLine(0, 1),                // 1
		{Kind: AnnSpacing},                // 2
		{Kind: AnnFileHeader, FileIdx: 1}, // 3
		decoDiffLine(1, 1),                // 4
	}
}

func TestForwardSkipsSpacingAndHeaderToNextContentLine(t *testing.T) {
	annotations := decoFixture()
	assertEq(t, skipDecorationForward(annotations, 2, 4), 4, "skip forward from spacing")
}

func TestForwardKeepsPositionOnContentLine(t *testing.T) {
	annotations := decoFixture()
	assertEq(t, skipDecorationForward(annotations, 1, 4), 1, "keep position on content")
}

func TestForwardClampsAtMaxLineEvenOnDecoration(t *testing.T) {
	annotations := []AnnotatedLine{
		decoDiffLine(0, 1),                // 0
		{Kind: AnnSpacing},                // 1
		{Kind: AnnFileHeader, FileIdx: 1}, // 2
	}
	assertEq(t, skipDecorationForward(annotations, 1, 2), 2, "clamp at max line")
}

func TestBackwardSkipsHeaderAndSpacingToPreviousContentLine(t *testing.T) {
	annotations := decoFixture()
	assertEq(t, skipDecorationBackward(annotations, 3), 1, "skip backward from header")
}

func TestBackwardStopsAtZeroWhenTopIsDecoration(t *testing.T) {
	annotations := decoFixture()
	assertEq(t, skipDecorationBackward(annotations, 0), 0, "stop at zero")
}
