package forge

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// withSSHConfig points ssh alias resolution at a fixture file for the test.
func withSSHConfig(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := sshConfigPathFn
	sshConfigPathFn = func() string { return path }
	t.Cleanup(func() { sshConfigPathFn = prev })
}

// withoutSSHConfig makes alias resolution a no-op for the test.
func withoutSSHConfig(t *testing.T) {
	t.Helper()
	prev := sshConfigPathFn
	sshConfigPathFn = func() string { return "" }
	t.Cleanup(func() { sshConfigPathFn = prev })
}

func TestSplitRemoteURLTable(t *testing.T) {
	withoutSSHConfig(t)
	cases := []struct {
		name     string
		raw      string
		host     string
		segments []string
	}{
		{"github https", "https://github.com/agavra/tuicr.git", "github.com", []string{"agavra", "tuicr"}},
		{"github https no suffix", "https://github.com/agavra/tuicr", "github.com", []string{"agavra", "tuicr"}},
		{"github scp", "git@github.com:agavra/tuicr.git", "github.com", []string{"agavra", "tuicr"}},
		{"ssh scheme with user", "ssh://git@github.example.com/agavra/tuicr.git", "github.example.com", []string{"agavra", "tuicr"}},
		{"ssh scheme with port", "ssh://git@git.example.com:2222/owner/repo.git", "git.example.com", []string{"owner", "repo"}},
		{"http gitea with port", "http://gitea.example.com:3000/owner/repo.git", "gitea.example.com", []string{"owner", "repo"}},
		{"transport ssh.github.com", "git@ssh.github.com:owner/repo.git", "github.com", []string{"owner", "repo"}},
		{"transport altssh.gitlab.com", "git@altssh.gitlab.com:group/repo.git", "gitlab.com", []string{"group", "repo"}},
		{"gitlab subgroups https", "https://gitlab.com/group/sub/repo.git", "gitlab.com", []string{"group", "sub", "repo"}},
		{"gitlab subgroups scp", "git@gitlab.corp.example:platform/tools/cli.git", "gitlab.corp.example", []string{"platform", "tools", "cli"}},
		{"ado https", "https://dev.azure.com/org/project/_git/repo", "dev.azure.com", []string{"org", "project", "_git", "repo"}},
		{"ado https user prefix", "https://org@dev.azure.com/org/project/_git/repo", "dev.azure.com", []string{"org", "project", "_git", "repo"}},
		{"ado ssh v3", "git@ssh.dev.azure.com:v3/org/project/repo", "dev.azure.com", []string{"v3", "org", "project", "repo"}},
		{"ado legacy visualstudio", "https://myorg.visualstudio.com/DefaultCollection/project/_git/repo", "myorg.visualstudio.com", []string{"DefaultCollection", "project", "_git", "repo"}},
		{"query and fragment stripped", "https://github.com/o/r.git?ref=x", "github.com", []string{"o", "r"}},
		{"trailing slash stripped", "https://github.com/o/r/", "github.com", []string{"o", "r"}},
		{"uppercase host lowered", "https://GitHub.com/o/r", "github.com", []string{"o", "r"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, segments, ok := SplitRemoteURL(tc.raw)
			if !ok {
				t.Fatalf("SplitRemoteURL(%q) failed", tc.raw)
			}
			if host != tc.host || !reflect.DeepEqual(segments, tc.segments) {
				t.Fatalf("got (%q, %v), want (%q, %v)", host, segments, tc.host, tc.segments)
			}
		})
	}
}

func TestSplitRemoteURLRejectsMalformed(t *testing.T) {
	withoutSSHConfig(t)
	for _, raw := range []string{
		"",
		"   ",
		"ftp://example.com/o/r",
		"no-path-here",
		"https://",
		"https://hostonly",
		"git@github.com:",
	} {
		if _, _, ok := SplitRemoteURL(raw); ok {
			t.Errorf("SplitRemoteURL(%q) unexpectedly parsed", raw)
		}
	}
}

func TestResolvesSSHAliasFromConfig(t *testing.T) {
	withSSHConfig(t, `
# work account
Host gh-work other-alias
    HostName github.com
    User git

Host gl
    HostName = gitlab.corp.example
`)
	host, segments, ok := SplitRemoteURL("git@gh-work:acme/api.git")
	if !ok || host != "github.com" {
		t.Fatalf("alias resolution: host=%q ok=%v", host, ok)
	}
	if !reflect.DeepEqual(segments, []string{"acme", "api"}) {
		t.Fatalf("segments = %v", segments)
	}

	host, _, ok = SplitRemoteURL("git@gl:group/repo.git")
	if !ok || host != "gitlab.corp.example" {
		t.Fatalf("equals-form alias: host=%q ok=%v", host, ok)
	}
}

