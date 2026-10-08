// Package azdof implements the Azure DevOps forge driver on top of
// microsoft/azure-devops-go-api (REST api-version 7.1). Azure DevOps is
// the most alien of the supported forges: it has no unified-diff endpoint
// (the driver synthesizes patches from blob pairs with an internal Myers
// diff), no draft reviews, votes instead of review events, and comment
// threads anchored through iteration contexts and change-tracking ids.
//
// The driver serves both SaaS (https://dev.azure.com/{org}) and Azure
// DevOps Server (collection URL) hosts; the organization/collection URL is
// also the seam driver tests use to point at fixture servers.
package azdof

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/vcs"
)

// Options configures an Azure DevOps driver.
type Options struct {
	// OrgURL is the organization base URL: https://dev.azure.com/{org} on
	// SaaS, or the collection URL on Azure DevOps Server (comes from the
	// host's api_base config). Required.
	OrgURL string
	// Project is the default project name used when a repository
	// coordinate does not carry one.
	Project string
	// Repo is the default repository name used when a repository
	// coordinate does not carry one.
	Repo string
	// HTTPClient overrides the transport (custom CA bundles, fixtures).
	// Nil uses a plain client.
	HTTPClient *http.Client
	// Token is the personal access token; the API uses basic auth with an
	// empty user. Empty means unauthenticated (almost nothing works).
	Token string
	// LocalCheckout is an optional path to a local git checkout used as a
	// read-only fast path for blobs and range diffs. Never the source of
	// truth; empty disables the fast path.
	LocalCheckout string
	// Runner executes local git commands for the fast paths. Nil disables
	// them even when LocalCheckout is set.
	Runner vcs.Runner
}

// Driver is the Azure DevOps implementation of forge.Forge.
type Driver struct {
	orgURL        string
	host          string
	project       string
	repo          string
	gitClient     git.Client
	core          *azuredevops.Client
	localCheckout string
	runner        vcs.Runner

	mu     sync.Mutex
	viewer *connectionUser // cached connectionData identity
}

var _ forge.Forge = (*Driver)(nil)

// New builds an Azure DevOps driver from opts.
func New(opts Options) (*Driver, error) {
	orgURL := strings.TrimRight(strings.TrimSpace(opts.OrgURL), "/")
	parsed, err := url.Parse(orgURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, forge.NewError(forgetypes.KindAzureDevOps, "new", parsed.Host,
			forge.ErrorValidation, fmt.Errorf("invalid organization URL %q", opts.OrgURL),
		).WithHint("OrgURL must be absolute, like https://dev.azure.com/my-org " +
			"(or the collection URL on Azure DevOps Server)")
	}
	connection := azuredevops.NewPatConnection(orgURL, opts.Token)
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	// Build the API client directly instead of via git.NewClient: that
	// constructor performs a network round-trip (resource-area discovery)
	// and offers no HTTP-client injection. The connection base URL is the
	// org/collection URL, which is exactly where the git area lives.
	core := azuredevops.NewClientWithOptions(connection, connection.BaseUrl,
		azuredevops.WithHTTPClient(httpClient))
	return &Driver{
		orgURL:        orgURL,
		host:          parsed.Host,
		project:       opts.Project,
		repo:          opts.Repo,
		gitClient:     &git.ClientImpl{Client: *core},
		core:          core,
		localCheckout: opts.LocalCheckout,
		runner:        opts.Runner,
	}, nil
}

// ID returns the forge kind.
func (d *Driver) ID() forgetypes.Kind { return forgetypes.KindAzureDevOps }

