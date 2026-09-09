// Package diffbackend reviews the difference between two arbitrary paths —
// the `mrman diff <old> <new>` entry point. Neither path need be in any
// repository, and they need not be related to each other.
//
// It is a distinct package rather than a fourth filebackend.Mode because
// the two render nothing in common. Every filebackend mode emits a whole
// file as one synthetic hunk with no old side; this one computes a real
// two-sided diff. Sharing a type would mean a `switch b.mode` in every
// method with one arm structurally unlike the rest. What is genuinely
// common — the gitignore-aware walker, the binary sniff, the size ceiling —
// is imported from filebackend instead, so a directory comparison and
// `--file <dir>` agree about what counts as a reviewable file.
//
// The diff itself comes from internal/diffgen rather than from
// `git diff --no-index`. That is not a preference. --no-index does not
// honour .gitignore (comparing two directories in any JS or Go project
// walks node_modules), it strips the leading slash from absolute paths so
// every display path arrives mangled, and it exits 1 both when files differ
// and when it cannot open them. Generating the text in-process avoids all
// three and needs no subprocess.
//
// # The root invariant
//
// Info.RootPath is the NEW side's root, and every DisplayPath is relative
// to it. That one choice is what lets open-in-editor, ignore filtering, the
// -p prefix filter, context expansion and the session filename all work
// with no special-casing for this backend.
package diffbackend

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/infrashift/mrman/internal/diffgen"
	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
	"github.com/infrashift/mrman/internal/vcs/diffparser"
	"github.com/infrashift/mrman/internal/vcs/filebackend"
)

// Mode says whether the comparison is between two files or two trees.
type Mode int

// Comparison modes.
const (
	// Files compares two regular files, paired by construction.
	Files Mode = iota
	// Trees compares two directories, pairing entries by their path
	// relative to each root; unpaired entries are additions or deletions.
	Trees
)

// pair is one file to compare. oldAbs is empty when the file exists only on
// the new side (an addition), newAbs when it exists only on the old side (a
// deletion); never both.
//
// oldDisplay and newDisplay are usually the same relative path — a file is
// compared against the file at the same place in the other tree. They differ
// only for a detected rename, which is what makes the entry a rename at all.
// An absent side has an empty display path, mirroring its empty abs path.
type pair struct {
	oldDisplay string
	newDisplay string
	oldAbs     string
	newAbs     string
}

// key is the path this pair is identified by: the new side, falling back to
// the old for a deletion. It matches model.DiffFile.DisplayPath, which is
// what session state, the file tree and context expansion are all keyed on.
func (p pair) key() string {
	if p.newDisplay != "" {
		return p.newDisplay
	}
	return p.oldDisplay
}

// isRename reports whether this pair moved between the two trees.
func (p pair) isRename() bool {
	return p.oldDisplay != "" && p.newDisplay != "" && p.oldDisplay != p.newDisplay
}

// Backend compares two paths outside any VCS.
type Backend struct {
	vcs.UnsupportedBase

	info    vcs.Info
	oldRoot string
	newRoot string
	// oldTarget and newTarget are the paths the user actually named, which
	// for two files are the files themselves rather than their directories.
	// Kept so the review can say what is being compared; the roots cannot,
	// because in Files mode they are only the containing directories.
	oldTarget string
	newTarget string
	mode      Mode
	pairs     []pair
	// byPath resolves a display path back to its pair. Context expansion
	// looks paths up here rather than joining them onto a root, which makes
	// the legal set enumerated instead of merely validated: a session
	// carrying "../../etc/passwd" finds no pair and reads nothing.
	byPath   map[string]pair
	opts     diffgen.Options
	warnings []string
}

// Compile-time interface check.
var _ vcs.Backend = (*Backend)(nil)

