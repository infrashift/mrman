package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/slug"
)

// Session summary kinds reported by ListSessions and ListAllSessions.
const (
	// SummaryKindLocal marks a summary of a local session.
	SummaryKindLocal = "local"
	// SummaryKindPr marks a summary of a PR session.
	SummaryKindPr = "pr"
)

// SessionSummary is one row of a session listing, denormalized from the
// manifest so listings need not open session files.
type SessionSummary struct {
	// Path is the absolute path of the session JSON file.
	Path string
	// Slug is the session's slug string (the manifest key).
	Slug string
	// Kind is SummaryKindLocal or SummaryKindPr.
	Kind string
	// UpdatedAt is the session's last-modified timestamp.
	UpdatedAt time.Time
	// CommentCount is the total comment count across all scopes.
	CommentCount int
	// ReviewedCount is the number of files marked reviewed.
	ReviewedCount int
	// FileCount is the number of files in the session.
	FileCount int
	// GrantedEvents are the submit events an agent may post for this
	// session right now, empty when none. Exposed so an agent can see what
	// it may do without attempting it and being refused.
	GrantedEvents []string
	// Anchor is the slug anchor segment (branch, short SHA, or pr/<n>).
	Anchor string
	// Active reports whether a live TUI currently has the session open.
	Active bool
}

// SaveSession saves a session to disk and updates the manifest. The on-disk
// path is derived from the session's slug, which is computed from the
// session's fields at save time. It returns the absolute session file path.
func (s *Store) SaveSession(sess *model.ReviewSession) (string, error) {
	if err := s.maybeMigrate(); err != nil {
		return "", err
	}
	var path string
	err := s.withLock(func() error {
		var lerr error
		path, lerr = s.saveSessionUnlocked(sess)
		return lerr
	})
	return path, err
}

// UpdateSession loads the session at path, applies fn to it, and saves the
// result (at its identity-derived path) under the storage lock. It returns
// the updated session.
func (s *Store) UpdateSession(path string, fn func(*model.ReviewSession) error) (*model.ReviewSession, error) {
	if err := s.maybeMigrate(); err != nil {
		return nil, err
	}
	var sess *model.ReviewSession
	err := s.withLock(func() error {
		loaded, lerr := s.LoadSession(path)
		if lerr != nil {
			return lerr
		}
		if lerr := fn(loaded); lerr != nil {
			return lerr
		}
		if _, lerr := s.saveSessionUnlocked(loaded); lerr != nil {
			return lerr
		}
		sess = loaded
		return nil
	})
	return sess, err
}

// SaveSessionByIdentity is the merge-on-write primitive. Under the storage
// lock it resolves the identity session's on-disk path, loads whatever is
// currently persisted there (nil when absent), passes it to fn, and saves the
// session fn returns. It returns the saved path and the saved session.
func (s *Store) SaveSessionByIdentity(
	identity *model.ReviewSession,
	fn func(persisted *model.ReviewSession) (*model.ReviewSession, error),
) (string, *model.ReviewSession, error) {
	if err := s.maybeMigrate(); err != nil {
		return "", nil, err
	}
	var savedPath string
	var saved *model.ReviewSession
	err := s.withLock(func() error {
		path, lerr := s.SessionPath(identity)
		if lerr != nil {
			return lerr
		}
		var persisted *model.ReviewSession
		exists, lerr := pathExists(path)
		if lerr != nil {
			return lerr
		}
		if exists {
			persisted, lerr = s.LoadSession(path)
			if lerr != nil {
				return lerr
			}
		}
		saved, lerr = fn(persisted)
		if lerr != nil {
			return lerr
		}
		savedPath, lerr = s.saveSessionUnlocked(saved)
		return lerr
	})
	if err != nil {
		return "", nil, err
	}
	return savedPath, saved, nil
}

// saveSessionUnlocked writes the session file and upserts its manifest entry.
// The caller must hold the reviews-dir lock.
func (s *Store) saveSessionUnlocked(sess *model.ReviewSession) (string, error) {
	sl, err := sessionSlug(sess)
	if err != nil {
		return "", err
	}
	relative, err := relativePathForSession(sl, sess)
	if err != nil {
		return "", err
	}
	fullPath := filepath.Join(s.ReviewsDir, relative)

	data, err := marshalPretty(sess)
	if err != nil {
		return "", err
	}
	if err := writeAtomic(fullPath, data); err != nil {
		return "", err
	}

	manifest := loadManifestOrDefault(s.ReviewsDir)
	manifest.Upsert(sl.String(), entryFromSession(sess, relative, manifestAnchorFor(sl)))
	if err := SaveManifest(s.ReviewsDir, manifest); err != nil {
		return "", err
	}
	return fullPath, nil
}

