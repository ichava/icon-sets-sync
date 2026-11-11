# Architecture

`icon-sets-sync` is one Go module in `src/`: a thin binary, a cobra command tree, and one small
package per concern.

## A pipeline of stages

Every refresh is the same shape: fetch, narrow, clean, write, commit.

```text
Source        ->  Transforms         ->  Sinks
NpmTarball        FindLicences           Filesystem
GithubArchive     SubsetTo               LicenceTexts
UnicodeCldr       Sanitise               StatedFacts
                                         VersionStamp
Compound          Categorise / Indexer   GitBranch
Static            Slugify                Discard (dry run only)
                  FlattenVariants
```

`internal/core/pipeline` runs the stages in order and threads a `Context` between them. A stage that
fails stops that pipeline; the remaining stages do not run, which is what keeps a sink from
committing assets a transform failed to produce. In Go:

```go
p := pipeline.Named("icon-sets-tabler@3.49.0").
	Source(sources.NpmTarball{Package: "@tabler/icons", Version: "3.49.0"}).
	Transform(transforms.FindLicences{}).
	Transform(transforms.SubsetTo{Subdir: "icons"}).
	Transform(transforms.Sanitise{}).
	Sink(sinks.Filesystem{Root: "/work/icon-sets-tabler/resources/assets/svg/files"}).
	Sink(sinks.LicenceTexts{Root: "/work/icon-sets-tabler/resources/assets/svg/licenses/tabler-icons", Set: "tabler-icons"}).
	Sink(sinks.StatedFacts{Pack: pack}).
	Sink(sinks.VersionStamp{Pack: pack}).
	Sink(sinks.GitBranch{RepoRoot: "/work/icon-sets-tabler", OpenPR: true})
result, err := p.Run()
```

### What a pack states about itself moves with its assets

A pack's own tests pin its icon count and vendored version, and its README and `composer.json` state
them, so a set that silently shrinks or grows fails. Before a commit, `StatedFacts` compares the SVGs
the sync wrote with the ones at the pack's `HEAD`, total and per set folder, and the fetched version
with the one the pack's version file names. It rewrites each figure that moved, wherever `README.md`,
`composer.json` and `tests/**/*.php` state it, so an update PR opens green. Only whole figures move
(2,773 inside 12,773 is another number), and counts under ten are left alone. A previous figure that
no file states is logged and recorded under `stated_facts.unstated`, never guessed at. The licence
check's version is `VersionStamp`'s: it advances `license.checked_version` only while the licence text
still hashes to `license.sha256`.

## Layout

```text
src/
├── cmd/icon-sets-sync/   the binary, the only entrypoint; calls cli.Execute and nothing else
└── internal/
    ├── cli/              the cobra tree: list-packs, check, sync, sync-all, recipe,
    │                     config validate|print, manifest-pack audit, menu, version
    ├── tui/              the Bubble Tea menu
    ├── app/              orchestration both front-ends share: loading, checking, syncing
    ├── ui/               TTY detection, colour, status lines, the check table
    ├── version/          build metadata
    ├── tools/            repository tooling run by make, never shipped: flatdist (GoReleaser's
    │                     output into build/{os}/), buildlayout + checkbuild (the build/ guard), …
    └── core/
        ├── settings/     the five-layer settings resolution and its key registry
        ├── config/       pack configs, the registry, the embedded JSON Schema
        ├── docfile/      JSON and YAML reading, one value model for both
        ├── checker/      upstream version polling, in parity with ichava/core's PHP
        ├── httpx/        the one HTTP client: retries, backoff, timeouts
        ├── gitx/         git and gh as subprocesses
        ├── pipeline/     Pipeline, Stage, Context
        ├── sources/      NpmTarball, GithubArchive, UnicodeCldr, Compound, Static
        ├── transforms/   FindLicences, SubsetTo, Sanitise, Slugify, Categorise, Indexer, FlattenVariants
        ├── svg/          the shared SVG policy and the parser that applies it
        ├── kinds/        a pack's icon/illustration kind keys, the glob, and the suggestion heuristic
        ├── sinks/        Filesystem, LicenceTexts, StatedFacts, VersionStamp, GitBranch, Discard
        ├── recipes/      config -> pipelines: generic, twemoji-sets, manifest-pack
        └── manifestpack/ the aggregator pack's set manifest and audit
```

The CLI and the TUI are two front-ends over `internal/app`, so they cannot sync differently: a sync
chosen in the menu exits the menu and runs through the same `app.Sync` the `sync` command calls.