func TestSSHAliasResolutionNormalizesTransportHost(t *testing.T) {
	// The GitHub SSH-over-443 workaround maps github.com to
	// ssh.github.com; the resolved host must come back as github.com.
	withSSHConfig(t, "Host github.com\n  HostName ssh.github.com\n")
	host, _, ok := SplitRemoteURL("git@github.com:o/r.git")
	if !ok || host != "github.com" {
		t.Fatalf("host = %q ok = %v", host, ok)
	}
}

func TestSSHAliasExactMatchOnly(t *testing.T) {
	withSSHConfig(t, "Host *\n  HostName wildcard.example\n\nMatch all\n  HostName matched.example\n")
	host, _, ok := SplitRemoteURL("git@myhost.example:o/r.git")
	if !ok || host != "myhost.example" {
		t.Fatalf("wildcard must not match: host=%q ok=%v", host, ok)
	}
}

func TestSSHAliasMissingConfigFallsBack(t *testing.T) {
	prev := sshConfigPathFn
	sshConfigPathFn = func() string { return "/nonexistent/ssh/config" }
	t.Cleanup(func() { sshConfigPathFn = prev })
	host, _, ok := SplitRemoteURL("git@myalias.example:o/r.git")
	if !ok || host != "myalias.example" {
		t.Fatalf("host = %q ok = %v", host, ok)
	}
}

func TestResolveSSHHostnameFromConfigMatchBlockResets(t *testing.T) {
	config := "Host gh\n  HostName github.com\nMatch host other\n  HostName elsewhere.example\n"
	if got := resolveSSHHostnameFromConfig("gh", config); got != "github.com" {
		t.Fatalf("got %q", got)
	}
	// HostName under a Match block must not apply.
	config = "Match all\n  HostName elsewhere.example\n"
	if got := resolveSSHHostnameFromConfig("gh", config); got != "gh" {
		t.Fatalf("got %q", got)
	}
}

// fakeRunner replays canned git outputs.
type fakeRunner struct {
	outputs map[string]string
	errs    map[string]error
	calls   []string
}

func (f *fakeRunner) Run(_ string, name string, args ...string) ([]byte, []byte, error) {
	key := name
	for _, a := range args {
		key += " " + a
	}
	f.calls = append(f.calls, key)
	if err, ok := f.errs[key]; ok {
		return nil, nil, err
	}
	return []byte(f.outputs[key]), nil, nil
}

func TestRemoteURLsOriginFirstThenAllRemotesDeduped(t *testing.T) {
	run := &fakeRunner{outputs: map[string]string{
		"git remote get-url origin": "https://github.com/contributor/tuicr.git\n",
		"git remote -v": "origin\thttps://github.com/contributor/tuicr.git (fetch)\n" +
			"origin\thttps://github.com/contributor/tuicr.git (push)\n" +
			"upstream\thttps://github.com/agavra/tuicr.git (fetch)\n" +
			"upstream\thttps://github.com/agavra/tuicr.git (push)\n",
	}}
	urls := RemoteURLs("/repo", run)
	want := []string{
		"https://github.com/contributor/tuicr.git",
		"https://github.com/agavra/tuicr.git",
	}
	if !reflect.DeepEqual(urls, want) {
		t.Fatalf("urls = %v", urls)
	}
}

func TestRemoteURLsToleratesMissingOrigin(t *testing.T) {
	run := &fakeRunner{
		outputs: map[string]string{
			"git remote -v": "upstream\thttps://github.com/agavra/tuicr.git (fetch)\n",
		},
		errs: map[string]error{
			"git remote get-url origin": os.ErrNotExist,
		},
	}
	urls := RemoteURLs("/repo", run)
	if !reflect.DeepEqual(urls, []string{"https://github.com/agavra/tuicr.git"}) {
		t.Fatalf("urls = %v", urls)
	}
}

func TestRemoteURLsEmptyWhenGitFails(t *testing.T) {
	run := &fakeRunner{errs: map[string]error{
		"git remote get-url origin": os.ErrNotExist,
		"git remote -v":             os.ErrNotExist,
	}}
	if urls := RemoteURLs("/repo", run); len(urls) != 0 {
		t.Fatalf("urls = %v", urls)
	}
}
