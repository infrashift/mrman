package output

import (
	"sort"
	"strings"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
)

// This file builds the review-reply artifact: the diff quoted back with "> "
// and the reviewer's comments interleaved beneath the lines they refer to.
// That is the shape a mailing-list review takes, and the shape a patch author
// can read against the mail they sent.
//
// It is a parallel builder rather than an extension of TemplateData.
// TemplateComment.Number is documented as continuous across a path-sorted
// export, and a reply is ordered by patch position instead — one numbering
// cannot be both. Extending the notes model would also hand every existing
// user template fields it structurally cannot use.

// ReplyHeaders are the mail headers that thread a reply into the conversation
// the patch was posted in. All fields are empty when the artifact carried no
// threading metadata, and the exporter then emits no header block at all —
// mrman never invents a Message-Id, because a reply that threads to the wrong
// message is worse than one that does not thread.
type ReplyHeaders struct {
	Subject    string
	InReplyTo  string
	References []string
	// Attribution is the "On <date>, <author> wrote:" line.
	Attribution string
}

// Empty reports whether there is nothing to thread against.
func (h ReplyHeaders) Empty() bool {
	return h.Subject == "" && h.InReplyTo == "" && len(h.References) == 0
}

// PatchLine is one quoted diff line and whatever hangs off it.
type PatchLine struct {
	// Text is the verbatim source line including its +/-/space prefix.
	Text string
	// OldLine and NewLine are the line's numbers on each side, nil when it
	// does not exist there.
	OldLine, NewLine *uint32
	// Comments are the reviewer's notes on this line, in the order they were
	// written, old-side before new-side.
	Comments []TemplateComment
}

// Quoted renders the line with the mail quote prefix.
//
// Trailing whitespace is trimmed, so a blank context line — whose diff text is
// a lone space — emits a bare ">" rather than ">  ". Mailers strip trailing
// whitespace in transit anyway, so emitting it would only make what is
// recorded here disagree with what the recipient reads.
func (l PatchLine) Quoted() string {
	text := strings.TrimRight(l.Text, " \t")
	if text == "" {
		return ">"
	}
	return "> " + l.Text
}

// PatchHunk is one quoted hunk.
type PatchHunk struct {
	// Header is the verbatim "@@ ... @@" line.
	Header string
	Lines  []PatchLine
}

// HasComments reports whether any line in the hunk was commented on.
func (h PatchHunk) HasComments() bool {
	for _, l := range h.Lines {
		if len(l.Comments) > 0 {
			return true
		}
	}
	return false
}

// QuotedHeader renders the hunk header with the quote prefix.
func (h PatchHunk) QuotedHeader() string { return "> " + h.Header }

// PatchFile is one file of the reply.
type PatchFile struct {
	Path   string
	Status string
	// Header is the file's verbatim diff preamble.
	Header []string
	// Comments are file-level notes, rendered before the first hunk.
	Comments []TemplateComment
	Hunks    []PatchHunk
	// Skipped explains why a file carries no quoted diff — binary, too large,
	// or absent from the reviewed diff. Empty when the file quotes normally.
	Skipped string
}

// QuotedHeader renders the file preamble with the quote prefix.
func (f PatchFile) QuotedHeader() []string {
	out := make([]string, 0, len(f.Header))
	for _, line := range f.Header {
		if line == "" {
			out = append(out, ">")
			continue
		}
		out = append(out, "> "+line)
	}
	return out
}

// OrphanComment is a comment that could not be placed against a quoted line.
//
// These are listed separately rather than guessed at. A comment whose anchor
// no longer resolves still has a line number that would "work", and quoting
// that line would attach the reviewer's criticism to code they never read —
// the one outcome worth going out of the way to avoid.
type OrphanComment struct {
	TemplateComment
	// Reason says why it could not be placed, in the same words the submit
	// resolver uses.
	Reason string
	// Snapshot is the anchored line's text as it was when the comment was
	// written, so the reader can see what it was about.
	Snapshot string
}

// PatchData is the reply artifact's template model.
type PatchData struct {
	Slug            string
	Repo            string
	Branch          string
	DiffSourceLabel string
	SessionNotes    string
	Reply           ReplyHeaders
	CommentTypes    []LegendEntry
	ShowLegend      bool
	// ReviewComments are notes with no file, rendered above the quoted diff —
	// where a reply's prose belongs.
	ReviewComments []TemplateComment
	Files          []PatchFile
	Orphans        []OrphanComment
	Counts         struct {
		Files    int
		Reviewed int
		Comments int
	}
}

// PatchContext selects how much of the diff to quote.
type PatchContext int

// Quoting modes.
const (
	// ContextCommented quotes only hunks that were commented on. A reply
	// re-quoting an entire series is unreadable, and the author already has
	// the patch.
	ContextCommented PatchContext = iota
	// ContextAll quotes every hunk, for an archive of the whole review.
	ContextAll
)

