package prnoop

import (
	"errors"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// Compile-time check that Backend satisfies the full interface.
var _ vcs.Backend = (*Backend)(nil)

func TestNewInfoValues(t *testing.T) {
	b := New("forge:github.com/a/b")
	info := b.Info()
	if info.RootPath != "forge:github.com/a/b" {
		t.Fatalf("RootPath = %q", info.RootPath)
	}
	if info.HeadCommit != "" {
		t.Fatalf("HeadCommit = %q, want empty", info.HeadCommit)
	}
	if info.BranchName != nil {
		t.Fatalf("BranchName = %v, want nil", info.BranchName)
	}
	if info.Type != vcs.TypeFile {
		t.Fatalf("Type = %q, want %q", info.Type, vcs.TypeFile)
	}
}

func TestShouldReturnEmptyContextLinesFromNoopBackend(t *testing.T) {
	b := New("forge:github.com/a/b")
	lines, err := b.FetchContextLines("x", model.StatusModified, nil, 1, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("expected no lines, got %d", len(lines))
	}
}

func TestShouldRejectWorkingTreeDiffOnNoopBackend(t *testing.T) {
	b := New("forge:github.com/a/b")
	_, err := b.WorkingTreeDiff(nil)
	if !errors.Is(err, errs.ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
	if want := "MR mode does not read from the local working tree"; err == nil || !contains(err.Error(), want) {
		t.Fatalf("error %v missing %q", err, want)
	}
}

func TestFileLineCountReportsZero(t *testing.T) {
	b := New("root")
	n, err := b.FileLineCount("x", model.StatusModified, nil)
	if err != nil || n != 0 {
		t.Fatalf("got %d, %v; want 0, nil", n, err)
	}
}

func TestEverythingElseIsUnsupported(t *testing.T) {
	b := New("root")
	if _, err := b.StagedDiff(nil); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("StagedDiff must be unsupported")
	}
	if _, err := b.UnstagedDiff(nil); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("UnstagedDiff must be unsupported")
	}
	if _, err := b.ChangeStatus(); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("ChangeStatus must be unsupported")
	}
	if err := b.StageFile("x"); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("StageFile must be unsupported")
	}
	if _, err := b.ResolveRevisionRange("@"); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("ResolveRevisionRange must be unsupported")
	}
	if _, err := b.CommitRangeDiff(vcs.ResolvedRevisionRange{}, nil); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("CommitRangeDiff must be unsupported")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
