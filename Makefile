# icon-sets-sync (ichava/icon-sets-sync) -- Makefile.
#
# The run targets shell out to docker compose, so the host needs nothing but
# Docker. The dev targets run Go in a throwaway container for the same reason; `build-local` and `test-local`
# use a Go toolchain on the host when you have one. Run `make help`.

SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

# Pack identifier for single-pack targets. Override:
#   make sync PACK=icon-sets-emoji
PACK ?=

# Packs synced at once by sync-all.
CONCURRENCY ?= 1

DC := docker compose
RUN := $(DC) run --rm
SERVICE := ichava-maintainer-toolkit

# The Go module lives in src/, so every go command names it with -C src. A bare
# `go test ./...` at the root finds no packages and passes.
GO_IMAGE := golang:1.26-trixie
GO_RUN := docker run --rm -v "$(CURDIR):/repo" -w /repo $(GO_IMAGE)

BIN := build/icon-sets-sync

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
# Image lifecycle
# ---------------------------------------------------------------------------

.PHONY: build
build:  ## Build the docker image (run once + after source or Dockerfile changes)
	$(DC) build

.PHONY: pull
pull:  ## Pull the latest base images
	$(DC) pull

.PHONY: shell
shell:  ## Drop into a bash shell inside the container
	$(RUN) --entrypoint bash $(SERVICE)

# ---------------------------------------------------------------------------
# The actual work
# ---------------------------------------------------------------------------

.PHONY: menu
menu:  ## Open the interactive menu (default action)
	$(RUN) $(SERVICE) menu

.PHONY: check
check:  ## Check upstream status for every configured pack (no writes)
	$(RUN) $(SERVICE) check

.PHONY: check-pack
check-pack:  ## Check upstream status for one pack: PACK=<name>
	@if [ -z "$(PACK)" ]; then echo "PACK= is required (e.g. make check-pack PACK=icon-sets-emoji)"; exit 2; fi
	$(RUN) $(SERVICE) check --pack=$(PACK)

.PHONY: sync
sync:  ## Sync upstream for one pack (refresh + commit + PR): PACK=<name>
	@if [ -z "$(PACK)" ]; then echo "PACK= is required (e.g. make sync PACK=icon-sets-emoji)"; exit 2; fi
	$(RUN) $(SERVICE) sync --pack=$(PACK)

.PHONY: sync-all
sync-all:  ## Sync every pack as a dry run (files only): CONCURRENCY=<n>
	$(RUN) $(SERVICE) sync-all --dry-run --concurrency=$(CONCURRENCY)

.PHONY: sync-all-force
sync-all-force:  ## Sync every pack for real (commits + PRs). Be careful.
	$(RUN) $(SERVICE) sync-all --concurrency=$(CONCURRENCY)

.PHONY: build-emoji
build-emoji:  ## Run the icon-sets-emoji recipe (Twemoji + OpenMoji + Unicode taxonomy)
	$(RUN) $(SERVICE) recipe icon-sets-emoji

.PHONY: validate
validate:  ## Validate every pack config against config/schema/pack.schema.json
	$(RUN) $(SERVICE) config validate

.PHONY: config-print
config-print:  ## Print the effective settings and where each came from [PACK=<name>]
	$(RUN) $(SERVICE) config print --origins $(if $(PACK),--pack=$(PACK),)

# ---------------------------------------------------------------------------
# Dev workflow
# ---------------------------------------------------------------------------

.PHONY: test
test: test-go  ## Run the Go suite

.PHONY: test-go
test-go:  ## Go: vet + test, in a golang container
	$(GO_RUN) sh -c 'go -C src vet ./... && go -C src test -count=1 ./...'

.PHONY: lint
lint:  ## gofmt check + go vet
	$(GO_RUN) sh -c 'test -z "$$(gofmt -l src)" || { gofmt -l src; exit 1; }; go -C src vet ./...'

.PHONY: format
format:  ## gofmt -w
	$(GO_RUN) gofmt -w src

.PHONY: build-local
build-local:  ## Build $(BIN) with the host's Go toolchain
	go -C src build -trimpath -o ../$(BIN) ./cmd/icon-sets-sync

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
ui:  ## Run the web UI in Docker on http://127.0.0.1:7878 (the address it logs has the token)
	$(DC) --profile ui up ui

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
nuke:  ## Drop the docker volumes (npm cache). Forces a cold rebuild.
	$(DC) down -v
