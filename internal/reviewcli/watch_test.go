package reviewcli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
)

// watchSession writes a local session and returns the store and its path.
func watchSession(t *testing.T) (*persistence.Store, *model.ReviewSession, string) {
	t.Helper()
	store := &persistence.Store{ReviewsDir: filepath.Join(t.TempDir(), "reviews")}
	session := model.NewReviewSession("/repo", "abc1234", nil, model.SourceWorkingTree)
	path, err := store.SaveSession(session)
	if err != nil {
		t.Fatalf("save session: %v", err)
	}
	return store, session, path
}

// runWatch streams until the watcher returns, and decodes the events.
func runWatch(t *testing.T, store *persistence.Store, path string, w WatchOptions) []WatchEvent {
	t.Helper()
	var out bytes.Buffer
	if w.Interval == 0 {
		w.Interval = 5 * time.Millisecond
	}
	if w.Timeout == 0 && w.Done == nil {
		w.Timeout = 2 * time.Second // never hang a test
	}
	if err := Watch(store, Options{Session: path}, w, &out); err != nil {
		t.Fatalf("watch: %v", err)
	}
	var events []WatchEvent
	dec := json.NewDecoder(strings.NewReader(out.String()))
	for {
		var ev WatchEvent
		if err := dec.Decode(&ev); err != nil {
			break
		}
		events = append(events, ev)
	}
	return events
}

func TestWatchOpensWithASnapshot(t *testing.T) {
	store, session, path := watchSession(t)
	session.ReviewComments = append(session.ReviewComments,
		model.NewComment("first", model.CommentTypeFromID("note"), nil))
	if _, err := store.SaveSession(session); err != nil {
		t.Fatal(err)
	}

	events := runWatch(t, store, path, WatchOptions{Timeout: 30 * time.Millisecond})
	if len(events) == 0 || events[0].Event != WatchSnapshot {
		t.Fatalf("the stream must open with a snapshot, got %+v", events)
	}
	if len(events[0].Comments) != 1 {
		t.Errorf("snapshot carried %d comments, want 1", len(events[0].Comments))
	}
}

func TestWatchEmitsAddedAndRemoved(t *testing.T) {
	store, session, path := watchSession(t)

	// Add one comment shortly after the watch starts, then delete it.
	added := model.NewComment("appears", model.CommentTypeFromID("note"), nil)
	go func() {
		time.Sleep(20 * time.Millisecond)
		session.ReviewComments = append(session.ReviewComments, added)
		_, _ = store.SaveSession(session)
		time.Sleep(30 * time.Millisecond)
		session.ReviewComments = nil
		_, _ = store.SaveSession(session)
	}()

	events := runWatch(t, store, path, WatchOptions{Timeout: 250 * time.Millisecond})
	var sawAdd, sawRemove bool
	for _, ev := range events {
		switch ev.Event {
		case WatchCommentAdded:
			if ev.Comment != nil && ev.Comment.Content == "appears" {
				sawAdd = true
			}
		case WatchCommentRemoved:
			if ev.ID == added.ID {
				sawRemove = true
			}
		}
	}
	if !sawAdd {
		t.Errorf("a new comment must produce comment_added, got %+v", eventNames(events))
	}
	if !sawRemove {
		t.Errorf("a deleted comment must produce comment_removed, got %+v", eventNames(events))
	}
}

// TestWatchDoesNotChurnOnUnchangedComments guards the pointer-comparison
// trap: CommentOutput carries pointers, so a naive == would report every
// unchanged comment as changed on every poll.
func TestWatchDoesNotChurnOnUnchangedComments(t *testing.T) {
	store, session, path := watchSession(t)
	line := uint32(12)
	side := model.LineSideNew
	c := model.NewComment("stable", model.CommentTypeFromID("note"), &side)
	session.AddFile("src/x.go", model.StatusModified, 0)
	session.File("src/x.go").LineComments = map[uint32][]*model.Comment{line: {c}}
	if _, err := store.SaveSession(session); err != nil {
		t.Fatal(err)
	}

	// Touch the file repeatedly without changing its content.
	go func() {
		for range 3 {
			time.Sleep(15 * time.Millisecond)
			_, _ = store.SaveSession(session)
		}
	}()

	events := runWatch(t, store, path, WatchOptions{Timeout: 120 * time.Millisecond})
	for _, ev := range events {
		if ev.Event == WatchCommentChanged {
			t.Errorf("an unchanged comment must not emit comment_changed: %+v", ev.Comment)
		}
	}
}

