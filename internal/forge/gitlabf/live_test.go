package gitlabf_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/infrashift/mrman/internal/agentsubmit"
	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/forge"
	_ "github.com/infrashift/mrman/internal/forge/drivers" // register drivers
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/forge/gitlabf"
	"github.com/infrashift/mrman/internal/forge/submit"
	"github.com/infrashift/mrman/internal/livetest"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/prload"
	"github.com/infrashift/mrman/internal/reviewcli"
)

// envReadonlyToken optionally names a read_api token for the same user, so
// the permission-denied path is exercised against the real server.
const envReadonlyToken = "MRMAN_LIVE_READONLY_TOKEN"

// TestLiveGitLabReview drives the GitLab driver through a whole review of a
// real merge request — read, comment, draft, request changes, a head that
// moves under an open session, approve — and reads every outcome back from
// GitLab itself rather than trusting the driver's own report of it. The
// driver synthesizes its review state locally (submitState), so without the
// read-back a mutation GitLab silently ignored would still look like a pass.
//
// The token's user must be a reviewer of the merge request, and the merge
// request must change at least one file of each kind: modified (with an
// addition run of two lines, a deletion and a context line), added,
// deleted and renamed. It writes, and it leaves its comments in place as
// evidence; drafts and the approval are removed so it can run again.
//
//	MRMAN_LIVE_PR=https://HOST/group/repo/-/merge_requests/N MRMAN_LIVE_SUBMIT=1 \
//	    go test ./internal/forge/gitlabf/ -run TestLiveGitLabReview -v
func TestLiveGitLabReview(t *testing.T) {
	cfg := livetest.Config(t)
	repo, number := livetest.Target(t, cfg)
	livetest.RequireKind(t, repo, forgetypes.KindGitLab)

	lv := newLiveMR(t, cfg, repo, number)
	if !t.Run("L1 read", lv.read) {
		return
	}
	livetest.RequireSubmit(t)
	lv.clearApproval(t)
	for _, step := range []struct {
		name string
		fn   func(*testing.T)
	}{
		{"L2 comment", lv.comment},
		{"L3 draft", lv.draft},
		{"L4 request changes", lv.requestChanges},
		{"L5 head moved", lv.headMoved},
		{"L6 approve", lv.approve},
		{"L7 read-only token", lv.readonlyToken},
	} {
		if !t.Run(step.name, step.fn) {
			return
		}
	}
}

// liveMR is the merge request under test plus a second, independent channel
// to GitLab — the SDK and GraphQL with a bearer token — for reading back
// what the driver did.
type liveMR struct {
	cfg     config.ForgeConfig
	repo    forgetypes.Repository
	number  uint64
	pid     string
	iid     int64
	backend forge.Forge
	hc      forge.HostConfig
	http    *http.Client
	api     *gitlab.Client
	apiBase string
	viewer  string
	stamp   string
	load    app.PullRequestLoad
}

func newLiveMR(t *testing.T, cfg config.ForgeConfig, repo forgetypes.Repository, number uint64) *liveMR {
	t.Helper()
	backend, err := forge.ForRepository(repo, cfg)
	if err != nil {
		t.Fatalf("resolve driver: %v", err)
	}
	hc, err := forge.ResolveHostConfig(repo.Host, repo.Kind, cfg)
	if err != nil {
		t.Fatalf("resolve host config: %v", err)
	}
	if hc.Token == "" {
		t.Fatalf("no token for %s: add a [[forge.hosts]] entry with token or token_cmd", repo.Host)
	}
	client, err := forge.BuildHTTPClient(hc)
	if err != nil {
		t.Fatalf("http client: %v", err)
	}
	apiBase := hc.APIBase
	if apiBase == "" {
		apiBase = "https://" + repo.Host + "/api/v4"
	}
	api, err := gitlab.NewClient(hc.Token, gitlab.WithBaseURL(apiBase), gitlab.WithHTTPClient(client))
	if err != nil {
		t.Fatalf("gitlab client: %v", err)
	}
	me, _, err := api.Users.CurrentUser()
	if err != nil {
		t.Fatalf("GET /user: %v", err)
	}
	lv := &liveMR{
		cfg: cfg, repo: repo, number: number,
		pid:     repo.Owner + "/" + repo.Name,
		iid:     int64(number),
		backend: backend, hc: hc, http: client, api: api, apiBase: apiBase,
		viewer: me.Username,
		stamp:  time.Now().UTC().Format("20060102T150405Z"),
	}
	lv.refetch(t)
	t.Logf("reviewing %s!%d as %s at %.12s (run %s)", lv.pid, number, lv.viewer, lv.load.Details.HeadSHA, lv.stamp)
	return lv
}

