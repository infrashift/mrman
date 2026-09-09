package forge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// hostileForge is a driver whose every string carries a terminal escape.
type hostileForge struct {
	Forge // nil: any method not overridden here panics, which is the point
}

const (
	bold = "\x1b[1m"
	link = "\x1b]8;;https://evil.example\x07"
	bell = "\x07"
)

func (hostileForge) ListPullRequests(context.Context, ListQuery) (*PullRequestPage, error) {
	return &PullRequestPage{Items: []PullRequestSummary{{
		Title: bold + "title", Author: link + "me", HeadRefName: "feat" + bell, BaseRefName: "main\x1b[2J",
	}}}, nil
}

func (hostileForge) GetPullRequest(context.Context, Target) (*PullRequestDetails, error) {
	return &PullRequestDetails{
		PullRequestSummary: PullRequestSummary{Title: bold + "t"},
		Body:               "line one\n" + bell + "line two",
	}, nil
}

func (hostileForge) ListCommits(context.Context, *PullRequestDetails) ([]Commit, error) {
	return []Commit{{Summary: bold + "s", Author: bell + "a"}}, nil
}

func (hostileForge) FetchFileLines(context.Context, FileLinesRequest) ([]model.DiffLine, error) {
	return []model.DiffLine{{Content: bold + "code", Raw: " " + bold + "code"}}, nil
}

func (hostileForge) ListReviewThreads(context.Context, *PullRequestDetails) ([]RemoteReviewThread, error) {
	return []RemoteReviewThread{{
		Path: "a" + bell + ".go", Disposition: bold + "won't fix",
		Comments: []RemoteReviewComment{{Author: link + "x", Body: "see " + link + "here", URL: bell}},
	}}, nil
}

func (hostileForge) ListReviewSummaries(context.Context, *PullRequestDetails) ([]RemoteReviewSummary, error) {
	return []RemoteReviewSummary{{Author: bold + "r", Body: bold + "LGTM"}}, nil
}

func (hostileForge) ReviewMetadata(context.Context, *PullRequestDetails) (*ReviewMetadata, error) {
	return &ReviewMetadata{ViewerLogin: bold + "me", Reviews: []ReviewRecord{{Author: bell + "r"}}}, nil
}

func (hostileForge) CreateReview(context.Context, *PullRequestDetails, CreateReviewRequest) (*SubmitResult, error) {
	return &SubmitResult{URL: bold + "u", State: bell + "APPROVED"}, nil
}

func TestSanitizedForgeScrubsEveryString(t *testing.T) {
	f := Sanitized(hostileForge{})
	ctx := context.Background()

	page, _ := f.ListPullRequests(ctx, ListQuery{})
	row := page.Items[0]
	if row.Title != "title" || row.Author != "me" || row.HeadRefName != "feat" || row.BaseRefName != "main" {
		t.Errorf("list row = %+v", row)
	}
	d, _ := f.GetPullRequest(ctx, Target{})
	if d.Title != "t" || d.Body != "line one\nline two" {
		t.Errorf("details = %q / %q", d.Title, d.Body)
	}
	commits, _ := f.ListCommits(ctx, nil)
	if commits[0].Summary != "s" || commits[0].Author != "a" {
		t.Errorf("commit = %+v", commits[0])
	}
	lines, _ := f.FetchFileLines(ctx, FileLinesRequest{})
	if lines[0].Content != "code" || lines[0].Raw != " code" {
		t.Errorf("file line = %+v", lines[0])
	}
	threads, _ := f.ListReviewThreads(ctx, nil)
	th := threads[0]
	if th.Path != "a.go" || th.Disposition != "won't fix" ||
		th.Comments[0].Author != "x" || th.Comments[0].Body != "see here" || th.Comments[0].URL != "" {
		t.Errorf("thread = %+v", th)
	}
	sums, _ := f.ListReviewSummaries(ctx, nil)
	if sums[0].Author != "r" || sums[0].Body != "LGTM" {
		t.Errorf("summary = %+v", sums[0])
	}
	meta, _ := f.ReviewMetadata(ctx, nil)
	if meta.ViewerLogin != "me" || meta.Reviews[0].Author != "r" {
		t.Errorf("metadata = %+v", meta)
	}
	res, _ := f.CreateReview(ctx, nil, CreateReviewRequest{})
	if res.URL != "u" || res.State != "APPROVED" {
		t.Errorf("submit result = %+v", res)
	}
}

func TestSanitizedIsIdempotentAndNilSafe(t *testing.T) {
	if Sanitized(nil) != nil {
		t.Fatal("nil in, nil out")
	}
	once := Sanitized(hostileForge{})
	if Sanitized(once) != once {
		t.Fatal("wrapping twice should return the same wrapper")
	}
	if _, ok := once.(*sanitizedForge).Unwrap().(hostileForge); !ok {
		t.Fatal("Unwrap should expose the driver")
	}
}

func TestForgeErrorTextIsScrubbed(t *testing.T) {
	err := NewError("github", "list", "gh.example", ErrorValidation, errors.New("\x1b[31mnope\x1b[0m\x07"))
	if got := err.Error(); !strings.HasSuffix(got, "on gh.example: nope") || strings.ContainsRune(got, 0x1b) {
		t.Fatalf("Error() = %q", got)
	}
}
