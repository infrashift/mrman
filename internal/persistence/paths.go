// Package persistence implements mrman's slug-addressed storage layer for
// review sessions, ported from tuicr's src/persistence.
//
// Layout under the data dir's mrman/reviews/:
//
//	reviews/
//	  index.json            # manifest, source of truth for lookups
//	  active_sessions.json  # which sessions live TUIs have open
//	  sessions/
//	    <16-hex>.json       # one file per session, deterministic name
//
// The session filename is a hash of the slug plus the canonical repo path
// (for local) or head SHA (for PR), so the same logical session always lands
// at the same path without consulting the manifest. The manifest is the
// authoritative slug -> file mapping; if it goes missing or corrupts, the
// session JSONs are self-describing and the manifest can be rebuilt by
// walking sessions/.
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create reviews dir: %w", err)
	}
	s := &Store{ReviewsDir: dir}
	if err := s.maybeMigrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// maybeMigrate moves a pre-flat-layout reviews dir aside on first run. The
// current layout is identified by the presence of the sessions/ subdirectory;
// if it is missing but the reviews dir has any other contents, the whole
// directory is renamed to <reviews>.bak1 (.bak2, ...) and a fresh one is
// created.
func (s *Store) maybeMigrate() error {
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
	if err := os.MkdirAll(s.ReviewsDir, 0o755); err != nil {
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
// reviews dir. Files live in a flat sessions/ directory; the name is the
// FNV-1a 64 hash of identity-defining inputs:
//
//   - Local: "local|" + slug + "|" + canonical repo path. Two checkouts of
//     the same repo produce distinct hashes because their canonical paths
//     differ.
//   - PR: "pr|" + slug + "|" + head SHA. A new head produces a new file.
func relativePathForSession(sl slug.Slug, sess *model.ReviewSession) (string, error) {
	local, pr := slugKindParts(sl)
	h := fnv.New64a()
	switch {
	case local != nil:
		_, _ = io.WriteString(h, "local|")
		_, _ = io.WriteString(h, sl.String())
		_, _ = io.WriteString(h, "|")
		_, _ = io.WriteString(h, canonicalPathForHash(sess.RepoPath))
	case pr != nil:
		key := sess.PrSessionKey
		if key == nil {
			return "", &errs.CorruptedSession{
				Detail: "PR slug requires session.pr_session_key to be populated",
			}
		}
		_, _ = io.WriteString(h, "pr|")
		_, _ = io.WriteString(h, sl.String())
		_, _ = io.WriteString(h, "|")
		_, _ = io.WriteString(h, key.HeadSHA)
	default:
		return "", &errs.CorruptedSession{Detail: fmt.Sprintf("unrecognized slug type %T", sl)}
	}
	return filepath.Join(SessionsDirname, fmt.Sprintf("%016x.json", h.Sum64())), nil
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
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(parent, fmt.Sprintf(".%s.%s.tmp", filepath.Base(path), uuid.NewString()))
	f, err := os.Create(tmp)
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