// LoadSession reads and parses a session JSON file from an absolute path.
// Unparseable content is reported as *errs.CorruptedSession.
func (s *Store) LoadSession(path string) (*model.ReviewSession, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sess model.ReviewSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, &errs.CorruptedSession{Detail: err.Error()}
	}
	return &sess, nil
}

// SessionPath computes the absolute on-disk path for a session from its
// identity fields, without touching the manifest.
func (s *Store) SessionPath(sess *model.ReviewSession) (string, error) {
	sl, err := sessionSlug(sess)
	if err != nil {
		return "", err
	}
	relative, err := relativePathForSession(sl, sess)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.ReviewsDir, relative), nil
}

// DeleteSessionIfEmpty removes the session at path and its manifest entry
// only when the session carries no comments and no reviewed state. It reports
// whether a deletion happened.
func (s *Store) DeleteSessionIfEmpty(path string) (bool, error) {
	if err := s.maybeMigrate(); err != nil {
		return false, err
	}
	deleted := false
	err := s.withLock(func() error {
		exists, lerr := pathExists(path)
		if lerr != nil || !exists {
			return lerr
		}
		sess, lerr := s.LoadSession(path)
		if lerr != nil {
			return lerr
		}
		if sess.HasComments() || sess.HasReviewedState() {
			return nil
		}
		if lerr := s.removeSessionAt(path, sess); lerr != nil {
			return lerr
		}
		deleted = true
		return nil
	})
	return deleted, err
}

// DeleteSession removes the session file at path and its manifest entry
// unconditionally, reporting false when there was no file to remove. Unlike
// DeleteSessionIfEmpty it discards a session even when it has reviewed-file
// markers; it backs the "discard reviewed-only state on quit" path.
func (s *Store) DeleteSession(path string) (bool, error) {
	if err := s.maybeMigrate(); err != nil {
		return false, err
	}
	deleted := false
	err := s.withLock(func() error {
		exists, lerr := pathExists(path)
		if lerr != nil || !exists {
			return lerr
		}
		sess, lerr := s.LoadSession(path)
		if lerr != nil {
			return lerr
		}
		if lerr := s.removeSessionAt(path, sess); lerr != nil {
			return lerr
		}
		deleted = true
		return nil
	})
	return deleted, err
}

// removeSessionAt removes path and prunes its manifest entry. The caller must
// hold the reviews-dir lock and have loaded sess from path.
func (s *Store) removeSessionAt(path string, sess *model.ReviewSession) error {
	sl, err := sessionSlug(sess)
	if err != nil {
		return err
	}
	relative := path
	if rel, rerr := filepath.Rel(s.ReviewsDir, path); rerr == nil && !strings.HasPrefix(rel, "..") {
		relative = rel
	}

	manifest := loadManifestOrDefault(s.ReviewsDir)
	if bucket, ok := manifest.Entries[sl.String()]; ok {
		kept := bucket[:0]
		for _, entry := range bucket {
			if entry.Path != relative {
				kept = append(kept, entry)
			}
		}
		if len(kept) == 0 {
			delete(manifest.Entries, sl.String())
		} else {
			manifest.Entries[sl.String()] = kept
		}
		if err := SaveManifest(s.ReviewsDir, manifest); err != nil {
			return err
		}
	}
	return os.Remove(path)
}

