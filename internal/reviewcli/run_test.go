package reviewcli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
)

func line(n uint32) *uint32 { return &n }

func TestBuildCommentTargetForms(t *testing.T) {
	// Review by default.
	target, err := buildCommentTarget("", nil, nil, "new")
	if err != nil || target.Kind != TargetReview {
		t.Fatalf("got %+v, %v", target, err)
	}
	// File.
	target, err = buildCommentTarget("a.go", nil, nil, "new")
	if err != nil || target.Kind != TargetFile || target.Path != "a.go" {
		t.Fatalf("got %+v, %v", target, err)
	}
	// Line.
	target, err = buildCommentTarget("a.go", line(10), nil, "old")
	if err != nil || target.Kind != TargetLine || target.Line != 10 || target.Side != model.LineSideOld {
		t.Fatalf("got %+v, %v", target, err)
	}
	// Range — reversed bounds normalize (tuicr parity: 12,10 → 10..12).
	target, err = buildCommentTarget("a.go", line(12), line(10), "old")
	if err != nil || target.Kind != TargetLineRange ||
		target.Range.Start != 10 || target.Range.End != 12 {
		t.Fatalf("got %+v, %v", target, err)
	}
}

func TestBuildCommentTargetErrors(t *testing.T) {
	cases := []struct {
		file       string
		line, end  *uint32
		side       string
		wantSubstr string
	}{
		{"a.go", line(0), nil, "new", "--line must be greater than zero"},
		{"a.go", line(1), line(0), "new", "--end-line must be greater than zero"},
		{"", line(5), nil, "new", "--line requires --target-file"},
		{"", nil, line(5), "new", "--end-line requires --line and --target-file"},
		{"a.go", nil, line(5), "new", "--end-line requires --line"},
		{"a.go", line(1), nil, "sideways", "unknown side"},
	}
	for _, c := range cases {
		_, err := buildCommentTarget(c.file, c.line, c.end, c.side)
		if err == nil || !strings.Contains(err.Error(), c.wantSubstr) {
			t.Errorf("file=%q line=%v end=%v side=%q: err=%v, want %q",
				c.file, c.line, c.end, c.side, err, c.wantSubstr)
		}
	}
}

func TestBuildAddRequestFromJSONPayload(t *testing.T) {
	opts := Options{
		Input: `{"content": "from json", "type": "issue", "username": "Claude",
		         "target": {"file": "a.go", "start_line": 3, "end_line": 5, "side": "old"}}`,
	}
	target, content, commentType, username, err := buildAddRequest(opts)
	if err != nil {
		t.Fatal(err)
	}
	if content != "from json" || commentType != "issue" || username != "Claude" {
		t.Fatalf("got content=%q type=%q user=%q", content, commentType, username)
	}
	if target.Kind != TargetLineRange || target.Range.Start != 3 || target.Range.End != 5 ||
		target.Side != model.LineSideOld {
		t.Fatalf("got %+v", target)
	}
}

func TestBuildAddRequestJSONOverridesFlags(t *testing.T) {
	opts := Options{
		Comment: "flag content", Type: "note", TargetFile: "flag.go", Line: 1,
		Input: `{"content": "json content", "comment_type": "praise", "file": "json.go", "line": 9}`,
	}
	target, content, commentType, _, err := buildAddRequest(opts)
	if err != nil {
		t.Fatal(err)
	}
	if content != "json content" || commentType != "praise" {
		t.Fatalf("JSON must override flags: content=%q type=%q", content, commentType)
	}
	if target.Kind != TargetLine || target.Path != "json.go" || target.Line != 9 {
		t.Fatalf("got %+v", target)
	}
}

