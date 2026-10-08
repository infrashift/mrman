package forge

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// HostConfig is the fully-resolved per-host configuration a driver is
// constructed with, combining routing, the resolved token, and TLS
// settings.
type HostConfig struct {
	// Host is the forge hostname.
	Host string
	// Kind names the driver serving this host.
	Kind forgetypes.Kind
	// APIBase overrides the API base URL; "" derives the default from
	// Host and Kind inside the driver.
	APIBase string
	// Token is the resolved API token, "" for unauthenticated access.
	Token string
	// CAFile is a path to an extra PEM CA bundle for this host.
	CAFile string
	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool
	// Warning is a notice for the user about how this host was resolved
	// — set when the host is untrusted and therefore unauthenticated. ""
	// when there is nothing to say.
	Warning string
}

// Driver describes one registered forge driver.
type Driver struct {
	// ID is the forge kind this driver serves.
	ID forgetypes.Kind
	// SlugPrefix is the short prefix used in PR slugs ("gh", "gl", ...).
	SlugPrefix string
	// DefaultHosts lists additional built-in host claims beyond the
	// registry's own SaaS host table.
	DefaultHosts []string
	// New constructs the driver for a resolved host.
	New func(cfg HostConfig) (Forge, error)
	// NewForRepo constructs a repository-scoped driver; set for forges
	// whose API clients bind to a repository coordinate at construction
	// (Azure DevOps). Preferred over New when non-nil.
	NewForRepo func(cfg HostConfig, repo forgetypes.Repository) (Forge, error)
	// ParseRepoPath interprets pre-split (host, path segments) from a
	// remote URL as a repository, overriding the registry's built-in
	// interpretation when set.
	ParseRepoPath func(host string, segments []string) (*forgetypes.Repository, bool)
	// ParseTargetURL claims a full PR web URL, overriding the built-in
	// URL shapes when set. Host resolution may still override the
	// resulting kind (config wins).
	ParseTargetURL func(u *url.URL) (*Target, bool)
}

// registry is the ordered driver list. Registration happens at startup
// from driver package wiring; no locking, mutation after init is a bug.
var registry []Driver

// Register adds a driver to the registry, replacing any driver already
// registered under the same ID and preserving registration order
// otherwise.
func Register(d Driver) {
	for i := range registry {
		if registry[i].ID == d.ID {
			registry[i] = d
			return
		}
	}
	registry = append(registry, d)
}

// Drivers returns the registered drivers in registration order.
func Drivers() []Driver {
	out := make([]Driver, len(registry))
	copy(out, registry)
	return out
}

// DriverFor finds the registered driver for a forge kind.
func DriverFor(kind forgetypes.Kind) (Driver, bool) {
	for _, d := range registry {
		if d.ID == kind {
			return d, true
		}
	}
	return Driver{}, false
}

// ResolveHostConfig assembles the HostConfig for a host: TLS and API-base
// overrides from the matching [[forge.hosts]] entry plus the token
// resolved through TokenForHost.
func ResolveHostConfig(host string, kind forgetypes.Kind, cfg config.ForgeConfig) (HostConfig, error) {
	hc := HostConfig{Host: host, Kind: kind}
	if entry, ok := configHostEntry(host, cfg); ok {
		hc.APIBase = entry.APIBase
		hc.CAFile = entry.CAFile
		hc.InsecureSkipVerify = entry.InsecureSkipVerify
	}
	if !HostTrusted(host, cfg) {
		// No token lookup at all: not even a token_cmd runs for a host
		// nothing vouches for.
		hc.Warning = UntrustedHostWarning(host)
		return hc, nil
	}
	token, err := TokenForHost(host, kind, cfg)
	if err != nil {
		return hc, err
	}
	hc.Token = token
	return hc, nil
}

