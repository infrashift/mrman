package forge

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// Injectable seams for credential sources so auth tests never touch the
// real environment or spawn processes.
var (
	// lookupEnv reads an environment variable.
	lookupEnv = os.LookupEnv
	// runTokenCmd executes a config token_cmd via `sh -c` (`cmd /C` on
	// Windows) and returns the first stdout line.
	runTokenCmd = func(command string) (string, error) {
		shell, flag := "sh", "-c"
		if runtime.GOOS == "windows" {
			shell, flag = "cmd", "/C"
		}
		out, err := exec.Command(shell, flag, command).Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
				return "", fmt.Errorf("token_cmd %q failed: %s", command, firstLine(string(exitErr.Stderr)))
			}
			return "", fmt.Errorf("token_cmd %q failed: %w", command, err)
		}
		return firstLine(string(out)), nil
	}
	// runGhAuthToken borrows the GitHub CLI's stored token for a host.
	runGhAuthToken = func(host string) (string, error) {
		out, err := exec.Command("gh", "auth", "token", "--hostname", host).Output()
		if err != nil {
			return "", err
		}
		return firstLine(string(out)), nil
	}
)

// tokenCmdCache caches token_cmd results for the process lifetime so
// secret-manager commands run once, not per request.
var tokenCmdCache = struct {
	sync.Mutex
	results map[string]string
}{results: make(map[string]string)}

// resetTokenCmdCache clears the cache. Test hook.
func resetTokenCmdCache() {
	tokenCmdCache.Lock()
	defer tokenCmdCache.Unlock()
	tokenCmdCache.results = make(map[string]string)
}

// TokenForHost resolves the API token for a host, first hit wins:
//
//  1. Env conventions, scoped to each forge's SaaS host so environment
//     tokens never leak to on-prem hosts: github.com reads GITHUB_TOKEN
//     then GH_TOKEN, other github-kind hosts read GH_ENTERPRISE_TOKEN
//     (the GHE convention), gitlab.com reads GITLAB_TOKEN, dev.azure.com
//     reads AZURE_DEVOPS_EXT_PAT, codeberg.org reads FORGEJO_TOKEN then
//     CODEBERG_TOKEN.
//  2. The host's config token, with $VAR/${VAR} expansion; an empty
//     expansion counts as not set.
//  3. The host's config token_cmd, run via `sh -c` and cached for the
//     process lifetime; a failing command is a hard error since the user
//     explicitly configured it.
//  4. `gh auth token --hostname <host>` for github-kind hosts when
//     cfg.CLITokenFallback allows it; failures fall through.
//
// Before any of that, the host must be trusted (HostTrusted): a credential
// never goes to a host mrman merely guessed the forge kind for. A merge
// request URL on an arbitrary host parses as GitHub by shape, and a
// hostname containing "github" is assumed GitHub — neither is grounds to
// hand over GH_ENTERPRISE_TOKEN. Untrusted hosts get unauthenticated access
// and a warning naming the config entry that would change that.
//
// An empty result with nil error means unauthenticated access.
func TokenForHost(host string, kind forgetypes.Kind, cfg config.ForgeConfig) (string, error) {
	if !HostTrusted(host, cfg) {
		return "", nil
	}
	if token := envToken(host, kind); token != "" {
		return token, nil
	}

	if entry, ok := configHostEntry(host, cfg); ok {
		if token, set := expandConfigToken(entry.Token); set {
			return token, nil
		}
		if entry.TokenCmd != "" {
			token, err := cachedTokenCmd(entry.TokenCmd)
			if err != nil {
				return "", err
			}
			if token != "" {
				return token, nil
			}
		}
	}

	if kind == forgetypes.KindGitHub && cfg.CLITokenFallback {
		if token, err := runGhAuthToken(host); err == nil && token != "" {
			return token, nil
		}
	}
	return "", nil
}

// HostTrusted reports whether host may receive credentials: it is a
// built-in SaaS host, a registered driver's default host, or the user
// listed it under [[forge.hosts]]. Kind detection is deliberately not
// enough — heuristics decide how to talk to a host, never whether to
// trust it with a token.
func HostTrusted(host string, cfg config.ForgeConfig) bool {
	if _, ok := configHostEntry(host, cfg); ok {
		return true
	}
	if _, ok := builtinKindForHost(strings.ToLower(host)); ok {
		return true
	}
	for _, d := range registry {
		for _, dh := range d.DefaultHosts {
			if strings.EqualFold(dh, host) {
				return true
			}
		}
	}
	return false
}

// UntrustedHostWarning is the one-line notice shown when a host runs
// unauthenticated because nothing vouches for it. It names the remedy.
func UntrustedHostWarning(host string) string {
	return fmt.Sprintf("%s is not in [[forge.hosts]]: connecting without credentials; add a host entry to authenticate", host)
}

// envToken applies the SaaS-scoped environment conventions.
func envToken(host string, kind forgetypes.Kind) string {
	switch strings.ToLower(host) {
	case "github.com":
		return firstEnv("GITHUB_TOKEN", "GH_TOKEN")
	case "gitlab.com":
		return firstEnv("GITLAB_TOKEN")
	case "dev.azure.com":
		return firstEnv("AZURE_DEVOPS_EXT_PAT")
	case "codeberg.org":
		return firstEnv("FORGEJO_TOKEN", "CODEBERG_TOKEN")
	}
	if strings.HasSuffix(strings.ToLower(host), ".visualstudio.com") {
		// Legacy Azure DevOps Services URLs: the same SaaS, the same PAT.
		return firstEnv("AZURE_DEVOPS_EXT_PAT")
	}
	if kind == forgetypes.KindGitHub {
		return firstEnv("GH_ENTERPRISE_TOKEN")
	}
	return ""
}

