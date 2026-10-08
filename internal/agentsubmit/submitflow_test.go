package agentsubmit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/reviewcli"
)

// The interlock tests stop before any forge work. These go past it: a fake
// driver, registered for the test binary's Forgejo kind, stands in for the
// forge so every outcome of a granted submit can be driven.

const fixtureDiff = "diff --git a/src/x.go b/src/x.go\n--- a/src/x.go\n+++ b/src/x.go\n" +
	"@@ -1,2 +1,3 @@\n a\n+b\n c\n"

// submitForge is a forge.Forge whose pull request, diff and review outcome
// a test sets.
type submitForge struct {
	head     string
	result   *forge.SubmitResult
	err      error
	requests []forge.CreateReviewRequest
}

func (f *submitForge) ID() forgetypes.Kind { return forgetypes.KindForgejo }
func (f *submitForge) Capabilities() forge.Capabilities {
	return forge.Capabilities{Approve: true, RequestChanges: true, MultiLineComments: true}
}
func (f *submitForge) ListPullRequests(context.Context, forge.ListQuery) (*forge.PullRequestPage, error) {
	return &forge.PullRequestPage{}, nil
}
func (f *submitForge) GetPullRequest(_ context.Context, t forge.Target) (*forge.PullRequestDetails, error) {
	return &forge.PullRequestDetails{
		PullRequestSummary: forge.PullRequestSummary{Repository: *t.Repository, Number: t.Number, State: "OPEN"},
		HeadSHA:            f.head, BaseSHA: "base1234",
	}, nil
}
func (f *submitForge) GetDiff(context.Context, *forge.PullRequestDetails) (string, error) {
	return fixtureDiff, nil
}
func (f *submitForge) GetCommitRangeDiff(context.Context, *forge.PullRequestDetails, string, string) (string, error) {
	return "", nil
}
func (f *submitForge) ListCommits(context.Context, *forge.PullRequestDetails) ([]forge.Commit, error) {
	return nil, nil
}
func (f *submitForge) FetchFileLines(context.Context, forge.FileLinesRequest) ([]model.DiffLine, error) {
	return nil, nil
}
func (f *submitForge) FileLineCount(context.Context, forge.FileLinesRequest) (int, error) {
	return 0, nil
}
func (f *submitForge) ListReviewThreads(context.Context, *forge.PullRequestDetails) ([]forge.RemoteReviewThread, error) {
	return nil, nil
}
func (f *submitForge) ListReviewSummaries(context.Context, *forge.PullRequestDetails) ([]forge.RemoteReviewSummary, error) {
	return nil, nil
}
func (f *submitForge) ReviewMetadata(context.Context, *forge.PullRequestDetails) (*forge.ReviewMetadata, error) {
	return &forge.ReviewMetadata{}, nil
}
func (f *submitForge) CreateReview(_ context.Context, _ *forge.PullRequestDetails, req forge.CreateReviewRequest) (*forge.SubmitResult, error) {
	f.requests = append(f.requests, req)
	return f.result, f.err
}
func (f *submitForge) LocalCheckoutPath() string { return "" }

// grantedSession registers fake as the Forgejo driver and writes a granted
// PR session at head1234 with a line comment on src/x.go:2 and a review
// comment.
func grantedSession(t *testing.T, fake *submitForge) (*persistence.Store, string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no user config
	xdg.Reload()
	t.Cleanup(xdg.Reload)
	previous, hadPrevious := forge.DriverFor(forgetypes.KindForgejo)
	forge.Register(forge.Driver{ID: forgetypes.KindForgejo, SlugPrefix: "fj",
		New: func(forge.HostConfig) (forge.Forge, error) { return fake, nil }})
	t.Cleanup(func() {
		if hadPrevious {
			forge.Register(previous)
		}
	})

	store := &persistence.Store{ReviewsDir: filepath.Join(t.TempDir(), "reviews")}
	repo := forgetypes.Repository{Kind: forgetypes.KindForgejo, Host: "forgejo.test", Owner: "acme", Name: "widget"}
	session := model.NewReviewSession("forge:forgejo.test/acme/widget", "head1234", nil, model.SourcePullRequest)
	session.PrSessionKey = &forgetypes.PrSessionKey{Repository: repo, Number: 7, HeadSHA: "head1234"}
	session.AddFile("src/x.go", model.StatusModified, 0)
	side := model.LineSideNew
	session.File("src/x.go").AddLineComment(2, model.NewComment("inline", model.CommentTypeFromID("issue"), &side))
	session.ReviewComments = append(session.ReviewComments,
		model.NewComment("summary", model.CommentTypeFromID("note"), nil))
	path, err := store.SaveSession(session)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"comment"}); err != nil {
		t.Fatal(err)
	}
	return store, path
}

