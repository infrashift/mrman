// Package drivers registers every built forge driver into the registry.
// Importing it (usually for side effects from the UI layer) makes the
// GitHub driver — and, as they land, the others — resolvable by host.
package drivers

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/azdof"
	"github.com/infrashift/mrman/internal/forge/forgejof"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/forge/githubf"
	"github.com/infrashift/mrman/internal/forge/gitlabf"
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
	forge.Register(forge.Driver{
		ID:         forgetypes.KindGitLab,
		SlugPrefix: "gl",
		New: func(cfg forge.HostConfig) (forge.Forge, error) {
			// client-go takes the token itself; the client carries TLS.
			client, err := forge.BuildHTTPClient(cfg)
			if err != nil {
				return nil, err
			}
			return gitlabf.New(gitlabf.Options{
				Host:       cfg.Host,
				APIBase:    cfg.APIBase,
				HTTPClient: client,
				Token:      cfg.Token,
				Runner:     vcs.SystemRunner{},
			})
		},
	})
	forge.Register(forge.Driver{
		ID:         forgetypes.KindAzureDevOps,
		SlugPrefix: "ado",
		// ADO clients bind to an org/project/repo coordinate, so the
		// repo-aware constructor is required.
		NewForRepo: func(cfg forge.HostConfig, repo forgetypes.Repository) (forge.Forge, error) {
			client, err := forge.BuildHTTPClient(cfg)
			if err != nil {
				return nil, err
			}
			orgURL := cfg.APIBase
			if orgURL == "" {
				orgURL = "https://" + cfg.Host + "/" + repo.Owner
			}
			return azdof.New(azdof.Options{
				OrgURL:     orgURL,
				Project:    repo.Project,
				Repo:       repo.Name,
				HTTPClient: client,
				Token:      cfg.Token,
				Runner:     vcs.SystemRunner{},
			})
		},
	})
	forge.Register(forge.Driver{
		ID:         forgetypes.KindForgejo,
		SlugPrefix: "fj",
		New: func(cfg forge.HostConfig) (forge.Forge, error) {
			// The Forgejo SDK carries the token itself; the client only
			// provides TLS configuration.
			client, err := forge.BuildHTTPClient(cfg)
			if err != nil {
				return nil, err
			}
			return forgejof.New(forgejof.Options{
				Host:       cfg.Host,
				APIBase:    cfg.APIBase,
				HTTPClient: client,
				Token:      cfg.Token,
				Runner:     vcs.SystemRunner{},
			})
		},
	})
}
