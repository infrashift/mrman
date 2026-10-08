package jj

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
)

// response is one canned jj invocation result.
type response struct {
	stdout string
	err    error
}

// fakeRunner responds to "arg1 arg2 ..." keys (jj is implied) with canned
// stdout and errors for any command without a canned response.
type fakeRunner struct {
	responses map[string]response
	calls     []string
	dirs      []string
}

func (r *fakeRunner) Run(dir, name string, args ...string) ([]byte, []byte, error) {
	if name != "jj" {
		return nil, nil, errors.New("unexpected command " + name)
	}
	key := strings.Join(args, " ")
	r.calls = append(r.calls, key)
	r.dirs = append(r.dirs, dir)
	resp, ok := r.responses[key]
	if !ok {
		return nil, []byte("no canned response for: " + key), errors.New("exit status 1")
	}
	if resp.err != nil {
		return nil, []byte(resp.stdout), resp.err
	}
	return []byte(resp.stdout), nil, nil
}

const (
	headKey             = "log -r @ --no-graph -T change_id.short()"
	bookmarkKey         = "log -r @ --no-graph -T bookmarks"
	ancestorBookmarkKey = "log -r heads(::@ & bookmarks()) --no-graph -T bookmarks --limit 1"
)

// baseResponses cans the discovery command set for a repo at root.
func baseResponses(root string) map[string]response {
	return map[string]response{
		"root":              {stdout: root + "\n"},
		headKey:             {stdout: "zxykpqrs\n"},
		bookmarkKey:         {stdout: ""},
		ancestorBookmarkKey: {stdout: ""},
	}
}

func discover(t *testing.T, run *fakeRunner) *Backend {
	t.Helper()
	b, err := Discover("/anywhere", vcs.WhitespaceNormal, run)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	return b
}

func canonTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func highlighter() *syntax.Highlighter {
	return syntax.NewHighlighter("monokai", "#1c3d1c", "#3d1c1c")
}

func TestDiscover(t *testing.T) {
	run := &fakeRunner{responses: baseResponses("/repo")}
	b := discover(t, run)
	info := b.Info()
	if info.RootPath != "/repo" {
		t.Fatalf("root = %q", info.RootPath)
	}
	if info.Type != vcs.TypeJujutsu {
		t.Fatalf("type = %q", info.Type)
	}
	if info.HeadCommit != "zxykpqrs" {
		t.Fatalf("head = %q", info.HeadCommit)
	}
	if info.BranchName != nil {
		t.Fatalf("branch = %v, want nil", *info.BranchName)
	}
	// `jj root` must run in the starting directory, not the root.
	if run.dirs[0] != "/anywhere" {
		t.Fatalf("root probe ran in %q", run.dirs[0])
	}
}

func TestDiscoverNotARepository(t *testing.T) {
	run := &fakeRunner{responses: map[string]response{}} // `jj root` fails
	if _, err := Discover("/anywhere", vcs.WhitespaceNormal, run); !errors.Is(err, errs.ErrNotARepository) {
		t.Fatalf("expected ErrNotARepository, got %v", err)
	}

	run = &fakeRunner{responses: map[string]response{"root": {stdout: "\n"}}}
	if _, err := Discover("/anywhere", vcs.WhitespaceNormal, run); !errors.Is(err, errs.ErrNotARepository) {
		t.Fatalf("empty root output: expected ErrNotARepository, got %v", err)
	}
}

func TestDiscoverHeadFallsBackToUnknown(t *testing.T) {
	responses := baseResponses("/repo")
	delete(responses, headKey) // head lookup fails
	b := discover(t, &fakeRunner{responses: responses})
	if b.Info().HeadCommit != "unknown" {
		t.Fatalf("head = %q, want unknown", b.Info().HeadCommit)
	}
}

func TestBookmarkOnCurrentRevision(t *testing.T) {
	responses := baseResponses("/repo")
	responses[bookmarkKey] = response{stdout: "my-feature\n"}
	run := &fakeRunner{responses: responses}
	b := discover(t, run)
	if b.Info().BranchName == nil || *b.Info().BranchName != "my-feature" {
		t.Fatalf("branch = %v, want my-feature", b.Info().BranchName)
	}
	if slices.Contains(run.calls, ancestorBookmarkKey) {
		t.Fatal("ancestor bookmark query must be skipped when @ has a bookmark")
	}
}

