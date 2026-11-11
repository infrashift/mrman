package agentsubmit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/reviewcli"
)

// resolverForge adds threads and resolution to the fake submit forge.
type resolverForge struct {
	*submitForge
	threads  []forge.RemoteReviewThread
	outcomes map[string]forge.ResolveOutcome // missing ids fail
	calls    []string
	replies  []string
}

func (f *resolverForge) ListReviewThreads(context.Context, *forge.PullRequestDetails) ([]forge.RemoteReviewThread, error) {
	return f.threads, nil
}

func (f *resolverForge) ReviewMetadata(context.Context, *forge.PullRequestDetails) (*forge.ReviewMetadata, error) {
	return &forge.ReviewMetadata{ViewerLogin: "vizzini-bot"}, nil
}

func (f *resolverForge) ResolveOwnThread(_ context.Context, _ *forge.PullRequestDetails, id, reply string) (forge.ResolveOutcome, error) {
	f.calls = append(f.calls, id)
	f.replies = append(f.replies, reply)
	if o, ok := f.outcomes[id]; ok {
		return o, nil
	}
	return "", errors.New("thread " + id + " was not started by this token's identity")
}

// sessionWithGrant writes the fake PR session with a human grant for events.
func sessionWithGrant(t *testing.T, fake forgeUnderTest, events []string) (*persistence.Store, string) {
	t.Helper()
	store, path := grantedSession(t, fake.base())
	registerFake(t, fake)
	session, err := store.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSessionActiveWithGrant(session, path, events); err != nil {
		t.Fatal(err)
	}
	return store, path
}

func resolve(t *testing.T, store *persistence.Store, opts ResolveOptions) (ResolveOutput, error) {
	t.Helper()
	var out bytes.Buffer
	err := Resolve(store, opts, &out)
	var got ResolveOutput
	if out.Len() > 0 {
		if jerr := json.Unmarshal(out.Bytes(), &got); jerr != nil {
			t.Fatalf("output is not JSON: %v (%s)", jerr, out.String())
		}
	}
	return got, err
}

func TestThreadsListsThePRsThreads(t *testing.T) {
	line := uint32(2)
	fake := &resolverForge{submitForge: &submitForge{head: "head1234"}, threads: []forge.RemoteReviewThread{
		{ID: "7", Path: "src/x.go", Line: &line, Comments: []forge.RemoteReviewComment{{ID: "1", Author: "vizzini-bot", Body: "b <!-- vizzini:fp=v1:ab -->"}}},
	}}
	store, path := sessionWithGrant(t, fake, nil)
	var out bytes.Buffer
	if err := Threads(store, reviewcli.Options{Session: path}, &out); err != nil {
		t.Fatal(err)
	}
	var got ThreadsOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.HeadSHA != "head1234" || len(got.Threads) != 1 ||
		got.Threads[0].ID != "7" || got.Threads[0].Comments[0].Author != "vizzini-bot" || got.PR == "" {
		t.Errorf("threads %s (%v)", out.String(), err)
	}

	// No threads is an empty list, not null.
	fake.threads = nil
	out.Reset()
	if err := Threads(store, reviewcli.Options{Session: path}, &out); err != nil || !bytes.Contains(out.Bytes(), []byte(`"threads":[]`)) {
		t.Errorf("empty threads %s (%v)", out.String(), err)
	}
}

