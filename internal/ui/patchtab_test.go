package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/app"
)

// inbox builds a directory of artifacts copied from the patch package's
// fixtures, plus whatever extra files the test names. Real files, because the
// scan reads and parses them — a fake would only test the fake.
func inbox(t *testing.T, fixtures ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range fixtures {
		data, err := os.ReadFile(filepath.Join("..", "patch", "testdata", name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScanDescribesArtifacts is what the tab shows: a reviewer scanning an
// inbox needs the subject and the series shape, not just a filename.
func TestScanDescribesArtifacts(t *testing.T) {
	dir := inbox(t, "format-patch-single.patch", "format-patch-series-3.mbox", "quilt-p1.diff")

	rows, err := scanPatchDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}

	byName := map[string]app.PatchRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}

	single := byName["format-patch-single.patch"]
	if single.Subject != "drivers/foo: guard the probe" {
		t.Errorf("subject = %q", single.Subject)
	}
	if !strings.Contains(single.Author, "Dev Eloper") {
		t.Errorf("author = %q", single.Author)
	}
	if single.Series != "" {
		t.Errorf("a lone patch should not claim a series, got %q", single.Series)
	}

	if got := byName["format-patch-series-3.mbox"].Series; got != "3 patches" {
		t.Errorf("series = %q, want %q", got, "3 patches")
	}

	// A bare diff carries no mail metadata; that is honest, not a failure.
	bare := byName["quilt-p1.diff"]
	if !bare.Reviewable() {
		t.Errorf("a bare diff should still be reviewable, got err %q", bare.Err)
	}
	if bare.Subject != "" {
		t.Errorf("a bare diff has no subject, got %q", bare.Subject)
	}
}

// TestScanIgnoresNonArtifacts keeps the inbox from filling with everything
// else in the directory.
func TestScanIgnoresNonArtifacts(t *testing.T) {
	dir := inbox(t, "format-patch-single.patch")
	write(t, dir, "notes.txt", "not a patch")
	write(t, dir, "README.md", "# hello")
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "subdir"), "nested.patch", "should not be found")

	rows, err := scanPatchDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "format-patch-single.patch" {
		t.Errorf("rows = %v, want just the patch file", rowNames(rows))
	}
}

// TestUnreadableArtifactIsListedWithItsReason is a deliberate choice: a file
// the reviewer expected to see and cannot open is worth showing, with the
// reason, rather than vanishing from the list.
func TestUnreadableArtifactIsListedWithItsReason(t *testing.T) {
	dir := t.TempDir()
	// Two distinct failures a reviewer will actually hit: something that is
	// not a patch at all, and a real mail message that carries no diff.
	write(t, dir, "prose.patch", "Hello,\n\nJust a note, no diff here.\n")
	write(t, dir, "cover.patch",
		"From x y\nSubject: [PATCH 0/3] a series\nFrom: a@b.c\n\nWhat the series does.\n")

	rows, err := scanPatchDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	byName := map[string]app.PatchRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	for name, wantSubstring := range map[string]string{
		"prose.patch": "not a patch",
		"cover.patch": "no diff",
	} {
		row := byName[name]
		if row.Reviewable() {
			t.Errorf("%s should not be reviewable", name)
		}
		if !strings.Contains(row.Err, wantSubstring) {
			t.Errorf("%s: err = %q, want it to mention %q", name, row.Err, wantSubstring)
		}
	}
}

func TestScanOfAMissingDirectoryErrors(t *testing.T) {
	if _, err := scanPatchDir(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("scanning a missing directory should error")
	}
	if _, err := scanPatchDir(""); err == nil {
		t.Error("scanning no directory at all should error")
	}
}

