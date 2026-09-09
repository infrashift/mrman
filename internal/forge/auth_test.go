package forge

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// authSeams installs fake env/exec seams and returns call counters.
type authSeams struct {
	env         map[string]string
	tokenCmdOut map[string]string
	tokenCmdErr error
	ghToken     string
	ghErr       error

	tokenCmdCalls int
	ghCalls       int
}

func installAuthSeams(t *testing.T, s *authSeams) {
	t.Helper()
	prevEnv, prevCmd, prevGh := lookupEnv, runTokenCmd, runGhAuthToken
	lookupEnv = func(name string) (string, bool) {
		v, ok := s.env[name]
		return v, ok
	}
	runTokenCmd = func(command string) (string, error) {
		s.tokenCmdCalls++
		if s.tokenCmdErr != nil {
			return "", s.tokenCmdErr
		}
		return s.tokenCmdOut[command], nil
	}
	runGhAuthToken = func(string) (string, error) {
		s.ghCalls++
		return s.ghToken, s.ghErr
	}
	resetTokenCmdCache()
	t.Cleanup(func() {
		lookupEnv, runTokenCmd, runGhAuthToken = prevEnv, prevCmd, prevGh
		resetTokenCmdCache()
	})
}

func TestTokenEnvConventionsForSaaSHosts(t *testing.T) {
	cases := []struct {
		name string
		host string
		kind forgetypes.Kind
		env  map[string]string
		want string
	}{
		{"github.com GITHUB_TOKEN", "github.com", forgetypes.KindGitHub,
			map[string]string{"GITHUB_TOKEN": "a", "GH_TOKEN": "b"}, "a"},
		{"github.com GH_TOKEN fallback", "github.com", forgetypes.KindGitHub,
			map[string]string{"GH_TOKEN": "b"}, "b"},
		{"listed ghe host GH_ENTERPRISE_TOKEN", "ghe.corp.example", forgetypes.KindGitHub,
			map[string]string{"GH_ENTERPRISE_TOKEN": "e", "GITHUB_TOKEN": "a"}, "e"},
		{"unlisted ghe host gets nothing", "ghe.unlisted.example", forgetypes.KindGitHub,
			map[string]string{"GH_ENTERPRISE_TOKEN": "e", "GITHUB_TOKEN": "a"}, ""},
		{"gitlab.com GITLAB_TOKEN", "gitlab.com", forgetypes.KindGitLab,
			map[string]string{"GITLAB_TOKEN": "g"}, "g"},
		{"dev.azure.com AZURE_DEVOPS_EXT_PAT", "dev.azure.com", forgetypes.KindAzureDevOps,
			map[string]string{"AZURE_DEVOPS_EXT_PAT": "p"}, "p"},
		{"codeberg.org FORGEJO_TOKEN", "codeberg.org", forgetypes.KindForgejo,
			map[string]string{"FORGEJO_TOKEN": "f", "CODEBERG_TOKEN": "c"}, "f"},
		{"codeberg.org CODEBERG_TOKEN fallback", "codeberg.org", forgetypes.KindForgejo,
			map[string]string{"CODEBERG_TOKEN": "c"}, "c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seams := &authSeams{env: tc.env}
			installAuthSeams(t, seams)
			cfg := defaultForgeConfig()
			cfg.CLITokenFallback = false
			cfg.Hosts = []config.ForgeHost{{Host: "ghe.corp.example", Forge: "github"}}
			got, err := TokenForHost(tc.host, tc.kind, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("token = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTokenEnvNeverLeaksToOnPremHosts(t *testing.T) {
	seams := &authSeams{env: map[string]string{
		"GITLAB_TOKEN":         "gl",
		"FORGEJO_TOKEN":        "fj",
		"CODEBERG_TOKEN":       "cb",
		"AZURE_DEVOPS_EXT_PAT": "ado",
		"GITHUB_TOKEN":         "gh",
		"GH_ENTERPRISE_TOKEN":  "ghe",
	}}
	installAuthSeams(t, seams)
	cfg := defaultForgeConfig()
	cfg.CLITokenFallback = false
	for _, tc := range []struct {
		host string
		kind forgetypes.Kind
	}{
		{"gitlab.corp.example", forgetypes.KindGitLab},
		{"forgejo.corp.example", forgetypes.KindForgejo},
		{"ado.corp.example", forgetypes.KindAzureDevOps},
		// A /pull/N URL on any host parses as GitHub by shape; that shape
		// must not be enough to send GH_ENTERPRISE_TOKEN there.
		{"evil.example", forgetypes.KindGitHub},
	} {
		got, err := TokenForHost(tc.host, tc.kind, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Errorf("env token leaked to %s: %q", tc.host, got)
		}
	}
}

func TestTokenConfigLiteralAndVarExpansion(t *testing.T) {
	seams := &authSeams{env: map[string]string{"CORP_TOKEN": "expanded", "EMPTY": ""}}
	installAuthSeams(t, seams)
	cfg := defaultForgeConfig()
	cfg.CLITokenFallback = false
	cfg.Hosts = []config.ForgeHost{
		{Host: "literal.example", Forge: "gitlab", Token: "literal-tok"},
		{Host: "dollar.example", Forge: "gitlab", Token: "$CORP_TOKEN"},
		{Host: "braces.example", Forge: "gitlab", Token: "${CORP_TOKEN}"},
		{Host: "empty.example", Forge: "gitlab", Token: "$EMPTY", TokenCmd: "echo from-cmd"},
	}
	seams.tokenCmdOut = map[string]string{"echo from-cmd": "from-cmd"}

	for host, want := range map[string]string{
		"literal.example": "literal-tok",
		"dollar.example":  "expanded",
		"braces.example":  "expanded",
		"empty.example":   "from-cmd", // empty expansion falls through to token_cmd
	} {
		got, err := TokenForHost(host, forgetypes.KindGitLab, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("TokenForHost(%s) = %q, want %q", host, got, want)
		}
	}
}

func TestTokenCmdCachedPerProcess(t *testing.T) {
	seams := &authSeams{tokenCmdOut: map[string]string{"pass show tok": "secret"}}
	installAuthSeams(t, seams)
	cfg := defaultForgeConfig()
	cfg.Hosts = []config.ForgeHost{{Host: "corp.example", Forge: "gitlab", TokenCmd: "pass show tok"}}

	for i := 0; i < 3; i++ {
		got, err := TokenForHost("corp.example", forgetypes.KindGitLab, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got != "secret" {
			t.Fatalf("token = %q", got)
		}
	}
	if seams.tokenCmdCalls != 1 {
		t.Fatalf("token_cmd ran %d times, want 1", seams.tokenCmdCalls)
	}
}

func TestTokenCmdFailureIsHardError(t *testing.T) {
	seams := &authSeams{tokenCmdErr: errors.New("boom")}
	installAuthSeams(t, seams)
	cfg := defaultForgeConfig()
	cfg.Hosts = []config.ForgeHost{{Host: "corp.example", Forge: "gitlab", TokenCmd: "explode"}}
	_, err := TokenForHost("corp.example", forgetypes.KindGitLab, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGhFallbackGatedByKindAndConfig(t *testing.T) {
	// Allowed: github kind + fallback enabled.
	seams := &authSeams{ghToken: "gh-tok"}
	installAuthSeams(t, seams)
	cfg := defaultForgeConfig()
	cfg.Hosts = []config.ForgeHost{{Host: "ghe.corp.example", Forge: "github"}}
	got, err := TokenForHost("ghe.corp.example", forgetypes.KindGitHub, cfg)
	if err != nil || got != "gh-tok" {
		t.Fatalf("token = %q err = %v", got, err)
	}
	if seams.ghCalls != 1 {
		t.Fatalf("gh calls = %d", seams.ghCalls)
	}

	// Disabled by config.
	seams = &authSeams{ghToken: "gh-tok"}
	installAuthSeams(t, seams)
	cfg.CLITokenFallback = false
	got, err = TokenForHost("ghe.corp.example", forgetypes.KindGitHub, cfg)
	if err != nil || got != "" {
		t.Fatalf("token = %q err = %v", got, err)
	}
	if seams.ghCalls != 0 {
		t.Fatalf("gh called despite cli_token_fallback=false")
	}

	// Never for non-github kinds.
	seams = &authSeams{ghToken: "gh-tok"}
	installAuthSeams(t, seams)
	cfg.CLITokenFallback = true
	got, err = TokenForHost("gitlab.corp.example", forgetypes.KindGitLab, cfg)
	if err != nil || got != "" {
		t.Fatalf("token = %q err = %v", got, err)
	}
	if seams.ghCalls != 0 {
		t.Fatalf("gh called for gitlab kind")
	}
}

func TestGhFallbackFailureFallsThroughToUnauthenticated(t *testing.T) {
	seams := &authSeams{ghErr: errors.New("no gh")}
	installAuthSeams(t, seams)
	got, err := TokenForHost("ghe.corp.example", forgetypes.KindGitHub, defaultForgeConfig())
	if err != nil || got != "" {
		t.Fatalf("token = %q err = %v", got, err)
	}
}

func TestTokenPrecedenceEnvBeatsConfig(t *testing.T) {
	seams := &authSeams{
		env:         map[string]string{"GITHUB_TOKEN": "from-env"},
		tokenCmdOut: map[string]string{"cmd": "from-cmd"},
	}
	installAuthSeams(t, seams)
	cfg := defaultForgeConfig()
	cfg.Hosts = []config.ForgeHost{{Host: "github.com", Forge: "github", Token: "from-config"}}
	got, err := TokenForHost("github.com", forgetypes.KindGitHub, cfg)
	if err != nil || got != "from-env" {
		t.Fatalf("token = %q err = %v", got, err)
	}
}

// BuildHTTPClient

func selfSignedPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "mrman-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestBuildHTTPClientDefault(t *testing.T) {
	client, err := BuildHTTPClient(HostConfig{Host: "github.com"})
	if err != nil {
		t.Fatal(err)
	}
	if client.Transport != nil {
		t.Fatal("default client should use the default transport")
	}
}

func TestBuildHTTPClientWithCAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, selfSignedPEM(t), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := BuildHTTPClient(HostConfig{Host: "corp.example", CAFile: path})
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig := transportTLS(t, client)
	if tlsConfig.RootCAs == nil {
		t.Fatal("RootCAs not set")
	}
	if tlsConfig.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify unexpectedly set")
	}
}

func TestBuildHTTPClientInsecureSkipVerify(t *testing.T) {
	client, err := BuildHTTPClient(HostConfig{Host: "corp.example", InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	if !transportTLS(t, client).InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify not set")
	}
}

func TestBuildHTTPClientCAFileErrors(t *testing.T) {
	if _, err := BuildHTTPClient(HostConfig{Host: "h", CAFile: "/nonexistent/ca.pem"}); err == nil {
		t.Fatal("expected error for missing file")
	}
	path := filepath.Join(t.TempDir(), "junk.pem")
	if err := os.WriteFile(path, []byte("not a pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := BuildHTTPClient(HostConfig{Host: "h", CAFile: path})
	if err == nil || !strings.Contains(err.Error(), "no valid PEM") {
		t.Fatalf("err = %v", err)
	}
}

func transportTLS(t *testing.T, client *http.Client) *tls.Config {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil {
		t.Fatalf("transport = %T, want *http.Transport with TLS config", client.Transport)
	}
	return transport.TLSClientConfig
}

func TestTokenOnlyForTrustedHosts(t *testing.T) {
	env := map[string]string{"GH_ENTERPRISE_TOKEN": "e", "GITLAB_TOKEN": "gl"}

	t.Run("listed host is trusted", func(t *testing.T) {
		installAuthSeams(t, &authSeams{env: env})
		cfg := defaultForgeConfig()
		cfg.Hosts = []config.ForgeHost{{Host: "ghe.corp.example", Forge: "github"}}
		got, err := TokenForHost("ghe.corp.example", forgetypes.KindGitHub, cfg)
		if err != nil || got != "e" {
			t.Fatalf("token = %q err = %v", got, err)
		}
	})

	t.Run("unlisted host gets no env token", func(t *testing.T) {
		installAuthSeams(t, &authSeams{env: env})
		got, err := TokenForHost("ghe.corp.example", forgetypes.KindGitHub, defaultForgeConfig())
		if err != nil || got != "" {
			t.Fatalf("token = %q err = %v", got, err)
		}
	})

	t.Run("unlisted host never consults gh", func(t *testing.T) {
		seams := &authSeams{ghToken: "gh-tok"}
		installAuthSeams(t, seams)
		cfg := defaultForgeConfig()
		cfg.CLITokenFallback = true
		got, err := TokenForHost("ghe.corp.example", forgetypes.KindGitHub, cfg)
		if err != nil || got != "" {
			t.Fatalf("token = %q err = %v", got, err)
		}
		if seams.ghCalls != 0 {
			t.Fatalf("gh called %d times for an unlisted host", seams.ghCalls)
		}
	})

	t.Run("SaaS hosts are trusted without config", func(t *testing.T) {
		installAuthSeams(t, &authSeams{env: env})
		got, err := TokenForHost("gitlab.com", forgetypes.KindGitLab, defaultForgeConfig())
		if err != nil || got != "gl" {
			t.Fatalf("token = %q err = %v", got, err)
		}
	})
}

func TestHostTrusted(t *testing.T) {
	cfg := defaultForgeConfig()
	cfg.Hosts = []config.ForgeHost{{Host: "GHE.Corp.Example", Forge: "github"}}
	for host, want := range map[string]bool{
		"github.com":            true,
		"dev.azure.com":         true,
		"corp.visualstudio.com": true,
		"ghe.corp.example":      true, // config entry, case-insensitive
		"github.evil.example":   false,
		"evil.example":          false,
	} {
		if got := HostTrusted(host, cfg); got != want {
			t.Errorf("HostTrusted(%q) = %v, want %v", host, got, want)
		}
	}
}

// redirectPair is server A redirecting to server B; both are TLS servers
// with self-signed certificates, so the client under test skips
// verification — the property being tested is the redirect policy, not
// certificate handling.
func redirectPair(t *testing.T, location func(b *httptest.Server) string) (a, b *httptest.Server, bHits *int) {
	t.Helper()
	hits := 0
	b = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(b.Close)
	a = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{URL: &url.URL{}}, location(b), http.StatusFound)
	}))
	t.Cleanup(a.Close)
	return a, b, &hits
}

func TestBuildHTTPClientRefusesCrossHostRedirect(t *testing.T) {
	a, _, bHits := redirectPair(t, func(b *httptest.Server) string { return b.URL + "/elsewhere" })
	aHost := mustURL(t, a.URL).Host
	client, err := BuildHTTPClient(HostConfig{Host: aHost, InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(a.URL + "/start")
	if resp != nil {
		_ = resp.Body.Close()
	}
	var refused *RedirectRefused
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want *RedirectRefused", err)
	}
	if *bHits != 0 {
		t.Fatalf("the other host was contacted %d times", *bHits)
	}
	if refused.From != aHost || refused.To == "" {
		t.Fatalf("refusal = %+v", refused)
	}
}

func TestBuildHTTPClientAllowsSameHostRedirect(t *testing.T) {
	hits := 0
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, srv.URL+"/final", http.StatusFound)
			return
		}
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	client, err := BuildHTTPClient(HostConfig{Host: mustURL(t, srv.URL).Host, InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(srv.URL + "/start")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || hits != 1 {
		t.Fatalf("status = %d hits = %d", resp.StatusCode, hits)
	}
}

func TestBuildHTTPClientPinsToAPIBaseOrigin(t *testing.T) {
	// With api_base on another origin, the redirect target that matches
	// api_base is allowed even though it is not hc.Host.
	a, b, bHits := redirectPair(t, func(b *httptest.Server) string { return b.URL + "/api/v3/x" })
	client, err := BuildHTTPClient(HostConfig{
		Host: "ghe.corp.example", APIBase: b.URL + "/api/v3", InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(a.URL + "/start")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if *bHits != 1 {
		t.Fatalf("api_base origin hit %d times, want 1", *bHits)
	}
}

func TestSameOriginRules(t *testing.T) {
	allowed := mustURL(t, "https://ghe.corp.example")
	for raw, want := range map[string]bool{
		"https://ghe.corp.example/api/v3":     true,
		"https://GHE.CORP.EXAMPLE:443/x":      true,
		"https://ghe.corp.example:8443/x":     false,
		"http://ghe.corp.example/x":           false, // downgrade
		"https://ghe.corp.example.evil.tld/x": false,
		"https://evil.tld/x":                  false,
	} {
		if got := sameOrigin(mustURL(t, raw), allowed); got != want {
			t.Errorf("sameOrigin(%s) = %v, want %v", raw, got, want)
		}
	}
	if !sameOrigin(mustURL(t, "http://fixture.local:8080/a"), mustURL(t, "http://fixture.local:8080")) {
		t.Error("a plaintext api_base allows plaintext on the same origin")
	}
}

func TestBuildHTTPClientHasTimeout(t *testing.T) {
	client, err := BuildHTTPClient(HostConfig{Host: "github.com"})
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout == 0 {
		t.Fatal("forge requests must not wait forever")
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
