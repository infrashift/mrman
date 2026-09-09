# mrman — build tooling
BINARY      := mrman
MODULE      := github.com/infrashift/mrman
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(DATE)
GOFLAGS     := -trimpath
BIN_DIR     := bin
DIST_DIR    := dist
COVER_FILE  := coverage.out
PLATFORMS   := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

# Tool locations that survive minimal PATHs: gofmt ships in GOROOT, installed
# dev tools land in GOBIN (default GOPATH/bin).
GOFMT         := $(shell go env GOROOT)/bin/gofmt
GOBIN_DIR     := $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)
GOIMPORTS     := $(GOBIN_DIR)/goimports
GOLANGCI_LINT := $(GOBIN_DIR)/golangci-lint
GOVULNCHECK   := $(GOBIN_DIR)/govulncheck

.DEFAULT_GOAL := build

.PHONY: all build install run test test-race cover cover-html cover-check bench lint fmt vet vuln tidy \
        generate check check-charmkit package clean tools help docs-dev docs-build

all: check build

build: ## Build the mrman binary into ./bin
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) .

install: ## Install mrman into GOBIN
	go install $(GOFLAGS) -ldflags '$(LDFLAGS)' .

run: build ## Build and run the TUI
	./$(BIN_DIR)/$(BINARY)

test: ## Run unit tests
	go test ./...

check-charmkit: ## Build, vet and test the nested charmkit module
	cd charmkit && go vet ./... && go test ./...

test-race: ## Run tests with the race detector
	go test -race ./...

cover: ## Run tests with coverage profile
	go test -coverprofile=$(COVER_FILE) -covermode=atomic ./...
	go tool cover -func=$(COVER_FILE) | tail -1

cover-html: cover ## Open HTML coverage report
	go tool cover -html=$(COVER_FILE)

COVER_MIN ?= 85
cover-check: cover ## Fail if total coverage < COVER_MIN (default 85%)
	@total=$$(go tool cover -func=$(COVER_FILE) | awk '/^total:/ {sub(/%/,"",$$3); print $$3}'); \
	echo "total coverage: $$total% (minimum $(COVER_MIN)%)"; \
	awk -v t=$$total -v m=$(COVER_MIN) 'BEGIN { exit (t+0 >= m+0) ? 0 : 1 }' || \
		{ echo "FAIL: coverage $$total% is below $(COVER_MIN)%"; exit 1; }

bench: ## Run benchmarks
	go test -bench=. -benchmem -run=^$$ ./...

lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

fmt: ## gofmt + goimports over the tree
	$(GOFMT) -w main.go internal
	$(GOIMPORTS) -local $(MODULE) -w main.go internal

vet: ## go vet (host, plus a Windows cross-vet so the platform stays buildable)
	go vet ./...
	GOOS=windows go vet ./...

vuln: ## govulncheck over both modules
	$(GOVULNCHECK) ./...
	cd charmkit && $(GOVULNCHECK) ./...

tidy: ## go mod tidy, fail if it changes anything (CI-friendly)
	go mod tidy
	git diff --exit-code go.mod go.sum

generate: ## go generate (CUE schema embedding, etc.)
	go generate ./...

check: fmt vet lint test cover-check check-charmkit ## Everything CI runs (lint + tests + 85% coverage gate)

## Docs

docs-dev: ## Run the Astro documentation site locally (bun)
	cd docs && bun install && bun --bun run dev

docs-build: ## Build the documentation site into docs/dist
	cd docs && bun install --frozen-lockfile && bun --bun run build

package: ## Cross-compile release tarballs/zips into ./dist
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		out=$(DIST_DIR)/$(BINARY)_$(VERSION)_$${os}_$${arch}; \
		echo "-> $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
			go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $$out/$(BINARY)$$ext . || exit 1; \
		cp README.md LICENSE NOTICE $$out/ || exit 1; \
		if [ "$$os" = "windows" ]; then \
			(cd $(DIST_DIR) && zip -qr $$(basename $$out).zip $$(basename $$out)); \
		else \
			tar -czf $$out.tar.gz -C $(DIST_DIR) $$(basename $$out); \
		fi; \
		rm -rf $$out; \
	done
	@(cd $(DIST_DIR) && sha256sum * > SHA256SUMS 2>/dev/null || shasum -a 256 * > SHA256SUMS)

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) $(DIST_DIR) $(COVER_FILE)
	go clean

tools: ## Install dev tools (golangci-lint, goimports, govulncheck)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

# charmkit is a nested module (github.com/infrashift/mrman/charmkit) so its
# consumers do not inherit mrman's dependency tree.
#
# go.work is committed, so every clone and every make target here builds.
# What does NOT build is mrman with the workspace disabled — `GOWORK=off go
# build ./...`, and therefore `go install github.com/infrashift/mrman@vX.Y.Z`
# — because go.mod cannot require a charmkit version that has never been
# tagged. Releasing means, in order:
#
#     git tag charmkit/vX.Y.Z && git push origin charmkit/vX.Y.Z
#     go mod edit -require=github.com/infrashift/mrman/charmkit@vX.Y.Z
#     git commit go.mod && git tag vX.Y.Z && git push --tags
#
# After that first release, `GOWORK=off go build ./...` should pass; treat a
# failure there as a release blocker, not a local-setup problem.
