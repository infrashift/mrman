// policy.go is the unattended counterpart of the human grant: a submit with
// --grant-policy DIR proceeds only when garmr, evaluating the committed
// policy in DIR, allows it (Vizzini ADR-0016).
//
// The human grant exists so an agent cannot authorize itself through mrman.
// A policy keeps that property because the agent does not write the policy:
// it is committed to the repository and changed only through review, so a
// person decided, in advance and on the record, exactly what a named bot
// identity may post. The identity checked is the forge's own answer to "who
// is this token", not anything the caller claims. Every decision is printed
// and appended to an audit log beside the session.

package agentsubmit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// PolicyInput is what the policy decides on: kind AgentSubmit in
// mrman.infrashift.io/v1 (see policies/garmr/mrman).
type PolicyInput struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	// Event is the submit event: comment, approve, request-changes, draft.
	Event string `json:"event"`
	// Identity is the forge login the token authenticates as.
	Identity string `json:"identity"`
	Forge    string `json:"forge"`
	// PR is the session's pull-request slug.
	PR string `json:"pr"`
	// Comments counts the comments this submit would post.
	Comments int `json:"comments"`
	// RunID and Agent come from the caller (e.g. vizzini-bot) for the audit.
	RunID string `json:"run_id,omitempty"`
	Agent string `json:"agent,omitempty"`
}

// AuthorizationRecord is the audit entry for one policy decision.
type AuthorizationRecord struct {
	Time     time.Time   `json:"time"`
	Decision string      `json:"decision"`
	Allowed  bool        `json:"allowed"`
	Input    PolicyInput `json:"input"`
	// Failed lists the rule ids that did not pass.
	Failed       []string `json:"failed,omitempty"`
	Messages     []string `json:"messages,omitempty"`
	PolicyDir    string   `json:"policy_dir"`
	Namespace    string   `json:"namespace"`
	PolicyDigest string   `json:"policy_digest"`
}

// PolicyDenied is the JSON a caller gets when the policy refuses.
type PolicyDenied struct {
	Code          string              `json:"error"`
	Authorization AuthorizationRecord `json:"authorization"`
	Message       string              `json:"message"`
}

// Error makes a denial usable as an error while still carrying its shape.
func (d *PolicyDenied) Error() string { return d.Message }

// garmrResult is the part of `garmr eval -o json` mrman reads.
type garmrResult struct {
	Decision string `json:"decision"`
	Results  []struct {
		RuleID  string `json:"rule_id"`
		Passed  bool   `json:"passed"`
		Message string `json:"message"`
	} `json:"results"`
}

// runGarmr evaluates input against the policy directory in-process (no
// garmr-server). It returns garmr's stdout and exit code: 0 allow or warn,
// 1 deny, anything else an evaluation failure. Tests replace it.
var runGarmr = func(ctx context.Context, bin, dir, namespace string, input []byte) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, bin, "eval", "--policy-dir", dir, "-n", namespace, "-o", "json", "--input", "-")
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return stdout.Bytes(), exitErr.ExitCode(), nil
	}
	if err != nil {
		return nil, -1, fmt.Errorf("run %s: %w", bin, err)
	}
	if stderr.Len() > 0 && stdout.Len() == 0 {
		return nil, -1, fmt.Errorf("%s: %s", bin, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), 0, nil
}

// authorize asks the policy whether in may be submitted. An evaluation it
// cannot complete is an error, never an allow.
func authorize(ctx context.Context, opts *Options, in *PolicyInput) (*AuthorizationRecord, error) {
	dir, err := filepath.Abs(opts.GrantPolicy)
	if err != nil {
		return nil, err
	}
	digest, err := policyDigest(dir)
	if err != nil {
		return nil, fmt.Errorf("--grant-policy: %w", err)
	}
	in.APIVersion, in.Kind = "mrman.infrashift.io/v1", "AgentSubmit"
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	ns := opts.GrantNamespace
	if ns == "" {
		ns = "mrman"
	}
	bin := opts.Garmr
	if bin == "" {
		bin = "garmr"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, code, err := runGarmr(ctx, bin, dir, ns, body)
	if err != nil {
		return nil, fmt.Errorf("policy evaluation failed, not submitting: %w", err)
	}
	var res garmrResult
	if jerr := json.Unmarshal(out, &res); jerr != nil || res.Decision == "" {
		return nil, fmt.Errorf("policy evaluation failed, not submitting: garmr exit %d, unreadable decision", code)
	}
	rec := &AuthorizationRecord{
		Time: time.Now().UTC(), Decision: res.Decision, Input: *in,
		PolicyDir: dir, Namespace: ns, PolicyDigest: digest,
	}
	for _, r := range res.Results {
		if !r.Passed {
			rec.Failed = append(rec.Failed, r.RuleID)
			rec.Messages = append(rec.Messages, r.Message)
		}
	}
	// garmr's exit code and its decision must agree; anything else refuses.
	rec.Allowed = (code == 0 && (res.Decision == "allow" || res.Decision == "warn"))
	if !rec.Allowed && code == 0 {
		return nil, fmt.Errorf("policy evaluation failed, not submitting: garmr exit 0 with decision %q", res.Decision)
	}
	if !rec.Allowed && code != 1 {
		return nil, fmt.Errorf("policy evaluation failed, not submitting: garmr exit %d", code)
	}
	return rec, nil
}

// policyDigest hashes the policy directory's .cue files (paths and
// contents, sorted), so the audit names exactly which policy decided.
func policyDigest(dir string) (string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".cue") && !strings.HasSuffix(p, "_test.cue") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no policy (.cue) files in %s", dir)
	}
	slices.Sort(files)
	h := sha256.New()
	for _, f := range files {
		data, rerr := os.ReadFile(f)
		if rerr != nil {
			return "", rerr
		}
		rel, _ := filepath.Rel(dir, f)
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", rel, len(data)) // a hash write cannot fail
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// appendAudit adds rec to <session>.audit.jsonl, beside the session file.
func appendAudit(sessionPath string, rec *AuthorizationRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(sessionPath+".audit.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func policyDenial(rec *AuthorizationRecord) *PolicyDenied {
	return &PolicyDenied{
		Code:          "policy_denied",
		Authorization: *rec,
		Message: fmt.Sprintf("the grant policy %s (%s) denied %s by %q: %s", rec.PolicyDir, rec.Namespace,
			rec.Input.Event, rec.Input.Identity, strings.Join(rec.Messages, "; ")),
	}
}