func TestBuildAddRequestInferredTargetTypes(t *testing.T) {
	cases := []struct {
		payload string
		want    TargetKind
	}{
		{`{"content":"x","target":{}}`, TargetReview},
		{`{"content":"x","target":{"file":"a.go"}}`, TargetFile},
		{`{"content":"x","target":{"file":"a.go","line":2}}`, TargetLine},
		{`{"content":"x","target":{"file":"a.go","line":2,"end_line":4}}`, TargetLineRange},
		{`{"content":"x","target":{"type":"line-range","file":"a.go","start_line":2,"end_line":4}}`, TargetLineRange},
	}
	for _, c := range cases {
		target, _, _, _, err := buildAddRequest(Options{Input: c.payload})
		if err != nil {
			t.Errorf("%s: %v", c.payload, err)
			continue
		}
		if target.Kind != c.want {
			t.Errorf("%s: kind = %v, want %v", c.payload, target.Kind, c.want)
		}
	}
}

func TestBuildAddRequestRejects(t *testing.T) {
	// No content anywhere.
	if _, _, _, _, err := buildAddRequest(Options{Input: `{"type":"note"}`}); err == nil {
		t.Error("missing content must error")
	}
	// Bad JSON.
	if _, _, _, _, err := buildAddRequest(Options{Input: `{nope`}); err == nil {
		t.Error("bad JSON must error")
	}
	// Unknown target type.
	if _, _, _, _, err := buildAddRequest(Options{Input: `{"content":"x","target":{"type":"galaxy"}}`}); err == nil {
		t.Error("unknown target type must error")
	}
	// Line target without file.
	if _, _, _, _, err := buildAddRequest(Options{Input: `{"content":"x","target":{"type":"line","line":2}}`}); err == nil {
		t.Error("line target without file must error")
	}
}

