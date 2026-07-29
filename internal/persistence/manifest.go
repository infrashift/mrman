package persistence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
)

// Manifest and layout constants.
const (
	// ManifestFilename is the manifest file's name inside the reviews dir.
	ManifestFilename = "index.json"
	// ManifestVersion is the persisted manifest schema version.
	ManifestVersion = "2.0"
	// SessionsDirname is the subdirectory inside reviews/ where session JSON
	// files live under the flat layout. Its presence signals "current layout"
	// to the migration check.
	SessionsDirname = "sessions"
)

// Manifest is the source of truth for slug -> session-file lookups. It lives
// at <reviews_dir>/index.json and carries enough denormalized metadata to
// drive session listings without opening every session JSON.
//
// A slug may map to more than one ManifestEntry when two local checkouts of
// the same repo are both under review (same slug, different canonical paths).
// PR slugs always have at most one entry: the current head's session.
type Manifest struct {
	// Version is the manifest schema version, currently "2.0".
	Version string `json:"version"`
	// Entries maps slug strings to their session entries.
	Entries map[string][]ManifestEntry `json:"entries"`
}

// ManifestEntry describes one persisted session file.
type ManifestEntry struct {
	// Path to the session JSON, relative to the reviews directory.
	Path string `json:"path"`
	// Kind discriminates local sessions from PR sessions.
	Kind ManifestKind `json:"kind"`
	// UpdatedAt mirrors the session's updated_at for sorting listings.
	UpdatedAt time.Time `json:"updated_at"`
	// CanonicalRepoPath is the canonical path of the local checkout this
	// session belongs to; nil for PR sessions. It disambiguates two checkouts
	// of the same repo when their slugs collide.
	CanonicalRepoPath *string `json:"canonical_repo_path"`
	// Display carries denormalized listing metadata.
	Display DisplayMetadata `json:"display"`
}

// DisplayMetadata is denormalized session metadata for listings.
type DisplayMetadata struct {
	// CommentCount is the total comment count across all scopes.
	CommentCount int `json:"comment_count"`
	// ReviewedCount is the number of files marked reviewed.
	ReviewedCount int `json:"reviewed_count"`
	// FileCount is the number of files in the session.
	FileCount int `json:"file_count"`
	// Anchor is the slug anchor segment (branch name, short SHA, or pr/<n>),
	// cached so listings need not re-parse the slug.
	Anchor string `json:"anchor"`
}

// ManifestKind is the tagged discriminator of a manifest entry, serialized as
// {"type":"local"} or {"type":"pr","number":N,"head_sha":"..."} to match
// tuicr's serde representation.
type ManifestKind struct {
	pr      bool
	number  uint64
	headSHA string
}

// LocalManifestKind returns the kind tagging a local session entry.
func LocalManifestKind() ManifestKind {
	return ManifestKind{}
}

// PrManifestKind returns the kind tagging a PR session entry at a head SHA.
func PrManifestKind(number uint64, headSHA string) ManifestKind {
	return ManifestKind{pr: true, number: number, headSHA: headSHA}
}

// IsLocal reports whether this kind tags a local session.
func (k ManifestKind) IsLocal() bool { return !k.pr }

// IsPr reports whether this kind tags a PR session.
func (k ManifestKind) IsPr() bool { return k.pr }

// PrDetails returns the PR number and head SHA; ok is false for local kinds.
func (k ManifestKind) PrDetails() (number uint64, headSHA string, ok bool) {
	return k.number, k.headSHA, k.pr
}

type manifestKindJSON struct {
	Type    string  `json:"type"`
	Number  *uint64 `json:"number,omitempty"`
	HeadSHA *string `json:"head_sha,omitempty"`
}

// MarshalJSON serializes the kind in tuicr's internally tagged form.
func (k ManifestKind) MarshalJSON() ([]byte, error) {
	if !k.pr {
		return json.Marshal(manifestKindJSON{Type: "local"})
	}
	number := k.number
	headSHA := k.headSHA
	return json.Marshal(manifestKindJSON{Type: "pr", Number: &number, HeadSHA: &headSHA})
}

// UnmarshalJSON parses the internally tagged form; unknown tags are errors.
func (k *ManifestKind) UnmarshalJSON(data []byte) error {
	var raw manifestKindJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch raw.Type {
	case "local":
		*k = LocalManifestKind()
	case "pr":
		var number uint64
		var headSHA string
		if raw.Number != nil {
			number = *raw.Number
		}
		if raw.HeadSHA != nil {
			headSHA = *raw.HeadSHA
		}
		*k = PrManifestKind(number, headSHA)
	default:
		return fmt.Errorf("unknown manifest kind %q", raw.Type)
	}
	return nil
}

// NewManifest returns an empty manifest at the current schema version.
func NewManifest() *Manifest {
	return &Manifest{Version: ManifestVersion, Entries: map[string][]ManifestEntry{}}
}

// GetLocal finds the local entry for slug that matches canonicalRepoPath.
func (m *Manifest) GetLocal(slugStr, canonicalRepoPath string) *ManifestEntry {
	for i, e := range m.Entries[slugStr] {
		if e.Kind.IsLocal() && e.CanonicalRepoPath != nil && *e.CanonicalRepoPath == canonicalRepoPath {
			return &m.Entries[slugStr][i]
		}
	}
	return nil
}