func TestBookmarkOnAncestorRevision(t *testing.T) {
	responses := baseResponses("/repo")
	responses[ancestorBookmarkKey] = response{stdout: "main\n"}
	b := discover(t, &fakeRunner{responses: responses})
	if b.Info().BranchName == nil || *b.Info().BranchName != "main" {
		t.Fatalf("branch = %v, want main", b.Info().BranchName)
	}
}

func TestBookmarkFiltersRemoteTracking(t *testing.T) {
	responses := baseResponses("/repo")
	responses[bookmarkKey] = response{stdout: "feat@origin feat\n"}
	b := discover(t, &fakeRunner{responses: responses})
	if got := *b.Info().BranchName; got != "feat" {
		t.Fatalf("branch = %q, want local bookmark feat", got)
	}

	// All-remote bookmarks fall back to the first token.
	responses = baseResponses("/repo")
	responses[bookmarkKey] = response{stdout: "only@origin\n"}
	b = discover(t, &fakeRunner{responses: responses})
	if got := *b.Info().BranchName; got != "only@origin" {
		t.Fatalf("branch = %q, want only@origin", got)
	}
}

func TestNoBookmarks(t *testing.T) {
	b := discover(t, &fakeRunner{responses: baseResponses("/repo")})
	if b.Info().BranchName != nil {
		t.Fatalf("expected no bookmark, got %v", *b.Info().BranchName)
	}
}

const helloDiff = "diff --git a/hello.txt b/hello.txt\n" +
	"index 1111111..2222222 100644\n" +
	"--- a/hello.txt\n" +
	"+++ b/hello.txt\n" +
	"@@ -1,1 +1,2 @@\n" +
	" hello world\n" +
	"+modified line\n"

func TestWorkingTreeDiff(t *testing.T) {
	responses := baseResponses("/repo")
	responses["diff --git"] = response{stdout: helloDiff}
	b := discover(t, &fakeRunner{responses: responses})

	files, err := b.WorkingTreeDiff(highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files", len(files))
	}
	if files[0].NewPath == nil || *files[0].NewPath != "hello.txt" {
		t.Fatalf("path = %v", files[0].NewPath)
	}
	if files[0].Status != model.StatusModified {
		t.Fatalf("status = %q", files[0].Status)
	}
}

func TestWorkingTreeDiffNoChanges(t *testing.T) {
	responses := baseResponses("/repo")
	responses["diff --git"] = response{stdout: "\n"}
	b := discover(t, &fakeRunner{responses: responses})
	if _, err := b.WorkingTreeDiff(highlighter()); !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("expected ErrNoChanges, got %v", err)
	}
}

func TestWorkingTreeDiffCommandFailure(t *testing.T) {
	b := discover(t, &fakeRunner{responses: baseResponses("/repo")}) // no diff response
	_, err := b.WorkingTreeDiff(highlighter())
	if _, ok := errors.AsType[*errs.VcsCommand](err); !ok {
		t.Fatalf("expected VcsCommand error, got %v", err)
	}
}