// New prepares a comparison between oldPath and newPath. Both must be
// regular files, or both directories.
//
// It returns a wrapped I/O error when either path cannot be resolved, an
// errs.InvalidInput when they are of different kinds or resolve to the same
// file, and errs.ErrNoChanges when a directory pair yields nothing to
// compare.
func New(oldPath, newPath string, ws vcs.WhitespaceMode) (*Backend, error) {
	oldAbs, err := canonicalize(oldPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open %q: %w", oldPath, err)
	}
	newAbs, err := canonicalize(newPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open %q: %w", newPath, err)
	}

	oldStat, err := os.Stat(oldAbs)
	if err != nil {
		return nil, fmt.Errorf("cannot open %q: %w", oldPath, err)
	}
	newStat, err := os.Stat(newAbs)
	if err != nil {
		return nil, fmt.Errorf("cannot open %q: %w", newPath, err)
	}

	if oldAbs == newAbs {
		return nil, &errs.InvalidInput{Detail: fmt.Sprintf(
			"%q and %q are the same file, so there is nothing to compare", oldPath, newPath)}
	}

	b := &Backend{
		oldRoot:   oldAbs,
		newRoot:   newAbs,
		oldTarget: oldAbs,
		newTarget: newAbs,
		opts:      diffgenOptions(ws),
	}

	switch {
	case oldStat.Mode().IsRegular() && newStat.Mode().IsRegular():
		b.mode = Files
		b.oldRoot = filepath.Dir(oldAbs)
		b.newRoot = filepath.Dir(newAbs)
		// The display path names the new side, per the root invariant. Two
		// files with different names still make one entry, and both sides
		// share that one path: they are the two halves of one comparison,
		// not a rename. Naming them apart here would make diffgen emit
		// rename headers and paint a meaningless R badge.
		display := filepath.Base(newAbs)
		b.pairs = []pair{{
			oldDisplay: display,
			newDisplay: display,
			oldAbs:     oldAbs,
			newAbs:     newAbs,
		}}
	case oldStat.IsDir() && newStat.IsDir():
		b.mode = Trees
		b.pairs = pairTrees(oldAbs, newAbs)
		if len(b.pairs) == 0 {
			return nil, errs.ErrNoChanges
		}
	default:
		return nil, &errs.InvalidInput{Detail: fmt.Sprintf(
			"cannot compare a file to a directory: %q and %q must both be files or both be directories",
			oldPath, newPath)}
	}

	b.byPath = make(map[string]pair, len(b.pairs))
	for _, p := range b.pairs {
		b.byPath[p.key()] = p
	}
	b.warnings = lineEndingWarnings(b.pairs)
	b.info = vcs.Info{
		RootPath:   b.newRoot,
		HeadCommit: pairIdentity(oldAbs, newAbs),
		BranchName: sessionLabel(oldAbs, newAbs),
		Type:       vcs.TypeDiff,
	}
	return b, nil
}

// diffgenOptions maps mrman's whitespace setting onto the diff generator.
//
// NoRenameHeaders is always set: `mrman diff a.txt b.txt` names the two
// things being compared, not one file's history, and a rename header would
// make diffparser report StatusRenamed and paint an R badge that means
// nothing here.
func diffgenOptions(ws vcs.WhitespaceMode) diffgen.Options {
	opts := diffgen.Options{NoRenameHeaders: true}
	if ws == vcs.WhitespaceIgnoreAll {
		opts.LineKey = stripAllWhitespace
	}
	return opts
}

// stripAllWhitespace is the comparison key for WhitespaceIgnoreAll, giving
// the same result as git's --ignore-all-space: whitespace stops deciding
// whether two lines differ, but the reviewer still reads the real text.
func stripAllWhitespace(line string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r':
			return -1
		}
		return r
	}, line)
}

// pairTrees pairs entries by their path relative to each root, so a file at
// the same relative path in both trees is one comparison, then matches what
// is left over as renames. Anything still unpaired is an addition or a
// deletion.
func pairTrees(oldRoot, newRoot string) []pair {
	oldFiles := filebackend.CollectTextFiles(oldRoot)
	newFiles := filebackend.CollectTextFiles(newRoot)

	byRel := make(map[string]*pair, len(oldFiles)+len(newFiles))
	at := func(rel string) *pair {
		if p, ok := byRel[rel]; ok {
			return p
		}
		p := &pair{}
		byRel[rel] = p
		return p
	}
	for _, f := range oldFiles {
		p := at(f.RelPath)
		p.oldDisplay, p.oldAbs = f.RelPath, f.Path
	}
	for _, f := range newFiles {
		p := at(f.RelPath)
		p.newDisplay, p.newAbs = f.RelPath, f.Path
	}

	pairs := make([]pair, 0, len(byRel))
	for _, p := range byRel {
		pairs = append(pairs, *p)
	}
	pairs = detectRenames(pairs)
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].key() < pairs[j].key() })
	return pairs
}

// renameCandidateLimit bounds rename detection, mirroring git's
// diff.renameLimit. Similarity matching compares every unpaired deletion
// against every unpaired addition, so a large reorganisation would otherwise
// turn opening a review into a quadratic amount of work. Past the limit only
// the exact pass runs, which stays linear.
const renameCandidateLimit = 1000

