package gitlabf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

// testDiscussion is the discussion id the thread-write fixtures serve.
const testDiscussion = "6a9c1750b37d513a43987b574953fceb50b03ce7"

// discussionPath is testDiscussion's API path on testRepo's MR 42.
const discussionPath = projectPrefix + "/discussions/" + testDiscussion

// threadWriteFixture serves the current user, discussion testDiscussion and
// the two writes to it, recording the writes in order.
type threadWriteFixture struct {
	viewer     string
	author     string
	resolved   bool
	resolvable bool
	writes     []string
}

func (f *threadWriteFixture) mux(t *testing.T) *fixtureMux {
	m := newFixtureMux(t)
	m.JSON("GET /api/v4/user", fmt.Sprintf(`{"id": 9, "username": %q}`, f.viewer))
	m.JSON("GET "+discussionPath, fmt.Sprintf(`{"id": %q, "individual_note": false, "notes": [
		{"id": 100, "body": "finding", "author": {"id": 3, "username": %q},
		 "system": false, "resolvable": %t, "resolved": %t},
		{"id": 101, "body": "a reply", "author": {"id": 4, "username": "bob"}, "system": false}
	]}`, testDiscussion, f.author, f.resolvable, f.resolved))
	m.Handle("POST "+discussionPath+"/notes", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("note body is not JSON: %v", err)
		}
		f.writes = append(f.writes, fmt.Sprintf("note %v", body["body"]))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 555, "body": "ok", "author": {"username": "vizzini-bot"}}`))
	})
	m.Handle("PUT "+discussionPath, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		resolved := r.URL.Query().Get("resolved")
		if resolved == "" {
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			resolved = fmt.Sprint(body["resolved"])
		}
		f.writes = append(f.writes, "resolved="+resolved)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": "` + testDiscussion + `", "notes": []}`))
	})
	return m
}

func TestReplyToThreadAddsANote(t *testing.T) {
	f := &threadWriteFixture{viewer: "vizzini-bot", author: "alice"}
	d := newTestDriver(t, f.mux(t))
	// The parent is ignored: GitLab discussions are flat. Replying is not
	// limited to the token's own threads, so no ownership lookup is made.
	id, err := d.ReplyToThread(context.Background(), testPR(), testDiscussion, "100", "The answer.")
	if err != nil {
		t.Fatalf("ReplyToThread: %v", err)
	}
	if id != "555" {
		t.Errorf("comment id %q, want the new note's id", id)
	}
	if strings.Join(f.writes, "|") != "note The answer." {
		t.Errorf("writes %v", f.writes)
	}
}

func TestReplyToThreadValidates(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	for _, id := range []string{"", "abc/../../x", "abc?resolved=true"} {
		_, err := d.ReplyToThread(context.Background(), testPR(), id, "", "body")
		wantForgeErr(t, err, forge.ErrorValidation)
	}
	_, err := d.ReplyToThread(context.Background(), testPR(), testDiscussion, "", " \n")
	wantForgeErr(t, err, forge.ErrorValidation)
	_, err = d.ResolveOwnThread(context.Background(), testPR(), "a b", "")
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestReplyToThreadForbidden(t *testing.T) {
	m := newFixtureMux(t)
	m.Handle("POST "+discussionPath+"/notes", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message": "403 Forbidden"}`))
	})
	d := newTestDriver(t, m)
	_, err := d.ReplyToThread(context.Background(), testPR(), testDiscussion, "", "hi")
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorForbidden || fe.Status != http.StatusForbidden || fe.Hint != hintForbidden {
		t.Errorf("error %v (status %d, hint %q)", fe, fe.Status, fe.Hint)
	}
}

func TestReplyToThreadOfAnotherMergeRequestIsNotFound(t *testing.T) {
	m := newFixtureMux(t)
	m.Handle("POST "+discussionPath+"/notes", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message": "404 Not found"}`))
	})
	d := newTestDriver(t, m)
	_, err := d.ReplyToThread(context.Background(), testPR(), testDiscussion, "", "hi")
	wantForgeErr(t, err, forge.ErrorNotFound)
}

func TestResolveOwnThreadRepliesThenResolves(t *testing.T) {
	f := &threadWriteFixture{viewer: "Vizzini-Bot", author: "vizzini-bot", resolvable: true}
	d := newTestDriver(t, f.mux(t))
	got, err := d.ResolveOwnThread(context.Background(), testPR(), testDiscussion, "Fixed in abc123.")
	if err != nil {
		t.Fatalf("ResolveOwnThread: %v", err)
	}
	if got != forge.ResolveResolved {
		t.Errorf("outcome %q", got)
	}
	if want := "note Fixed in abc123.|resolved=true"; strings.Join(f.writes, "|") != want {
		t.Errorf("writes %v, want %s", f.writes, want)
	}
}

func TestResolveOwnThreadWithoutReplyOnlyResolves(t *testing.T) {
	f := &threadWriteFixture{viewer: "vizzini-bot", author: "vizzini-bot", resolvable: true}
	d := newTestDriver(t, f.mux(t))
	if _, err := d.ResolveOwnThread(context.Background(), testPR(), testDiscussion, ""); err != nil {
		t.Fatalf("ResolveOwnThread: %v", err)
	}
	if strings.Join(f.writes, "|") != "resolved=true" {
		t.Errorf("writes %v", f.writes)
	}
}