func TestWorkingTreeDiffIgnoresWhitespaceFlag(t *testing.T) {
	responses := baseResponses("/repo")
	responses["diff --ignore-all-space --git"] = response{stdout: helloDiff}
	run := &fakeRunner{responses: responses}
	b, err := Discover("/anywhere", vcs.WhitespaceIgnoreAll, run)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.WorkingTreeDiff(highlighter()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(run.calls, "diff --ignore-all-space --git") {
		t.Fatalf("whitespace flag not injected; calls = %v", run.calls)
	}
}

func TestDiffSurfacesNoopFileWhenWhitespaceOnlyDiffIsEmpty(t *testing.T) {
	// With --ignore-all-space, a whitespace-only edit produces a file header
	// with no hunks; the file must still surface (empty hunks, not an error).
	headerOnly := "diff --git a/hello.txt b/hello.txt\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/hello.txt\n" +
		"+++ b/hello.txt\n"
	responses := baseResponses("/repo")
	responses["diff --ignore-all-space --git"] = response{stdout: headerOnly}
	run := &fakeRunner{responses: responses}
	b, err := Discover("/anywhere", vcs.WhitespaceIgnoreAll, run)
	if err != nil {
		t.Fatal(err)
	}
	files, err := b.WorkingTreeDiff(highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || len(files[0].Hunks) != 0 {
		t.Fatalf("expected one no-op file, got %+v", files)
	}
}

func TestFetchContextLines(t *testing.T) {
	root := canonTempDir(t)
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello world\nmodified line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := discover(t, &fakeRunner{responses: baseResponses(root)})

	lines, err := b.FetchContextLines("hello.txt", model.StatusModified, nil, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0].Content != "hello world" || lines[1].Content != "modified line" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestFetchContextLinesFromRevisions(t *testing.T) {
	responses := baseResponses("/repo")
	responses["file show -r abc123 root-file:\"x.txt\""] = response{stdout: "l1\nl2\nl3\n"}
	responses["file show -r @- root-file:\"gone.txt\""] = response{stdout: "old1\nold2\n"}
	b := discover(t, &fakeRunner{responses: responses})

	ref := "abc123"
	lines, err := b.FetchContextLines("x.txt", model.StatusModified, &ref, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0].Content != "l2" {
		t.Fatalf("ref-commit lines = %+v", lines)
	}

	// Deleted files read from @-.
	lines, err = b.FetchContextLines("gone.txt", model.StatusDeleted, nil, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[1].Content != "old2" {
		t.Fatalf("deleted-file lines = %+v", lines)
	}

	// Invalid ranges short-circuit without running commands.
	if lines, err := b.FetchContextLines("x.txt", model.StatusModified, &ref, 0, 2); err != nil || lines != nil {
		t.Fatalf("start=0: got %v, %v", lines, err)
	}
	if lines, err := b.FetchContextLines("x.txt", model.StatusModified, &ref, 5, 2); err != nil || lines != nil {
		t.Fatalf("start>end: got %v, %v", lines, err)
	}
}

func TestFileLineCount(t *testing.T) {
	root := canonTempDir(t)
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	responses := baseResponses(root)
	responses["file show -r abc123 root-file:\"x.txt\""] = response{stdout: "l1\nl2\n"}
	b := discover(t, &fakeRunner{responses: responses})

	if n, err := b.FileLineCount("hello.txt", model.StatusModified, nil); err != nil || n != 3 {
		t.Fatalf("workdir count = %d, %v", n, err)
	}
	ref := "abc123"
	if n, err := b.FileLineCount("x.txt", model.StatusModified, &ref); err != nil || n != 2 {
		t.Fatalf("ref count = %d, %v", n, err)
	}
	if _, err := b.FileLineCount("missing.txt", model.StatusModified, nil); err == nil {
		t.Fatal("missing workdir file must error")
	}
}

func TestResolveRevisionRange(t *testing.T) {
	responses := baseResponses("/repo")
	responses[`log -r main..@ --no-graph -T commit_id ++ "\n"`] = response{stdout: "ccc\nbbb\naaa\n"}
	b := discover(t, &fakeRunner{responses: responses})

	rng, err := b.ResolveRevisionRange("main..@")
	if err != nil {
		t.Fatal(err)
	}
	// jj log outputs newest first; the range must be oldest-first.
	want := []string{"aaa", "bbb", "ccc"}
	if !slices.Equal(rng.CommitIDs, want) {
		t.Fatalf("ids = %v, want %v", rng.CommitIDs, want)
	}
	if rng.Target.Explicit {
		t.Fatal("commit-list target must not be explicit")
	}
}

func TestResolveRevisionRangeEmpty(t *testing.T) {
	responses := baseResponses("/repo")
	responses[`log -r none() --no-graph -T commit_id ++ "\n"`] = response{stdout: "\n"}
	b := discover(t, &fakeRunner{responses: responses})
	if _, err := b.ResolveRevisionRange("none()"); !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("expected ErrNoChanges, got %v", err)
	}
}

// record builds one commitTemplate output record.
func record(id, short, desc, email, ts string) string {
	return id + "\x00" + short + "\x00" + desc + "\x00" + email + "\x00" + ts + "\x01"
}

func TestRecentCommits(t *testing.T) {
	out := record("full3", "s3", "Third commit", "a@example.com", "2024-01-17T10:30:00.000-05:00") +
		record("full2", "s2", "Second commit\n\nWith a body\nsecond line", "a@example.com", "2024-01-16T10:30:00.000-05:00") +
		record("full1", "s1", "First commit", "b@example.com", "not-a-timestamp") +
		"bad\x00record\x01" // too few fields: skipped
	responses := baseResponses("/repo")
	responses["log -r ::@ --limit 5 --no-graph -T "+commitTemplate] = response{stdout: out}
	b := discover(t, &fakeRunner{responses: responses})

	commits, err := b.RecentCommits(0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 3 {
		t.Fatalf("got %d commits", len(commits))
	}
	if commits[0].ID != "full3" || commits[0].ShortID != "s3" || commits[0].Summary != "Third commit" {
		t.Fatalf("first commit = %+v", commits[0])
	}
	if commits[0].Body != nil {
		t.Fatalf("body = %v, want nil", *commits[0].Body)
	}
	wantTime := time.Date(2024, 1, 17, 15, 30, 0, 0, time.UTC)
	if !commits[0].Time.Equal(wantTime) {
		t.Fatalf("time = %v, want %v", commits[0].Time, wantTime)
	}
	if commits[1].Body == nil || *commits[1].Body != "With a body\nsecond line" {
		t.Fatalf("body = %v", commits[1].Body)
	}
	// Unparsable timestamps fall back to now.
	if time.Since(commits[2].Time) > time.Minute {
		t.Fatalf("fallback time = %v", commits[2].Time)
	}
	if commits[2].Author != "b@example.com" {
		t.Fatalf("author = %q", commits[2].Author)
	}
}

func TestRecentCommitsOffset(t *testing.T) {
	out := record("full2", "s2", "Second", "a@x.com", "2024-01-16T10:30:00Z") +
		record("full1", "s1", "First", "a@x.com", "2024-01-15T10:30:00Z")
	responses := baseResponses("/repo")
	responses["log -r ::@ --limit 3 --no-graph -T "+commitTemplate] = response{stdout: out}
	b := discover(t, &fakeRunner{responses: responses})

	commits, err := b.RecentCommits(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || commits[0].ID != "full1" {
		t.Fatalf("offset skip wrong: %+v", commits)
	}

	// Offset past the available records yields an empty result.
	responses["log -r ::@ --limit 12 --no-graph -T "+commitTemplate] = response{stdout: out}
	commits, err = b.RecentCommits(10, 2)
	if err != nil || len(commits) != 0 {
		t.Fatalf("got %v, %v", commits, err)
	}
}

func TestCommitsInfo(t *testing.T) {
	out := record("bbb", "b", "B commit", "a@x.com", "2024-01-16T10:30:00Z") +
		record("aaa", "a", "A commit", "a@x.com", "2024-01-15T10:30:00Z")
	responses := baseResponses("/repo")
	responses["log -r aaa | bbb | missing --no-graph -T "+commitTemplate] = response{stdout: out}
	run := &fakeRunner{responses: responses}
	b := discover(t, run)

	commits, err := b.CommitsInfo([]string{"aaa", "bbb", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	// Input order preserved, unknown IDs skipped.
	if len(commits) != 2 || commits[0].ID != "aaa" || commits[1].ID != "bbb" {
		t.Fatalf("commits = %+v", commits)
	}

	// Empty input returns empty without running jj.
	before := len(run.calls)
	commits, err = b.CommitsInfo(nil)
	if err != nil || len(commits) != 0 {
		t.Fatalf("got %v, %v", commits, err)
	}
	if len(run.calls) != before {
		t.Fatal("empty input must not invoke jj")
	}
}

func TestCommitRangeDiff(t *testing.T) {
	responses := baseResponses("/repo")
	responses["diff --from aaa- --to ccc --git"] = response{stdout: helloDiff}
	run := &fakeRunner{responses: responses}
	b := discover(t, run)

	rng := vcs.ResolvedRevisionRange{CommitIDs: []string{"aaa", "bbb", "ccc"}}
	files, err := b.CommitRangeDiff(rng, highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || *files[0].NewPath != "hello.txt" {
		t.Fatalf("files = %+v", files)
	}
	if !slices.Contains(run.calls, "diff --from aaa- --to ccc --git") {
		t.Fatalf("diff range args wrong; calls = %v", run.calls)
	}
}

func TestCommitRangeDiffEmpty(t *testing.T) {
	b := discover(t, &fakeRunner{responses: baseResponses("/repo")})
	if _, err := b.CommitRangeDiff(vcs.ResolvedRevisionRange{}, highlighter()); !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("empty ids: expected ErrNoChanges, got %v", err)
	}

	responses := baseResponses("/repo")
	responses["diff --from aaa- --to aaa --git"] = response{stdout: ""}
	b = discover(t, &fakeRunner{responses: responses})
	rng := vcs.ResolvedRevisionRange{CommitIDs: []string{"aaa"}}
	if _, err := b.CommitRangeDiff(rng, highlighter()); !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("empty diff: expected ErrNoChanges, got %v", err)
	}
}

func TestWorkingTreeWithCommitsDiff(t *testing.T) {
	responses := baseResponses("/repo")
	responses["diff --from aaa- --to @ --git"] = response{stdout: helloDiff}
	run := &fakeRunner{responses: responses}
	b := discover(t, run)

	files, err := b.WorkingTreeWithCommitsDiff([]string{"aaa", "bbb"}, highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files", len(files))
	}
	if !slices.Contains(run.calls, "diff --from aaa- --to @ --git") {
		t.Fatalf("diff args wrong; calls = %v", run.calls)
	}

	if _, err := b.WorkingTreeWithCommitsDiff(nil, highlighter()); !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("empty ids: expected ErrNoChanges, got %v", err)
	}
}

func TestStagingOperationsAreUnsupported(t *testing.T) {
	b := discover(t, &fakeRunner{responses: baseResponses("/repo")})
	if _, err := b.StagedDiff(nil); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("StagedDiff must be unsupported")
	}
	if _, err := b.UnstagedDiff(nil); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("UnstagedDiff must be unsupported")
	}
	if _, err := b.ChangeStatus(); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("ChangeStatus must be unsupported")
	}
	if _, err := b.ListChangedPaths(vcs.ChangeStaged); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("ListChangedPaths must be unsupported")
	}
	if err := b.StageFile("x"); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("StageFile must be unsupported")
	}
}

