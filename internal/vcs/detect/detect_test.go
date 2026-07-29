package detect

import (
	"errors"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/vcs"
)

// probeRunner cans responses per "name arg1 arg2 ..." key. Commands without
// a canned response fail, which both jj.Discover (for `jj root`) and
// git.Discover (for `rev-parse --show-toplevel`) treat as "not a repo",
// while their secondary info lookups degrade gracefully.
type probeRunner struct {
	responses map[string]string
}

func (r *probeRunner) Run(_ string, name string, args ...string) ([]byte, []byte, error) {
	key := name + " " + strings.Join(args, " ")
	out, ok := r.responses[key]
	if !ok {
		return nil, []byte("no canned response for: " + key), errors.New("exit status 1")
	}
	return []byte(out), nil, nil
}

func TestJujutsuBeatsGitWhenBothProbeOK(t *testing.T) {
	// jj repos are git-backed: both probes would succeed, jj must win.
	run := &probeRunner{responses: map[string]string{
		"jj root":                       "/repo\n",
		"git rev-parse --show-toplevel": "/repo\n",
	}}
	backend, err := Detect("/repo/sub", vcs.WhitespaceNormal, run)
	if err != nil {
		t.Fatal(err)
	}
	if got := backend.Info().Type; got != vcs.TypeJujutsu {
		t.Fatalf("type = %q, want %q", got, vcs.TypeJujutsu)
	}
}

func TestGitWinsWhenJujutsuFails(t *testing.T) {
	run := &probeRunner{responses: map[string]string{
		"git rev-parse --show-toplevel": "/repo\n",
	}}
	backend, err := Detect("/repo/sub", vcs.WhitespaceNormal, run)
	if err != nil {
		t.Fatal(err)
	}
	if got := backend.Info().Type; got != vcs.TypeGit {
		t.Fatalf("type = %q, want %q", got, vcs.TypeGit)
	}
	if backend.Info().RootPath != "/repo" {
		t.Fatalf("root = %q", backend.Info().RootPath)
	}
}

func TestNotARepositoryWhenAllProbesFail(t *testing.T) {
	run := &probeRunner{responses: map[string]string{}}
	if _, err := Detect("/nowhere", vcs.WhitespaceNormal, run); !errors.Is(err, errs.ErrNotARepository) {
		t.Fatalf("expected ErrNotARepository, got %v", err)
	}
}
