# ichava/icon-sets-sync

[![Tests](https://github.com/ichava/icon-sets-sync/actions/workflows/tests.yml/badge.svg)](https://github.com/ichava/icon-sets-sync/actions/workflows/tests.yml)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Two badges, not four: this ships as a Docker image, a Go binary and a Composer package that is not on
Packagist yet, so there is no registry version to show, and static analysis (`gofmt`, `go vet`) runs
inside the Tests workflow.

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

From a PHP project, through Composer (once the package is on Packagist; until then, add this
repository as a `vcs` repository):

```bash
composer require --dev ichava/icon-sets-sync
vendor/bin/icon-sets-sync version
```

The package carries no Go code you build. Its first run downloads the release binary for your OS
and architecture, checks it against the release's `checksums.txt`, caches it under
`vendor/ichava/icon-sets-sync/.cache/bin/`, and runs it; later runs reuse the cached copy. It
fetches the release matching the installed tag, or the latest release for a branch install. Set
`ICHAVA_ICON_SETS_SYNC_VERSION` to pin another. With `cosign` on `PATH` the Sigstore signature over
`checksums.txt` is verified as well, and `ICHAVA_ICON_SETS_SYNC_REQUIRE_SIGNATURE=1` refuses to run
without it.

Without Docker, with a Go toolchain:

```bash
go -C src build -o ../build/icon-sets-sync ./cmd/icon-sets-sync
```

The original Python implementation was removed before the re-based 0.1.0; see
[Architecture](docs/architecture.md#why-go-and-what-happened-to-the-python).

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

Or do the same from a browser, dry run first:

```bash
go -C src run ./cmd/icon-sets-sync serve   # prints http://127.0.0.1:7878/?token=...
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
- [`serve`](docs/tools/serve.md) -- the web UI: pages, flags, sign-in, safety, updating Tabler

### Recipes

- [Run a sync from the browser](docs/recipes/run-a-sync-from-the-browser.md) -- dry run, review, then the real pull request

## Community

Issues and pull requests are welcome at <https://github.com/ichava/icon-sets-sync/issues>. See
[CONTRIBUTING.md](CONTRIBUTING.md), and [SECURITY.md](SECURITY.md) for reporting a vulnerability.

## License

MIT. Copyright (c) Simtabi LLC.
