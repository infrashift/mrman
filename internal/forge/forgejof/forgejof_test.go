package forgejof

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

func TestNewDefaultsHostAndBase(t *testing.T) {
	d, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.host != "codeberg.org" || d.base != "https://codeberg.org" {
		t.Errorf("host=%q base=%q", d.host, d.base)
	}

	d, err = New(Options{Host: "forgejo.corp.example"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.host != "forgejo.corp.example" || d.base != "https://forgejo.corp.example" {
		t.Errorf("host=%q base=%q", d.host, d.base)
	}
}

func TestNewStripsAPISuffixAndTrailingSlash(t *testing.T) {
	d, err := New(Options{Host: "fj.example", APIBase: "https://fj.example/api/v1/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.base != "https://fj.example" {
		t.Errorf("base = %q, want https://fj.example", d.base)
	}
}

func TestNewRejectsInvalidAPIBase(t *testing.T) {
	for _, apiBase := range []string{"://bad", "not-a-url"} {
		_, err := New(Options{APIBase: apiBase})
		fe := mustForgeErr(t, err)
		if fe.Kind != forge.ErrorValidation {
			t.Errorf("APIBase %q: kind = %v, want validation", apiBase, fe.Kind)
		}
		if fe.Hint == "" {
			t.Errorf("APIBase %q: expected an actionable hint", apiBase)
		}
	}
}

func TestIDAndLocalCheckoutPath(t *testing.T) {
	d, err := New(Options{LocalCheckout: "/repo"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.ID() != forgetypes.KindForgejo {
		t.Errorf("ID = %q", d.ID())
	}
	if d.LocalCheckoutPath() != "/repo" {
		t.Errorf("LocalCheckoutPath = %q", d.LocalCheckoutPath())
	}
}

func TestCapabilitiesMatrix(t *testing.T) {
	d, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := d.Capabilities()
	want := forge.Capabilities{
		DraftReviews:          true,
		Approve:               true,
		RequestChanges:        true,
		ReviewSummaries:       true,
		ReviewThreads:         true,
		ThreadResolution:      false,
		ThreadOutdated:        false,
		MultiLineComments:     false,
		CommitRangeDiff:       false,
		ReviewRequestedFilter: true,
		AtomicSubmit:          true,
		CommitScopedReviews:   true,
	}
	if got != want {
		t.Errorf("Capabilities = %+v, want %+v", got, want)
	}
}

// TestRequestsCarryTokenAndContextPath proves the per-request client clone
// sends the token header and hits the /api/v1 prefix without any /version
// handshake.
func TestRequestsCarryTokenAndContextPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42.diff", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token "+testToken {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = fmt.Fprint(w, "diff --git a/f b/f\n")
	})
	mux.HandleFunc("GET /api/v1/version", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("driver must not perform the SDK version handshake")
	})
	d := newTestDriver(t, mux)

	diff, err := d.GetDiff(context.Background(), testPR())
	if err != nil {
		t.Fatalf("GetDiff: %v", err)
	}
	if diff != "diff --git a/f b/f\n" {
		t.Errorf("diff = %q", diff)
	}
}
