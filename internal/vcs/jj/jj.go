// Package jj implements the Jujutsu (jj) VCS backend using jj CLI commands,
// ported from tuicr's vcs/jj module.
//
// jj has no staging area, so the staged/unstaged operations keep their
// vcs.UnsupportedBase defaults; callers degrade gracefully via
// errs.ErrUnsupported.
package jj

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
	"github.com/infrashift/mrman/internal/vcs/diffparser"
)

// commitTemplate is the jj log template emitting one record per commit:
// fields separated by \x00, records terminated by \x01 (escapes interpreted
// by jj's template language). jj uses change IDs to identify changes;
// commit_id is the underlying git commit.
const commitTemplate = `commit_id ++ "\x00" ++ commit_id.short() ++ "\x00" ++ description ++ "\x00" ++ author.email() ++ "\x00" ++ committer.timestamp() ++ "\x01"`

// Backend is the Jujutsu backend implementation driving the jj CLI through
// a vcs.Runner.
type Backend struct {
	vcs.UnsupportedBase
	info vcs.Info
	ws   vcs.WhitespaceMode
	run  vcs.Runner
}

// Compile-time interface check.
var _ vcs.Backend = (*Backend)(nil)

// Discover locates a Jujutsu repository from cwd via `jj root` (which
// handles being called from subdirectories). Any failure — jj missing, or
// cwd not inside a jj repo — maps to errs.ErrNotARepository so detection
// can fall through to the next backend.
func Discover(cwd string, ws vcs.WhitespaceMode, run vcs.Runner) (*Backend, error) {
	stdout, _, err := run.Run(cwd, "jj", "root")
	if err != nil {
		return nil, errs.ErrNotARepository
	}
	root := strings.TrimSpace(string(stdout))
	if root == "" {
		return nil, errs.ErrNotARepository
	}
	return fromPath(root, ws, run), nil
}

// fromPath builds the backend for a known root, gathering head and bookmark
// info. Lookup failures degrade (head "unknown", no branch) rather than
// failing discovery.
func fromPath(root string, ws vcs.WhitespaceMode, run vcs.Runner) *Backend {
	// Resolve symlinks (e.g. /var -> /private/var on macOS), keeping the raw
	// path when resolution fails.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}

	b := &Backend{ws: ws, run: run}
	b.info = vcs.Info{
		RootPath:   root,
		HeadCommit: "unknown",
		Type:       vcs.TypeJujutsu,
	}

	// jj uses change IDs rather than commit hashes for the current change.
	if out, err := b.runJJ("log", "-r", "@", "--no-graph", "-T", "change_id.short()"); err == nil {
		b.info.HeadCommit = strings.TrimSpace(out)
	}
	b.info.BranchName = b.discoverBookmark()
	return b
}

// discoverBookmark finds the bookmark to display as the branch name: the
// bookmark on @ if set, otherwise the closest ancestor bookmark. Remote
// tracking bookmarks ("name@upstream") are filtered out in favor of the
// first local one.
func (b *Backend) discoverBookmark() *string {
	bookmarks := b.bookmarkOutput("log", "-r", "@", "--no-graph", "-T", "bookmarks")
	if bookmarks == "" {
		bookmarks = b.bookmarkOutput(
			"log", "-r", "heads(::@ & bookmarks())", "--no-graph", "-T", "bookmarks", "--limit", "1")
	}
	if bookmarks == "" {
		return nil
	}
	fields := strings.Fields(bookmarks)
	pick := fields[0]
	for _, f := range fields {
		if !strings.Contains(f, "@") {
			pick = f
			break
		}
	}
	return &pick
}

