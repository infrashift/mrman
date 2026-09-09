// Package persistence implements mrman's slug-addressed storage layer for
// review sessions, ported from tuicr's src/persistence.
//
// Layout under the data dir's mrman/reviews/:
//
//	reviews/
//	  index.json            # manifest, source of truth for lookups
//	  active_sessions.json  # which sessions live TUIs have open
//	  sessions/
//	    <repo>@<what>-<16-hex>.json   # one file per session
//
// e.g. infrashift-mrman@main-worktree-abc1234-3f2a1b0c9d8e7f60.json, or
// infrashift-mrman@github.com-pr-12-a1b2c3d4e5f60718.json.
//
// The trailing hash is the identity: the slug plus the canonical repo path
// (local) or head SHA (PR), so the same logical session always lands at the
// same path without consulting the manifest. The leading label is for whoever
// has to live in this directory — it puts the repo first, spelled the same for
// a repo's local and PR sessions, so `ls` is readable and one glob prunes or
// backs up a single repo's reviews. It is decoration and is never parsed back.
//
// The manifest is the authoritative slug -> file mapping; if it goes missing
// or corrupts, the session JSONs are self-describing and the manifest can be
// rebuilt by walking sessions/. Its version doubles as the layout marker, so
// an older store's bare-hash filenames are renamed in place on first open.
package persistence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/google/uuid"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/slug"
)

// nowFn is an injection seam for deterministic time-dependent tests.
var nowFn = time.Now

// Everything under the reviews dir is private to the user: a session holds
// the diff context of whatever was reviewed, which may be a private
// repository, and the comments written about it. The umask is not enough —
// the common 022 leaves files world-readable.
const (
	dirMode  = 0o700
	fileMode = 0o600
)

// Store is the persistence layer rooted at one reviews directory. Tests point
// ReviewsDir at a t.TempDir(); production code uses NewDefaultStore.
type Store struct {
	// ReviewsDir is the absolute path of the reviews directory.
	ReviewsDir string
}

// NewDefaultStore opens the platform-default store at
// $XDG_DATA_HOME/mrman/reviews, creating it if needed and migrating any
// pre-flat layout aside.
func NewDefaultStore() (*Store, error) {
	dir := filepath.Join(xdg.DataHome, "mrman", "reviews")
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("create reviews dir: %w", err)
	}
	s := &Store{ReviewsDir: dir}
	s.hardenPermissions()
	if err := s.maybeMigrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// hardenPermissions brings a store written by an earlier mrman, which
// created world-readable files, down to owner-only. Best effort and
// effectively once: when the manifest is already private the walk is
// skipped. Windows has no POSIX modes to fix.
func (s *Store) hardenPermissions() {
	if runtime.GOOS == "windows" {
		return
	}
	if info, err := os.Stat(filepath.Join(s.ReviewsDir, ManifestFilename)); err == nil &&
		info.Mode().Perm()&0o077 == 0 {
		return
	}
	// Walk and chmod through an os.Root so a symlink planted in the store
	// cannot redirect the chmod outside it; symlinks themselves are skipped.
	root, err := os.OpenRoot(s.ReviewsDir)
	if err != nil {
		return
	}
	defer func() { _ = root.Close() }()
	_ = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&fs.ModeSymlink != 0 {
			return nil //nolint:nilerr // best effort: skip what cannot be read or is a link
		}
		mode := fs.FileMode(fileMode)
		if d.IsDir() {
			mode = dirMode
		}
		_ = root.Chmod(path, mode)
		return nil
	})
}

// maybeMigrate brings the reviews dir up to the current layout. Two steps,
// deliberately different in kind: a pre-sessions/ layout is moved aside
// wholesale because its files cannot be mapped onto the current identity
// scheme, whereas the rename from hashed to readable filenames is a real
// migration that keeps every session.
func (s *Store) maybeMigrate() error {
	if err := s.migrateLegacyLayout(); err != nil {
		return err
	}
	return s.migrateSessionNames()
}

