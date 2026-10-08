package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

const modifiedDiff = `diff --git a/file.txt b/file.txt
index 1111111..2222222 100644
--- a/file.txt
+++ b/file.txt
@@ -1,2 +1,2 @@
 alpha
-beta
+gamma
`

const addedDiff = `diff --git a/staged.txt b/staged.txt
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/staged.txt
@@ -0,0 +1 @@
+staged changed
`

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkingTreeDiffWithUntracked(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "new.txt", "hello\n")
	backend, _ := newTestBackend(t, root, map[string]response{
		"git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary HEAD --": {stdout: modifiedDiff},
		"git sparse-checkout list":                    {err: exitError(1)},
		"git ls-files --others --exclude-standard -z": {stdout: "new.txt\x00"},
	})

	files, err := backend.WorkingTreeDiff(testHighlighter())
	if err != nil {
		t.Fatalf("WorkingTreeDiff failed: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	if files[0].Status != model.StatusModified || files[0].DisplayPath() != "file.txt" {
		t.Errorf("files[0] = %v %q", files[0].Status, files[0].DisplayPath())
	}

	untracked := files[1]
	if untracked.Status != model.StatusAdded {
		t.Errorf("untracked status = %v", untracked.Status)
	}
	if untracked.OldPath == nil || *untracked.OldPath != "new.txt" ||
		untracked.NewPath == nil || *untracked.NewPath != "new.txt" {
		t.Errorf("untracked paths not normalized: %v %v", untracked.OldPath, untracked.NewPath)
	}
	if len(untracked.Hunks) != 1 {
		t.Fatalf("untracked hunks = %d", len(untracked.Hunks))
	}
	hunk := untracked.Hunks[0]
	if hunk.Header != "@@ -0,0 +1,1 @@" {
		t.Errorf("hunk header = %q", hunk.Header)
	}
	if hunk.OldStart != 0 || hunk.OldCount != 0 || hunk.NewStart != 1 || hunk.NewCount != 1 {
		t.Errorf("hunk spans = %d,%d %d,%d", hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount)
	}
	if len(hunk.Lines) != 1 {
		t.Fatalf("hunk lines = %d", len(hunk.Lines))
	}
	line := hunk.Lines[0]
	if line.Origin != model.OriginAddition || line.Content != "hello" {
		t.Errorf("line = %v %q", line.Origin, line.Content)
	}
	if line.NewLineno == nil || *line.NewLineno != 1 || line.OldLineno != nil {
		t.Errorf("line numbers = %v %v", line.OldLineno, line.NewLineno)
	}
	if untracked.ContentHash == 0 {
		t.Error("untracked content hash not computed")
	}
}

func TestWorkingTreeDiffNoChanges(t *testing.T) {
	backend, _ := newTestBackend(t, t.TempDir(), map[string]response{
		"git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary HEAD --": {},
		"git sparse-checkout list":                    {err: exitError(1)},
		"git ls-files --others --exclude-standard -z": {},
	})

	_, err := backend.WorkingTreeDiff(testHighlighter())

	if !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("err = %v, want ErrNoChanges", err)
	}
}

func TestWorkingTreeDiffIgnoresAllWhitespaceWhenConfigured(t *testing.T) {
	backend, runner := newTestBackend(t, t.TempDir(), map[string]response{
		"git diff --ignore-all-space --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary HEAD --": {},
		"git sparse-checkout list":                    {err: exitError(1)},
		"git ls-files --others --exclude-standard -z": {},
	})
	backend.whitespace = vcs.WhitespaceIgnoreAll

	_, err := backend.WorkingTreeDiff(testHighlighter())

	if !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("err = %v, want ErrNoChanges", err)
	}
	if !runner.called("git diff --ignore-all-space --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary HEAD --") {
		t.Error("--ignore-all-space flag missing from diff invocation")
	}
}

func TestWorkingTreeDiffCommandFailure(t *testing.T) {
	backend, _ := newTestBackend(t, t.TempDir(), map[string]response{
		"git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary HEAD --": {
			stderr: "fatal: bad revision 'HEAD'", err: exitError(128)},
	})

	_, err := backend.WorkingTreeDiff(testHighlighter())

	var vcsErr *errs.VcsCommand
	if !errors.As(err, &vcsErr) {
		t.Fatalf("err = %v, want *errs.VcsCommand", err)
	}
	if !strings.Contains(vcsErr.Detail, "failed:") || !strings.Contains(vcsErr.Detail, "fatal: bad revision 'HEAD'") {
		t.Errorf("detail = %q", vcsErr.Detail)
	}
}

