package agentsubmit

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/reviewcli"
)

// prSession writes a pull-request session to a fresh store and returns both.
func prSession(t *testing.T) (*persistence.Store, *model.ReviewSession, string) {
	t.Helper()
	store := &persistence.Store{ReviewsDir: filepath.Join(t.TempDir(), "reviews")}
	repo := forgetypes.Repository{
		Kind: forgetypes.KindGitHub, Host: "github.com", Owner: "acme", Name: "widget",
	}
	session := model.NewReviewSession("forge:github.com/acme/widget", "head1234", nil,
		model.SourcePullRequest)
	session.PrSessionKey = &forgetypes.PrSessionKey{
		Repository: repo, Number: 7, HeadSHA: "head1234",
	}
	session.ReviewComments = append(session.ReviewComments,
		model.NewComment("please fix", model.CommentTypeFromID("issue"), nil))

	path, err := store.SaveSession(session)
	if err != nil {
		t.Fatalf("save session: %v", err)
	}
	return store, session, path
}

// submitTo runs a submit and returns the denial, failing if one is absent.
//
// Every case here must be refused *before* any forge work, which is why
// these tests need no driver and no network: reaching either would mean the
// interlock let the call through.
func denialFrom(t *testing.T, store *persistence.Store, session, event string) *SubmitDenied {
	t.Helper()
	var out bytes.Buffer
	err := Submit(store, Options{
		Options: reviewcli.Options{Session: session},
		Event:   event,
	}, &out)
	if err == nil {
		t.Fatalf("submit was allowed; it must be refused (output: %s)", out.String())
	}
	var denied *SubmitDenied
	if !errors.As(err, &denied) {
		t.Fatalf("expected an interlock denial, got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("a refused submit must write nothing, wrote %q", out.String())
	}
	return denied
}

func TestSubmitRefusedWithoutAGrant(t *testing.T) {
	store, _, path := prSession(t)

	d := denialFrom(t, store, path, "comment")
	if d.Reason != string(persistence.GrantNone) {
		t.Errorf("reason = %q, want %q", d.Reason, persistence.GrantNone)
	}
	if d.Code != "agent_submit_not_permitted" {
		t.Errorf("error code = %q", d.Code)
	}
	if len(d.GrantedEvents) != 0 {
		t.Errorf("granted = %v, want none", d.GrantedEvents)
	}
	// The remedy must be something only a human can perform.
	if !strings.Contains(d.Message, "--auto") || !strings.Contains(d.Message, "Ask the user") {
		t.Errorf("message must direct the agent to ask a human, got %q", d.Message)
	}
}

func TestSubmitRefusedForAnUngrantedEvent(t *testing.T) {
	store, session, path := prSession(t)
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"comment", "draft"}); err != nil {
		t.Fatal(err)
	}

	d := denialFrom(t, store, path, "approve")
	if d.Reason != string(persistence.GrantEventNotAllowed) {
		t.Errorf("reason = %q, want %q", d.Reason, persistence.GrantEventNotAllowed)
	}
	if len(d.GrantedEvents) != 2 {
		t.Errorf("the denial must report what IS granted, got %v", d.GrantedEvents)
	}
	// Granting comment must never imply granting approve: approving is what
	// gets code merged.
	if strings.Contains(strings.Join(d.GrantedEvents, ","), "approve") {
		t.Error("approve leaked into a comment,draft grant")
	}
}

func TestSubmitRefusedWhenTheGrantingProcessIsGone(t *testing.T) {
	store, session, path := prSession(t)
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"comment"}); err != nil {
		t.Fatal(err)
	}
	// Rewrite the entry's pid to one that cannot be running. This is what
	// the registry sees after a TUI exits or crashes.
	killGrantingProcess(t, store)

	d := denialFrom(t, store, path, "comment")
	if d.Reason != string(persistence.GrantExpired) {
		t.Errorf("reason = %q, want %q — an expired grant must be "+
			"distinguishable from one that never existed", d.Reason, persistence.GrantExpired)
	}
}

func TestSubmitRefusedWhenTheGrantIsForAnotherSession(t *testing.T) {
	store, session, path := prSession(t)
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"comment"}); err != nil {
		t.Fatal(err)
	}

	// A second session, with no grant of its own.
	other := model.NewReviewSession("forge:github.com/acme/widget", "otherhead", nil,
		model.SourcePullRequest)
	other.PrSessionKey = &forgetypes.PrSessionKey{
		Repository: session.PrSessionKey.Repository, Number: 8, HeadSHA: "otherhead",
	}
	otherPath, err := store.SaveSession(other)
	if err != nil {
		t.Fatal(err)
	}

	d := denialFrom(t, store, otherPath, "comment")
	if d.Reason != string(persistence.GrantNone) {
		t.Errorf("a grant must not carry to another session: reason = %q", d.Reason)
	}
}

func TestRevokedGrantRefuses(t *testing.T) {
	store, session, path := prSession(t)
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"comment"}); err != nil {
		t.Fatal(err)
	}
	if _, reason, _ := store.AgentSubmitGrant(path, "comment"); reason != persistence.GrantOK {
		t.Fatalf("expected the grant to start valid, got %q", reason)
	}

	if err := store.RevokeGrantForPid(); err != nil {
		t.Fatal(err)
	}
	d := denialFrom(t, store, path, "comment")
	if d.Reason != string(persistence.GrantNone) {
		t.Errorf(":agent off must leave no grant behind, reason = %q", d.Reason)
	}
}

func TestUnknownEventIsRejectedBeforeAnythingElse(t *testing.T) {
	store, _, path := prSession(t)
	var out bytes.Buffer
	err := Submit(store, Options{
		Options: reviewcli.Options{Session: path},
		Event:   "merge",
	}, &out)
	if err == nil {
		t.Fatal("an unknown event must be rejected")
	}
	if !strings.Contains(err.Error(), "unknown submit event") {
		t.Errorf("error = %v", err)
	}
}

func TestDenialIsMachineReadable(t *testing.T) {
	store, _, path := prSession(t)
	d := denialFrom(t, store, path, "comment")

	var buf bytes.Buffer
	if err := WriteDenial(&buf, d); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("a denial must be parseable JSON: %v", err)
	}
	for _, key := range []string{"error", "reason", "requested_event", "granted_events", "message"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("denial JSON is missing %q", key)
		}
	}
}

// killGrantingProcess rewrites the registry so the grant's owning pid is one
// that cannot be alive, simulating a closed TUI.
func killGrantingProcess(t *testing.T, store *persistence.Store) {
	t.Helper()
	path := filepath.Join(store.ReviewsDir, "active_sessions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read active sessions: %v", err)
	}
	var file struct {
		Version  string           `json:"version"`
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("parse active sessions: %v", err)
	}
	for i := range file.Sessions {
		// PID 0 is never a live user process.
		file.Sessions[i]["pid"] = 0
	}
	out, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}