func (b *Backend) bookmarkOutput(args ...string) string {
	out, err := b.runJJ(args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// runJJ runs jj in the repository root and returns stdout, mapping failures
// to errs.VcsCommand.
func (b *Backend) runJJ(args ...string) (string, error) {
	stdout, stderr, err := b.run.Run(b.info.RootPath, "jj", args...)
	if err != nil {
		return "", vcs.CommandError("jj", args, stdout, stderr, err)
	}
	return string(stdout), nil
}

// diffArgs injects the whitespace flag after the subcommand when the mode
// ignores all whitespace.
func (b *Backend) diffArgs(args []string) []string {
	if b.ws != vcs.WhitespaceIgnoreAll {
		return args
	}
	out := make([]string, 0, len(args)+1)
	out = append(out, args[0], "--ignore-all-space")
	return append(out, args[1:]...)
}

// Info returns the repository info gathered at discovery.
func (b *Backend) Info() *vcs.Info { return &b.info }

// WorkingTreeDiff diffs the working copy against its parent (`jj diff
// --git`). Returns errs.ErrNoChanges when the diff is empty.
func (b *Backend) WorkingTreeDiff(h *syntax.Highlighter) ([]model.DiffFile, error) {
	out, err := b.runJJ(b.diffArgs([]string{"diff", "--git"})...)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, errs.ErrNoChanges
	}
	files, err := diffparser.Parse(out, diffparser.GitStyle, h)
	if err != nil {
		return nil, err
	}
	if err := b.applyContainerFullFileHighlight("@-", nil, files, h); err != nil {
		return nil, err
	}
	return files, nil
}

// FetchContextLines reads [start, end] (1-indexed, inclusive) of the file:
// at refCommit when given, at @- for deleted files, or from the working
// tree otherwise.
func (b *Backend) FetchContextLines(path string, status model.FileStatus, refCommit *string, start, end uint32) ([]model.DiffLine, error) {
	if start > end || start == 0 {
		return nil, nil
	}
	content, err := b.fileContent(path, status, refCommit)
	if err != nil {
		return nil, err
	}
	return vcs.SliceContextLines(content, start, end), nil
}

// FileLineCount returns the number of lines in the file at the same
// revision-selection rules as FetchContextLines.
func (b *Backend) FileLineCount(path string, status model.FileStatus, refCommit *string) (uint32, error) {
	content, err := b.fileContent(path, status, refCommit)
	if err != nil {
		return 0, err
	}
	return countLines(content), nil
}

func (b *Backend) fileContent(path string, status model.FileStatus, refCommit *string) (string, error) {
	switch {
	case refCommit != nil:
		return b.runJJ("file", "show", "-r", *refCommit, path)
	case status == model.StatusDeleted:
		return b.runJJ("file", "show", "-r", "@-", path)
	default:
		data, err := os.ReadFile(filepath.Join(b.info.RootPath, path))
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}

// ResolveRevisionRange resolves a jj revset to explicit commit IDs. jj log
// outputs newest first; the result is reversed so the oldest commit is
// first (matching CommitRangeDiff expectations).
func (b *Backend) ResolveRevisionRange(revset string) (vcs.ResolvedRevisionRange, error) {
	out, err := b.runJJ("log", "-r", revset, "--no-graph", "-T", `commit_id ++ "\n"`)
	if err != nil {
		return vcs.ResolvedRevisionRange{}, err
	}

	var commitIDs []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			commitIDs = append(commitIDs, line)
		}
	}
	if len(commitIDs) == 0 {
		return vcs.ResolvedRevisionRange{}, errs.ErrNoChanges
	}

	for i, j := 0, len(commitIDs)-1; i < j; i, j = i+1, j-1 {
		commitIDs[i], commitIDs[j] = commitIDs[j], commitIDs[i]
	}
	// Target zero value = diff the commit list (not an explicit base..head).
	return vcs.ResolvedRevisionRange{CommitIDs: commitIDs}, nil
}

// RecentCommits lists commits reachable from @ (newest first). jj log has
// no --skip option, so offset+limit records are fetched and the first
// offset dropped here.
func (b *Backend) RecentCommits(offset, limit int) ([]vcs.CommitInfo, error) {
	fetchCount := offset + limit
	out, err := b.runJJ(
		"log", "-r", "::@", "--limit", strconv.Itoa(fetchCount), "--no-graph", "-T", commitTemplate)
	if err != nil {
		return nil, err
	}
	commits := parseCommitRecords(out)
	if offset >= len(commits) {
		return nil, nil
	}
	return commits[offset:], nil
}

// CommitsInfo returns commit details for the given IDs, in input order;
// unknown IDs are silently skipped.
func (b *Backend) CommitsInfo(ids []string) ([]vcs.CommitInfo, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	revset := strings.Join(ids, " | ")
	out, err := b.runJJ("log", "-r", revset, "--no-graph", "-T", commitTemplate)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]vcs.CommitInfo)
	for _, c := range parseCommitRecords(out) {
		byID[c.ID] = c
	}
	var ordered []vcs.CommitInfo
	for _, id := range ids {
		if c, ok := byID[id]; ok {
			ordered = append(ordered, c)
		}
	}
	return ordered, nil
}

