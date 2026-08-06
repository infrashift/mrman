package output

import (
	"fmt"
	"sort"
	"strings"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
)

// ScopeKind mirrors internal/app's DiffSourceKind (same variants, same
// order) without importing it, so app can depend on output for export and
// clipboard without a cycle. Callers convert with a plain int cast.
type ScopeKind int

// Scope kinds, ordered identically to app.DiffSourceKind.
const (
	// ScopeWorkingTree reviews all uncommitted changes.
	ScopeWorkingTree ScopeKind = iota
	// ScopeStaged reviews the index only.
	ScopeStaged
	// ScopeUnstaged reviews the worktree against the index.
	ScopeUnstaged
	// ScopeStagedAndUnstaged reviews both halves of the working tree.
	ScopeStagedAndUnstaged
	// ScopeCommitRange reviews a selected commit range.
	ScopeCommitRange
	// ScopeStagedUnstagedAndCommits reviews commits plus uncommitted changes.
	ScopeStagedUnstagedAndCommits
	// ScopePullRequest reviews a pull request (scope strings are stubs until
	// PR mode lands).
	ScopePullRequest
	// ScopePatch reviews a standalone patch artifact.
	ScopePatch
	// ScopeDiffPaths compares two arbitrary paths with no repository.
	ScopeDiffPaths
)

// ScopeLine returns the "Reviewing ..." header line for the scope, exactly
// as tuicr's generate_markdown emits it. Working-tree reviews (and the PR
// stub) have no scope line and return "".
func (k ScopeKind) ScopeLine(commits []string) string {
	switch k {
	case ScopeStaged:
		return "Reviewing staged changes"
	case ScopeUnstaged:
		return "Reviewing unstaged changes"
	case ScopeStagedAndUnstaged:
		return "Reviewing staged + unstaged changes"
	case ScopeCommitRange:
		if len(commits) == 1 {
			return "Reviewing commit: " + shortSHA(commits[0])
		}
		return "Reviewing commits: " + strings.Join(shortSHAs(commits), ", ")
	case ScopeStagedUnstagedAndCommits:
		return "Reviewing staged + unstaged + commits: " + strings.Join(shortSHAs(commits), ", ")
	case ScopeWorkingTree, ScopePullRequest, ScopePatch, ScopeDiffPaths:
		// A patch review's scope is the artifact's own name, and a two-path
		// review's is the pair being compared. Both come from the caller
		// through ExportOptions.DiffSourceLabel — this function only sees
		// commit ids, and neither source has any.
		return ""
	}
	return ""
}

// Label returns the human scope description used in review-comment
// locations ("Review Comment (scope: <label>)") and TemplateData's
// DiffSourceLabel, matching tuicr's review_scope_label.
func (k ScopeKind) Label() string {
	switch k {
	case ScopeWorkingTree:
		return "working tree changes"
	case ScopeStaged:
		return "staged changes"
	case ScopeUnstaged:
		return "unstaged changes"
	case ScopeStagedAndUnstaged:
		return "staged + unstaged changes"
	case ScopeCommitRange:
		return "selected commit range"
	case ScopeStagedUnstagedAndCommits:
		return "selected commit range + staged/unstaged changes"
	case ScopePullRequest:
		return "merge request"
	case ScopePatch:
		return "patch file"
	case ScopeDiffPaths:
		return "two paths"
	}
	return ""
}

// ExportOptions carries the caller-provided context that is not derivable
// from the session alone.
type ExportOptions struct {
	// SessionSlug renders as "## Session: <slug>" when non-empty.
	SessionSlug string
	// DiffSourceLabel is the scope description for review-comment locations,
	// typically ScopeKind.Label().
	DiffSourceLabel string
	// ShowLegend gates the "Comment types:" legend line.
	ShowLegend bool
	// CommentTypes is the configured comment-type set, in configuration
	// order; only types actually used in the session make it into the data.
	CommentTypes []LegendEntry
	// IncludeDiff quotes the hunk each line comment is anchored in into
	// TemplateComment.Diff. Off by default: it changes what every export
	// looks like, so it is the export_diff setting's to turn on.
	IncludeDiff bool
}

