// Package gitlabf implements the GitLab forge driver on top of the
// official gitlab.com/gitlab-org/api/client-go SDK, serving both
// gitlab.com and self-hosted instances. The API base URL is derived from
// the host or overridden explicitly, which is also the seam driver tests
// use to point at fixture servers.
//
// GitLab has no atomic review submit: CreateReview runs an N+1 sequence
// (general note, one discussion or draft note per inline comment, then the
// approve/request-changes event) and reports mid-sequence failures through
// forge.SubmitResult.Partial.
package gitlabf

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/vcs"
)

// defaultHost is the GitLab SaaS host.
const defaultHost = "gitlab.com"

// Options configures a GitLab driver.
type Options struct {
	// Host is "gitlab.com" (default when empty) or a self-hosted GitLab
	// hostname.
	Host string
	// APIBase overrides the REST base URL. Empty derives it from Host:
	// https://HOST/api/v4. The GraphQL endpoint used for the
	// request-changes mutation is derived from the same base (…/api/v4 →
	// …/api/graphql, anything else gets /graphql appended), so tests can
	// point both clients at one fixture server.
	APIBase string
	// HTTPClient is the transport the SDK and the raw GraphQL call use.
	// Nil uses http.DefaultClient.
	HTTPClient *http.Client
	// Token is the personal access token (needs the `api` scope). Empty
	// runs unauthenticated, which only works against public projects.
	Token string
	// LocalCheckout is an optional path to a local git checkout used as a
	// read-only fast path for blobs and range diffs. Never the source of
	// truth; empty disables the fast path.
	LocalCheckout string
	// Runner executes local git commands for the fast paths. Nil disables
	// them even when LocalCheckout is set.
	Runner vcs.Runner
}

// Driver is the GitLab implementation of forge.Forge.
type Driver struct {
	host          string
	client        *gitlab.Client
	httpClient    *http.Client
	token         string
	graphqlURL    string
	localCheckout string
	runner        vcs.Runner

	mu     sync.Mutex
	viewer string // cached CurrentUser login, "" until fetched
}

var _ forge.Forge = (*Driver)(nil)

// New builds a GitLab driver from opts.
func New(opts Options) (*Driver, error) {
	host := opts.Host
	if host == "" {
		host = defaultHost
	}
	apiBase := opts.APIBase
	if apiBase == "" {
		apiBase = "https://" + host + "/api/v4"
	}
	parsed, err := url.Parse(apiBase)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, forge.NewError(forgetypes.KindGitLab, "new", host,
			forge.ErrorValidation, fmt.Errorf("invalid api base %q", apiBase),
		).WithHint("api_base must be an absolute URL like https://HOST/api/v4")
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	client, err := gitlab.NewClient(opts.Token,
		gitlab.WithBaseURL(apiBase),
		gitlab.WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, forge.NewError(forgetypes.KindGitLab, "new", host,
			forge.ErrorValidation, err)
	}
	return &Driver{
		host:          host,
		client:        client,
		httpClient:    httpClient,
		token:         opts.Token,
		graphqlURL:    graphqlEndpoint(apiBase),
		localCheckout: opts.LocalCheckout,
		runner:        opts.Runner,
	}, nil
}

// graphqlEndpoint derives the GraphQL URL from a REST base: the standard
// https://HOST/api/v4 maps to https://HOST/api/graphql; any other base
// (fixture servers) gets /graphql appended.
func graphqlEndpoint(apiBase string) string {
	s := strings.TrimSuffix(apiBase, "/")
	if base, ok := strings.CutSuffix(s, "/api/v4"); ok {
		return base + "/api/graphql"
	}
	return s + "/graphql"
}

// ID returns the forge kind.
func (d *Driver) ID() forgetypes.Kind { return forgetypes.KindGitLab }

// Capabilities reports GitLab's capability set. Review summaries do not
// exist as a concept (review bodies post as plain MR notes and come back
// through discussions), outdated detection is not exposed, and submits are
// an N+1 sequence rather than one atomic request.
func (d *Driver) Capabilities() forge.Capabilities {
	return forge.Capabilities{
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
}

// LocalCheckoutPath returns the optional local checkout root, "" when none.
func (d *Driver) LocalCheckoutPath() string { return d.localCheckout }

// projectID returns the SDK project identifier for repo: the joined
// owner/name path (the owner may itself contain subgroup slashes). The SDK
// URL-encodes it as a single path segment.
func projectID(repo forgetypes.Repository) string {
	return repo.Owner + "/" + repo.Name
}

// forgePayload is the GitLab-private anchoring data round-tripped through
// PullRequestDetails.ForgePayload: the diff start SHA that discussion
// positions require.
type forgePayload struct {
	StartSHA string `json:"start_sha"`
}

// startSHA extracts the diff start SHA stashed in the PR's ForgePayload,
// falling back to the base SHA when absent (tuicr parity).
func startSHA(pr *forge.PullRequestDetails) string {
	if len(pr.ForgePayload) > 0 {
		var payload forgePayload
		if err := json.Unmarshal(pr.ForgePayload, &payload); err == nil && payload.StartSHA != "" {
			return payload.StartSHA
		}
	}
	return pr.BaseSHA
}

// normalizeState maps GitLab MR states onto the lowercase vocabulary the
// rest of the app uses (githubf reports "open"/"closed").
func normalizeState(state string) string {
	if strings.EqualFold(state, "opened") {
		return "open"
	}
	return strings.ToLower(state)
}