// ForRepository constructs the driver serving a repository's host.
func ForRepository(repo forgetypes.Repository, cfg config.ForgeConfig) (Forge, error) {
	d, ok := DriverFor(repo.Kind)
	if !ok {
		return nil, fmt.Errorf("no forge driver registered for kind %q", repo.Kind)
	}
	hc, err := ResolveHostConfig(repo.Host, repo.Kind, cfg)
	if err != nil {
		return nil, err
	}
	var f Forge
	if d.NewForRepo != nil {
		f, err = d.NewForRepo(hc, repo)
	} else {
		f, err = d.New(hc)
	}
	if err != nil {
		return nil, err
	}
	return Sanitized(f), nil
}

// kindFromConfigName maps a config forge name to a Kind, accepting the
// documented names plus close aliases.
func kindFromConfigName(name string) (forgetypes.Kind, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "github":
		return forgetypes.KindGitHub, true
	case "gitlab":
		return forgetypes.KindGitLab, true
	case "azuredevops", "azure_devops", "ado":
		return forgetypes.KindAzureDevOps, true
	case "forgejo", "gitea":
		return forgetypes.KindForgejo, true
	}
	return "", false
}

// defaultKind resolves the configured default forge, falling back to
// GitHub when the config value is missing or unknown.
func defaultKind(cfg config.ForgeConfig) forgetypes.Kind {
	if kind, ok := kindFromConfigName(cfg.Default); ok {
		return kind
	}
	return forgetypes.KindGitHub
}

// DefaultSaaSHost returns the forge kind's default SaaS host, used when a
// coordinate target names no host.
func DefaultSaaSHost(kind forgetypes.Kind) string {
	switch kind {
	case forgetypes.KindGitHub:
		return "github.com"
	case forgetypes.KindGitLab:
		return "gitlab.com"
	case forgetypes.KindAzureDevOps:
		return "dev.azure.com"
	case forgetypes.KindForgejo:
		return "codeberg.org"
	}
	return ""
}

// legacyADOSSHHost is the SSH transport host for {org}.visualstudio.com
// remotes; the organization is the first path segment after "v3".
const legacyADOSSHHost = "vs-ssh.visualstudio.com"

// builtinKindForHost claims the well-known SaaS hosts.
func builtinKindForHost(host string) (forgetypes.Kind, bool) {
	switch host {
	case "github.com":
		return forgetypes.KindGitHub, true
	case "gitlab.com":
		return forgetypes.KindGitLab, true
	case "dev.azure.com", "ssh.dev.azure.com":
		return forgetypes.KindAzureDevOps, true
	case "codeberg.org":
		return forgetypes.KindForgejo, true
	}
	if strings.HasSuffix(host, ".visualstudio.com") {
		return forgetypes.KindAzureDevOps, true
	}
	return "", false
}

// ResolveHostKind resolves which forge serves a host, in precedence order:
// an exact [[forge.hosts]] config entry, the built-in SaaS host table plus
// registered drivers' DefaultHosts, then hostname heuristics (substrings
// "github", "gitlab", "gitea"/"forgejo"). known is false for hosts nothing
// claims; callers decide between the configured default forge (for
// structurally-parseable paths) and an actionable error.
func ResolveHostKind(host string, cfg config.ForgeConfig) (kind forgetypes.Kind, known bool) {
	lower := strings.ToLower(host)
	for _, entry := range cfg.Hosts {
		if strings.EqualFold(entry.Host, host) {
			if k, ok := kindFromConfigName(entry.Forge); ok {
				return k, true
			}
		}
	}
	if k, ok := builtinKindForHost(lower); ok {
		return k, true
	}
	for _, d := range registry {
		for _, dh := range d.DefaultHosts {
			if strings.EqualFold(dh, host) {
				return d.ID, true
			}
		}
	}
	switch {
	case strings.Contains(lower, "github"):
		return forgetypes.KindGitHub, true
	case strings.Contains(lower, "gitlab"):
		return forgetypes.KindGitLab, true
	case strings.Contains(lower, "gitea"), strings.Contains(lower, "forgejo"):
		return forgetypes.KindForgejo, true
	}
	return "", false
}