func (lv *liveMR) refetch(t *testing.T) {
	t.Helper()
	load, err := prload.Fetch(context.Background(), lv.backend, &lv.repo,
		forge.Target{Repository: &lv.repo, Number: lv.number}, nil, "")
	if err != nil {
		t.Fatalf("fetch merge request: %v", err)
	}
	lv.load = load
}

// --- L1: every read the TUI makes, checked against the raw API ---

func (lv *liveMR) read(t *testing.T) {
	ctx := context.Background()
	details := lv.load.Details

	mr, _, err := lv.api.MergeRequests.GetMergeRequest(lv.pid, lv.iid, nil)
	if err != nil {
		t.Fatalf("GET merge request: %v", err)
	}
	if details.HeadSHA != mr.DiffRefs.HeadSha || details.BaseSHA != mr.DiffRefs.BaseSha {
		t.Errorf("diff refs: driver head/base %.12s/%.12s, GitLab %.12s/%.12s",
			details.HeadSHA, details.BaseSHA, mr.DiffRefs.HeadSha, mr.DiffRefs.BaseSha)
	}

	diffs, _, err := lv.api.MergeRequests.ListMergeRequestDiffs(lv.pid, lv.iid,
		&gitlab.ListMergeRequestDiffsOptions{ListOptions: gitlab.ListOptions{PerPage: 100}})
	if err != nil {
		t.Fatalf("GET diffs: %v", err)
	}
	want := map[string]model.FileStatus{}
	for _, d := range diffs {
		switch {
		case d.NewFile:
			want[d.NewPath] = model.StatusAdded
		case d.DeletedFile:
			want[d.OldPath] = model.StatusDeleted
		case d.RenamedFile:
			want[d.NewPath] = model.StatusRenamed
		default:
			want[d.NewPath] = model.StatusModified
		}
	}
	got := map[string]model.FileStatus{}
	for i := range lv.load.Files {
		f := &lv.load.Files[i]
		got[f.DisplayPath()] = f.Status
	}
	for path, status := range want {
		if got[path] != status {
			t.Errorf("file %s: driver says %q, GitLab says %q", path, got[path], status)
		}
	}
	if len(got) != len(want) {
		t.Errorf("driver parsed %d files, GitLab lists %d", len(got), len(want))
	}
	for _, status := range []model.FileStatus{model.StatusModified, model.StatusAdded, model.StatusDeleted, model.StatusRenamed} {
		if !slices.Contains(slices.Collect(maps.Values(want)), status) {
			t.Errorf("fixture has no %s file; the merge request must change one of each kind", status)
		}
	}
	t.Logf("files: %v", got)

	commits, _, err := lv.api.MergeRequests.GetMergeRequestCommits(lv.pid, lv.iid, nil)
	if err != nil {
		t.Fatalf("GET commits: %v", err)
	}
	if len(lv.load.Commits) != len(commits) {
		t.Errorf("commits: driver %d, GitLab %d", len(lv.load.Commits), len(commits))
	}
	if n := len(lv.load.Commits); n > 0 && lv.load.Commits[n-1].OID != details.HeadSHA {
		t.Errorf("newest commit %.12s is not the head %.12s", lv.load.Commits[n-1].OID, details.HeadSHA)
	}

	file, _ := lv.modifiedFile(t)
	raw, _, err := lv.api.RepositoryFiles.GetRawFile(lv.pid, file.DisplayPath(),
		&gitlab.GetRawFileOptions{Ref: new(details.HeadSHA)})
	if err != nil {
		t.Fatalf("GET raw file: %v", err)
	}
	lines, err := lv.backend.FetchFileLines(ctx, forge.FileLinesRequest{
		Repository: lv.repo, BaseSHA: details.BaseSHA, HeadSHA: details.HeadSHA,
		Path: file.DisplayPath(), Status: file.Status, Side: forge.FileSideHead,
		StartLine: 1, EndLine: 2,
	})
	if err != nil {
		t.Fatalf("FetchFileLines: %v", err)
	}
	rawLines := strings.SplitN(string(raw), "\n", 3)
	if len(lines) < 1 || len(rawLines) < 1 || lines[0].Content != rawLines[0] {
		t.Errorf("FetchFileLines line 1 = %v, raw file starts %q", lines, rawLines[0])
	}

	meta, err := lv.backend.ReviewMetadata(ctx, details)
	if err != nil {
		t.Fatalf("ReviewMetadata: %v", err)
	}
	if meta.ViewerLogin != lv.viewer {
		t.Errorf("ReviewMetadata viewer %q, token user %q", meta.ViewerLogin, lv.viewer)
	}

	page, err := lv.backend.ListPullRequests(ctx, forge.ListQuery{
		Repository: lv.repo, Scope: forge.ScopeReviewRequested, PageSize: 50,
	})
	if err != nil {
		t.Fatalf("ListPullRequests(review requested): %v", err)
	}
	if !slices.ContainsFunc(page.Items, func(s forge.PullRequestSummary) bool { return s.Number == lv.number }) {
		t.Errorf("!%d is not listed under review-requested for %s; is %s a reviewer?", lv.number, lv.viewer, lv.viewer)
	}
}