// PatchOptions carries what the caller knows and the session does not.
type PatchOptions struct {
	SessionSlug     string
	DiffSourceLabel string
	ShowLegend      bool
	CommentTypes    []LegendEntry
	Context         PatchContext
	Reply           ReplyHeaders
	// Outdated reports whether a comment's anchor failed validation. Supplied
	// by the caller because output must not import app; pass
	// App.HasOutdatedAnchor.
	Outdated func(commentID string) bool
	// AnchorLabel renders a non-empty badge for a comment whose anchor moved,
	// so a relocated note says so. Optional.
	AnchorLabel func(commentID string) string
}

// outdated reports a comment stale, defaulting to false when the caller
// supplied no predicate.
func (o PatchOptions) outdated(id string) bool {
	return o.Outdated != nil && o.Outdated(id)
}

func (o PatchOptions) anchorLabel(id string) string {
	if o.AnchorLabel == nil {
		return ""
	}
	return o.AnchorLabel(id)
}

// BuildPatchData assembles the reply from a session and the diff it was
// written against.
//
// Files are emitted in the order the diff carried them, not the order they are
// displayed in: NewApp sorts by directory for the file tree, and a reply
// quoted out of the author's order is hard to follow against the original
// mail.
func BuildPatchData(
	session *model.ReviewSession, files []model.DiffFile, opts PatchOptions,
) (*PatchData, error) {
	if !session.HasComments() {
		return nil, errs.ErrNoComments
	}

	data := &PatchData{
		Slug:            opts.SessionSlug,
		Repo:            session.RepoPath,
		DiffSourceLabel: opts.DiffSourceLabel,
		ShowLegend:      opts.ShowLegend,
		Reply:           opts.Reply,
		CommentTypes:    usedLegendEntries(session, opts.CommentTypes),
	}
	if session.BranchName != nil {
		data.Branch = *session.BranchName
	}
	if session.SessionNotes != nil {
		data.SessionNotes = *session.SessionNotes
	}

	number := 0
	next := func() int { number++; return number }

	reviewLocation := "Review Comment"
	if opts.DiffSourceLabel != "" {
		reviewLocation = "Review Comment (scope: " + opts.DiffSourceLabel + ")"
	}
	for _, c := range session.ReviewComments {
		data.ReviewComments = append(data.ReviewComments,
			templateComment(c, reviewLocation, next(), opts.CommentTypes))
	}

	byPath := diffFilesByPath(files)
	for _, path := range patchOrderedPaths(session, files) {
		review := session.Files[path]
		if review == nil || review.CommentCount() == 0 {
			continue
		}
		file, orphans := buildPatchFile(path, review, byPath[path], next, opts)
		data.Orphans = append(data.Orphans, orphans...)
		// A file left with nothing to show is dropped rather than emitted as a
		// bare header. That happens when every comment on it was orphaned, or
		// when ContextCommented pruned its only hunks — and the orphan section
		// already names the file and line, so the header would be noise.
		if len(file.Hunks) == 0 && len(file.Comments) == 0 && file.Skipped == "" {
			continue
		}
		data.Files = append(data.Files, file)
	}

	data.Counts.Files = len(session.Files)
	data.Counts.Reviewed = session.ReviewedCount()
	data.Counts.Comments = number
	return data, nil
}

// diffFilesByPath indexes the diff by display path, keeping the earliest entry
// when a series touches one path more than once — that is the file the
// reviewer read first.
func diffFilesByPath(files []model.DiffFile) map[string]*model.DiffFile {
	out := make(map[string]*model.DiffFile, len(files))
	for i := range files {
		path := files[i].DisplayPath()
		if existing, ok := out[path]; ok && existing.SourceIndex <= files[i].SourceIndex {
			continue
		}
		out[path] = &files[i]
	}
	return out
}

// patchOrderedPaths lists the session's commented paths in the order the diff
// carried them, with any path not in the diff sorted after, alphabetically.
func patchOrderedPaths(session *model.ReviewSession, files []model.DiffFile) []string {
	order := make(map[string]int, len(files))
	for i := range files {
		path := files[i].DisplayPath()
		if _, seen := order[path]; !seen {
			order[path] = files[i].SourceIndex
		}
	}

	paths := make([]string, 0, len(session.Files))
	for path := range session.Files {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		a, aOK := order[paths[i]]
		b, bOK := order[paths[j]]
		switch {
		case aOK && bOK:
			return a < b
		case aOK != bOK:
			return aOK // files present in the diff come first
		}
		return paths[i] < paths[j]
	})
	return paths
}

// buildPatchFile quotes one file and hangs its comments off the right lines.
func buildPatchFile(
	path string, review *model.FileReview, diff *model.DiffFile,
	next func() int, opts PatchOptions,
) (PatchFile, []OrphanComment) {
	file := PatchFile{Path: path, Status: string(review.Status)}
	for _, c := range review.FileComments {
		file.Comments = append(file.Comments, templateComment(c, path, next(), opts.CommentTypes))
	}

	// Everything that cannot be quoted becomes an orphan rather than a guess.
	if skip := skipReason(diff); skip != "" {
		file.Skipped = skip
		return file, orphanAll(path, review, skip, next, opts)
	}

	placed, orphans := placeComments(path, review, diff, next, opts)
	file.Header = diff.RawHeader
	file.Hunks = quoteHunks(diff, placed, opts.Context)
	return file, orphans
}

