# Release

How a version of `icon-sets-sync` is cut, and what it reaches.

## Where this package is

`v0.1.3` is the latest tag. The next release, 0.2.0, is the first whose image runs the Go binary.

The Docker image is published to GHCR and is **private**, which is why the pack workflows build from
a checkout of this repository rather than pulling it: another repository's `GITHUB_TOKEN` cannot pull
a private package.

## Cutting a release

1. `go -C src vet ./...`, `go -C src test -count=1 ./...` and `gofmt -l src` (empty). `make test-go`
   runs the first two in a container. The suite covers config loading and the schema, the settings
   precedence, the checker's version comparison, the pipeline, the recipes, git and gh invocation,
   the sync orchestration, and the vendored-version contract.
2. Update `CHANGELOG.md`. The GitHub release body is that version's section; auto-generated notes
   are not a substitute. Keep exactly one `## [Unreleased]` heading.
3. Tag `vX.Y.Z` and push the tag. The Go module is in `src/`, so a release that should be
   `go install`-able also needs `src/vX.Y.Z` on the same commit.

The Tests workflow's Docker job builds the image and runs it the way the pack workflows do, which is
the only check on the Dockerfile before a merge.

## What a release affects

Nothing installs this package as a dependency, so a release here is not a consumer-facing event. It
matters to the pack workflows, which check this repository out at `main`: a break lands in every
pack's scheduled sync at once.

Pin the checkout to a tag in the pack workflows if that becomes uncomfortable. Floating on `main` is
tolerable while this is the maintainer's own tooling and the blast radius is a pull request nobody
has to merge.

## Versioning

Semver against the **config schema, the settings and the CLI**, the things the pack repositories and
their workflows depend on. Renaming a config key, a setting, an environment variable or a command is
breaking. Adding a source type, a transform, a setting or a recipe is a minor. While the version is
`0.x`, a breaking change takes the minor.

---

[← Docs index](../README.md#documentation)