// unknownHostError is the actionable detection failure, embedding a
// ready-to-paste config snippet.
func unknownHostError(host string) error {
	return fmt.Errorf(`cannot determine the forge for host %q — add it to your config:

[[forge.hosts]]
host  = %q
forge = "github"  # one of: github, gitlab, azuredevops, forgejo`, host, host)
}

// repoFromSegments interprets pre-split path segments as a repository of
// the given kind, delegating to a registered driver's ParseRepoPath when
// one exists.
func repoFromSegments(kind forgetypes.Kind, host string, segments []string) (*forgetypes.Repository, bool) {
	if len(segments) == 0 {
		return nil, false
	}
	if d, ok := DriverFor(kind); ok && d.ParseRepoPath != nil {
		return d.ParseRepoPath(host, segments)
	}
	switch kind {
	case forgetypes.KindGitHub, forgetypes.KindForgejo:
		if len(segments) == 2 {
			return &forgetypes.Repository{Kind: kind, Host: host, Owner: segments[0], Name: segments[1]}, true
		}
	case forgetypes.KindGitLab:
		if len(segments) >= 2 {
			return &forgetypes.Repository{
				Kind:  kind,
				Host:  host,
				Owner: strings.Join(segments[:len(segments)-1], "/"),
				Name:  segments[len(segments)-1],
			}, true
		}
	case forgetypes.KindAzureDevOps:
		return adoRepoFromSegments(host, segments)
	}
	return nil, false
}

// adoRepoFromSegments interprets Azure DevOps remote path shapes:
// dev.azure.com/{org}/{project}/_git/{repo}, the v3 SSH form
// {v3}/{org}/{project}/{repo}, and legacy
// {org}.visualstudio.com/[DefaultCollection/]{project}/_git/{repo}, whose
// SSH form is vs-ssh.visualstudio.com:v3/{org}/{project}/{repo}.
func adoRepoFromSegments(host string, segments []string) (*forgetypes.Repository, bool) {
	if host == legacyADOSSHHost {
		if len(segments) == 4 && segments[0] == "v3" {
			return &forgetypes.Repository{
				Kind: forgetypes.KindAzureDevOps, Host: segments[1] + ".visualstudio.com",
				Owner: segments[1], Project: segments[2], Name: segments[3],
			}, true
		}
		return nil, false
	}
	if org, ok := strings.CutSuffix(host, ".visualstudio.com"); ok {
		if len(segments) > 0 && strings.EqualFold(segments[0], "DefaultCollection") {
			segments = segments[1:]
		}
		if len(segments) == 3 && segments[1] == "_git" {
			return &forgetypes.Repository{
				Kind: forgetypes.KindAzureDevOps, Host: host,
				Owner: org, Project: segments[0], Name: segments[2],
			}, true
		}
		return nil, false
	}
	if len(segments) == 4 && segments[0] == "v3" {
		return &forgetypes.Repository{
			Kind: forgetypes.KindAzureDevOps, Host: host,
			Owner: segments[1], Project: segments[2], Name: segments[3],
		}, true
	}
	if len(segments) == 4 && segments[2] == "_git" {
		return &forgetypes.Repository{
			Kind: forgetypes.KindAzureDevOps, Host: host,
			Owner: segments[0], Project: segments[1], Name: segments[3],
		}, true
	}
	return nil, false
}

// ResolveRepository detects the forge repository from a checkout's remote
// URLs (origin first). Unknown hosts whose paths still parse under the
// configured default forge resolve to it; hosts nothing claims produce an
// actionable error embedding a [[forge.hosts]] snippet.
func ResolveRepository(remoteURLs []string, cfg config.ForgeConfig) (*forgetypes.Repository, error) {
	var unknownErr error
	for _, raw := range remoteURLs {
		host, segments, ok := SplitRemoteURL(raw)
		if !ok {
			continue
		}
		kind, known := ResolveHostKind(host, cfg)
		if known {
			if repo, parsed := repoFromSegments(kind, host, segments); parsed {
				return repo, nil
			}
			continue
		}
		if repo, parsed := repoFromSegments(defaultKind(cfg), host, segments); parsed {
			return repo, nil
		}
		if unknownErr == nil {
			unknownErr = unknownHostError(host)
		}
	}
	if unknownErr != nil {
		return nil, unknownErr
	}
	return nil, errs.ErrNotARepository
}