// migrateSessionNames renames bare-hash session files to the readable
// <repo>@<descriptor>-<hash> form and rewrites the manifest to match.
//
// Nothing is lost and nothing is moved aside: the identity hash is unchanged,
// so each file keeps its identity and only gains a prefix. The manifest is
// rebuilt from the session files themselves afterwards rather than patched,
// because the files are self-describing and that also repairs any entry whose
// path had already drifted.
func (s *Store) migrateSessionNames() error {
	manifest, err := LoadManifest(s.ReviewsDir)
	if err != nil || manifest.Version != manifestVersionHashedNames {
		// A fresh store reports the current version, and a corrupt manifest is
		// not something to rename files on the strength of.
		return nil //nolint:nilerr // deliberate: nothing to migrate
	}
	return s.withLock(func() error {
		// Another process may have migrated while this one waited for the lock.
		current, lerr := LoadManifest(s.ReviewsDir)
		if lerr != nil || current.Version != manifestVersionHashedNames {
			return nil //nolint:nilerr // deliberate: already migrated
		}
		sessionsDir := filepath.Join(s.ReviewsDir, SessionsDirname)
		if _, serr := os.Stat(sessionsDir); errors.Is(serr, fs.ErrNotExist) {
			return s.stampManifestVersion(current)
		}
		if rerr := s.renameSessionFiles(sessionsDir); rerr != nil {
			return rerr
		}
		rebuilt, rerr := RebuildFromFiles(s.ReviewsDir, s.extractManifestEntry)
		if rerr != nil {
			return rerr
		}
		return SaveManifest(s.ReviewsDir, rebuilt)
	})
}

// renameSessionFiles moves every session file in dir to its derived name,
// skipping any it cannot read or that is already correctly named. A file that
// resists is left where it is: the manifest rebuild that follows indexes it at
// whatever path it actually has, so a stubborn session stays reachable.
func (s *Store) renameSessionFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		oldPath := filepath.Join(dir, entry.Name())
		sess, lerr := s.LoadSession(oldPath)
		if lerr != nil {
			continue
		}
		sl, serr := sessionSlug(sess)
		if serr != nil {
			continue
		}
		relative, rerr := relativePathForSession(sl, sess)
		if rerr != nil {
			continue
		}
		newPath := filepath.Join(s.ReviewsDir, relative)
		if newPath == oldPath {
			continue
		}
		if _, statErr := os.Stat(newPath); statErr == nil {
			// The destination is taken by a session with the same identity;
			// the duplicate is redundant, not worth clobbering a file over.
			continue
		}
		if rerr := os.Rename(oldPath, newPath); rerr != nil {
			return rerr
		}
	}
	return nil
}

// extractManifestEntry builds a (slug, entry) pair for one session file, for
// the manifest rebuild. ok is false for anything unreadable.
func (s *Store) extractManifestEntry(path string) (string, ManifestEntry, bool) {
	sess, err := s.LoadSession(path)
	if err != nil {
		return "", ManifestEntry{}, false
	}
	sl, err := sessionSlug(sess)
	if err != nil {
		return "", ManifestEntry{}, false
	}
	relative, err := filepath.Rel(s.ReviewsDir, path)
	if err != nil {
		return "", ManifestEntry{}, false
	}
	return sl.String(), entryFromSession(sess, relative, manifestAnchorFor(sl)), true
}

// stampManifestVersion records the current layout version without touching
// entries, for a store that has nothing to rename.
func (s *Store) stampManifestVersion(m *Manifest) error {
	m.Version = ManifestVersion
	return SaveManifest(s.ReviewsDir, m)
}

