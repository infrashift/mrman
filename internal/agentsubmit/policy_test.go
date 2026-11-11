package agentsubmit

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func policyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent-submit.cue"), []byte("package mrman\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fakeGarmr replaces the engine for one test.
func fakeGarmr(t *testing.T, stdout string, code int, err error) *[]byte {
	t.Helper()
	var got []byte
	prev := runGarmr
	runGarmr = func(_ context.Context, _, _, _ string, input []byte) ([]byte, int, error) {
		got = input
		return []byte(stdout), code, err
	}
	t.Cleanup(func() { runGarmr = prev })
	return &got
}

func TestAuthorize_Allow(t *testing.T) {
	input := fakeGarmr(t, `{"decision":"allow","results":[]}`, 0, nil)
	opts := &Options{GrantPolicy: policyDir(t), Event: "comment"}
	rec, err := authorize(context.Background(), opts, &PolicyInput{Event: "comment", Identity: "vizzini-bot", Comments: 3, RunID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Allowed || rec.Decision != "allow" || rec.Namespace != "mrman" || len(rec.PolicyDigest) != 64 {
		t.Errorf("record %+v", rec)
	}
	var sent PolicyInput
	if err := json.Unmarshal(*input, &sent); err != nil || sent.APIVersion != "mrman.infrashift.io/v1" || sent.Kind != "AgentSubmit" ||
		sent.Identity != "vizzini-bot" || sent.RunID != "r1" {
		t.Errorf("garmr input %s (%v)", *input, err)
	}
}

func TestAuthorize_Deny(t *testing.T) {
	fakeGarmr(t, `{"decision":"deny","results":[{"rule_id":"MRM-001","passed":false,"message":"event approve is not allowed unattended"}]}`, 1, nil)
	rec, err := authorize(context.Background(), &Options{GrantPolicy: policyDir(t)}, &PolicyInput{Event: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Allowed || !slices.Equal(rec.Failed, []string{"MRM-001"}) {
		t.Errorf("record %+v", rec)
	}
	d := policyDenial(rec)
	if d.Code != "policy_denied" || !strings.Contains(d.Message, "not allowed unattended") {
		t.Errorf("denial %+v", d)
	}
}

// Anything short of a clean, consistent allow refuses: never fail open.
func TestAuthorize_FailsClosed(t *testing.T) {
	for name, tc := range map[string]struct {
		stdout string
		code   int
		err    error
	}{
		"engine missing":            {"", -1, exec.ErrNotFound},
		"evaluation error (exit 2)": {`{"decision":"allow"}`, 2, nil},
		"unreadable output":         {"not json", 0, nil},
		"exit 0 but decision deny":  {`{"decision":"deny"}`, 0, nil},
		"no decision":               {`{}`, 0, nil},
	} {
		fakeGarmr(t, tc.stdout, tc.code, tc.err)
		if rec, err := authorize(context.Background(), &Options{GrantPolicy: policyDir(t)}, &PolicyInput{Event: "comment"}); err == nil {
			t.Errorf("%s: authorized %+v", name, rec)
		}
	}
	if _, err := authorize(context.Background(), &Options{GrantPolicy: t.TempDir()}, &PolicyInput{}); err == nil ||
		!strings.Contains(err.Error(), "no policy") {
		t.Errorf("an empty policy directory must refuse, got %v", err)
	}
}

func TestPolicyDigest(t *testing.T) {
	dir := policyDir(t)
	a, err := policyDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Test suites are not policy: adding one does not change the digest.
	if err := os.WriteFile(filepath.Join(dir, "agent-submit_test.cue"), []byte("tests: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := policyDigest(dir); b != a {
		t.Error("a _test.cue file changed the digest")
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-submit.cue"), []byte("package mrman\n_maxComments: 500\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, _ := policyDigest(dir); c == a {
		t.Error("changing the policy must change the digest")
	}
}

func TestAppendAudit(t *testing.T) {
	session := filepath.Join(t.TempDir(), "s.json")
	for _, d := range []string{"allow", "deny"} {
		if err := appendAudit(session, &AuthorizationRecord{Decision: d}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(session + ".audit.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(data)), "\n"); len(lines) != 2 || !strings.Contains(lines[1], `"decision":"deny"`) {
		t.Errorf("audit log %q", data)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(session + ".audit.jsonl"); st.Mode().Perm() != 0o600 {
			t.Errorf("audit log mode %v, want 0600", st.Mode().Perm())
		}
	}
}

// With a real garmr (on PATH or MRMAN_TEST_GARMR), the committed pack
// decides as documented.
func TestAuthorize_CommittedPolicyWithRealGarmr(t *testing.T) {
	bin := os.Getenv("MRMAN_TEST_GARMR")
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("garmr"); err != nil {
			t.Skip("garmr not installed; set MRMAN_TEST_GARMR to run")
		}
	}
	dir := filepath.Join("..", "..", "policies", "garmr", "mrman")
	for _, tc := range []struct {
		in      PolicyInput
		allowed bool
		failed  []string
	}{
		{PolicyInput{Event: "comment", Identity: "vizzini-bot", Comments: 5, RunID: "r"}, true, nil},
		{PolicyInput{Event: "approve", Identity: "vizzini-bot", Comments: 5, RunID: "r"}, false, []string{"MRM-001"}},
		{PolicyInput{Event: "resolve", Identity: "vizzini-bot", Comments: 2, RunID: "r"}, true, nil},
		{PolicyInput{Event: "comment", Identity: "someone-else", Comments: 5, RunID: "r"}, false, []string{"MRM-002"}},
		{PolicyInput{Event: "comment", Identity: "vizzini-bot", Comments: 5}, false, []string{"MRM-004"}},
	} {
		in := tc.in
		rec, err := authorize(context.Background(), &Options{GrantPolicy: dir, Garmr: bin}, &in)
		if err != nil {
			t.Fatalf("%+v: %v", tc.in, err)
		}
		if rec.Allowed != tc.allowed || !slices.Equal(rec.Failed, tc.failed) {
			t.Errorf("%+v: allowed %v failed %v, want %v %v", tc.in, rec.Allowed, rec.Failed, tc.allowed, tc.failed)
		}
	}
}
