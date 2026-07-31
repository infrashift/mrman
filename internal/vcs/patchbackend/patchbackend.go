// Package patchbackend reviews a standalone patch artifact — a .patch file, a
// mail message, or an mbox holding a series — as if it were a repository.
//
// The trick that makes this cheap is that a patch series is structurally a
// commit range. Each patch has a subject, an author, a date, a message body
// and a diff, which is exactly vcs.CommitInfo, so the backend presents the
// series through RecentCommits/CommitsInfo/CommitRangeDiff and every piece of
// mrman that already understands a multi-commit review works unchanged: the
// inline commit strip, walking patch by patch with ( and ), the selector's
// multi-select, per-patch comment scoping, and the changelog rendered as a
// reviewable pseudo-file.
//
// That last one matters more than it sounds. Mailing-list reviewers comment on
// the commit message as often as on the code, and presenting each patch as a
// commit is what puts the changelog on screen for free.
package patchbackend

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/patch"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
	"github.com/infrashift/mrman/internal/vcs/diffparser"
)

// Backend serves one patch artifact.
type Backend struct {
	vcs.UnsupportedBase

	source string
	opts   patch.Options
	info   vcs.Info
	series *patch.Series
}

var _ vcs.Backend = (*Backend)(nil)

// New reads a patch artifact and prepares it for review.
func New(path string, opts patch.Options) (*Backend, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	switch {
	case st.IsDir():
		return nil, &errs.InvalidInput{Detail: fmt.Sprintf(
			"%s is a directory; --patch takes a single .patch, .diff or .mbox file", path)}
	case !st.Mode().IsRegular():
		// A pipe or process substitution can only be read once, and this
		// backend re-reads the artifact on every reload — that is how :e
		// works. Failing here says so, rather than letting the second read
		// come back empty and report a perfectly good patch as unparseable.
		return nil, &errs.InvalidInput{Detail: fmt.Sprintf(
			"%s is not a regular file; --patch needs one it can re-read, so "+
				"redirect to a file first", path)}
	}

	b := &Backend{source: abs, opts: opts}
	if err := b.reload(); err != nil {
		return nil, err
	}
	return b, nil
}

// reload re-reads the artifact from disk and refreshes the derived identity.
func (b *Backend) reload() error {
	series, err := patch.LoadFile(b.source, b.opts)
	if err != nil {
		return err
	}
	b.series = series

	stem := SanitizeStem(strings.TrimSuffix(filepath.Base(b.source), filepath.Ext(b.source)))
	b.info = vcs.Info{
		// The artifact's directory, so `mrman review list` finds the review
		// from where the patch lives and the slug's repo half means something.
		RootPath: filepath.Dir(b.source),
		// Bare hex, never prefixed. A patch has no commit, and the session
		// anchor falls back to shortSHA(HeadCommit) when there is no branch —
		// anything with a colon in the first seven characters would make the
		// slug unparseable.
		HeadCommit: fmt.Sprintf("%016x", series.ContentHash),
		BranchName: &stem,
		Type:       vcs.TypePatch,
	}
	return nil
}

// Series exposes the parsed artifact, for callers that need the mail metadata
// (the review-reply exporter especially).
func (b *Backend) Series() *patch.Series { return b.series }

// Source is the absolute path of the artifact under review.
func (b *Backend) Source() string { return b.source }

// Info describes the artifact as if it were a repository.
func (b *Backend) Info() *vcs.Info { return &b.info }

// unsafeStemChars matches everything a slug anchor must not carry. A colon is
// the dangerous one — slug.Parse cuts on the first colon and then demands a
// known forge prefix — but "/" would break the segment count and "@" would
// break the project/anchor split.
var unsafeStemChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SanitizeStem reduces a filename to something safe as a slug anchor,
// collapsing runs of unsafe characters and trimming the separators that would
// read badly at either end. An empty result becomes "patch" so the anchor is
// never blank.
func SanitizeStem(stem string) string {
	cleaned := strings.Trim(unsafeStemChars.ReplaceAllString(stem, "-"), "-._")
	if cleaned == "" {
		return "patch"
	}
	return cleaned
}

// WorkingTreeDiff returns the whole artifact's diff, re-read from disk.
//
// Re-reading is what makes :e work: the reload path calls this for any source
// it does not special-case, so editing the patch file and reloading picks up
// the new content with no changes to the reload machinery.
func (b *Backend) WorkingTreeDiff(h *syntax.Highlighter) ([]model.DiffFile, error) {
	if err := b.reload(); err != nil {
		return nil, err
	}
	return b.diffFor(b.series.Patches, h)
}

// RecentCommits presents the patches as commits, newest first — the order
// selectors expect, and the reverse of how a series is posted.
func (b *Backend) RecentCommits(offset, limit int) ([]vcs.CommitInfo, error) {
	all := b.commits()
	if offset >= len(all) {
		return nil, nil
	}
	end := len(all)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return all[offset:end], nil
}

