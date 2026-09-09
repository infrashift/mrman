package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
	"github.com/infrashift/mrman/internal/vcs/diffparser"
)

// contentSource says where a diff side's full file content can be read
// from, for the container-grammar full-file re-highlight pass.
type contentSource struct {
	kind sourceKind
	rev  string
}

type sourceKind int

const (
	sourceNone sourceKind = iota
	sourceWorkdir
	sourceIndex
	sourceRevision
)

func revisionSource(rev string) contentSource {
	return contentSource{kind: sourceRevision, rev: rev}
}

// WorkingTreeDiff diffs HEAD against the working tree, including untracked
// files as synthetic addition-only diffs.
func (b *Backend) WorkingTreeDiff(h *syntax.Highlighter) ([]model.DiffFile, error) {
	return b.cliDiff(
		[]string{"diff", "--no-ext-diff", "--binary", "HEAD", "--"},
		true,
		revisionSource("HEAD"),
		contentSource{kind: sourceWorkdir},
		h,
	)
}

// StagedDiff diffs HEAD against the index.
func (b *Backend) StagedDiff(h *syntax.Highlighter) ([]model.DiffFile, error) {
	oldSource := contentSource{kind: sourceNone}
	if _, err := b.git("rev-parse", "--verify", "HEAD"); err == nil {
		oldSource = revisionSource("HEAD")
	}
	return b.cliDiff(
		[]string{"diff", "--no-ext-diff", "--binary", "--cached", "--"},
		false,
		oldSource,
		contentSource{kind: sourceIndex},
		h,
	)
}

// UnstagedDiff diffs the index against the working tree, including
// untracked files.
func (b *Backend) UnstagedDiff(h *syntax.Highlighter) ([]model.DiffFile, error) {
	return b.cliDiff(
		[]string{"diff", "--no-ext-diff", "--binary", "--"},
		true,
		contentSource{kind: sourceIndex},
		contentSource{kind: sourceWorkdir},
		h,
	)
}

// CommitRangeDiff diffs the resolved revision range. CommitList targets
// diff the oldest commit's parent against the newest commit; explicit
// targets diff base..head, with a nil base meaning the empty tree.
func (b *Backend) CommitRangeDiff(rng vcs.ResolvedRevisionRange, h *syntax.Highlighter) ([]model.DiffFile, error) {
	if len(rng.CommitIDs) == 0 {
		return nil, errs.ErrNoChanges
	}

	var baseRev, newestRev string
	if rng.Target.Explicit {
		baseRev = emptyTreeOID
		if rng.Target.Base != nil {
			baseRev = *rng.Target.Base
		}
		newestRev = rng.Target.Head
	} else {
		baseRev = b.parentRevOrEmpty(rng.CommitIDs[0])
		newestRev = rng.CommitIDs[len(rng.CommitIDs)-1]
	}
	return b.cliDiff(
		[]string{"diff", "--no-ext-diff", "--binary", baseRev, newestRev, "--"},
		false,
		revisionSource(baseRev),
		revisionSource(newestRev),
		h,
	)
}

// WorkingTreeWithCommitsDiff diffs the parent of the oldest selected commit
// against the working tree, including untracked files.
func (b *Backend) WorkingTreeWithCommitsDiff(ids []string, h *syntax.Highlighter) ([]model.DiffFile, error) {
	if len(ids) == 0 {
		return nil, errs.ErrNoChanges
	}

	baseRev := b.parentRevOrEmpty(ids[0])
	return b.cliDiff(
		[]string{"diff", "--no-ext-diff", "--binary", baseRev, "--"},
		true,
		revisionSource(baseRev),
		contentSource{kind: sourceWorkdir},
		h,
	)
}

// cliDiff runs a `git diff` invocation, parses it, optionally appends
// synthetic diffs for untracked files, and applies the container-grammar
// full-file re-highlight pass.
func (b *Backend) cliDiff(args []string, includeUntracked bool, oldSource, newSource contentSource, h *syntax.Highlighter) ([]model.DiffFile, error) {
	args = b.withDiffFlags(args)
	files, err := b.runDiff(args, h)
	if err != nil {
		return nil, err
	}

	if includeUntracked {
		files, err = b.appendUntrackedDiffs(files, h)
		if err != nil {
			return nil, err
		}
	}
	normalizeGitCliPaths(files)

	if len(files) == 0 {
		return nil, errs.ErrNoChanges
	}

	b.enhanceWithFullFileHighlight(files, h,
		func(path string) (string, bool) { return b.readPathFromSource(oldSource, path) },
		func(path string) (string, bool) { return b.readPathFromSource(newSource, path) },
	)
	return files, nil
}