// ParseTarget parses a `mrman pr <target>` argument: a bare PR number
// (requires a detected checkout repository), an owner/repo#N or
// owner/repo!N coordinate (optionally host-qualified), or a full PR web
// URL for any supported forge. Host resolution can override a URL-shape
// guess — config wins over the shape.
func ParseTarget(s string, checkoutRepo *forgetypes.Repository, cfg config.ForgeConfig) (*Target, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil, &errs.InvalidInput{Detail: "empty merge request target"}
	}

	if isAllDigits(trimmed) {
		number, ok := parsePRNumber(trimmed)
		if !ok {
			return nil, malformedTarget(s)
		}
		if checkoutRepo == nil {
			return nil, &errs.InvalidInput{Detail: fmt.Sprintf(
				"bare MR number %q requires a repository checkout; run inside a checkout, or pass owner/repo#%s or a full URL (unrecognized hosts fall back to default_forge)",
				trimmed, trimmed)}
		}
		repo := *checkoutRepo
		return &Target{Repository: &repo, Number: number, Original: trimmed}, nil
	}

	if strings.Contains(trimmed, "://") {
		u, err := url.Parse(trimmed)
		if err != nil {
			return nil, malformedTarget(s)
		}
		for _, d := range registry {
			if d.ParseTargetURL == nil {
				continue
			}
			if target, ok := d.ParseTargetURL(u); ok && target != nil {
				finalizeTarget(target, trimmed, cfg)
				return target, nil
			}
		}
		if target, ok := parseBuiltinTargetURL(u, trimmed); ok {
			finalizeTarget(target, trimmed, cfg)
			return target, nil
		}
		return nil, malformedTarget(s)
	}

	if target, ok := parseCoordinateTarget(trimmed, checkoutRepo, cfg); ok {
		return target, nil
	}
	return nil, malformedTarget(s)
}

func malformedTarget(input string) error {
	return &errs.InvalidInput{Detail: fmt.Sprintf(
		"cannot parse merge request target %q; expected an MR number, owner/repo#N, or a merge request URL", input)}
}

// finalizeTarget applies the host-over-shape override and preserves the
// user's input.
func finalizeTarget(target *Target, original string, cfg config.ForgeConfig) {
	if target.Original == "" {
		target.Original = original
	}
	if target.Repository == nil {
		return
	}
	if kind, known := ResolveHostKind(target.Repository.Host, cfg); known {
		target.Repository.Kind = kind
	}
}

