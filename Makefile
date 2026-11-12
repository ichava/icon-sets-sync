# icon-sets-sync (ichava/icon-sets-sync) -- Makefile.
#
# Every target runs on the host: Go 1.26 or later to build, and git, gh and
# npm on PATH for a sync. The run targets build build/<os>/icon-sets-sync first
# and run it against this checkout's config/. Run `make help`.

SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

# Pack identifier for single-pack targets. Override:
#   make sync PACK=icon-sets-twemoji
PACK ?=

# Packs synced at once by sync-all.
CONCURRENCY ?= 1

# The Go module lives in src/, so every go command names it with -C src. A bare
# `go test ./...` at the root finds no packages and passes.
#
# build/ holds one folder per OS and nothing else (build/macos, build/linux,
# build/windows, build/freebsd, plus checksums.txt and, after `make dist`,
# archives/). The one folder an OS folder may hold is build/macos/icon-sets-sync.app,
# after `make app`. The host build is build/<host-os>/icon-sets-sync, macOS written
# as macos and .exe on Windows. checkbuild fails on anything else.
HOST_GOOS := $(shell go env GOOS)
HOST_OS := $(if $(filter darwin,$(HOST_GOOS)),macos,$(HOST_GOOS))
EXE := $(if $(filter windows,$(HOST_GOOS)),.exe,)
BIN := build/$(HOST_OS)/icon-sets-sync$(EXE)
CHECK_BUILD := go -C src run ./internal/tools/checkbuild -dir ../build

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
	@printf "verify: cd build && shasum -a 256 -c checksums.txt   (after make cross)\n\n"
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
shell: build-local  ## Open a shell with build/<os>/ on PATH and ICHAVA_CONFIG_DIR set to config/
	PATH="$(CURDIR)/build/$(HOST_OS):$$PATH" ICHAVA_CONFIG_DIR="$(CURDIR)/config" $${SHELL:-bash}

# ---------------------------------------------------------------------------
# The actual work
# ---------------------------------------------------------------------------

.PHONY: menu
menu: build-local  ## Open the interactive terminal menu (no arguments opens the web UI)
	$(RUN) menu

.PHONY: check
check: build-local  ## Check upstream status for every configured pack (no writes)
	$(RUN) check

.PHONY: check-pack
check-pack: build-local  ## Check upstream status for one pack: PACK=<name>
	@if [ -z "$(PACK)" ]; then echo "PACK= is required (e.g. make check-pack PACK=icon-sets-twemoji)"; exit 2; fi
	$(RUN) check --pack=$(PACK)

.PHONY: sync
sync: build-local  ## Sync upstream for one pack (refresh + commit + PR): PACK=<name>
	@if [ -z "$(PACK)" ]; then echo "PACK= is required (e.g. make sync PACK=icon-sets-twemoji)"; exit 2; fi
	$(RUN) sync --pack=$(PACK)

.PHONY: sync-all
sync-all: build-local  ## Dry-run every pack: fetch and report the diff, write nothing: CONCURRENCY=<n>
	$(RUN) sync-all --dry-run --concurrency=$(CONCURRENCY)

.PHONY: sync-all-force
sync-all-force: build-local  ## Sync every pack for real (commits + PRs). Be careful.
	$(RUN) sync-all --concurrency=$(CONCURRENCY)

.PHONY: build-twemoji
build-twemoji: build-local  ## Run the icon-sets-twemoji recipe (Twemoji, on the Unicode taxonomy)
	$(RUN) recipe icon-sets-twemoji

.PHONY: validate
validate: build-local  ## Validate every pack config against config/schema/pack.schema.json
	$(RUN) config validate

.PHONY: embed-config
embed-config:  ## Copy config/ into the binary's embedded defaults (run after editing config/)
	go -C src generate ./internal/defaultconfig

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
	@$(CHECK_BUILD) -min 2