// skipReason names why a file carries no quotable diff, reusing the words the
// submit resolver already uses for the same situations.
func skipReason(diff *model.DiffFile) string {
	switch {
	case diff == nil:
		return "not in the reviewed diff"
	case diff.IsBinary:
		return "binary file"
	case diff.IsTooLarge:
		return "file too large"
	case len(diff.Hunks) == 0:
		return "no diff content"
	}
	return ""
}

// lineKey identifies a diff line by side and number.
type lineKey struct {
	side model.LineSide
	line uint32
}

// placeComments maps each line comment onto the diff line it belongs to,
// returning the placements and everything that could not be placed.
func placeComments(
	path string, review *model.FileReview, diff *model.DiffFile,
	next func() int, opts PatchOptions,
) (map[lineKey][]TemplateComment, []OrphanComment) {
	present := presentLines(diff)
	placed := map[lineKey][]TemplateComment{}
	var orphans []OrphanComment

	lines := make([]uint32, 0, len(review.LineComments))
	for line := range review.LineComments {
		lines = append(lines, line)
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i] < lines[j] })

	for _, line := range lines {
		for _, c := range review.LineComments[line] {
			tc := templateComment(c, lineLocation(path, line, c), next(), opts.CommentTypes)
			if label := opts.anchorLabel(c.ID); label != "" {
				tc.Anchor = label
			}

			key := lineKey{side: model.SideOf(c), line: line}
			switch {
			case opts.outdated(c.ID):
				orphans = append(orphans, orphan(tc, "anchored line changed since the comment was written", c))
			case !present[key]:
				orphans = append(orphans, orphan(tc, "line not in current diff", c))
			default:
				placed[key] = append(placed[key], tc)
			}
		}
	}
	return placed, orphans
}

// presentLines is the set of (side, line) pairs the diff actually contains.
func presentLines(diff *model.DiffFile) map[lineKey]bool {
	present := map[lineKey]bool{}
	for hi := range diff.Hunks {
		for _, l := range diff.Hunks[hi].Lines {
			if l.OldLineno != nil {
				present[lineKey{model.LineSideOld, *l.OldLineno}] = true
			}
			if l.NewLineno != nil {
				present[lineKey{model.LineSideNew, *l.NewLineno}] = true
			}
		}
	}
	return present
}

// quoteHunks renders the diff's hunks, keeping only commented ones under
// ContextCommented.
func quoteHunks(diff *model.DiffFile, placed map[lineKey][]TemplateComment, mode PatchContext) []PatchHunk {
	var out []PatchHunk
	for hi := range diff.Hunks {
		src := &diff.Hunks[hi]
		hunk := PatchHunk{Header: src.Header}
		for _, l := range src.Lines {
			line := PatchLine{Text: rawLine(l), OldLine: l.OldLineno, NewLine: l.NewLineno}
			// Old side first, then new — the order the diff itself reads in,
			// and the order the TUI already renders comments in.
			if l.OldLineno != nil {
				line.Comments = append(line.Comments, placed[lineKey{model.LineSideOld, *l.OldLineno}]...)
			}
			if l.NewLineno != nil {
				line.Comments = append(line.Comments, placed[lineKey{model.LineSideNew, *l.NewLineno}]...)
			}
			hunk.Lines = append(hunk.Lines, line)
		}
		if mode == ContextCommented && !hunk.HasComments() {
			continue
		}
		out = append(out, hunk)
	}
	return out
}

// rawLine is the verbatim source line, reconstructed from the display text
// when the line was synthesized rather than parsed from a diff.
func rawLine(l model.DiffLine) string {
	if l.Raw != "" {
		return l.Raw
	}
	return originPrefix(l.Origin) + l.Content
}

func originPrefix(o model.LineOrigin) string {
	switch o {
	case model.OriginAddition:
		return "+"
	case model.OriginDeletion:
		return "-"
	}
	return " "
}

// orphan builds an unplaceable comment, carrying the anchor snapshot so the
// reader can see what the note was written about.
func orphan(tc TemplateComment, reason string, c *model.Comment) OrphanComment {
	o := OrphanComment{TemplateComment: tc, Reason: reason}
	if c.LineContext != nil {
		o.Snapshot = strings.TrimRight(c.LineContext.Content, " \t")
	}
	return o
}

// orphanAll converts every line comment on a skipped file.
func orphanAll(
	path string, review *model.FileReview, reason string,
	next func() int, opts PatchOptions,
) []OrphanComment {
	lines := make([]uint32, 0, len(review.LineComments))
	for line := range review.LineComments {
		lines = append(lines, line)
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i] < lines[j] })

	var out []OrphanComment
	for _, line := range lines {
		for _, c := range review.LineComments[line] {
			tc := templateComment(c, lineLocation(path, line, c), next(), opts.CommentTypes)
			out = append(out, orphan(tc, reason, c))
		}
	}
	return out
}