// renameSimilarity is the fraction of lines two files must share to be
// called a rename, matching git's default of 50%.
const renameSimilarity = 0.5

// detectRenames folds unpaired deletions and additions into rename pairs,
// in two passes for the same reason git uses two: an exact content match is
// certain and cheap, so it is claimed first and never lost to a merely
// similar candidate.
//
// Empty files sit out. Every empty file is byte-identical to every other, so
// they would pair arbitrarily, and calling an empty file a rename of another
// empty file tells the reviewer nothing.
func detectRenames(pairs []pair) []pair {
	var kept, deletions, additions []pair
	for _, p := range pairs {
		switch {
		case p.oldAbs != "" && p.newAbs != "":
			kept = append(kept, p)
		case p.newAbs == "":
			deletions = append(deletions, p)
		default:
			additions = append(additions, p)
		}
	}
	if len(deletions) == 0 || len(additions) == 0 {
		return pairs
	}

	// Deterministic order so an ambiguous match resolves the same way twice;
	// session identity depends on the review not reshuffling between runs.
	sort.Slice(deletions, func(i, j int) bool { return deletions[i].oldDisplay < deletions[j].oldDisplay })
	sort.Slice(additions, func(i, j int) bool { return additions[i].newDisplay < additions[j].newDisplay })

	oldContents := readAll(deletions, func(p pair) string { return p.oldAbs })
	newContents := readAll(additions, func(p pair) string { return p.newAbs })
	claimed := make([]bool, len(additions))

	matchExact(deletions, additions, oldContents, newContents, claimed, &kept)
	if len(deletions)*len(additions) <= renameCandidateLimit {
		matchSimilar(deletions, additions, oldContents, newContents, claimed, &kept)
	}

	for i, p := range deletions {
		if oldContents[i] != nil {
			kept = append(kept, p)
		}
	}
	for i, p := range additions {
		if !claimed[i] {
			kept = append(kept, p)
		}
	}
	return kept
}

// matchExact claims byte-identical pairs. A deletion it consumes is marked
// by clearing its content slot, which is also what stops the leftover sweep
// from emitting it a second time.
func matchExact(deletions, additions []pair, oldContents, newContents []*string, claimed []bool, kept *[]pair) {
	byContent := make(map[string][]int, len(additions))
	for i, c := range newContents {
		if c == nil || *c == "" {
			continue
		}
		byContent[*c] = append(byContent[*c], i)
	}
	for di, d := range deletions {
		content := oldContents[di]
		if content == nil || *content == "" {
			continue
		}
		for _, ai := range byContent[*content] {
			if claimed[ai] {
				continue
			}
			claimed[ai] = true
			oldContents[di] = nil
			*kept = append(*kept, renamePair(d, additions[ai]))
			break
		}
	}
}

// matchSimilar claims the best remaining candidate above the similarity
// threshold, so a deletion that resembles several additions goes to the one
// it resembles most rather than to whichever was seen first.
func matchSimilar(deletions, additions []pair, oldContents, newContents []*string, claimed []bool, kept *[]pair) {
	newLines := make([]map[string]int, len(additions))
	for i, c := range newContents {
		if c != nil {
			newLines[i] = lineCounts(*c)
		}
	}
	for di, d := range deletions {
		content := oldContents[di]
		if content == nil || *content == "" {
			continue
		}
		oldLines := lineCounts(*content)
		best, bestScore := -1, renameSimilarity
		for ai := range additions {
			if claimed[ai] || newLines[ai] == nil {
				continue
			}
			if score := similarity(oldLines, newLines[ai]); score > bestScore {
				best, bestScore = ai, score
			}
		}
		if best >= 0 {
			claimed[best] = true
			oldContents[di] = nil
			*kept = append(*kept, renamePair(d, additions[best]))
		}
	}
}

// renamePair joins a deletion and an addition into the single entry that
// says the file moved.
func renamePair(deletion, addition pair) pair {
	return pair{
		oldDisplay: deletion.oldDisplay,
		newDisplay: addition.newDisplay,
		oldAbs:     deletion.oldAbs,
		newAbs:     addition.newAbs,
	}
}

// readAll reads one side of each pair, leaving nil where the file could not
// be read so a later pass skips it.
func readAll(pairs []pair, side func(pair) string) []*string {
	out := make([]*string, len(pairs))
	for i, p := range pairs {
		if data, err := os.ReadFile(side(p)); err == nil {
			content := string(data)
			out[i] = &content
		}
	}
	return out
}

