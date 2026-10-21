# ichava/icon-sets-sync

[![Tests](https://github.com/ichava/icon-sets-sync/actions/workflows/tests.yml/badge.svg)](https://github.com/ichava/icon-sets-sync/actions/workflows/tests.yml)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Two badges, not four: this ships as a Docker image and a Go binary, on no package registry, so there
is no registry version to show, and static analysis (`gofmt`, `go vet`) runs inside the Tests workflow.

> `icon-sets-sync` keeps the Ichava icon packs in step with their upstream registries: it polls
> each upstream, refreshes the vendored SVGs, records the version in the pack and opens a pull
> request.

Go 1.26 or later to build from source; Docker for everything else. The repository was
`ichava/maintainer-toolkit` until 2026-10-21; GitHub redirects the old URL.

## Install

```bash
git clone https://github.com/ichava/icon-sets-sync.git ~/Artisan/projects/opensource/ichava/packages/icon-sets-sync
cd ~/Artisan/projects/opensource/ichava/packages/icon-sets-sync
make build      # the Docker image: the Go binary plus git, gh and npm
```

Without Docker, with a Go toolchain:

```bash
go -C src build -o ../build/icon-sets-sync ./cmd/icon-sets-sync
```

The Python package under `src/ichava_maintainer_toolkit/` is **deprecated** since 0.2.0 and nothing
runs it any more; see [Architecture](docs/architecture.md#why-go-and-what-happened-to-the-python).

## Quick start guide and usage

### Getting started

The image mounts the parent of this checkout at `/work`, so the pack repositories must be its
siblings (`../icon-sets-tabler`, `../icon-sets-flag`, ...): every shipped config names its pack as
`/work/<pack>`. A sync that commits needs `GH_TOKEN` in the environment, with permission to push a
branch and open a pull request in the pack repositories. Nothing else is required; there are no
config files to publish and no settings you must set.

Check the configs once:

```bash
make validate
```

### Usage

See which packs are behind upstream, without writing anything:

```bash
make check
```

Refresh one pack, commit it to `chore/sync-upstream-<version>` and open a pull request:

```bash
make sync PACK=icon-sets-tabler
```

Refresh every pack's files as a dry run, two at a time, against a different base branch:

```bash
ICHAVA_GIT_BASE=develop docker compose run --rm ichava-maintainer-toolkit sync-all --dry-run --concurrency=2
```

The full walkthrough is in [Getting started](docs/getting-started.md), and everything else is under
[Documentation](#documentation).

## <a name="documentation"></a>Documentation

### Guides

- [Installation](docs/installation.md) -- the Docker image, `go build`, and what the image contains
- [Getting started](docs/getting-started.md) -- check, sync, dry runs and what lands afterwards
- [Configuration](docs/configuration.md) -- pack configs, the schema, settings and their precedence
- [Architecture](docs/architecture.md) -- the pipeline, the recipes, and why the version lives in the pack
- [Release](docs/release.md) -- cutting a version and what it affects

### Reference

- [Maintainer sync](docs/maintainer-sync.md) -- the cron, pull request, tag and fan-out cycle
- [Upstream tracking](docs/upstream-tracking.md) -- the end-user side of the same story

## Community

Issues and pull requests are welcome at <https://github.com/ichava/icon-sets-sync/issues>. See
[CONTRIBUTING.md](CONTRIBUTING.md), and [SECURITY.md](SECURITY.md) for reporting a vulnerability.

## License

MIT. Copyright (c) Simtabi LLC.