// The human grant must name resolve: a comment grant does not cover it.
func TestResolveNeedsAGrantNamingResolve(t *testing.T) {
	fake := &resolverForge{submitForge: &submitForge{head: "head1234"}, outcomes: map[string]forge.ResolveOutcome{"7": forge.ResolveResolved}}
	store, path := sessionWithGrant(t, fake, []string{"comment"})
	_, err := resolve(t, store, ResolveOptions{Options: Options{Options: reviewcli.Options{Session: path}}, ThreadIDs: []string{"7"}})
	if denied, ok := errors.AsType[*SubmitDenied](err); !ok || denied.Reason != string(persistence.GrantEventNotAllowed) {
		t.Fatalf("err = %v, want event not allowed", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("resolved without a grant: %v", fake.calls)
	}

	store, path = sessionWithGrant(t, fake, []string{"comment", "resolve"})
	got, err := resolve(t, store, ResolveOptions{Options: Options{Options: reviewcli.Options{Session: path}}, ThreadIDs: []string{"7", "7"}, Reply: "fixed"})
	if err != nil || !slices.Equal(got.Resolved, []string{"7"}) || !slices.Equal(fake.calls, []string{"7"}) || fake.replies[0] != "fixed" {
		t.Errorf("resolve %+v (%v), calls %v", got, err, fake.calls)
	}
}

func TestResolveByPolicy(t *testing.T) {
	fake := &resolverForge{submitForge: &submitForge{head: "head1234"},
		outcomes: map[string]forge.ResolveOutcome{"7": forge.ResolveResolved, "8": forge.ResolveAlreadyResolved}}
	store, path := sessionWithGrant(t, fake, nil) // no human grant at all
	input := fakeGarmr(t, `{"decision":"allow","results":[]}`, 0, nil)
	opts := ResolveOptions{
		Options:   Options{Options: reviewcli.Options{Session: path}, GrantPolicy: policyDir(t), RunID: "r1", Agent: "vizzini-bot"},
		ThreadIDs: []string{"8", "7", "9"},
	}
	got, err := resolve(t, store, opts)
	if !errors.Is(err, ErrPartialResolve) {
		t.Fatalf("err = %v, want a partial resolve (9 is not ours)", err)
	}
	if !slices.Equal(got.Resolved, []string{"7"}) || !slices.Equal(got.AlreadyResolved, []string{"8"}) ||
		len(got.Failed) != 1 || got.Failed[0].Thread != "9" || got.Authorization == nil || !got.Authorization.Allowed {
		t.Errorf("output %+v", got)
	}
	var sent PolicyInput
	if err := json.Unmarshal(*input, &sent); err != nil || sent.Event != "resolve" || sent.Comments != 3 ||
		sent.Identity != "vizzini-bot" || sent.RunID != "r1" {
		t.Errorf("policy input %s", *input)
	}
	if _, err := os.Stat(path + ".audit.jsonl"); err != nil {
		t.Errorf("no audit log: %v", err)
	}

	// A deny resolves nothing.
	fake.calls = nil
	fakeGarmr(t, `{"decision":"deny","results":[{"rule_id":"MRM-001","passed":false,"message":"event resolve is not allowed unattended"}]}`, 1, nil)
	if _, err := resolve(t, store, opts); err == nil {
		t.Fatal("a deny must refuse")
	} else if _, ok := errors.AsType[*PolicyDenied](err); !ok {
		t.Errorf("err = %T %v, want a policy denial", err, err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("resolved after a deny: %v", fake.calls)
	}
}

func TestResolveRefusals(t *testing.T) {
	plain := &submitForge{head: "head1234"} // no ThreadResolver
	store, path := sessionWithGrant(t, plainForge{plain}, []string{"resolve"})
	if _, err := resolve(t, store, ResolveOptions{Options: Options{Options: reviewcli.Options{Session: path}}, ThreadIDs: []string{"1"}}); err == nil ||
		!strings.Contains(err.Error(), "not supported") {
		t.Errorf("unsupported forge: %v", err)
	}
	if _, err := resolve(t, store, ResolveOptions{Options: Options{Options: reviewcli.Options{Session: path}}}); err == nil {
		t.Error("no thread ids must be an error")
	}
}

// forgeUnderTest lets sessionWithGrant register either fake.
type forgeUnderTest interface {
	forge.Forge
	base() *submitForge
}

func (f *resolverForge) base() *submitForge { return f.submitForge }

// plainForge is the submit fake without thread resolution.
type plainForge struct{ *submitForge }

func (f plainForge) base() *submitForge { return f.submitForge }

// registerFake makes f the Forgejo driver; grantedSession's cleanup
// restores the real one.
func registerFake(t *testing.T, f forge.Forge) {
	t.Helper()
	forge.Register(forge.Driver{ID: forgetypes.KindForgejo, SlugPrefix: "fj",
		New: func(forge.HostConfig) (forge.Forge, error) { return f, nil }})
}
