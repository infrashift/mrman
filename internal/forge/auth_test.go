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
		{"ghe host GH_ENTERPRISE_TOKEN", "ghe.corp.example", forgetypes.KindGitHub,
			map[string]string{"GH_ENTERPRISE_TOKEN": "e", "GITHUB_TOKEN": "a"}, "e"},
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
