package azdof

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

func TestNewValidatesOrgURL(t *testing.T) {
	for _, bad := range []string{"", "not-a-url", "/relative/path"} {
		_, err := New(Options{OrgURL: bad})
		if err == nil {
			t.Errorf("New(%q) must fail", bad)
			continue
		}
		var fe *forge.Error
		if !errors.As(err, &fe) {
			t.Fatalf("expected *forge.Error, got %T", err)
		}
		if fe.Kind != forge.ErrorValidation {
			t.Errorf("kind = %v, want validation", fe.Kind)
		}
		if !strings.Contains(fe.Hint, "OrgURL") {
			t.Errorf("hint %q must explain OrgURL", fe.Hint)
		}
	}
}

func TestNewTrimsTrailingSlash(t *testing.T) {
	d, err := New(Options{OrgURL: "https://dev.azure.com/org/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.orgURL != "https://dev.azure.com/org" {
		t.Errorf("orgURL = %q", d.orgURL)
	}
	if d.host != "dev.azure.com" {
		t.Errorf("host = %q", d.host)
	}
}

func TestIdentityAndCapabilities(t *testing.T) {
	d, err := New(Options{OrgURL: "https://dev.azure.com/org", LocalCheckout: "/repo"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.ID() != forgetypes.KindAzureDevOps {
		t.Errorf("ID = %q", d.ID())
	}
	if d.LocalCheckoutPath() != "/repo" {
		t.Errorf("LocalCheckoutPath = %q", d.LocalCheckoutPath())
	}
	caps := d.Capabilities()
	if caps.DraftReviews || caps.ReviewSummaries || caps.ThreadOutdated ||
		caps.CommitRangeDiff || caps.AtomicSubmit || caps.CommitScopedReviews {
		t.Errorf("capabilities that ADO lacks must be false: %+v", caps)
	}
	if !caps.Approve || !caps.RequestChanges || !caps.ReviewThreads ||
		!caps.ThreadResolution || !caps.MultiLineComments || !caps.ReviewRequestedFilter {
		t.Errorf("capabilities that ADO has must be true: %+v", caps)
	}
}

func TestViewerUserCachesConnectionData(t *testing.T) {
	var hits atomic.Int32
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.URL.Path, "/_apis/connectionData") {
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
			return
		}
		hits.Add(1)
		writeJSON(w, http.StatusOK, connectionDataJSON())
	}))
	for range 2 {
		viewer, err := d.viewerUser(context.Background(), "test")
		if err != nil {
			t.Fatalf("viewerUser: %v", err)
		}
		if viewer.ID != testViewerID {
			t.Errorf("viewer id = %q", viewer.ID)
		}
		if viewer.displayName() != "Vera Viewer" {
			t.Errorf("displayName = %q", viewer.displayName())
		}
	}
	if hits.Load() != 1 {
		t.Errorf("connectionData hit %d times, want 1 (cached)", hits.Load())
	}
}

// TestViewerUserRequestsPreviewAPIVersion pins the "-preview" suffix on the
// raw connectionData call. Azure DevOps serves that resource under preview
// versioning only and answers a plain "7.1" with HTTP 400
// VssInvalidPreviewVersionException, which broke every approve and
// request-changes against dev.azure.com. The path-matching fakes elsewhere in
// this package cannot catch it, so assert the negotiated version directly.
func TestViewerUserRequestsPreviewAPIVersion(t *testing.T) {
	var accept string
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		writeJSON(w, http.StatusOK, connectionDataJSON())
	}))
	if _, err := d.viewerUser(context.Background(), "test"); err != nil {
		t.Fatalf("viewerUser: %v", err)
	}
	// The SDK encodes the requested api-version into the Accept header.
	if !strings.Contains(accept, "api-version="+connectionDataAPIVersion) {
		t.Errorf("Accept = %q, want api-version=%s", accept, connectionDataAPIVersion)
	}
	if !strings.Contains(connectionDataAPIVersion, "-preview") {
		t.Errorf("connectionDataAPIVersion = %q, want a -preview version", connectionDataAPIVersion)
	}
}

func TestViewerUserMissingIdentity(t *testing.T) {
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{}`)
	}))
	_, err := d.viewerUser(context.Background(), "test")
	wantForgeErr(t, err, forge.ErrorAuth)
}

func TestConnectionUserDisplayNamePrefersCustom(t *testing.T) {
	u := &connectionUser{ProviderDisplayName: "provider", CustomDisplayName: "custom"}
	if u.displayName() != "custom" {
		t.Errorf("displayName = %q", u.displayName())
	}
}
