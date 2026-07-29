package gitlabf

import (
	"context"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

// TestErrorTranslationTable drives GetPullRequest into each HTTP failure
// and asserts the forge error kind, status, and hint. 501 stands in for
// the 5xx family because the SDK retries plain 500s before surfacing them.
func TestErrorTranslationTable(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		wantKind forge.ErrorKind
		wantHint string
	}{
		{"unauthorized", http.StatusUnauthorized, forge.ErrorAuth, hintAuth},
		{"forbidden", http.StatusForbidden, forge.ErrorForbidden, hintForbidden},
		{"not found", http.StatusNotFound, forge.ErrorNotFound, hintNotFound},
		{"rate limited", http.StatusTooManyRequests, forge.ErrorRateLimited, hintRateLimited},
		{"validation", http.StatusUnprocessableEntity, forge.ErrorValidation, ""},
		{"conflict", http.StatusConflict, forge.ErrorConflict, ""},
		{"server", http.StatusNotImplemented, forge.ErrorServer, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux := newFixtureMux(t)
			mux.Handle("GET "+projectPrefix, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(`{"message": "nope"}`))
			})
			d := newTestDriver(t, mux)

			_, err := d.GetPullRequest(context.Background(), target())
			fe := mustForgeErr(t, err)
			if fe.Kind != c.wantKind {
				t.Errorf("kind = %v, want %v", fe.Kind, c.wantKind)
			}
			if fe.Status != c.status {
				t.Errorf("status = %d, want %d", fe.Status, c.status)
			}
			if fe.Hint != c.wantHint {
				t.Errorf("hint = %q, want %q", fe.Hint, c.wantHint)
			}
		})
	}
}

// TestErrorCanceled asserts context cancellation surfaces as ErrorCanceled
// so the app can discard it silently.
func TestErrorCanceled(t *testing.T) {
	mux := newFixtureMux(t)
	mux.JSON("GET "+projectPrefix, mrDetailsBody)
	d := newTestDriver(t, mux)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := d.GetPullRequest(ctx, target())
	wantForgeErr(t, err, forge.ErrorCanceled)
}
