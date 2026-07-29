package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLineRangeNormalizesAndQueries(t *testing.T) {
	r := NewLineRange(20, 10)
	if r.Start != 10 || r.End != 20 {
		t.Fatalf("NewLineRange(20,10) = %+v", r)
	}
	if !NewLineRange(10, 20).Contains(10) || !NewLineRange(10, 20).Contains(20) || !NewLineRange(10, 20).Contains(15) {
		t.Error("Contains must include bounds and middle")
	}
	if NewLineRange(10, 20).Contains(5) || NewLineRange(10, 20).Contains(25) {
		t.Error("Contains must exclude outside lines")
	}
	single := SingleLineRange(42)
	if !single.IsSingle() || single.Contains(41) || !single.Contains(42) || single.Contains(43) {
		t.Errorf("single range misbehaves: %+v", single)
	}
	if NewLineRange(10, 15).IsSingle() {
		t.Error("multi-line range must not be single")
	}
}

func TestLineRangeSerializes(t *testing.T) {
	data, err := json.Marshal(NewLineRange(10, 20))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != `{"start":10,"end":20}` {
		t.Fatalf("got %s", got)
	}
	var r LineRange
	if err := json.Unmarshal([]byte(`{"start":10,"end":20}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Start != 10 || r.End != 20 {
		t.Fatalf("got %+v", r)
	}
}

func TestCommentTypeNormalization(t *testing.T) {
	for _, id := range []string{"none", "NONE", "", "   "} {
		if !CommentTypeFromID(id).IsNone() {
			t.Errorf("CommentTypeFromID(%q) should be none", id)
		}
	}
	if CommentTypeFromID("issue").IsNone() {
		t.Error("issue must not be none")
	}
	if got := CommentTypeFromID("  question ").ID(); got != "question" {
		t.Errorf("trim failed: %q", got)
	}
	if got := CommentTypeFromID("issue").Display(); got != "ISSUE" {
		t.Errorf("Display = %q", got)
	}
}

func TestCommentTypeSerde(t *testing.T) {
	data, _ := json.Marshal(CommentTypeFromID("question"))
	if string(data) != `"question"` {
		t.Fatalf("got %s", data)
	}
	var ct CommentType
	if err := json.Unmarshal([]byte(`"question"`), &ct); err != nil {
		t.Fatal(err)
	}
	if ct.ID() != "question" {
		t.Fatalf("got %q", ct.ID())
	}

	// None round-trips as the literal "none".
	data, _ = json.Marshal(CommentType(""))
	if string(data) != `"none"` {
		t.Fatalf("none serializes as %s", data)
	}
	if err := json.Unmarshal([]byte(`"none"`), &ct); err != nil {
		t.Fatal(err)
	}
	if !ct.IsNone() {
		t.Fatal("restored none must be none")
	}
}

func TestNewCommentDefaults(t *testing.T) {
	side := LineSideNew
	c := NewComment("Test comment", CommentTypeFromID("note"), &side)
	if c.LineRange != nil || c.Content != "Test comment" || c.CommentType.ID() != "note" {
		t.Fatalf("got %+v", c)
	}
	if c.Side == nil || *c.Side != LineSideNew {
		t.Fatal("side must be preserved")
	}
	if c.LifecycleState != LifecycleLocalDraft || c.IsLocked() {
		t.Fatal("new comments must be unlocked local drafts")
	}
	if c.Author != DefaultAuthor || c.RemoteReviewID != nil || c.RemoteCommentID != nil {
		t.Fatalf("got %+v", c)
	}
	if c.ID == "" {
		t.Fatal("id must be generated")
	}
}

func TestNewCommentWithRange(t *testing.T) {
	side := LineSideOld
	c := NewCommentWithRange("Range comment", CommentTypeFromID("issue"), &side, NewLineRange(10, 15))
	if c.LineRange == nil || c.LineRange.Start != 10 || c.LineRange.End != 15 {
		t.Fatalf("got %+v", c.LineRange)
	}
	if *c.Side != LineSideOld {
		t.Fatal("side lost")
	}
}

func TestCommentBuilders(t *testing.T) {
	c := NewComment("x", CommentTypeFromID("note"), nil).WithAuthor("Claude").WithCommitID("aaa111")
	if c.Author != "Claude" || c.CommitID == nil || *c.CommitID != "aaa111" {
		t.Fatalf("got %+v", c)
	}
}

func TestLockedStates(t *testing.T) {
	for _, state := range []CommentLifecycleState{LifecyclePushedDraft, LifecycleSubmitted} {
		c := NewComment("x", CommentTypeFromID("note"), nil)
		c.LifecycleState = state
		if !c.IsLocked() {
			t.Errorf("%s must be locked", state)
		}
	}
}

func TestCommentLifecycleRoundTrip(t *testing.T) {
	original := NewComment("body", CommentTypeFromID("issue"), nil)
	original.LifecycleState = LifecycleSubmitted
	rid, cid := "R_kgDOEx", "RC_kgDOEx"
	original.RemoteReviewID, original.RemoteCommentID = &rid, &cid

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored Comment
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.LifecycleState != LifecycleSubmitted ||
		restored.RemoteReviewID == nil || *restored.RemoteReviewID != rid ||
		restored.RemoteCommentID == nil || *restored.RemoteCommentID != cid {
		t.Fatalf("got %+v", restored)
	}
}

// Pre-PR-5 comment JSON: no side, line_range, author, lifecycle, remote ids,
// or commit_id. Everything must default like tuicr's serde defaults.
func TestLegacyCommentDefaults(t *testing.T) {
	legacy := `{
		"id": "legacy",
		"content": "pre-pr5",
		"comment_type": "note",
		"created_at": "2024-01-01T00:00:00Z",
		"line_context": null
	}`
	var c Comment
	if err := json.Unmarshal([]byte(legacy), &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != "legacy" || c.Content != "pre-pr5" {
		t.Fatalf("identity lost: %+v", c)
	}
	if c.LifecycleState != LifecycleLocalDraft || c.Author != DefaultAuthor {
		t.Fatalf("defaults wrong: %+v", c)
	}
	if c.Side != nil || c.LineRange != nil || c.RemoteReviewID != nil ||
		c.RemoteCommentID != nil || c.CommitID != nil {
		t.Fatalf("optional fields must default nil: %+v", c)
	}
}

// The serialized field set must stay identical to tuicr's: all twelve keys
// present, optionals as null.
func TestCommentSerializedShape(t *testing.T) {
	c := NewComment("x", CommentTypeFromID("note"), nil)
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, key := range []string{
		`"id"`, `"content"`, `"comment_type"`, `"created_at"`, `"line_context"`,
		`"side"`, `"line_range"`, `"author"`, `"lifecycle_state"`,
		`"remote_review_id"`, `"remote_comment_id"`, `"commit_id"`,
	} {
		if !strings.Contains(s, key) {
			t.Errorf("serialized comment missing %s: %s", key, s)
		}
	}
	if !strings.Contains(s, `"line_context":null`) || !strings.Contains(s, `"side":null`) {
		t.Errorf("optionals must serialize as null: %s", s)
	}
}
