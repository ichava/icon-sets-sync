# Installation

Two ways to get `icon-sets-sync`: the Docker image, which is what CI runs, or a Go build.

## The Docker image

```bash
docker build -t icon-sets-sync:local .
```

The build is two stages. A `golang:1.26-trixie` stage compiles a static binary from the module in
`src/`; a `debian:trixie-slim` stage carries it with the tools it shells out to:

| Tool | Why |
|---|---|
| `git` | branch, commit and push in the pack repositories |
| `gh` | open the pull request |
| `nodejs`, `npm` | `npm pack <package>@<version>`, the npm source strategy |
| `ca-certificates` | TLS to the registries, GitHub and unicode.org |

The binary is `/usr/local/bin/icon-sets-sync` and is the `ENTRYPOINT`. `imt` is a symlink to it,
and so is `ichava-maintainer-toolkit`, the removed Python console script's name, kept so existing
invocations keep working; it prints a deprecation notice when invoked under that name. The shipped `config/` is copied to `/app/config` and
`ICHAVA_CONFIG_DIR` points there.

The pack workflows build from a checkout of this repository rather than pulling the published image,
because the GHCR package is private and another repository's `GITHUB_TOKEN` cannot pull it.

## Run it

```bash
docker run --rm -v "$PWD/..:/work" icon-sets-sync:local list-packs
```

`/work` must be the directory that **contains** the pack repositories, not a pack itself: each
shipped config's `pack_root` is `/work/<pack>`.

This is the invocation every pack's `sync-upstream.yml` uses, unchanged from the Python image:

```bash
docker run --rm \
  -v "$PWD/..:/work" \
  -v "$PWD/.ichava-maintainer-toolkit/config:/app/config" \
  -e GH_TOKEN \
  icon-sets-sync:local sync --pack="$PACK_SLUG"
```

## A Go build

```bash
go -C src build -o ../build/icon-sets-sync ./cmd/icon-sets-sync
build/icon-sets-sync --config-dir config list-packs
```

The module lives in `src/`, so every `go` command names it with `-C src`; a bare `go build ./...` at
the repository root finds no packages. `cmd/imt` builds the same program under its short name.

A local build needs `git`, `gh` and `npm` on `PATH` to sync. For a real sync prefer the image: it is
what CI runs, and a laptop with a different `npm` or `gh` is a different tool.

## Requirements

Docker, or Go 1.26+. A `GH_TOKEN` with permission to push a branch and open a pull request in the
pack repositories, for any sync that commits.

---

[← Docs index](../README.md#documentation)
