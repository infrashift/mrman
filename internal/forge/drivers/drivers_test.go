package drivers

import (
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// TestEveryForgeKindHasADriver pins the init wiring: importing this package
// registers all four drivers, each constructible without a network.
func TestEveryForgeKindHasADriver(t *testing.T) {
	for kind, host := range map[forgetypes.Kind]string{
		forgetypes.KindGitHub:      "github.com",
		forgetypes.KindGitLab:      "gitlab.com",
		forgetypes.KindAzureDevOps: "dev.azure.com",
		forgetypes.KindForgejo:     "codeberg.org",
	} {
		d, ok := forge.DriverFor(kind)
		if !ok {
			t.Fatalf("no driver registered for %s", kind)
		}
		if d.SlugPrefix == "" {
			t.Errorf("%s has no slug prefix", kind)
		}
		hc := forge.HostConfig{Host: host, Kind: kind, Token: "t"}
		var (
			f   forge.Forge
			err error
		)
		if d.NewForRepo != nil {
			f, err = d.NewForRepo(hc, forgetypes.Repository{Kind: kind, Host: host, Owner: "org", Project: "proj", Name: "repo"})
		} else {
			f, err = d.New(hc)
		}
		if err != nil || f == nil {
			t.Fatalf("%s: constructing the driver: %v", kind, err)
		}
		if f.ID() != kind {
			t.Errorf("%s driver reports kind %s", kind, f.ID())
		}
	}
}

// TestADOOrgURL: a legacy {org}.visualstudio.com host already names the
// organization, so repeating it in the path made the API read it as a
// project and fail every call.
func TestADOOrgURL(t *testing.T) {
	repo := forgetypes.Repository{Kind: forgetypes.KindAzureDevOps, Owner: "myorg", Project: "p", Name: "r"}
	cases := []struct {
		cfg  forge.HostConfig
		want string
	}{
		{forge.HostConfig{Host: "dev.azure.com"}, "https://dev.azure.com/myorg"},
		{forge.HostConfig{Host: "myorg.visualstudio.com"}, "https://myorg.visualstudio.com"},
		{forge.HostConfig{Host: "ado.corp.example", APIBase: "https://ado.corp.example/tfs/Coll"},
			"https://ado.corp.example/tfs/Coll"},
	}
	for _, tc := range cases {
		if got := adoOrgURL(tc.cfg, repo); got != tc.want {
			t.Errorf("adoOrgURL(%s) = %q, want %q", tc.cfg.Host, got, tc.want)
		}
	}
}
