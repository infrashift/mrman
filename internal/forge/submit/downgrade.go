package submit

import "fmt"

// DowngradeMultiline collapses multi-line range comments to their end line
// for forges without the MultiLineComments capability. The dropped span is
// preserved in prose with a "Lines X–Y: " body prefix so reviewers still see
// what the comment covered. Single-line comments pass through unchanged; the
// input slice is not mutated.
func DowngradeMultiline(comments []InlineComment) []InlineComment {
	out := make([]InlineComment, len(comments))
	for i, c := range comments {
		if c.StartLine != nil && *c.StartLine != c.Line {
			c.Body = fmt.Sprintf("Lines %d–%d: %s", *c.StartLine, c.Line, c.Body)
		}
		c.StartLine = nil
		c.StartSide = nil
		out[i] = c
	}
	return out
}
