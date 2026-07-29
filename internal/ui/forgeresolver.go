package ui

import (
	"errors"
	"sync"

	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/vcs"
)

// errNoForgeRepository is returned when the checkout has no remote that maps
// to a supported forge, so the Pull Requests tab has nothing to list.
var errNoForgeRepository = errors.New(
	"no forge remote found: run inside a checkout with a GitHub, GitLab, Forgejo or Azure DevOps remote")

// forgeResolver lazily resolves the forge driver and repository the Pull
// Requests tab lists from.
//
// Resolution is deferred and memoized because it can shell out — a
// token_cmd, or `gh auth token` — and a local `mrman` launch that never
// opens the PR tab must not pay for that. Every call happens inside a
// tea.Cmd goroutine, off the render loop.
type forgeResolver struct {
	once    sync.Once
	resolve func() (forge.Forge, forgetypes.Repository, error)

	backend forge.Forge
	repo    forgetypes.Repository
	err     error
}

// Get resolves the backend and repository, memoizing both the success and
// the failure so a broken token is not retried on every keystroke.
func (r *forgeResolver) Get() (forge.Forge, forgetypes.Repository, error) {
	if r == nil || r.resolve == nil {
		return nil, forgetypes.Repository{}, errNoForgeRepository
	}
	r.once.Do(func() { r.backend, r.repo, r.err = r.resolve() })
	return r.backend, r.repo, r.err
}

// staticForgeResolver wraps an already-resolved backend, used by `mrman pr`
// where opening the PR resolved the forge before the TUI started.
func staticForgeResolver(backend forge.Forge, repo forgetypes.Repository) *forgeResolver {
	return &forgeResolver{
		resolve: func() (forge.Forge, forgetypes.Repository, error) { return backend, repo, nil },
	}
}

// checkoutForgeResolver resolves the forge from a local checkout's remotes,
// exactly as `mrman pr <number>` does for bare-number targets.
func checkoutForgeResolver(cwd string, forgeCfg config.ForgeConfig) *forgeResolver {
	return &forgeResolver{
		resolve: func() (forge.Forge, forgetypes.Repository, error) {
			urls := forge.RemoteURLs(cwd, vcs.SystemRunner{})
			if len(urls) == 0 {
				return nil, forgetypes.Repository{}, errNoForgeRepository
			}
			repo, err := forge.ResolveRepository(urls, forgeCfg)
			if err != nil {
				return nil, forgetypes.Repository{}, err
			}
			backend, err := forge.ForRepository(*repo, forgeCfg)
			if err != nil {
				return nil, forgetypes.Repository{}, err
			}
			return backend, *repo, nil
		},
	}
}