func TestResolveOwnThreadRefusesSomeoneElsesThread(t *testing.T) {
	for name, author := range map[string]string{"a person": "alice", "no author": ""} {
		t.Run(name, func(t *testing.T) {
			f := &threadWriteFixture{viewer: "vizzini-bot", author: author, resolvable: true}
			d := newTestDriver(t, f.mux(t))
			_, err := d.ResolveOwnThread(context.Background(), testPR(), testDiscussion, "closing")
			fe := mustForgeErr(t, err)
			if fe.Kind != forge.ErrorForbidden || fe.Hint != hintNotOwnThread {
				t.Errorf("error %v (hint %q), want the not-own-thread refusal", fe, fe.Hint)
			}
			if len(f.writes) != 0 {
				t.Errorf("a refused thread was written to: %v", f.writes)
			}
		})
	}
}

func TestResolveOwnThreadAlreadyResolvedIsLeftAlone(t *testing.T) {
	f := &threadWriteFixture{viewer: "vizzini-bot", author: "vizzini-bot", resolvable: true, resolved: true}
	d := newTestDriver(t, f.mux(t))
	got, err := d.ResolveOwnThread(context.Background(), testPR(), testDiscussion, "Fixed.")
	if err != nil {
		t.Fatalf("ResolveOwnThread: %v", err)
	}
	if got != forge.ResolveAlreadyResolved {
		t.Errorf("outcome %q", got)
	}
	if len(f.writes) != 0 {
		t.Errorf("an already-resolved thread was written to: %v", f.writes)
	}
}

func TestResolveOwnThreadRefusesAnUnresolvableNote(t *testing.T) {
	f := &threadWriteFixture{viewer: "vizzini-bot", author: "vizzini-bot"}
	d := newTestDriver(t, f.mux(t))
	_, err := d.ResolveOwnThread(context.Background(), testPR(), testDiscussion, "Fixed.")
	wantForgeErr(t, err, forge.ErrorValidation)
	if len(f.writes) != 0 {
		t.Errorf("an unresolvable note was written to: %v", f.writes)
	}
}

func TestResolveOwnThreadErrors(t *testing.T) {
	forbidden := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message": "403 Forbidden"}`))
	}
	t.Run("current user unauthorized", func(t *testing.T) {
		m := newFixtureMux(t)
		m.Handle("GET /api/v4/user", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message": "401 Unauthorized"}`))
		})
		_, err := newTestDriver(t, m).ResolveOwnThread(context.Background(), testPR(), testDiscussion, "")
		fe := mustForgeErr(t, err)
		if fe.Kind != forge.ErrorAuth || !strings.Contains(fe.Hint, "personal_access_tokens") {
			t.Errorf("error %v (hint %q)", fe, fe.Hint)
		}
	})
	t.Run("discussion not found", func(t *testing.T) {
		m := newFixtureMux(t)
		m.JSON("GET /api/v4/user", `{"id": 9, "username": "vizzini-bot"}`)
		m.Handle("GET "+discussionPath, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message": "404 Not found"}`))
		})
		_, err := newTestDriver(t, m).ResolveOwnThread(context.Background(), testPR(), testDiscussion, "")
		wantForgeErr(t, err, forge.ErrorNotFound)
	})
	t.Run("empty discussion", func(t *testing.T) {
		m := newFixtureMux(t)
		m.JSON("GET /api/v4/user", `{"id": 9, "username": "vizzini-bot"}`)
		m.JSON("GET "+discussionPath, `{"id": "`+testDiscussion+`", "notes": []}`)
		_, err := newTestDriver(t, m).ResolveOwnThread(context.Background(), testPR(), testDiscussion, "")
		wantForgeErr(t, err, forge.ErrorNotFound)
	})
	t.Run("resolve forbidden", func(t *testing.T) {
		f := &threadWriteFixture{viewer: "vizzini-bot", author: "vizzini-bot", resolvable: true}
		m := f.mux(t)
		m.Handle("PUT "+discussionPath, forbidden)
		_, err := newTestDriver(t, m).ResolveOwnThread(context.Background(), testPR(), testDiscussion, "")
		fe := mustForgeErr(t, err)
		if fe.Kind != forge.ErrorForbidden || fe.Hint != hintForbidden {
			t.Errorf("error %v (hint %q)", fe, fe.Hint)
		}
	})
	t.Run("reply forbidden stops before resolving", func(t *testing.T) {
		f := &threadWriteFixture{viewer: "vizzini-bot", author: "vizzini-bot", resolvable: true}
		m := f.mux(t)
		m.Handle("POST "+discussionPath+"/notes", forbidden)
		_, err := newTestDriver(t, m).ResolveOwnThread(context.Background(), testPR(), testDiscussion, "Fixed.")
		wantForgeErr(t, err, forge.ErrorForbidden)
		if len(f.writes) != 0 {
			t.Errorf("resolved after the reply failed: %v", f.writes)
		}
	})
}