// lineCounts is a file as a multiset of its lines, which is what similarity
// is measured over. A multiset rather than a set so a file of mostly
// repeated lines cannot look identical to a much shorter one.
func lineCounts(content string) map[string]int {
	counts := make(map[string]int)
	for _, line := range strings.SplitAfter(content, "\n") {
		if line != "" {
			counts[line]++
		}
	}
	return counts
}

// similarity is the shared fraction of two line multisets: twice the
// overlap over the combined size, so it is 1 for identical files and falls
// off as either side gains lines the other lacks.
func similarity(a, b map[string]int) float64 {
	total := 0
	for _, n := range a {
		total += n
	}
	for _, n := range b {
		total += n
	}
	if total == 0 {
		return 0
	}
	shared := 0
	for line, n := range a {
		if m, ok := b[line]; ok {
			shared += min(n, m)
		}
	}
	return 2 * float64(shared) / float64(total)
}

// Info returns the pseudo-repository info: the new side's root, a content-
// free identity derived from the two paths, and TypeDiff.
func (b *Backend) Info() *vcs.Info { return &b.info }

// StartupWarnings reports anything about the pair the reviewer should know
// before reading the diff.
func (b *Backend) StartupWarnings() []string { return b.warnings }

// Mode reports whether this compares two files or two trees.
func (b *Backend) Mode() Mode { return b.mode }

// OldRoot is the canonical root the old side's paths are relative to. In
// Files mode it is the containing directory, not the file, so describe a
// comparison with Label rather than with the roots.
func (b *Backend) OldRoot() string { return b.oldRoot }

// NewRoot is the canonical root every DisplayPath is relative to, and the
// value Info reports as RootPath. The same Files-mode caveat as OldRoot
// applies.
func (b *Backend) NewRoot() string { return b.newRoot }

// Label names the two sides as "<old> → <new>", using the shortest tail of
// each path that tells them apart.
//
// Nothing else in the UI can say this: DiffFile.OldPath is never rendered,
// and every display path is the new side. Trailing separators mark a
// directory comparison so it cannot be mistaken for a file one.
func (b *Backend) Label() string {
	oldName, newName := distinguishingTails(b.oldTarget, b.newTarget)
	if b.mode == Trees {
		sep := string(filepath.Separator)
		return oldName + sep + " → " + newName + sep
	}
	return oldName + " → " + newName
}

// distinguishingTails returns the shortest trailing path components of a
// and b that differ, so comparing two files both called config.yaml reads
// as "staging/config.yaml → prod/config.yaml" rather than the useless
// "config.yaml → config.yaml".
func distinguishingTails(a, b string) (string, string) {
	aParts := strings.Split(filepath.ToSlash(a), "/")
	bParts := strings.Split(filepath.ToSlash(b), "/")
	for n := 1; n <= len(aParts) && n <= len(bParts); n++ {
		aTail := strings.Join(aParts[len(aParts)-n:], "/")
		bTail := strings.Join(bParts[len(bParts)-n:], "/")
		if aTail != bTail {
			return aTail, bTail
		}
	}
	// One path is a suffix of the other, or they are equal — which New
	// already refused. Fall back to the full paths.
	return a, b
}

// WorkingTreeDiff renders the comparison. It is the only diff entry point
// this backend serves: there is no index, no history and no revision to
// select, so every other diff method stays unsupported.
//
// Every pair's text is generated first and parsed in one pass, rather than
// parsed per pair, so SourceIndex ordering falls out of the parser instead
// of being stitched together afterwards.
func (b *Backend) WorkingTreeDiff(h *syntax.Highlighter) ([]model.DiffFile, error) {
	var (
		text     strings.Builder
		stubs    []model.DiffFile
		anyPairs bool
	)

	for _, p := range b.pairs {
		if stub, isStub := b.stubFor(p); isStub {
			stubs = append(stubs, stub)
			continue
		}
		oldContent, okOld := readSide(p.oldAbs)
		newContent, okNew := readSide(p.newAbs)
		if !okOld || !okNew {
			continue
		}
		// Rename headers are emitted only for a pair that genuinely moved.
		// Everywhere else the two paths are the same anyway, except in Files
		// mode, where two separately named files are one comparison rather
		// than a rename and must not be labelled as one.
		opts := b.opts
		opts.NoRenameHeaders = !p.isRename()
		diff := diffgen.UnifiedFileDiff(
			p.oldDisplay, p.newDisplay, oldContent, newContent, opts)
		if diff == "" {
			continue // identical; nothing to review
		}
		text.WriteString(diff)
		anyPairs = true
	}

	var files []model.DiffFile
	if anyPairs {
		parsed, err := diffparser.Parse(text.String(), diffparser.GitStyle, h)
		if err != nil {
			return nil, err
		}
		files = parsed
	}
	files = append(files, stubs...)

	if len(files) == 0 {
		return nil, errs.ErrNoChanges
	}
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].DisplayPath() < files[j].DisplayPath()
	})
	return files, nil
}

