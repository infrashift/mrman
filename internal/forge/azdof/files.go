package azdof

import (
	"context"
	"io"
	"strings"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// FetchFileLines returns [StartLine, EndLine] (1-indexed, inclusive) of the
// file at the requested side of the PR as context diff lines. Out-of-range
// requests return an empty slice.
func (d *Driver) FetchFileLines(ctx context.Context, req forge.FileLinesRequest) ([]model.DiffLine, error) {
	const op = "fetch_file_lines"
	if req.StartLine == 0 || req.StartLine > req.EndLine {
		return nil, nil
	}
	project, repoName := d.coords(req.Repository)
	content, err := d.blobAt(ctx, op, project, repoName, req.SHA(), req.Path)
	if err != nil {
		return nil, err
	}
	return vcs.SliceContextLines(content, req.StartLine, req.EndLine), nil
}

// FileLineCount returns the number of lines in the file at the requested
// side of the PR.
func (d *Driver) FileLineCount(ctx context.Context, req forge.FileLinesRequest) (int, error) {
	const op = "file_line_count"
	project, repoName := d.coords(req.Repository)
	content, err := d.blobAt(ctx, op, project, repoName, req.SHA(), req.Path)
	if err != nil {
		return 0, err
	}
	return countLines(content), nil
}

// blobAt reads the whole file at sha: the local checkout answers first when
// it has the blob, otherwise the items API serves the raw content at the
// commit version.
func (d *Driver) blobAt(ctx context.Context, op, project, repoName, sha, path string) (string, error) {
	if content, ok := d.localBlob(sha, path); ok {
		return content, nil
	}
	itemPath := "/" + strings.TrimPrefix(path, "/")
	reader, err := d.gitClient.GetItemContent(ctx, git.GetItemContentArgs{
		RepositoryId: &repoName,
		Project:      &project,
		Path:         &itemPath,
		VersionDescriptor: &git.GitVersionDescriptor{
			Version:     &sha,
			VersionType: &git.GitVersionTypeValues.Commit,
		},
	})
	if err != nil {
		return "", d.wrap(op, err)
	}
	defer reader.Close() //nolint:errcheck // read errors surface below
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", d.wrap(op, err)
	}
	return string(data), nil
}

// localBlob reads sha:path from the local checkout, guarded by
// `git cat-file -e`. ok=false on any failure so callers fall back to the
// API; the checkout is an optimization, never the source of truth.
func (d *Driver) localBlob(sha, path string) (string, bool) {
	if d.localCheckout == "" || d.runner == nil {
		return "", false
	}
	spec := sha + ":" + path
	if _, _, err := d.runner.Run(d.localCheckout, "git", "cat-file", "-e", spec); err != nil {
		return "", false
	}
	stdout, _, err := d.runner.Run(d.localCheckout, "git", "show", spec)
	if err != nil {
		return "", false
	}
	return string(stdout), true
}

// countLines counts logical lines: a trailing newline does not open a
// final empty line.
func countLines(content string) int {
	if content == "" {
		return 0
	}
	n := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		n++
	}
	return n
}