// CommitsInfo looks patches up by the ids RecentCommits handed out.
func (b *Backend) CommitsInfo(ids []string) ([]vcs.CommitInfo, error) {
	byID := make(map[string]vcs.CommitInfo, len(b.series.Patches))
	for _, c := range b.commits() {
		byID[c.ID] = c
	}
	out := make([]vcs.CommitInfo, 0, len(ids))
	for _, id := range ids {
		if c, ok := byID[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// CommitRangeDiff returns the combined diff of the selected patches, in the
// order they were posted.
func (b *Backend) CommitRangeDiff(rng vcs.ResolvedRevisionRange, h *syntax.Highlighter) ([]model.DiffFile, error) {
	if len(rng.CommitIDs) == 0 {
		return nil, errs.ErrNoChanges
	}
	wanted := make(map[string]bool, len(rng.CommitIDs))
	for _, id := range rng.CommitIDs {
		wanted[id] = true
	}

	var selected []patch.Patch
	var diffless int
	for i := range b.series.Patches {
		if !wanted[b.patchID(i)] {
			continue
		}
		selected = append(selected, b.series.Patches[i])
		if strings.TrimSpace(b.series.Patches[i].DiffText) == "" {
			diffless++
		}
	}

	// Selecting only a cover letter is a normal thing to do — it is a row in
	// the strip like any other — and "no changes to review" would send the
	// reviewer looking for an empty patch. Say what actually happened.
	if len(selected) > 0 && diffless == len(selected) {
		return nil, &errs.InvalidInput{Detail: coverLetterDetail(selected)}
	}
	return b.diffFor(selected, h)
}

// coverLetterDetail names why a selection carries nothing to review.
func coverLetterDetail(selected []patch.Patch) string {
	if len(selected) == 1 {
		return "that message is prose only — a cover letter carries no diff"
	}
	return "none of the selected messages carry a diff"
}

// ChangeStatus reports no staged or unstaged changes.
//
// This matters more than it looks: resolveChangeStatus falls back to
// WorkingTreeDiff when a backend cannot answer, which would report the
// patch's own files as "unstaged changes" and offer a target-selector row
// that fails the moment it is chosen. Answering plainly is the fix.
func (b *Backend) ChangeStatus() (vcs.ChangeStatus, error) {
	return vcs.ChangeStatus{}, nil
}

// FetchContextLines returns nothing: a patch carries only the context inside
// its hunks, and there is no tree to read the rest from.
//
// Returning empty rather than an error is deliberate — the app suppresses the
// expanders for a patch source anyway, and a stray call should quietly do
// nothing rather than surface a failure the reviewer cannot act on.
func (b *Backend) FetchContextLines(string, model.FileStatus, *string, uint32, uint32) ([]model.DiffLine, error) {
	return nil, nil
}

// FileLineCount is unknowable for a patch: the file it applies to is not here.
// The error is the honest answer, and callers treat it as "no EOF gap".
func (b *Backend) FileLineCount(string, model.FileStatus, *string) (uint32, error) {
	return 0, errs.Unsupportedf("file length of a patched file")
}

// diffFor parses the given patches into diff files, tagging every file with
// the patch it came from so comments can be scoped per patch.
func (b *Backend) diffFor(patches []patch.Patch, h *syntax.Highlighter) ([]model.DiffFile, error) {
	// Index the patches by identity once, so a file can be tagged with the
	// patch it came from without re-deriving the id per file.
	idFor := make(map[string]string, len(b.series.Patches))
	for i := range b.series.Patches {
		idFor[b.series.Patches[i].DiffText] = b.patchID(i)
	}

	var out []model.DiffFile
	for _, p := range patches {
		if strings.TrimSpace(p.DiffText) == "" {
			continue // a cover letter carries prose, not code
		}
		files, err := diffparser.Parse(p.DiffText, diffparser.GitStyle, h)
		if err != nil {
			return nil, err
		}
		for i := range files {
			// Two patches touching one path yield two entries with the same
			// display path; the patch id is what keeps their comments apart.
			files[i].CommitID = idFor[p.DiffText]
			// Parse numbers files from zero per patch, so renumber across the
			// series — otherwise every patch claims index 0 and anything
			// ordering by source position has ambiguous keys.
			files[i].SourceIndex = len(out) + i
		}
		out = append(out, files...)
	}
	if len(out) == 0 {
		return nil, errs.ErrNoChanges
	}
	return out, nil
}

// commits renders the patches as CommitInfo rows, newest first.
func (b *Backend) commits() []vcs.CommitInfo {
	out := make([]vcs.CommitInfo, 0, len(b.series.Patches))
	for i := len(b.series.Patches) - 1; i >= 0; i-- {
		p := b.series.Patches[i]
		id := b.patchID(i)
		body := p.Changelog
		when := p.Date
		if when.IsZero() {
			when = time.Time{}
		}
		out = append(out, vcs.CommitInfo{
			ID:      id,
			ShortID: shortPatchID(i, p),
			Summary: patchSummary(i, p),
			Body:    &body,
			Author:  p.Author,
			Time:    when,
		})
	}
	return out
}

// patchID is a patch's stable identity within the artifact. The Message-Id is
// preferred because it is the identity the wider world uses; the position is
// the fallback, since plain `git format-patch` writes no Message-Id.
func (b *Backend) patchID(i int) string {
	if id := b.series.Patches[i].MessageID; id != "" {
		return id
	}
	return fmt.Sprintf("patch-%04d", i+1)
}

// shortPatchID is the compact form shown in the commit strip: "2/7" for a
// series member, otherwise a plain index.
func shortPatchID(i int, p patch.Patch) string {
	if p.SeriesLen > 0 {
		return fmt.Sprintf("%d/%d", p.SeriesPos, p.SeriesLen)
	}
	return fmt.Sprintf("%d", i+1)
}

// patchSummary is the subject line, falling back to a positional label for a
// bare diff that carries no mail headers at all.
func patchSummary(i int, p patch.Patch) string {
	if p.Subject != "" {
		return p.Subject
	}
	return fmt.Sprintf("patch %d", i+1)
}