// BuildTemplateData flattens a review session into the template data model:
// review comments first, then files sorted by path with file comments before
// line comments (ordered by line key), all numbered continuously. It returns
// errs.ErrNoComments when the session has nothing to export.
//
// files is the diff the session was written against, used only to quote each
// line comment's hunk when opts.IncludeDiff is set. It may be nil otherwise —
// the notes export has never needed the diff for anything else.
func BuildTemplateData(
	session *model.ReviewSession, files []model.DiffFile,
	scopeLine string, opts ExportOptions,
) (*TemplateData, error) {
	if !session.HasComments() {
		return nil, errs.ErrNoComments
	}

	data := &TemplateData{
		Slug:            opts.SessionSlug,
		Repo:            session.RepoPath,
		DiffSourceLabel: opts.DiffSourceLabel,
		ScopeLine:       scopeLine,
		ShowLegend:      opts.ShowLegend,
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
		reviewLocation = fmt.Sprintf("Review Comment (scope: %s)", opts.DiffSourceLabel)
	}
	for _, c := range session.ReviewComments {
		data.ReviewComments = append(data.ReviewComments,
			templateComment(c, reviewLocation, next(), opts.CommentTypes))
	}

	paths := make([]string, 0, len(session.Files))
	for path := range session.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	quoter := newHunkQuoter(files, opts.IncludeDiff)

	comments := 0
	for _, path := range paths {
		review := session.Files[path]
		comments += review.CommentCount()
		if review.CommentCount() == 0 {
			continue
		}
		// A repeated hunk is only suppressed within one file's run of
		// comments; the next file starts over.
		quoter.reset()
		file := TemplateFile{Path: path, Status: string(review.Status)}
		for _, c := range review.FileComments {
			file.Comments = append(file.Comments,
				templateComment(c, path, next(), opts.CommentTypes))
		}
		lines := make([]uint32, 0, len(review.LineComments))
		for line := range review.LineComments {
			lines = append(lines, line)
		}
		sort.Slice(lines, func(i, j int) bool { return lines[i] < lines[j] })
		for _, line := range lines {
			for _, c := range review.LineComments[line] {
				tc := templateComment(c, lineLocation(path, line, c), next(), opts.CommentTypes)
				tc.Diff = quoter.quote(path, line, c)
				file.Comments = append(file.Comments, tc)
			}
		}
		data.Files = append(data.Files, file)
	}

	data.Counts.Files = len(session.Files)
	data.Counts.Reviewed = session.ReviewedCount()
	data.Counts.Comments = len(session.ReviewComments) + comments
	return data, nil
}

// templateComment converts a model comment at the given location and number.
func templateComment(c *model.Comment, location string, number int, configured []LegendEntry) TemplateComment {
	tc := TemplateComment{
		Type:     exportTypeLabel(c.CommentType, configured),
		Author:   c.Author,
		Location: location,
		Content:  c.Content,
		Number:   number,
	}
	if c.CommitID != nil {
		tc.CommitID = shortSHA(*c.CommitID)
	}
	return tc
}

// exportTypeLabel resolves the **[TYPE]** marker text: empty for the
// typeless default, the configured label uppercased when the id is known,
// and the uppercased id for unconfigured types.
func exportTypeLabel(t model.CommentType, configured []LegendEntry) string {
	if t.IsNone() {
		return ""
	}
	for _, entry := range configured {
		if entry.ID == t.ID() {
			return strings.ToUpper(entry.Label)
		}
	}
	return t.Display()
}

// lineLocation formats a line comment's anchor: the comment's own range when
// present, otherwise the keyed line; single lines collapse to one number and
// old-side (deleted) lines get "~" prefixes.
func lineLocation(path string, line uint32, c *model.Comment) string {
	lineRange := model.SingleLineRange(line)
	if c.LineRange != nil {
		lineRange = *c.LineRange
	}
	old := model.SideOf(c) == model.LineSideOld
	switch {
	case old && lineRange.IsSingle():
		return fmt.Sprintf("%s:~%d", path, lineRange.Start)
	case old:
		return fmt.Sprintf("%s:~%d-~%d", path, lineRange.Start, lineRange.End)
	case lineRange.IsSingle():
		return fmt.Sprintf("%s:%d", path, lineRange.Start)
	default:
		return fmt.Sprintf("%s:%d-%d", path, lineRange.Start, lineRange.End)
	}
}

// usedLegendEntries filters the configured comment types down to the ones
// used in the session, preserving configuration order, excluding the
// typeless "none" default and defaulting empty definitions to the type id.
func usedLegendEntries(session *model.ReviewSession, configured []LegendEntry) []LegendEntry {
	used := usedCommentTypeIDs(session)
	var entries []LegendEntry
	for _, entry := range configured {
		if entry.ID == model.CommentTypeNoneID || !used[entry.ID] {
			continue
		}
		if entry.Definition == "" {
			entry.Definition = entry.ID
		}
		entries = append(entries, entry)
	}
	return entries
}

// usedCommentTypeIDs collects the type ids of every comment in the session.
func usedCommentTypeIDs(session *model.ReviewSession) map[string]bool {
	ids := make(map[string]bool)
	for _, c := range session.ReviewComments {
		ids[c.CommentType.ID()] = true
	}
	for _, review := range session.Files {
		for _, c := range review.FileComments {
			ids[c.CommentType.ID()] = true
		}
		for _, comments := range review.LineComments {
			for _, c := range comments {
				ids[c.CommentType.ID()] = true
			}
		}
	}
	return ids
}

// shortSHA truncates a commit id to tuicr's 7-character short form.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// shortSHAs maps shortSHA over commits.
func shortSHAs(commits []string) []string {
	out := make([]string, len(commits))
	for i, c := range commits {
		out[i] = shortSHA(c)
	}
	return out
}