// GetPr finds the PR entry for slug (PR entries are singletons per slug).
func (m *Manifest) GetPr(slugStr string) *ManifestEntry {
	for i, e := range m.Entries[slugStr] {
		if e.Kind.IsPr() {
			return &m.Entries[slugStr][i]
		}
	}
	return nil
}

// Upsert inserts or replaces an entry. For local entries the matching key is
// (slug, canonical repo path); for PR entries the slug alone suffices — a new
// head replaces the previous entry regardless of its stored head.
func (m *Manifest) Upsert(slugStr string, entry ManifestEntry) {
	if m.Entries == nil {
		m.Entries = map[string][]ManifestEntry{}
	}
	bucket := m.Entries[slugStr]
	for i, existing := range bucket {
		switch {
		case existing.Kind.IsPr() && entry.Kind.IsPr():
		case existing.Kind.IsLocal() && entry.Kind.IsLocal() &&
			equalOptString(existing.CanonicalRepoPath, entry.CanonicalRepoPath):
		default:
			continue
		}
		bucket[i] = entry
		return
	}
	m.Entries[slugStr] = append(bucket, entry)
}

func equalOptString(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// RemoveAll removes every entry for slug, returning the removed entries
// (empty when the slug is absent).
func (m *Manifest) RemoveAll(slugStr string) []ManifestEntry {
	removed := m.Entries[slugStr]
	delete(m.Entries, slugStr)
	return removed
}

// each visits every (slug, entry) pair.
func (m *Manifest) each(visit func(slugStr string, entry ManifestEntry)) {
	for slugStr, bucket := range m.Entries {
		for _, entry := range bucket {
			visit(slugStr, entry)
		}
	}
}

// Len is the total entry count across all slugs (a slug with two local
// entries counts twice).
func (m *Manifest) Len() int {
	n := 0
	for _, bucket := range m.Entries {
		n += len(bucket)
	}
	return n
}

// IsEmpty reports whether the manifest holds no entries.
func (m *Manifest) IsEmpty() bool { return m.Len() == 0 }

// LoadManifest reads <reviewsDir>/index.json. A missing file yields an empty
// manifest; an unparseable file yields *errs.CorruptedSession (callers may
// recover by rebuilding from session files).
func LoadManifest(reviewsDir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(reviewsDir, ManifestFilename))
	if errors.Is(err, fs.ErrNotExist) {
		return NewManifest(), nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, &errs.CorruptedSession{Detail: fmt.Sprintf("manifest parse error: %v", err)}
	}
	if m.Entries == nil {
		m.Entries = map[string][]ManifestEntry{}
	}
	return &m, nil
}

// loadManifestOrDefault loads the manifest, treating any failure as an empty
// manifest (mirroring tuicr's pervasive unwrap_or_default).
func loadManifestOrDefault(reviewsDir string) *Manifest {
	m, err := LoadManifest(reviewsDir)
	if err != nil {
		return NewManifest()
	}
	return m
}

// SaveManifest writes the manifest atomically: content goes to a sibling
// index.json.tmp which is then renamed over index.json, so a concurrent
// reader sees either the old version or the new one, never a partial write.
func SaveManifest(reviewsDir string, m *Manifest) error {
	if err := os.MkdirAll(reviewsDir, 0o755); err != nil {
		return err
	}
	data, err := marshalPretty(m)
	if err != nil {
		return err
	}
	tmp := filepath.Join(reviewsDir, ManifestFilename+".tmp")
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
	return os.Rename(tmp, filepath.Join(reviewsDir, ManifestFilename))
}

// entryFromSession builds a manifest entry for a session stored at
// relativePath under the reviews dir. The caller supplies the slug (the
// manifest key) separately and the display anchor.
func entryFromSession(sess *model.ReviewSession, relativePath, anchor string) ManifestEntry {
	kind := LocalManifestKind()
	var canonical *string
	if key := sess.PrSessionKey; key != nil {
		kind = PrManifestKind(key.Number, key.HeadSHA)
	} else {
		c := canonicalPath(sess.RepoPath)
		canonical = &c
	}

	commentCount := len(sess.ReviewComments)
	for _, f := range sess.Files {
		commentCount += f.CommentCount()
	}

	return ManifestEntry{
		Path:              relativePath,
		Kind:              kind,
		UpdatedAt:         sess.UpdatedAt,
		CanonicalRepoPath: canonical,
		Display: DisplayMetadata{
			CommentCount:  commentCount,
			ReviewedCount: sess.ReviewedCount(),
			FileCount:     len(sess.Files),
			Anchor:        anchor,
		},
	}
}

// RebuildFromFiles walks every .json file under reviewsDir (skipping the
// manifest and the active-sessions file) and invokes extract to build a
// (slug, entry) pair for each; files extract rejects are skipped. It returns
// the rebuilt manifest, which is empty when the directory does not exist.
func RebuildFromFiles(reviewsDir string, extract func(path string) (string, ManifestEntry, bool)) (*Manifest, error) {
	m := NewManifest()
	if _, err := os.Stat(reviewsDir); errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	err := walkJSON(reviewsDir, func(path string) {
		if slugStr, entry, ok := extract(path); ok {
			m.Upsert(slugStr, entry)
		}
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// walkJSON visits every *.json file under dir recursively, excluding the
// manifest file and the active-sessions file.
func walkJSON(dir string, visit func(path string)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if err := walkJSON(path, visit); err != nil {
				return err
			}
			continue
		}
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		if name == ManifestFilename || name == activeSessionsFilename {
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		visit(path)
	}
	return nil
}
