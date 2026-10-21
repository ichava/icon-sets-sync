# icon-sets-sync (ichava/icon-sets-sync) -- Makefile.
#
# The run targets shell out to docker compose, so the host needs nothing but
# Docker. The dev targets run Go (and the deprecated Python suite) in their
# own throwaway containers for the same reason; `build-local` and `test-local`
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

# Deprecated Python implementation; kept testable until it is removed.
PY_IMAGE := python:3.12-slim
PY_RUN := docker run --rm -v "$(CURDIR):/repo" -w /repo $(PY_IMAGE) sh -c

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
test: test-go test-python  ## Run the Go suite, then the deprecated Python suite

.PHONY: test-go
test-go:  ## Go: vet + test, in a golang container
	$(GO_RUN) sh -c 'go -C src vet ./... && go -C src test -count=1 ./...'

.PHONY: test-python
test-python:  ## Deprecated Python implementation: pytest, in a python container
	$(PY_RUN) 'pip install -q -e ".[dev]" && pytest -q'

.PHONY: lint
lint:  ## gofmt check + go vet, then ruff on the deprecated Python
	$(GO_RUN) sh -c 'test -z "$$(gofmt -l src)" || { gofmt -l src; exit 1; }; go -C src vet ./...'
	$(PY_RUN) 'pip install -q -e ".[dev]" && ruff check src tests'

.PHONY: typecheck
typecheck:  ## mypy on the deprecated Python (Go is checked by the compiler)
	$(PY_RUN) 'pip install -q -e ".[dev]" && mypy src'

.PHONY: format
format:  ## gofmt -w, then ruff format on the deprecated Python
	$(GO_RUN) gofmt -w src
	$(PY_RUN) 'pip install -q -e ".[dev]" && ruff format src tests'

.PHONY: build-local
build-local:  ## Build $(BIN) with the host's Go toolchain
	go -C src build -trimpath -o ../$(BIN) ./cmd/icon-sets-sync

.PHONY: test-local
test-local:  ## go vet + go test with the host's Go toolchain
	go -C src vet ./...
	go -C src test -count=1 ./...

# ---------------------------------------------------------------------------
# House-keeping
# ---------------------------------------------------------------------------

.PHONY: clean
clean:  ## Remove generated caches, temp dirs and build/
	rm -rf .cache .downloads __pycache__ .pytest_cache .mypy_cache .ruff_cache build
	find . -type d -name '__pycache__' -prune -exec rm -rf {} +

.PHONY: nuke
nuke:  ## Drop the docker volumes (npm cache, pip cache). Forces a cold rebuild.
	$(DC) down -v
