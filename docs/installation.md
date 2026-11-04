# Installation

`icon-sets-sync` is built from source: releases are on hold, so there is no binary to download.

## Build from source

```bash
git clone https://github.com/ichava/icon-sets-sync.git
cd icon-sets-sync
make build-local                  # writes build/<os>/icon-sets-sync; make build does the same
build/macos/icon-sets-sync --config-dir config list-packs   # build/linux/, build/windows/….exe elsewhere
```

Run with no arguments, or double-clicked, it opens the web UI in your browser; with a command it runs
headless, as on CI and servers (see [`serve`](tools/serve.md#flags)).

Double-clicked from inside the checkout, every binary under `build/<os>/` finds
the checkout's `config/` from their own location; a binary copied anywhere else uses the pack configs
built into it, read-only. After editing `config/`, run `make embed-config` so the built-in copy
matches; a test fails until it does.

### Build outputs

`build/` holds one folder per OS and nothing else, and is gitignored; `make clean` removes it.

```text
build/
  checksums.txt   SHA-256 over every cross binary below, as macos/icon-sets-sync_macos_arm64 …
  macos/          icon-sets-sync (the host build)  icon-sets-sync_macos_amd64  _arm64  _universal
  linux/          icon-sets-sync_linux_<arch> …
  windows/        icon-sets-sync_windows_<arch>.exe …
  freebsd/        icon-sets-sync_freebsd_<arch> …
  archives/       only after `make dist`
```

| Command | Writes |
|---|---|
| `make build-local` | `build/<os>/icon-sets-sync` for this machine (`build/macos/` on a Mac, `.exe` on Windows) |
| `make cross` | `build/{os}/{binary}_{os}_{arch}` (`.exe` on Windows) for every release target of `icon-sets-sync`, with macOS as `macos` plus a `macos_universal` binary, and `build/checksums.txt` over them. Nothing else. |
| `make dist` | the same, plus `build/archives/`: the `.tar.gz`/`.zip` archives, the `.deb`/`.rpm`/`.apk` packages, the source tarball and their own `checksums.txt` |

`cross` and `dist` need GoReleaser v2 and read the target list from `src/.goreleaser.yaml`; they are
snapshots with publishing, signing and SBOMs skipped, so nothing is released. GoReleaser writes into a
scratch directory, `.cache/goreleaser/`, and only the bare binaries are copied out of it into
`build/{os}/`; the scratch directory is then removed. `make check-build` (run by all three targets)
fails if `build/` holds anything else or an OS folder holds a folder. Measured 2026-11-04,
`make cross` leaves 19 binaries and `checksums.txt`; it left 38 (568 MB) while it also built a short
alias, and 86 items (1.0 GB) before it was trimmed, most of them GoReleaser's per-target folders. `bin/icon-sets-sync` is the tracked Composer
launcher (PHP), not a build output.

`build/checksums.txt` names each cross binary by its path inside `build/`
(`macos/icon-sets-sync_macos_arm64`), so it verifies as-is (`sha256sum -c` on Linux):

verify: cd build && shasum -a 256 -c checksums.txt

The release's `checksums.txt`, which CI uploads beside the assets, keeps bare names, because install
scripts and self-update look assets up by name.

Put `build/<os>/` on your `PATH` (`make shell` does it for one shell) to call it from anywhere. To pin a version, check out its
tag (`git checkout vX.Y.Z`) before building. To run it without keeping a binary:

```bash
go -C src run ./cmd/icon-sets-sync version
```

The module lives in `src/`, so every `go` command names it with `-C src`; a bare `go build ./...` at
the repository root finds no packages.

> The `imt` alias binary was retired on 2026-11-04: use `icon-sets-sync`, which takes the same commands.

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
