package githubf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

// reviewEditServer serves the lookup of review PRR_7 and the
// updatePullRequestReview mutation, recording the mutations it receives.
type reviewEditServer struct {
	t      *testing.T
	viewer string // the token's login
	author string // the review's author; "" for a deleted account
	number int    // the review's pull request; 0 means 42
	calls  []gqlRequest
}

func (s *reviewEditServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req gqlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.t.Errorf("decode graphql request: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasPrefix(req.Query, "mutation"):
		s.calls = append(s.calls, req)
		_, _ = fmt.Fprint(w, `{"data":{"updatePullRequestReview":{"pullRequestReview":{"id":"PRR_7"}}}}`)
	case strings.Contains(req.Query, "node(id: $id)") && strings.Contains(req.Query, "PullRequestReview"):
		if req.Variables["id"] != "PRR_7" {
			// A node of another type answers an empty fragment.
			_, _ = fmt.Fprintf(w, `{"data":{"viewer":{"login":%q},"node":{}}}`, s.viewer)
			return
		}
		number := s.number
		if number == 0 {
			number = 42
		}
		author := "null"
		if s.author != "" {
			author = fmt.Sprintf(`{"login":%q}`, s.author)
		}
		_, _ = fmt.Fprintf(w, `{"data":{"viewer":{"login":%q},"node":{"id":"PRR_7","author":%s,
			"pullRequest":{"number":%d,"repository":{"name":"hello","owner":{"login":"octo"}}}}}}`,
			s.viewer, author, number)
	default:
		s.t.Errorf("unexpected graphql query %s", req.Query)
	}
}

func TestEditOwnSummary(t *testing.T) {
	// A GITHUB_TOKEN review's author is the bare app login; the viewer
	// carries the [bot] suffix.
	s := &reviewEditServer{t: t, viewer: "github-actions[bot]", author: "github-actions"}
	d := newTestDriver(t, s)
	if err := d.EditOwnSummary(context.Background(), testPR(), "PRR_7", "New status."); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 1 {
		t.Fatalf("mutations %d, want 1", len(s.calls))
	}
	input, _ := s.calls[0].Variables["input"].(map[string]any)
	if input["pullRequestReviewId"] != "PRR_7" || input["body"] != "New status." {
		t.Errorf("input %v", input)
	}
}

func TestEditOwnSummaryRefusesOthersReviews(t *testing.T) {
	for name, author := range map[string]string{"another login": "alice", "a deleted account": ""} {
		t.Run(name, func(t *testing.T) {
			s := &reviewEditServer{t: t, viewer: "vizzini-bot", author: author}
			d := newTestDriver(t, s)
			wantForgeErr(t, d.EditOwnSummary(context.Background(), testPR(), "PRR_7", "x"), forge.ErrorForbidden)
			if len(s.calls) != 0 {
				t.Error("edited another's review")
			}
		})
	}
}

func TestEditOwnSummaryRefusesAReviewOfAnotherPullRequest(t *testing.T) {
	s := &reviewEditServer{t: t, viewer: "vizzini-bot", author: "vizzini-bot", number: 41}
	d := newTestDriver(t, s)
	wantForgeErr(t, d.EditOwnSummary(context.Background(), testPR(), "PRR_7", "x"), forge.ErrorNotFound)
	if len(s.calls) != 0 {
		t.Error("edited a review of another pull request")
	}
}

// A thread id is not a review id.
func TestEditOwnSummaryRefusesOtherNodes(t *testing.T) {
	s := &reviewEditServer{t: t, viewer: "vizzini-bot", author: "vizzini-bot"}
	d := newTestDriver(t, s)
	wantForgeErr(t, d.EditOwnSummary(context.Background(), testPR(), "PRRT_7", "x"), forge.ErrorNotFound)
	if len(s.calls) != 0 {
		t.Error("mutated a node that is not a review")
	}
}

func TestEditOwnSummaryValidates(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	wantForgeErr(t, d.EditOwnSummary(context.Background(), testPR(), "PRR_7", "  "), forge.ErrorValidation)
	wantForgeErr(t, d.EditOwnSummary(context.Background(), testPR(), " ", "body"), forge.ErrorValidation)
}