## Why `build/` has one folder per OS

`build/` is what a person opens to find a binary, so it holds the binaries and nothing else:
`build/macos/`, `build/linux/`, `build/windows/`, `build/freebsd/`, `checksums.txt`, and
`archives/` after `make dist`. GoReleaser's own output (one folder per target, `artifacts.json`,
`metadata.json`) stays in `.cache/goreleaser/`, and `flatdist` copies the bare binaries out of it.
The estate rule was decided on 2026-11-04 and replaced the `build/dist/` layout.

Two levels below the root, a binary in `build/<os>/` still finds the checkout's `config/` by walking
up to `src/go.mod`, inside the config resolver's limit of five parents;
`TestBuildOSFoldersReachThisCheckout` fails if a deeper layout ever needs more. `checkbuild` fails
on any other name in `build/` or any folder inside an OS folder, which is why the kind suggestions
moved to `.cache/kind-suggestions/`.

## Why the parts are separate

**Sources** differ by upstream, not by pack: three packs pull npm tarballs, and the code for that
exists once. **Transforms** are per-pack policy: which subdirectory to take, how to derive a
category. **Sinks** are where the result goes, and their order is load-bearing: `Filesystem` writes
the assets, `VersionStamp` records the version, `GitBranch` commits everything uncommitted in the
pack repo. Stamping after the commit would record a version that never shipped.

## Upstream licence texts travel with the icons

MIT, ISC, BSD, Apache-2.0 and OFL each require their licence text, and Apache any upstream `NOTICE`,
to accompany every copy. The text sits at the upstream root, beside a subdirectory of icons that
`SubsetTo` or `FlattenVariants` then makes the whole tree, so a refresh used to drop it. Every asset
pipeline in every recipe now runs `FindLicences` straight after its source, which records the root's
`LICENSE*`, `LICENCE*`, `COPYING*`, `NOTICE*` and `OFL*` files, and `LicenceTexts` straight after
`Filesystem`, which copies them byte for byte under their upstream names to:

| Recipe | Destination |
|---|---|
| `generic` | `resources/assets/svg/licenses/<upstream>`, the npm package with its scope folded in (`tabler-icons`, `flag-icons`) or the GitHub repository |
| `twemoji-sets` | `resources/assets/svg/licenses/twemoji` |
| `openmoji-sets` | `resources/assets/svg/licenses/openmoji`, one directory for both sets |
| `manifest-pack` | `resources/assets/svg/licenses/<set>`, pairing with `files/<set>` |

`resources/` is not export-ignored in any pack, so the texts ship in the dist archive. Each
destination is replaced on every run, so a `NOTICE` upstream drops goes too.

**A pack may declare its licence** (`"licence": "CC-BY-SA-4.0"` in its config). The sink then checks
that one of the upstream licence files still opens with that licence's lines, and fails the run,
before the existing texts are touched, if none does. A relicensed upstream therefore blocks the sync
until a person has read the new terms and changed the declaration. `icon-sets-openmoji` declares
CC-BY-SA-4.0. The declarable ids are `Apache-2.0`, `CC-BY-3.0`, `CC-BY-4.0`, `CC-BY-SA-4.0`,
`CC0-1.0`, `ISC` and `MIT`; each is a list of accepted opening lines in `licenceSignatures`, and the
schema's enum must match it. The opening lines rather than a hash, because a copyright year moving
is not a relicensing.

**A pack may also pin the text** (`"licence_sha256"`). One upstream licence file must then have
exactly that SHA-256, or the run fails the same way. It is the pack plan's licence-change guard, the
one that would have caught Remix Icon moving from Apache-2.0 to its own licence at v4.9.0, and the
only check available for a licence with no standard text: Font Awesome Free's, the Pictogrammers
licence, Game Icons' statement. Every v0.1.0 pack config pins one whose upstream ships a licence. A
pack that declares neither is unguarded.

**An upstream with no licence file at its root fails that pipeline**, naming the set and the source.
Shipping icons whose licence text cannot be found is the defect this exists to end, so a scheduled
run stops rather than warns. The one way past it is `licence_missing`, in a pack config or in a
set of a pack's set manifest, which records the evidence that the upstream ships no licence file; that set then
refreshes, its licence directory is removed rather than left stale, and the gap is logged on every
run, unless the directory is hand-supplied (below). CC-BY and CC-BY-SA upstreams get the same copy; their licences also
require visible credit wherever the icons are shown, which no sync can provide and which each pack's
`ATTRIBUTION.md` and the site's licences page carry.