// LoadLatestSessionForContext looks up the persisted local session matching
// the requested context. found is false when no matching slug is in the
// manifest or when a manifest entry exists but belongs to a different
// canonical checkout (same slug, different path on disk). PR sessions are
// resolved via LoadPrSession; callers passing SourcePullRequest here get a
// not-found result rather than an error, mirroring tuicr.
func (s *Store) LoadLatestSessionForContext(
	repoPath string,
	branch *string,
	head string,
	src model.SessionDiffSource,
	commitRange []string,
) (string, *model.ReviewSession, bool, error) {
	if src == model.SourcePullRequest {
		return "", nil, false, nil
	}
	if err := s.maybeMigrate(); err != nil {
		return "", nil, false, err
	}

	// Build the slug from a transient identity session carrying the context.
	identity := model.NewReviewSession(repoPath, head, branch, src)
	identity.CommitRange = commitRange
	sl, err := sessionSlug(identity)
	if err != nil {
		return "", nil, false, err
	}

	manifest := loadManifestOrDefault(s.ReviewsDir)
	entry := manifest.GetLocal(sl.String(), canonicalPath(repoPath))
	if entry == nil {
		return "", nil, false, nil
	}
	fullPath := filepath.Join(s.ReviewsDir, entry.Path)
	sess, err := s.LoadSession(fullPath)
	if err != nil {
		return "", nil, false, err
	}
	return fullPath, sess, true, nil
}

// LoadPrSession looks up the persisted PR session for a key. found is false
// when no entry exists for the key's slug, or when the manifest's current
// head differs from the requested head (the old head's file may still be on
// disk but is not surfaced).
func (s *Store) LoadPrSession(key *forgetypes.PrSessionKey) (string, *model.ReviewSession, bool, error) {
	if err := s.maybeMigrate(); err != nil {
		return "", nil, false, err
	}
	sl := prSlugForKey(key)
	manifest := loadManifestOrDefault(s.ReviewsDir)
	entry := manifest.GetPr(sl.String())
	if entry == nil {
		return "", nil, false, nil
	}
	if _, headSHA, ok := entry.Kind.PrDetails(); !ok || headSHA != key.HeadSHA {
		return "", nil, false, nil
	}
	fullPath := filepath.Join(s.ReviewsDir, entry.Path)
	sess, err := s.LoadSession(fullPath)
	if err != nil {
		return "", nil, false, err
	}
	return fullPath, sess, true, nil
}

// ListSessions lists the persisted sessions matching a repo selector, newest
// first. A selector naming a local directory matches local sessions precisely
// by canonical path (so two checkouts of the same repo stay separate) and
// contributes an owner/repo coordinate derived from its origin remote; a
// non-directory selector is parsed directly as a forge coordinate. PR
// sessions have no checkout, so they always match by coordinate — which is
// what lets a checkout or an owner/repo selector surface its PR sessions.
func (s *Store) ListSessions(coordinate string) ([]SessionSummary, error) {
	if err := s.maybeMigrate(); err != nil {
		return nil, err
	}
	selector := resolveRepoSelector(coordinate)
	manifest := loadManifestOrDefault(s.ReviewsDir)
	active := s.activeSessionPathsOrEmpty()

	var summaries []SessionSummary
	manifest.each(func(slugStr string, entry ManifestEntry) {
		if selector.matches(slugStr, entry) {
			summaries = append(summaries, s.summaryFromEntry(slugStr, entry, active))
		}
	})
	sortSummaries(summaries)
	return summaries, nil
}

// ListAllSessions lists every persisted session (local and PR), newest first.
func (s *Store) ListAllSessions() ([]SessionSummary, error) {
	if err := s.maybeMigrate(); err != nil {
		return nil, err
	}
	manifest := loadManifestOrDefault(s.ReviewsDir)
	active := s.activeSessionPathsOrEmpty()

	var summaries []SessionSummary
	manifest.each(func(slugStr string, entry ManifestEntry) {
		summaries = append(summaries, s.summaryFromEntry(slugStr, entry, active))
	})
	sortSummaries(summaries)
	return summaries, nil
}

// activeSessionPathsOrEmpty is ActiveSessionPaths degraded to best-effort for
// listings, where a broken active file must not fail the whole listing.
func (s *Store) activeSessionPathsOrEmpty() map[string]bool {
	active, err := s.ActiveSessionPaths()
	if err != nil {
		return map[string]bool{}
	}
	return active
}

func (s *Store) summaryFromEntry(slugStr string, entry ManifestEntry, active map[string]bool) SessionSummary {
	kind := SummaryKindLocal
	if entry.Kind.IsPr() {
		kind = SummaryKindPr
	}
	fullPath := filepath.Join(s.ReviewsDir, entry.Path)
	return SessionSummary{
		Path:          fullPath,
		Slug:          slugStr,
		Kind:          kind,
		UpdatedAt:     entry.UpdatedAt,
		CommentCount:  entry.Display.CommentCount,
		ReviewedCount: entry.Display.ReviewedCount,
		FileCount:     entry.Display.FileCount,
		Anchor:        entry.Display.Anchor,
		Active:        active[normalizeActivePath(fullPath)],
		GrantedEvents: s.GrantedEventsForPath(fullPath),
	}
}

