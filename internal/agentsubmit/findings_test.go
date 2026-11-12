package agentsubmit

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/reviewcli"
	"github.com/infrashift/mrman/internal/risk"
)

// A finding as vizzini-bot renders it (Vizzini's own pkg/marker).
func botFinding() string {
	c := risk.FindingComment{Severity: "Major", Category: "Security", Title: "SQL built by concatenation",
		Body: "The name is concatenated into the query.", Suggestion: `db.Query("… WHERE name = ?", name)`,
		Citations: []string{"01a1cae2"}, Agent: "vizzini-bot",
		Marker: risk.FindingMarker{FP: "v1:abc", Run: "r1", Head: strings.Repeat("ab", 20), Category: "Security", Start: 12, End: 14}}
	return c.Render()
}

func TestDecodeFindings(t *testing.T) {
	line, start := uint32(13), uint32(12)
	threads := []forge.RemoteReviewThread{
		{ID: "7", Path: "/src/x.go", Line: &line, StartLine: &start, IsResolved: true, Disposition: "won't fix", Comments: []forge.RemoteReviewComment{
			{ID: "1", Author: "vizzini-bot", Body: botFinding()},
			{ID: "2", Author: "dev-agent", Body: "/vizzini reconsider it is bound upstream"},
			{ID: "3", Author: "vizzini-bot", Body: "**The finding stands.**\n\n<!-- vizzini:reply-to=2 run=r2 head=abcdef0 -->"},
		}},
		{ID: "8", Path: "src/y.go", Comments: []forge.RemoteReviewComment{{ID: "4", Author: "ana", Body: "Why this name?"}}},
		{ID: "9", Path: "src/z.go", Comments: []forge.RemoteReviewComment{{ID: "5", Author: "vizzini-bot", Body: "x <!-- vizzini:fp= broken -->"}}},
		{ID: "10", Path: "src/w.go"},
	}
	findings, malformed := DecodeFindings(threads)
	if len(findings) != 1 || len(malformed) != 1 || malformed[0].Thread != "9" {
		t.Fatalf("findings %+v, malformed %+v", findings, malformed)
	}
	f := findings[0]
	want := Finding{Thread: "7", Path: "src/x.go", Line: &line, StartLine: &start, Status: "resolved", Disposition: "won't fix",
		Severity: "Major", Category: "Security", Title: "SQL built by concatenation", Body: "The name is concatenated into the query.",
		Suggestion: `db.Query("… WHERE name = ?", name)`, Citations: []string{"01a1cae2"}, Fingerprint: "v1:abc", Run: "r1",
		Head: strings.Repeat("ab", 20), PostedStartLine: 12, PostedEndLine: 14, Agent: "vizzini-bot"}
	got := f
	got.Replies = nil
	gj, _ := json.Marshal(got)
	wj, _ := json.Marshal(want)
	if string(gj) != string(wj) {
		t.Errorf("finding\n got %s\nwant %s", gj, wj)
	}
	if len(f.Replies) != 2 || f.Replies[0].FromVizzini || f.Replies[0].Author != "dev-agent" ||
		!f.Replies[1].FromVizzini || f.Replies[1].ReplyTo != "2" {
		t.Errorf("replies %+v", f.Replies)
	}
	if f, _ := DecodeFindings(nil); f == nil {
		t.Error("no threads: want an empty list, not nil")
	}
}

func TestFindingsPrintsTheView(t *testing.T) {
	fake := &resolverForge{submitForge: &submitForge{head: "head1234"}, threads: []forge.RemoteReviewThread{
		{ID: "7", Path: "src/x.go", Comments: []forge.RemoteReviewComment{{ID: "1", Author: "vizzini-bot", Body: botFinding()}}},
		{ID: "8", Path: "src/y.go", Comments: []forge.RemoteReviewComment{{ID: "4", Author: "ana", Body: "a person's thread"}}},
	}}
	store, path := sessionWithGrant(t, fake, nil)
	var out bytes.Buffer
	if err := Findings(store, reviewcli.Options{Session: path}, &out); err != nil {
		t.Fatal(err)
	}
	var got FindingsOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.HeadSHA != "head1234" || got.PR == "" ||
		len(got.Findings) != 1 || got.Findings[0].Thread != "7" || got.Findings[0].Status != "open" || got.Malformed == nil {
		t.Errorf("view %s (%v)", out.String(), err)
	}
}
