package jj

// Container-grammar full-file highlighting, ported from tuicr's
// vcs/mod.rs (apply_container_full_file_highlight and friends). Container
// grammars (Vue, Svelte, Astro, MDX, ...) need whole-file context before
// nested grammars activate, so their diff lines are re-highlighted from the
// full file content at the requested revisions instead of per-hunk. Other
// files keep the highlighting the diff parser already assigned.

import (
	"strings"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
)

// applyContainerFullFileHighlight re-highlights container-grammar files
// using their full content: the old side at oldRev, the new side at newRev,
// or from the working tree on disk when newRev is nil. Content is fetched
// via showBatch to avoid one subprocess per file.
func (b *Backend) applyContainerFullFileHighlight(oldRev string, newRev *string, files []model.DiffFile, h *syntax.Highlighter) error {
	if h == nil {
		return nil
	}

	oldPaths := vcs.ContainerFilePaths(files, model.LineSideOld, syntax.NeedsFullFileHighlight)
	newPaths := vcs.ContainerFilePaths(files, model.LineSideNew, syntax.NeedsFullFileHighlight)
	if len(oldPaths) == 0 && len(newPaths) == 0 {
		return nil
	}

	oldMap, err := b.showBatch(oldRev, oldPaths)
	if err != nil {
		return err
	}
	newMap := map[string]string{}
	if newRev != nil {
		newMap, err = b.showBatch(*newRev, newPaths)
		if err != nil {
			return err
		}
	}

	fetchOld := func(path string) (string, bool) {
		content, ok := oldMap[path]
		return content, ok
	}
	fetchNew := func(path string) (string, bool) {
		if content, ok := newMap[path]; ok {
			return content, true
		}
		if newRev == nil {
			return vcs.ReadWorkdirFile(b.info.RootPath, path)
		}
		return "", false
	}

	enhanceWithFullFileHighlight(files, h, fetchOld, fetchNew)
	return nil
}

// enhanceWithFullFileHighlight re-highlights each diff line of
// container-grammar files using full-file context. When a side's content is
// available, every diff line on that side is replaced with the spans at its
// 1-based lineno from the full-file highlight; lines whose side could not
// be fetched keep whatever the parser already assigned.
func enhanceWithFullFileHighlight(
	files []model.DiffFile,
	h *syntax.Highlighter,
	fetchOld, fetchNew func(string) (string, bool),
) {
	for i := range files {
		f := &files[i]
		if f.IsBinary || f.IsTooLarge || len(f.Hunks) == 0 {
			continue
		}
		syntaxPath := f.DisplayPath()
		if syntaxPath == "" || !syntax.NeedsFullFileHighlight(syntaxPath) {
			continue
		}

		var oldHL, newHL [][]syntax.Span
		var oldOK, newOK bool
		if f.OldPath != nil {
			if content, ok := fetchOld(*f.OldPath); ok {
				oldHL, oldOK = highlightContent(h, syntaxPath, content)
			}
		}
		if f.NewPath != nil {
			if content, ok := fetchNew(*f.NewPath); ok {
				newHL, newOK = highlightContent(h, syntaxPath, content)
			}
		}
		if !oldOK && !newOK {
			continue
		}
		applyFullFileSpans(f, h, oldHL, oldOK, newHL, newOK)
	}
}

// highlightContent tokenizes the whole file, skipping oversized or binary
// content (the size ceiling keeps runaway generated artifacts cheap).
func highlightContent(h *syntax.Highlighter, path, content string) ([][]syntax.Span, bool) {
	if len(content) > syntax.MaxHighlightFileBytes || strings.ContainsRune(content, 0) {
		return nil, false
	}
	raw := splitLines(content)
	lines := make([]string, len(raw))
	for i, l := range raw {
		lines[i] = vcs.Tabify(l)
	}
	return h.HighlightFileLines(path, lines)
}

// applyFullFileSpans replaces line spans from the side-appropriate
// full-file highlight: deletions read the old side, additions and context
// read the new side. Lines with no highlight available keep their existing
// spans.
func applyFullFileSpans(f *model.DiffFile, h *syntax.Highlighter, oldHL [][]syntax.Span, oldOK bool, newHL [][]syntax.Span, newOK bool) {
	for hi := range f.Hunks {
		for li := range f.Hunks[hi].Lines {
			line := &f.Hunks[hi].Lines[li]
			var spans []syntax.Span
			var found bool
			switch line.Origin {
			case model.OriginDeletion:
				spans, found = spanAt(oldHL, oldOK, line.OldLineno)
			case model.OriginAddition, model.OriginContext:
				spans, found = spanAt(newHL, newOK, line.NewLineno)
			}
			if !found {
				continue
			}
			line.HighlightedSpans = h.ApplyDiffBackground(spans, toDiffOrigin(line.Origin))
		}
	}
}

// spanAt returns the spans of the 1-based lineno in a full-file highlight.
func spanAt(hl [][]syntax.Span, ok bool, lineno *uint32) ([]syntax.Span, bool) {
	if !ok || lineno == nil || *lineno == 0 {
		return nil, false
	}
	idx := int(*lineno) - 1
	if idx >= len(hl) {
		return nil, false
	}
	return hl[idx], true
}

// toDiffOrigin maps model.LineOrigin to the syntax package's mirror type.
func toDiffOrigin(origin model.LineOrigin) syntax.DiffOrigin {
	switch origin {
	case model.OriginAddition:
		return syntax.DiffOriginAddition
	case model.OriginDeletion:
		return syntax.DiffOriginDeletion
	default:
		return syntax.DiffOriginContext
	}
}