// withDiffFlags forces the traditional "a/" and "b/" path prefixes. The
// user's Git config can change this: diff.mnemonicPrefix emits mnemonic
// prefixes (i/, w/, c/, o/) and diff.noprefix drops prefixes entirely. The
// diff parser only strips "a/" and "b/", so these flags override the config
// to keep output parseable.
func (b *Backend) withDiffFlags(args []string) []string {
	flags := []string{"--src-prefix=a/", "--dst-prefix=b/"}
	if b.whitespace == vcs.WhitespaceIgnoreAll {
		flags = append([]string{"--ignore-all-space"}, flags...)
	}
	out := make([]string, 0, len(args)+len(flags))
	out = append(out, args[0])
	out = append(out, flags...)
	out = append(out, args[1:]...)
	return out
}

// runDiff runs a diff command and parses its output; an empty diff yields
// an empty slice, not an error.
func (b *Backend) runDiff(args []string, h *syntax.Highlighter) ([]model.DiffFile, error) {
	stdout, stderr, err := b.run.Run(b.root, "git", args...)
	if err != nil {
		return nil, &errs.VcsCommand{Detail: fmt.Sprintf("git %s failed: %s",
			strings.Join(args, " "), strings.TrimSpace(string(stderr)))}
	}

	files, err := diffparser.Parse(string(stdout), diffparser.GitStyle, h)
	if err != nil {
		if errors.Is(err, errs.ErrNoChanges) {
			return nil, nil
		}
		return nil, err
	}
	return files, nil
}

// appendUntrackedDiffs synthesizes addition-only diffs for untracked files
// and appends them to files.
func (b *Backend) appendUntrackedDiffs(files []model.DiffFile, h *syntax.Highlighter) ([]model.DiffFile, error) {
	pathspecs, err := b.sparseCheckoutUntrackedPathspecs()
	if err != nil {
		return nil, err
	}
	paths, err := b.listUntrackedPaths(pathspecs)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		if file := b.buildUntrackedDiffFile(path, h); file != nil {
			files = append(files, *file)
		}
	}
	return files, nil
}

// buildUntrackedDiffFile turns an untracked working-tree file into an
// addition-only DiffFile. Files over maxUntrackedFileSize are listed
// without content; files containing NUL bytes are listed as binary. Files
// that vanish mid-scan are skipped (nil).
func (b *Backend) buildUntrackedDiffFile(path string, h *syntax.Highlighter) *model.DiffFile {
	fullPath := filepath.Join(b.root, path)
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil
	}
	if info.Size() > maxUntrackedFileSize {
		return diffFileWithoutHunks(path, false, true)
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return diffFileWithoutHunks(path, true, false)
	}

	rawLines := splitLines(string(data))
	lines := make([]string, len(rawLines))
	for i, line := range rawLines {
		lines[i] = vcs.Tabify(line)
	}
	if len(lines) == 0 {
		return diffFileWithoutHunks(path, false, false)
	}

	highlighted, ok := h.HighlightFileLines(path, lines)
	diffLines := make([]model.DiffLine, len(lines))
	for i, content := range lines {
		newLineno := uint32(i + 1)
		var spans []syntax.Span
		if ok && i < len(highlighted) && highlighted[i] != nil {
			spans = h.ApplyDiffBackground(highlighted[i], syntax.DiffOriginAddition)
		}
		diffLines[i] = model.DiffLine{
			Origin:           model.OriginAddition,
			Content:          content,
			NewLineno:        &newLineno,
			HighlightedSpans: spans,
		}
	}

	newCount := uint32(len(diffLines)) //nolint:gosec // G115: line numbers fit uint32
	hunks := []model.DiffHunk{{
		Header:   fmt.Sprintf("@@ -0,0 +1,%d @@", newCount),
		Lines:    diffLines,
		OldStart: 0,
		OldCount: 0,
		NewStart: 1,
		NewCount: newCount,
	}}
	contentHash := model.ComputeContentHash(hunks)

	newPath := path
	return &model.DiffFile{
		SourceIndex: -1,
		NewPath:     &newPath,
		Status:      model.StatusAdded,
		Hunks:       hunks,
		ContentHash: contentHash,
	}
}

func diffFileWithoutHunks(path string, isBinary, isTooLarge bool) *model.DiffFile {
	newPath := path
	return &model.DiffFile{
		SourceIndex: -1,
		NewPath:     &newPath,
		Status:      model.StatusAdded,
		IsBinary:    isBinary,
		IsTooLarge:  isTooLarge,
	}
}

