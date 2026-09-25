// Package livetest resolves the target and configuration for mrman's opt-in
// live tests — the ones that run against a real forge instead of a fixture.
//
// Every live test used to carry its own owner/repo#N parser hard-wired to
// github.com and to config.Default(), so none of them could reach a
// self-hosted forge: no [[forge.hosts]] entry, no api_base, no token_cmd, no
// CA. This package parses MRMAN_LIVE_PR with the same parser `mrman pr`
// uses and loads the same config file, so a live test sees exactly what the
// binary would.
//
//	MRMAN_LIVE_PR=owner/repo#N                                  GitHub
//	MRMAN_LIVE_PR=https://host/group/sub/repo/-/merge_requests/N GitLab
//	MRMAN_LIVE_PR=host/group/repo!N                             GitLab, configured host
//	MRMAN_LIVE_SUBMIT=1                                         also run the tests that write
package livetest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// Environment variables that opt a test run into the live suite.
const (
	// EnvTarget names the pull or merge request the live tests run against.
	EnvTarget = "MRMAN_LIVE_PR"
	// EnvSubmit, when non-empty, also runs the live tests that post.
	EnvSubmit = "MRMAN_LIVE_SUBMIT"
)

// Parse resolves a live-test target through forge.ParseTarget, so every
// shape `mrman pr` accepts works here, and host-over-shape overrides from
// cfg apply.
func Parse(target string, cfg config.ForgeConfig) (forgetypes.Repository, uint64, error) {
	parsed, err := forge.ParseTarget(target, nil, cfg)
	if err != nil {
		return forgetypes.Repository{}, 0, err
	}
	if parsed.Repository == nil {
		return forgetypes.Repository{}, 0, fmt.Errorf("%s=%q names no repository", EnvTarget, target)
	}
	if parsed.Number == 0 {
		return forgetypes.Repository{}, 0, errors.New(EnvTarget + " must name a pull request number")
	}
	return *parsed.Repository, parsed.Number, nil
}

// Config loads the user's configuration — $XDG_CONFIG_HOME/mrman/config.toml
// — and returns its forge section. Warnings are logged, not fatal, exactly as
// the binary treats them.
func Config(t testing.TB) config.ForgeConfig {
	t.Helper()
	return configFrom(t, filepath.Join(config.Dir(), "config.toml"))
}

func configFrom(t testing.TB, path string) config.ForgeConfig {
	t.Helper()
	cfg, warnings := config.LoadFrom(path)
	for _, w := range warnings {
		t.Logf("config: %s", w)
	}
	return cfg.Forge
}

// Target skips the test unless MRMAN_LIVE_PR is set, and otherwise returns
// the repository and number it names.
func Target(t testing.TB, cfg config.ForgeConfig) (forgetypes.Repository, uint64) {
	t.Helper()
	target := os.Getenv(EnvTarget)
	if target == "" {
		t.Skip("set " + EnvTarget + " to a pull request (owner/repo#N or a merge request URL) to run against a real forge")
	}
	repo, number, err := Parse(target, cfg)
	if err != nil {
		t.Fatalf("%s=%q: %v", EnvTarget, target, err)
	}
	return repo, number
}

// RequireKind skips a driver-specific live test when the target belongs to
// another forge, so one MRMAN_LIVE_PR can drive `go test ./...`.
func RequireKind(t testing.TB, repo forgetypes.Repository, kind forgetypes.Kind) {
	t.Helper()
	if repo.Kind != kind {
		t.Skipf("%s targets %s, this test needs %s", EnvTarget, repo.Kind, kind)
	}
}

// RequireSubmit skips a test that posts to the forge unless
// MRMAN_LIVE_SUBMIT is set.
func RequireSubmit(t testing.TB) {
	t.Helper()
	if os.Getenv(EnvSubmit) == "" {
		t.Skip("set " + EnvSubmit + "=1 as well to post a real review")
	}
}
