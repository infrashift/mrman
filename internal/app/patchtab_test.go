package app

import (
	"testing"
	"time"
)

// rowsAt builds n rows, newest last so sorting has something to do.
func rowsAt(names ...string) []PatchRow {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]PatchRow, 0, len(names))
	for i, n := range names {
		rows = append(rows, PatchRow{
			Name:     n,
			Path:     "/inbox/" + n,
			Subject:  n + " subject",
			Author:   "Dev Eloper <dev@example.org>",
			Modified: base.Add(time.Duration(i) * time.Hour),
		})
	}
	return rows
}

// ---- Tab cycling ----

// TestCycleTargetTabHonoursDirection is the one genuinely behaviour-changing
// edit in this feature. With two tabs the direction argument was decorative
// and Shift-Tab did what Tab did; with three it has to mean something.
func TestCycleTargetTabHonoursDirection(t *testing.T) {
	a := &App{}

	forward := []TargetTab{TargetTabPullRequests, TargetTabPatches, TargetTabLocal}
	for i, want := range forward {
		a.CycleTargetTab(true)
		if a.TargetTab != want {
			t.Fatalf("forward step %d: tab = %v, want %v", i, a.TargetTab, want)
		}
	}

	backward := []TargetTab{TargetTabPatches, TargetTabPullRequests, TargetTabLocal}
	for i, want := range backward {
		a.CycleTargetTab(false)
		if a.TargetTab != want {
			t.Fatalf("backward step %d: tab = %v, want %v", i, a.TargetTab, want)
		}
	}
}

func TestAllTargetTabsCoversEveryTab(t *testing.T) {
	tabs := AllTargetTabs()
	if len(tabs) != targetTabCount {
		t.Fatalf("AllTargetTabs has %d entries but targetTabCount is %d — "+
			"cycling and rendering would disagree", len(tabs), targetTabCount)
	}
	for _, tab := range tabs {
		if tab.Label() == "" {
			t.Errorf("tab %v has no chip label", tab)
		}
	}
}

// TestSelectorTabForPatchReview means reopening the selector lands where the
// review came from rather than on whichever tab happens to be first.
func TestSelectorTabForPatchReview(t *testing.T) {
	a := &App{DiffSource: DiffSource{Kind: DiffSourcePatch}}
	if got := a.SelectorTabForReview(); got != TargetTabPatches {
		t.Errorf("tab = %v, want TargetTabPatches", got)
	}
}

// ---- Loading ----

// TestPatchTabLoadIsPulledNotPushed pins the split the PR tab established:
// the app records what to load, the UI performs it. The app never touches the
// filesystem, which is what keeps a directory scan off the render loop.
func TestPatchTabLoadIsPulledNotPushed(t *testing.T) {
	a := &App{}
	a.SetPatchTabDir("/inbox")

	req, ok := a.TakePatchTabLoad()
	if !ok {
		t.Fatal("setting a directory should arm a scan")
	}
	if req.Dir != "/inbox" {
		t.Errorf("request dir = %q, want /inbox", req.Dir)
	}
	if _, again := a.TakePatchTabLoad(); again {
		t.Error("a request must be handed out once, not repeatedly")
	}
	if !a.PatchTab.Loading {
		t.Error("the tab should show as loading until a result arrives")
	}
}

// TestStaleScanResultIsDiscarded is why the generation counter exists: a scan
// of a directory the reviewer has navigated away from must not repopulate the
// list under them.
func TestStaleScanResultIsDiscarded(t *testing.T) {
	a := &App{}
	a.SetPatchTabDir("/first")
	stale, _ := a.TakePatchTabLoad()

	a.SetPatchTabDir("/second")
	a.TakePatchTabLoad()

	a.ApplyPatchTabRows(stale.Gen, rowsAt("old.patch"))
	if len(a.PatchTab.Rows) != 0 {
		t.Errorf("a superseded scan repopulated the list: %+v", a.PatchTab.Rows)
	}

	a.SetPatchTabError(stale.Gen, "boom")
	if a.PatchTab.Err != "" {
		t.Errorf("a superseded error surfaced: %q", a.PatchTab.Err)
	}
}

// TestRevisitingTheTabDoesNotRescan keeps switching tabs cheap; rescanning is
// an explicit action.
func TestRevisitingTheTabDoesNotRescan(t *testing.T) {
	a := &App{}
	a.SetTargetTab(TargetTabPatches)
	if _, ok := a.TakePatchTabLoad(); !ok {
		t.Fatal("first visit should arm a scan")
	}
	a.ApplyPatchTabRows(a.PatchTab.Gen, rowsAt("a.patch"))

	a.SetTargetTab(TargetTabLocal)
	a.SetTargetTab(TargetTabPatches)
	if _, ok := a.TakePatchTabLoad(); ok {
		t.Error("revisiting a loaded tab should not rescan")
	}

	a.ReloadPatchTab()
	if _, ok := a.TakePatchTabLoad(); !ok {
		t.Error("an explicit reload should rescan")
	}
}

// TestRowsSortNewestFirst matches how an inbox is read: what just arrived is
// what you want to look at.
func TestRowsSortNewestFirst(t *testing.T) {
	a := &App{}
	a.SetTargetTab(TargetTabPatches)
	a.TakePatchTabLoad()
	a.ApplyPatchTabRows(a.PatchTab.Gen, rowsAt("oldest.patch", "middle.patch", "newest.patch"))

	got := a.PatchTabFilteredRows()
	if len(got) != 3 || got[0].Name != "newest.patch" || got[2].Name != "oldest.patch" {
		t.Errorf("rows = %v, want newest first", names(got))
	}
}

