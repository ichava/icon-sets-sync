# Release

How a version of `icon-sets-sync` is cut, and what it reaches.

## Where this package is

`v0.1.0`, re-based on 2026-10-23 with every other ichava package. It is the first release of the Go
binary and the first to ship binaries. The pre-reset tags `v0.1.0` to `v0.1.3` (2026-08-31 to
2026-09-22) were the Python `ichava/maintainer-toolkit`; they were removed, their commits are on `main`,
and their notes are kept in `CHANGELOG.md` under *Archived pre-reset history*.

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
3. Tag `vX.Y.Z` (annotated) on `main` and push that tag alone.

The tag starts two workflows:

| Workflow | Publishes |
|---|---|
| `release.yml` | the GitHub release: bare binaries for both entrypoints (`icon-sets-sync`, `imt`), `.tar.gz`/`.zip` archives, `.deb`/`.rpm`/`.apk`, a source tarball, an SPDX SBOM per archive, `checksums.txt`, its keyless cosign signature `checksums.txt.sigstore.json`, and a build-provenance attestation for every artifact |
| `docker.yml` | the GHCR image |

`release.yml` also pushes `src/vX.Y.Z` on the same commit before GoReleaser runs. The Go module is
`github.com/ichava/icon-sets-sync/src`, and Go resolves a subdirectory module only through a tag
named after the subdirectory. **A `src/` tag is permanent once the Go proxy has fetched it**: never
move or recreate one. The step leaves an existing alias alone.

The release config is `src/.goreleaser.yaml`, run from the repository root. The Tests workflow
builds the whole release set on every pull request (`goreleaser release --snapshot`, no publish, no
signature) and asserts what came out, and its Docker job builds the image the way the pack
workflows do. Locally:

```bash
goreleaser check --config src/.goreleaser.yaml
goreleaser release --snapshot --clean --config src/.goreleaser.yaml --skip=sign,publish   # into build/dist
```

## Verifying a release

```bash
gh attestation verify icon-sets-sync_linux_amd64 --repo ichava/icon-sets-sync

cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/ichava/icon-sets-sync/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum -c checksums.txt --ignore-missing      # shasum -a 256 -c on macOS
```

No Homebrew tap, Scoop bucket or Winget manifest is published: ichava has none of those
repositories yet.

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
