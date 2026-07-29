package forgejof

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// FetchFileLines returns [StartLine, EndLine] (1-indexed, inclusive) of the
// file at the requested side of the PR as context diff lines. Out-of-range
// requests return an empty slice, matching the tuicr semantics.
func (d *Driver) FetchFileLines(ctx context.Context, req forge.FileLinesRequest) ([]model.DiffLine, error) {
	const op = "fetch_file_lines"
	if req.StartLine == 0 || req.StartLine > req.EndLine {
		return nil, nil
	}
	content, err := d.fileContent(ctx, op, req)
	if err != nil {
		return nil, err
	}
	return vcs.SliceContextLines(content, req.StartLine, req.EndLine), nil
}

// FileLineCount returns the number of lines in the file at the requested
// side of the PR.
func (d *Driver) FileLineCount(ctx context.Context, req forge.FileLinesRequest) (int, error) {
	const op = "file_line_count"
	content, err := d.fileContent(ctx, op, req)
	if err != nil {
		return 0, err
	}
	return countLines(content), nil
}

// fileContent reads the whole file at the side's SHA: the local checkout
// answers first when it has the blob, then the contents API (base64), with
// a raw-endpoint fallback for responses the contents API cannot inline.
func (d *Driver) fileContent(ctx context.Context, op string, req forge.FileLinesRequest) (string, error) {
	sha := req.SHA()
	if content, ok := d.localBlob(sha, req.Path); ok {
		return content, nil
	}
	api, err := d.api(ctx)
	if err != nil {
		return "", d.wrap(op, nil, err)
	}
	contents, resp, err := api.GetContents(req.Repository.Owner, req.Repository.Name, sha, req.Path)
	if err != nil {
		return "", d.wrap(op, resp, err)
	}
	if contents == nil || contents.Type != "file" {
		return "", d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("path %q at %s is not a file", req.Path, sha))
	}
	if contents.Content != nil && contents.Encoding != nil && *contents.Encoding == "base64" {
		if raw, decErr := base64.StdEncoding.DecodeString(*contents.Content); decErr == nil {
			return string(raw), nil
		}
	}
	// Content missing or undecodable (e.g. files over the inline-content
	// limit): fetch the blob through the raw endpoint instead.
	raw, rawResp, err := api.GetFile(req.Repository.Owner, req.Repository.Name, sha, req.Path)
	if err != nil {
		return "", d.wrap(op, rawResp, err)
	}
	return string(raw), nil
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

// countLines counts logical lines the way Rust's str::lines does: a
// trailing newline does not open a final empty line.
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