**A licence directory holding a `source.md` is hand-maintained, and no sync deletes or overwrites
it.** That is how a `licence_missing` set gets its text: a maintainer adds it by hand to
`resources/assets/svg/licenses/<set>`, beside a `source.md` saying where it came from. The sink then reports the set as `hand-supplied`
instead of missing, in its log line and its `licence_texts` metric (`status`), and the
`licence_missing` warning stops, because the exception is satisfied. Without a marker the
directory is treated as before: replaced from upstream, or removed when upstream ships none.

The marker name is matched exactly, as a regular file. `source.md` is the lowercase slug the estate
names files with, and it is the only spelling: the uppercase `SOURCE.md` the sink accepted first is
no longer a marker, and nor is any other casing (`Source.md`), on any filesystem. A case-insensitive
one included, so a sync on macOS treats a folder as the Linux runners do.

| Upstream ships a licence | `source.md` | `licence_missing` | Result |
|---|---|---|---|
| no | yes | set | folder kept, `hand-supplied`, no warning |
| no | yes | unset | run fails, folder kept; record the evidence in `licence_missing` |
| no | no | set | folder removed, `missing`, warning |
| yes | yes | either | folder kept, `hand-supplied`, warning to reconcile (`reconcile: true`) |
| yes | no | either | folder replaced with upstream's texts, `copied` |

When an upstream starts shipping a licence for a hand-supplied set, the sink keeps the folder and
warns rather than switching or failing. The hand-supplied text may be the better one, a fuller
copyright line for instance, and only a person can tell; a scheduled run should not stop over a
pack that is still correctly licensed. To reconcile, compare the texts, then either keep the folder
or delete its `source.md` (and the set's `licence_missing`), so the next sync copies upstream's.

## Recipes

A recipe assembles the pipelines for one pack. `generic` builds them from the config alone, which
covers the common case and is what icon-sets-tabler and icon-sets-flag run. `twemoji-sets` is three
upstreams feeding three filesystem sinks, then one trailing pipeline that only stamps and commits.
`manifest-pack` is one pipeline per set in the manifest, each from its own upstream, so each set keeps its
own SVG dialect, and a set absent from the manifest is never touched.

Recipes are code; what they run over is data. A pack that is private keeps that data -- its pack
config and set manifest -- in its own repository, and the toolkit merges it in at run time from a
private config directory (`--private-config-dir`, `ICHAVA_PRIVATE_CONFIG_DIR`; see
[configuration](configuration.md#private-packs)). So a private pack runs through the same recipes as
a public one, and this repository carries no description of it.

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
Anything written to *this* repository does not: in CI, `config/` is the runner's throwaway
checkout of this repository, deleted when the job ends.

That is not hypothetical. `current_version` used to live only here, the bump was written through
that mount, and it was discarded on every run, so the checker compared against a version that never
advanced and re-opened the same pull request indefinitely. Moving the record into the pack, beside
the assets it describes, is what makes the sync converge.

## Why sync fails loudly

A scheduled job's exit code is the only thing anyone reads. The earlier implementation skipped a
pack whose upstream could not be resolved and exited 0, so a dead registry or a renamed package looked identical to an
up-to-date pack, every week. `sync` now exits 1 for an unresolvable upstream and for a pack with no
sinks outside a dry run, after the other packs have run.

## Why a human merges

The tool opens a pull request and stops. An upstream refresh can change thousands of files, and
"upstream released a version" is not evidence that the result renders. The review is the point of
the workflow, not a formality around it.

## Why Go, and what happened to the Python

The first implementation was a Python package. The Go port reached parity, then went further: it is
the only implementation of the manifest-pack refresh, it fixed the emoji recipe's version stamp (the
old commit pipeline had no version to stamp, so that pack never converged), and it filters
`gh pr list` to open pull requests. The Python package, its tests and its CI job were removed on the
owner's decision of 2026-10-21, before the re-based 0.1.0. The SVG filter's agreement with the lxml
implementation it replaced is still checked, against
`src/internal/core/svg/testdata/lxml-oracle.json`, the frozen results of the last lxml run.

The Docker image that carried the binary went next. Once `sync-pack.yml` built the binary from
source on the runner, no pack pulled the image, so the Dockerfile, the compose file and the
workflow that published `ghcr.io/ichava/icon-sets-sync` were removed (see the CHANGELOG).
`TestNoDockerOrPythonSurface` and `TestNoWorkflowBuildsOrPublishesAnImage` fail if either comes back.

---

[← Docs index](../README.md#documentation)
