// vizzini.go shows a Vizzini review report next to the diff it reviewed:
// gutter markers on the lines each finding anchors to, a peek panel with the
// finding's text, and promotion of a finding into an ordinary draft comment
// that the reviewer edits and submits like any other.
//
// mrman never runs the model. The report comes from CI or from someone
// running pr-review by hand, and its text is model output: it is sanitized on
// load and only ever becomes a comment through the reviewer's own edit box.

package app

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/risk"
	"github.com/infrashift/mrman/internal/textsafe"
)

// VizziniState is a loaded review report. App.Vizzini is nil unless one was
// given with --vizzini-report.
type VizziniState struct {
	// Path is where the report was read from.
	Path   string
	Report *risk.Report
	// Policy scores the diff on screen to tell whether the report is stale.
	Policy *risk.Policy
	// Stale is set when the report's deterministic score differs from a
	// fresh assessment of the diff on screen: it reviewed another diff.
	Stale bool
	// Unanchored counts findings whose file is not in the diff.
	Unanchored int
	// Promoted marks findings already turned into a draft comment, by index.
	Promoted map[int]bool
}

// NewVizziniState sanitizes r's model-written text and checks it against
// files, the diff it is meant to describe.
func NewVizziniState(path string, r *risk.Report, files []model.DiffFile, p *risk.Policy) *VizziniState {
	for i := range r.Findings {
		f := &r.Findings[i]
		f.File = textsafe.SanitizeLine(f.File)
		f.Severity = textsafe.SanitizeLine(f.Severity)
		f.Category = textsafe.SanitizeLine(f.Category)
		f.Title = textsafe.SanitizeLine(f.Title)
		f.Body = textsafe.Sanitize(f.Body)
		f.Suggestion = textsafe.Sanitize(f.Suggestion)
		for j := range f.Citations {
			f.Citations[j] = textsafe.SanitizeLine(f.Citations[j])
		}
	}
	r.Summary = textsafe.Sanitize(r.Summary)
	v := &VizziniState{Path: path, Report: r, Policy: p, Promoted: map[int]bool{}}
	v.refresh(files)
	return v
}

// refresh recomputes staleness and anchoring after the diff changed.
func (v *VizziniState) refresh(files []model.DiffFile) {
	a := risk.Assess(files, v.Policy)
	v.Stale = risk.Stale(v.Report, &a)
	paths := make(map[string]bool, len(files))
	for i := range files {
		paths[files[i].DisplayPath()] = true
	}
	v.Unanchored = 0
	for i := range v.Report.Findings {
		if !paths[v.Report.Findings[i].File] {
			v.Unanchored++
		}
	}
}

// LoadVizzini attaches a parsed report to the session.
func (a *App) LoadVizzini(path string, r *risk.Report, p *risk.Policy) {
	a.Vizzini = NewVizziniState(path, r, a.DiffFiles, p)
}

// refreshVizzini re-checks the report after DiffFiles changed.
func (a *App) refreshVizzini() {
	if a.Vizzini != nil {
		a.Vizzini.refresh(a.DiffFiles)
	}
}

// FindingMark is how a diff row relates to the findings: none, the first
// line of one, or a later line inside one's range.
type FindingMark int

// Finding marks, weakest first.
const (
	FindingNone FindingMark = iota
	FindingInside
	FindingStart
)

// FindingsOnLine returns the indices of the findings covering new-side line
// lineno of path, most severe first.
func (a *App) FindingsOnLine(path string, lineno uint32) []int {
	if a.Vizzini == nil {
		return nil
	}
	var out []int
	for i := range a.Vizzini.Report.Findings {
		f := &a.Vizzini.Report.Findings[i]
		if f.File == path && int(lineno) >= f.StartLine && int(lineno) <= f.EndLine {
			out = append(out, i)
		}
	}
	slices.SortStableFunc(out, func(x, y int) int {
		return SeverityRank(a.Vizzini.Report.Findings[y].Severity) - SeverityRank(a.Vizzini.Report.Findings[x].Severity)
	})
	return out
}