// TestOpeningAPatchSwapsTheReview is the end-to-end path: pick a row, get a
// review of it, with the backend and session swapped underneath.
func TestOpeningAPatchSwapsTheReview(t *testing.T) {
	dir := inbox(t, "format-patch-series-3.mbox")
	m := testModel(t)
	a := m.App

	a.SetPatchTabDir(dir)
	req, ok := a.TakePatchTabLoad()
	if !ok {
		t.Fatal("expected a scan request")
	}
	rows, err := scanPatchDir(req.Dir)
	if err != nil {
		t.Fatal(err)
	}
	a.ApplyPatchTabRows(req.Gen, rows)
	a.SetTargetTab(app.TargetTabPatches)

	m.openSelectedPatch()

	if a.DiffSource.Kind != app.DiffSourcePatch {
		t.Errorf("diff source = %v, want DiffSourcePatch", a.DiffSource.Kind)
	}
	if len(a.DiffFiles) == 0 {
		t.Fatal("opening should load the artifact's diff")
	}
	// The series presents as a commit strip, one row per patch.
	if len(a.ReviewCommits) != 3 {
		t.Errorf("commit strip has %d rows, want 3", len(a.ReviewCommits))
	}
	if a.Session.DiffSource != "patch" {
		t.Errorf("session source = %q, want patch", a.Session.DiffSource)
	}
	// And no dead expanders, since a patch has nothing to expand into.
	for i := range a.LineAnnotations {
		if k := a.LineAnnotations[i].Kind; k == app.AnnExpander || k == app.AnnHiddenLines {
			t.Fatal("a patch review should render no gap rows")
		}
	}
}

// TestOpeningAnUnreadablePatchReportsRatherThanCrashes covers pressing Enter
// on a row the scan already flagged.
func TestOpeningAnUnreadablePatchReportsRatherThanCrashes(t *testing.T) {
	m := testModel(t)
	a := m.App
	a.SetTargetTab(app.TargetTabPatches)
	a.TakePatchTabLoad()
	a.ApplyPatchTabRows(a.PatchTab.Gen, []app.PatchRow{
		{Name: "broken.patch", Path: "/nope/broken.patch", Err: "not a patch"},
	})

	before := a.DiffSource.Kind
	m.openSelectedPatch()

	if a.DiffSource.Kind != before {
		t.Error("an unreadable row must not swap the review")
	}
	if a.Message == nil || a.Message.Type != app.MessageError {
		t.Errorf("expected an error message, got %+v", a.Message)
	}
}

// TestShortAuthor reduces a From: line to what fits in a column.
func TestShortAuthor(t *testing.T) {
	cases := map[string]string{
		"Dev Eloper <dev@example.org>": "Dev Eloper",
		"<dev@example.org>":            "dev@example.org",
		"dev@example.org":              "dev@example.org",
		"":                             "",
	}
	for in, want := range cases {
		if got := shortAuthor(in); got != want {
			t.Errorf("shortAuthor(%q) = %q, want %q", in, got, want)
		}
	}
}

func rowNames(rows []app.PatchRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

// TestShortenDirKeepsTheTail covers the header: a deep inbox path is
// identified by its last components, and an untruncated one pushes the tab
// chips off screen.
func TestShortenDirKeepsTheTail(t *testing.T) {
	short := "/home/r/inbox"
	if got := shortenDir(short, 44); got != short {
		t.Errorf("a short path should pass through, got %q", got)
	}

	long := "/home/ryancraig/very/deeply/nested/workspace/patches/inbox"
	got := shortenDir(long, 24)
	if len(got) > 24 {
		t.Errorf("shortenDir(%q, 24) = %q, still too long", long, got)
	}
	if !strings.HasSuffix(got, "inbox") {
		t.Errorf("shortenDir kept the wrong end: %q", got)
	}
	if !strings.HasPrefix(got, "…") {
		t.Errorf("a truncated path should say so: %q", got)
	}

	// Even a single component longer than the budget must not overflow.
	if got := shortenDir("/"+strings.Repeat("x", 80), 10); len(got) > 10 {
		t.Errorf("clipped path is %d wide, want <= 10: %q", len(got), got)
	}
}
