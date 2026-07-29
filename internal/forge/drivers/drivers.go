// Package drivers registers every built forge driver into the registry.
// Importing it (usually for side effects from the UI layer) makes the
// GitHub driver — and, as they land, the others — resolvable by host.
package drivers

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/forge/githubf"
	"github.com/infrashift/mrman/internal/vcs"
)

// authedClient layers the resolved token onto the host's TLS-configured
// transport via oauth2; unauthenticated hosts keep the plain client.
func authedClient(cfg forge.HostConfig) (*http.Client, error) {
	base, err := forge.BuildHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Token == "" {
		return base, nil
	}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, base)
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.Token})
	return oauth2.NewClient(ctx, src), nil
}

func init() {
	forge.Register(forge.Driver{
		ID:         forgetypes.KindGitHub,
		SlugPrefix: "gh",
		New: func(cfg forge.HostConfig) (forge.Forge, error) {
			client, err := authedClient(cfg)
			if err != nil {
				return nil, err
			}
			return githubf.New(githubf.Options{
				Host:       cfg.Host,
				APIBase:    cfg.APIBase,
				HTTPClient: client,
				Runner:     vcs.SystemRunner{},
			})
		},
	})
}