// FindingMarkAt reports how annotation ann relates to the findings, and the
// severity of the most severe finding covering it. Findings anchor to the
// new side, so deleted lines are never marked.
func (a *App) FindingMarkAt(ann *AnnotatedLine) (FindingMark, string) {
	if a.Vizzini == nil || ann.NewLineno == nil || ann.FileIdx < 0 || ann.FileIdx >= len(a.DiffFiles) {
		return FindingNone, ""
	}
	if ann.Kind != AnnDiffLine && ann.Kind != AnnSideBySideLine {
		return FindingNone, ""
	}
	idx := a.FindingsOnLine(a.DiffFiles[ann.FileIdx].DisplayPath(), *ann.NewLineno)
	if len(idx) == 0 {
		return FindingNone, ""
	}
	mark := FindingInside
	for _, i := range idx {
		if a.Vizzini.Report.Findings[i].StartLine == int(*ann.NewLineno) {
			mark = FindingStart
		}
	}
	return mark, a.Vizzini.Report.Findings[idx[0]].Severity
}

// SeverityRank orders Vizzini severities, unknown ones lowest.
func SeverityRank(s string) int {
	switch s {
	case "Critical":
		return 4
	case "Major":
		return 3
	case "Minor":
		return 2
	case "Trivial":
		return 1
	}
	return 0
}

// findingStartRows lists the annotation rows where a finding starts, in
// display order, so n/N-style navigation can walk them.
func (a *App) findingStartRows() []int {
	var rows []int
	for i := range a.LineAnnotations {
		if mark, _ := a.FindingMarkAt(&a.LineAnnotations[i]); mark == FindingStart {
			rows = append(rows, i)
		}
	}
	return rows
}

// NextFinding moves the cursor to the next row where a finding starts,
// wrapping to the first.
func (a *App) NextFinding() {
	if a.Vizzini == nil {
		a.SetMessage("No Vizzini report loaded (--vizzini-report)")
		return
	}
	rows := a.findingStartRows()
	if len(rows) == 0 {
		a.SetMessage(fmt.Sprintf("No Vizzini findings in the visible diff (%d in folded or missing files)", len(a.Vizzini.Report.Findings)))
		return
	}
	target := 0
	for i, r := range rows {
		if r > a.DiffState.CursorLine {
			target = i
			break
		}
	}
	a.MoveCursorToAnnotation(rows[target])
	a.FocusedPanel = PanelDiff
	a.SetMessage(fmt.Sprintf("Finding %d/%d · f to read it", target+1, len(rows)))
}

// OpenFindingPeek opens the peek panel on the findings covering the cursor
// line. Reports whether there were any.
func (a *App) OpenFindingPeek() bool {
	if a.Vizzini == nil {
		a.SetMessage("No Vizzini report loaded (--vizzini-report)")
		return false
	}
	lineno, side, ok := a.LineAtCursor()
	path, okPath := a.CurrentFilePath()
	if !ok || !okPath || side != model.LineSideNew {
		a.SetMessage("Move the cursor to a marked line (F jumps to the next finding)")
		return false
	}
	idx := a.FindingsOnLine(path, lineno)
	if len(idx) == 0 {
		a.SetMessage("No Vizzini finding on this line (F jumps to the next)")
		return false
	}
	newSide := model.LineSideNew
	var lines []PeekLine
	anchorRow := -1
	if fileIdx := a.fileIdxForPath(path); fileIdx >= 0 {
		lines, anchorRow = a.peekContextLines(fileIdx, lineno, &newSide)
	}
	width := max(a.DiffState.ViewportWidth-10, 40)
	for _, i := range idx {
		lines = append(lines, PeekLine{Kind: PeekSeparator})
		lines = append(lines, findingPeekLines(&a.Vizzini.Report.Findings[i], a.Vizzini.Promoted[i], width)...)
	}
	a.CommentPeek = &CommentPeekState{
		Lines: lines, AnchorRow: anchorRow,
		Title:    fmt.Sprintf(" Vizzini · %s:%d ", path, lineno),
		Findings: idx,
	}
	a.InputMode = input.ModeCommentPeek
	return true
}

