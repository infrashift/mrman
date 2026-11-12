package agentsubmit

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/reviewcli"
	"github.com/infrashift/mrman/internal/risk"
)

// findings.go is the findings view for developer agents (Vizzini
// ISSUE-027): `review threads --findings` decodes the hidden marker of each
// thread vizzini-bot opened into structured fields, so an agent reads the
// findings without parsing comment text. The format is Vizzini's own
// (pkg/marker, through internal/risk), so mrman decodes exactly what the bot writes.

// Finding is one vizzini-bot finding thread on the pull request.
type Finding struct {
	// Thread is the forge thread id: what `review reply --thread` takes.
	Thread string `json:"thread"`
	Path   string `json:"path"`
	// Line and StartLine are where the thread is anchored now, as the
	// forge reports it (nil when fully outdated).
	Line      *uint32 `json:"line"`
	StartLine *uint32 `json:"start_line"`
	// Status is "open" or "resolved"; Disposition is the forge's own state
	// where it has one (Azure DevOps: "won't fix", "by design", ...).
	Status      string `json:"status"`
	Disposition string `json:"disposition,omitempty"`
	Outdated    bool   `json:"outdated"`

	Severity   string   `json:"severity"`
	Category   string   `json:"category"`
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Suggestion string   `json:"suggestion,omitempty"`
	Citations  []string `json:"citations,omitempty"`
	// Fingerprint identifies the finding across runs and rewordings.
	Fingerprint string `json:"fingerprint"`
	// Run and Head are the bot run that posted it and the commit it
	// reviewed; PostedStartLine and PostedEndLine the lines at that head.
	Run             string `json:"run"`
	Head            string `json:"head,omitempty"`
	PostedStartLine int    `json:"posted_start_line,omitempty"`
	PostedEndLine   int    `json:"posted_end_line,omitempty"`
	Agent           string `json:"agent,omitempty"`
	// Replies are the thread's later comments, oldest first.
	Replies []FindingReply `json:"replies"`
}

// FindingReply is a comment after the finding in its thread.
type FindingReply struct {
	ID     string `json:"id"`
	Author string `json:"author"`
	Body   string `json:"body"`
	// FromVizzini marks the bot's own comments (answers, resolutions,
	// suggested rules); ReplyTo is the comment a bot answer answers.
	FromVizzini bool   `json:"from_vizzini"`
	ReplyTo     string `json:"reply_to,omitempty"`
}

// MalformedThread is a thread whose root looks like a vizzini-bot finding but
// whose marker could not be read; it is reported, never fatal.
type MalformedThread struct {
	Thread string `json:"thread"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// FindingsOutput is what `review threads --findings` prints.
type FindingsOutput struct {
	PR        string            `json:"pr"`
	HeadSHA   string            `json:"head_sha"`
	Findings  []Finding         `json:"findings"`
	Malformed []MalformedThread `json:"malformed"`
}

// DecodeFindings picks vizzini-bot's finding threads out of threads; other
// threads (people's discussions) are left out.
func DecodeFindings(threads []forge.RemoteReviewThread) (findings []Finding, malformed []MalformedThread) {
	findings, malformed = []Finding{}, []MalformedThread{}
	for i := range threads {
		t := &threads[i]
		if len(t.Comments) == 0 {
			continue
		}
		root := t.Comments[0].Body
		c, ok := risk.ParseFindingComment(root)
		if !ok {
			if strings.Contains(root, "<!-- vizzini:fp=") {
				malformed = append(malformed, MalformedThread{Thread: t.ID, Path: t.Path, Reason: "unreadable vizzini finding marker"})
			}
			continue
		}
		f := Finding{
			Thread: t.ID, Path: strings.TrimPrefix(t.Path, "/"), Line: t.Line, StartLine: t.StartLine,
			Status: "open", Disposition: t.Disposition, Outdated: t.IsOutdated,
			Severity: c.Severity, Category: c.Category, Title: c.Title, Body: c.Body, Suggestion: c.Suggestion,
			Citations: c.Citations, Fingerprint: c.Marker.FP, Run: c.Marker.Run, Head: c.Marker.Head,
			PostedStartLine: c.Marker.Start, PostedEndLine: c.Marker.End, Agent: c.Agent, Replies: []FindingReply{},
		}
		if t.IsResolved {
			f.Status = "resolved"
		}
		for _, r := range t.Comments[1:] {
			f.Replies = append(f.Replies, FindingReply{ID: r.ID, Author: r.Author, Body: r.Body,
				FromVizzini: risk.FromVizzini(r.Body), ReplyTo: risk.ReplyTo(r.Body)})
		}
		findings = append(findings, f)
	}
	return findings, malformed
}

// Findings prints the findings view of the session's PR. It only reads.
func Findings(store *persistence.Store, opts reviewcli.Options, out io.Writer) error {
	path, err := reviewcli.ResolveSessionPath(store, opts.Repo, opts.Session)
	if err != nil {
		return err
	}
	backend, details, pr, err := prForSession(store, path, opts.Session)
	if err != nil {
		return err
	}
	threads, err := backend.ListReviewThreads(context.Background(), details)
	if err != nil {
		return err
	}
	findings, malformed := DecodeFindings(threads)
	return json.NewEncoder(out).Encode(FindingsOutput{PR: pr, HeadSHA: details.HeadSHA, Findings: findings, Malformed: malformed})
}