// parseBuiltinTargetURL recognizes the four forges' PR web URL shapes:
// GitHub /owner/repo/pull/N, Forgejo /owner/repo/pulls/N, GitLab
// /group[/sub...]/repo/-/merge_requests/N, and Azure DevOps
// /{org}/{project}/_git/{repo}/pullrequest/N (plus the legacy
// {org}.visualstudio.com form).
func parseBuiltinTargetURL(u *url.URL, original string) (*Target, bool) {
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, false
	}
	host := normalizeTransportHost(strings.ToLower(u.Hostname()))
	if host == "" {
		return nil, false
	}
	segments := nonEmptySegments(u.Path)

	// GitHub /pull/N and Forgejo /pulls/N, possibly copied from a tab.
	if len(segments) >= 4 && (segments[2] == "pull" || segments[2] == "pulls") &&
		isPRTabSuffix(segments[4:], prTabsGitHub) {
		number, ok := parsePRNumber(segments[3])
		if !ok {
			return nil, false
		}
		kind := forgetypes.KindGitHub
		if segments[2] == "pulls" {
			kind = forgetypes.KindForgejo
		}
		return &Target{
			Repository: &forgetypes.Repository{Kind: kind, Host: host, Owner: segments[0], Name: stripGitSuffix(segments[1])},
			Number:     number,
			Original:   original,
		}, true
	}

	// GitLab /group[/sub...]/repo/-/merge_requests/N, possibly copied from
	// a tab.
	for i := 2; i+2 < len(segments); i++ {
		if segments[i] != "-" || segments[i+1] != "merge_requests" || !isPRTabSuffix(segments[i+3:], prTabsGitLab) {
			continue
		}
		number, ok := parsePRNumber(segments[i+2])
		if !ok {
			return nil, false
		}
		return &Target{
			Repository: &forgetypes.Repository{
				Kind:  forgetypes.KindGitLab,
				Host:  host,
				Owner: strings.Join(segments[:i-1], "/"),
				Name:  stripGitSuffix(segments[i-1]),
			},
			Number:   number,
			Original: original,
		}, true
	}

	// Azure DevOps pullrequest shapes.
	if target, ok := parseADOTargetURL(host, segments, original); ok {
		return target, true
	}
	return nil, false
}

// PR page tabs whose URLs extend the PR URL by one segment (and, for a
// commit in the commits tab, its SHA). Azure DevOps selects tabs with a
// query parameter, which never reaches the path.
var (
	prTabsGitHub = map[string]bool{"files": true, "commits": true, "checks": true, "changes": true}
	prTabsGitLab = map[string]bool{"diffs": true, "commits": true, "pipelines": true}
)

// isPRTabSuffix reports whether the segments after a PR number are empty or
// a known tab, optionally followed by one more segment (a commit SHA).
func isPRTabSuffix(rest []string, tabs map[string]bool) bool {
	switch len(rest) {
	case 0:
		return true
	case 1, 2:
		return tabs[rest[0]]
	}
	return false
}

// parseADOTargetURL recognizes dev.azure.com and visualstudio.com PR URLs.
func parseADOTargetURL(host string, segments []string, original string) (*Target, bool) {
	if org, isLegacy := strings.CutSuffix(host, ".visualstudio.com"); isLegacy {
		if len(segments) > 0 && strings.EqualFold(segments[0], "DefaultCollection") {
			segments = segments[1:]
		}
		if len(segments) != 5 || segments[1] != "_git" || segments[3] != "pullrequest" {
			return nil, false
		}
		number, ok := parsePRNumber(segments[4])
		if !ok {
			return nil, false
		}
		return &Target{
			Repository: &forgetypes.Repository{
				Kind: forgetypes.KindAzureDevOps, Host: host,
				Owner: org, Project: segments[0], Name: segments[2],
			},
			Number:   number,
			Original: original,
		}, true
	}
	if len(segments) != 6 || segments[2] != "_git" || segments[4] != "pullrequest" {
		return nil, false
	}
	number, ok := parsePRNumber(segments[5])
	if !ok {
		return nil, false
	}
	return &Target{
		Repository: &forgetypes.Repository{
			Kind: forgetypes.KindAzureDevOps, Host: host,
			Owner: segments[0], Project: segments[1], Name: segments[3],
		},
		Number:   number,
		Original: original,
	}, true
}

