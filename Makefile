# icon-sets-sync (ichava/icon-sets-sync) -- Makefile.
#
# Every target runs on the host: Go 1.26 or later to build, and git, gh and
# npm on PATH for a sync. The run targets build build/icon-sets-sync first and
# run it against this checkout's config/. Run `make help`.

SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

# Pack identifier for single-pack targets. Override:
#   make sync PACK=icon-sets-emoji
PACK ?=

# Packs synced at once by sync-all.
CONCURRENCY ?= 1

# The Go module lives in src/, so every go command names it with -C src. A bare
# `go test ./...` at the root finds no packages and passes.
BIN := build/icon-sets-sync

# The run targets: the binary build-local writes, reading this checkout's
# config/. Every shipped config names its pack as /work/<pack>, so /work must
# be the directory holding the pack checkouts (see docs/installation.md).
RUN := $(BIN) --config-dir config

# ---------------------------------------------------------------------------
# Help (auto-generated from `## ` comments after target names)
# ---------------------------------------------------------------------------

.PHONY: help
help:  ## Show this help
	@printf "\nicon-sets-sync -- sync the Ichava icon packs from upstream\n\n"
	@printf "Usage: make <target> [PACK=<name>] [CONCURRENCY=<n>]\n\n"
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
	@printf "\n"

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

.PHONY: build
build: build-local  ## Build $(BIN) (the same as build-local)

.PHONY: pull
pull:  ## Download and verify the Go module dependencies
	go -C src mod download
	go -C src mod verify

.PHONY: shell
shell: build-local  ## Open a shell with build/ on PATH and ICHAVA_CONFIG_DIR set to config/
	PATH="$(CURDIR)/build:$$PATH" ICHAVA_CONFIG_DIR="$(CURDIR)/config" $${SHELL:-bash}

# ---------------------------------------------------------------------------
# The actual work
# ---------------------------------------------------------------------------

.PHONY: menu
menu: build-local  ## Open the interactive menu (default action)
	$(RUN) menu

.PHONY: check
check: build-local  ## Check upstream status for every configured pack (no writes)
	$(RUN) check

.PHONY: check-pack
check-pack: build-local  ## Check upstream status for one pack: PACK=<name>
	@if [ -z "$(PACK)" ]; then echo "PACK= is required (e.g. make check-pack PACK=icon-sets-emoji)"; exit 2; fi
	$(RUN) check --pack=$(PACK)

.PHONY: sync
sync: build-local  ## Sync upstream for one pack (refresh + commit + PR): PACK=<name>
	@if [ -z "$(PACK)" ]; then echo "PACK= is required (e.g. make sync PACK=icon-sets-emoji)"; exit 2; fi
	$(RUN) sync --pack=$(PACK)

.PHONY: sync-all
sync-all: build-local  ## Dry-run every pack: fetch and report the diff, write nothing: CONCURRENCY=<n>
	$(RUN) sync-all --dry-run --concurrency=$(CONCURRENCY)

.PHONY: sync-all-force
sync-all-force: build-local  ## Sync every pack for real (commits + PRs). Be careful.
	$(RUN) sync-all --concurrency=$(CONCURRENCY)

.PHONY: build-emoji
build-emoji: build-local  ## Run the icon-sets-emoji recipe (Twemoji + OpenMoji + Unicode taxonomy)
	$(RUN) recipe icon-sets-emoji

.PHONY: validate
validate: build-local  ## Validate every pack config against config/schema/pack.schema.json
	$(RUN) config validate

.PHONY: config-print
config-print: build-local  ## Print the effective settings and where each came from [PACK=<name>]
	$(RUN) config print --origins $(if $(PACK),--pack=$(PACK),)

# ---------------------------------------------------------------------------
# Dev workflow
# ---------------------------------------------------------------------------

.PHONY: test
test: test-go  ## Run the Go suite

.PHONY: test-go
test-go:  ## Go: vet + test with the host's Go toolchain
	go -C src vet ./...
	go -C src test -count=1 ./...

# golangci-lint runs when it is installed and is skipped with a note when it is
# not, so `make lint` needs nothing beyond Go. CI runs gofmt and vet. It reports
# only issues new since main: the tree carried 19 older ones on 2026-11-04.
# `make lint GOLANGCI_ARGS=` shows them all.
GOLANGCI_ARGS ?= --new-from-rev=main
.PHONY: lint
lint:  ## gofmt check + go vet, and golangci-lint when it is installed
	@test -z "$$(gofmt -l src)" || { echo "gofmt would reformat:"; gofmt -l src; exit 1; }
	go -C src vet ./...
	@if command -v golangci-lint >/dev/null; then \
		echo "golangci-lint run $(GOLANGCI_ARGS) ./..."; cd src && golangci-lint run $(GOLANGCI_ARGS) ./...; \
	else \
		echo "golangci-lint is not installed; skipped (https://golangci-lint.run/welcome/install/)"; \
	fi

.PHONY: format
format:  ## gofmt -w
	gofmt -w src

.PHONY: build-local
build-local:  ## Build $(BIN) with the host's Go toolchain
	go -C src build -trimpath -o ../$(BIN) ./cmd/icon-sets-sync

# The target list is src/.goreleaser.yaml's builds, the one place it is kept,
# so this builds exactly the set a release would, into build/dist/: the flat
# {binary}_{os}_{arch} executables (macOS as `macos`, plus the universal
# binary), the archives and the Linux packages. A snapshot with publishing,
# signing and SBOMs skipped, so nothing leaves the machine.
.PHONY: cross
cross:  ## Build every release target locally into build/dist (needs goreleaser v2)
	@command -v goreleaser >/dev/null || { echo "goreleaser v2 is required: https://goreleaser.com/install/"; exit 2; }
	GOPRIVATE='github.com/ichava/*' GONOSUMDB='github.com/ichava/*' \
		goreleaser release --snapshot --clean --config src/.goreleaser.yaml --skip=publish,sign,sbom

.PHONY: test-local
test-local:  ## go vet + go test with the host's Go toolchain
	go -C src vet ./...
	go -C src test -count=1 ./...

# ---------------------------------------------------------------------------
# Web UI
# ---------------------------------------------------------------------------

.PHONY: serve
serve:  ## Run the web UI (icon-sets-sync serve) with the host's Go toolchain
	go -C src run ./cmd/icon-sets-sync serve

.PHONY: ui
ui: build-local  ## Run the web UI on http://127.0.0.1:7878 (the address it logs has the token)
	$(RUN) serve --port 7878

.PHONY: tabler-check
tabler-check:  ## Say whether a newer Tabler or Tabler Icons release is out (writes nothing)
	go -C src run ./internal/tools/tablervendor -check -root ..

.PHONY: tabler-update
tabler-update:  ## Move the web UI to the newest Tabler + Tabler Icons; re-vendor, notices, hashes
	go -C src run ./internal/tools/tablervendor -latest -root ..

.PHONY: tabler-vendor
tabler-vendor:  ## Re-vendor exactly what src/internal/web/assets/vendor/TABLER_VERSION pins
	go -C src run ./internal/tools/tablervendor -root ..

# ---------------------------------------------------------------------------
# House-keeping
# ---------------------------------------------------------------------------

.PHONY: clean
clean:  ## Remove generated caches, temp dirs and build/
	rm -rf .cache .downloads build

.PHONY: nuke
nuke: clean  ## clean, plus Go's cached test results. Forces a cold test run.
	go -C src clean -testcache
