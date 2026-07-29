package submit

import (
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

func note(content string) *model.Comment {
	return model.NewComment(content, model.CommentTypeFromID("note"), nil)
}

func untyped(content string) *model.Comment {
	return model.NewComment(content, model.CommentTypeFromID("none"), nil)
}

func TestReturnsEmptyBodyWhenNoInputs(t *testing.T) {
	if body := BuildReviewBody(nil, nil, true); body != "" {
		t.Fatalf("body = %q", body)
	}
}

func TestRendersReviewLevelCommentsWithTypePrefix(t *testing.T) {
	body := BuildReviewBody([]*model.Comment{note("first"), note("second")}, nil, true)
	if body != "[NOTE] first\n\n[NOTE] second" {
		t.Fatalf("body = %q", body)
	}
}

func TestRendersUnplacedCommentsSection(t *testing.T) {
	item := MovedItem{
		Comment: model.NewComment("kaboom", model.CommentTypeFromID("issue"), nil),
		File:    "src/lib.rs",
	}
	body := BuildReviewBody(nil, []MovedItem{item}, true)
	if !strings.Contains(body, "## Unplaced comments") {
		t.Fatalf("missing section: %q", body)
	}
	if !strings.Contains(body, "- [ISSUE] src/lib.rs: kaboom") {
		t.Fatalf("missing item: %q", body)
	}
	if strings.HasSuffix(body, "\n") {
		t.Fatalf("trailing newline: %q", body)
	}
}

func TestRendersReviewLevelAboveUnplacedSection(t *testing.T) {
	body := BuildReviewBody(
		[]*model.Comment{note("top")},
		[]MovedItem{{Comment: note("middle"), File: "a.rs"}},
		true,
	)
	top := strings.Index(body, "[NOTE] top")
	section := strings.Index(body, "## Unplaced comments")
	if top < 0 || section < 0 || top >= section {
		t.Fatalf("ordering wrong: %q", body)
	}
}

func TestOmitsTypePrefixInBodyWhenDisabled(t *testing.T) {
	body := BuildReviewBody([]*model.Comment{note("just text")}, nil, false)
	if body != "just text" {
		t.Fatalf("body = %q", body)
	}
}

func TestOmitsTypePrefixForNoneTypedCommentsEvenWhenEnabled(t *testing.T) {
	body := BuildReviewBody(
		[]*model.Comment{untyped("untyped")},
		[]MovedItem{{Comment: untyped("also untyped"), File: "a.rs"}},
		true,
	)
	if strings.Contains(body, "[") {
		t.Fatalf("no type tag expected on none: %q", body)
	}
	if !strings.Contains(body, "untyped") || !strings.Contains(body, "- a.rs: also untyped") {
		t.Fatalf("body = %q", body)
	}
}

func TestBuildInlineBodyOmitsTagForNoneButKeepsFileLevelMarker(t *testing.T) {
	c := untyped("untyped")
	if got := BuildInlineBody(c, false, true); got != "untyped" {
		t.Fatalf("line-level body = %q", got)
	}
	if got := BuildInlineBody(c, true, true); got != "File-level: untyped" {
		t.Fatalf("file-level body = %q", got)
	}
}

func TestBuildInlineBodyKeepsTagAndMarkerForTypedFileLevel(t *testing.T) {
	c := note("messy")
	if got := BuildInlineBody(c, true, true); got != "[NOTE] File-level: messy" {
		t.Fatalf("body = %q", got)
	}
}