// stubFor returns a placeholder entry for a pair that must be listed but
// not rendered — binary content, or a file past the size ceiling — and
// reports whether one was needed.
//
// Binary is checked before reading so a large binary is skipped without
// loading it, and so it is never mistaken for a too-large text file.
func (b *Backend) stubFor(p pair) (model.DiffFile, bool) {
	binary := (p.oldAbs != "" && filebackend.IsProbablyBinary(p.oldAbs)) ||
		(p.newAbs != "" && filebackend.IsProbablyBinary(p.newAbs))
	tooLarge := sideTooLarge(p.oldAbs) || sideTooLarge(p.newAbs)
	if !binary && !tooLarge {
		return model.DiffFile{}, false
	}
	stub := model.DiffFile{
		SourceIndex: -1,
		Status:      statusFor(p),
		IsBinary:    binary,
		IsTooLarge:  !binary && tooLarge,
		ContentHash: model.ComputeContentHash(nil),
	}
	// Only the sides that exist get a path. Leaving NewPath nil for a
	// deletion is what makes DisplayPath fall back to the old one, the same
	// way a parsed deletion entry behaves.
	if p.oldAbs != "" {
		old := p.oldDisplay
		stub.OldPath = &old
	}
	if p.newAbs != "" {
		newPath := p.newDisplay
		stub.NewPath = &newPath
	}
	return stub, true
}

// statusFor derives a file's status from which sides it exists on. It is
// only needed for stubs — parsed entries get their status from the headers
// diffgen emitted.
func statusFor(p pair) model.FileStatus {
	switch {
	case p.oldAbs == "":
		return model.StatusAdded
	case p.newAbs == "":
		return model.StatusDeleted
	case p.isRename():
		return model.StatusRenamed
	default:
		return model.StatusModified
	}
}

