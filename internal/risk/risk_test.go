package risk

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	vrisk "github.com/infrashift/vizzini-pr-reviewer/pkg/risk"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs/diffparser"
)

// patch touches an auth file (weight 3, decision points 1 → 3), a doc
// (weight 0.1), go.sum (excluded by a review), and deletes a Go file. The
// auth body uses tabs, which Content expands and Raw keeps.
const patch = "diff --git a/internal/auth/login.go b/internal/auth/login.go\n" +
	"--- a/internal/auth/login.go\n+++ b/internal/auth/login.go\n" +
	"@@ -1,3 +1,3 @@\n x\n-\tif a {\n+\tif a && b || c {\n x\n" +
	"diff --git a/docs/a.md b/docs/a.md\n--- a/docs/a.md\n+++ b/docs/a.md\n" +
	"@@ -1,1 +1,5 @@\n x\n+t\n+t\n+t\n+t\n" +
	"diff --git a/go.sum b/go.sum\n--- a/go.sum\n+++ b/go.sum\n@@ -1,1 +1,2 @@\n x\n+y\n" +
	"diff --git a/old.go b/old.go\ndeleted file mode 100644\n--- a/old.go\n+++ /dev/null\n" +
	"@@ -1,2 +0,0 @@\n-package old\n-var x = 1\n"

