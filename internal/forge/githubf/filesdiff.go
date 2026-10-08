package githubf

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/go-github/v76/github"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/vcs"
)

// maxFilePages bounds the pull request files listing: GitHub serves at most
// 3000 files, 100 to a page.
const maxFilePages = 30

// isDiffTooLarge reports whether err is GitHub refusing a raw diff for its
// size: 406 with a too_large error, which it returns for a pull request of
// more than 300 files.
func isDiffTooLarge(err error) bool {
	ghErr, ok := errors.AsType[*github.ErrorResponse](err)
	if !ok || ghErr.Response == nil || ghErr.Response.StatusCode != http.StatusNotAcceptable {
		return false
	}
	for _, e := range ghErr.Errors {
		if e.Code == "too_large" {
			return true
		}
	}
	return strings.Contains(ghErr.Message, "maximum number of files")
}

// filesDiff synthesizes the pull request's unified diff from the files
// listing, for when the raw diff is refused as too large. Each entry carries
// its hunks as `patch`; the git file headers are written here. GitHub omits
// the patch for a binary file and for one whose diff is itself too large:
// the first reports no added or deleted lines, the second does.
func (d *Driver) filesDiff(ctx context.Context, pr *forge.PullRequestDetails) (string, error) {
	const op = "get_diff"
	var b strings.Builder
	opts := &github.ListOptions{PerPage: 100}
	for range maxFilePages {
		files, resp, err := d.rest.PullRequests.ListFiles(ctx, pr.Repository.Owner, pr.Repository.Name,
			int(pr.Number), opts) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
		if err != nil {
			return "", d.wrap(op, err)
		}
		for _, f := range files {
			appendFileDiff(&b, f)
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return b.String(), nil
}

// appendFileDiff writes one files-listing entry as a git-style diff section.
func appendFileDiff(b *strings.Builder, f *github.CommitFile) {
	newPath := f.GetFilename()
	oldPath := newPath
	if prev := f.GetPreviousFilename(); prev != "" {
		oldPath = prev
	}
	if newPath == "" {
		return
	}
	fmt.Fprintf(b, "diff --git a/%s b/%s\n", oldPath, newPath)
	oldHeader, newHeader := "a/"+oldPath, "b/"+newPath
	switch f.GetStatus() {
	case "added":
		b.WriteString("new file mode 100644\n")
		oldHeader = "/dev/null"
	case "removed":
		b.WriteString("deleted file mode 100644\n")
		newHeader = "/dev/null"
	case "renamed":
		fmt.Fprintf(b, "rename from %s\nrename to %s\n", oldPath, newPath)
	case "copied":
		fmt.Fprintf(b, "copy from %s\ncopy to %s\n", oldPath, newPath)
	}
	patch := f.GetPatch()
	switch {
	case patch != "":
		fmt.Fprintf(b, "--- %s\n+++ %s\n%s", oldHeader, newHeader, patch)
		if !strings.HasSuffix(patch, "\n") {
			b.WriteByte('\n')
		}
	case f.GetAdditions()+f.GetDeletions() > 0:
		b.WriteString(vcs.TooLargeDiffMarker + "\n")
	case f.GetChanges() > 0 || f.GetStatus() == "added" || f.GetStatus() == "removed" || f.GetStatus() == "modified":
		fmt.Fprintf(b, "Binary files %s and %s differ\n", oldHeader, newHeader)
	}
	// A pure rename has no patch and no changes: its header is the diff.
}