// firstEnv returns the first non-empty environment variable value.
func firstEnv(names ...string) string {
	for _, name := range names {
		if value, ok := lookupEnv(name); ok {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// configHostEntry finds the [[forge.hosts]] entry for host, matching
// case-insensitively.
func configHostEntry(host string, cfg config.ForgeConfig) (config.ForgeHost, bool) {
	for _, entry := range cfg.Hosts {
		if strings.EqualFold(entry.Host, host) {
			return entry, true
		}
	}
	return config.ForgeHost{}, false
}

// expandConfigToken resolves a config token value: "$VAR"/"${VAR}" expands
// from the environment (empty expansion = not set), anything else is a
// literal. set is false when no usable token results.
func expandConfigToken(raw string) (token string, set bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if !strings.HasPrefix(raw, "$") {
		return raw, true
	}
	name := strings.TrimPrefix(raw, "$")
	name = strings.TrimPrefix(name, "{")
	name = strings.TrimSuffix(name, "}")
	if name == "" {
		return "", false
	}
	value, _ := lookupEnv(name)
	value = strings.TrimSpace(value)
	return value, value != ""
}

// cachedTokenCmd runs a token_cmd once per process and caches its output.
func cachedTokenCmd(command string) (string, error) {
	tokenCmdCache.Lock()
	defer tokenCmdCache.Unlock()
	if token, ok := tokenCmdCache.results[command]; ok {
		return token, nil
	}
	token, err := runTokenCmd(command)
	if err != nil {
		return "", err
	}
	tokenCmdCache.results[command] = token
	return token, nil
}

// requestTimeout bounds every forge request, body included. Forge calls
// run on the TUI's async path, where a hung server would otherwise leave a
// spinner up forever with no way to cancel short of quitting.
const requestTimeout = 90 * time.Second

// maxRedirects mirrors net/http's own default ceiling.
const maxRedirects = 10

// RedirectRefused is returned when a forge answers with a redirect to a
// different origin. Following it would carry the host's credentials along:
// the oauth2 transport re-attaches the bearer token on every hop, and the
// GitLab, Forgejo and Azure DevOps SDKs set their auth headers per request.
type RedirectRefused struct {
	From string
	To   string
}

func (e *RedirectRefused) Error() string {
	return fmt.Sprintf("refusing redirect from %s to %s: credentials stay on the configured host", e.From, e.To)
}

// BuildHTTPClient builds the *http.Client injected into every forge SDK,
// honoring the host's TLS overrides: CAFile appends a PEM bundle to the
// system pool, InsecureSkipVerify disables verification (last resort).
//
// Every client refuses cross-origin redirects (see RedirectRefused) and
// carries a request timeout. The oauth2 wrapper the GitHub driver applies
// copies CheckRedirect from this client, and the other SDKs call Do on it
// directly, so this is the one place the policy lives.
func BuildHTTPClient(hc HostConfig) (*http.Client, error) {
	client := &http.Client{
		Timeout:       requestTimeout,
		CheckRedirect: pinnedRedirectPolicy(allowedOrigin(hc)),
	}
	if hc.CAFile == "" && !hc.InsecureSkipVerify {
		return client, nil
	}

	tlsConfig := &tls.Config{InsecureSkipVerify: hc.InsecureSkipVerify} //nolint:gosec // explicit per-host opt-in
	if hc.CAFile != "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		pemBytes, err := os.ReadFile(hc.CAFile)
		if err != nil {
			return nil, fmt.Errorf("forge host %s: reading ca_file: %w", hc.Host, err)
		}
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("forge host %s: ca_file %s contains no valid PEM certificates", hc.Host, hc.CAFile)
		}
		tlsConfig.RootCAs = pool
	}

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		transport = &http.Transport{}
	} else {
		transport = transport.Clone()
	}
	transport.TLSClientConfig = tlsConfig
	client.Transport = transport
	return client, nil
}

// allowedOrigin is the scheme://host[:port] a host's requests may land on:
// api_base when configured, else https on the host itself.
func allowedOrigin(hc HostConfig) *url.URL {
	if hc.APIBase != "" {
		if u, err := url.Parse(hc.APIBase); err == nil && u.Host != "" {
			return u
		}
	}
	return &url.URL{Scheme: "https", Host: hc.Host}
}

// pinnedRedirectPolicy follows redirects only within the allowed origin,
// and never from https down to http.
func pinnedRedirectPolicy(allowed *url.URL) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if !sameOrigin(req.URL, allowed) {
			return &RedirectRefused{From: via[0].URL.Host, To: req.URL.Host}
		}
		return nil
	}
}

// sameOrigin compares host and port case-insensitively, defaulting the
// port from the scheme, and treats an https→http downgrade as a change of
// origin regardless of host.
func sameOrigin(u, allowed *url.URL) bool {
	if strings.EqualFold(allowed.Scheme, "https") && !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	return strings.EqualFold(u.Hostname(), allowed.Hostname()) &&
		portOf(u) == portOf(allowed)
}

func portOf(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return "443"
}