func submitComment(t *testing.T, store *persistence.Store, path string) (SubmitResultOutput, error) {
	t.Helper()
	var out bytes.Buffer
	err := Submit(store, Options{Options: reviewcli.Options{Session: path}, Event: "comment"}, &out)
	var result SubmitResultOutput
	if out.Len() > 0 {
		if jerr := json.Unmarshal(out.Bytes(), &result); jerr != nil {
			t.Fatalf("output is not JSON: %v (%s)", jerr, out.String())
		}
	}
	return result, err
}

func TestGrantedSubmitPostsAndLocks(t *testing.T) {
	fake := &submitForge{head: "head1234", result: &forge.SubmitResult{ReviewID: "R9", State: "COMMENTED", URL: "https://x/7"}}
	store, path := grantedSession(t, fake)

	result, err := submitComment(t, store, path)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !result.Submitted || result.ReviewID != "R9" || result.InlineCount != 1 || result.LockedComment != 2 {
		t.Errorf("result = %+v, want submitted R9 with 1 inline and 2 locked", result)
	}
	if len(fake.requests) != 1 || len(fake.requests[0].Comments) != 1 || fake.requests[0].Comments[0].Line != 2 {
		t.Fatalf("forge saw %+v, want one review with the src/x.go:2 comment", fake.requests)
	}
	if !strings.Contains(fake.requests[0].Body, "summary") {
		t.Errorf("review body %q lacks the review comment", fake.requests[0].Body)
	}
	// The locks reached disk: a second submit has nothing to send.
	if _, err := submitComment(t, store, path); err == nil || !strings.Contains(err.Error(), "Nothing to submit") {
		t.Errorf("second submit: err = %v, want nothing to submit", err)
	}
}

func TestPartialSubmitReportsAndKeepsTheRest(t *testing.T) {
	fake := &submitForge{head: "head1234", result: &forge.SubmitResult{
		State:   "COMMENTED",
		Partial: &forge.PartialFailure{FailedAt: 0, Cause: errors.New("HTTP 500")},
	}}
	store, path := grantedSession(t, fake)

	result, err := submitComment(t, store, path)
	if !errors.Is(err, ErrPartialSubmit) {
		t.Fatalf("err = %v, want ErrPartialSubmit", err)
	}
	if result.Submitted || result.Partial == nil || result.Partial.UnpostedInline != 1 ||
		!strings.Contains(result.Partial.Error, "HTTP 500") {
		t.Errorf("result = %+v, want a partial with the inline comment unposted", result)
	}
	// The unposted comment stays a draft for the retry.
	fake.result = &forge.SubmitResult{ReviewID: "R10", State: "COMMENTED"}
	retry, err := submitComment(t, store, path)
	if err != nil || retry.InlineCount != 1 {
		t.Errorf("retry = %+v, %v; want the remaining inline comment posted", retry, err)
	}
}

func TestSubmitRefusesWhenTheHeadMoved(t *testing.T) {
	fake := &submitForge{head: "newhead99"}
	store, path := grantedSession(t, fake)

	_, err := submitComment(t, store, path)
	if err == nil || !strings.Contains(err.Error(), "advanced") {
		t.Fatalf("err = %v, want the moved-head refusal", err)
	}
	if len(fake.requests) != 0 {
		t.Fatal("a moved head must stop the submit before the forge")
	}
}

func TestSubmitOfALocalSessionIsRefused(t *testing.T) {
	store := &persistence.Store{ReviewsDir: filepath.Join(t.TempDir(), "reviews")}
	session := model.NewReviewSession("/repo", "abc", nil, model.SourceWorkingTree)
	session.ReviewComments = append(session.ReviewComments, model.NewComment("x", model.CommentTypeFromID("note"), nil))
	path, err := store.SaveSession(session)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"comment"}); err != nil {
		t.Fatal(err)
	}
	if _, err := submitComment(t, store, path); err == nil || !strings.Contains(err.Error(), "local review") {
		t.Fatalf("err = %v, want a local-review refusal", err)
	}
}
