# ichava/icon-sets-sync

[![Tests](https://github.com/ichava/icon-sets-sync/actions/workflows/tests.yml/badge.svg)](https://github.com/ichava/icon-sets-sync/actions/workflows/tests.yml)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Two badges, not four: this ships as Go source you build and a Composer package that is not on
Packagist yet, so there is no registry version to show, and static analysis (`gofmt`, `go vet`) runs
inside the Tests workflow.

> `icon-sets-sync` keeps the Ichava icon packs in step with their upstream registries: it polls
> each upstream, refreshes the vendored SVGs, records the version in the pack and opens a pull
> request.

Go 1.26 or later to build from source, and `git`, `gh` and `npm` on `PATH` to sync. The repository
was `ichava/maintainer-toolkit` until 2026-10-21; GitHub redirects the old URL.

## Install

**Download a release.** Each release on the
[releases page](https://github.com/ichava/icon-sets-sync/releases) carries a binary for every
platform (`icon-sets-sync_<os>_<arch>`, `macos` rather than `darwin`), `.tar.gz`/`.zip` archives,
`.deb`/`.rpm`/`.apk` packages, the macOS app (`icon-sets-sync_macos_universal.app.zip`) and the
Windows launchers. Check a download against the release's `checksums.txt`
(`checksums-launchers.txt` for the app and launchers); [Release](docs/release.md#verifying-a-release)
shows the signature check. The GitHub release is the only download channel: there is no `go install`
path, Homebrew tap, Scoop bucket or image.

**Or build from source**, with Go 1.26 or later:

```bash
git clone https://github.com/ichava/icon-sets-sync.git
cd icon-sets-sync
make build-local          # writes build/<os>/icon-sets-sync, e.g. build/macos/ (make build does the same)
```

Or run it straight from the checkout without keeping a binary:

```bash
go -C src run ./cmd/icon-sets-sync version
```

To pin a version, check out its tag (`git checkout vX.Y.Z`) before building.

From a PHP project, through Composer (once the package is on Packagist; until then, add this
repository as a `vcs` repository), point the launcher at the binary you built:

```bash
composer require --dev ichava/icon-sets-sync
ICHAVA_ICON_SETS_SYNC_BIN=/path/to/icon-sets-sync/build/macos/icon-sets-sync vendor/bin/icon-sets-sync version
```

The Composer package carries no Go code. With `ICHAVA_ICON_SETS_SYNC_BIN` set it runs that binary.
Without it, it stops and says how to build one. It downloads the release binary for the host, checked against the release's
`checksums.txt`, only when `ICHAVA_ICON_SETS_SYNC_DOWNLOAD=1` opts in; the download is never the
default.

`go install` is not an install path. The Go module proxy fetched `src/v0.1.0` to `src/v0.1.2` from
commits that predate the single-commit publication, and it keeps the first copy of a tag for good,
so `go install …@v0.1.0` would serve that older code and `@latest` the older v0.1.2.

An earlier implementation was removed before the re-based 0.1.0, and the Docker image after it; see
[Architecture](docs/architecture.md#why-go-and-what-happened-to-the-python).

## Quick start guide and usage

### Getting started

Every shipped config names its pack as `/work/<pack>`, so `/work` must be the directory that holds
the pack checkouts (`icon-sets-tabler`, `icon-sets-flag`, ...), usually the parent of this one. On
Linux that is one symlink, the same one `sync-pack.yml` makes on the runner; on macOS, see
[Installation](docs/installation.md#the-work-directory):

```bash
sudo ln -sfn "$(dirname "$PWD")" /work
```

A sync that commits needs `GH_TOKEN` in the environment, with permission to push a branch and open a
pull request in the pack repositories. Nothing else is required; there are no config files to
publish and no settings you must set.

Check the configs once:

```bash
make validate
```

### Usage

Double-click to open the web UI in your browser, with no terminal window left behind:

- **macOS:** `make cross && make app`, then double-click `build/macos/icon-sets-sync.app` (unsigned:
  right-click → Open the first time).
- **Windows:** double-click `icon-sets-sync_windows_<arch>.exe`; its console window closes within a
  second and the server runs in the background.
- **Linux:** install `build/linux/icon-sets-sync.desktop` (written by `make cross` or `make desktop`)
  and start it from the desktop's launcher.

Stop it with **Stop server** on any page. The CLI is unchanged: pass a command for CLI and CI use (a
command never opens a browser), and with no terminal, with `CI` set, or on Linux without a display, no
arguments prints the help instead. The binary inside the bundle is the same CLI
(`icon-sets-sync.app/Contents/MacOS/icon-sets-sync check`). The terminal menu is
`icon-sets-sync menu` (`make menu`).

See which packs are behind upstream, without writing anything:

```bash
make check
```

Refresh one pack, commit it to `chore/sync-upstream-<version>` and open a pull request:

```bash
make sync PACK=icon-sets-tabler
```

Dry-run every pack (fetch and report the diff, write nothing), two at a time, against a different base branch:

```bash
ICHAVA_GIT_BASE=develop build/macos/icon-sets-sync --config-dir config sync-all --dry-run --concurrency=2
```

Or do the same from a browser, dry run first:

```bash
make serve   # prints http://127.0.0.1:7878/?token=...
```

The web UI exists only while `serve` is running; stop the command and the page is gone. Open the
**printed** address, token included. Opening `http://127.0.0.1:7878` on its own is refused on purpose
(a 401 "Sign-in link required" page), because the token is what proves the request came from you. The
same command from the repository root also works without `make`:
`go -C src run ./cmd/icon-sets-sync serve`. It finds this repository's `config/` itself; to use
another directory, set `ICHAVA_CONFIG_DIR` or pass `--config-dir`.

The full walkthrough is in [Getting started](docs/getting-started.md), and everything else is under
[Documentation](#documentation).

## <a name="documentation"></a>Documentation

### Guides

- [Installation](docs/installation.md) -- building from source, the `/work` directory, and what a sync needs
- [Getting started](docs/getting-started.md) -- check, sync, dry runs and what lands afterwards
- [Configuration](docs/configuration.md) -- pack configs, the schema, settings and their precedence
- [Architecture](docs/architecture.md) -- the pipeline, the recipes, and why the version lives in the pack
- [Release](docs/release.md) -- cutting a version and what it affects

### Reference

- [Maintainer sync](docs/maintainer-sync.md) -- the cron, pull request, tag and fan-out cycle
- [Upstream tracking](docs/upstream-tracking.md) -- the end-user side of the same story
- [`serve`](docs/tools/serve.md) -- the web UI: pages, flags, sign-in, safety, updating Tabler
- [REST API](docs/tools/api.md) -- `/api/v1`, bearer tokens and scopes, errors, limits and the OpenAPI document
- [Events](docs/tools/events.md) -- the CloudEvents envelope, the eleven types, their schemas and the event stream
- [Webhooks](docs/tools/webhooks.md) -- endpoints, secrets and rotation, signature checks in Go, PHP and JS, and the delivery rules
- [MCP](docs/tools/mcp.md) -- the stdio Model Context Protocol server, its read and write tools

### Recipes

- [Run a sync from the browser](docs/recipes/run-a-sync-from-the-browser.md) -- dry run, review, then the real pull request
- [Check upstream in CI](docs/recipes/check-upstream-in-ci.md) -- `--fail-on`, `--github` step summaries and outputs
- [Run it in GitHub Actions](docs/recipes/github-actions.md) -- `doctor`, `validate`, `check-upstream` and a dry run; exit codes and outputs

## Community

Issues and pull requests are welcome at <https://github.com/ichava/icon-sets-sync/issues>. See
[CONTRIBUTING.md](CONTRIBUTING.md), and [SECURITY.md](SECURITY.md) for reporting a vulnerability.

## License

MIT. Copyright (c) Simtabi LLC.
