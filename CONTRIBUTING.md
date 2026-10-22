# Contributing

## Running it

```bash
gofmt -l src                  # must print nothing
go -C src vet ./...
go -C src test -count=1 ./...
```

Or `make test-go` and `make lint`, which run the same in a container. The module lives in `src/`,
so every `go` command names it with `-C src`: a bare `go test ./...` at the root finds no packages
and passes.

For an actual sync, use the Docker image. It is what CI runs, and a local machine with a different
`npm` or `gh` is a different tool.

## Where things go

- A new **upstream shape** is a source strategy in `src/internal/core/sources/`.
- A new **per-pack policy** is usually config, not code. Adding a recipe because a pack wants a
  different subdirectory means the config is missing a knob.
- A new **destination** is a sink in `src/internal/core/sinks/`. Sink order matters: `Filesystem`
  writes assets, `VersionStamp` records the version, `GitBranch` commits. Stamping after the commit records a
  version that never shipped.

See [docs/architecture.md](docs/architecture.md) for why the parts are split that way.

## The rule that is easy to get wrong

**The vendored version belongs in the pack repo, not this one.** Under Docker, `config/` is a mount
from the runner's throwaway checkout, deleted by the workflow's cleanup step. Writing the version
there is how this tool spent weeks re-opening a pull request it had already opened.

## Conventions

- Conventional Commits. Subject 72 characters or fewer, imperative mood. The body explains why.
- No AI attribution anywhere: not in commits, PR titles or bodies, code comments or docs.
- No em-dashes in shipped docs or code comments.
- Table-driven tests for anything new, and no test that needs the network or a listening socket:
  use the `Runner`, `Fetcher` and `Transport` seams.
- The pack config schema is `config/schema/pack.schema.json`; the binary embeds a copy at
  `src/internal/core/config/schema/`, and a test fails until the two are identical. A new setting
  goes in `src/internal/core/settings` and in the settings table in `docs/configuration.md`, which a
  test also checks.

## Security

See [`SECURITY.md`](SECURITY.md).