# The target list is src/.goreleaser.yaml's builds, the one place it is kept,
# so these build exactly the set a release would. A snapshot with publishing,
# signing and SBOMs skipped, so nothing leaves the machine, and no Go release.
# GoReleaser writes into its scratch dist, .cache/goreleaser (src/.goreleaser.yaml
# `dist:`); flatdist copies only the bare binaries out and removes the rest:
#   cross -> build/{os}/{binary}_{os}_{arch}[.exe] (macOS as `macos`, plus the
#            universal binary) and build/checksums.txt over them, nothing else;
#            its lines name build/-relative paths (macos/icon-sets-sync_macos_arm64),
#            so verify: cd build && shasum -a 256 -c checksums.txt
#   dist  -> the same, plus build/archives/: the .tar.gz/.zip archives, the
#            deb/rpm/apk packages, the source tarball and their checksums.txt.
GORELEASER_SNAPSHOT := GOPRIVATE='github.com/ichava/*' GONOSUMDB='github.com/ichava/*' \
	goreleaser release --snapshot --clean --config src/.goreleaser.yaml

.PHONY: cross
cross:  ## Build every release target's bare binary into build/<os>/ (needs goreleaser v2)
	@command -v goreleaser >/dev/null || { echo "goreleaser v2 is required: https://goreleaser.com/install/"; exit 2; }
	$(GORELEASER_SNAPSHOT) --skip=publish,sign,sbom,nfpm
	go -C src run ./internal/tools/flatdist -root ..
	go -C src run ./internal/tools/launchers -desktop ../build/linux -windows ../build/windows
	@$(CHECK_BUILD) -min 23

# Double-click launchers, built from what cross wrote (src/internal/tools/launchers).
# macOS opens a bare executable inside Terminal, so it gets an app bundle whose
# binary is the universal binary itself; Linux gets a .desktop entry (cross
# writes it); Windows gets icon-sets-sync-launcher_windows_<arch>.exe, a
# GUI-subsystem program that opens no console (cross writes those too). Unsigned: see docs/installation.md.
UNIVERSAL := build/macos/icon-sets-sync_macos_universal
APP := build/macos/icon-sets-sync.app

.PHONY: app
app:  ## Build build/macos/icon-sets-sync.app from the universal binary (run make cross first)
	@test -f $(UNIVERSAL) || { echo "$(UNIVERSAL) is missing; run make cross first"; exit 2; }
	go -C src run ./internal/tools/launchers -app ../$(APP) -universal ../$(UNIVERSAL) \
		-version "$$(git describe --tags --always 2>/dev/null || echo 0.0.0)"
	@$(CHECK_BUILD) -min 8

.PHONY: desktop
desktop:  ## Write build/linux/icon-sets-sync.desktop and its icon (make cross does this too)
	go -C src run ./internal/tools/launchers -desktop ../build/linux
	@$(CHECK_BUILD)

.PHONY: dist
dist:  ## cross, plus the archives and Linux packages in build/archives (needs goreleaser v2)
	@command -v goreleaser >/dev/null || { echo "goreleaser v2 is required: https://goreleaser.com/install/"; exit 2; }
	$(GORELEASER_SNAPSHOT) --skip=publish,sign,sbom
	go -C src run ./internal/tools/flatdist -root .. -archives
	@$(CHECK_BUILD) -min 40

.PHONY: check-build
check-build:  ## Fail when build/ holds anything but the OS folders, checksums.txt and archives/
	@$(CHECK_BUILD)

.PHONY: test-local
test-local:  ## go vet + go test with the host's Go toolchain
	go -C src vet ./...
	go -C src test -count=1 ./...

# ---------------------------------------------------------------------------
# Web UI
# ---------------------------------------------------------------------------

.PHONY: serve
serve:  ## Run the web UI (icon-sets-sync serve; opens a browser unless --no-open) with the host Go toolchain
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
clean:  ## Remove generated caches, temp dirs (GoReleaser's .cache/goreleaser too) and build/
	rm -rf .cache .downloads build

.PHONY: nuke
nuke: clean  ## clean, plus Go's cached test results. Forces a cold test run.
	go -C src clean -testcache
