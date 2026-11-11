package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/risk"
)

// writeVizziniReport writes a pr-review style report for testApp's diff
// (src/x.go, new lines 1..3) and returns its path. base < 0 uses the
// diff's real deterministic score, so the report is not stale.
func writeVizziniReport(t *testing.T, a *app.App, base int, findings []map[string]any) string {
	t.Helper()
	if base < 0 {
		as := risk.Assess(a.DiffFiles, risk.DefaultPolicy())
		base = as.Score
	}
	model := max(base, 72)
	doc := map[string]any{
		"apiVersion": "vizzini.infrashift.io/v1", "kind": "Review",
		"summary":    "two problems",
		"risk_score": max(base, model), "base_risk_score": base, "model_risk_score": model,
		"auto_approve": false, "findings": findings, "chunks": 1,
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "review.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func sampleFindings() []map[string]any {
	return []map[string]any{
		{"file": "src/x.go", "start_line": 2, "end_line": 3, "severity": "Critical", "category": "Security",
			"title": "Hardcoded \x1b[31mvalue", "body": "A is now 10 and C is new; both are magic numbers.",
			"suggestion": "const A = 10", "citations": []string{"adr-0001"}},
		{"file": "src/gone.go", "start_line": 1, "end_line": 1, "severity": "Minor", "category": "Logic",
			"title": "elsewhere", "body": "b"},
	}
}

func vizziniModel(t *testing.T, base int) (*Model, []string) {
	t.Helper()
	_, m := testLifecycle(t)
	path := writeVizziniReport(t, m.App, base, sampleFindings())
	warnings := applyVizzini(config.Config{}, m.App, path)
	m.App.RebuildAnnotations()
	m.syncViewport()
	return m, warnings
}

func TestVizziniReportShowsHeaderMarkersAndWarnings(t *testing.T) {
	m, warnings := vizziniModel(t, -1)
	if m.App.Vizzini == nil {
		t.Fatal("report not loaded")
	}
	if m.App.Vizzini.Stale {
		t.Error("a report scored on this diff must not be stale")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "1 Vizzini finding(s) name files that are not in this diff") {
		t.Errorf("warnings %q", warnings)
	}
	view := plainView(m)
	for _, want := range []string{"vizzini 72 (diff", "2 findings", "◆", "┊"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "STALE") {
		t.Error("fresh report marked STALE")
	}
	// Model text is sanitized on load: no escape sequences reach the screen.
	if got := m.App.Vizzini.Report.Findings[0].Title; strings.ContainsRune(got, '\x1b') {
		t.Errorf("title not sanitized: %q", got)
	}
}

func TestVizziniMarkersSkipDeletedAndOtherLines(t *testing.T) {
	m, _ := vizziniModel(t, -1)
	marks := map[app.FindingMark]int{}
	for i := range m.App.LineAnnotations {
		ann := &m.App.LineAnnotations[i]
		mark, sev := m.App.FindingMarkAt(ann)
		marks[mark]++
		if mark != app.FindingNone && sev != "Critical" {
			t.Errorf("row %d severity %q", i, sev)
		}
		if mark != app.FindingNone && ann.NewLineno == nil {
			t.Errorf("deleted row %d marked", i)
		}
	}
	if marks[app.FindingStart] != 1 || marks[app.FindingInside] != 1 {
		t.Errorf("marks %v, want one start (line 2) and one inside (line 3)", marks)
	}
}

func TestVizziniSideBySideMarksTheNewColumn(t *testing.T) {
	m, _ := vizziniModel(t, -1)
	m.App.DiffViewMode = app.ViewSideBySide
	m.App.RebuildAnnotations()
	m.syncViewport()
	view := plainView(m)
	if !strings.Contains(view, "◆") {
		t.Errorf("side-by-side view lacks the finding marker:\n%s", view)
	}
}

func TestVizziniStaleReportIsFlagged(t *testing.T) {
	m, warnings := vizziniModel(t, 99)
	if !m.App.Vizzini.Stale {
		t.Fatal("report with another diff's score must be stale")
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "looks stale") {
		t.Errorf("warnings %q", warnings)
	}
	if !strings.Contains(plainView(m), "STALE") {
		t.Error("header must say STALE")
	}
}

func TestVizziniNextPeekAndPromote(t *testing.T) {
	m, _ := vizziniModel(t, -1)

	pressRune(m, 'F')
	if mark, _ := m.App.FindingMarkAt(&m.App.LineAnnotations[m.App.DiffState.CursorLine]); mark != app.FindingStart {
		t.Fatalf("F must land on the finding's first line, cursor row %d", m.App.DiffState.CursorLine)
	}
	pressRune(m, 'f')
	if m.App.InputMode != input.ModeCommentPeek || m.App.CommentPeek == nil {
		t.Fatal("f must open the findings peek")
	}
	view := plainView(m)
	if !strings.Contains(modeHint(m.App), "p promote to draft") {
		t.Errorf("status hint %q must mention p", modeHint(m.App))
	}
	for _, want := range []string{"Vizzini · src/x.go:2", "p promote to draft", "Critical · Security · Hardcoded", "suggestion:", "cites: adr-0001"} {
		if !strings.Contains(view, want) {
			t.Errorf("peek lacks %q:\n%s", want, view)
		}
	}

	pressRune(m, 'p')
	if m.App.InputMode != input.ModeComment {
		t.Fatal("p must open the comment box with the finding")
	}
	if !strings.HasPrefix(m.App.CommentBuffer, "**Critical (Security): Hardcoded") ||
		!strings.Contains(m.App.CommentBuffer, "const A = 10") {
		t.Errorf("draft %q", m.App.CommentBuffer)
	}
	if r := m.App.CommentLineRange; r == nil || r.Range.Start != 2 || r.Range.End != 3 {
		t.Errorf("draft must span the finding's lines, got %+v", r)
	}
	press(m, "", tea.KeyEnter, 0)
	file := m.App.Session.File("src/x.go")
	if file == nil || file.CommentCount() != 1 {
		t.Fatalf("promoted draft not saved: %+v", file)
	}

	// The finding is now promoted: the peek says so and p does nothing more.
	pressRune(m, 'f')
	if !strings.Contains(plainView(m), "[draft comment made]") {
		t.Error("peek must mark a promoted finding")
	}
	pressRune(m, 'p')
	if m.App.InputMode == input.ModeComment {
		t.Error("a finding must not be promoted twice")
	}
	press(m, "", tea.KeyEscape, 0)
	if m.App.InputMode != input.ModeNormal {
		t.Error("Esc must close the peek")
	}
}

func TestVizziniKeysWithoutAReport(t *testing.T) {
	m := testModel(t)
	pressRune(m, 'F')
	pressRune(m, 'f')
	if m.App.InputMode != input.ModeNormal {
		t.Error("f without a report must not open a peek")
	}
	if !strings.Contains(plainView(m), "No Vizzini report loaded") {
		t.Error("missing hint about --vizzini-report")
	}
	if m.App.PromoteFinding() {
		t.Error("nothing to promote without a peek")
	}
}

func TestVizziniPeekNeedsAFindingUnderTheCursor(t *testing.T) {
	m, _ := vizziniModel(t, -1)
	m.App.MoveCursorToAnnotation(0)
	pressRune(m, 'f')
	if m.App.InputMode == input.ModeCommentPeek {
		t.Error("f off a marked line must not open the peek")
	}
}

func TestApplyVizziniBadReportIsAWarning(t *testing.T) {
	m := testModel(t)
	if w := applyVizzini(config.Config{}, m.App, filepath.Join(t.TempDir(), "missing.json")); len(w) != 1 ||
		!strings.Contains(w[0], "Vizzini report not loaded") {
		t.Errorf("warnings %q", w)
	}
	if m.App.Vizzini != nil {
		t.Error("a failed load must leave no report")
	}
	if w := applyVizzini(config.Config{}, m.App, ""); w != nil {
		t.Errorf("no flag, no warnings: %q", w)
	}
}

func TestApplyVizziniUsesTheRiskPolicyWhenRiskIsOn(t *testing.T) {
	_, m := testLifecycle(t)
	applyRisk(config.Config{Risk: config.RiskConfig{Enabled: true}}, m.App)
	path := writeVizziniReport(t, m.App, -1, sampleFindings())
	applyVizzini(config.Config{Risk: config.RiskConfig{Enabled: true}}, m.App, path)
	if m.App.Vizzini == nil || m.App.Vizzini.Policy != m.App.Risk.Policy {
		t.Fatal("the report must be checked with the policy risk scoring uses")
	}
}

func TestHeadlessVizzini(t *testing.T) {
	a := testApp(t)
	path := writeVizziniReport(t, a, -1, sampleFindings())
	out, warnings, err := headlessVizzini(config.Config{}, "", path, a.DiffFiles)
	if err != nil {
		t.Fatal(err)
	}
	if out.Report != path || out.Stale || out.Unanchored != 1 || len(out.Findings) != 2 || out.RiskScore != 72 || out.Summary != "two problems" {
		t.Errorf("output %+v", out)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings %q", warnings)
	}
	b, _ := json.Marshal(PrSessionOutput{Vizzini: out})
	if !strings.Contains(string(b), `"vizzini":{"report":`) || !strings.Contains(string(b), `"start_line":2`) {
		t.Errorf("json %s", b)
	}
	if b, _ := json.Marshal(PrSessionOutput{}); strings.Contains(string(b), "vizzini") {
		t.Error("no report, no vizzini key")
	}

	if o, _, err := headlessVizzini(config.Config{}, "", "", a.DiffFiles); o != nil || err != nil {
		t.Error("no flag must be a no-op")
	}
	if _, _, err := headlessVizzini(config.Config{}, "", filepath.Join(t.TempDir(), "nope.json"), a.DiffFiles); err == nil {
		t.Error("headless must fail on an unreadable report")
	}
}

func TestFindingCommentText(t *testing.T) {
	got := app.FindingCommentText(&risk.Finding{Severity: "Major", Category: "Logic", Title: "t", Body: " b \n"})
	want := "**Major (Logic): t**\n\nb\n\n_(from a Vizzini review)_"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
