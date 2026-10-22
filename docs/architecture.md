# Architecture

`icon-sets-sync` is one Go module in `src/`: a thin binary, a cobra command tree, and one small
package per concern.

## A pipeline of stages

Every refresh is the same shape: fetch, narrow, clean, write, commit.

```text
Source        ->  Transforms         ->  Sinks
NpmTarball        SubsetTo               Filesystem
GithubArchive     Sanitise               VersionStamp
UnicodeCldr       Categorise / Indexer   GitBranch
Compound          Slugify                Discard (dry run only)
Static            FlattenVariants
```

`internal/core/pipeline` runs the stages in order and threads a `Context` between them. A stage that
fails stops that pipeline; the remaining stages do not run, which is what keeps a sink from
committing assets a transform failed to produce. In Go:

```go
p := pipeline.Named("icon-sets-tabler@3.49.0").
	Source(sources.NpmTarball{Package: "@tabler/icons", Version: "3.49.0"}).
	Transform(transforms.SubsetTo{Subdir: "icons"}).
	Transform(transforms.Sanitise{}).
	Sink(sinks.Filesystem{Root: "/work/icon-sets-tabler/resources/assets/svg/files"}).
	Sink(sinks.VersionStamp{Pack: pack}).
	Sink(sinks.GitBranch{RepoRoot: "/work/icon-sets-tabler", OpenPR: true})
result, err := p.Run()
```

## Layout

```text
src/
├── cmd/icon-sets-sync/   the binary; calls cli.Execute and nothing else
├── cmd/imt/              the same program under its short name
└── internal/
    ├── cli/              the cobra tree: list-packs, check, sync, sync-all, recipe,
    │                     config validate|print, bundled audit, menu, version
    ├── tui/              the Bubble Tea menu
    ├── app/              orchestration both front-ends share: loading, checking, syncing
    ├── ui/               TTY detection, colour, status lines, the check table
    ├── version/          build metadata
    └── core/
        ├── settings/     the five-layer settings resolution and its key registry
        ├── config/       pack configs, the registry, the embedded JSON Schema
        ├── docfile/      JSON and YAML reading, one value model for both
        ├── checker/      upstream version polling, in parity with ichava/core's PHP
        ├── httpx/        the one HTTP client: retries, backoff, timeouts
        ├── gitx/         git and gh as subprocesses
        ├── pipeline/     Pipeline, Stage, Context
        ├── sources/      NpmTarball, GithubArchive, UnicodeCldr, Compound, Static
        ├── transforms/   SubsetTo, Sanitise, Slugify, Categorise, Indexer, FlattenVariants
        ├── svg/          the shared SVG policy and the parser that applies it
        ├── sinks/        Filesystem, VersionStamp, GitBranch, Discard
        ├── recipes/      config -> pipelines: generic, emoji-sets, bundled
        └── bundled/      the aggregator pack's set manifest and audit
```

The CLI and the TUI are two front-ends over `internal/app`, so they cannot sync differently: a sync
chosen in the menu exits the menu and runs through the same `app.Sync` the `sync` command calls.

## Why the parts are separate

**Sources** differ by upstream, not by pack: three packs pull npm tarballs, and the code for that
exists once. **Transforms** are per-pack policy: which subdirectory to take, how to derive a
category. **Sinks** are where the result goes, and their order is load-bearing: `Filesystem` writes
the assets, `VersionStamp` records the version, `GitBranch` commits everything uncommitted in the
pack repo. Stamping after the commit would record a version that never shipped.

## Recipes

A recipe assembles the pipelines for one pack. `generic` builds them from the config alone, which
covers the common case and is what icon-sets-tabler and icon-sets-flag run. `emoji-sets` is three
upstreams feeding three filesystem sinks, then one trailing pipeline that only stamps and commits.
`bundled` is one pipeline per set in the manifest, each from its own upstream, so each set keeps its
own SVG dialect, and a set absent from the manifest is never touched.

Add a recipe when an upstream's shape genuinely differs. Adding one because a pack wants a different
subdirectory or transform means the config is missing a knob; the `transforms` list and the sink
options exist so that it is not.

## Why settings have layers

The same binary runs from a Monday cron in five pack repositories and from a maintainer's terminal,
and they want different things from it: the cron wants the shipped defaults, a maintainer wants a dry
run, or another base branch, or two packs at once, without editing a file that is committed. So a
setting resolves from flags, then `ICHAVA_*` variables, then the global config, then the pack's own
config, then the default, and `config print --origins` says which layer won. An unknown key in any
layer is refused, because a typo that silently did nothing would commit.

## Why the version lives in the pack repo

`GitBranch` commits the pack repo, so anything written before it travels in the pull request.
Anything written to *this* repository does not: under Docker, `config/` is a mount from the runner's
checkout, deleted by the workflow's cleanup step.

That is not hypothetical. `current_version` used to live only here, the bump was written through
that mount, and it was discarded on every run, so the checker compared against a version that never
advanced and re-opened the same pull request indefinitely. Moving the record into the pack, beside
the assets it describes, is what makes the sync converge.

## Why sync fails loudly

A scheduled job's exit code is the only thing anyone reads. The Python skipped a pack whose upstream
could not be resolved and exited 0, so a dead registry or a renamed package looked identical to an
up-to-date pack, every week. `sync` now exits 1 for an unresolvable upstream and for a pack with no
sinks outside a dry run, after the other packs have run.

## Why a human merges

The tool opens a pull request and stops. An upstream refresh can change thousands of files, and
"upstream released a version" is not evidence that the result renders. The review is the point of
the workflow, not a formality around it.

## Why Go, and what happened to the Python

The first implementation was a Python package (`src/ichava_maintainer_toolkit/`, Typer, Pydantic,
lxml). The Go port reached parity, then went further: it is the only implementation of the bundled
pack's refresh, it fixed the emoji recipe's version stamp (the Python's commit pipeline had no
version to stamp, so that pack never converged), it filters `gh pr list` to open pull requests, and
it ships as one static binary in a smaller image.

From 0.2.0 the Docker image runs only the Go binary, and **the Python package is removed**: the
package, `pyproject.toml`, its tests and its CI job were deleted on the owner's decision of
2026-10-21. The SVG filter's agreement with the lxml implementation it replaced is still checked,
against `src/internal/core/svg/testdata/lxml-oracle.json`, the frozen results of the last lxml run.

The Python's fluent API looked like this, for anyone reading an old recipe:

```python
Pipeline.named("icon-sets-tabler@3.44.0") \
    .source(sources.NpmTarball("@tabler/icons", version="3.44.0")) \
    .transform(transforms.SubsetTo("icons")) \
    .transform(transforms.Sanitise()) \
    .sink(sinks.Filesystem(root="/work/icon-sets-tabler/resources/assets/svg/files")) \
    .run()
```

---

[← Docs index](../README.md#documentation)
