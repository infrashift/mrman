package git

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
)

// response is one canned fake-runner result.
type response struct {
	stdout string
	stderr string
	err    error
}

// exitError simulates a subprocess exiting non-zero.
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

// ExitCode reports the simulated exit code.
func (e exitError) ExitCode() int { return int(e) }

// fakeRunner is a scripted vcs.Runner keyed on the exact command argv.
type fakeRunner struct {
	t         *testing.T
	responses map[string]response
	calls     []string
}

func (r *fakeRunner) Run(_, name string, args ...string) ([]byte, []byte, error) {
	r.t.Helper()
	key := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, key)
	resp, ok := r.responses[key]
	if !ok {
		r.t.Fatalf("unscripted command: %q", key)
	}
	return []byte(resp.stdout), []byte(resp.stderr), resp.err
}

func (r *fakeRunner) called(key string) bool { return slices.Contains(r.calls, key) }

// newTestBackend builds a Backend over a fake runner without going through
// Discover.
func newTestBackend(t *testing.T, root string, responses map[string]response) (*Backend, *fakeRunner) {
	t.Helper()
	runner := &fakeRunner{t: t, responses: responses}
	backend := &Backend{
		run:  runner,
		root: root,
		info: vcs.Info{RootPath: root, HeadCommit: "headid", Type: vcs.TypeGit},
	}
	return backend, runner
}

func testHighlighter() *syntax.Highlighter {
	return syntax.NewHighlighter("github", "#144212", "#421212")
}

const (
	sparseConfigKey  = `git config --get-regexp ^(core\.sparsecheckout|index\.sparse)$`
	runtimeConfigKey = `git config --get-regexp ^(core\.untrackedcache|core\.fsmonitor|feature\.manyfiles)$`
)

func TestDiscover(t *testing.T) {
	backend, runner := discoverWith(t, map[string]response{
		"git rev-parse --show-toplevel":         {stdout: "/repo/root\n"},
		sparseConfigKey:                         {err: exitError(1)},
		"git rev-parse HEAD":                    {stdout: "abc123\n"},
		"git symbolic-ref --quiet --short HEAD": {stdout: "main\n"},
		runtimeConfigKey:                        {err: exitError(1)},
	})

	info := backend.Info()
	if info.RootPath != "/repo/root" {
		t.Errorf("RootPath = %q", info.RootPath)
	}
	if info.HeadCommit != "abc123" {
		t.Errorf("HeadCommit = %q", info.HeadCommit)
	}
	if info.BranchName == nil || *info.BranchName != "main" {
		t.Errorf("BranchName = %v", info.BranchName)
	}
	if info.Type != vcs.TypeGit {
		t.Errorf("Type = %q", info.Type)
	}
	if backend.repoMode != repoStandard {
		t.Errorf("repoMode = %v", backend.repoMode)
	}
	if !backend.SupportsSparseCheckout() {
		t.Error("SupportsSparseCheckout should be true")
	}
	if got := backend.StartupWarnings(); got != nil {
		t.Errorf("StartupWarnings = %v", got)
	}
	if runner.calls[0] != "git rev-parse --show-toplevel" {
		t.Errorf("first call = %q", runner.calls[0])
	}
}

func TestDiscoverNotARepository(t *testing.T) {
	runner := &fakeRunner{t: t, responses: map[string]response{
		"git rev-parse --show-toplevel": {stderr: "fatal: not a git repository", err: exitError(128)},
	}}

	_, err := Discover("/nowhere", vcs.WhitespaceNormal, runner)

	if !errors.Is(err, errs.ErrNotARepository) {
		t.Fatalf("err = %v, want ErrNotARepository", err)
	}
}

func TestDiscoverUnbornHeadAndDetachedBranch(t *testing.T) {
	backend, _ := discoverWith(t, map[string]response{
		"git rev-parse --show-toplevel":         {stdout: "/repo\n"},
		sparseConfigKey:                         {err: exitError(1)},
		"git rev-parse HEAD":                    {stderr: "fatal: bad revision", err: exitError(128)},
		"git symbolic-ref --quiet --short HEAD": {err: exitError(1)},
		runtimeConfigKey:                        {err: exitError(1)},
	})

	if backend.info.HeadCommit != "HEAD" {
		t.Errorf("HeadCommit = %q, want HEAD fallback", backend.info.HeadCommit)
	}
	if backend.info.BranchName != nil {
		t.Errorf("BranchName = %v, want nil", backend.info.BranchName)
	}
}