// --- L2: comment submit — every anchor kind lands as a positioned DiffNote ---

// anchorCase is one inline comment L2 posts and the position GitLab must
// report back for it.
type anchorCase struct {
	kind    string
	file    *model.DiffFile
	comment *model.Comment
	anchor  submit.CommentAnchor
	side    submit.Side
	line    uint32
	start   *uint32 // range start, nil for single-line
	oldToo  *uint32 // context lines carry the old line as well
	inline  submit.InlineComment
}

func (lv *liveMR) comment(t *testing.T) {
	cases := lv.anchorCases(t)
	marker := "mrman live L2 " + lv.stamp
	req := forge.CreateReviewRequest{Event: forge.SubmitComment, Body: marker + " body"}
	for i := range cases {
		c := &cases[i]
		c.comment.Content = marker + " " + c.kind
		mapped := submit.MapComment(c.comment, c.anchor, c.file, false)
		if mapped.Inline == nil {
			t.Fatalf("%s: mrman's own mapper refused the anchor: %s", c.kind, mapped.Unmappable.Reason.HumanLabel())
		}
		c.inline = *mapped.Inline
		req.Comments = append(req.Comments, c.inline)
	}

	result, err := lv.backend.CreateReview(context.Background(), lv.load.Details, req)
	if err != nil {
		t.Fatalf("CreateReview(comment): %v", err)
	}
	if result.Partial != nil {
		t.Fatalf("partial submit: failed at %d of %d: %v",
			result.Partial.FailedAt, len(req.Comments), result.Partial.Cause)
	}

	discussions := lv.discussionsWith(t, marker)
	for i := range cases {
		c := &cases[i]
		note := findNote(discussions, marker+" "+c.kind)
		if note == nil {
			t.Errorf("%s: no discussion carries %q", c.kind, marker+" "+c.kind)
			continue
		}
		if note.Type != gitlab.DiffNote || note.Position == nil {
			t.Errorf("%s: landed as a %q note with position %v — not anchored to the diff", c.kind, note.Type, note.Position)
			continue
		}
		p := note.Position
		t.Logf("%s: %s old=%d new=%d range=%s", c.kind, p.NewPath, p.OldLine, p.NewLine, describeRange(p.LineRange))
		if c.side == submit.SideNew && p.NewLine != int64(c.line) {
			t.Errorf("%s: new_line %d, want %d", c.kind, p.NewLine, c.line)
		}
		if c.side == submit.SideOld && p.OldLine != int64(c.line) {
			t.Errorf("%s: old_line %d, want %d", c.kind, p.OldLine, c.line)
		}
		if c.oldToo != nil && p.OldLine != int64(*c.oldToo) {
			t.Errorf("%s: context line old_line %d, want %d", c.kind, p.OldLine, *c.oldToo)
		}
		if c.start != nil {
			if p.LineRange == nil || p.LineRange.StartRange == nil || p.LineRange.EndRange == nil {
				t.Errorf("%s: range comment came back with no line_range", c.kind)
				continue
			}
			if got := rangeLine(p.LineRange.StartRange, c.side); got != int64(*c.start) {
				t.Errorf("%s: line_range start %d, want %d", c.kind, got, *c.start)
			}
			if got := rangeLine(p.LineRange.EndRange, c.side); got != int64(c.line) {
				t.Errorf("%s: line_range end %d, want %d", c.kind, got, c.line)
			}
		}
	}
	if note := findNote(discussions, marker+" body"); note == nil || note.Position != nil {
		t.Errorf("review body must land as one general (unpositioned) note, got %v", note)
	}

	// The driver's own read path must see what it wrote, on the same lines.
	threads, err := lv.backend.ListReviewThreads(context.Background(), lv.load.Details)
	if err != nil {
		t.Fatalf("ListReviewThreads: %v", err)
	}
	for i := range cases {
		c := &cases[i]
		th := findThread(threads, marker+" "+c.kind)
		if th == nil {
			t.Errorf("%s: ListReviewThreads does not return the thread just posted", c.kind)
			continue
		}
		if th.Line == nil || *th.Line != c.line || th.Side != c.side {
			t.Errorf("%s: ListReviewThreads reads it back at %v/%s, want %d/%s", c.kind, th.Line, th.Side, c.line, c.side)
		}
		if c.start != nil && (th.StartLine == nil || *th.StartLine != *c.start) {
			t.Errorf("%s: ListReviewThreads reads the range start back as %v, want %d", c.kind, th.StartLine, *c.start)
		}
	}
}

