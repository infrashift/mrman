package gitlabf

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/submit"
)

// sha1LibRS is SHA1("src/lib.rs"), the hash inside golden line codes.
const sha1LibRS = "b24749917179fb5e3e613ed2a703fcdcc6cdf9da"

// captureJSON decodes the request body into a generic map and appends it
// to sink, then answers with the canned body.
func captureJSON(t *testing.T, sink *[]map[string]any, reply string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var body map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("unmarshal body %q: %v", raw, err)
			}
		}
		*sink = append(*sink, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}
}

func position(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	pos, ok := body["position"].(map[string]any)
	if !ok {
		t.Fatalf("body has no position object: %v", body)
	}
	return pos
}

func TestCreateReviewBodyAndInlineComment(t *testing.T) {
	mux := newFixtureMux(t)
	var notes, discussions []map[string]any
	mux.Handle("POST "+projectPrefix+"/notes",
		captureJSON(t, &notes, `{"id": 900, "body": "review body"}`))
	mux.Handle("POST "+projectPrefix+"/discussions",
		captureJSON(t, &discussions, `{"id": "disc-abc", "individual_note": false}`))
	d := newTestDriver(t, mux)

	result, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event:    forge.SubmitComment,
		CommitID: "headsha1",
		Body:     "overall review body",
		Comments: []submit.InlineComment{{
			Path:      "src/lib.rs",
			Line:      15,
			Side:      submit.SideNew,
			Body:      "[ISSUE] nice work",
			CommentID: "c1",
		}},
	})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if result.Partial != nil {
		t.Fatalf("unexpected partial: %+v", result.Partial)
	}
	if result.ReviewID != "disc-abc" || result.State != "COMMENTED" {
		t.Errorf("result = %+v", result)
	}
	if result.URL != testPR().URL {
		t.Errorf("URL = %q", result.URL)
	}

	if len(notes) != 1 || notes[0]["body"] != "overall review body" {
		t.Errorf("notes = %+v", notes)
	}
	if len(discussions) != 1 {
		t.Fatalf("discussions = %+v", discussions)
	}
	if discussions[0]["body"] != "[ISSUE] nice work" {
		t.Errorf("discussion body = %v", discussions[0]["body"])
	}
	pos := position(t, discussions[0])
	want := map[string]any{
		"position_type": "text",
		"base_sha":      "basesha1",
		"start_sha":     "startsha1",
		"head_sha":      "headsha1",
		"old_path":      "src/lib.rs",
		"new_path":      "src/lib.rs",
		"new_line":      float64(15),
	}
	for key, wantValue := range want {
		if pos[key] != wantValue {
			t.Errorf("position[%s] = %v, want %v", key, pos[key], wantValue)
		}
	}
	if _, present := pos["old_line"]; present {
		t.Errorf("old_line must be absent for a pure new-side comment: %v", pos)
	}
	if _, present := pos["line_range"]; present {
		t.Errorf("line_range must be absent for single-line comments: %v", pos)
	}
}

func TestCreateReviewContextLineSendsCounterpart(t *testing.T) {
	mux := newFixtureMux(t)
	var discussions []map[string]any
	mux.Handle("POST "+projectPrefix+"/discussions",
		captureJSON(t, &discussions, `{"id": "disc-1"}`))
	d := newTestDriver(t, mux)

	_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event: forge.SubmitComment,
		Comments: []submit.InlineComment{{
			Path:            "src/lib.rs",
			Line:            20,
			Side:            submit.SideNew,
			CounterpartLine: new(uint32(18)),
			Body:            "context line",
			CommentID:       "c1",
		}},
	})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	pos := position(t, discussions[0])
	if pos["new_line"] != float64(20) || pos["old_line"] != float64(18) {
		t.Errorf("context line must carry both sides: %v", pos)
	}
}

func TestCreateReviewOldSideComment(t *testing.T) {
	mux := newFixtureMux(t)
	var discussions []map[string]any
	mux.Handle("POST "+projectPrefix+"/discussions",
		captureJSON(t, &discussions, `{"id": "disc-1"}`))
	d := newTestDriver(t, mux)

	oldPath := "renamed/old.rs"
	_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event: forge.SubmitComment,
		Comments: []submit.InlineComment{{
			Path:      "renamed/new.rs",
			OldPath:   &oldPath,
			Line:      7,
			Side:      submit.SideOld,
			Body:      "deleted line",
			CommentID: "c1",
		}},
	})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	pos := position(t, discussions[0])
	if pos["old_line"] != float64(7) {
		t.Errorf("old_line = %v", pos["old_line"])
	}
	if _, present := pos["new_line"]; present {
		t.Errorf("new_line must be absent for a pure old-side comment: %v", pos)
	}
	if pos["old_path"] != "renamed/old.rs" || pos["new_path"] != "renamed/new.rs" {
		t.Errorf("paths = %v/%v", pos["old_path"], pos["new_path"])
	}
}

