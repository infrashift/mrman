package vcs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/infrashift/mrman/internal/errs"
)

// Runner executes VCS subprocesses. The seam lets backend tests inject
// canned outputs instead of real repositories.
type Runner interface {
	Run(dir string, name string, args ...string) (stdout, stderr []byte, err error)
}

// SystemRunner executes commands with os/exec.
type SystemRunner struct{}

// Run executes name with args in dir, capturing both streams.
func (SystemRunner) Run(dir string, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// CommandError converts a failed invocation into a VcsCommand error whose
// message combines stderr and stdout — some tools (notably `gh api`) write
// the useful error body to stdout with only a status line on stderr.
func CommandError(name string, args []string, stdout, stderr []byte, err error) error {
	var exitErr *exec.ExitError
	detail := combineStreams(stdout, stderr)
	switch {
	case errors.As(err, &exitErr):
		return &errs.VcsCommand{Detail: fmt.Sprintf("%s %s exited %d: %s",
			name, strings.Join(args, " "), exitErr.ExitCode(), detail)}
	case err != nil:
		if detail != "" {
			return &errs.VcsCommand{Detail: fmt.Sprintf("%s: %v: %s", name, err, detail)}
		}
		return &errs.VcsCommand{Detail: fmt.Sprintf("%s: %v", name, err)}
	}
	return nil
}

func combineStreams(stdout, stderr []byte) string {
	out := strings.TrimSpace(string(stderr))
	if extra := strings.TrimSpace(string(stdout)); extra != "" {
		if out != "" {
			out += "\n"
		}
		out += extra
	}
	return out
}
