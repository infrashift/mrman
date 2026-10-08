package ui

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/x/term"

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
	// lastHeartbeatAt is when the active-session entry was last refreshed;
	// the store treats an entry silent for hours as abandoned.
	lastHeartbeatAt time.Time
	// activateErr records a failure to mark the session active (and to
	// record an agent grant with it), so the user is told rather than
	// shown a grant that does not exist.
	activateErr error
	// adoptedFrom is the HEAD a carried-forward session was written against,
	// empty when the session was resolved at the current HEAD or created
	// fresh. The reviewer is told: comments arriving from a HEAD they have
	// since rewritten is the one thing about this that could surprise them.
	adoptedFrom string
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

// openSession resolves the session to review: the one persisted at this exact
// HEAD, else a live session carried forward from a previous HEAD, else the
// fresh one. It then eagerly persists the result, announces the slug on
// stderr, and marks it active.
//
// The carry-forward is what lets a review survive an amend or a rebase. It is
// tried only when the exact lookup misses, so an unmoved HEAD never pays for
// it, and it is safe only because NewApp re-validates every comment's anchor
// against the new diff afterwards.
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
		} else if adopted, ok, aerr := store.AdoptSessionForNewHead(
			fresh.RepoPath, fresh.BranchName, fresh.BaseCommit, fresh.DiffSource,
		); aerr == nil && ok {
			session = adopted.Session
			lc.path = adopted.Path
			lc.wasCreated = false
			lc.adoptedFrom = adopted.FromHead
		}
		if path, err := store.SaveSession(session); err == nil {
			lc.path = path
			lc.snapshot = session.Clone()
			lc.fileState = statFile(path)
			lc.lastHeartbeatAt = time.Now()
			lc.activateErr = store.MarkSessionActive(session, path)
		}
	}

	announceSessionDuringRun(session)
	return lc, session
}

// stderrIsTerminal is the TTY probe for the session announcement, a var so
// tests can drive both paths.
var stderrIsTerminal = func() bool {
	return term.IsTerminal(os.Stderr.Fd())
}

// announceSession prints the session slug to stderr for collaborating
// agents, before the TUI takes over the terminal. Safe only from the
// pre-Run call sites; see announceSessionDuringRun.
func announceSession(session *model.ReviewSession) {
	if s, err := slug.ForSession(session); err == nil {
		fmt.Fprintf(os.Stderr, "mrman-session: %s\n", s.String())
	}
}

// announceSessionDuringRun announces a session opened while the TUI is
// already running — the target selector confirming a commit range, which
// creates the session mid-loop rather than before Run.
//
// A raw stderr write is only safe when stderr is not the terminal. Bubble
// Tea owns the alt screen and diffs its own frames; an unsolicited write
// lands inside the current frame, corrupts it, and leaves the renderer's
// idea of the screen wrong for every frame after — which shows up as
// comment text appearing one character per line. Agents redirect stderr, so
// they still get the slug; a human at a terminal loses a line they could not
// have read inside a full-screen frame anyway, and `mrman review list`
// still finds the session.
func announceSessionDuringRun(session *model.ReviewSession) {
	if stderrIsTerminal() {
		return
	}
	announceSession(session)
}

// save persists the live session with merge-on-write and refreshes the
// snapshot and file state.
//
// Changes merged in from the file (an agent's `mrman review add` since the
// last poll) are redrawn here: the refreshed file state matches what this
// save wrote, so no later poll will see them as new.
func (lc *sessionLifecycle) save(a *app.App) error {
	if lc.store == nil {
		return nil
	}
	externalChanges := 0
	path, merged, err := lc.store.SaveSessionByIdentity(a.Session,
		func(persisted *model.ReviewSession) (*model.ReviewSession, error) {
			if persisted != nil && lc.snapshot != nil {
				externalChanges = app.MergeExternalSessionChanges(a.Session, lc.snapshot, persisted)
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
	if externalChanges > 0 {
		a.RebuildAnnotations()
	}
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
	lc.heartbeat()

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

// heartbeatEvery is how often a running TUI refreshes its active-session
// entry: far inside the store's staleness window, cheap enough to ignore.
const heartbeatEvery = 5 * time.Minute

// heartbeat refreshes the active-session entry when it is due.
func (lc *sessionLifecycle) heartbeat() {
	if lc.store == nil || time.Since(lc.lastHeartbeatAt) < heartbeatEvery {
		return
	}
	lc.lastHeartbeatAt = time.Now()
	_ = lc.store.TouchActiveSession() // best effort; the next tick retries
}

// activationWarning is the message for a session whose active marker (and
// any grant) could not be written, "" when it was.
func (lc *sessionLifecycle) activationWarning(grantRequested bool) string {
	if lc.activateErr == nil {
		return ""
	}
	if grantRequested {
		return "Agent submit grant was NOT recorded: " + lc.activateErr.Error()
	}
	return "Session could not be marked active for agents: " + lc.activateErr.Error()
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