// ---- Cursor and filter ----

func TestCursorStaysInsideTheList(t *testing.T) {
	a := &App{}
	a.SetTargetTab(TargetTabPatches)
	a.TakePatchTabLoad()
	a.ApplyPatchTabRows(a.PatchTab.Gen, rowsAt("a.patch", "b.patch"))

	a.PatchTabUp()
	if a.PatchTab.Cursor != 0 {
		t.Errorf("cursor = %d, want 0 at the top", a.PatchTab.Cursor)
	}
	a.PatchTabDown()
	a.PatchTabDown()
	a.PatchTabDown()
	if a.PatchTab.Cursor != 1 {
		t.Errorf("cursor = %d, want 1 at the bottom", a.PatchTab.Cursor)
	}
}

func TestScrollWindowFollowsTheCursor(t *testing.T) {
	a := &App{}
	a.SetTargetTab(TargetTabPatches)
	a.TakePatchTabLoad()
	a.ApplyPatchTabRows(a.PatchTab.Gen, rowsAt("1", "2", "3", "4", "5", "6"))
	a.SetPatchTabViewportHeight(2)

	for range 4 {
		a.PatchTabDown()
	}
	pt := a.PatchTab
	if pt.Cursor < pt.ScrollOffset || pt.Cursor >= pt.ScrollOffset+pt.ViewportHeight {
		t.Errorf("cursor %d outside window [%d,%d)", pt.Cursor, pt.ScrollOffset,
			pt.ScrollOffset+pt.ViewportHeight)
	}
}

func TestFilterNarrowsAcrossEveryVisibleField(t *testing.T) {
	a := &App{}
	a.SetTargetTab(TargetTabPatches)
	a.TakePatchTabLoad()
	rows := rowsAt("net-fix.patch", "fs-cleanup.mbox")
	rows[1].Series = "3 patches"
	a.ApplyPatchTabRows(a.PatchTab.Gen, rows)

	for _, needle := range []string{"net", "NET", "fs-cleanup", "3 patches"} {
		a.PatchTab.Filter = needle
		if a.PatchTabRowCount() == 0 {
			t.Errorf("filter %q matched nothing", needle)
		}
	}
	a.PatchTab.Filter = "nothing matches this"
	if a.PatchTabRowCount() != 0 {
		t.Error("filter should be able to match nothing")
	}
}

func TestFilterPromptLifecycle(t *testing.T) {
	a := &App{}
	a.SetTargetTab(TargetTabPatches)
	a.TakePatchTabLoad()
	a.ApplyPatchTabRows(a.PatchTab.Gen, rowsAt("net-fix.patch", "fs.patch"))

	a.BeginPatchTabFilter()
	if !a.PatchTabFilterEditing() {
		t.Fatal("the prompt should be open")
	}
	for _, r := range "net" {
		a.InsertPatchTabFilterChar(r)
	}
	if a.PatchTabRowCount() != 1 {
		t.Errorf("typing should filter live, got %d rows", a.PatchTabRowCount())
	}

	a.DeletePatchTabFilterChar()
	a.CommitPatchTabFilter()
	if a.PatchTabFilterEditing() {
		t.Error("committing should close the prompt")
	}
	if a.PatchTab.Filter != "ne" {
		t.Errorf("filter = %q, want it kept after commit", a.PatchTab.Filter)
	}

	a.BeginPatchTabFilter()
	a.CancelPatchTabFilter()
	if a.PatchTab.Filter != "" || a.PatchTabFilterEditing() {
		t.Error("cancelling should clear the filter and close the prompt")
	}
}

// TestFilterPromptOnlyOpensOnItsOwnTab keeps "/" from opening a prompt the
// reviewer cannot see.
func TestFilterPromptOnlyOpensOnItsOwnTab(t *testing.T) {
	a := &App{TargetTab: TargetTabLocal}
	a.BeginPatchTabFilter()
	if a.PatchTabFilterEditing() {
		t.Error("the patch filter must not open from another tab")
	}
}

// TestSelectedRowFollowsTheFilter guards the subtle one: the cursor indexes
// the filtered list, so opening must use the same list the reviewer sees.
func TestSelectedRowFollowsTheFilter(t *testing.T) {
	a := &App{}
	a.SetTargetTab(TargetTabPatches)
	a.TakePatchTabLoad()
	a.ApplyPatchTabRows(a.PatchTab.Gen, rowsAt("aaa.patch", "bbb.patch", "ccc.patch"))

	a.PatchTab.Filter = "bbb"
	a.clampPatchTabCursor()
	row, ok := a.SelectedPatchRow()
	if !ok || row.Name != "bbb.patch" {
		t.Errorf("selected %+v, want bbb.patch", row)
	}
}

func TestUnreadableRowIsListedButNotReviewable(t *testing.T) {
	row := PatchRow{Name: "broken.patch", Err: "not a patch, diff, or mbox"}
	if row.Reviewable() {
		t.Error("a row that could not be read is not reviewable")
	}
	if (PatchRow{Name: "ok.patch"}).Reviewable() != true {
		t.Error("a readable row is reviewable")
	}
}

func names(rows []PatchRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}