func TestParseDescription(t *testing.T) {
	cases := []struct {
		desc     string
		summary  string
		wantBody *string
	}{
		{"", "(no message)", nil},
		{"Just a summary", "Just a summary", nil},
		{"Just a summary\n", "Just a summary", nil},
		{"Sum\n\n\n", "Sum", nil},
		{"Sum\n\nBody line1\nline2", "Sum", new("Body line1\nline2")},
		{"Sum\nBody without blank", "Sum", new("Body without blank")},
	}
	for _, tc := range cases {
		summary, body := parseDescription(tc.desc)
		if summary != tc.summary {
			t.Fatalf("%q: summary = %q, want %q", tc.desc, summary, tc.summary)
		}
		switch {
		case tc.wantBody == nil && body != nil:
			t.Fatalf("%q: body = %q, want nil", tc.desc, *body)
		case tc.wantBody != nil && (body == nil || *body != *tc.wantBody):
			t.Fatalf("%q: body = %v, want %q", tc.desc, body, *tc.wantBody)
		}
	}
}

const oldVue = "<template>\n  <div>{{ msg }}</div>\n</template>\n\n<script setup>\nimport { ref } from 'vue'\nconst msg = ref('hi')\nconst other = 1\n</script>\n"

const newVue = "<template>\n  <div>{{ msg }}</div>\n</template>\n\n<script setup>\nimport { ref } from 'vue'\nconst msg = ref('hello')\nconst other = 1\n</script>\n"