// Capabilities reports the honest Azure DevOps capability set.
//
//   - DraftReviews false: there is no server-side pending-review concept.
//   - ReviewSummaries false: ADO has no review object. ListReviewSummaries
//     returns its context-less threads (general discussions, review bodies
//     included), which the app fetches from every driver.
//   - ThreadOutdated false: outdated state is approximated (a thread with
//     no pull-request tracking context is assumed stale).
//   - CommitRangeDiff false: no server-side text diff exists at all; the
//     PR diff is synthesized from blob pairs and arbitrary commit ranges
//     are not supported.
//   - AtomicSubmit false: reviews post as an N+1 thread/vote sequence.
//   - CommitScopedReviews false: votes carry no commit OIDs.
func (d *Driver) Capabilities() forge.Capabilities {
	return forge.Capabilities{
		DraftReviews:          false,
		Approve:               true,
		RequestChanges:        true,
		ReviewSummaries:       false,
		ReviewThreads:         true,
		ThreadResolution:      true,
		ThreadOutdated:        false,
		MultiLineComments:     true,
		CommitRangeDiff:       false,
		ReviewRequestedFilter: true,
		AtomicSubmit:          false,
		CommitScopedReviews:   false,
	}
}

// LocalCheckoutPath returns the optional local checkout root, "" when none.
func (d *Driver) LocalCheckoutPath() string { return d.localCheckout }

// coords resolves the project and repository names for a repository
// coordinate, falling back to the driver defaults for empty fields.
func (d *Driver) coords(repo forgetypes.Repository) (project, repoName string) {
	project = repo.Project
	if project == "" {
		project = d.project
	}
	repoName = repo.Name
	if repoName == "" {
		repoName = d.repo
	}
	return project, repoName
}

// prWebURL builds the human-facing pull request URL.
func (d *Driver) prWebURL(project, repoName string, number uint64) string {
	return fmt.Sprintf("%s/%s/_git/%s/pullrequest/%d",
		d.orgURL, url.PathEscape(project), url.PathEscape(repoName), number)
}

// connectionUser is the slice of a connectionData identity the driver needs.
type connectionUser struct {
	ID                  string `json:"id"`
	ProviderDisplayName string `json:"providerDisplayName"`
	CustomDisplayName   string `json:"customDisplayName"`
}

// displayName returns the best human label for the identity.
func (u *connectionUser) displayName() string {
	if u.CustomDisplayName != "" {
		return u.CustomDisplayName
	}
	return u.ProviderDisplayName
}

// connectionDataAPIVersion is the api-version sent for the raw
// connectionData call. Azure DevOps serves that resource under preview
// versioning only, so the "-preview" suffix is mandatory.
const connectionDataAPIVersion = "7.1-preview"

// connectionData mirrors GET {org}/_apis/connectionData, which the SDK does
// not expose.
type connectionData struct {
	AuthenticatedUser *connectionUser `json:"authenticatedUser"`
	AuthorizedUser    *connectionUser `json:"authorizedUser"`
}

// viewerUser resolves the authenticated identity via a raw connectionData
// call (the SDK has no wrapper), cached for the driver's lifetime. The
// authorized user wins over the authenticated one when both are present.
func (d *Driver) viewerUser(ctx context.Context, op string) (*connectionUser, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.viewer != nil {
		return d.viewer, nil
	}
	requestURL := d.orgURL + "/_apis/connectionData"
	// connectionData is a preview-only resource: a plain "7.1" (or "7.0",
	// or "6.0") is rejected with VssInvalidPreviewVersionException and HTTP
	// 400, which surfaced as every approve/request-changes failing against
	// dev.azure.com. The suffix is required, not cosmetic.
	req, err := d.core.CreateRequestMessage(ctx, http.MethodGet, requestURL, connectionDataAPIVersion,
		nil, "", azuredevops.MediaTypeApplicationJson, nil)
	if err != nil {
		return nil, d.wrap(op, err)
	}
	resp, err := d.core.SendRequest(req) //nolint:bodyclose // closed by d.core.UnmarshalBody
	if err != nil {
		return nil, d.wrap(op, err)
	}
	var data connectionData
	if err := d.core.UnmarshalBody(resp, &data); err != nil {
		return nil, d.wrap(op, err)
	}
	user := data.AuthorizedUser
	if user == nil {
		user = data.AuthenticatedUser
	}
	if user == nil || user.ID == "" {
		return nil, d.err(op, forge.ErrorAuth, 0, hintAuth,
			fmt.Errorf("connectionData returned no authenticated user"))
	}
	d.viewer = user
	return user, nil
}