// readSide reads one side of a pair. An absent side reads as empty content,
// which is exactly what an addition or deletion should diff against.
func readSide(abs string) (string, bool) {
	if abs == "" {
		return "", true
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// sideTooLarge reports whether a side is past the render ceiling.
func sideTooLarge(abs string) bool {
	if abs == "" {
		return false
	}
	st, err := os.Stat(abs)
	return err == nil && st.Size() > filebackend.MaxFileBytes
}

// FetchContextLines reads [start, end] (1-indexed, inclusive) around a hunk
// from the file behind path's new side, or its old side for a deletion.
//
// The vcs.Backend signature carries no side and does not need one: gap
// expansion works exclusively in new-side coordinates (see the
// ContextProvider contract in internal/app/gaps.go), deriving old line
// numbers from the surrounding hunk boundaries rather than fetching them.
// So the whole rule is "new side, unless there is no new side" — the same
// rule the git backend applies when a file's status is deleted.
//
// path is resolved through byPath, never joined onto a root, so a path from
// a stale or malformed session cannot escape the two directories under
// comparison. Unknown paths read as nothing rather than as an error.
func (b *Backend) FetchContextLines(path string, status model.FileStatus, _ *string, start, end uint32) ([]model.DiffLine, error) {
	if start > end || start == 0 {
		return nil, nil
	}
	abs, ok := b.resolve(path, status)
	if !ok {
		return nil, nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, nil //nolint:nilerr // a side that cannot be read has no context to offer
	}
	return vcs.SliceContextLines(string(data), start, end), nil
}

// FileLineCount returns the number of lines on the side FetchContextLines
// would read, so the UI can tell when a gap reaches the end of the file.
func (b *Backend) FileLineCount(path string, status model.FileStatus, _ *string) (uint32, error) {
	abs, ok := b.resolve(path, status)
	if !ok {
		return 0, nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return 0, nil //nolint:nilerr // a side that cannot be read has no lines
	}
	return countLines(string(data)), nil
}

// resolve maps a display path and status onto the absolute file to read.
func (b *Backend) resolve(path string, status model.FileStatus) (string, bool) {
	p, ok := b.byPath[path]
	if !ok {
		return "", false
	}
	abs := p.newAbs
	if status == model.StatusDeleted || abs == "" {
		abs = p.oldAbs
	}
	return abs, abs != ""
}

// ChangeStatus reports no staged or unstaged changes, because a two-path
// comparison has no index to compare against.
//
// Answering plainly matters: when a backend cannot answer,
// resolveChangeStatus falls back to UnstagedDiff and then to
// WorkingTreeDiff, which returns this backend's own files and so reports
// them as "unstaged changes". The target selector would then offer an
// UNSTAGED row that fails with "unsupported operation" the moment it is
// chosen.
func (b *Backend) ChangeStatus() (vcs.ChangeStatus, error) {
	return vcs.ChangeStatus{}, nil
}

// lineEndingWarnings reports pairs whose two sides disagree about line
// endings.
//
// This is worth saying out loud because the disagreement is invisible in
// the rendered diff: the generator compares the lines with their \r intact
// and calls them different, but the parser strips \r before display, so the
// reviewer sees a -/+ pair that looks byte-identical and has no way to tell
// why it is marked as changed.
func lineEndingWarnings(pairs []pair) []string {
	var warnings []string
	for _, p := range pairs {
		if p.oldAbs == "" || p.newAbs == "" {
			continue
		}
		oldCRLF, okOld := usesCRLF(p.oldAbs)
		newCRLF, okNew := usesCRLF(p.newAbs)
		if !okOld || !okNew || oldCRLF == newCRLF {
			continue
		}
		oldStyle, newStyle := "LF", "CRLF"
		if oldCRLF {
			oldStyle, newStyle = "CRLF", "LF"
		}
		warnings = append(warnings, fmt.Sprintf(
			"%s: old side uses %s line endings and new side uses %s, so every line reads as changed",
			p.key(), oldStyle, newStyle))
	}
	return warnings
}

// usesCRLF reports whether the file's first line ending is a CRLF. Files
// with no line ending at all report false with ok=false, since there is
// nothing to disagree about.
func usesCRLF(abs string) (crlf, ok bool) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return false, false
	}
	i := bytes.IndexByte(data, '\n')
	if i < 0 {
		return false, false
	}
	return i > 0 && data[i-1] == '\r', true
}

// pairIdentity is the session identity for a comparison: an FNV-1a hash of
// the two canonical paths.
//
// Keying on the paths rather than on the files' contents is deliberate, and
// is the opposite of what the patch backend does. A patch is an artifact
// whose bytes are its identity, so editing it makes a new review. A
// comparison is of live files that are expected to change between sittings
// — editing one and reloading is the central workflow — so the session must
// survive that and let anchor validation report what moved.
//
// The result is bare hex with no colon: slug.Parse cuts on the first colon
// and then requires a known forge prefix, so a colon here would make the
// session slug unparseable.
func pairIdentity(oldAbs, newAbs string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(oldAbs))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(newAbs))
	return fmt.Sprintf("%016x", h.Sum64())
}

// sessionLabel is the readable half of the session filename, sanitized to
// what the slug grammar accepts.
//
// It uses the same distinguishing tails the header does, so comparing two
// files that share a name produces `staging-config.yaml-vs-prod-config.yaml`
// rather than two identical halves.
func sessionLabel(oldAbs, newAbs string) *string {
	oldName, newName := distinguishingTails(oldAbs, newAbs)
	label := SanitizeStem(oldName) + "-vs-" + SanitizeStem(newName)
	return &label
}

// SanitizeStem reduces a path component to the characters a session slug
// and filename may carry, collapsing every run of anything else to a single
// hyphen.
func SanitizeStem(name string) string {
	var sb strings.Builder
	lastHyphen := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '.', r == '_':
			sb.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				sb.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(sb.String(), "-.")
}

// canonicalize resolves path to an absolute, symlink-free form.
func canonicalize(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// countLines counts lines with Rust str::lines semantics: a trailing
// newline does not produce a final empty line, and "" yields none.
func countLines(content string) uint32 {
	if content == "" {
		return 0
	}
	return uint32(strings.Count(strings.TrimSuffix(content, "\n"), "\n") + 1) //nolint:gosec // G115: line numbers fit uint32
}