const vueDiff = "diff --git a/App.vue b/App.vue\n" +
	"index 1111111..2222222 100644\n" +
	"--- a/App.vue\n" +
	"+++ b/App.vue\n" +
	"@@ -4,6 +4,6 @@\n" +
	" \n" +
	" <script setup>\n" +
	" import { ref } from 'vue'\n" +
	"-const msg = ref('hi')\n" +
	"+const msg = ref('hello')\n" +
	" const other = 1\n"

func TestHighlightsVueScriptHunkUsingFullFileContext(t *testing.T) {
	root := canonTempDir(t)
	if err := os.WriteFile(filepath.Join(root, "App.vue"), []byte(newVue), 0o644); err != nil {
		t.Fatal(err)
	}

	batchTemplate := `"\n` + vcs.BatchBoundary + `\n" ++ path ++ "\n"`
	responses := baseResponses(root)
	responses["diff --git"] = response{stdout: vueDiff}
	responses["file show -r @- -T "+batchTemplate+" root-file:\"App.vue\""] = response{
		stdout: "\n" + vcs.BatchBoundary + "\nApp.vue\n" + oldVue,
	}
	b := discover(t, &fakeRunner{responses: responses})

	files, err := b.WorkingTreeDiff(highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files", len(files))
	}

	changed := 0
	for _, line := range files[0].Hunks[0].Lines {
		if line.Origin != model.OriginAddition && line.Origin != model.OriginDeletion {
			continue
		}
		changed++
		if len(line.HighlightedSpans) == 0 {
			t.Fatalf("vue line %q should be highlighted", line.Content)
		}
		uniqueFGs := map[string]bool{}
		for _, span := range line.HighlightedSpans {
			if span.Style.FG != "" {
				uniqueFGs[span.Style.FG] = true
			}
		}
		if len(uniqueFGs) < 2 {
			t.Fatalf("vue hunk line %q should have varied fg colors, got %v", line.Content, uniqueFGs)
		}
	}
	if changed == 0 {
		t.Fatal("expected change lines in hunk")
	}
}

