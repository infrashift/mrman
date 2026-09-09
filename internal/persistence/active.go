package persistence

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/infrashift/mrman/internal/model"
)

// activeSessionsFilename is the file inside reviews/ tracking which sessions
// live TUIs currently have open.
const activeSessionsFilename = "active_sessions.json"

// activeSessionsVersion is the active-sessions file schema version.
const activeSessionsVersion = "1.0"

// activeSessionStaleAfter is when an active-session heartbeat is considered
// abandoned. A var so tests can shrink it.
var activeSessionStaleAfter = 12 * time.Hour

type activeSessionsFile struct {
	Version  string               `json:"version"`
	Sessions []activeSessionEntry `json:"sessions"`
}

func newActiveSessionsFile() activeSessionsFile {
	return activeSessionsFile{Version: activeSessionsVersion, Sessions: []activeSessionEntry{}}
}

func (a *activeSessionsFile) pruneStale() {
	kept := a.Sessions[:0]
	for _, e := range a.Sessions {
		if e.isFresh() {
			kept = append(kept, e)
		}
	}
	a.Sessions = kept
}

type activeSessionEntry struct {
	Pid        int       `json:"pid"`
	Slug       string    `json:"slug"`
	Path       string    `json:"path"`
	LastSeenAt time.Time `json:"last_seen_at"`
	// GrantedEvents are the submit events this session's owner authorized an
	// agent to post. Empty — the default, and the value for every entry
	// written before this existed — means none.
	//
	// The grant lives here rather than in the session file precisely because
	// an agent can write the session file (that is what `review add` does)
	// but cannot fabricate a live process. It is issued only by an
	// interactive TUI launch and dies with that process.
	GrantedEvents []string `json:"granted_events,omitempty"`
}

// isFresh reports whether the entry still describes a live review: recent
// heartbeat (non-negative age within the staleness window), a running
// process, and a session file that still exists.
func (e activeSessionEntry) isFresh() bool {
	age := nowFn().Sub(e.LastSeenAt)
	if age < 0 || age > activeSessionStaleAfter {
		return false
	}
	if !processAlive(e.Pid) {
		return false
	}
	_, err := os.Stat(e.Path)
	return err == nil
}

// normalizeActivePath canonicalizes a session path for comparison, falling
// back to the raw path when resolution fails.
func normalizeActivePath(path string) string {
	return canonicalPath(path)
}

func (s *Store) activeSessionsPath() string {
	return filepath.Join(s.ReviewsDir, activeSessionsFilename)
}

// loadActiveSessionsOrDefault reads the active-sessions file, treating a
// missing or unparseable file as empty (mirroring tuicr's unwrap_or_default
// at every call site).
func (s *Store) loadActiveSessionsOrDefault() activeSessionsFile {
	data, err := os.ReadFile(s.activeSessionsPath())
	if err != nil {
		return newActiveSessionsFile()
	}
	var active activeSessionsFile
	if err := json.Unmarshal(data, &active); err != nil {
		return newActiveSessionsFile()
	}
	if active.Sessions == nil {
		active.Sessions = []activeSessionEntry{}
	}
	return active
}

func (s *Store) saveActiveSessionsUnlocked(active *activeSessionsFile) error {
	data, err := marshalPretty(active)
	if err != nil {
		return err
	}
	return writeAtomic(s.activeSessionsPath(), data)
}

// MarkSessionActive records that this process has the session at path open,
// pruning stale entries and superseding any previous entry for this pid or
// path.
func (s *Store) MarkSessionActive(sess *model.ReviewSession, path string) error {
	return s.MarkSessionActiveWithGrant(sess, path, nil)
}

// MarkSessionActiveWithGrant is MarkSessionActive plus an agent-submit
// grant. Only an interactive TUI launch passes a non-empty grant; every
// other caller uses MarkSessionActive and records none.
func (s *Store) MarkSessionActiveWithGrant(
	sess *model.ReviewSession, path string, grantedEvents []string,
) error {
	if err := s.maybeMigrate(); err != nil {
		return err
	}
	sl, err := sessionSlug(sess)
	if err != nil {
		return err
	}
	normalized := normalizeActivePath(path)
	return s.withLock(func() error {
		active := s.loadActiveSessionsOrDefault()
		active.pruneStale()
		pid := os.Getpid()
		kept := active.Sessions[:0]
		for _, e := range active.Sessions {
			if e.Pid != pid && e.Path != normalized {
				kept = append(kept, e)
			}
		}
		kept = append(kept, activeSessionEntry{
			Pid:           pid,
			Slug:          sl.String(),
			Path:          normalized,
			LastSeenAt:    nowFn(),
			GrantedEvents: append([]string(nil), grantedEvents...),
		})
		active.Sessions = kept
		return s.saveActiveSessionsUnlocked(&active)
	})
}

