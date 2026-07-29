package forgejof

import (
	"context"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

// TestErrorTranslationTable drives GetDiff through every interesting HTTP
// status and asserts the resulting kind, status, and hint.
func TestErrorTranslationTable(t *testing.T) {
	cases := []struct {
		status int
		kind   forge.ErrorKind
		hint   string
	}{
		{http.StatusUnauthorized, forge.ErrorAuth, hintAuth},
		{http.StatusForbidden, forge.ErrorForbidden, hintForbidden},
		{http.StatusNotFound, forge.ErrorNotFound, hintNotFound},
		{http.StatusGone, forge.ErrorNotFound, hintNotFound},
		{http.StatusConflict, forge.ErrorConflict, ""},
		{http.StatusUnprocessableEntity, forge.ErrorValidation, ""},
		{http.StatusTooManyRequests, forge.ErrorRateLimited, hintRateLimited},
		{http.StatusBadGateway, forge.ErrorServer, ""},
	}
	for _, tc := range cases {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42.diff", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"nope"}`, tc.status)
		})
		d := newTestDriver(t, mux)
		_, err := d.GetDiff(context.Background(), testPR())
		fe := mustForgeErr(t, err)
		if fe.Kind != tc.kind || fe.Status != tc.status || fe.Hint != tc.hint {
			t.Errorf("status %d: got kind=%v status=%d hint=%q, want kind=%v hint=%q",
				tc.status, fe.Kind, fe.Status, fe.Hint, tc.kind, tc.hint)
		}
	}
}

func TestCanceledContextIsCanceledKind(t *testing.T) {
	d := newTestDriver(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := d.GetDiff(ctx, testPR())
	wantForgeErr(t, err, forge.ErrorCanceled)

	_, err = d.ListPullRequests(ctx, forge.ListQuery{
		Repository: testRepo(), Scope: forge.ScopeReviewRequested,
	})
	wantForgeErr(t, err, forge.ErrorCanceled)
}

func TestWrapPassesThroughForgeErrors(t *testing.T) {
	d, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	original := d.err("op", forge.ErrorConflict, 409, "keep me", nil)
	got := d.wrap("other_op", nil, original)
	fe := mustForgeErr(t, got)
	if fe.Kind != forge.ErrorConflict || fe.Op != "op" || fe.Hint != "keep me" {
		t.Errorf("wrap must pass through forge errors unchanged, got %v", got)
	}
	if d.wrap("op", nil, nil) != nil {
		t.Error("wrap(nil) must be nil")
	}
}
