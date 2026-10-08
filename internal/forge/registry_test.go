package forge

import (
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

func defaultForgeConfig() config.ForgeConfig {
	return config.ForgeConfig{Default: "github", CommentTypePrefix: true, CLITokenFallback: true}
}

// withEmptyRegistry runs the test against a clean registry.
func withEmptyRegistry(t *testing.T) {
	t.Helper()
	prev := registry
	registry = nil
	t.Cleanup(func() { registry = prev })
}

func repo(kind forgetypes.Kind, host, owner, name string) *forgetypes.Repository {
	return &forgetypes.Repository{Kind: kind, Host: host, Owner: owner, Name: name}
}

func adoRepo(host, org, project, name string) *forgetypes.Repository {
	return &forgetypes.Repository{Kind: forgetypes.KindAzureDevOps, Host: host, Owner: org, Project: project, Name: name}
}

// Host resolution precedence

func TestResolveHostKindPrecedence(t *testing.T) {
	withEmptyRegistry(t)
	cfg := defaultForgeConfig()
	cfg.Hosts = []config.ForgeHost{
		// Config pins gitlab.com to github — config must beat built-ins.
		{Host: "gitlab.com", Forge: "github"},
		{Host: "githubbish.corp.example", Forge: "forgejo"}, // config beats heuristics
		{Host: "git.corp.example", Forge: "azuredevops"},
		{Host: "aliased.example", Forge: "azure_devops"}, // alias name accepted
	}
	cases := []struct {
		host  string
		kind  forgetypes.Kind
		known bool
	}{
		{"gitlab.com", forgetypes.KindGitHub, true},               // config beats built-in
		{"githubbish.corp.example", forgetypes.KindForgejo, true}, // config beats heuristic
		{"git.corp.example", forgetypes.KindAzureDevOps, true},
		{"aliased.example", forgetypes.KindAzureDevOps, true},
		{"github.com", forgetypes.KindGitHub, true},         // built-in
		{"dev.azure.com", forgetypes.KindAzureDevOps, true}, // built-in
		{"ssh.dev.azure.com", forgetypes.KindAzureDevOps, true},
		{"codeberg.org", forgetypes.KindForgejo, true},
		{"myorg.visualstudio.com", forgetypes.KindAzureDevOps, true},
		{"github.corp.example", forgetypes.KindGitHub, true}, // heuristic
		{"gitlab.corp.example", forgetypes.KindGitLab, true}, // heuristic
		{"gitea.corp.example", forgetypes.KindForgejo, true}, // heuristic
		{"forgejo.corp.example", forgetypes.KindForgejo, true},
		{"git.example.com", "", false}, // nothing claims it
	}
	for _, tc := range cases {
		kind, known := ResolveHostKind(tc.host, cfg)
		if known != tc.known || kind != tc.kind {
			t.Errorf("ResolveHostKind(%q) = (%q, %v), want (%q, %v)", tc.host, kind, known, tc.kind, tc.known)
		}
	}
}

func TestResolveHostKindDriverDefaultHosts(t *testing.T) {
	withEmptyRegistry(t)
	Register(Driver{ID: forgetypes.KindForgejo, SlugPrefix: "fj", DefaultHosts: []string{"gitfoo.example"}})
	kind, known := ResolveHostKind("gitfoo.example", defaultForgeConfig())
	if !known || kind != forgetypes.KindForgejo {
		t.Fatalf("driver default host: (%q, %v)", kind, known)
	}
}

// ResolveRepository

func TestResolveRepositoryTable(t *testing.T) {
	withEmptyRegistry(t)
	withoutSSHConfig(t)
	cfg := defaultForgeConfig()
	cases := []struct {
		name string
		urls []string
		want *forgetypes.Repository
	}{
		{"github https", []string{"https://github.com/agavra/tuicr.git"},
			repo(forgetypes.KindGitHub, "github.com", "agavra", "tuicr")},
		{"github scp", []string{"git@github.com:agavra/tuicr.git"},
			repo(forgetypes.KindGitHub, "github.com", "agavra", "tuicr")},
		{"gitlab subgroups", []string{"https://gitlab.com/platform/tools/cli.git"},
			repo(forgetypes.KindGitLab, "gitlab.com", "platform/tools", "cli")},
		{"forgejo codeberg", []string{"git@codeberg.org:forgejo/forgejo.git"},
			repo(forgetypes.KindForgejo, "codeberg.org", "forgejo", "forgejo")},
		{"ado https", []string{"https://dev.azure.com/org/project/_git/repo"},
			adoRepo("dev.azure.com", "org", "project", "repo")},
		{"ado ssh v3", []string{"git@ssh.dev.azure.com:v3/org/project/repo"},
			adoRepo("dev.azure.com", "org", "project", "repo")},
		{"ado legacy visualstudio", []string{"https://myorg.visualstudio.com/DefaultCollection/project/_git/repo"},
			adoRepo("myorg.visualstudio.com", "myorg", "project", "repo")},
		{"ado legacy without collection", []string{"https://myorg.visualstudio.com/project/_git/repo"},
			adoRepo("myorg.visualstudio.com", "myorg", "project", "repo")},
		{"unknown host defaults to github", []string{"https://git.example.com/owner/repo.git"},
			repo(forgetypes.KindGitHub, "git.example.com", "owner", "repo")},
		{"first parseable remote wins", []string{"not a url", "https://github.com/o/r.git"},
			repo(forgetypes.KindGitHub, "github.com", "o", "r")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveRepository(tc.urls, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestResolveRepositoryConfigBeatsBuiltinAndDefault(t *testing.T) {
	withEmptyRegistry(t)
	withoutSSHConfig(t)
	cfg := defaultForgeConfig()
	cfg.Default = "forgejo"
	cfg.Hosts = []config.ForgeHost{{Host: "github.com", Forge: "gitlab"}}

	// Config entry rebinds github.com to gitlab.
	got, err := ResolveRepository([]string{"https://github.com/o/r.git"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != forgetypes.KindGitLab {
		t.Fatalf("kind = %q", got.Kind)
	}

	// Unknown parseable host falls to Default (forgejo), not github.
	got, err = ResolveRepository([]string{"https://git.example.com/o/r.git"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != forgetypes.KindForgejo {
		t.Fatalf("kind = %q", got.Kind)
	}
}

func TestResolveRepositoryUnknownHostErrorEmbedsConfigSnippet(t *testing.T) {
	withEmptyRegistry(t)
	withoutSSHConfig(t)
	cfg := defaultForgeConfig()
	cfg.Default = "azuredevops" // 2-segment path cannot parse as ADO
	_, err := ResolveRepository([]string{"https://git.example.com/owner/repo.git"}, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"[[forge.hosts]]", `host  = "git.example.com"`, "forge ="} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q: %s", want, msg)
		}
	}
}

func TestResolveRepositoryNoRemotes(t *testing.T) {
	withEmptyRegistry(t)
	_, err := ResolveRepository(nil, defaultForgeConfig())
	if !errors.Is(err, errs.ErrNotARepository) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveRepositoryUsesDriverParseRepoPath(t *testing.T) {
	withEmptyRegistry(t)
	withoutSSHConfig(t)
	Register(Driver{
		ID: forgetypes.KindGitHub,
		ParseRepoPath: func(host string, segments []string) (*forgetypes.Repository, bool) {
			return &forgetypes.Repository{Kind: forgetypes.KindGitHub, Host: host, Owner: "driver", Name: segments[len(segments)-1]}, true
		},
	})
	got, err := ResolveRepository([]string{"https://github.com/a/b.git"}, defaultForgeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got.Owner != "driver" {
		t.Fatalf("driver override not used: %+v", got)
	}
}

// Registry mechanics

func TestRegisterReplacesSameIDAndPreservesOrder(t *testing.T) {
	withEmptyRegistry(t)
	Register(Driver{ID: forgetypes.KindGitHub, SlugPrefix: "gh"})
	Register(Driver{ID: forgetypes.KindGitLab, SlugPrefix: "gl"})
	Register(Driver{ID: forgetypes.KindGitHub, SlugPrefix: "gh2"})
	ds := Drivers()
	if len(ds) != 2 || ds[0].SlugPrefix != "gh2" || ds[1].SlugPrefix != "gl" {
		t.Fatalf("drivers = %+v", ds)
	}
	d, ok := DriverFor(forgetypes.KindGitLab)
	if !ok || d.SlugPrefix != "gl" {
		t.Fatalf("DriverFor = %+v, %v", d, ok)
	}
	if _, ok := DriverFor(forgetypes.KindForgejo); ok {
		t.Fatal("unexpected driver")
	}
}

func TestForRepositoryConstructsDriver(t *testing.T) {
	withEmptyRegistry(t)
	var gotCfg HostConfig
	Register(Driver{
		ID: forgetypes.KindGitHub,
		New: func(cfg HostConfig) (Forge, error) {
			gotCfg = cfg
			return nil, nil
		},
	})
	cfg := defaultForgeConfig()
	cfg.CLITokenFallback = false
	cfg.Hosts = []config.ForgeHost{{
		Host: "ghe.corp.example", Forge: "github",
		APIBase: "https://ghe.corp.example/api/v3", Token: "tok-123", CAFile: "/ca.pem",
	}}
	if _, err := ForRepository(*repo(forgetypes.KindGitHub, "ghe.corp.example", "o", "r"), cfg); err != nil {
		t.Fatal(err)
	}
	want := HostConfig{
		Host: "ghe.corp.example", Kind: forgetypes.KindGitHub,
		APIBase: "https://ghe.corp.example/api/v3", Token: "tok-123", CAFile: "/ca.pem",
	}
	if gotCfg != want {
		t.Fatalf("host config = %+v, want %+v", gotCfg, want)
	}
}

func TestForRepositoryUnregisteredKind(t *testing.T) {
	withEmptyRegistry(t)
	_, err := ForRepository(*repo(forgetypes.KindGitLab, "gitlab.com", "o", "r"), defaultForgeConfig())
	if err == nil || !strings.Contains(err.Error(), "no forge driver registered") {
		t.Fatalf("err = %v", err)
	}
}

func TestDefaultSaaSHost(t *testing.T) {
	cases := map[forgetypes.Kind]string{
		forgetypes.KindGitHub:      "github.com",
		forgetypes.KindGitLab:      "gitlab.com",
		forgetypes.KindAzureDevOps: "dev.azure.com",
		forgetypes.KindForgejo:     "codeberg.org",
		forgetypes.Kind("other"):   "",
	}
	for kind, want := range cases {
		if got := DefaultSaaSHost(kind); got != want {
			t.Errorf("DefaultSaaSHost(%q) = %q, want %q", kind, got, want)
		}
	}
}

// ParseTarget

func TestParseTargetBareNumber(t *testing.T) {
	withEmptyRegistry(t)
	checkout := repo(forgetypes.KindGitHub, "github.com", "agavra", "tuicr")
	target, err := ParseTarget("125", checkout, defaultForgeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if target.Number != 125 || target.Original != "125" {
		t.Fatalf("target = %+v", target)
	}
	if !reflect.DeepEqual(target.Repository, checkout) {
		t.Fatalf("repository = %+v", target.Repository)
	}
	// The target owns a copy, not the caller's pointer.
	if target.Repository == checkout {
		t.Fatal("repository aliases the checkout pointer")
	}
}

func TestParseTargetBareNumberWithoutCheckoutErrors(t *testing.T) {
	withEmptyRegistry(t)
	_, err := ParseTarget("125", nil, defaultForgeConfig())
	if _, ok := errors.AsType[*errs.InvalidInput](err); !ok {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "default_forge") {
		t.Fatalf("error should name default_forge semantics: %v", err)
	}
}

func TestParseTargetCoordinates(t *testing.T) {
	withEmptyRegistry(t)
	glCheckout := repo(forgetypes.KindGitLab, "gitlab.corp.example", "platform", "cli")
	adoCheckout := adoRepo("dev.azure.com", "org", "project", "repo")
	cfgForgejoDefault := defaultForgeConfig()
	cfgForgejoDefault.Default = "forgejo"

	cases := []struct {
		name     string
		input    string
		checkout *forgetypes.Repository
		cfg      config.ForgeConfig
		want     *forgetypes.Repository
		number   uint64
	}{
		{"owner/repo#N without checkout uses default SaaS host", "agavra/tuicr#125", nil, defaultForgeConfig(),
			repo(forgetypes.KindGitHub, "github.com", "agavra", "tuicr"), 125},
		{"owner/repo#N default forge forgejo", "o/r#3", nil, cfgForgejoDefault,
			repo(forgetypes.KindForgejo, "codeberg.org", "o", "r"), 3},
		{"owner/repo!N takes checkout host", "group/cli!17", glCheckout, defaultForgeConfig(),
			repo(forgetypes.KindGitLab, "gitlab.corp.example", "group", "cli"), 17},
		{"project/repo#N inside ADO checkout", "proj2/repo2#9", adoCheckout, defaultForgeConfig(),
			adoRepo("dev.azure.com", "org", "proj2", "repo2"), 9},
		{"host/owner/repo#N resolves host", "gitlab.corp.example/group/cli#4", nil, defaultForgeConfig(),
			repo(forgetypes.KindGitLab, "gitlab.corp.example", "group", "cli"), 4},
		{"host/group/sub/repo#N gitlab subgroups", "gitlab.com/group/sub/repo#8", nil, defaultForgeConfig(),
			repo(forgetypes.KindGitLab, "gitlab.com", "group/sub", "repo"), 8},
		{"host/org/project/repo#N ado", "dev.azure.com/org/project/repo#2", nil, defaultForgeConfig(),
			adoRepo("dev.azure.com", "org", "project", "repo"), 2},
		{"unknown host falls back to default forge", "git.example.com/o/r#5", nil, defaultForgeConfig(),
			repo(forgetypes.KindGitHub, "git.example.com", "o", "r"), 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, err := ParseTarget(tc.input, tc.checkout, tc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if target.Number != tc.number || target.Original != tc.input {
				t.Fatalf("target = %+v", target)
			}
			if !reflect.DeepEqual(target.Repository, tc.want) {
				t.Fatalf("repository = %+v, want %+v", target.Repository, tc.want)
			}
		})
	}
}

func TestParseTargetURLs(t *testing.T) {
	withEmptyRegistry(t)
	cases := []struct {
		name   string
		input  string
		want   *forgetypes.Repository
		number uint64
	}{
		{"github pull", "https://github.com/agavra/tuicr/pull/125",
			repo(forgetypes.KindGitHub, "github.com", "agavra", "tuicr"), 125},
		{"gitlab merge_requests", "https://gitlab.com/group/cli/-/merge_requests/17",
			repo(forgetypes.KindGitLab, "gitlab.com", "group", "cli"), 17},
		{"gitlab subgroups", "https://gitlab.corp.example/platform/tools/cli/-/merge_requests/3",
			repo(forgetypes.KindGitLab, "gitlab.corp.example", "platform/tools", "cli"), 3},
		{"forgejo pulls", "https://codeberg.org/forgejo/forgejo/pulls/3401",
			repo(forgetypes.KindForgejo, "codeberg.org", "forgejo", "forgejo"), 3401},
		{"ado pullrequest", "https://dev.azure.com/org/project/_git/repo/pullrequest/9",
			adoRepo("dev.azure.com", "org", "project", "repo"), 9},
		{"ado legacy visualstudio", "https://myorg.visualstudio.com/project/_git/repo/pullrequest/7",
			adoRepo("myorg.visualstudio.com", "myorg", "project", "repo"), 7},
		{"ado legacy DefaultCollection", "https://myorg.visualstudio.com/DefaultCollection/project/_git/repo/pullrequest/7",
			adoRepo("myorg.visualstudio.com", "myorg", "project", "repo"), 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, err := ParseTarget(tc.input, nil, defaultForgeConfig())
			if err != nil {
				t.Fatal(err)
			}
			if target.Number != tc.number || target.Original != tc.input {
				t.Fatalf("target = %+v", target)
			}
			if !reflect.DeepEqual(target.Repository, tc.want) {
				t.Fatalf("repository = %+v, want %+v", target.Repository, tc.want)
			}
		})
	}
}

func TestParseTargetURLHostResolutionOverridesShapeGuess(t *testing.T) {
	withEmptyRegistry(t)
	cfg := defaultForgeConfig()
	cfg.Hosts = []config.ForgeHost{{Host: "git.corp.example", Forge: "forgejo"}}
	// /pull/N is GitHub's shape, but config pins the host to forgejo.
	target, err := ParseTarget("https://git.corp.example/o/r/pull/5", nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if target.Repository.Kind != forgetypes.KindForgejo {
		t.Fatalf("kind = %q", target.Repository.Kind)
	}

	// A gitea-substring host overrides the /pull/N GitHub guess too.
	target, err = ParseTarget("https://gitea.example.com/o/r/pulls/6", nil, defaultForgeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if target.Repository.Kind != forgetypes.KindForgejo {
		t.Fatalf("kind = %q", target.Repository.Kind)
	}
}

func TestParseTargetDriverURLHook(t *testing.T) {
	withEmptyRegistry(t)
	Register(Driver{
		ID: forgetypes.KindGitHub,
		ParseTargetURL: func(u *url.URL) (*Target, bool) {
			if u.Host != "special.example" {
				return nil, false
			}
			return &Target{
				Repository: repo(forgetypes.KindGitHub, "special.example", "o", "r"),
				Number:     42,
			}, true
		},
	})
	target, err := ParseTarget("https://special.example/whatever", nil, defaultForgeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if target.Number != 42 || target.Original != "https://special.example/whatever" {
		t.Fatalf("target = %+v", target)
	}
}

func TestParseTargetMalformedInputs(t *testing.T) {
	withEmptyRegistry(t)
	checkout := repo(forgetypes.KindGitHub, "github.com", "o", "r")
	for _, input := range []string{
		"",
		"   ",
		"0",
		"owner/repo#0",
		"owner/repo",
		"https://github.com/owner/repo",
		"https://github.com/owner/repo/pull/notanumber",
		"https://github.com/owner/repo/issues/5",
		"onlyowner#5",
		"dev.azure.com/org/repo#5", // ADO needs org/project/repo
	} {
		if _, err := ParseTarget(input, checkout, defaultForgeConfig()); err == nil {
			t.Errorf("ParseTarget(%q) unexpectedly succeeded", input)
		}
	}
}

func TestParseTargetStripsGitSuffixInURLAndCoordinate(t *testing.T) {
	withEmptyRegistry(t)
	target, err := ParseTarget("https://github.com/o/r.git/pull/5", nil, defaultForgeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if target.Repository.Name != "r" {
		t.Fatalf("name = %q", target.Repository.Name)
	}
	target, err = ParseTarget("o/r.git#5", nil, defaultForgeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if target.Repository.Name != "r" {
		t.Fatalf("name = %q", target.Repository.Name)
	}
}

func TestResolveHostConfigWarnsAndWithholdsTokenForUntrustedHost(t *testing.T) {
	withEmptyRegistry(t)
	installAuthSeams(t, &authSeams{env: map[string]string{"GH_ENTERPRISE_TOKEN": "leak"}})
	cfg := defaultForgeConfig()

	target, err := ParseTarget("https://evil.example/o/r/pull/1", nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if target.Repository.Kind != forgetypes.KindGitHub {
		t.Fatalf("kind = %q; the URL shape still decides how to talk to the host", target.Repository.Kind)
	}
	hc, err := ResolveHostConfig(target.Repository.Host, target.Repository.Kind, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if hc.Token != "" {
		t.Fatalf("token %q resolved for an untrusted host", hc.Token)
	}
	if !strings.Contains(hc.Warning, "evil.example") || !strings.Contains(hc.Warning, "[[forge.hosts]]") {
		t.Fatalf("warning = %q, want it to name the host and the remedy", hc.Warning)
	}

	// Listing the host is the remedy, and clears the warning.
	cfg.Hosts = []config.ForgeHost{{Host: "evil.example", Forge: "github"}}
	hc, err = ResolveHostConfig("evil.example", forgetypes.KindGitHub, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if hc.Token != "leak" || hc.Warning != "" {
		t.Fatalf("token = %q warning = %q after listing the host", hc.Token, hc.Warning)
	}
}

// TestKindFromConfigNameAliases pins the names config accepts to the drivers
// they select.
func TestKindFromConfigNameAliases(t *testing.T) {
	cases := map[string]forgetypes.Kind{
		"github": forgetypes.KindGitHub, "gitlab": forgetypes.KindGitLab,
		"azuredevops": forgetypes.KindAzureDevOps, "azure_devops": forgetypes.KindAzureDevOps,
		"ado": forgetypes.KindAzureDevOps, "forgejo": forgetypes.KindForgejo,
		"gitea": forgetypes.KindForgejo, " GitLab ": forgetypes.KindGitLab,
	}
	for name, want := range cases {
		if got, ok := kindFromConfigName(name); !ok || got != want {
			t.Errorf("kindFromConfigName(%q) = %v, %v; want %v", name, got, ok, want)
		}
	}
	if _, ok := kindFromConfigName("bitbucket"); ok {
		t.Error("an unknown forge name must not resolve")
	}
}