func TestBuildAddRequestStdinInput(t *testing.T) {
	orig := stdinReader
	stdinReader = strings.NewReader(`{"content": "from stdin"}`)
	defer func() { stdinReader = orig }()
	_, content, _, _, err := buildAddRequest(Options{Input: "-"})
	if err != nil || content != "from stdin" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestAddCommentToSessionTargets(t *testing.T) {
	s := model.NewReviewSession("/repo", "abc", nil, model.SourceWorkingTree)
	s.AddFile("a.go", model.StatusModified, 1)

	// Review scope.
	c, err := AddCommentToSession(s, AddCommentRequest{
		Target: CommentTarget{Kind: TargetReview}, Content: "  review note  ",
		CommentType: model.CommentTypeFromID("note"), Author: "ryan",
	})
	if err != nil || c.Content != "review note" || c.Author != "ryan" {
		t.Fatalf("got %+v err=%v", c, err)
	}
	if len(s.ReviewComments) != 1 {
		t.Fatal("review comment not stored")
	}

	// Range scope keyed by end line.
	_, err = AddCommentToSession(s, AddCommentRequest{
		Target: CommentTarget{
			Kind: TargetLineRange, Path: "a.go",
			Range: model.NewLineRange(3, 7), Side: model.LineSideNew,
		},
		Content: "range", CommentType: model.CommentTypeFromID("issue"), Author: "ryan",
	})
	if err != nil {
		t.Fatal(err)
	}
	stored := s.File("a.go").LineComments[7]
	if len(stored) != 1 || stored[0].LineRange == nil || stored[0].LineRange.Start != 3 {
		t.Fatalf("range comment must be keyed by end line: %+v", stored)
	}

	// Unknown file rejected.
	_, err = AddCommentToSession(s, AddCommentRequest{
		Target:  CommentTarget{Kind: TargetFile, Path: "missing.go"},
		Content: "x", CommentType: model.CommentTypeFromID("note"), Author: "ryan",
	})
	var invalid *errs.InvalidInput
	if !errors.As(err, &invalid) {
		t.Fatalf("unknown file must yield InvalidInput, got %v", err)
	}

	// Empty content rejected.
	_, err = AddCommentToSession(s, AddCommentRequest{
		Target: CommentTarget{Kind: TargetReview}, Content: "   ",
		CommentType: model.CommentTypeFromID("note"), Author: "ryan",
	})
	if err == nil {
		t.Fatal("empty content must error")
	}
}

func TestCollectCommentsOrderingAndLocations(t *testing.T) {
	s := model.NewReviewSession("/repo", "abc", nil, model.SourceWorkingTree)
	s.AddFile("b.go", model.StatusModified, 1)
	s.AddFile("a.go", model.StatusModified, 1)

	s.ReviewComments = append(s.ReviewComments,
		model.NewComment("review scope", model.CommentTypeFromID("note"), nil))

	oldSide := model.LineSideOld
	s.File("b.go").AddFileComment(model.NewComment("file scope", model.CommentTypeFromID("note"), nil))
	s.File("b.go").AddLineComment(20, model.NewComment("line twenty", model.CommentTypeFromID("note"), nil))
	s.File("b.go").AddLineComment(5, model.NewComment("line five", model.CommentTypeFromID("issue"), &oldSide))
	rangeComment := model.NewCommentWithRange("ranged", model.CommentTypeFromID("note"), &oldSide, model.NewLineRange(8, 12))
	s.File("a.go").AddLineComment(12, rangeComment)

	out := collectComments(s)
	if len(out) != 5 {
		t.Fatalf("got %d comments", len(out))
	}
	// Order: review, then a.go (sorted first), then b.go file comment, then lines 5, 20.
	if out[0].Location != "review" || out[0].Path != nil {
		t.Fatalf("first must be review scope: %+v", out[0])
	}
	if out[1].Location != "a.go:8-12 [old]" {
		t.Fatalf("range location = %q", out[1].Location)
	}
	if out[1].StartLine == nil || *out[1].StartLine != 8 || *out[1].EndLine != 12 {
		t.Fatalf("range lines = %+v", out[1])
	}
	if out[2].Location != "b.go" || out[2].StartLine != nil {
		t.Fatalf("file comment = %+v", out[2])
	}
	if out[3].Location != "b.go:5 [old]" || out[4].Location != "b.go:20" {
		t.Fatalf("line order wrong: %q, %q", out[3].Location, out[4].Location)
	}
	if out[3].Side == nil || *out[3].Side != "old" {
		t.Fatalf("side = %v", out[3].Side)
	}
	if out[4].LifecycleState != "local_draft" {
		t.Fatalf("lifecycle = %q", out[4].LifecycleState)
	}
}

func TestResolveAuthorPrecedence(t *testing.T) {
	original := configUsername
	t.Cleanup(func() { configUsername = original })

	if got := resolveAuthor("  Claude  "); got != "Claude" {
		t.Errorf("explicit = %q", got)
	}
	configUsername = func() string { return "cfg-user" }
	if got := resolveAuthor("  "); got != "cfg-user" {
		t.Errorf("config fallback = %q", got)
	}
	configUsername = func() string { return "" }
	if got := resolveAuthor(""); got != model.DefaultAuthor {
		t.Errorf("default = %q", got)
	}
}

// TestResolveAuthorReadsConfigFile covers the wiring itself rather than the
// precedence: the seam above will happily report a config username that
// nothing ever reads from the config, which is exactly what it did while the
// default was a stub returning "".
func TestResolveAuthorReadsConfigFile(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	dir := filepath.Join(base, "mrman")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte("username = \"ryan.craig@example.com\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := resolveAuthor(""); got != "ryan.craig@example.com" {
		t.Errorf("resolveAuthor = %q, want the configured username", got)
	}
	// An explicit --username still wins over the file.
	if got := resolveAuthor("agent"); got != "agent" {
		t.Errorf("explicit author = %q, want agent", got)
	}
}

// TestResolveAuthorWithoutConfigFile keeps a missing or username-less config
// on the documented default instead of an empty author.
func TestResolveAuthorWithoutConfigFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	if got := resolveAuthor(""); got != model.DefaultAuthor {
		t.Errorf("resolveAuthor = %q, want %q", got, model.DefaultAuthor)
	}
}
