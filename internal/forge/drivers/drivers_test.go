package drivers

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
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

// TestEveryDriverSendsItsToken: a driver built through the registry must
// put the host's token on its requests, in its forge's own scheme. Nothing
// else checked the wiring between HostConfig.Token and the SDK clients.
func TestEveryDriverSendsItsToken(t *testing.T) {
	const token = "s3cr3t-token"
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+token))
	for kind, want := range map[forgetypes.Kind][]string{
		forgetypes.KindGitHub:      {"Bearer " + token},
		forgetypes.KindGitLab:      {token}, // PRIVATE-TOKEN header
		forgetypes.KindAzureDevOps: {basic},
		forgetypes.KindForgejo:     {"token " + token},
	} {
		t.Run(string(kind), func(t *testing.T) {
			var mu sync.Mutex
			var seen []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				seen = append(seen, r.Header.Get("Authorization"), r.Header.Get("PRIVATE-TOKEN"))
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"not found"}`))
			}))
			t.Cleanup(srv.Close)

			d, _ := forge.DriverFor(kind)
			hc := forge.HostConfig{Host: "forge.test", Kind: kind, Token: token, APIBase: srv.URL}
			repo := forgetypes.Repository{Kind: kind, Host: "forge.test", Owner: "org", Project: "proj", Name: "repo"}
			var f forge.Forge
			var err error
			if d.NewForRepo != nil {
				f, err = d.NewForRepo(hc, repo)
			} else {
				f, err = d.New(hc)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: 1})

			mu.Lock()
			defer mu.Unlock()
			if len(seen) == 0 {
				t.Fatal("the driver made no request")
			}
			for _, w := range want {
				if !slices.Contains(seen, w) {
					t.Errorf("no request carried %q; saw headers %q", w, seen)
				}
			}
		})
	}
}