// normalizeGitCliPaths mirrors an added file's new path onto the old side
// (and vice versa for deletions) so both sides always carry a path.
func normalizeGitCliPaths(files []model.DiffFile) {
	for i := range files {
		file := &files[i]
		switch {
		case file.Status == model.StatusAdded && file.OldPath == nil:
			file.OldPath = file.NewPath
		case file.Status == model.StatusDeleted && file.NewPath == nil:
			file.NewPath = file.OldPath
		}
	}
}

// readPathFromSource reads a file's full content from a diff side's source.
func (b *Backend) readPathFromSource(source contentSource, path string) (string, bool) {
	switch source.kind {
	case sourceWorkdir:
		return vcs.ReadWorkdirFile(b.root, path)
	case sourceIndex:
		return b.readGitObject(":0:" + path)
	case sourceRevision:
		return b.readGitObject(source.rev + ":" + path)
	case sourceNone:
	}
	return "", false
}

// readGitObject reads a blob via `git show`, e.g. "HEAD:path" or ":0:path".
func (b *Backend) readGitObject(spec string) (string, bool) {
	out, err := b.git("show", spec)
	if err != nil {
		return "", false
	}
	return out, true
}

// enhanceWithFullFileHighlight re-highlights each diff line of
// container-grammar files (Vue, Svelte, Astro, MDX, ...) using full-file
// context fetched from the diff's sources. Other files keep their existing
// per-hunk highlighting. This is a serial port of tuicr's
// enhance_with_full_file_highlight.
func (b *Backend) enhanceWithFullFileHighlight(files []model.DiffFile, h *syntax.Highlighter, fetchOld, fetchNew func(string) (string, bool)) {
	for i := range files {
		file := &files[i]
		if file.IsBinary || file.IsTooLarge || len(file.Hunks) == 0 {
			continue
		}
		syntaxPath := file.DisplayPath()
		if syntaxPath == "" || !syntax.NeedsFullFileHighlight(syntaxPath) {
			continue
		}

		oldHighlight := highlightSideContent(h, syntaxPath, file.OldPath, fetchOld)
		newHighlight := highlightSideContent(h, syntaxPath, file.NewPath, fetchNew)
		if oldHighlight == nil && newHighlight == nil {
			continue
		}
		applyFullFileSpans(file, h, oldHighlight, newHighlight)
	}
}

func highlightSideContent(h *syntax.Highlighter, syntaxPath string, sidePath *string, fetch func(string) (string, bool)) [][]syntax.Span {
	if sidePath == nil {
		return nil
	}
	content, ok := fetch(*sidePath)
	if !ok {
		return nil
	}
	lines := splitLines(content)
	for i, line := range lines {
		lines[i] = vcs.Tabify(line)
	}
	highlighted, ok := h.HighlightFileLines(syntaxPath, lines)
	if !ok {
		return nil
	}
	return highlighted
}

func applyFullFileSpans(file *model.DiffFile, h *syntax.Highlighter, oldHighlight, newHighlight [][]syntax.Span) {
	for hi := range file.Hunks {
		for li := range file.Hunks[hi].Lines {
			line := &file.Hunks[hi].Lines[li]
			var spans []syntax.Span
			if line.Origin == model.OriginDeletion {
				spans = spanAt(oldHighlight, line.OldLineno)
			} else {
				spans = spanAt(newHighlight, line.NewLineno)
			}
			if spans == nil {
				continue
			}
			line.HighlightedSpans = h.ApplyDiffBackground(spans, diffOrigin(line.Origin))
		}
	}
}

// spanAt returns the highlighted spans for the 1-based lineno, or nil when
// the side was not highlighted, the line is out of range, or the line has
// no spans (empty line).
func spanAt(highlight [][]syntax.Span, lineno *uint32) []syntax.Span {
	if highlight == nil || lineno == nil || *lineno == 0 {
		return nil
	}
	idx := int(*lineno) - 1
	if idx >= len(highlight) {
		return nil
	}
	return highlight[idx]
}

func diffOrigin(origin model.LineOrigin) syntax.DiffOrigin {
	switch origin {
	case model.OriginAddition:
		return syntax.DiffOriginAddition
	case model.OriginDeletion:
		return syntax.DiffOriginDeletion
	case model.OriginContext:
	}
	return syntax.DiffOriginContext
}

// splitLines splits content like Rust's str::lines: no trailing empty line
// for newline-terminated content, and trailing carriage returns stripped.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, "\r")
	}
	return lines
}
