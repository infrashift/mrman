package slug

import (
	"errors"
	"testing"

	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/model"
)

// fakeRunner returns canned git output (or a canned failure) so owner/repo
// resolution is tested without a real git checkout.
type fakeRunner struct {
	out string
	err error
}

func (f fakeRunner) Run(string, string, ...string) ([]byte, []byte, error) {
	if f.err != nil {
		return nil, []byte("fatal: not a git repository"), f.err
	}
	return []byte(f.out), nil, nil
}

// originRunner is a fakeRunner serving a github origin URL for agavra/tuicr.
var originRunner = fakeRunner{out: "https://github.com/agavra/tuicr.git\n"}

// swapForSessionRunner overrides the Runner ForSession uses for the duration
// of a test.
func swapForSessionRunner(t *testing.T, r Runner) {
	t.Helper()
	old := forSessionRunner
	forSessionRunner = r
	t.Cleanup(func() { forSessionRunner = old })
}

func strPtr(s string) *string { return &s }

// localSession builds a minimal local review session for derivation tests.
func localSession(branch *string, baseCommit string, source model.SessionDiffSource, commitRange []string) *model.ReviewSession {
	return &model.ReviewSession{
		RepoPath:    "/home/user/src/tuicr",
		BranchName:  branch,
		BaseCommit:  baseCommit,
		DiffSource:  source,
		CommitRange: commitRange,
	}
}

// ---------- ResolveOwnerRepo ----------

