package forge

import (
	"context"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/textsafe"
)

// Sanitized wraps a driver so that every string a forge hands back — titles,
// branch names, authors, comment bodies, file paths, file contents — is
// scrubbed of terminal control sequences before anything above the forge
// layer sees it. Drivers stay oblivious; ForRepository applies this to
// each one it constructs.
//
// Diffs are the exception: GetDiff and GetCommitRangeDiff return raw patch
// text that goes through the diff parser, which scrubs line by line and so
// covers local git, jj and patch-file input in the same place.
func Sanitized(f Forge) Forge {
	if f == nil {
		return nil
	}
	if _, already := f.(*sanitizedForge); already {
		return f
	}
	return &sanitizedForge{Forge: f}
}

type sanitizedForge struct {
	Forge
}

// Unwrap returns the driver beneath the sanitizer, for tests and for
// callers that need the concrete type.
func (s *sanitizedForge) Unwrap() Forge { return s.Forge }

// ListPullRequests scrubs every row of the page.
func (s *sanitizedForge) ListPullRequests(ctx context.Context, q ListQuery) (*PullRequestPage, error) {
	page, err := s.Forge.ListPullRequests(ctx, q)
	if page != nil {
		for i := range page.Items {
			sanitizeSummary(&page.Items[i])
		}
	}
	return page, err
}

// GetPullRequest scrubs the summary fields and the body.
func (s *sanitizedForge) GetPullRequest(ctx context.Context, target Target) (*PullRequestDetails, error) {
	d, err := s.Forge.GetPullRequest(ctx, target)
	if d != nil {
		sanitizeSummary(&d.PullRequestSummary)
		d.Body = textsafe.Sanitize(d.Body)
	}
	return d, err
}

// ListCommits scrubs commit summaries and authors.
func (s *sanitizedForge) ListCommits(ctx context.Context, pr *PullRequestDetails) ([]Commit, error) {
	commits, err := s.Forge.ListCommits(ctx, pr)
	for i := range commits {
		commits[i].Summary = textsafe.SanitizeLine(commits[i].Summary)
		commits[i].Author = textsafe.SanitizeLine(commits[i].Author)
	}
	return commits, err
}

// FetchFileLines scrubs the fetched file lines.
func (s *sanitizedForge) FetchFileLines(ctx context.Context, req FileLinesRequest) ([]model.DiffLine, error) {
	lines, err := s.Forge.FetchFileLines(ctx, req)
	SanitizeDiffLines(lines)
	return lines, err
}

// ListReviewThreads scrubs paths, dispositions and every comment.
func (s *sanitizedForge) ListReviewThreads(ctx context.Context, pr *PullRequestDetails) ([]RemoteReviewThread, error) {
	threads, err := s.Forge.ListReviewThreads(ctx, pr)
	for i := range threads {
		t := &threads[i]
		t.Path = textsafe.SanitizeLine(t.Path)
		t.Disposition = textsafe.SanitizeLine(t.Disposition)
		for j := range t.Comments {
			c := &t.Comments[j]
			c.Author = textsafe.SanitizeLine(c.Author)
			c.Body = textsafe.Sanitize(c.Body)
			c.URL = textsafe.SanitizeLine(c.URL)
		}
	}
	return threads, err
}

// ListReviewSummaries scrubs authors and bodies.
func (s *sanitizedForge) ListReviewSummaries(ctx context.Context, pr *PullRequestDetails) ([]RemoteReviewSummary, error) {
	summaries, err := s.Forge.ListReviewSummaries(ctx, pr)
	for i := range summaries {
		r := &summaries[i]
		r.Author = textsafe.SanitizeLine(r.Author)
		r.Body = textsafe.Sanitize(r.Body)
		r.URL = textsafe.SanitizeLine(r.URL)
	}
	return summaries, err
}

// ReviewMetadata scrubs the viewer login and reviewer names.
func (s *sanitizedForge) ReviewMetadata(ctx context.Context, pr *PullRequestDetails) (*ReviewMetadata, error) {
	meta, err := s.Forge.ReviewMetadata(ctx, pr)
	if meta != nil {
		meta.ViewerLogin = textsafe.SanitizeLine(meta.ViewerLogin)
		for i := range meta.Reviews {
			meta.Reviews[i].Author = textsafe.SanitizeLine(meta.Reviews[i].Author)
		}
	}
	return meta, err
}

// CreateReview scrubs the URL and state the forge reports back.
func (s *sanitizedForge) CreateReview(ctx context.Context, pr *PullRequestDetails, req CreateReviewRequest) (*SubmitResult, error) {
	result, err := s.Forge.CreateReview(ctx, pr, req)
	if result != nil {
		result.URL = textsafe.SanitizeLine(result.URL)
		result.State = textsafe.SanitizeLine(result.State)
	}
	return result, err
}

func sanitizeSummary(p *PullRequestSummary) {
	p.Title = textsafe.SanitizeLine(p.Title)
	p.Author = textsafe.SanitizeLine(p.Author)
	p.HeadRefName = textsafe.SanitizeLine(p.HeadRefName)
	p.BaseRefName = textsafe.SanitizeLine(p.BaseRefName)
	p.URL = textsafe.SanitizeLine(p.URL)
	p.State = textsafe.SanitizeLine(p.State)
}

// SanitizeDiffLines scrubs the text of file lines in place. Shared with
// the local backends, which read context lines straight from the checkout.
func SanitizeDiffLines(lines []model.DiffLine) {
	for i := range lines {
		lines[i].Content = textsafe.SanitizeLine(lines[i].Content)
		lines[i].Raw = textsafe.SanitizeLine(lines[i].Raw)
	}
}
