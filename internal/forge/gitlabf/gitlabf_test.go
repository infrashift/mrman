package gitlabf

import (
	"encoding/json"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

func TestNewDerivesDefaults(t *testing.T) {
	d, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.host != "gitlab.com" {
		t.Errorf("host = %q, want gitlab.com", d.host)
	}
	if d.graphqlURL != "https://gitlab.com/api/graphql" {
		t.Errorf("graphqlURL = %q", d.graphqlURL)
	}
	if d.ID() != forgetypes.KindGitLab {
		t.Errorf("ID = %q", d.ID())
	}
}

func TestNewSelfHostedDerivesAPIBase(t *testing.T) {
	d, err := New(Options{Host: "gitlab.corp.example", LocalCheckout: "/repo"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.graphqlURL != "https://gitlab.corp.example/api/graphql" {
		t.Errorf("graphqlURL = %q", d.graphqlURL)
	}
	if d.LocalCheckoutPath() != "/repo" {
		t.Errorf("LocalCheckoutPath = %q", d.LocalCheckoutPath())
	}
}

func TestNewRejectsInvalidAPIBase(t *testing.T) {
	_, err := New(Options{APIBase: "not-a-url"})
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestGraphqlEndpointFixtureBase(t *testing.T) {
	if got := graphqlEndpoint("http://127.0.0.1:1234"); got != "http://127.0.0.1:1234/graphql" {
		t.Errorf("graphqlEndpoint = %q", got)
	}
	if got := graphqlEndpoint("https://host/api/v4/"); got != "https://host/api/graphql" {
		t.Errorf("graphqlEndpoint = %q", got)
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
		ReviewSummaries:       false,
		ReviewThreads:         true,
		ThreadResolution:      true,
		ThreadOutdated:        false,
		MultiLineComments:     true,
		CommitRangeDiff:       true,
		ReviewRequestedFilter: true,
		AtomicSubmit:          false,
		CommitScopedReviews:   true,
	}
	if got != want {
		t.Errorf("Capabilities = %+v, want %+v", got, want)
	}
}

func TestStartSHAFallsBackToBase(t *testing.T) {
	pr := testPR()
	if got := startSHA(pr); got != "startsha1" {
		t.Errorf("startSHA = %q, want startsha1", got)
	}
	pr.ForgePayload = nil
	if got := startSHA(pr); got != "basesha1" {
		t.Errorf("startSHA without payload = %q, want basesha1", got)
	}
	pr.ForgePayload = json.RawMessage(`{broken`)
	if got := startSHA(pr); got != "basesha1" {
		t.Errorf("startSHA with broken payload = %q, want basesha1", got)
	}
}

func TestNormalizeState(t *testing.T) {
	cases := map[string]string{
		"opened": "open",
		"OPENED": "open",
		"merged": "merged",
		"closed": "closed",
		"locked": "locked",
	}
	for in, want := range cases {
		if got := normalizeState(in); got != want {
			t.Errorf("normalizeState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProjectIDJoinsSubgroups(t *testing.T) {
	if got := projectID(testRepo()); got != "group/sub/repo" {
		t.Errorf("projectID = %q", got)
	}
}
