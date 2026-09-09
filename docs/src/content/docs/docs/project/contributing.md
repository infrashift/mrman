---
title: Contributing
description: Repository layout, the quality gates make check enforces, the charmkit nested module, and how a release is cut.
---

## Getting set up

```sh
git clone https://github.com/infrashift/mrman
cd mrman
make tools     # golangci-lint, goimports
make check     # the full gate
```

`make help` lists every target.

## The quality gate

```sh
make check
```

Runs, in order: `fmt` (gofmt + goimports) → `vet` (host and a `GOOS=windows`
cross-vet) → `lint` (golangci-lint, including `gosec`) → `test` →
`cover-check` → `check-charmkit`. `make vuln` runs `govulncheck` over both
modules; it is not part of `check` because it needs the network.

The same gate runs in CI (`.github/workflows/ci.yml`) on every push and pull
request, on Linux and macOS, plus a Windows build and `govulncheck`. A pull
request that fails it is not mergeable.

**Coverage is gated at 85%** and the gate is not advisory — `make check` fails
below it:

```sh
make cover-check              # default minimum
make cover-check COVER_MIN=90 # raise it locally
make cover-html               # open the report and find the gap
```

Other targets worth knowing:

| Target | Does |
|---|---|
| `make build` | `./bin/mrman` |
| `make run` | Build and launch the TUI |
| `make test-race` | Tests under the race detector |
| `make bench` | Benchmarks |
| `make tidy` | `go mod tidy`, failing if it changes anything |
| `make package` | Cross-compiled release artifacts into `./dist` |
| `make docs-dev` | This documentation site, live-reloading |
| `make docs-build` | Production build of this site |

## Repository layout

The codebase mirrors tuicr's layout under `internal/`, and tuicr's own test suite
is ported throughout as the behavioral parity spec. If you are unsure what
mrman's behavior *should* be, the ported test is usually the answer.

| Path | Holds |
|---|---|
| `internal/app/` | Review state: diff model, comments, gaps, commits, reviewed marks |
| `internal/ui/` | The Bubble Tea model, rendering and command dispatch |
| `internal/input/` | Key and `:` command parsing |
| `internal/forge/` | The four-forge layer — see below |
| `internal/vcs/` | git, Jujutsu, file, pristine, patch, two-path and merge-request (no-op) backends |
| `internal/config/` | TOML loading and CUE validation |
| `internal/theme/` | Bundled and local themes, chroma styles |
| `internal/output/` | Markdown export and review-body templating |
| `internal/persistence/` | Session storage |
| `internal/reviewcli/` | The `mrman review` JSON surface |
| `internal/agentsubmit/` | The agent-submit grant and interlock |
| `internal/cli/` | The command line: cobra tree, flag rules, `--auto` parsing |
| `internal/model/` | Sessions, comments, diff types — the persisted shapes |
| `internal/patch/` | `.patch`, `.diff` and mbox loading, header parsing, path normalisation |
| `internal/prload/` | The network round of opening a merge request, shared by TUI and CLI |
| `internal/slug/` | Session slugs and repository coordinates |
| `internal/diffgen/` | Myers diff for forges that serve no patch (Azure DevOps) and `mrman diff` |
| `internal/editor/` | `$EDITOR` invocations for `:edit` |
| `internal/errs/` | The error taxonomy every layer shares |
| `internal/ignore/` | `.gitignore` / `.mrmanignore` filtering |
| `internal/syntax/` | chroma highlighting |
| `internal/textsafe/` | Scrubbing terminal control sequences from untrusted text |
| `internal/version/` | Build metadata for `--version` |
| `charmkit/` | Reusable Bubble Tea layers, a separate module |
| `skills/mrman/` | The packaged agent skill and multiplexer wrappers |

### The forge layer

```
internal/forge/
  forge.go          neutral types and the Driver interface
  capabilities.go   the capability struct every driver declares
  registry.go       host → driver resolution, target parsing
  auth.go           per-host token resolution
  submit/           comment→position mapping, downgrades, review body
  githubf/  gitlabf/  azdof/  forgejof/     the four drivers
```

A driver declares what it can do in `Capabilities()`, and the app checks that
before offering an action. **If you change a capability, update
[Forge Capabilities](../../reference/forge-capabilities/) and the relevant forge
page** — those pages document the `false` values as user-visible behavior, so a
drift there is a documentation bug that reads as a product bug.

## charmkit

Three layers other Bubble Tea projects can reuse live in the nested
[`charmkit`](https://github.com/infrashift/mrman/tree/main/charmkit) module:

| Package | Provides |
|---|---|
| `vimtext` | Modal text editing |
| `keychord` | vim chord and count resolution |
| `cellrender` | A terminal-cell text layer with horizontal scrolling and wrap-safe background overlays |

It is a separate module so consumers do not inherit mrman's dependency tree.
`go.work` is committed, so **every clone and every `make` target builds**.

What does *not* build is mrman with the workspace disabled — `GOWORK=off go build
./...`, and therefore `go install github.com/infrashift/mrman@vX.Y.Z` — because
`go.mod` cannot require a `charmkit` version that has never been tagged.

### Releasing

Order matters:

```sh
git tag charmkit/vX.Y.Z && git push origin charmkit/vX.Y.Z
go mod edit -require=github.com/infrashift/mrman/charmkit@vX.Y.Z
git commit go.mod && git tag vX.Y.Z && git push --tags
```

After that first release, `GOWORK=off go build ./...` should pass. Treat a failure
there as a release blocker, not a local-setup problem.

## Testing against a real forge

Every unit test runs against fakes. Fakes cannot catch a driver that builds a
request the forge then rejects, or a pane that renders correctly from invented
data and wrongly from real data.

Three opt-in tests exercise a real forge. They skip unless their environment
variables are set, so `make check` is unaffected:

```sh
MRMAN_LIVE_PR=owner/repo#1 go test ./internal/forge/githubf/ -run Live -v
MRMAN_LIVE_PR=owner/repo#1 go test ./internal/ui/ -run LivePullRequestReview -v
MRMAN_LIVE_PR=owner/repo#1 MRMAN_LIVE_SUBMIT=1 \
    go test ./internal/ui/ -run LivePullRequestSubmit -v   # posts a real review
```

[Testing Against a Real Forge](../../contributing/live-testing/) walks through
setting up a scratch merge request to point them at, with real transcripts of what
each one prints — including what live testing caught that fakes did not.

## Documentation

This site lives in `docs/` as an Astro + Starlight project, built with bun.

```sh
make docs-dev      # http://localhost:4321/mrman/
make docs-build
```

Pages are markdown under `docs/src/content/docs/docs/`. The doubled `docs/docs/`
is deliberate: `docs/src/pages/index.astro` owns the site root for the landing
page, so Starlight's content sits one level down. A page's sidebar slug in
`docs/astro.config.mjs` is its path without the extension —
`docs/reference/keybindings` for
`docs/src/content/docs/docs/reference/keybindings.md`.

Every page is deployed from `main` by `.github/workflows/docs-release.yml`.

## Filing issues and MRs

- Behavior questions: check whether a ported tuicr test already pins the answer.
- Forge behavior: include `mrman --version`, the forge and host kind, and the
  exact error text.
- New keybindings: they need an entry in
  [Keybindings](../../reference/keybindings/) and in the in-app `?` popup, which
  are separate sources.