func TestStagedDiff(t *testing.T) {
	backend, _ := newTestBackend(t, t.TempDir(), map[string]response{
		"git rev-parse --verify HEAD": {stdout: "abc\n"},
		"git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary --cached --": {stdout: addedDiff},
	})

	files, err := backend.StagedDiff(testHighlighter())
	if err != nil {
		t.Fatalf("StagedDiff failed: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files[0].Status != model.StatusAdded {
		t.Errorf("status = %v", files[0].Status)
	}
	if files[0].OldPath == nil || *files[0].OldPath != "staged.txt" {
		t.Errorf("added file old path not normalized: %v", files[0].OldPath)
	}
}

func TestStagedDiffUnbornHead(t *testing.T) {
	backend, _ := newTestBackend(t, t.TempDir(), map[string]response{
		"git rev-parse --verify HEAD": {stderr: "fatal: needed a single revision", err: exitError(128)},
		"git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary --cached --": {stdout: addedDiff},
	})

	files, err := backend.StagedDiff(testHighlighter())
	if err != nil {
		t.Fatalf("StagedDiff failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
}

func TestUnstagedDiff(t *testing.T) {
	backend, runner := newTestBackend(t, t.TempDir(), map[string]response{
		"git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary --": {stdout: modifiedDiff},
		"git sparse-checkout list":                    {err: exitError(1)},
		"git ls-files --others --exclude-standard -z": {},
	})

	files, err := backend.UnstagedDiff(testHighlighter())
	if err != nil {
		t.Fatalf("UnstagedDiff failed: %v", err)
	}

	if len(files) != 1 || files[0].DisplayPath() != "file.txt" {
		t.Fatalf("files = %+v", files)
	}
	if !runner.called("git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary --") {
		t.Error("unstaged diff argv not invoked")
	}
}

func TestBuildUntrackedDiffFileTooLarge(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, "huge.log")
	if err := os.WriteFile(full, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(full, maxUntrackedFileSize+1); err != nil {
		t.Fatal(err)
	}
	backend, _ := newTestBackend(t, root, nil)

	file := backend.buildUntrackedDiffFile("huge.log", testHighlighter())

	if file == nil || !file.IsTooLarge || file.IsBinary || len(file.Hunks) != 0 {
		t.Fatalf("file = %+v", file)
	}
	if file.Status != model.StatusAdded {
		t.Errorf("status = %v", file.Status)
	}
}

func TestBuildUntrackedDiffFileBinary(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "blob.bin", "abc\x00def")
	backend, _ := newTestBackend(t, root, nil)

	file := backend.buildUntrackedDiffFile("blob.bin", testHighlighter())

	if file == nil || !file.IsBinary || file.IsTooLarge || len(file.Hunks) != 0 {
		t.Fatalf("file = %+v", file)
	}
}

func TestBuildUntrackedDiffFileEmpty(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "empty.txt", "")
	backend, _ := newTestBackend(t, root, nil)

	file := backend.buildUntrackedDiffFile("empty.txt", testHighlighter())

	if file == nil || file.IsBinary || file.IsTooLarge || len(file.Hunks) != 0 {
		t.Fatalf("file = %+v", file)
	}
}

func TestBuildUntrackedDiffFileVanished(t *testing.T) {
	backend, _ := newTestBackend(t, t.TempDir(), nil)

	if file := backend.buildUntrackedDiffFile("gone.txt", testHighlighter()); file != nil {
		t.Fatalf("file = %+v, want nil", file)
	}
}

func TestBuildUntrackedDiffFileTabifiesAndStripsCarriageReturns(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "crlf.txt", "a\tb\r\n")
	backend, _ := newTestBackend(t, root, nil)

	file := backend.buildUntrackedDiffFile("crlf.txt", testHighlighter())

	if file == nil || len(file.Hunks) != 1 || len(file.Hunks[0].Lines) != 1 {
		t.Fatalf("file = %+v", file)
	}
	if got := file.Hunks[0].Lines[0].Content; got != "a    b" {
		t.Errorf("content = %q", got)
	}
}

