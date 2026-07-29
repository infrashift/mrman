// Package githubf implements the GitHub forge driver on top of
// go-github (REST) and shurcooL/githubv4 (GraphQL). It serves both
// github.com and GitHub Enterprise Server hosts; the API base URL is
// derived from the host or overridden explicitly, which is also the
// seam driver tests use to point at fixture servers.
package githubf

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-github/v76/github"
	"github.com/shurcooL/githubv4"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/vcs"
)

// defaultHost is the GitHub SaaS host.
const defaultHost = "github.com"

// Options configures a GitHub driver.
type Options struct {
	// Host is "github.com" (default when empty) or a GitHub Enterprise
	// Server hostname.
	Host string
	// APIBase overrides the REST base URL. Empty derives it from Host:
	// SaaS uses https://api.github.com, GHE uses https://HOST/api/v3.
	// The GraphQL endpoint is derived from the same base (…/api/v3 →
	// …/api/graphql, anything else gets /graphql appended), so tests can
	// point both clients at one fixture server.
	APIBase string
	// HTTPClient carries authentication (e.g. an oauth2 client). Nil uses
	// http.DefaultClient, unauthenticated.
	HTTPClient *http.Client
	// LocalCheckout is an optional path to a local git checkout used as a
	// read-only fast path for blobs and range diffs. Never the source of
	// truth; empty disables the fast path.
	LocalCheckout string
	// Runner executes local git commands for the fast paths. Nil disables
	// them even when LocalCheckout is set.
	Runner vcs.Runner
}

// Driver is the GitHub implementation of forge.Forge.
type Driver struct {
	host          string
	rest          *github.Client
	gql           *githubv4.Client
	graphqlURL    string
	localCheckout string
	runner        vcs.Runner
}

var _ forge.Forge = (*Driver)(nil)

// New builds a GitHub driver from opts.
func New(opts Options) (*Driver, error) {
	host := opts.Host
	if host == "" {
		host = defaultHost
	}
	d := &Driver{
		host:          host,
		localCheckout: opts.LocalCheckout,
		runner:        opts.Runner,
	}
	rest := github.NewClient(opts.HTTPClient)
	if opts.APIBase == "" && host == defaultHost {
		d.graphqlURL = "https://api.github.com/graphql"
		d.gql = githubv4.NewClient(opts.HTTPClient)
	} else {
		apiBase := opts.APIBase
		if apiBase == "" {
			apiBase = "https://" + host + "/api/v3"
		}
		base, err := url.Parse(strings.TrimSuffix(apiBase, "/") + "/")
		if err != nil || base.Scheme == "" || base.Host == "" {
			return nil, forge.NewError(forgetypes.KindGitHub, "new", host,
				forge.ErrorValidation, fmt.Errorf("invalid api base %q", apiBase),
			).WithHint("api_base must be an absolute URL like https://HOST/api/v3")
		}
		rest.BaseURL = base
		d.graphqlURL = graphqlEndpoint(apiBase)
		d.gql = githubv4.NewEnterpriseClient(d.graphqlURL, opts.HTTPClient)
	}
	d.rest = rest
	return d, nil
}

// graphqlEndpoint derives the GraphQL URL from a REST base: the GHE
// convention https://HOST/api/v3 maps to https://HOST/api/graphql; any
// other base (fixture servers) gets /graphql appended.
func graphqlEndpoint(apiBase string) string {
	s := strings.TrimSuffix(apiBase, "/")
	if base, ok := strings.CutSuffix(s, "/api/v3"); ok {
		return base + "/api/graphql"
	}
	return s + "/graphql"
}

// ID returns the forge kind.
func (d *Driver) ID() forgetypes.Kind { return forgetypes.KindGitHub }

// Capabilities reports GitHub's capability set: everything is supported.
func (d *Driver) Capabilities() forge.Capabilities {
	return forge.Capabilities{
		DraftReviews:          true,
		Approve:               true,
		RequestChanges:        true,
		ReviewSummaries:       true,
		ReviewThreads:         true,
		ThreadResolution:      true,
		ThreadOutdated:        true,
		MultiLineComments:     true,
		CommitRangeDiff:       true,
		ReviewRequestedFilter: true,
		AtomicSubmit:          true,
		CommitScopedReviews:   true,
	}
}

// LocalCheckoutPath returns the optional local checkout root, "" when none.
func (d *Driver) LocalCheckoutPath() string { return d.localCheckout }