func parse(t *testing.T, text string) []model.DiffFile {
	t.Helper()
	files, err := diffparser.Parse(text, diffparser.GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// The score mrman shows must be the base_risk_score a Vizzini review of the
// same patch reports. AssessPatch is that computation.
func TestAssess_MatchesVizzini(t *testing.T) {
	p := DefaultPolicy()
	got := Assess(parse(t, patch), p)
	want, err := vrisk.AssessPatch(strings.NewReader(patch), vrisk.DefaultPolicy(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Score != want.Score || got.Band != want.Band {
		t.Errorf("mrman %d %s, vizzini %d %s", got.Score, got.Band, want.Score, want.Band)
	}
	byPath := map[string]vrisk.FileRisk{}
	for _, f := range want.Files {
		byPath[f.Path] = f
	}
	counted := 0
	for _, f := range got.Files {
		if !f.Counted {
			continue
		}
		counted++
		if byPath[f.Path] != f.FileRisk {
			t.Errorf("%s: mrman %+v, vizzini %+v", f.Path, f.FileRisk, byPath[f.Path])
		}
	}
	if counted != len(want.Files) {
		t.Errorf("counted %d files, vizzini %d", counted, len(want.Files))
	}
}

func TestAssess_IndexAlignedAndMarked(t *testing.T) {
	files := parse(t, patch)
	a := Assess(files, DefaultPolicy())
	if len(a.Files) != len(files) {
		t.Fatalf("%d scores for %d files", len(a.Files), len(files))
	}
	for i := range files {
		path := files[i].DisplayPath()
		s := a.Files[i]
		switch path {
		case "go.sum":
			if s.Counted || s.Points != 0 {
				t.Errorf("go.sum must not count: %+v", s)
			}
		case "internal/auth/login.go":
			if !s.Counted || !s.Sensitive || s.Points != 36 || s.Path != path {
				t.Errorf("auth %+v", s)
			}
		case "docs/a.md", "old.go":
			if !s.Counted || s.Sensitive || s.Path != path {
				t.Errorf("%s %+v", path, s)
			}
		default:
			t.Errorf("unexpected path %q", path)
		}
	}
}

// A patch series touching one path twice lists it twice; each entry keeps
// its own score, and the commit-message pseudo-file never counts.
func TestAssess_DuplicatePathsAndCommitMessage(t *testing.T) {
	one := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,2 @@\n x\n+y\n"
	two := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,3 @@\n x\n+if a {\n+}\n"
	files := append(parse(t, one), parse(t, two)...)
	msg := "COMMIT_MSG"
	files = append([]model.DiffFile{{NewPath: &msg, IsCommitMessage: true, Hunks: []model.DiffHunk{{Lines: []model.DiffLine{{Origin: model.OriginAddition, Content: "if if if"}}}}}}, files...)

	a := Assess(files, DefaultPolicy())
	if a.Files[0].Counted {
		t.Error("commit message counted")
	}
	if a.Files[1].Points != 1 || a.Files[2].Points != 7 {
		t.Errorf("per-entry points %v, %v; want 1 and 7", a.Files[1].Points, a.Files[2].Points)
	}
	if want := vrisk.Combine([]vrisk.FileRisk{a.Files[1].FileRisk, a.Files[2].FileRisk}, vrisk.DefaultPolicy()); a.Score != want {
		t.Errorf("score %d, want %d", a.Score, want)
	}
}

// A line mrman synthesized has no Raw; its Content still counts.
func TestAssess_SynthesizedLines(t *testing.T) {
	path := "a.go"
	files := []model.DiffFile{{NewPath: &path, Hunks: []model.DiffHunk{{Lines: []model.DiffLine{
		{Origin: model.OriginAddition, Content: "if a {"},
		{Origin: model.OriginContext, Content: "x"},
	}}}}}
	if got := Assess(files, DefaultPolicy()).Files[0].Points; got != 6 {
		t.Errorf("points %v, want 6 (1 changed + 5 for one decision point)", got)
	}
}

func TestAssess_Empty(t *testing.T) {
	a := Assess(nil, DefaultPolicy())
	if a.Score != 0 || a.Band != BandLow || len(a.Files) != 0 {
		t.Errorf("empty diff %+v", a)
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolvePolicy(t *testing.T) {
	repo := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "risk.json")
	writeFile(t, cfg, `{"high_threshold": 50}`)

	// Nothing configured, no repo file: the defaults, silently.
	p, w := ResolvePolicy(repo, "")
	if p.Source != SourceDefault || p.Path != "" || len(w) != 0 {
		t.Errorf("defaults: %+v %v", p, w)
	}

	// The config file applies when the repo has none.
	p, w = ResolvePolicy(repo, cfg)
	if p.Source != SourceConfig || p.Path != cfg || p.Band(55) != BandHigh || len(w) != 0 {
		t.Errorf("config: %+v %v", p, w)
	}

	// The repository's own policy wins.
	repoPolicy := filepath.Join(repo, ".vizzini", "risk.json")
	writeFile(t, repoPolicy, `{"high_threshold": 90}`)
	p, w = ResolvePolicy(repo, cfg)
	if p.Source != SourceRepo || p.Path != repoPolicy || p.Band(85) != BandMedium || len(w) != 0 {
		t.Errorf("repo: %+v %v", p, w)
	}

	// A broken repo policy warns and falls through to the config.
	writeFile(t, repoPolicy, `{"approve_all": true}`)
	p, w = ResolvePolicy(repo, cfg)
	if p.Source != SourceConfig || len(w) != 1 || !strings.Contains(w[0], repoPolicy) {
		t.Errorf("broken repo: %+v %v", p, w)
	}

	// A configured file that is missing warns; the defaults apply.
	p, w = ResolvePolicy("", filepath.Join(t.TempDir(), "nope.json"))
	if p.Source != SourceDefault || len(w) != 1 || !strings.Contains(w[0], "nope.json") {
		t.Errorf("missing config: %+v %v", p, w)
	}

	// A malformed glob is caught at load, not at score time.
	writeFile(t, cfg, `{"weights": [{"pattern": "[", "weight": 1}]}`)
	p, w = ResolvePolicy("", cfg)
	if p.Source != SourceDefault || len(w) != 1 {
		t.Errorf("bad glob: %+v %v", p, w)
	}
}

func TestReadReportAndStale(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "review.json")
	writeFile(t, good, `{"apiVersion":"vizzini.infrashift.io/v1","kind":"Review","summary":"s",
		"risk_score":72,"base_risk_score":23,"model_risk_score":72,"auto_approve":false,"findings":[],"chunks":1}`)
	r, err := ReadReport(good)
	if err != nil {
		t.Fatal(err)
	}
	if r.ModelRiskScore != 72 || r.Divergence() != 49 {
		t.Errorf("report %+v", r)
	}
	a := Assess(parse(t, patch), DefaultPolicy())
	if a.Score != 23 {
		t.Fatalf("fixture score %d, want 23", a.Score)
	}
	if Stale(r, &a) {
		t.Error("a report for this diff is not stale")
	}
	a.Score = 30
	if !Stale(r, &a) {
		t.Error("a report for another diff is stale")
	}

	if _, err := ReadReport(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("missing report accepted")
	}
	bad := filepath.Join(dir, "risk.json")
	writeFile(t, bad, `{"apiVersion":"vizzini.infrashift.io/v1","kind":"RiskAssessment","score":5,"band":"low","files":[]}`)
	if _, err := ReadReport(bad); !errors.Is(err, vrisk.ErrNotReviewReport) {
		t.Errorf("err = %v, want ErrNotReviewReport", err)
	}
	huge := filepath.Join(dir, "huge.json")
	writeFile(t, huge, strings.Repeat(" ", maxReportBytes+1))
	if _, err := ReadReport(huge); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("oversized report: %v", err)
	}
}

// Only this package may import Vizzini, and the binary must not link its
// LLM stack: risk is an optional extra, not a reason for mrman to carry a
// model client.
func TestVizziniStaysBehindThisPackage(t *testing.T) {
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	run := func(args ...string) string {
		cmd := exec.CommandContext(t.Context(), gobin, args...)
		cmd.Dir = "../.."
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go %v: %v", args, err)
		}
		return string(out)
	}

	const vizzini = "github.com/infrashift/vizzini-pr-reviewer"
	for line := range strings.SplitSeq(run("list", "-f", "{{.ImportPath}} {{join .Imports \" \"}}", "./..."), "\n") {
		pkg, imports, _ := strings.Cut(line, " ")
		if pkg == "github.com/infrashift/mrman/internal/risk" {
			for imp := range strings.FieldsSeq(imports) {
				// Only Vizzini's LLM-free, dependency-free packages: pkg/risk and
				// pkg/marker (the bot's comment format).
				if strings.HasPrefix(imp, vizzini) && imp != vizzini+"/pkg/risk" && imp != vizzini+"/pkg/marker" {
					t.Errorf("internal/risk imports %s; only pkg/risk and pkg/marker are LLM-free", imp)
				}
			}
			continue
		}
		if strings.Contains(" "+imports, " "+vizzini) {
			t.Errorf("%s imports Vizzini directly; go through internal/risk", pkg)
		}
	}

	for dep := range strings.SplitSeq(run("list", "-deps", "."), "\n") {
		for _, banned := range []string{"github.com/sashabaranov/go-openai", "github.com/pkoukk/tiktoken-go", vizzini + "/internal/llm", vizzini + "/pkg/reviewer"} {
			if dep == banned || strings.HasPrefix(dep, banned+"/") {
				t.Errorf("mrman links %s", dep)
			}
		}
	}
}
