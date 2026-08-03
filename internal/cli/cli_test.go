package cli

import (
	"strings"
	"testing"
)

func TestParseDefaultIsTui(t *testing.T) {
	args, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if args.Command != CommandTui {
		t.Fatalf("Command = %v, want CommandTui", args.Command)
	}
}

func TestParseTuiFlags(t *testing.T) {
	args, err := Parse([]string{
		"-r", "main..HEAD", "--theme", "tokyo-night-storm", "--appearance", "dark",
		"-p", "internal/", "-w", "--stdout", "--repo-url", "https://github.com/o/r",
		"--forge", "forgejo",
	})
	if err != nil {
		t.Fatal(err)
	}
	o := args.Tui
	if o.Revisions != "main..HEAD" || o.Theme != "tokyo-night-storm" || o.Appearance != "dark" {
		t.Errorf("unexpected options: %+v", o)
	}
	if o.Path != "internal/" || !o.WorkingTree || !o.Stdout || o.Forge != "forgejo" {
		t.Errorf("unexpected options: %+v", o)
	}
	if o.RepoURL != "https://github.com/o/r" {
		t.Errorf("RepoURL = %q", o.RepoURL)
	}
}

func TestParseExplicitTuiSubcommand(t *testing.T) {
	args, err := Parse([]string{"tui", "-w"})
	if err != nil {
		t.Fatal(err)
	}
	if args.Command != CommandTui || !args.Tui.WorkingTree {
		t.Fatalf("got %+v", args)
	}
}

func TestParsePrTargets(t *testing.T) {
	for _, argv := range [][]string{
		{"pr", "125"},
		{"mr", "125"},
		{"tui", "pr", "125"},
		{"tui", "mr", "125"},
	} {
		args, err := Parse(argv)
		if err != nil {
			t.Fatalf("%v: %v", argv, err)
		}
		if args.Command != CommandPr || args.PrTarget != "125" {
			t.Fatalf("%v: got %+v", argv, args)
		}
	}
}

func TestParsePrRequiresTarget(t *testing.T) {
	if _, err := Parse([]string{"pr"}); err == nil {
		t.Fatal("expected an error for pr without a target")
	}
}

func TestParseConflictMatrix(t *testing.T) {
	conflicts := [][]string{
		{"--file", "x.go", "-p", "y"},
		{"--file", "x.go", "-r", "main..HEAD"},
		{"--file", "x.go", "-w"},
		{"--file", "x.go", "-A"},
		{"-A", "-p", "y"},
		{"-A", "-r", "main..HEAD"},
		{"-A", "-w"},
	}
	for _, argv := range conflicts {
		if _, err := Parse(argv); err == nil {
			t.Errorf("%v: expected a conflict error", argv)
		}
	}
}

func TestParseHyphenRevisions(t *testing.T) {
	for _, argv := range [][]string{
		{"-r", "-3"},
		{"--revisions", "-3"},
	} {
		args, err := Parse(argv)
		if err != nil {
			t.Fatalf("%v: %v", argv, err)
		}
		if args.Tui.Revisions != "-3" {
			t.Fatalf("%v: Revisions = %q, want -3", argv, args.Tui.Revisions)
		}
	}
}

func TestParseReviewList(t *testing.T) {
	args, err := Parse([]string{"review", "list", "--repo", "owner/repo", "--all"})
	if err != nil {
		t.Fatal(err)
	}
	if args.Command != CommandReviewList || args.Review.Repo != "owner/repo" || !args.Review.All {
		t.Fatalf("got %+v", args.Review)
	}
}

