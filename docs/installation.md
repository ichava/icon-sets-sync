# Installation

`icon-sets-sync` is built from source: releases are on hold, so there is no binary to download.

## Build from source

```bash
git clone https://github.com/ichava/icon-sets-sync.git
cd icon-sets-sync
make build-local                  # writes build/icon-sets-sync; make build does the same
build/icon-sets-sync --config-dir config list-packs
```

Run with no arguments, or double-clicked, it opens the web UI in your browser; with a command it runs
headless, as on CI and servers (see [`serve`](tools/serve.md#flags)).

### Build outputs

Everything generated goes under `build/`, which is gitignored; `make clean` removes it.

| Command | Writes |
|---|---|
| `make build-local` | `build/icon-sets-sync`, for this machine |
| `make cross` | `build/dist/{binary}_{os}_{arch}` (`.exe` on Windows) for every release target of both entrypoints, `icon-sets-sync` and `imt`, with macOS as `macos` plus a `macos_universal` binary, and `build/dist/checksums.txt` over them. Nothing else. |
| `make dist` | the same, plus `build/dist/archives/`: the `.tar.gz`/`.zip` archives, the `.deb`/`.rpm`/`.apk` packages, the source tarball and their own `checksums.txt` |

`cross` and `dist` need GoReleaser v2 and read the target list from `src/.goreleaser.yaml`; they are
snapshots with publishing, signing and SBOMs skipped, so nothing is released. Measured 2026-11-04,
`make cross` leaves 38 binaries and `checksums.txt` (568 MB); before it was trimmed it left 86 items
(1.0 GB), most of them GoReleaser's per-target folders. `bin/icon-sets-sync` is the tracked Composer
launcher (PHP), not a build output.

Put `build/icon-sets-sync` on your `PATH` to call it from anywhere. To pin a version, check out its
tag (`git checkout vX.Y.Z`) before building. To run it without keeping a binary:

```bash
go -C src run ./cmd/icon-sets-sync version
```

The module lives in `src/`, so every `go` command names it with `-C src`; a bare `go build ./...` at
the repository root finds no packages. `cmd/imt` builds the same program under its short name.

`go install` is not an install path: the Go module proxy holds copies of the early `src/v0.1.x` tags
taken from commits before the single-commit publication, so it would serve older code. See
[Release](release.md#where-this-package-is).

Releases are on hold until the maintainers decide on one. Archives, Linux packages and checksums
from a release would come from `release.yml`, dispatched by hand; none has been published. See
[Release](release.md#binary-releases-are-off).

## What a sync needs

| Tool | Why |
|---|---|
| `git` | branch, commit and push in the pack repositories |
| `gh` | open the pull request |
| `npm` | `npm pack <package>@<version>`, the npm source strategy |

A `GH_TOKEN` with permission to push a branch and open a pull request in the pack repositories, for
any sync that commits. `check`, `list-packs` and `config validate` need only the binary.

## The `/work` directory

Every shipped config names its pack as `/work/<pack>` (`pack_root`), so `/work` must be the
directory that **contains** the pack checkouts, not a pack itself. That is the layout
`sync-pack.yml` makes on the runner, with the same symlink. On Linux:

```bash
sudo ln -sfn ~/src/ichava /work   # the directory holding icon-sets-tabler, icon-sets-flag, ...
```

On macOS the root volume is read-only, so the link is declared in `/etc/synthetic.conf` and created
at the next boot. The two columns are separated by a tab, and the target has no leading slash:

```text
work	Users/you/src/ichava
```

Without `/work`, keep your own pack configs with a relative `pack_root` and point the `workspace`
setting (`ICHAVA_WORKSPACE`) at the directory holding the packs; see
[Configuration](configuration.md#settings).

## Requirements

Go 1.26 or later to build. `git`, `gh` and `npm` on `PATH` to sync.

---

[← Docs index](../README.md#documentation)
