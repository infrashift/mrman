package forgetypes

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSlugPrefixRoundTrip(t *testing.T) {
	for _, kind := range []Kind{KindGitHub, KindGitLab, KindAzureDevOps, KindForgejo} {
		prefix := kind.SlugPrefix()
		restored, ok := KindFromSlugPrefix(prefix)
		if !ok || restored != kind {
			t.Errorf("prefix %q: got %q, %v", prefix, restored, ok)
		}
	}
	if _, ok := KindFromSlugPrefix("zz"); ok {
		t.Error("unknown prefix must not resolve")
	}
	// Unknown kinds pass through as their own prefix (open-string design).
	if got := Kind("sourcehut").SlugPrefix(); got != "sourcehut" {
		t.Errorf("unknown kind prefix = %q", got)
	}
}

func TestRepositoryPathSegments(t *testing.T) {
	gh := Repository{Kind: KindGitHub, Host: "github.com", Owner: "infrashift", Name: "mrman"}
	if got := gh.Slug(); got != "infrashift/mrman" {
		t.Errorf("gh slug = %q", got)
	}
	subgroup := Repository{Kind: KindGitLab, Host: "gitlab.example.com", Owner: "group/subgroup", Name: "proj"}
	if got := subgroup.Slug(); got != "group/subgroup/proj" {
		t.Errorf("gitlab slug = %q", got)
	}
	ado := Repository{Kind: KindAzureDevOps, Host: "dev.azure.com", Owner: "org", Project: "project", Name: "repo"}
	segs := ado.PathSegments()
	if len(segs) != 3 || segs[0] != "org" || segs[1] != "project" || segs[2] != "repo" {
		t.Errorf("ado segments = %v", segs)
	}
}

func TestRepositoryJSONProjectOmitted(t *testing.T) {
	gh := Repository{Kind: KindGitHub, Host: "github.com", Owner: "o", Name: "r"}
	data, err := json.Marshal(gh)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "project") {
		t.Errorf("project must be omitted for non-ADO repos: %s", data)
	}
	ado := Repository{Kind: KindAzureDevOps, Host: "dev.azure.com", Owner: "o", Project: "p", Name: "r"}
	data, _ = json.Marshal(ado)
	if !strings.Contains(string(data), `"project":"p"`) {
		t.Errorf("ado project must serialize: %s", data)
	}
}

func TestPrSessionKeyShortHead(t *testing.T) {
	k := PrSessionKey{HeadSHA: "abcdef0123456789"}
	if got := k.ShortHead(); got != "abcdef01" {
		t.Errorf("ShortHead = %q", got)
	}
	short := PrSessionKey{HeadSHA: "abc"}
	if got := short.ShortHead(); got != "abc" {
		t.Errorf("short ShortHead = %q", got)
	}
}
