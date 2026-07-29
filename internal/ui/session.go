package ui

import (
	"fmt"
	"os"
	"time"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/slug"
)

// sessionLifecycle owns the persisted-session plumbing around a running TUI:
// eager creation, the stderr slug announcement, periodic external-change
// merging, saves, and exit cleanup.
type sessionLifecycle struct {
	store      *persistence.Store
	path       string
	snapshot   *model.ReviewSession // last persisted state, the 3-way merge base
	fileState  *fileState
	wasCreated bool // session was auto-created this run (delete-if-empty on exit)
	watchEvery time.Duration
	// watchDisabled turns the external-change poll off entirely
	// (review_watch_interval_ms = 0).
	watchDisabled bool
	lastWatchAt   time.Time
}

// msDuration converts config milliseconds to a duration.
func msDuration(ms int) time.Duration { return time.Duration(ms) * time.Millisecond }

type fileState struct {
	modTime time.Time
	size    int64
}

func statFile(path string) *fileState {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return &fileState{modTime: info.ModTime(), size: info.Size()}
}

// openSession loads the latest matching persisted session or keeps the fresh
// one, eagerly persists it, announces the slug on stderr, and marks it
// active.
func openSession(store *persistence.Store, fresh *model.ReviewSession) (*sessionLifecycle, *model.ReviewSession) {
	lc := &sessionLifecycle{store: store, watchEvery: time.Second}
	session := fresh
	lc.wasCreated = true

	if store != nil {
		if path, existing, found, err := store.LoadLatestSessionForContext(
			fresh.RepoPath, fresh.BranchName, fresh.BaseCommit, fresh.DiffSource, fresh.CommitRange,
		); err == nil && found {
			session = existing
			lc.path = path
			lc.wasCreated = false
		}
		if path, err := store.SaveSession(session); err == nil {
			lc.path = path
			lc.snapshot = session.Clone()
			lc.fileState = statFile(path)
			_ = store.MarkSessionActive(session, path)
		}
	}

	announceSession(session)
	return lc, session
}

// announceSession prints the session slug to stderr for collaborating
// agents, before the TUI takes over the terminal.
func announceSession(session *model.ReviewSession) {
	if s, err := slug.ForSession(session); err == nil {
		fmt.Fprintf(os.Stderr, "mrman-session: %s\n", s.String())
	}
}

// save persists the live session with merge-on-write and refreshes the
// snapshot and file state.
func (lc *sessionLifecycle) save(a *app.App) error {
	if lc.store == nil {
		return nil
	}
	path, merged, err := lc.store.SaveSessionByIdentity(a.Session,
		func(persisted *model.ReviewSession) (*model.ReviewSession, error) {
			if persisted != nil && lc.snapshot != nil {
				app.MergeExternalSessionChanges(a.Session, lc.snapshot, persisted)
			}
			return a.Session, nil
		})
	if err != nil {
		return err
	}
	lc.path = path
	lc.snapshot = merged.Clone()
	lc.fileState = statFile(path)
	a.Dirty = false
	return nil
}

// pollExternalChanges merges externally written session changes (agents
// running `mrman review add`) into the live session. Returns the number of
// merged changes. Skipped while composing a comment and between intervals.
func (lc *sessionLifecycle) pollExternalChanges(a *app.App, composing bool) int {
	if lc.store == nil || lc.path == "" || composing || lc.watchDisabled {
		return 0
	}
	if time.Since(lc.lastWatchAt) < lc.watchEvery {
		return 0
	}
	lc.lastWatchAt = time.Now()

	current := statFile(lc.path)
	if current == nil {
		return 0
	}
	if lc.fileState != nil && current.modTime.Equal(lc.fileState.modTime) && current.size == lc.fileState.size {
		return 0
	}
	latest, err := lc.store.LoadSession(lc.path)
	if err != nil {
		return 0
	}
	base := lc.snapshot
	if base == nil {
		base = latest
	}
	changed := app.MergeExternalSessionChanges(a.Session, base, latest)
	lc.snapshot = latest
	lc.fileState = current
	if changed > 0 {
		a.RebuildAnnotations()
	}
	return changed
}

// finish cleans up on exit: auto-created sessions that stayed empty are
// deleted, and the active-session marker is cleared.
func (lc *sessionLifecycle) finish(a *app.App) {
	if lc.store == nil {
		return
	}
	if lc.wasCreated && lc.path != "" &&
		!a.Session.HasComments() && !a.Session.HasReviewedState() {
		_, _ = lc.store.DeleteSessionIfEmpty(lc.path)
	}
	_ = lc.store.ClearActiveSessionForPid()
}