// TouchActiveSession refreshes this process's heartbeat. Freshness is
// bounded by activeSessionStaleAfter; a TUI left open past that would
// otherwise vanish from `review list`, lose its agent-submit grant and be
// reported as exited by `review watch` while still running.
func (s *Store) TouchActiveSession() error {
	if err := s.maybeMigrate(); err != nil {
		return err
	}
	return s.withLock(func() error {
		active := s.loadActiveSessionsOrDefault()
		pid := os.Getpid()
		touched := false
		for i := range active.Sessions {
			if active.Sessions[i].Pid == pid {
				active.Sessions[i].LastSeenAt = nowFn()
				touched = true
			}
		}
		if !touched {
			return nil
		}
		return s.saveActiveSessionsUnlocked(&active)
	})
}

// RevokeGrantForPid drops this process's agent-submit grant while keeping
// the session active. Dropping privilege needs no ceremony, so this never
// fails on a missing entry.
func (s *Store) RevokeGrantForPid() error {
	if err := s.maybeMigrate(); err != nil {
		return err
	}
	return s.withLock(func() error {
		active := s.loadActiveSessionsOrDefault()
		pid := os.Getpid()
		for i := range active.Sessions {
			if active.Sessions[i].Pid == pid {
				active.Sessions[i].GrantedEvents = nil
			}
		}
		return s.saveActiveSessionsUnlocked(&active)
	})
}

// GrantReason explains why a grant lookup denied a submit.
type GrantReason string

// Grant denial reasons, reported verbatim to the agent so it can tell an
// expired grant from one that never existed.
const (
	// GrantOK means the requested event is authorized.
	GrantOK GrantReason = ""
	// GrantNone means the session has no active entry carrying a grant.
	GrantNone GrantReason = "no_grant"
	// GrantExpired means an entry exists but its owning TUI is gone.
	GrantExpired GrantReason = "grant_expired"
	// GrantEventNotAllowed means a grant exists but not for this event.
	GrantEventNotAllowed GrantReason = "event_not_granted"
)

// AgentSubmitGrant reports whether the session at path authorizes an agent
// to submit the named event.
//
// A grant counts only while its issuing process is alive: isFresh checks the
// pid, so quitting the TUI revokes it without any explicit teardown, and a
// crashed TUI cannot leave authority behind.
func (s *Store) AgentSubmitGrant(path, event string) (granted []string, reason GrantReason, err error) {
	if err := s.maybeMigrate(); err != nil {
		return nil, GrantNone, err
	}
	normalized := normalizeActivePath(path)
	active := s.loadActiveSessionsOrDefault()

	var stale bool
	for _, e := range active.Sessions {
		if normalizeActivePath(e.Path) != normalized {
			continue
		}
		if !e.isFresh() {
			// Remember that something was here: "your grant expired" is a
			// materially different message from "there was never one".
			stale = stale || len(e.GrantedEvents) > 0
			continue
		}
		if len(e.GrantedEvents) == 0 {
			continue
		}
		if slices.Contains(e.GrantedEvents, event) {
			return e.GrantedEvents, GrantOK, nil
		}
		return e.GrantedEvents, GrantEventNotAllowed, nil
	}
	if stale {
		return nil, GrantExpired, nil
	}
	return nil, GrantNone, nil
}

// GrantedEventsForPath returns the live grant on a session, for display.
func (s *Store) GrantedEventsForPath(path string) []string {
	normalized := normalizeActivePath(path)
	active := s.loadActiveSessionsOrDefault()
	for _, e := range active.Sessions {
		if normalizeActivePath(e.Path) == normalized && e.isFresh() {
			return e.GrantedEvents
		}
	}
	return nil
}

// ClearActiveSessionForPid removes this process's active-session entry.
func (s *Store) ClearActiveSessionForPid() error {
	if err := s.maybeMigrate(); err != nil {
		return err
	}
	return s.withLock(func() error {
		active := s.loadActiveSessionsOrDefault()
		pid := os.Getpid()
		kept := active.Sessions[:0]
		for _, e := range active.Sessions {
			if e.Pid != pid {
				kept = append(kept, e)
			}
		}
		active.Sessions = kept
		return s.saveActiveSessionsUnlocked(&active)
	})
}

// ActiveSessionPaths returns the set of normalized session paths that fresh
// active entries reference: heartbeat within the staleness window, owner
// process alive, and session file still on disk.
func (s *Store) ActiveSessionPaths() (map[string]bool, error) {
	if err := s.maybeMigrate(); err != nil {
		return nil, err
	}
	active := s.loadActiveSessionsOrDefault()
	paths := make(map[string]bool)
	for _, e := range active.Sessions {
		if e.isFresh() {
			paths[normalizeActivePath(e.Path)] = true
		}
	}
	return paths, nil
}

// pathExists normalizes a stat call to a boolean existence check while
// propagating unexpected failures.
func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}