// CommitRangeDiff diffs from the parent of the oldest commit to the newest
// commit so the oldest commit's own changes are included (jj's "{commit}-"
// revset addresses the parent).
func (b *Backend) CommitRangeDiff(rng vcs.ResolvedRevisionRange, h *syntax.Highlighter) ([]model.DiffFile, error) {
	if len(rng.CommitIDs) == 0 {
		return nil, errs.ErrNoChanges
	}
	oldest := rng.CommitIDs[0]
	newest := rng.CommitIDs[len(rng.CommitIDs)-1]
	fromRev := oldest + "-"
	return b.rangeDiff(fromRev, newest, &newest, h)
}

// WorkingTreeWithCommitsDiff diffs from the parent of the oldest selected
// commit to the working copy (@).
func (b *Backend) WorkingTreeWithCommitsDiff(commitIDs []string, h *syntax.Highlighter) ([]model.DiffFile, error) {
	if len(commitIDs) == 0 {
		return nil, errs.ErrNoChanges
	}
	fromRev := commitIDs[0] + "-"
	return b.rangeDiff(fromRev, "@", nil, h)
}

// rangeDiff runs `jj diff --from fromRev --to toRev --git`, parses it, and
// applies container-grammar full-file highlighting. newRev nil means the
// new side is the working tree on disk.
func (b *Backend) rangeDiff(fromRev, toRev string, newRev *string, h *syntax.Highlighter) ([]model.DiffFile, error) {
	args := b.diffArgs([]string{"diff", "--from", fromRev, "--to", toRev, "--git"})
	out, err := b.runJJ(args...)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, errs.ErrNoChanges
	}
	files, err := diffparser.Parse(out, diffparser.GitStyle, h)
	if err != nil {
		return nil, err
	}
	if err := b.applyContainerFullFileHighlight(fromRev, newRev, files, h); err != nil {
		return nil, err
	}
	return files, nil
}

// showBatch fetches the full content of paths at rev in a single `jj file
// show` subprocess. jj is much cheaper per-call than hg, but batching still
// avoids repeated process startup when there are many container files in a
// diff.
func (b *Backend) showBatch(rev string, paths []string) (map[string]string, error) {
	if len(paths) == 0 {
		return map[string]string{}, nil
	}
	template := `"\n` + vcs.BatchBoundary + `\n" ++ path ++ "\n"`
	args := append([]string{"file", "show", "-r", rev, "-T", template}, paths...)
	out, err := b.runJJ(args...)
	if err != nil {
		return nil, err
	}
	return vcs.ParseBatchedFiles(out), nil
}

// parseCommitRecords parses commitTemplate output into CommitInfo values.
// Malformed records are skipped; unparsable timestamps fall back to now.
func parseCommitRecords(out string) []vcs.CommitInfo {
	var commits []vcs.CommitInfo
	for _, record := range strings.Split(out, "\x01") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		parts := strings.Split(record, "\x00")
		if len(parts) < 5 {
			continue
		}
		summary, body := parseDescription(parts[2])
		// jj timestamps are ISO 8601: "2024-01-15T10:30:00.000-05:00".
		when, err := time.Parse(time.RFC3339, parts[4])
		if err != nil {
			when = time.Now()
		}
		commits = append(commits, vcs.CommitInfo{
			ID:      parts[0],
			ShortID: parts[1],
			Summary: summary,
			Body:    body,
			Author:  parts[3],
			Time:    when.UTC(),
		})
	}
	return commits
}

// parseDescription splits a jj description into (summary, optional body):
// the first line is the summary; the body is the remaining lines after
// leading blank lines, nil when blank.
func parseDescription(desc string) (string, *string) {
	lines := splitLines(desc)
	if len(lines) == 0 {
		return "(no message)", nil
	}
	summary := lines[0]
	rest := lines[1:]
	for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
		rest = rest[1:]
	}
	body := strings.Join(rest, "\n")
	if strings.TrimSpace(body) == "" {
		return summary, nil
	}
	return summary, &body
}

// splitLines splits content with Rust str::lines semantics: no final empty
// line for newline-terminated input, and "" yields no lines.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// countLines counts lines with str::lines semantics.
func countLines(content string) uint32 {
	return uint32(len(splitLines(content)))
}
