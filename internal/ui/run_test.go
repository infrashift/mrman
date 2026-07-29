package ui

import (
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/cli"
	"github.com/infrashift/mrman/internal/model"
)

func TestFilterByPath(t *testing.T) {
	paths := []string{"src/a.go", "src/sub/b.go", "srcx/c.go", "README.md"}
	var files []model.DiffFile
	for i := range paths {
		files = append(files, model.DiffFile{NewPath: &paths[i]})
	}

	got := filterByPath(append([]model.DiffFile(nil), files...), "src")
	if len(got) != 2 {
		t.Fatalf("prefix src: got %d files", len(got))
	}
	got = filterByPath(append([]model.DiffFile(nil), files...), "src/")
	if len(got) != 2 {
		t.Fatalf("prefix src/: got %d files", len(got))
	}
	got = filterByPath(append([]model.DiffFile(nil), files...), "README.md")
	if len(got) != 1 || got[0].DisplayPath() != "README.md" {
		t.Fatalf("exact: got %v", got)
	}
	// srcx must NOT match the "src" prefix (directory-boundary check).
	for _, f := range filterByPath(append([]model.DiffFile(nil), files...), "src") {
		if f.DisplayPath() == "srcx/c.go" {
			t.Fatal("srcx must not match src prefix")
		}
	}
}

func TestSessionSourceMapping(t *testing.T) {
	cases := map[app.DiffSourceKind]model.SessionDiffSource{
		app.DiffSourceWorkingTree:       model.SourceWorkingTree,
		app.DiffSourceStaged:            model.SourceStaged,
		app.DiffSourceUnstaged:          model.SourceUnstaged,
		app.DiffSourceStagedAndUnstaged: model.SourceStagedAndUnstaged,
		app.DiffSourceCommitRange:       model.SourceCommitRange,
	}
	for kind, want := range cases {
		if got := sessionSource(app.DiffSource{Kind: kind}); got != want {
			t.Errorf("%v → %v, want %v", kind, got, want)
		}
	}
}

func TestReversed(t *testing.T) {
	got := reversed([]string{"a", "b", "c"})
	if got[0] != "c" || got[2] != "a" {
		t.Fatalf("got %v", got)
	}
	if len(reversed(nil)) != 0 {
		t.Fatal("nil input must yield empty")
	}
}

func TestResolveThemeFlagPaths(t *testing.T) {
	// Explicit theme flag wins.
	resolved, _, err := resolveTheme(cli.TuiOptions{Theme: "tokyo-night-storm"})
	if err != nil || resolved.Name != "tokyo-night-storm" {
		t.Fatalf("got %v err=%v", resolved, err)
	}
	// Unknown theme flag is a hard error (tuicr parity).
	if _, _, err := resolveTheme(cli.TuiOptions{Theme: "no-such"}); err == nil {
		t.Fatal("unknown theme must error")
	}
	// Bad appearance flag errors.
	if _, _, err := resolveTheme(cli.TuiOptions{Appearance: "purple"}); err == nil {
		t.Fatal("bad appearance must error")
	}
	// Default resolves via system detection (stubbed dark).
	resolved, _, err = resolveTheme(cli.TuiOptions{})
	if err != nil || resolved == nil {
		t.Fatalf("default resolve failed: %v", err)
	}
}

func TestHelpContentMentionsLeader(t *testing.T) {
	content := helpContent(',')
	joined := ""
	for _, l := range content {
		joined += l + "\n"
	}
	if !contains(joined, ",e") || !contains(joined, ",f") {
		t.Fatal("help must interpolate the leader key")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
