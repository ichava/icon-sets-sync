# Contributing

## Running it

```bash
gofmt -l src                  # must print nothing
go -C src vet ./...
go -C src test -count=1 ./...
```

Or `make test` and `make lint`, which run the same with your Go toolchain (`make lint` adds
golangci-lint when it is installed, for issues new since `main`). The module lives in `src/`, so
every `go` command names it with `-C src`: a bare `go test ./...` at the root finds no packages and
passes.

For an actual sync, build the binary (`make build-local`) and put the pack checkouts at `/work`; see
[docs/installation.md](docs/installation.md#the-work-directory). CI does the same in
`sync-pack.yml`, so a local machine with a different `npm` or `gh` is the one difference to keep in
mind.

## Where things go

- A new **upstream shape** is a source strategy in `src/internal/core/sources/`.
- A new **per-pack policy** is usually config, not code. Adding a recipe because a pack wants a
  different subdirectory means the config is missing a knob.
- A new **destination** is a sink in `src/internal/core/sinks/`. Sink order matters: `Filesystem`
  writes assets, `VersionStamp` records the version, `GitBranch` commits. Stamping after the commit records a
  version that never shipped.

See [docs/architecture.md](docs/architecture.md) for why the parts are split that way.

## The rule that is easy to get wrong

**The vendored version belongs in the pack repo, not this one.** In CI, `config/` comes from the
runner's throwaway checkout of this repository, deleted when the job ends. Writing the version
there is how this tool spent weeks re-opening a pull request it had already opened.

## Conventions

- Conventional Commits. Subject 72 characters or fewer, imperative mood. The body explains why.
- No AI attribution anywhere: not in commits, PR titles or bodies, code comments or docs.
- No em-dashes in shipped docs or code comments.
- Table-driven tests for anything new, and no test that needs the network or a listening socket:
  use the `Runner`, `Fetcher` and `Transport` seams.
- **Pack names are lowercase slugs**: `a-z` and digits in words joined by single hyphens, the same
  string as the config file name and the `packs.json` entry (`icon-sets-extras-feather`, never
  `icon-sets-extras.feather`). The loader refuses anything else and a test fails on a shipped one;
  see [Naming packs](docs/configuration.md#naming-packs).
- The pack config schema is `config/schema/pack.schema.json`; the binary embeds a copy at
  `src/internal/core/config/schema/`, and a test fails until the two are identical. A new setting
  goes in `src/internal/core/settings` and in the settings table in `docs/configuration.md`, which a
  test also checks.

## Releases and versioning

- **Binaries are published on the GitHub release only.** A pushed `vX.Y.Z` tag builds and publishes
  the release, and so does `gh workflow run release.yml -f tag=vX.Y.Z`; push a version tag only for
  final code. `release.yml` calls `simtabi/.github`'s shared `go-release.yml`: change the pipeline
  there, never here. Never add a `go install` path, tap, bucket, image or any other channel. See
  [Release](docs/release.md#binaries-are-published-on-the-github-release).
- **A version tag is pushed only for final code.** `vX.Y.Z` marks a release, nothing else. Test a
  change on a branch; a pre-release tag (`vX.Y.Z-rc.N`) is for the day binary releases are on.
- **A public tag is never moved, deleted or reused.** Once a tag has been fetched, moving it makes
  consumers of the same version disagree. Composer and npm git dependencies resolve the tag afresh, so
  new installs get the moved commit while every existing lockfile and cache keeps the old one (an npm
  registry version cannot be republished at all). For a Go module, the module proxy keeps the copy it
  first served and `sum.golang.org` records its checksum, so `go` fetches after the move fail
  verification. The next fix gets the next number.
- **A tag ruleset enforces this.** Applied once the v0.1.0 tags are pushed, it blocks deleting,
  updating and force-pushing `v*` and `src/v*` tags on this repository. The v0.1.0 publication as
  a single commit was the one planned exception.
- **A bad release is retracted, not replaced.** Add `retract vX.Y.Z // <reason>` to `src/go.mod`
  in the next release, so `go get` and `@latest` skip it. Then ship the fix as the next version.

## Security

See [`SECURITY.md`](SECURITY.md).