func TestParseReviewAdd(t *testing.T) {
	args, err := Parse([]string{
		"review", "add", "--session", "repo@main/worktree/abc1234",
		"--type", "issue", "--target-file", "a.go", "--line", "10", "--end-line", "12",
		"--side", "old", "--username", "ryan", "--", "-starts with hyphen",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := args.Review
	if args.Command != CommandReviewAdd || r.Session != "repo@main/worktree/abc1234" {
		t.Fatalf("got %+v", r)
	}
	if r.Type != "issue" || r.TargetFile != "a.go" || r.Line != 10 || r.EndLine != 12 {
		t.Fatalf("got %+v", r)
	}
	if r.Side != "old" || r.Username != "ryan" || r.Comment != "-starts with hyphen" {
		t.Fatalf("got %+v", r)
	}
}

func TestParseReviewAddRequiresContentOrInput(t *testing.T) {
	if _, err := Parse([]string{"review", "add", "--session", "s"}); err == nil {
		t.Fatal("expected an error without comment or --input")
	}
	args, err := Parse([]string{"review", "add", "--session", "s", "--input", "-"})
	if err != nil {
		t.Fatal(err)
	}
	if args.Review.Input != "-" {
		t.Fatalf("Input = %q", args.Review.Input)
	}
}

func TestParseReviewAddRequiresSession(t *testing.T) {
	if _, err := Parse([]string{"review", "add", "hello"}); err == nil {
		t.Fatal("expected an error without --session")
	}
}

func TestParseReviewComments(t *testing.T) {
	for _, sub := range []string{"comments", "get"} {
		args, err := Parse([]string{"review", sub, "--session", "s"})
		if err != nil {
			t.Fatalf("%s: %v", sub, err)
		}
		if args.Command != CommandReviewComments || args.Review.Session != "s" {
			t.Fatalf("%s: got %+v", sub, args)
		}
	}
}

func TestParseReviewRejectsTuiFlags(t *testing.T) {
	for _, argv := range [][]string{
		{"-w", "review", "list"},
		{"--theme", "dark", "review", "comments", "--session", "s"},
		{"--stdout", "review", "add", "--session", "s", "hi"},
	} {
		_, err := Parse(argv)
		if err == nil {
			t.Errorf("%v: expected TUI-flag rejection", argv)
			continue
		}
		if got := err.Error(); !strings.Contains(got, "cannot be used with") {
			t.Errorf("%v: error %q missing rejection message", argv, got)
		}
	}
}

func TestPreprocessArgsStopsAtTerminator(t *testing.T) {
	got := preprocessArgs([]string{"review", "add", "--", "-r", "not-a-flag"})
	want := []string{"review", "add", "--", "-r", "not-a-flag"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestParseDiffSubcommand(t *testing.T) {
	args, err := Parse([]string{"diff", "old.txt", "new.txt"})
	if err != nil {
		t.Fatal(err)
	}
	// A comparison is an ordinary review, so it resolves to CommandTui and
	// reaches ui.Run like every other local target.
	if args.Command != CommandTui {
		t.Errorf("Command = %v, want CommandTui", args.Command)
	}
	if args.Tui.DiffOld != "old.txt" || args.Tui.DiffNew != "new.txt" {
		t.Errorf("got (%q, %q), want (old.txt, new.txt)",
			args.Tui.DiffOld, args.Tui.DiffNew)
	}
}

func TestParseDiffRequiresTwoPaths(t *testing.T) {
	for _, argv := range [][]string{
		{"diff"},
		{"diff", "only-one.txt"},
		{"diff", "a.txt", "b.txt", "c.txt"},
	} {
		if _, err := Parse(argv); err == nil {
			t.Errorf("%v: expected an arity error", argv)
		}
	}
}

// TestParseDiffRejectsTargetFlags covers the guard that
// MarkFlagsMutuallyExclusive cannot express: the conflicting flags are
// persistent on the root, so a subcommand has to refuse them itself.
func TestParseDiffRejectsTargetFlags(t *testing.T) {
	for _, argv := range [][]string{
		{"diff", "a", "b", "--file", "x.go"},
		{"diff", "a", "b", "--patch", "s.mbox"},
		{"diff", "a", "b", "--patch-strip", "2"},
		{"diff", "a", "b", "-r", "main..HEAD"},
		{"diff", "a", "b", "-w"},
		{"diff", "a", "b", "-A"},
		{"diff", "a", "b", "-p", "src/"},
	} {
		if _, err := Parse(argv); err == nil {
			t.Errorf("%v: expected a conflict error", argv)
		}
	}
}

// Presentation flags say how to render a review, not which one to open, so
// they must stay legal alongside a comparison.
func TestParseDiffAllowsPresentationFlags(t *testing.T) {
	for _, argv := range [][]string{
		{"diff", "a", "b", "--theme", "dracula"},
		{"diff", "a", "b", "--appearance", "dark"},
		{"diff", "a", "b", "--stdout"},
	} {
		if _, err := Parse(argv); err != nil {
			t.Errorf("%v: unexpected error: %v", argv, err)
		}
	}
}