func TestCreateReviewMultiLineRangeGolden(t *testing.T) {
	mux := newFixtureMux(t)
	var discussions []map[string]any
	mux.Handle("POST "+projectPrefix+"/discussions",
		captureJSON(t, &discussions, `{"id": "disc-1"}`))
	d := newTestDriver(t, mux)

	_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event: forge.SubmitComment,
		Comments: []submit.InlineComment{{
			Path:      "src/lib.rs",
			Line:      15,
			Side:      submit.SideNew,
			StartLine: new(uint32(12)),
			StartSide: new(submit.SideNew),
			Body:      "range comment",
			CommentID: "c1",
		}},
	})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	pos := position(t, discussions[0])
	lineRange, ok := pos["line_range"].(map[string]any)
	if !ok {
		t.Fatalf("position has no line_range: %v", pos)
	}
	start, _ := lineRange["start"].(map[string]any)
	end, _ := lineRange["end"].(map[string]any)
	if start["type"] != "new" || start["new_line"] != float64(12) {
		t.Errorf("start = %v", start)
	}
	if start["line_code"] != sha1LibRS+"_0_12" {
		t.Errorf("start line_code = %v, want %s_0_12", start["line_code"], sha1LibRS)
	}
	if end["type"] != "new" || end["new_line"] != float64(15) {
		t.Errorf("end = %v", end)
	}
	if end["line_code"] != sha1LibRS+"_0_15" {
		t.Errorf("end line_code = %v", end["line_code"])
	}
}

func TestCreateReviewOldSideRangeEndpointLineCode(t *testing.T) {
	if got := lineCode("src/lib.rs", 9, 0); got != sha1LibRS+"_9_0" {
		t.Errorf("lineCode = %q", got)
	}
	endpoint := rangeEndpoint("src/lib.rs", submit.SideOld, 9)
	if *endpoint.Type != "old" || *endpoint.OldLine != 9 || endpoint.NewLine != nil {
		t.Errorf("endpoint = %+v", endpoint)
	}
}

func TestCreateReviewDraftUsesDraftNotes(t *testing.T) {
	mux := newFixtureMux(t)
	var draftNotes []map[string]any
	mux.Handle("POST "+projectPrefix+"/draft_notes",
		captureJSON(t, &draftNotes, `{"id": 77}`))
	d := newTestDriver(t, mux)

	result, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event: forge.SubmitDraft,
		Body:  "draft summary",
		Comments: []submit.InlineComment{{
			Path:      "src/lib.rs",
			Line:      3,
			Side:      submit.SideNew,
			Body:      "draft inline",
			CommentID: "c1",
		}},
	})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if result.State != "PENDING" || result.ReviewID != "77" {
		t.Errorf("result = %+v", result)
	}
	if len(draftNotes) != 2 {
		t.Fatalf("draft notes = %+v, want 2", draftNotes)
	}
	// Draft notes use `note`, not `body`.
	if draftNotes[0]["note"] != "draft summary" {
		t.Errorf("draft body note = %+v", draftNotes[0])
	}
	if _, present := draftNotes[0]["position"]; present {
		t.Errorf("body draft note must carry no position: %+v", draftNotes[0])
	}
	if draftNotes[1]["note"] != "draft inline" {
		t.Errorf("draft inline note = %+v", draftNotes[1])
	}
	pos := position(t, draftNotes[1])
	if pos["new_line"] != float64(3) || pos["head_sha"] != "headsha1" {
		t.Errorf("draft position = %v", pos)
	}
}

func TestCreateReviewApprove(t *testing.T) {
	mux := newFixtureMux(t)
	approved := false
	mux.Handle("POST "+projectPrefix+"/approve", func(w http.ResponseWriter, _ *http.Request) {
		approved = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 42}`))
	})
	d := newTestDriver(t, mux)

	result, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event: forge.SubmitApprove,
	})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if !approved {
		t.Error("approve endpoint was not called")
	}
	if result.State != "APPROVED" {
		t.Errorf("State = %q", result.State)
	}
}

func TestCreateReviewRequestChangesGraphQLGolden(t *testing.T) {
	mux := newFixtureMux(t)
	var gql []map[string]any
	mux.Handle("POST /graphql", captureJSON(t, &gql,
		`{"data": {"mergeRequestRequestChanges": {"errors": []}}}`))
	d := newTestDriver(t, mux)

	result, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event: forge.SubmitRequestChanges,
		Body:  "", // no note; straight to the mutation
	})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if result.State != "CHANGES_REQUESTED" {
		t.Errorf("State = %q", result.State)
	}
	if len(gql) != 1 {
		t.Fatalf("graphql calls = %d, want 1", len(gql))
	}
	if gql[0]["query"] != requestChangesMutation {
		t.Errorf("query = %v", gql[0]["query"])
	}
	variables, _ := gql[0]["variables"].(map[string]any)
	if variables["projectPath"] != "group/sub/repo" || variables["iid"] != "42" {
		t.Errorf("variables = %v", variables)
	}
}

func TestCreateReviewRequestChangesLogicalErrors(t *testing.T) {
	cases := map[string]string{
		"payload errors":   `{"data": {"mergeRequestRequestChanges": {"errors": ["not allowed"]}}}`,
		"top-level errors": `{"errors": [{"message": "field missing"}]}`,
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			mux := newFixtureMux(t)
			mux.JSON("POST /graphql", reply)
			d := newTestDriver(t, mux)
			_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
				Event: forge.SubmitRequestChanges,
			})
			wantForgeErr(t, err, forge.ErrorValidation)
		})
	}
}

func TestCreateReviewRequestChangesHTTPError(t *testing.T) {
	mux := newFixtureMux(t)
	mux.Handle("POST /graphql", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message": "forbidden"}`))
	})
	d := newTestDriver(t, mux)
	_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event: forge.SubmitRequestChanges,
	})
	wantForgeErr(t, err, forge.ErrorForbidden)
}