// anchorCases picks one comment of every anchor kind out of the parsed diff:
// an addition, a deletion, a context line, a two-line addition run, and a
// whole-hunk span (the TUI's hunk comment), plus one on a renamed file.
func (lv *liveMR) anchorCases(t *testing.T) []anchorCase {
	t.Helper()
	file, hunk := lv.modifiedFile(t)
	var cases []anchorCase
	lineCase := func(kind string, f *model.DiffFile, dl *model.DiffLine) {
		side := model.LineSideNew
		line := dl.NewLineno
		if dl.Origin == model.OriginDeletion {
			side, line = model.LineSideOld, dl.OldLineno
		}
		c := anchorCase{
			kind: kind, file: f, anchor: submit.LineAnchor(*line, side),
			comment: model.NewComment("", model.CommentTypeFromID("note"), new(side)),
			side:    submit.SideFromLineSide(side), line: *line,
		}
		c.comment.LineContext = &model.LineContext{NewLine: dl.NewLineno, OldLine: dl.OldLineno, Content: dl.Content}
		if dl.Origin == model.OriginContext {
			c.oldToo = dl.OldLineno
		}
		cases = append(cases, c)
	}
	for _, origin := range []struct {
		kind string
		o    model.LineOrigin
	}{{"addition", model.OriginAddition}, {"deletion", model.OriginDeletion}, {"context", model.OriginContext}} {
		dl := firstLine(file, origin.o)
		if dl == nil {
			t.Fatalf("modified file %s has no %s line; the fixture must carry one", file.DisplayPath(), origin.kind)
		}
		lineCase(origin.kind, file, dl)
	}

	start, end, ok := additionRun(file)
	if !ok {
		t.Fatalf("modified file %s has no run of two added lines; the fixture must carry one", file.DisplayPath())
	}
	cases = append(cases, rangeCase("addition range", file, model.NewLineRange(start, end), model.LineSideNew))

	span, side, ok := hunk.CommentSpan()
	if !ok || span.IsSingle() {
		t.Fatalf("first hunk of %s has no multi-line span", file.DisplayPath())
	}
	cases = append(cases, rangeCase("hunk span", file, span, side))

	if renamed := lv.fileWithStatus(model.StatusRenamed); renamed != nil {
		if dl := firstLine(renamed, model.OriginAddition); dl != nil {
			lineCase("renamed-file addition", renamed, dl)
		} else if dl := firstLine(renamed, model.OriginContext); dl != nil {
			lineCase("renamed-file context", renamed, dl)
		}
	}
	return cases
}

