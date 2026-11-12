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

// threadWriteServer serves the thread lookup and the two thread mutations
// for review thread PRRT_7, recording the mutations it receives in order.
type threadWriteServer struct {
	t        *testing.T
	viewer   string // the token's login
	author   string // the thread's first comment author; "" for a deleted account
	resolved bool
	owner    string // the thread's repository owner; "" means octo
	number   int    // the thread's pull request; 0 means 42
	mutErr   string // a GraphQL error answered to every mutation
	calls    []gqlRequest
}

func (s *threadWriteServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req gqlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.t.Errorf("decode graphql request: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasPrefix(req.Query, "mutation"):
		s.calls = append(s.calls, req)
		if s.mutErr != "" {
			_, _ = fmt.Fprintf(w, `{"data":null,"errors":[{"type":"FORBIDDEN","message":%q}]}`, s.mutErr)
			return
		}
		if strings.Contains(req.Query, "addPullRequestReviewThreadReply") {
			_, _ = fmt.Fprint(w, `{"data":{"addPullRequestReviewThreadReply":{"comment":{"id":"PRRC_new"}}}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":{"resolveReviewThread":{"thread":{"isResolved":true}}}}`)
	case strings.Contains(req.Query, "node(id: $id)"):
		if req.Variables["id"] != "PRRT_7" {
			_, _ = fmt.Fprintf(w, `{"data":{"viewer":{"login":%q},"node":null},"errors":[{"type":"NOT_FOUND",
				"message":"Could not resolve to a node with the global id of '%v'"}]}`, s.viewer, req.Variables["id"])
			return
		}
		owner, number := s.owner, s.number
		if owner == "" {
			owner = "octo"
		}
		if number == 0 {
			number = 42
		}
		author := "null"
		if s.author != "" {
			author = fmt.Sprintf(`{"login":%q}`, s.author)
		}
		_, _ = fmt.Fprintf(w, `{"data":{"viewer":{"login":%q},"node":{"id":"PRRT_7","isResolved":%t,
			"pullRequest":{"number":%d,"repository":{"name":"hello","owner":{"login":%q}}},
			"comments":{"nodes":[{"id":"PRRC_1","author":%s}]}}}}`,
			s.viewer, s.resolved, number, owner, author)
	default:
		s.t.Errorf("unexpected graphql query %s", req.Query)
	}
}

// mutations names the mutations the server received, in order.
func (s *threadWriteServer) mutations() []string {
	var out []string
	for _, c := range s.calls {
		switch {
		case strings.Contains(c.Query, "addPullRequestReviewThreadReply"):
			input, _ := c.Variables["input"].(map[string]any)
			out = append(out, fmt.Sprintf("reply %v %v", input["pullRequestReviewThreadId"], input["body"]))
		case strings.Contains(c.Query, "resolveReviewThread"):
			input, _ := c.Variables["input"].(map[string]any)
			out = append(out, fmt.Sprintf("resolve %v", input["threadId"]))
		}
	}
	return out
}

func TestReplyToThreadPostsAndReturnsNodeID(t *testing.T) {
	s := &threadWriteServer{t: t, viewer: "vizzini-bot", author: "alice"}
	d := newTestDriver(t, s)
	// The parent is ignored: GitHub threads are flat. Replying is not
	// limited to the token's own threads.
	id, err := d.ReplyToThread(context.Background(), testPR(), "PRRT_7", "PRRC_1", "The answer.")
	if err != nil {
		t.Fatalf("ReplyToThread: %v", err)
	}
	if id != "PRRC_new" {
		t.Errorf("comment id %q, want the reply's node id", id)
	}
	if got := s.mutations(); len(got) != 1 || got[0] != "reply PRRT_7 The answer." {
		t.Errorf("mutations %v", got)
	}
}

func TestReplyToThreadValidates(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	_, err := d.ReplyToThread(context.Background(), testPR(), "PRRT_7", "", "  ")
	wantForgeErr(t, err, forge.ErrorValidation)
	_, err = d.ReplyToThread(context.Background(), testPR(), " ", "", "body")
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestReplyToThreadRefusesAThreadOfAnotherPullRequest(t *testing.T) {
	for name, s := range map[string]*threadWriteServer{
		"other number": {number: 41},
		"other repo":   {owner: "someone"},
	} {
		t.Run(name, func(t *testing.T) {
			s.t, s.viewer, s.author = t, "vizzini-bot", "vizzini-bot"
			d := newTestDriver(t, s)
			_, err := d.ReplyToThread(context.Background(), testPR(), "PRRT_7", "", "hi")
			wantForgeErr(t, err, forge.ErrorNotFound)
			if len(s.calls) != 0 {
				t.Errorf("mutated a thread of another pull request: %v", s.mutations())
			}
		})
	}
}

func TestReplyToThreadUnknownThreadIsNotFound(t *testing.T) {
	s := &threadWriteServer{t: t, viewer: "vizzini-bot"}
	d := newTestDriver(t, s)
	_, err := d.ReplyToThread(context.Background(), testPR(), "7", "", "hi")
	wantForgeErr(t, err, forge.ErrorNotFound)
}

func TestReplyToThreadForbidden(t *testing.T) {
	s := &threadWriteServer{t: t, viewer: "vizzini-bot", author: "alice", mutErr: "Resource not accessible by integration"}
	d := newTestDriver(t, s)
	_, err := d.ReplyToThread(context.Background(), testPR(), "PRRT_7", "", "hi")
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorForbidden || !strings.Contains(fe.Hint, "scopes") {
		t.Errorf("error %v (hint %q), want forbidden with the scopes hint", fe, fe.Hint)
	}
}

func TestReplyToThreadHTTPForbidden(t *testing.T) {
	d := newTestDriver(t, errorServer(http.StatusForbidden, `{"message":"Forbidden"}`, nil))
	_, err := d.ReplyToThread(context.Background(), testPR(), "PRRT_7", "", "hi")
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorForbidden || fe.Status != http.StatusForbidden || fe.Hint != hintForbidden {
		t.Errorf("error %v (status %d, hint %q)", fe, fe.Status, fe.Hint)
	}
}

func TestResolveOwnThreadRepliesThenResolves(t *testing.T) {
	// Logins compare case-insensitively, and an app's [bot] suffix is
	// ignored on either side.
	s := &threadWriteServer{t: t, viewer: "Vizzini-Bot[bot]", author: "vizzini-bot"}
	d := newTestDriver(t, s)
	got, err := d.ResolveOwnThread(context.Background(), testPR(), "PRRT_7", "Fixed in abc123.")
	if err != nil {
		t.Fatalf("ResolveOwnThread: %v", err)
	}
	if got != forge.ResolveResolved {
		t.Errorf("outcome %q", got)
	}
	want := []string{"reply PRRT_7 Fixed in abc123.", "resolve PRRT_7"}
	if got := s.mutations(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("mutations %v, want %v", got, want)
	}
}

func TestResolveOwnThreadWithoutReplyOnlyResolves(t *testing.T) {
	s := &threadWriteServer{t: t, viewer: "vizzini-bot", author: "vizzini-bot"}
	d := newTestDriver(t, s)
	if _, err := d.ResolveOwnThread(context.Background(), testPR(), "PRRT_7", ""); err != nil {
		t.Fatalf("ResolveOwnThread: %v", err)
	}
	if got := s.mutations(); len(got) != 1 || got[0] != "resolve PRRT_7" {
		t.Errorf("mutations %v", got)
	}
}

func TestResolveOwnThreadRefusesSomeoneElsesThread(t *testing.T) {
	for name, author := range map[string]string{"a person": "alice", "a deleted account": ""} {
		t.Run(name, func(t *testing.T) {
			s := &threadWriteServer{t: t, viewer: "vizzini-bot", author: author}
			d := newTestDriver(t, s)
			_, err := d.ResolveOwnThread(context.Background(), testPR(), "PRRT_7", "closing")
			fe := mustForgeErr(t, err)
			if fe.Kind != forge.ErrorForbidden || fe.Hint != hintNotOwnThread {
				t.Errorf("error %v (hint %q), want the not-own-thread refusal", fe, fe.Hint)
			}
			if len(s.calls) != 0 {
				t.Errorf("a refused thread was written to: %v", s.mutations())
			}
		})
	}
}

func TestResolveOwnThreadAlreadyResolvedIsLeftAlone(t *testing.T) {
	s := &threadWriteServer{t: t, viewer: "vizzini-bot", author: "vizzini-bot", resolved: true}
	d := newTestDriver(t, s)
	got, err := d.ResolveOwnThread(context.Background(), testPR(), "PRRT_7", "Fixed.")
	if err != nil {
		t.Fatalf("ResolveOwnThread: %v", err)
	}
	if got != forge.ResolveAlreadyResolved {
		t.Errorf("outcome %q", got)
	}
	if len(s.calls) != 0 {
		t.Errorf("an already-resolved thread was written to: %v", s.mutations())
	}
}

func TestResolveOwnThreadMutationForbidden(t *testing.T) {
	s := &threadWriteServer{t: t, viewer: "vizzini-bot", author: "vizzini-bot",
		mutErr: "vizzini-bot does not have the correct permissions to execute `ResolveReviewThread`"}
	d := newTestDriver(t, s)
	_, err := d.ResolveOwnThread(context.Background(), testPR(), "PRRT_7", "")
	wantForgeErr(t, err, forge.ErrorForbidden)
	// With a reply, the failing reply stops the call before the resolve.
	s.calls = nil
	_, err = d.ResolveOwnThread(context.Background(), testPR(), "PRRT_7", "Fixed.")
	wantForgeErr(t, err, forge.ErrorForbidden)
	if got := s.mutations(); len(got) != 1 || !strings.HasPrefix(got[0], "reply") {
		t.Errorf("mutations %v, want only the failed reply", got)
	}
}

func TestResolveOwnThreadUnauthorized(t *testing.T) {
	d := newTestDriver(t, errorServer(http.StatusUnauthorized, `{"message":"Bad credentials"}`, nil))
	_, err := d.ResolveOwnThread(context.Background(), testPR(), "PRRT_7", "")
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorAuth || fe.Hint != hintAuth {
		t.Errorf("error %v (hint %q)", fe, fe.Hint)
	}
}

func TestSameLogin(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"bot", "bot", true},
		{"Bot", "bot", true},
		{"bot[bot]", "bot", true},
		{"bot", "other", false},
		{"", "", false},
		{"[bot]", "", false},
	}
	for _, c := range cases {
		if got := sameLogin(c.a, c.b); got != c.want {
			t.Errorf("sameLogin(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
