package ui

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/reviewcli"
)

func testLifecycle(t *testing.T) (*sessionLifecycle, *Model) {
	t.Helper()
	store := &persistence.Store{ReviewsDir: t.TempDir()}
	m := testModel(t)
	lc, session := openSession(store, m.App.Session)
	m.App.Session = session
	m.session = lc
	lc.watchEvery = 0 // no interval throttling in tests
	return lc, m
}

func TestOpenSessionPersistsEagerly(t *testing.T) {
	lc, _ := testLifecycle(t)
	if lc.path == "" {
		t.Fatal("session must be persisted on open")
	}
	if !lc.wasCreated {
		t.Fatal("fresh session must be marked created")
	}
	if _, err := lc.store.LoadSession(lc.path); err != nil {
		t.Fatalf("persisted session unreadable: %v", err)
	}
}

func TestOpenSessionReusesExisting(t *testing.T) {
	store := &persistence.Store{ReviewsDir: t.TempDir()}
	m := testModel(t)
	lc1, session := openSession(store, m.App.Session)
	// Leave a comment so the session survives.
	session.ReviewComments = append(session.ReviewComments,
		model.NewComment("keep me", model.CommentTypeFromID("note"), nil))
	m.App.Session = session
	m.session = lc1
	if err := lc1.save(m.App); err != nil {
		t.Fatal(err)
	}

	// A second open with the same context resumes the same session.
	fresh := model.NewReviewSession(session.RepoPath, session.BaseCommit, session.BranchName, session.DiffSource)
	lc2, resumed := openSession(store, fresh)
	if lc2.wasCreated {
		t.Fatal("existing session must be resumed, not recreated")
	}
	if len(resumed.ReviewComments) != 1 || resumed.ReviewComments[0].Content != "keep me" {
		t.Fatalf("resumed session lost comments: %+v", resumed.ReviewComments)
	}
}

func TestSaveClearsDirty(t *testing.T) {
	lc, m := testLifecycle(t)
	m.App.Dirty = true
	if err := lc.save(m.App); err != nil {
		t.Fatal(err)
	}
	if m.App.Dirty {
		t.Fatal("save must clear dirty")
	}
}

// The M4 flagship: an agent runs `mrman review add` against the session file
// while the TUI holds it open; the watcher merges the comment live.
func TestExternalAgentCommentMergesLive(t *testing.T) {
	lc, m := testLifecycle(t)

	err := reviewcli.Add(lc.store, reviewcli.Options{
		Session: lc.path,
		Comment: "agent finding",
		Type:    "issue",
		// Review-level comment: no target file needed.
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	merged := lc.pollExternalChanges(m.App, false)
	if merged != 1 {
		t.Fatalf("merged = %d, want 1", merged)
	}
	if len(m.App.Session.ReviewComments) != 1 ||
		m.App.Session.ReviewComments[0].Content != "agent finding" {
		t.Fatalf("comment not merged: %+v", m.App.Session.ReviewComments)
	}

	// Second poll with no external change is a no-op.
	if again := lc.pollExternalChanges(m.App, false); again != 0 {
		t.Fatalf("idle poll merged %d", again)
	}
}

func TestPollSkipsWhileComposing(t *testing.T) {
	lc, m := testLifecycle(t)
	err := reviewcli.Add(lc.store, reviewcli.Options{
		Session: lc.path, Comment: "held back",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if merged := lc.pollExternalChanges(m.App, true); merged != 0 {
		t.Fatal("poll must skip while composing")
	}
	if merged := lc.pollExternalChanges(m.App, false); merged != 1 {
		t.Fatal("poll must catch up after composing ends")
	}
}

func TestPollHonorsInterval(t *testing.T) {
	lc, m := testLifecycle(t)
	lc.watchEvery = time.Hour
	lc.lastWatchAt = time.Now()
	err := reviewcli.Add(lc.store, reviewcli.Options{
		Session: lc.path, Comment: "later",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if merged := lc.pollExternalChanges(m.App, false); merged != 0 {
		t.Fatal("poll must respect the watch interval")
	}
}

func TestFinishDeletesEmptyAutoCreatedSession(t *testing.T) {
	lc, m := testLifecycle(t)
	lc.finish(m.App)
	if _, err := lc.store.LoadSession(lc.path); err == nil {
		t.Fatal("empty auto-created session must be deleted on exit")
	}
}

func TestFinishKeepsSessionWithComments(t *testing.T) {
	lc, m := testLifecycle(t)
	m.App.Session.ReviewComments = append(m.App.Session.ReviewComments,
		model.NewComment("keep", model.CommentTypeFromID("note"), nil))
	if err := lc.save(m.App); err != nil {
		t.Fatal(err)
	}
	lc.finish(m.App)
	if _, err := lc.store.LoadSession(lc.path); err != nil {
		t.Fatal("session with comments must survive exit")
	}
}

func TestQuitGuardsDirtyComments(t *testing.T) {
	_, m := testLifecycle(t)
	m.App.Session.ReviewComments = append(m.App.Session.ReviewComments,
		model.NewComment("unsaved", model.CommentTypeFromID("note"), nil))
	m.App.Dirty = true

	// :q refuses.
	press(m, ":", ':', 0)
	pressRune(m, 'q')
	press(m, "", 0x0d, 0) // Enter
	if m.App.Message == nil {
		t.Fatal(":q with dirty comments must warn")
	}

	// :w then :q succeeds.
	press(m, ":", ':', 0)
	pressRune(m, 'w')
	press(m, "", 0x0d, 0)
	if m.App.Dirty {
		t.Fatal(":w must clear dirty")
	}
}

// TestAnnounceSessionDuringRunSkipsTerminal pins the fix for a frame-corruption
// bug. openSession runs mid-loop when the target selector confirms a commit
// range, so the slug announcement would land inside a frame Bubble Tea owns on
// the alt screen. That desynchronizes the renderer and shows up as saved
// comment text rendering one character per line.
//
// Agents redirect stderr and must still receive the slug, so the write is
// suppressed only when stderr is the terminal.
func TestAnnounceSessionDuringRunSkipsTerminal(t *testing.T) {
	branch := "main"
	session := model.NewReviewSession(t.TempDir(), "abc123", &branch, model.SourceWorkingTree)

	for _, tc := range []struct {
		name       string
		isTerminal bool
		wantWrite  bool
	}{
		{"terminal: stay silent so the frame is not corrupted", true, false},
		{"redirected: agents still get the slug", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restoreProbe := stderrIsTerminal
			stderrIsTerminal = func() bool { return tc.isTerminal }
			t.Cleanup(func() { stderrIsTerminal = restoreProbe })

			got := captureStderr(t, func() { announceSessionDuringRun(session) })
			if wrote := got != ""; wrote != tc.wantWrite {
				t.Errorf("wrote=%v (%q), want wrote=%v", wrote, got, tc.wantWrite)
			}
			if tc.wantWrite && !bytes.Contains([]byte(got), []byte("mrman-session: ")) {
				t.Errorf("announcement = %q, want the mrman-session prefix", got)
			}
		})
	}
}

// captureStderr redirects os.Stderr for the duration of fn and returns what
// was written to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stderr
	os.Stderr = w
	fn()
	os.Stderr = saved
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(out)
}