func rangeCase(kind string, file *model.DiffFile, r model.LineRange, side model.LineSide) anchorCase {
	start := r.Start
	return anchorCase{
		kind: kind, file: file, anchor: submit.RangeAnchor(),
		comment: model.NewCommentWithRange("", model.CommentTypeFromID("note"), new(side), r),
		side:    submit.SideFromLineSide(side), line: r.End, start: &start,
	}
}

// --- L3: draft submit — draft notes exist, nothing is published ---

func (lv *liveMR) draft(t *testing.T) {
	file, _ := lv.modifiedFile(t)
	dl := firstLine(file, model.OriginAddition)
	marker := "mrman live L3 " + lv.stamp
	c := model.NewComment(marker+" inline", model.CommentTypeFromID("note"), new(model.LineSideNew))
	mapped := submit.MapComment(c, submit.LineAnchor(*dl.NewLineno, model.LineSideNew), file, false)
	if mapped.Inline == nil {
		t.Fatal("draft anchor unmappable")
	}

	result, err := lv.backend.CreateReview(context.Background(), lv.load.Details, forge.CreateReviewRequest{
		Event: forge.SubmitDraft, Body: marker + " body", Comments: []submit.InlineComment{*mapped.Inline},
	})
	if err != nil {
		t.Fatalf("CreateReview(draft): %v", err)
	}
	if result.Partial != nil {
		t.Fatalf("partial draft submit: %v", result.Partial.Cause)
	}

	drafts, _, err := lv.api.DraftNotes.ListDraftNotes(lv.pid, lv.iid, nil)
	if err != nil {
		t.Fatalf("GET draft_notes: %v", err)
	}
	var mine []*gitlab.DraftNote
	for _, d := range drafts {
		if strings.Contains(d.Note, marker) {
			mine = append(mine, d)
		}
	}
	t.Cleanup(func() {
		for _, d := range mine {
			if _, err := lv.api.DraftNotes.DeleteDraftNote(lv.pid, lv.iid, d.ID); err != nil {
				t.Logf("cleanup: delete draft %d: %v", d.ID, err)
			}
		}
	})
	if len(mine) != 2 {
		t.Fatalf("want 2 draft notes carrying %q, GitLab holds %d", marker, len(mine))
	}
	for _, d := range mine {
		if strings.HasSuffix(d.Note, "inline") && (d.Position == nil || d.Position.NewLine != int64(*dl.NewLineno)) {
			t.Errorf("inline draft position %+v, want new_line %d", d.Position, *dl.NewLineno)
		}
	}
	if findNote(lv.discussionsWith(t, marker), marker) != nil {
		t.Error("a draft submit published a note")
	}
}

// --- L4: request changes (reject) ---

func (lv *liveMR) requestChanges(t *testing.T) {
	file, _ := lv.modifiedFile(t)
	dl := firstLine(file, model.OriginAddition)
	marker := "mrman live L4 " + lv.stamp
	c := model.NewComment(marker+" blocking", model.CommentTypeFromID("issue"), new(model.LineSideNew))
	mapped := submit.MapComment(c, submit.LineAnchor(*dl.NewLineno, model.LineSideNew), file, false)
	if mapped.Inline == nil {
		t.Fatal("request-changes anchor unmappable")
	}

	result, err := lv.backend.CreateReview(context.Background(), lv.load.Details, forge.CreateReviewRequest{
		Event: forge.SubmitRequestChanges, Comments: []submit.InlineComment{*mapped.Inline},
	})
	if err != nil {
		t.Fatalf("CreateReview(request-changes): %v", err)
	}
	if result.Partial != nil {
		t.Fatalf("request-changes did not complete: failed at step %d: %v", result.Partial.FailedAt, result.Partial.Cause)
	}
	if state := lv.reviewState(t).states[lv.viewer]; state != "REQUESTED_CHANGES" {
		t.Errorf("GitLab reports %s's review state %q after request-changes, want REQUESTED_CHANGES", lv.viewer, state)
	}
}