func TestCommitRangeDiffCommitList(t *testing.T) {
	backend, runner := newTestBackend(t, t.TempDir(), map[string]response{
		"git rev-parse c1^": {stdout: "c0\n"},
		"git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary c0 c2 --": {stdout: modifiedDiff},
	})

	files, err := backend.CommitRangeDiff(vcs.ResolvedRevisionRange{
		CommitIDs: []string{"c1", "c2"},
	}, testHighlighter())
	if err != nil {
		t.Fatalf("CommitRangeDiff failed: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("got %d files", len(files))
	}
	if !runner.called("git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary c0 c2 --") {
		t.Error("commit list diff argv not invoked")
	}
}

func TestCommitRangeDiffCommitListRootCommit(t *testing.T) {
	backend, runner := newTestBackend(t, t.TempDir(), map[string]response{
		"git rev-parse c1^": {stderr: "fatal: bad revision", err: exitError(128)},
		"git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary " + emptyTreeOID + " c1 --": {stdout: modifiedDiff},
	})

	_, err := backend.CommitRangeDiff(vcs.ResolvedRevisionRange{
		CommitIDs: []string{"c1"},
	}, testHighlighter())
	if err != nil {
		t.Fatalf("CommitRangeDiff failed: %v", err)
	}

	if !runner.called("git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary " + emptyTreeOID + " c1 --") {
		t.Error("empty-tree base argv not invoked")
	}
}

func TestCommitRangeDiffExplicit(t *testing.T) {
	base := "b1"
	backend, runner := newTestBackend(t, t.TempDir(), map[string]response{
		"git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary b1 h1 --": {stdout: modifiedDiff},
	})

	_, err := backend.CommitRangeDiff(vcs.ResolvedRevisionRange{
		CommitIDs: []string{"h1"},
		Target:    vcs.RevisionDiffTarget{Explicit: true, Base: &base, Head: "h1"},
	}, testHighlighter())
	if err != nil {
		t.Fatalf("CommitRangeDiff failed: %v", err)
	}

	if !runner.called("git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary b1 h1 --") {
		t.Error("explicit base..head argv not invoked")
	}
}

func TestCommitRangeDiffExplicitNilBaseUsesEmptyTree(t *testing.T) {
	backend, runner := newTestBackend(t, t.TempDir(), map[string]response{
		"git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary " + emptyTreeOID + " h1 --": {stdout: modifiedDiff},
	})

	_, err := backend.CommitRangeDiff(vcs.ResolvedRevisionRange{
		CommitIDs: []string{"h1"},
		Target:    vcs.RevisionDiffTarget{Explicit: true, Head: "h1"},
	}, testHighlighter())
	if err != nil {
		t.Fatalf("CommitRangeDiff failed: %v", err)
	}

	if !runner.called("git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary " + emptyTreeOID + " h1 --") {
		t.Error("empty-tree explicit base argv not invoked")
	}
}

func TestCommitRangeDiffEmptyCommitList(t *testing.T) {
	backend, _ := newTestBackend(t, t.TempDir(), nil)

	_, err := backend.CommitRangeDiff(vcs.ResolvedRevisionRange{}, testHighlighter())

	if !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("err = %v, want ErrNoChanges", err)
	}
}

func TestWorkingTreeWithCommitsDiff(t *testing.T) {
	backend, runner := newTestBackend(t, t.TempDir(), map[string]response{
		"git rev-parse c5^": {stdout: "c4\n"},
		"git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary c4 --": {stdout: modifiedDiff},
		"git sparse-checkout list":                    {err: exitError(1)},
		"git ls-files --others --exclude-standard -z": {},
	})

	files, err := backend.WorkingTreeWithCommitsDiff([]string{"c5", "c6"}, testHighlighter())
	if err != nil {
		t.Fatalf("WorkingTreeWithCommitsDiff failed: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("got %d files", len(files))
	}
	if !runner.called("git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary c4 --") {
		t.Error("working-tree-with-commits argv not invoked")
	}
}

func TestWorkingTreeWithCommitsDiffEmptyIDs(t *testing.T) {
	backend, _ := newTestBackend(t, t.TempDir(), nil)

	_, err := backend.WorkingTreeWithCommitsDiff(nil, testHighlighter())

	if !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("err = %v, want ErrNoChanges", err)
	}
}