func TestWatchEndsOnSubmit(t *testing.T) {
	store, session, path := watchSession(t)
	c := model.NewComment("will submit", model.CommentTypeFromID("note"), nil)
	session.ReviewComments = append(session.ReviewComments, c)
	if _, err := store.SaveSession(session); err != nil {
		t.Fatal(err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		c.LifecycleState = model.LifecycleSubmitted
		_, _ = store.SaveSession(session)
	}()

	events := runWatch(t, store, path, WatchOptions{Timeout: 300 * time.Millisecond})
	last := events[len(events)-1]
	if last.Event != WatchSubmitted {
		t.Fatalf("the stream must end on submitted, got %v", eventNames(events))
	}
	if last.LifecycleState != string(model.LifecycleSubmitted) {
		t.Errorf("lifecycle_state = %q", last.LifecycleState)
	}
}

func TestWatchTimesOut(t *testing.T) {
	store, _, path := watchSession(t)
	events := runWatch(t, store, path, WatchOptions{Timeout: 30 * time.Millisecond})
	last := events[len(events)-1]
	if last.Event != WatchClosed || last.Reason != ClosedTimeout {
		t.Errorf("expected closed/timeout, got %+v", last)
	}
}

func TestWatchClosesOnCancel(t *testing.T) {
	store, _, path := watchSession(t)
	done := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(done)
	}()

	events := runWatch(t, store, path, WatchOptions{Done: done, Timeout: time.Second})
	last := events[len(events)-1]
	if last.Event != WatchClosed || last.Reason != ClosedCanceled {
		t.Errorf("expected closed/canceled, got %+v", last)
	}
}

func TestWatchSinceSkipsTheSnapshot(t *testing.T) {
	store, session, path := watchSession(t)
	first := model.NewComment("one", model.CommentTypeFromID("note"), nil)
	second := model.NewComment("two", model.CommentTypeFromID("note"), nil)
	session.ReviewComments = append(session.ReviewComments, first, second)
	if _, err := store.SaveSession(session); err != nil {
		t.Fatal(err)
	}

	events := runWatch(t, store, path, WatchOptions{
		Since: first.ID, Timeout: 30 * time.Millisecond,
	})
	for _, ev := range events {
		if ev.Event == WatchSnapshot {
			t.Error("--since must resume without re-sending everything")
		}
	}
	replayed := 0
	for _, ev := range events {
		if ev.Event == WatchCommentAdded {
			replayed++
			if ev.Comment.ID == first.ID {
				t.Error("--since must not replay the comment it resumed from")
			}
		}
	}
	if replayed != 1 {
		t.Errorf("replayed %d comments, want just the one after --since", replayed)
	}
}

func eventNames(events []WatchEvent) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.Event
	}
	return out
}

// TestWatchIgnoresOtherSessionsTUIs pins the headless-agent loop: a session
// no TUI ever held must not be reported as exited just because some other
// review is open on the machine.
func TestWatchIgnoresOtherSessionsTUIs(t *testing.T) {
	store, _, path := watchSession(t)
	other := model.NewReviewSession("/other", "def5678", nil, model.SourceWorkingTree)
	otherPath, err := store.SaveSession(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSessionActive(other, otherPath); err != nil {
		t.Fatal(err)
	}

	events := runWatch(t, store, path, WatchOptions{Timeout: 60 * time.Millisecond})
	last := events[len(events)-1]
	if last.Event != WatchClosed || last.Reason != ClosedTimeout {
		t.Fatalf("watch ended with %+v, want a timeout — not tui_exited", last)
	}
}

// TestWatchReportsTUIExitOnlyAsATransition: a session that was held by a
// TUI and then released closes with tui_exited.
func TestWatchReportsTUIExitOnlyAsATransition(t *testing.T) {
	store, session, path := watchSession(t)
	if err := store.MarkSessionActive(session, path); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = store.ClearActiveSessionForPid()
	}()
	events := runWatch(t, store, path, WatchOptions{Timeout: 2 * time.Second})
	last := events[len(events)-1]
	if last.Event != WatchClosed || last.Reason != ClosedTUIExited {
		t.Fatalf("watch ended with %+v, want tui_exited", last)
	}
}

func TestWatchTimeoutIsNotRoundedUpToTheInterval(t *testing.T) {
	store, _, path := watchSession(t)
	start := time.Now()
	events := runWatch(t, store, path, WatchOptions{Timeout: 50 * time.Millisecond, Interval: 5 * time.Second})
	elapsed := time.Since(start)
	last := events[len(events)-1]
	if last.Reason != ClosedTimeout {
		t.Fatalf("ended with %+v", last)
	}
	if elapsed > time.Second {
		t.Fatalf("timeout of 50ms took %v: the poll interval must not delay the deadline", elapsed)
	}
}

func TestWatchIntervalDefaultsFromConfig(t *testing.T) {
	old := configWatchInterval
	configWatchInterval = func() time.Duration { return 7 * time.Millisecond }
	t.Cleanup(func() { configWatchInterval = old })
	store, _, path := watchSession(t)
	var out bytes.Buffer
	// Interval 0 must resolve through the config seam; the timeout proves
	// the loop ran at all.
	if err := Watch(store, Options{Session: path}, WatchOptions{Timeout: 20 * time.Millisecond}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"reason":"timeout"`) {
		t.Fatalf("output = %s", out.String())
	}
}