// --- L5: a head that moves under an open session refuses the submit ---

func (lv *liveMR) headMoved(t *testing.T) {
	store := &persistence.Store{ReviewsDir: filepath.Join(t.TempDir(), "reviews")}
	session := app.NewPrSession(lv.load.Details)
	app.RegisterDiffFiles(session, lv.load.Files)
	session.ReviewComments = append(session.ReviewComments,
		model.NewComment("mrman live L5 "+lv.stamp+" must never land", model.CommentTypeFromID("note"), nil))
	path, err := store.SaveSession(session)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"request-changes"}); err != nil {
		t.Fatal(err)
	}
	before := lv.threadCount(t)

	// [skip ci]: the commit exists only to move the head; a project whose
	// pipeline runs on every push would otherwise build it.
	branch := lv.load.Details.HeadRefName
	pushed, _, err := lv.api.Commits.CreateCommit(lv.pid, &gitlab.CreateCommitOptions{
		Branch:        new(branch),
		CommitMessage: new("[skip ci] mrman live L5 " + lv.stamp + ": move the head"),
		Actions: []*gitlab.CommitActionOptions{{
			Action:   new(gitlab.FileCreate),
			FilePath: new("mrman-live/" + lv.stamp + ".txt"),
			Content:  new("pushed under an open review session\n"),
		}},
	})
	if err != nil {
		t.Fatalf("push a commit to %s: %v", branch, err)
	}
	// GitLab moves the merge request's diff_refs asynchronously after a
	// push, and the guard can only see what GitLab reports: until then a
	// submit lands on the old head (observed live on CE 19.3, see
	// PLAN-GitLab-Live-Verification.md). Wait for GitLab to catch up, so
	// this asserts the guard rather than the refresh latency.
	lag := lv.awaitHead(t, pushed.ID)
	t.Logf("GitLab reported the pushed head after %s", lag.Round(100*time.Millisecond))

	err = agentsubmit.Submit(store, agentsubmit.Options{
		Options: reviewcli.Options{Session: path}, Event: "request-changes",
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "advanced") {
		t.Errorf("submit on a moved head must be refused, got %v", err)
	}
	if after := lv.threadCount(t); after != before {
		t.Errorf("a refused submit reached GitLab: %d threads before, %d after", before, after)
	}
	lv.refetch(t) // later steps review the new head
}