func TestDiscoverSparseIndexRepo(t *testing.T) {
	backend, _ := discoverWith(t, map[string]response{
		"git rev-parse --show-toplevel":         {stdout: "/repo\n"},
		sparseConfigKey:                         {stdout: "core.sparsecheckout true\nindex.sparse true\n"},
		"git rev-parse HEAD":                    {stdout: "abc\n"},
		"git symbolic-ref --quiet --short HEAD": {stdout: "main\n"},
		runtimeConfigKey:                        {stdout: "core.fsmonitor true\n"},
	})

	if backend.repoMode != repoSparseIndex {
		t.Fatalf("repoMode = %v, want sparse index", backend.repoMode)
	}
	warnings := backend.StartupWarnings()
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v", warnings)
	}
	if warnings[0] != "Sparse checkout detected; using Git CLI backend." {
		t.Errorf("warnings[0] = %q", warnings[0])
	}
	if !strings.Contains(warnings[1], "core.untrackedCache") ||
		!strings.Contains(warnings[1], "(fsmonitor: enabled)") {
		t.Errorf("warnings[1] = %q", warnings[1])
	}
}

func TestStartupWarningsSparseWithUntrackedCache(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", nil)
	backend.repoMode = repoSparseCheckout
	backend.untrackedCache = true

	warnings := backend.StartupWarnings()

	if len(warnings) != 1 || warnings[0] != "Sparse checkout detected; using Git CLI backend." {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestStartupWarningsSparseFsmonitorNotEnabled(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", nil)
	backend.repoMode = repoSparseCheckout

	warnings := backend.StartupWarnings()

	if len(warnings) != 2 || !strings.Contains(warnings[1], "(fsmonitor: not enabled)") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func discoverWith(t *testing.T, responses map[string]response) (*Backend, *fakeRunner) {
	t.Helper()
	runner := &fakeRunner{t: t, responses: responses}
	backend, err := Discover("/cwd", vcs.WhitespaceNormal, runner)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	return backend, runner
}

func TestParseRuntimeFlags(t *testing.T) {
	tests := []struct {
		output         string
		untrackedCache bool
		fsmonitor      bool
	}{
		{"core.untrackedcache true\ncore.fsmonitor .git/hooks/fsmonitor-watchman\n", true, true},
		{"feature.manyfiles true\n", true, false},
		{"feature.manyfiles true\ncore.untrackedcache keep\n", false, false},
		{"", false, false},
	}
	for _, tt := range tests {
		untracked, fsmonitor := parseRuntimeFlags(tt.output)
		if untracked != tt.untrackedCache || fsmonitor != tt.fsmonitor {
			t.Errorf("parseRuntimeFlags(%q) = (%v, %v), want (%v, %v)",
				tt.output, untracked, fsmonitor, tt.untrackedCache, tt.fsmonitor)
		}
	}
}

func TestRepoModeFromConfig(t *testing.T) {
	tests := []struct {
		output string
		want   repoMode
	}{
		{"", repoStandard},
		{"core.sparsecheckout true\n", repoSparseCheckout},
		{"core.sparsecheckout true\nindex.sparse true\n", repoSparseIndex},
		{"core.sparsecheckout false\n", repoStandard},
	}
	for _, tt := range tests {
		if got := repoModeFromConfig(tt.output); got != tt.want {
			t.Errorf("repoModeFromConfig(%q) = %v, want %v", tt.output, got, tt.want)
		}
	}
}

func TestGitFsmonitorConfigEnabled(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"true", true},
		{".git/hooks/fsmonitor-watchman", true},
		{"false", false},
		{"off", false},
		{"", false},
		{"0", false},
	}
	for _, tt := range tests {
		if got := gitFsmonitorConfigEnabled(tt.value); got != tt.want {
			t.Errorf("gitFsmonitorConfigEnabled(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestGitCommandErrorMapping(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git rev-parse HEAD": {stderr: "fatal: broken\n", err: exitError(128)},
		"git merge-base a b": {err: errors.New("executable not found")},
	})

	_, err := backend.git("rev-parse", "HEAD")
	var vcsErr *errs.VcsCommand
	if !errors.As(err, &vcsErr) || vcsErr.Detail != "fatal: broken" {
		t.Fatalf("exit error mapped to %v", err)
	}

	_, err = backend.git("merge-base", "a", "b")
	if !errors.As(err, &vcsErr) || !strings.Contains(vcsErr.Detail, "Failed to run git: executable not found") {
		t.Fatalf("spawn error mapped to %v", err)
	}
}
