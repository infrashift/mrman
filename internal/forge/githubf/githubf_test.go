package githubf

import (
	"reflect"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

func TestNewDefaultsToSaaSEndpoints(t *testing.T) {
	d, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := d.rest.BaseURL.String(); got != "https://api.github.com/" {
		t.Errorf("BaseURL = %q", got)
	}
	if d.graphqlURL != "https://api.github.com/graphql" {
		t.Errorf("graphqlURL = %q", d.graphqlURL)
	}
	if d.host != "github.com" {
		t.Errorf("host = %q", d.host)
	}
	if d.LocalCheckoutPath() != "" {
		t.Errorf("LocalCheckoutPath = %q, want empty", d.LocalCheckoutPath())
	}
}

func TestNewDerivesEnterpriseURLsFromHost(t *testing.T) {
	d, err := New(Options{Host: "ghe.corp.example"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := d.rest.BaseURL.String(); got != "https://ghe.corp.example/api/v3/" {
		t.Errorf("BaseURL = %q", got)
	}
	if d.graphqlURL != "https://ghe.corp.example/api/graphql" {
		t.Errorf("graphqlURL = %q", d.graphqlURL)
	}
}

func TestNewRespectsExplicitAPIBase(t *testing.T) {
	d, err := New(Options{Host: "ghe.corp.example", APIBase: "https://ghe.corp.example/api/v3/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := d.rest.BaseURL.String(); got != "https://ghe.corp.example/api/v3/" {
		t.Errorf("BaseURL = %q", got)
	}
	if d.graphqlURL != "https://ghe.corp.example/api/graphql" {
		t.Errorf("graphqlURL = %q", d.graphqlURL)
	}
}

func TestNewDerivesGraphQLForNonConventionBase(t *testing.T) {
	// Fixture servers have no /api/v3 suffix; GraphQL lands at /graphql.
	d, err := New(Options{APIBase: "http://127.0.0.1:8080"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.graphqlURL != "http://127.0.0.1:8080/graphql" {
		t.Errorf("graphqlURL = %q", d.graphqlURL)
	}
}

func TestNewRejectsInvalidAPIBase(t *testing.T) {
	for _, base := range []string{"://bad", "not-a-url"} {
		_, err := New(Options{APIBase: base})
		wantForgeErr(t, err, forge.ErrorValidation)
	}
}

func TestIDAndLocalCheckout(t *testing.T) {
	d, err := New(Options{LocalCheckout: "/some/repo"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.ID() != forgetypes.KindGitHub {
		t.Errorf("ID = %q", d.ID())
	}
	if d.LocalCheckoutPath() != "/some/repo" {
		t.Errorf("LocalCheckoutPath = %q", d.LocalCheckoutPath())
	}
}

func TestCapabilitiesEverythingSupported(t *testing.T) {
	d, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	caps := reflect.ValueOf(d.Capabilities())
	for i := 0; i < caps.NumField(); i++ {
		if !caps.Field(i).Bool() {
			t.Errorf("capability %s must be true for GitHub", caps.Type().Field(i).Name)
		}
	}
}