func TestGraphqlErrorsCollector(t *testing.T) {
	if got := graphqlErrors([]byte(`not json`), "m"); got != nil {
		t.Errorf("unparsable body must pass: %v", got)
	}
	got := graphqlErrors([]byte(
		`{"errors": ["plain", {"message": "boxed"}, {"other": 1}],`+
			`"data": {"m": {"errors": ["inner"]}}}`), "m")
	if len(got) != 4 || got[0] != "plain" || got[1] != "boxed" ||
		got[2] != `{"other": 1}` || got[3] != "inner" {
		t.Errorf("messages = %v", got)
	}
}

func TestCreateReviewPartialFailureMidway(t *testing.T) {
	mux := newFixtureMux(t)
	call := 0
	mux.Handle("POST "+projectPrefix+"/discussions", func(w http.ResponseWriter, _ *http.Request) {
		call++
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			_, _ = w.Write([]byte(`{"id": "disc-first"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message": "boom"}`))
	})
	d := newTestDriver(t, mux)

	result, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event: forge.SubmitComment,
		Comments: []submit.InlineComment{
			{Path: "a.go", Line: 1, Side: submit.SideNew, Body: "one", CommentID: "c1"},
			{Path: "b.go", Line: 2, Side: submit.SideNew, Body: "two", CommentID: "c2"},
			{Path: "c.go", Line: 3, Side: submit.SideNew, Body: "three", CommentID: "c3"},
		},
	})
	if err != nil {
		t.Fatalf("partial failures must return a result with Partial, got error: %v", err)
	}
	if result.Partial == nil {
		t.Fatal("expected Partial")
	}
	if len(result.Partial.SucceededCommentIDs) != 1 ||
		result.Partial.SucceededCommentIDs[0] != "c1" {
		t.Errorf("SucceededCommentIDs = %v, want [c1]", result.Partial.SucceededCommentIDs)
	}
	if result.Partial.FailedAt != 1 {
		t.Errorf("FailedAt = %d, want 1", result.Partial.FailedAt)
	}
	var fe *forge.Error
	if !errors.As(result.Partial.Cause, &fe) || fe.Kind != forge.ErrorServer {
		t.Errorf("Cause = %v, want forge server error", result.Partial.Cause)
	}
	if result.ReviewID != "disc-first" {
		t.Errorf("ReviewID = %q, want disc-first", result.ReviewID)
	}
}

func TestCreateReviewFailureBeforeAnythingPostedIsPlainError(t *testing.T) {
	mux := newFixtureMux(t)
	mux.Handle("POST "+projectPrefix+"/notes", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message": "403 Forbidden"}`))
	})
	d := newTestDriver(t, mux)

	result, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event: forge.SubmitComment,
		Body:  "body",
		Comments: []submit.InlineComment{
			{Path: "a.go", Line: 1, Side: submit.SideNew, Body: "one", CommentID: "c1"},
		},
	})
	if result != nil {
		t.Errorf("result = %+v, want nil when nothing succeeded", result)
	}
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorForbidden || fe.Hint != hintReviewForbidden {
		t.Errorf("error = %+v", fe)
	}
}

func TestCreateReviewEventFailureAfterCommentsIsPartial(t *testing.T) {
	mux := newFixtureMux(t)
	mux.JSON("POST "+projectPrefix+"/discussions", `{"id": "disc-1"}`)
	mux.Handle("POST "+projectPrefix+"/approve", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message": "401 Unauthorized"}`))
	})
	d := newTestDriver(t, mux)

	result, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event: forge.SubmitApprove,
		Comments: []submit.InlineComment{
			{Path: "a.go", Line: 1, Side: submit.SideNew, Body: "one", CommentID: "c1"},
		},
	})
	if err != nil {
		t.Fatalf("expected partial result, got error: %v", err)
	}
	if result.Partial == nil || result.Partial.FailedAt != 1 {
		t.Fatalf("Partial = %+v, want FailedAt=1 (event step)", result.Partial)
	}
	if len(result.Partial.SucceededCommentIDs) != 1 {
		t.Errorf("SucceededCommentIDs = %v", result.Partial.SucceededCommentIDs)
	}
	if result.State != "COMMENTED" {
		t.Errorf("State = %q, want COMMENTED when the event step failed", result.State)
	}
}
