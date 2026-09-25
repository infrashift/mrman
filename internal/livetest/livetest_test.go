package livetest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

func gitlabHost(host string) config.ForgeConfig {
	cfg := config.Default().Forge
	cfg.Hosts = []config.ForgeHost{{Host: host, Forge: "gitlab"}}
	return cfg
}

func TestParseAcceptsEveryTargetShape(t *testing.T) {
	cases := []struct {
		name, target string
		cfg          config.ForgeConfig
		want         forgetypes.Repository
		number       uint64
	}{
		{"github coordinate", "infrashift/scratch#1", config.Default().Forge,
			forgetypes.Repository{Kind: forgetypes.KindGitHub, Host: "github.com", Owner: "infrashift", Name: "scratch"}, 1},
		{"gitlab https url with subgroups", "https://gitlab.example.com/group/sub/repo/-/merge_requests/42", config.Default().Forge,
			forgetypes.Repository{Kind: forgetypes.KindGitLab, Host: "gitlab.example.com", Owner: "group/sub", Name: "repo"}, 42},
		{"gitlab plain http with a port", "http://forge.internal:8093/chad/python/-/merge_requests/3", gitlabHost("forge.internal"),
			forgetypes.Repository{Kind: forgetypes.KindGitLab, Host: "forge.internal", Owner: "chad", Name: "python"}, 3},
		{"gitlab host-qualified bang coordinate", "forge.internal/group/sub/repo!7", gitlabHost("forge.internal"),
			forgetypes.Repository{Kind: forgetypes.KindGitLab, Host: "forge.internal", Owner: "group/sub", Name: "repo"}, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, number, err := Parse(tc.target, tc.cfg)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.target, err)
			}
			if repo.Kind != tc.want.Kind || repo.Host != tc.want.Host ||
				repo.Owner != tc.want.Owner || repo.Name != tc.want.Name {
				t.Errorf("Parse(%q) = %+v, want %+v", tc.target, repo, tc.want)
			}
			if number != tc.number {
				t.Errorf("Parse(%q) number = %d, want %d", tc.target, number, tc.number)
			}
		})
	}
}

func TestParseRejectsMalformedTargets(t *testing.T) {
	for _, target := range []string{"", "not a target", "owner/repo", "https://example.com/nothing/here"} {
		if _, _, err := Parse(target, config.Default().Forge); err == nil {
			t.Errorf("Parse(%q) must fail", target)
		}
	}
}

func TestConfigReadsForgeHosts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[[forge.hosts]]\nhost = \"forge.internal\"\nforge = \"gitlab\"\napi_base = \"http://forge.internal:8093/api/v4\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := configFrom(t, path)
	if len(cfg.Hosts) != 1 || cfg.Hosts[0].APIBase != "http://forge.internal:8093/api/v4" {
		t.Fatalf("the forge host entry did not load: %+v", cfg.Hosts)
	}
}

func TestConfigToleratesAMissingFile(t *testing.T) {
	_ = Config(t) // a missing user config yields defaults, never a failure
}

func TestTargetSkipsWithoutTheEnvironment(t *testing.T) {
	t.Setenv(EnvTarget, "")
	ran := t.Run("inner", func(t *testing.T) {
		Target(t, config.Default().Forge)
		t.Error("Target must skip when " + EnvTarget + " is unset")
	})
	if !ran {
		t.Error("a skipped subtest still reports success")
	}
}

func TestTargetResolvesTheEnvironment(t *testing.T) {
	t.Setenv(EnvTarget, "infrashift/scratch#9")
	repo, number := Target(t, config.Default().Forge)
	if repo.Name != "scratch" || number != 9 {
		t.Errorf("Target = %+v #%d", repo, number)
	}
}

func TestRequireKindSkipsAnotherForge(t *testing.T) {
	repo := forgetypes.Repository{Kind: forgetypes.KindGitHub}
	t.Run("other forge", func(t *testing.T) {
		RequireKind(t, repo, forgetypes.KindGitLab)
		t.Error("RequireKind must skip a GitHub target for a GitLab test")
	})
	RequireKind(t, repo, forgetypes.KindGitHub) // same forge: no skip
}

func TestRequireSubmitSkipsWithoutOptIn(t *testing.T) {
	t.Setenv(EnvSubmit, "")
	t.Run("inner", func(t *testing.T) {
		RequireSubmit(t)
		t.Error("RequireSubmit must skip when " + EnvSubmit + " is unset")
	})
	t.Setenv(EnvSubmit, "1")
	RequireSubmit(t) // opted in: no skip
}