// sortSummaries orders newest first, breaking timestamp ties by slug then
// path so listings are deterministic despite map iteration order.
func sortSummaries(summaries []SessionSummary) {
	sort.SliceStable(summaries, func(i, j int) bool {
		if !summaries[i].UpdatedAt.Equal(summaries[j].UpdatedAt) {
			return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
		}
		if summaries[i].Slug != summaries[j].Slug {
			return summaries[i].Slug < summaries[j].Slug
		}
		return summaries[i].Path < summaries[j].Path
	})
}

// originRemoteURL resolves a checkout's origin remote URL. A var so tests can
// stub out the git subprocess.
var originRemoteURL = func(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", fmt.Errorf("resolve origin remote: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// repoSelector is how ListSessions interprets its argument: an optional
// canonical checkout path (directory selectors) plus an optional owner/repo
// coordinate. Local sessions match by canonical path when one is known,
// otherwise by coordinate; PR sessions always match by coordinate.
type repoSelector struct {
	canonicalPath string // empty when the selector is not a directory
	hasCoordinate bool
	coordinate    slug.RepoCoordinate
}

func resolveRepoSelector(selector string) repoSelector {
	info, err := os.Stat(selector)
	if err != nil || !info.IsDir() {
		sel := repoSelector{}
		if coord, perr := slug.ParseRepoCoordinate(selector); perr == nil {
			sel.coordinate, sel.hasCoordinate = coord, true
		}
		return sel
	}

	sel := repoSelector{canonicalPath: canonicalPath(selector)}
	if url, uerr := originRemoteURL(selector); uerr == nil && url != "" {
		if coord, perr := slug.ParseRepoCoordinate(url); perr == nil {
			sel.coordinate, sel.hasCoordinate = coord, true
			return sel
		}
	}
	// No usable origin: fall back to the directory name with no owner, the
	// same fallback tuicr's slug derivation uses.
	if base := filepath.Base(sel.canonicalPath); base != "." && base != string(filepath.Separator) {
		sel.coordinate, sel.hasCoordinate = slug.RepoCoordinate{Repo: base}, true
	}
	return sel
}

func (r repoSelector) matches(slugStr string, entry ManifestEntry) bool {
	// A PR session has no checkout, so it always matches by coordinate.
	if entry.Kind.IsPr() {
		return r.coordinateMatches(slugStr)
	}
	// A local session is tied to a physical checkout: match it by canonical
	// path when the selector named one, else by coordinate.
	if r.canonicalPath != "" {
		return entry.CanonicalRepoPath != nil && *entry.CanonicalRepoPath == r.canonicalPath
	}
	return r.coordinateMatches(slugStr)
}

func (r repoSelector) coordinateMatches(slugStr string) bool {
	if !r.hasCoordinate {
		return false
	}
	sl, err := slug.Parse(slugStr)
	if err != nil {
		return false
	}
	coord, ok := coordinateFromSlug(sl)
	if !ok {
		return false
	}
	return r.coordinate.Matches(coord)
}

// coordinateFromSlug derives the owner/repo coordinate a slug belongs to.
// For PR slugs the last two repo-path segments become owner/repo, degrading
// gracefully for nested GitLab groups and Azure DevOps projects — the same
// rule ParseRepoCoordinate applies to multi-segment inputs.
func coordinateFromSlug(sl slug.Slug) (slug.RepoCoordinate, bool) {
	local, pr := slugKindParts(sl)
	switch {
	case local != nil:
		return slug.RepoCoordinate{Owner: local.Owner, Repo: local.Repo}, true
	case pr != nil && len(pr.RepoPath) > 0:
		coord := slug.RepoCoordinate{Repo: pr.RepoPath[len(pr.RepoPath)-1]}
		if len(pr.RepoPath) > 1 {
			coord.Owner = pr.RepoPath[len(pr.RepoPath)-2]
		}
		return coord, true
	}
	return slug.RepoCoordinate{}, false
}
