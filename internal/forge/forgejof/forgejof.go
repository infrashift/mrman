// Package forgejof implements the Forgejo forge driver on top of the
// forgejo-sdk v3 REST client. It serves codeberg.org and on-prem Forgejo
// (or Gitea-compatible) hosts; the API base URL is derived from the host
// or overridden explicitly, which is also the seam driver tests use to
// point at fixture servers.
//
// The SDK is not context-native: a client binds one context via
// SetContext. The driver therefore builds a cheap per-request client
// clone for every call — construction is pure because SetForgejoVersion("")
// skips the SDK's version handshake — so every HTTP request honors the
// caller's context.
package forgejof

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	forgejo "codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/vcs"
)

// defaultHost is the flagship Forgejo SaaS host.
const defaultHost = "codeberg.org"

// Options configures a Forgejo driver.
type Options struct {
	// Host is "codeberg.org" (default when empty) or an on-prem Forgejo
	// hostname.
	Host string
	// APIBase overrides the base URL. Empty derives https://HOST. The SDK
	// appends /api/v1 itself, so the base is the bare origin; a trailing
	// /api/v1 is stripped defensively.
	APIBase string
	// HTTPClient carries TLS configuration for the host. Nil uses a plain
	// default client. Authentication rides on Token, not the client.
	HTTPClient *http.Client
	// Token is the Forgejo access token, sent as `Authorization: token …`
	// by the SDK and by the driver's raw calls. Empty runs
	// unauthenticated where the forge allows it.
	Token string
	// LocalCheckout is an optional path to a local git checkout used as a
	// read-only fast path for blobs and range diffs. Never the source of
	// truth; empty disables the fast path.
	LocalCheckout string
	// Runner executes local git commands for the fast paths. Nil disables
	// them even when LocalCheckout is set.
	Runner vcs.Runner
}

// Driver is the Forgejo implementation of forge.Forge.
type Driver struct {
	host          string
	base          string // scheme://host[:port], no trailing slash; SDK appends /api/v1
	httpClient    *http.Client
	token         string
	localCheckout string
	runner        vcs.Runner
}

var _ forge.Forge = (*Driver)(nil)

// New builds a Forgejo driver from opts. It performs no network I/O.
func New(opts Options) (*Driver, error) {
	host := opts.Host
	if host == "" {
		host = defaultHost
	}
	base := opts.APIBase
	if base == "" {
		base = "https://" + host
	}
	base = strings.TrimSuffix(base, "/")
	base = strings.TrimSuffix(base, "/api/v1")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, forge.NewError(forgetypes.KindForgejo, "new", host,
			forge.ErrorValidation, fmt.Errorf("invalid api base %q", opts.APIBase),
		).WithHint("api_base must be an absolute URL like https://HOST (the /api/v1 suffix is appended automatically)")
	}
	return &Driver{
		host:          host,
		base:          base,
		httpClient:    opts.HTTPClient,
		token:         opts.Token,
		localCheckout: opts.LocalCheckout,
		runner:        opts.Runner,
	}, nil
}

// api builds a per-request SDK client bound to ctx. SetForgejoVersion("")
// disables the SDK's server-version handshake so construction stays free
// of network I/O and version-gated SDK methods take their API code paths.
func (d *Driver) api(ctx context.Context) (*forgejo.Client, error) {
	options := []forgejo.ClientOption{
		forgejo.SetForgejoVersion(""),
		forgejo.SetContext(ctx),
	}
	if d.httpClient != nil {
		options = append(options, forgejo.SetHTTPClient(d.httpClient))
	}
	if d.token != "" {
		options = append(options, forgejo.SetToken(d.token))
	}
	return forgejo.NewClient(d.base, options...)
}

// ID returns the forge kind.
func (d *Driver) ID() forgetypes.Kind { return forgetypes.KindForgejo }

// Capabilities reports Forgejo's honest capability set. Threads are
// synthesized by grouping flat review comments, so resolution and outdated
// state are approximations (resolver presence, zeroed positions / commit
// mismatch) rather than faithful forge state. Inline comments carry a
// single old_position/new_position, so multi-line ranges are downgraded
// upstream, and there is no server-side commit-range diff API.
func (d *Driver) Capabilities() forge.Capabilities {
	return forge.Capabilities{
		DraftReviews:          true, // PENDING review state
		Approve:               true,
		RequestChanges:        true,
		ReviewSummaries:       true,  // review bodies
		ReviewThreads:         true,  // synthesized from grouped review comments
		ThreadResolution:      false, // approximated: any comment's resolver != nil
		ThreadOutdated:        false, // approximated: zero positions or commit mismatch
		MultiLineComments:     false, // single old_position/new_position only
		CommitRangeDiff:       false, // local-checkout fast path only
		ReviewRequestedFilter: true,  // issues-search review_requested=true
		AtomicSubmit:          true,  // one-shot CreatePullReview
		CommitScopedReviews:   true,  // reviews carry commit_id
	}
}

// LocalCheckoutPath returns the optional local checkout root, "" when none.
func (d *Driver) LocalCheckoutPath() string { return d.localCheckout }
