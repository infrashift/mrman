package persistence

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
		active.Sessions = append(kept, activeSessionEntry{
			Pid:        pid,
			Slug:       sl.String(),
			Path:       normalized,
			LastSeenAt: nowFn(),
		})
		return s.saveActiveSessionsUnlocked(&active)
	})
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