func TestResolveOwnerRepo(t *testing.T) {
	tests := []struct {
		name      string
		runner    Runner
		repoPath  string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "https remote url",
			runner:    fakeRunner{out: "https://github.com/agavra/tuicr.git\n"},
			repoPath:  "/tmp/checkout",
			wantOwner: "agavra",
			wantRepo:  "tuicr",
		},
		{
			name:      "https remote url without dot git suffix",
			runner:    fakeRunner{out: "https://github.com/agavra/tuicr\n"},
			repoPath:  "/tmp/checkout",
			wantOwner: "agavra",
			wantRepo:  "tuicr",
		},
		{
			name:      "http remote url",
			runner:    fakeRunner{out: "http://git.internal/team/proj.git\n"},
			repoPath:  "/tmp/checkout",
			wantOwner: "team",
			wantRepo:  "proj",
		},
		{
			name:      "scp-like ssh remote url",
			runner:    fakeRunner{out: "git@github.com:agavra/tuicr.git\n"},
			repoPath:  "/tmp/checkout",
			wantOwner: "agavra",
			wantRepo:  "tuicr",
		},
		{
			name:      "ssh scheme remote url",
			runner:    fakeRunner{out: "ssh://git@github.com/agavra/tuicr.git\n"},
			repoPath:  "/tmp/checkout",
			wantOwner: "agavra",
			wantRepo:  "tuicr",
		},
		{
			name:      "nested subgroups pick last two segments",
			runner:    fakeRunner{out: "git@gitlab.com:org/team/svc.git\n"},
			repoPath:  "/tmp/checkout",
			wantOwner: "team",
			wantRepo:  "svc",
		},
		{
			name:      "unparseable remote falls back to basename",
			runner:    fakeRunner{out: "not-a-url\n"},
			repoPath:  "/home/user/src/tuicr",
			wantOwner: "",
			wantRepo:  "tuicr",
		},
		{
			name:      "empty remote output falls back to basename",
			runner:    fakeRunner{out: "\n"},
			repoPath:  "/home/user/src/tuicr",
			wantOwner: "",
			wantRepo:  "tuicr",
		},
		{
			name:      "git failure falls back to basename",
			runner:    fakeRunner{err: errors.New("exit status 1")},
			repoPath:  "/home/user/src/tuicr",
			wantOwner: "",
			wantRepo:  "tuicr",
		},
		{
			name:      "nil runner falls back to basename",
			runner:    nil,
			repoPath:  "/home/user/src/tuicr",
			wantOwner: "",
			wantRepo:  "tuicr",
		},
		{
			name:      "trailing slash on repo path",
			runner:    nil,
			repoPath:  "/home/user/src/tuicr/",
			wantOwner: "",
			wantRepo:  "tuicr",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ResolveOwnerRepo(tt.repoPath, tt.runner)
			if err != nil {
				t.Fatalf("ResolveOwnerRepo error: %v", err)
			}
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("ResolveOwnerRepo = (%q, %q), want (%q, %q)",
					owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestResolveOwnerRepoRejectsPathsWithoutBasename(t *testing.T) {
	for _, path := range []string{"/", ".", "..", ""} {
		_, _, err := ResolveOwnerRepo(path, fakeRunner{err: errors.New("no git")})
		if !errors.Is(err, ErrNoRepoName) {
			t.Errorf("ResolveOwnerRepo(%q) error = %v, want ErrNoRepoName", path, err)
		}
	}
}

func TestParseRemoteOwnerRepoRejectsGarbage(t *testing.T) {
	for _, url := range []string{"not-a-url", "", "host-only", "host/onlyrepo"} {
		if _, _, ok := parseRemoteOwnerRepo(url); ok {
			t.Errorf("parseRemoteOwnerRepo(%q) succeeded, want failure", url)
		}
	}
}

func TestExecRunnerCapturesOutput(t *testing.T) {
	stdout, stderr, err := execRunner{}.Run(t.TempDir(), "sh", "-c", "echo out; echo err 1>&2")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if got := string(stdout); got != "out\n" {
		t.Errorf("stdout = %q, want %q", got, "out\n")
	}
	if got := string(stderr); got != "err\n" {
		t.Errorf("stderr = %q, want %q", got, "err\n")
	}
	if _, _, err := (execRunner{}).Run(t.TempDir(), "sh", "-c", "exit 3"); err == nil {
		t.Error("Run with failing command succeeded, want error")
	}
}

// ---------- ForSession: local sessions ----------

func TestForSessionWorktreeFromBranch(t *testing.T) {
	swapForSessionRunner(t, originRunner)
	slug, err := ForSession(localSession(strPtr("main"), "abcdef0123", model.SourceWorkingTree, nil))
	if err != nil {
		t.Fatalf("ForSession error: %v", err)
	}
	if got, want := slug.String(), "agavra/tuicr@main/worktree/abcdef0"; got != want {
		t.Errorf("slug = %q, want %q", got, want)
	}
}

func TestForSessionSanitizesBranch(t *testing.T) {
	swapForSessionRunner(t, originRunner)
	slug, err := ForSession(localSession(strPtr("feature/login"), "abcdef0123", model.SourceWorkingTree, nil))
	if err != nil {
		t.Fatalf("ForSession error: %v", err)
	}
	if got, want := slug.String(), "agavra/tuicr@feature-login/worktree/abcdef0"; got != want {
		t.Errorf("slug = %q, want %q", got, want)
	}
}

func TestForSessionAnonymousAnchorWhenBranchNil(t *testing.T) {
	swapForSessionRunner(t, originRunner)
	slug, err := ForSession(localSession(nil, "abcdef0123456789", model.SourceWorkingTree, nil))
	if err != nil {
		t.Fatalf("ForSession error: %v", err)
	}
	if got, want := slug.String(), "agavra/tuicr@~abcdef0/worktree/abcdef0"; got != want {
		t.Errorf("slug = %q, want %q", got, want)
	}
}

func TestForSessionChangesWhenHeadAdvances(t *testing.T) {
	// Regression for tuicr #378: two runs on the same branch but different
	// HEADs must produce distinct slugs so the persisted session from the
	// previous HEAD does not leak its comments into the new run.
	swapForSessionRunner(t, originRunner)
	before, err := ForSession(localSession(strPtr("main"), "abcdef0123", model.SourceWorkingTree, nil))
	if err != nil {
		t.Fatalf("ForSession error: %v", err)
	}
	after, err := ForSession(localSession(strPtr("main"), "9999999aaa", model.SourceWorkingTree, nil))
	if err != nil {
		t.Fatalf("ForSession error: %v", err)
	}
	if before.String() == after.String() {
		t.Errorf("slugs did not change with HEAD: %q", before.String())
	}
	if got, want := before.String(), "agavra/tuicr@main/worktree/abcdef0"; got != want {
		t.Errorf("before = %q, want %q", got, want)
	}
	if got, want := after.String(), "agavra/tuicr@main/worktree/9999999"; got != want {
		t.Errorf("after = %q, want %q", got, want)
	}
}

func TestForSessionUnbornHeadUsesNoneToken(t *testing.T) {
	swapForSessionRunner(t, fakeRunner{err: errors.New("no origin")})
	slug, err := ForSession(localSession(strPtr("main"), "", model.SourceWorkingTree, nil))
	if err != nil {
		t.Fatalf("ForSession error: %v", err)
	}
	if got, want := slug.String(), "tuicr@main/worktree/none"; got != want {
		t.Errorf("slug = %q, want %q", got, want)
	}
}

func TestForSessionLiveAndPristineSources(t *testing.T) {
	swapForSessionRunner(t, originRunner)
	tests := []struct {
		source model.SessionDiffSource
		want   string
	}{
		{model.SourceStaged, "agavra/tuicr@main/staged/abcdef0"},
		{model.SourceUnstaged, "agavra/tuicr@main/unstaged/abcdef0"},
		{model.SourceStagedAndUnstaged, "agavra/tuicr@main/staged-and-unstaged/abcdef0"},
		{model.SourcePristine, "agavra/tuicr@main/pristine"},
	}
	for _, tt := range tests {
		slug, err := ForSession(localSession(strPtr("main"), "abcdef0123", tt.source, nil))
		if err != nil {
			t.Fatalf("ForSession(%s) error: %v", tt.source, err)
		}
		if got := slug.String(); got != tt.want {
			t.Errorf("ForSession(%s) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

func TestForSessionRangeSources(t *testing.T) {
	swapForSessionRunner(t, originRunner)
	// commit_range is stored newest-first: head = first, base = last.
	commitRange := []string{"def5678aaa", "intermediate", "abc1234bbb"}
	tests := []struct {
		source model.SessionDiffSource
		want   string
	}{
		{model.SourceCommitRange, "agavra/tuicr@main/commits/abc1234..def5678"},
		{model.SourceWorkingTreeAndCommits, "agavra/tuicr@main/worktree-and-commits/abc1234..def5678"},
		{model.SourceStagedUnstagedAndCommits, "agavra/tuicr@main/staged-and-unstaged-and-commits/abc1234..def5678"},
	}
	for _, tt := range tests {
		slug, err := ForSession(localSession(strPtr("main"), "def5678", tt.source, commitRange))
		if err != nil {
			t.Fatalf("ForSession(%s) error: %v", tt.source, err)
		}
		if got := slug.String(); got != tt.want {
			t.Errorf("ForSession(%s) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

func TestForSessionRoundTripsThroughParse(t *testing.T) {
	swapForSessionRunner(t, originRunner)
	slug, err := ForSession(localSession(strPtr("feature/login"), "abcdef0123", model.SourceWorkingTree, nil))
	if err != nil {
		t.Fatalf("ForSession error: %v", err)
	}
	assertRoundtrip(t, slug.String())
}

func TestForSessionErrors(t *testing.T) {
	swapForSessionRunner(t, originRunner)
	tests := []struct {
		name    string
		session *model.ReviewSession
		want    error
	}{
		{"nil session", nil, ErrNilSession},
		{
			"missing commit range",
			localSession(strPtr("main"), "def5678", model.SourceCommitRange, nil),
			ErrMissingCommitRange,
		},
		{
			"empty commit range",
			localSession(strPtr("main"), "def5678", model.SourceWorkingTreeAndCommits, []string{}),
			ErrMissingCommitRange,
		},
		{
			"pull request session without key",
			localSession(strPtr("main"), "def5678", model.SourcePullRequest, nil),
			ErrMissingPrSessionKey,
		},
		{
			"unknown diff source",
			localSession(strPtr("main"), "def5678", model.SessionDiffSource("bogus"), nil),
			ErrUnsupportedDiffSource,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ForSession(tt.session)
			if !errors.Is(err, tt.want) {
				t.Errorf("ForSession error = %v, want errors.Is(%v)", err, tt.want)
			}
		})
	}
}

func TestForSessionNoRepoNamePropagates(t *testing.T) {
	swapForSessionRunner(t, fakeRunner{err: errors.New("no git")})
	session := localSession(strPtr("main"), "abc", model.SourceWorkingTree, nil)
	session.RepoPath = "/"
	if _, err := ForSession(session); !errors.Is(err, ErrNoRepoName) {
		t.Errorf("ForSession error = %v, want ErrNoRepoName", err)
	}
}

func TestForSessionDefaultRunnerFallsBackOutsideGit(t *testing.T) {
	// Exercises the real execRunner path: git fails in a nonexistent
	// directory, so the slug falls back to the directory basename.
	session := localSession(strPtr("main"), "abcdef0123", model.SourceWorkingTree, nil)
	session.RepoPath = "/nonexistent/path/to/myrepo"
	slug, err := ForSession(session)
	if err != nil {
		t.Fatalf("ForSession error: %v", err)
	}
	if got, want := slug.String(), "myrepo@main/worktree/abcdef0"; got != want {
		t.Errorf("slug = %q, want %q", got, want)
	}
}

// ---------- ForSession: PR sessions ----------

func prSession(key *forgetypes.PrSessionKey) *model.ReviewSession {
	return &model.ReviewSession{
		RepoPath:     "/home/user/src/tuicr",
		DiffSource:   model.SourcePullRequest,
		PrSessionKey: key,
	}
}

func TestForSessionPrSlugs(t *testing.T) {
	tests := []struct {
		name string
		key  forgetypes.PrSessionKey
		want string
	}{
		{
			name: "github",
			key: forgetypes.PrSessionKey{
				Repository: forgetypes.Repository{
					Kind:  forgetypes.KindGitHub,
					Host:  "github.com",
					Owner: "agavra",
					Name:  "tuicr",
				},
				Number:  125,
				HeadSHA: "abcdef0123456789",
			},
			want: "gh:github.com/agavra/tuicr/pr/125",
		},
		{
			name: "gitlab subgroups",
			key: forgetypes.PrSessionKey{
				Repository: forgetypes.Repository{
					Kind:  forgetypes.KindGitLab,
					Host:  "gitlab.example.com",
					Owner: "group/subgroup",
					Name:  "proj",
				},
				Number: 34,
			},
			want: "gl:gitlab.example.com/group/subgroup/proj/pr/34",
		},
		{
			name: "azure devops org/project/repo",
			key: forgetypes.PrSessionKey{
				Repository: forgetypes.Repository{
					Kind:    forgetypes.KindAzureDevOps,
					Host:    "dev.azure.com",
					Owner:   "org",
					Project: "project",
					Name:    "repo",
				},
				Number: 7,
			},
			want: "ado:dev.azure.com/org/project/repo/pr/7",
		},
		{
			name: "forgejo",
			key: forgetypes.PrSessionKey{
				Repository: forgetypes.Repository{
					Kind:  forgetypes.KindForgejo,
					Host:  "codeberg.org",
					Owner: "owner",
					Name:  "repo",
				},
				Number: 9,
			},
			want: "fj:codeberg.org/owner/repo/pr/9",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug, err := ForSession(prSession(&tt.key))
			if err != nil {
				t.Fatalf("ForSession error: %v", err)
			}
			if got := slug.String(); got != tt.want {
				t.Errorf("slug = %q, want %q", got, tt.want)
			}
			assertRoundtrip(t, tt.want)
		})
	}
}