func findingPeekLines(f *risk.Finding, promoted bool, width int) []PeekLine {
	head := fmt.Sprintf("◆ %s · %s · %s", f.Severity, f.Category, f.Title)
	if f.EndLine > f.StartLine {
		head += fmt.Sprintf("  (lines %d-%d)", f.StartLine, f.EndLine)
	}
	if promoted {
		head += "  [draft comment made]"
	}
	lines := []PeekLine{{Kind: PeekFindingHeader, Text: head, Severity: f.Severity}}
	lines = append(lines, wrapPeekBody(f.Body, width)...)
	if f.Suggestion != "" {
		lines = append(lines, PeekLine{Kind: PeekNotice, Text: "suggestion:"})
		for l := range strings.SplitSeq(f.Suggestion, "\n") {
			lines = append(lines, PeekLine{Kind: PeekCommentBody, Text: "  " + l})
		}
	}
	if len(f.Citations) > 0 {
		lines = append(lines, PeekLine{Kind: PeekNotice, Text: "cites: " + strings.Join(f.Citations, ", ")})
	}
	return lines
}

func wrapPeekBody(s string, width int) []PeekLine {
	var lines []PeekLine
	for para := range strings.SplitSeq(s, "\n") {
		for _, seg := range WrapSegments(para, width) {
			lines = append(lines, PeekLine{Kind: PeekCommentBody, Text: seg})
		}
	}
	return lines
}

// PromoteFinding turns the first not-yet-promoted finding in the open peek
// into a draft comment and opens it in the comment box, anchored to the
// finding's lines, for the reviewer to edit before saving. Nothing is saved
// until they do. Reports whether the comment box opened.
func (a *App) PromoteFinding() bool {
	p := a.CommentPeek
	if p == nil || len(p.Findings) == 0 || a.Vizzini == nil {
		return false
	}
	pick := -1
	for _, i := range p.Findings {
		if !a.Vizzini.Promoted[i] {
			pick = i
			break
		}
	}
	if pick < 0 {
		a.SetMessage("Every finding on this line already has a draft comment")
		return false
	}
	f := &a.Vizzini.Report.Findings[pick]
	a.CloseCommentPeek()
	a.enterCommentInput()
	a.CommentIsReviewLevel = false
	a.CommentIsFileLevel = false
	a.EditingCommentID = nil
	a.CommentLine = &CommentAnchor{Line: lineNo(f.EndLine), Side: model.LineSideNew}
	a.CommentLineRange = nil
	if f.EndLine > f.StartLine {
		a.CommentLineRange = &CommentRangeAnchor{
			Range: model.LineRange{Start: lineNo(f.StartLine), End: lineNo(f.EndLine)},
			Side:  model.LineSideNew,
		}
	}
	if t, ok := a.commentTypeByID("issue"); ok {
		a.CommentType = t
	}
	a.CommentBuffer = FindingCommentText(f)
	a.CommentCursor = len(a.CommentBuffer)
	a.Vizzini.Promoted[pick] = true
	a.SetMessage("Vizzini finding copied into a draft: edit, then save as usual")
	return true
}

// lineNo converts a report's line number for a comment anchor. ParseReport
// guarantees it is at least 1; a value past uint32 cannot be in any diff, so
// clamping only keeps the conversion defined.
func lineNo(n int) uint32 {
	return uint32(min(max(n, 1), math.MaxUint32))
}

func (a *App) commentTypeByID(id string) (model.CommentType, bool) {
	for i := range a.CommentTypes {
		if a.CommentTypes[i].ID == id {
			return model.CommentTypeFromID(id), true
		}
	}
	return "", false
}

// FindingCommentText is the draft comment a promoted finding starts as.
func FindingCommentText(f *risk.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s (%s): %s**\n\n%s", f.Severity, f.Category, f.Title, strings.TrimSpace(f.Body))
	if s := strings.TrimSpace(f.Suggestion); s != "" {
		fmt.Fprintf(&b, "\n\nSuggestion:\n```\n%s\n```", s)
	}
	if len(f.Citations) > 0 {
		fmt.Fprintf(&b, "\n\nSee: %s", strings.Join(f.Citations, ", "))
	}
	b.WriteString("\n\n_(from a Vizzini review)_")
	return b.String()
}

// VizziniHeader is the header chunk for a loaded report, e.g.
// "vizzini 72 (diff 41·model 72) · 9 findings · STALE".
func (a *App) VizziniHeader() string {
	v := a.Vizzini
	if v == nil {
		return ""
	}
	r := v.Report
	s := fmt.Sprintf("vizzini %d (diff %d·model %d) · %d finding%s", r.RiskScore, r.BaseRiskScore, r.ModelRiskScore,
		len(r.Findings), plural(len(r.Findings)))
	if r.AutoApprove {
		s += " · approve suggested"
	}
	if v.Stale {
		s += " · STALE"
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