func TestCommitRangeDiffHighlightsBothSidesFromRevisions(t *testing.T) {
	// With an explicit newest revision, both sides come from batch fetches
	// (no workdir reads).
	batchTemplate := `"\n` + vcs.BatchBoundary + `\n" ++ path ++ "\n"`
	responses := baseResponses("/repo")
	responses["diff --from aaa- --to ccc --git"] = response{stdout: vueDiff}
	responses["file show -r aaa- -T "+batchTemplate+" root-file:\"App.vue\""] = response{
		stdout: "\n" + vcs.BatchBoundary + "\nApp.vue\n" + oldVue,
	}
	responses["file show -r ccc -T "+batchTemplate+" root-file:\"App.vue\""] = response{
		stdout: "\n" + vcs.BatchBoundary + "\nApp.vue\n" + newVue,
	}
	b := discover(t, &fakeRunner{responses: responses})

	rng := vcs.ResolvedRevisionRange{CommitIDs: []string{"aaa", "ccc"}}
	files, err := b.CommitRangeDiff(rng, highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files", len(files))
	}
	for _, line := range files[0].Hunks[0].Lines {
		if line.Origin == model.OriginAddition || line.Origin == model.OriginDeletion {
			if len(line.HighlightedSpans) == 0 {
				t.Fatalf("changed vue line %q should be highlighted", line.Content)
			}
		}
	}
}

func TestContainerHighlightBatchFailurePropagates(t *testing.T) {
	root := canonTempDir(t)
	if err := os.WriteFile(filepath.Join(root, "App.vue"), []byte(newVue), 0o644); err != nil {
		t.Fatal(err)
	}
	responses := baseResponses(root)
	responses["diff --git"] = response{stdout: vueDiff} // batch fetch has no response → fails
	b := discover(t, &fakeRunner{responses: responses})

	_, err := b.WorkingTreeDiff(highlighter())
	if _, ok := errors.AsType[*errs.VcsCommand](err); !ok {
		t.Fatalf("expected VcsCommand error from batch fetch, got %v", err)
	}
}

func TestContainerHighlightSkipsWithoutContainerFiles(t *testing.T) {
	// Plain text diffs never trigger a batch fetch.
	responses := baseResponses("/repo")
	responses["diff --git"] = response{stdout: helloDiff}
	run := &fakeRunner{responses: responses}
	b := discover(t, run)
	if _, err := b.WorkingTreeDiff(highlighter()); err != nil {
		t.Fatal(err)
	}
	for _, call := range run.calls {
		if strings.HasPrefix(call, "file show") {
			t.Fatalf("unexpected batch fetch: %v", run.calls)
		}
	}
}

func TestRootFileQuotesFilesetSyntax(t *testing.T) {
	cases := map[string]string{
		"src/main.go":      `root-file:"src/main.go"`,
		"a (copy).txt":     `root-file:"a (copy).txt"`,
		`say "hi"|x&y~.md`: `root-file:"say \"hi\"|x&y~.md"`,
		`back\slash`:       `root-file:"back\\slash"`,
	}
	for in, want := range cases {
		if got := rootFile(in); got != want {
			t.Errorf("rootFile(%q) = %s, want %s", in, got, want)
		}
	}
}