// parseCoordinateTarget parses owner/repo#N (or !N) coordinates, optionally
// host-qualified: host/owner/repo#N, host/group/sub/repo#N (GitLab), or
// host/org/project/repo#N (Azure DevOps). Two-segment coordinates take the
// checkout's forge and host when one is detected, else the default forge's
// SaaS host; for an Azure DevOps checkout the two segments read as
// project/repo within the checkout's organization.
func parseCoordinateTarget(s string, checkoutRepo *forgetypes.Repository, cfg config.ForgeConfig) (*Target, bool) {
	sep := strings.IndexAny(s, "#!")
	if sep < 0 {
		return nil, false
	}
	number, ok := parsePRNumber(s[sep+1:])
	if !ok {
		return nil, false
	}
	segments := nonEmptySegments(s[:sep])

	var repo *forgetypes.Repository
	switch {
	case len(segments) == 2:
		owner, name := segments[0], stripGitSuffix(segments[1])
		if checkoutRepo != nil {
			repo = &forgetypes.Repository{Kind: checkoutRepo.Kind, Host: checkoutRepo.Host, Owner: owner, Name: name}
			if checkoutRepo.Kind == forgetypes.KindAzureDevOps {
				repo.Owner = checkoutRepo.Owner
				repo.Project = owner
			}
		} else {
			kind := defaultKind(cfg)
			repo = &forgetypes.Repository{Kind: kind, Host: DefaultSaaSHost(kind), Owner: owner, Name: name}
		}
	case len(segments) >= 3 && !looksLikeHost(segments[0], cfg):
		// group/sub/repo or org/project/repo with no host: the checkout's
		// forge and host, else the default forge's SaaS host. Only GitLab
		// (subgroups) and Azure DevOps (org/project/repo) have such paths.
		kind, host := defaultKind(cfg), DefaultSaaSHost(defaultKind(cfg))
		if checkoutRepo != nil {
			kind, host = checkoutRepo.Kind, checkoutRepo.Host
		}
		segments[len(segments)-1] = stripGitSuffix(segments[len(segments)-1])
		switch {
		case kind == forgetypes.KindGitLab:
			repo = &forgetypes.Repository{Kind: kind, Host: host,
				Owner: strings.Join(segments[:len(segments)-1], "/"), Name: segments[len(segments)-1]}
		case kind == forgetypes.KindAzureDevOps && len(segments) == 3:
			repo = &forgetypes.Repository{Kind: kind, Host: host,
				Owner: segments[0], Project: segments[1], Name: segments[2]}
		default:
			return nil, false
		}
	case len(segments) >= 3:
		host := strings.ToLower(segments[0])
		kind, known := ResolveHostKind(host, cfg)
		if !known {
			kind = defaultKind(cfg)
		}
		rest := segments[1:]
		rest[len(rest)-1] = stripGitSuffix(rest[len(rest)-1])
		switch {
		case kind == forgetypes.KindAzureDevOps && len(rest) == 3:
			repo = &forgetypes.Repository{Kind: kind, Host: host, Owner: rest[0], Project: rest[1], Name: rest[2]}
		case kind == forgetypes.KindGitLab && len(rest) >= 2:
			repo = &forgetypes.Repository{Kind: kind, Host: host, Owner: strings.Join(rest[:len(rest)-1], "/"), Name: rest[len(rest)-1]}
		case kind != forgetypes.KindAzureDevOps && len(rest) == 2:
			repo = &forgetypes.Repository{Kind: kind, Host: host, Owner: rest[0], Name: rest[1]}
		default:
			return nil, false
		}
	default:
		return nil, false
	}

	return &Target{Repository: repo, Number: number, Original: s}, true
}

// looksLikeHost reports whether a coordinate's first segment names a host
// rather than a GitLab group or Azure DevOps organization: it has a dot or
// a port, is localhost, or is a configured host. So
// "infrashift-group/sub/repo!5" reads as a group path. A GitLab group whose
// path contains a dot still reads as a host, and needs the host spelled out
// ("gitlab.com/my.group/repo!5").
func looksLikeHost(segment string, cfg config.ForgeConfig) bool {
	host := strings.ToLower(segment)
	if strings.ContainsAny(host, ".:") || host == "localhost" {
		return true
	}
	for _, entry := range cfg.Hosts {
		if strings.EqualFold(entry.Host, host) {
			return true
		}
	}
	return false
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// parsePRNumber parses a positive PR number.
func parsePRNumber(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if !isAllDigits(s) || len(s) > 18 {
		return 0, false
	}
	var n uint64
	for _, r := range s {
		n = n*10 + uint64(r-'0') //nolint:gosec // G115: r is a decimal digit
	}
	return n, n > 0
}