// awaitHead polls the merge request until GitLab reports sha as its head.
func (lv *liveMR) awaitHead(t *testing.T, sha string) time.Duration {
	t.Helper()
	start := time.Now()
	for time.Since(start) < 90*time.Second {
		mr, _, err := lv.api.MergeRequests.GetMergeRequest(lv.pid, lv.iid, nil)
		if err != nil {
			t.Fatalf("GET merge request: %v", err)
		}
		if mr.DiffRefs.HeadSha == sha {
			return time.Since(start)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("GitLab still does not report %.12s as the head after 90s", sha)
	return 0
}

// --- L6: approve ---

func (lv *liveMR) approve(t *testing.T) {
	result, err := lv.backend.CreateReview(context.Background(), lv.load.Details,
		forge.CreateReviewRequest{Event: forge.SubmitApprove})
	if err != nil {
		t.Fatalf("CreateReview(approve): %v", err)
	}
	if result.Partial != nil {
		t.Fatalf("approve did not complete: %v", result.Partial.Cause)
	}
	t.Cleanup(func() { lv.clearApproval(t) })

	approvals, _, err := lv.api.MergeRequestApprovals.GetConfiguration(lv.pid, lv.iid)
	if err != nil {
		t.Fatalf("GET approvals: %v", err)
	}
	if !slices.ContainsFunc(approvals.ApprovedBy, func(a *gitlab.MergeRequestApproverUser) bool {
		return a.User != nil && a.User.Username == lv.viewer
	}) {
		t.Errorf("GitLab's approved_by does not include %s", lv.viewer)
	}
	rs := lv.reviewState(t)
	if !slices.Contains(rs.approvedBy, lv.viewer) {
		t.Errorf("GraphQL approvedBy %v does not include %s", rs.approvedBy, lv.viewer)
	}
	if state := rs.states[lv.viewer]; state != "APPROVED" {
		t.Errorf("GitLab reports %s's review state %q after approve, want APPROVED", lv.viewer, state)
	}
}

// --- L7: a read_api token is refused with the actionable hint ---

func (lv *liveMR) readonlyToken(t *testing.T) {
	token := os.Getenv(envReadonlyToken)
	if token == "" {
		t.Skip("set " + envReadonlyToken + " to a read_api token for the same user")
	}
	d, err := gitlabf.New(gitlabf.Options{Host: lv.repo.Host, APIBase: lv.apiBase, HTTPClient: lv.http, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	before := lv.threadCount(t)
	_, err = d.CreateReview(context.Background(), lv.load.Details, forge.CreateReviewRequest{
		Event: forge.SubmitComment, Body: "mrman live L7 " + lv.stamp + " must never land",
	})
	var ferr *forge.Error
	if !errors.As(err, &ferr) {
		t.Fatalf("read_api submit must fail with a forge error, got %v", err)
	}
	t.Logf("refused: status=%d hint=%q", ferr.Status, ferr.Hint)
	if ferr.Status != http.StatusForbidden || !strings.Contains(ferr.Hint, "`api` scope") {
		t.Errorf("want 403 with the api-scope hint, got %d %q", ferr.Status, ferr.Hint)
	}
	if after := lv.threadCount(t); after != before {
		t.Errorf("a read_api token wrote to GitLab: %d -> %d threads", before, after)
	}
}

// --- read-back helpers ---

func (lv *liveMR) clearApproval(t *testing.T) {
	t.Helper()
	approvals, _, err := lv.api.MergeRequestApprovals.GetConfiguration(lv.pid, lv.iid)
	if err != nil {
		t.Logf("GET approvals: %v", err)
		return
	}
	if approvals.UserHasApproved {
		if _, err := lv.api.MergeRequestApprovals.UnapproveMergeRequest(lv.pid, lv.iid); err != nil {
			t.Logf("unapprove: %v", err)
		}
	}
}

func (lv *liveMR) threadCount(t *testing.T) int {
	t.Helper()
	threads, err := lv.backend.ListReviewThreads(context.Background(), lv.load.Details)
	if err != nil {
		t.Fatalf("ListReviewThreads: %v", err)
	}
	return len(threads)
}

func (lv *liveMR) discussionsWith(t *testing.T, marker string) []*gitlab.Discussion {
	t.Helper()
	var out []*gitlab.Discussion
	opts := &gitlab.ListMergeRequestDiscussionsOptions{ListOptions: gitlab.ListOptions{PerPage: 100}}
	for page := int64(1); page != 0; {
		opts.Page = page
		rows, resp, err := lv.api.Discussions.ListMergeRequestDiscussions(lv.pid, lv.iid, opts)
		if err != nil {
			t.Fatalf("GET discussions: %v", err)
		}
		for _, d := range rows {
			if len(d.Notes) > 0 && strings.Contains(d.Notes[0].Body, marker) {
				out = append(out, d)
			}
		}
		page = resp.NextPage
	}
	return out
}

// reviewSnapshot is GitLab's own account of who reviewed and approved.
type reviewSnapshot struct {
	states     map[string]string
	approvedBy []string
}

// reviewState reads reviewer states over GraphQL with a bearer token — a
// different auth header from the driver's PRIVATE-TOKEN, so a pass here does
// not depend on the thing under test.
func (lv *liveMR) reviewState(t *testing.T) reviewSnapshot {
	t.Helper()
	const query = `query($path: ID!, $iid: String!) { project(fullPath: $path) { mergeRequest(iid: $iid) {
		reviewers { nodes { username mergeRequestInteraction { reviewState } } }
		approvedBy { nodes { username } } } } }`
	body, _ := json.Marshal(map[string]any{
		"query":     query,
		"variables": map[string]string{"path": lv.pid, "iid": strconv.FormatUint(lv.number, 10)},
	})
	graphqlURL := strings.TrimSuffix(strings.TrimSuffix(lv.apiBase, "/"), "/api/v4") + "/api/graphql"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, graphqlURL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+lv.hc.Token)
	resp, err := lv.http.Do(req)
	if err != nil {
		t.Fatalf("GraphQL: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Errors []struct{ Message string } `json:"errors"`
		Data   struct {
			Project struct {
				MergeRequest struct {
					Reviewers struct {
						Nodes []struct {
							Username    string `json:"username"`
							Interaction struct {
								ReviewState string `json:"reviewState"`
							} `json:"mergeRequestInteraction"`
						} `json:"nodes"`
					} `json:"reviewers"`
					ApprovedBy struct {
						Nodes []struct {
							Username string `json:"username"`
						} `json:"nodes"`
					} `json:"approvedBy"`
				} `json:"mergeRequest"`
			} `json:"project"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || resp.StatusCode != http.StatusOK || len(out.Errors) > 0 {
		t.Fatalf("GraphQL review state: HTTP %d %v: %s", resp.StatusCode, err, raw)
	}
	snap := reviewSnapshot{states: map[string]string{}}
	for _, n := range out.Data.Project.MergeRequest.Reviewers.Nodes {
		snap.states[n.Username] = n.Interaction.ReviewState
	}
	for _, n := range out.Data.Project.MergeRequest.ApprovedBy.Nodes {
		snap.approvedBy = append(snap.approvedBy, n.Username)
	}
	t.Logf("GitLab review state: %v, approved by %v", snap.states, snap.approvedBy)
	return snap
}

// --- diff pickers ---

// modifiedFile is the first modified file and its first hunk: the fixture's
// anchor for single-line and range comments.
func (lv *liveMR) modifiedFile(t *testing.T) (*model.DiffFile, *model.DiffHunk) {
	t.Helper()
	f := lv.fileWithStatus(model.StatusModified)
	if f == nil || len(f.Hunks) == 0 {
		t.Fatal("the merge request modifies no text file; the fixture must")
	}
	return f, &f.Hunks[0]
}

func (lv *liveMR) fileWithStatus(status model.FileStatus) *model.DiffFile {
	for i := range lv.load.Files {
		f := &lv.load.Files[i]
		if f.Status == status && !f.IsBinary && !f.IsTooLarge {
			return f
		}
	}
	return nil
}

func firstLine(f *model.DiffFile, origin model.LineOrigin) *model.DiffLine {
	for hi := range f.Hunks {
		for li := range f.Hunks[hi].Lines {
			if dl := &f.Hunks[hi].Lines[li]; dl.Origin == origin {
				return dl
			}
		}
	}
	return nil
}

func additionRun(f *model.DiffFile) (start, end uint32, ok bool) {
	for hi := range f.Hunks {
		lines := f.Hunks[hi].Lines
		for li := 0; li+1 < len(lines); li++ {
			a, b := &lines[li], &lines[li+1]
			if a.Origin == model.OriginAddition && b.Origin == model.OriginAddition {
				return *a.NewLineno, *b.NewLineno, true
			}
		}
	}
	return 0, 0, false
}

func findNote(discussions []*gitlab.Discussion, body string) *gitlab.Note {
	for _, d := range discussions {
		for _, n := range d.Notes {
			if strings.HasSuffix(strings.TrimSpace(n.Body), body) {
				return n
			}
		}
	}
	return nil
}

func findThread(threads []forge.RemoteReviewThread, body string) *forge.RemoteReviewThread {
	for i := range threads {
		for _, c := range threads[i].Comments {
			if strings.HasSuffix(strings.TrimSpace(c.Body), body) {
				return &threads[i]
			}
		}
	}
	return nil
}

func rangeLine(p *gitlab.LinePosition, side submit.Side) int64 {
	if side == submit.SideOld {
		return p.OldLine
	}
	return p.NewLine
}

func describeRange(r *gitlab.LineRange) string {
	if r == nil || r.StartRange == nil || r.EndRange == nil {
		return "-"
	}
	return fmt.Sprintf("%s:%d/%d..%s:%d/%d", r.StartRange.Type, r.StartRange.OldLine, r.StartRange.NewLine,
		r.EndRange.Type, r.EndRange.OldLine, r.EndRange.NewLine)
}