// migrateLegacyLayout moves a pre-flat-layout reviews dir aside on first run.
// The flat layout is identified by the presence of the sessions/ subdirectory;
// if it is missing but the reviews dir has any other contents, the whole
// directory is renamed to <reviews>.bak1 (.bak2, ...) and a fresh one is
// created.
func (s *Store) migrateLegacyLayout() error {
	if _, err := os.Stat(s.ReviewsDir); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(s.ReviewsDir, SessionsDirname)); err == nil {
		return nil
	}
	entries, err := os.ReadDir(s.ReviewsDir)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	parent := filepath.Dir(s.ReviewsDir)
	stem := filepath.Base(s.ReviewsDir)
	var backup string
	for suffix := 1; ; suffix++ {
		backup = filepath.Join(parent, fmt.Sprintf("%s.bak%d", stem, suffix))
		if _, err := os.Stat(backup); errors.Is(err, fs.ErrNotExist) {
			break
		}
	}

	if err := os.Rename(s.ReviewsDir, backup); err != nil {
		return fmt.Errorf("migrate reviews dir: %w", err)
	}
	if err := os.MkdirAll(s.ReviewsDir, dirMode); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr,
		"[mrman] migrating reviews to new layout; previous reviews moved to %s\n", backup)
	return nil
}

// canonicalPath resolves p to an absolute, symlink-free path, falling back to
// the raw input when resolution fails (e.g. the path does not exist).
func canonicalPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return p
	}
	return resolved
}

// canonicalPathForHash is the canonical path as used in session filename
// hashing: lowercased on Windows so case-insensitive checkouts collide.
func canonicalPathForHash(p string) string {
	c := canonicalPath(p)
	if runtime.GOOS == "windows" {
		c = strings.ToLower(c)
	}
	return c
}

// sessionSlug derives the slug for a session, wrapping derivation failures as
// session corruption like tuicr does.
func sessionSlug(sess *model.ReviewSession) (slug.Slug, error) {
	sl, err := slug.ForSession(sess)
	if err != nil {
		return nil, &errs.CorruptedSession{Detail: fmt.Sprintf("slug derive: %v", err)}
	}
	return sl, nil
}

// slugKindParts down-casts a slug to its concrete kind, tolerating both value
// and pointer forms.
func slugKindParts(sl slug.Slug) (local *slug.LocalSlug, pr *slug.PrSlug) {
	switch v := sl.(type) {
	case slug.LocalSlug:
		return &v, nil
	case *slug.LocalSlug:
		return v, nil
	case slug.PrSlug:
		return nil, &v
	case *slug.PrSlug:
		return nil, v
	}
	return nil, nil
}

// prSlugForKey builds the PR slug identifying a PR session key.
func prSlugForKey(key *forgetypes.PrSessionKey) slug.PrSlug {
	return slug.PrSlug{
		Forge:    key.Repository.Kind,
		Host:     key.Repository.Host,
		RepoPath: key.Repository.PathSegments(),
		Number:   key.Number,
	}
}

// manifestAnchorFor is the display anchor cached in manifest entries: the
// local slug's anchor segment, or pr/<number> for PR slugs.
func manifestAnchorFor(sl slug.Slug) string {
	local, pr := slugKindParts(sl)
	switch {
	case pr != nil:
		return fmt.Sprintf("pr/%d", pr.Number)
	case local != nil:
		return local.Anchor.String()
	}
	return ""
}

// relativePathForSession computes the session file's path relative to the
// reviews dir. Files live in a flat sessions/ directory, named
//
//	<repo>@<descriptor>-<16hex>.json
//
// The trailing hash is the identity — the FNV-1a 64 of identity-defining
// inputs, and the only part correctness depends on:
//
//   - Local: "local|" + slug + "|" + canonical repo path. Two checkouts of
//     the same repo produce distinct hashes because their canonical paths
//     differ.
//   - PR: "pr|" + slug + "|" + head SHA. A new head produces a new file.
//
// Everything before it is for the human holding the directory. A pile of bare
// hashes cannot be pruned, backed up, or even read: there is no way to tell
// which repo a session belongs to without opening it. The repo token comes
// first and is spelled the same for a repo's local and PR sessions, so one
// glob reaches all of them:
//
//	rm ~/.local/share/mrman/reviews/sessions/infrashift-mrman@*
//
// The name is decoration and must never be parsed back. It is derived from
// the slug, so a renamed remote or a new branch changes it — which is exactly
// what the identity hash is there to survive.
func relativePathForSession(sl slug.Slug, sess *model.ReviewSession) (string, error) {
	local, pr := slugKindParts(sl)
	h := fnv.New64a()
	var label string
	switch {
	case local != nil:
		_, _ = io.WriteString(h, "local|")
		_, _ = io.WriteString(h, sl.String())
		_, _ = io.WriteString(h, "|")
		_, _ = io.WriteString(h, canonicalPathForHash(sess.RepoPath))
		label = localSessionLabel(*local)
	case pr != nil:
		key := sess.PrSessionKey
		if key == nil {
			return "", &errs.CorruptedSession{
				Detail: "MR slug requires session.pr_session_key to be populated",
			}
		}
		_, _ = io.WriteString(h, "pr|")
		_, _ = io.WriteString(h, sl.String())
		_, _ = io.WriteString(h, "|")
		_, _ = io.WriteString(h, key.HeadSHA)
		label = prSessionLabel(*pr)
	default:
		return "", &errs.CorruptedSession{Detail: fmt.Sprintf("unrecognized slug type %T", sl)}
	}
	name := fmt.Sprintf("%s-%016x.json", sanitizeFilenamePart(label), h.Sum64())
	return filepath.Join(SessionsDirname, name), nil
}

