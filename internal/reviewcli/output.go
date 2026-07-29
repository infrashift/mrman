package reviewcli

import (
	"fmt"
	"sort"
	"time"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
)

// nowFn is an injection seam for deterministic tests.
var nowFn = func() time.Time { return time.Now().UTC() }

// SessionSummaryOutput is one row of `mrman review list`, shape-identical to
// tuicr's output.
type SessionSummaryOutput struct {
	Slug          string `json:"slug"`
	Kind          string `json:"kind"`
	Path          string `json:"path"`
	UpdatedAt     string `json:"updated_at"`
	CommentCount  int    `json:"comment_count"`
	ReviewedCount int    `json:"reviewed_count"`
	FileCount     int    `json:"file_count"`
	Anchor        string `json:"anchor"`
	Active        bool   `json:"active"`
}

func summaryOutput(s persistence.SessionSummary) SessionSummaryOutput {
	return SessionSummaryOutput{
		Slug:          s.Slug,
		Kind:          s.Kind,
		Path:          s.Path,
		UpdatedAt:     s.UpdatedAt.Format(time.RFC3339),
		CommentCount:  s.CommentCount,
		ReviewedCount: s.ReviewedCount,
		FileCount:     s.FileCount,
		Anchor:        s.Anchor,
		Active:        s.Active,
	}
}

// CommentOutput is one row of `mrman review comments` and the result of
// `mrman review add`, shape-identical to tuicr's output.
type CommentOutput struct {
	ID             string  `json:"id"`
	Location       string  `json:"location"`
	Path           *string `json:"path"`
	StartLine      *uint32 `json:"start_line"`
	EndLine        *uint32 `json:"end_line"`
	Side           *string `json:"side"`
	CommentType    string  `json:"comment_type"`
	LifecycleState string  `json:"lifecycle_state"`
	CreatedAt      string  `json:"created_at"`
	Content        string  `json:"content"`
}

func lineLocation(path string, startLine, endLine uint32, side *model.LineSide) string {
	lines := fmt.Sprintf("%d", startLine)
	if startLine != endLine {
		lines = fmt.Sprintf("%d-%d", startLine, endLine)
	}
	if side != nil && *side == model.LineSideOld {
		return fmt.Sprintf("%s:%s [old]", path, lines)
	}
	return fmt.Sprintf("%s:%s", path, lines)
}

func targetLocation(t CommentTarget) string {
	side := t.Side
	switch t.Kind {
	case TargetReview:
		return "review"
	case TargetFile:
		return t.Path
	case TargetLine:
		return lineLocation(t.Path, t.Line, t.Line, &side)
	case TargetLineRange:
		return lineLocation(t.Path, t.Range.Start, t.Range.End, &side)
	}
	return ""
}

func sideID(side *model.LineSide) *string {
	if side == nil {
		return nil
	}
	s := string(*side)
	return &s
}

func commentOutputFromParts(location string, path *string, startLine, endLine *uint32,
	side *model.LineSide, c *model.Comment) CommentOutput {
	return CommentOutput{
		ID:             c.ID,
		Location:       location,
		Path:           path,
		StartLine:      startLine,
		EndLine:        endLine,
		Side:           sideID(side),
		CommentType:    c.CommentType.ID(),
		LifecycleState: string(c.LifecycleState),
		CreatedAt:      c.CreatedAt.Format(time.RFC3339),
		Content:        c.Content,
	}
}

func commentOutputFromTarget(t CommentTarget, c *model.Comment) CommentOutput {
	side := t.Side
	switch t.Kind {
	case TargetReview:
		return commentOutputFromParts(targetLocation(t), nil, nil, nil, nil, c)
	case TargetFile:
		path := t.Path
		return commentOutputFromParts(targetLocation(t), &path, nil, nil, nil, c)
	case TargetLine:
		path, line := t.Path, t.Line
		return commentOutputFromParts(targetLocation(t), &path, &line, &line, &side, c)
	case TargetLineRange:
		path, start, end := t.Path, t.Range.Start, t.Range.End
		return commentOutputFromParts(targetLocation(t), &path, &start, &end, &side, c)
	}
	return CommentOutput{}
}

// collectComments walks a session in tuicr's canonical order: review
// comments, then files sorted by path with file comments before line
// comments sorted by line key.
func collectComments(session *model.ReviewSession) []CommentOutput {
	comments := []CommentOutput{}
	for _, c := range session.ReviewComments {
		comments = append(comments, commentOutputFromParts("review", nil, nil, nil, nil, c))
	}

	paths := make([]string, 0, len(session.Files))
	for path := range session.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		review := session.Files[path]
		pathCopy := path
		for _, c := range review.FileComments {
			comments = append(comments, commentOutputFromParts(path, &pathCopy, nil, nil, nil, c))
		}

		lines := make([]uint32, 0, len(review.LineComments))
		for line := range review.LineComments {
			lines = append(lines, line)
		}
		sort.Slice(lines, func(i, j int) bool { return lines[i] < lines[j] })

		for _, line := range lines {
			for _, c := range review.LineComments[line] {
				startLine, endLine := line, line
				if c.LineRange != nil {
					startLine, endLine = c.LineRange.Start, c.LineRange.End
				}
				location := lineLocation(path, startLine, endLine, c.Side)
				start, end := startLine, endLine
				comments = append(comments,
					commentOutputFromParts(location, &pathCopy, &start, &end, c.Side, c))
			}
		}
	}
	return comments
}