func TestNormalizeGitCliPaths(t *testing.T) {
	oldPath := "gone.txt"
	newPath := "new.txt"
	files := []model.DiffFile{
		{Status: model.StatusAdded, NewPath: &newPath},
		{Status: model.StatusDeleted, OldPath: &oldPath},
		{Status: model.StatusModified, OldPath: &oldPath, NewPath: &newPath},
	}

	normalizeGitCliPaths(files)

	if files[0].OldPath == nil || *files[0].OldPath != "new.txt" {
		t.Errorf("added old path = %v", files[0].OldPath)
	}
	if files[1].NewPath == nil || *files[1].NewPath != "gone.txt" {
		t.Errorf("deleted new path = %v", files[1].NewPath)
	}
	if *files[2].OldPath != "gone.txt" || *files[2].NewPath != "new.txt" {
		t.Error("modified paths should be untouched")
	}
}

const phpDiff = `diff --git a/page.php b/page.php
index 1111111..2222222 100644
--- a/page.php
+++ b/page.php
@@ -2,1 +2,1 @@
-echo 'old';
+echo 'new';
`

func TestContainerGrammarFullFileHighlight(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "page.php", "<?php\necho 'new';\n")
	backend, runner := newTestBackend(t, root, map[string]response{
		"git diff --no-color --src-prefix=a/ --dst-prefix=b/ --no-ext-diff --binary HEAD --": {stdout: phpDiff},
		"git sparse-checkout list":                    {err: exitError(1)},
		"git ls-files --others --exclude-standard -z": {},
		"git show HEAD:page.php":                      {stdout: "<?php\necho 'old';\n"},
	})

	highlighter := testHighlighter()
	files, err := backend.WorkingTreeDiff(highlighter)
	if err != nil {
		t.Fatalf("WorkingTreeDiff failed: %v", err)
	}

	if !runner.called("git show HEAD:page.php") {
		t.Fatal("old side was not fetched via git show")
	}
	if len(files) != 1 || len(files[0].Hunks) != 1 {
		t.Fatalf("files = %+v", files)
	}
	for _, line := range files[0].Hunks[0].Lines {
		if line.HighlightedSpans == nil {
			t.Errorf("line %q has no highlighted spans", line.Content)
			continue
		}
		wantBG := ""
		switch line.Origin {
		case model.OriginAddition:
			wantBG = "#144212"
		case model.OriginDeletion:
			wantBG = "#421212"
		case model.OriginContext:
		}
		if wantBG != "" && line.HighlightedSpans[0].Style.BG != wantBG {
			t.Errorf("line %q BG = %q, want %q", line.Content, line.HighlightedSpans[0].Style.BG, wantBG)
		}
	}
}

func TestReadPathFromSource(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "wd.txt", "workdir\n")
	backend, _ := newTestBackend(t, root, map[string]response{
		"git show :0:idx.txt":    {stdout: "index\n"},
		"git show rev1:rev.txt":  {stdout: "revision\n"},
		"git show rev1:miss.txt": {stderr: "fatal: path does not exist", err: exitError(128)},
	})

	if content, ok := backend.readPathFromSource(contentSource{kind: sourceWorkdir}, "wd.txt"); !ok || content != "workdir\n" {
		t.Errorf("workdir = %q %v", content, ok)
	}
	if content, ok := backend.readPathFromSource(contentSource{kind: sourceIndex}, "idx.txt"); !ok || content != "index\n" {
		t.Errorf("index = %q %v", content, ok)
	}
	if content, ok := backend.readPathFromSource(revisionSource("rev1"), "rev.txt"); !ok || content != "revision\n" {
		t.Errorf("revision = %q %v", content, ok)
	}
	if _, ok := backend.readPathFromSource(revisionSource("rev1"), "miss.txt"); ok {
		t.Error("missing object should not be ok")
	}
	if _, ok := backend.readPathFromSource(contentSource{kind: sourceNone}, "any.txt"); ok {
		t.Error("none source should not be ok")
	}
}

func TestSplitLines(t *testing.T) {
	if got := splitLines(""); got != nil {
		t.Errorf("splitLines(\"\") = %v", got)
	}
	got := splitLines("a\r\nb\nc")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("splitLines = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitLines[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