// sessionLabelMaxLen caps the descriptive part of a session filename. Branch
// names have no practical length limit and filesystems do (255 bytes is the
// common ceiling), so the label is truncated and the hash — which is what
// actually identifies the session — is always kept.
const sessionLabelMaxLen = 100

// localSessionLabel renders a local slug as <owner>-<repo>@<anchor>-<source>.
func localSessionLabel(sl slug.LocalSlug) string {
	repo := sl.Repo
	if sl.Owner != "" {
		repo = sl.Owner + "-" + sl.Repo
	}
	return repo + "@" + sl.Anchor.String() + "-" + sl.Source.String()
}

// prSessionLabel renders a PR slug as <owner>-<repo>@<host>-pr-<n>.
//
// The repo token is the last two path segments, matching how
// slug.RepoCoordinate reduces a repo path — so a repo's PR sessions sort and
// glob alongside its local ones instead of under the forge prefix. The host
// stays in the descriptor because two hosts can carry the same owner/repo.
func prSessionLabel(sl slug.PrSlug) string {
	repo := strings.Join(sl.RepoPath, "-")
	if n := len(sl.RepoPath); n >= 2 {
		repo = sl.RepoPath[n-2] + "-" + sl.RepoPath[n-1]
	}
	return fmt.Sprintf("%s@%s-pr-%d", repo, sl.Host, sl.Number)
}

// sanitizeFilenamePart makes a label safe on every filesystem mrman runs on:
// only ASCII alphanumerics and ._~@- survive, runs of "-" collapse, and the
// result is trimmed of leading/trailing separators (a leading dot would make
// the session a hidden file) and truncated.
//
// Collisions are fine and expected — two labels that sanitize alike are still
// separated by the identity hash the caller appends.
func sanitizeFilenamePart(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		safe := r == '.' || r == '_' || r == '~' || r == '@' || r == '-' ||
			(r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		if !safe {
			r = '-'
		}
		if r == '-' {
			if lastDash {
				continue
			}
			lastDash = true
		} else {
			lastDash = false
		}
		b.WriteRune(r)
	}
	out := strings.Trim(b.String(), "-.")
	if len(out) > sessionLabelMaxLen {
		out = strings.Trim(out[:sessionLabelMaxLen], "-.")
	}
	if out == "" {
		return "session"
	}
	return out
}

// marshalPretty renders v as 2-space-indented JSON without HTML escaping and
// with a trailing newline, matching serde_json::to_string_pretty as closely
// as encoding/json allows.
func marshalPretty(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeAtomic writes bytes to path via a same-directory temp file, fsync, and
// rename, so concurrent readers see either the old or the new content.
func writeAtomic(path string, data []byte) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, dirMode); err != nil {
		return err
	}
	tmp := filepath.Join(parent, fmt.Sprintf(".%s.%s.tmp", filepath.Base(path), uuid.NewString()))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	_ = f.Sync() // best-effort, matching tuicr's sync_all().ok()
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
