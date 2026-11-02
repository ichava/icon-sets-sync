# Release

How a version of `icon-sets-sync` is cut, and what it reaches.

## Where this package is

The first published release is cut by hand on a single root commit, the same day as every other ichava
package, and tagged `v0.1.0` with its `src/v0.1.0` module alias. The Go module proxy and the checksum
database already hold `github.com/ichava/icon-sets-sync/src` at v0.1.0, v0.1.1 and v0.1.2: the proxy
fetches a public tag from git the first time anyone asks for it, and keeps that copy for good. Those
copies come from commits before the single-commit publication, so `go install …@v0.1.0` serves the
older code, and that is why the docs never offer `go install`. `TestDocsOfferNoGoInstall` fails if a `go install` of this module comes back into the README,
`CONTRIBUTING.md` or `docs/`. The earlier 0.1.x sections and the pre-reset Python
`ichava/maintainer-toolkit` sections (v0.1.0 to v0.1.3, 2026-08-31 to 2026-09-22) are kept in
`CHANGELOG.md` under *Archived history*.

The Docker image is published to GHCR and is **private**, which is why the pack workflows build from
a checkout of this repository rather than pulling it: another repository's `GITHUB_TOKEN` cannot pull
a private package.

## Binary releases are off

**No binary release is published until the maintainers decide on one.** A `vX.Y.Z` tag is git only:
it records the version and starts nothing. Users build from source (`make build-local`, see the
README). `release.yml` is manual and refuses to run without an explicit confirmation, and
`docker.yml` no longer publishes on a tag. When a binary release is decided on, dispatch it against
an existing tag:

```bash
gh workflow run release.yml -R ichava/icon-sets-sync --ref vX.Y.Z -f confirm=release-go
```

v0.1.0's release run was cancelled on 2026-11-02 before GoReleaser finished, so v0.1.0 has no GitHub
release. Its tags (`v0.1.0`, `src/v0.1.0`) are final and stay where they are.

## Cutting a release

1. `go -C src vet ./...`, `go -C src test -count=1 ./...` and `gofmt -l src` (empty). `make test-go`
   runs the first two in a container. The suite covers config loading and the schema, the settings
   precedence, the checker's version comparison, the pipeline, the recipes, git and gh invocation,
   the sync orchestration, and the vendored-version contract.
2. Write the notes in `CHANGELOG.md`'s one `## [Unreleased]` section, on `main`, through the pull
   requests that make the changes. The GitHub release body is that version's section;
   auto-generated notes are not a substitute.
3. Run *Release Please* from the Actions tab (it also runs every Monday). It opens or refreshes one
   release pull request that bumps `.release-please-manifest.json` and closes `[Unreleased]` as
   `## [X.Y.Z] - date` on that branch; it fails when `[Unreleased]` is empty. Merging it tags
   `vX.Y.Z`. That is the whole release while binary releases are off.

On 0.x a breaking change (`feat!:` or a `BREAKING CHANGE:` footer) bumps the minor and everything
else bumps the patch. icon-sets-sync is versioned independently of the coupled ichava packages.
Never edit the release pull request by hand: release-please rebuilds its branch and drops anything
it did not write. Change the notes on `main` instead.

When a binary release is decided on, `release.yml` is dispatched by hand against the tag (above).
It and `docker.yml` publish:

| Workflow | Publishes |
|---|---|
| `release.yml` | the GitHub release: bare binaries for both entrypoints (`icon-sets-sync`, `imt`), `.tar.gz`/`.zip` archives, `.deb`/`.rpm`/`.apk`, a source tarball, an SPDX SBOM per archive, `checksums.txt`, its keyless cosign signature `checksums.txt.sigstore.json`, and a build-provenance attestation for every artifact |
| `docker.yml` | the GHCR image |

A dispatched `release.yml` also pushes `src/vX.Y.Z` on the same commit before GoReleaser runs. The Go module is
`github.com/ichava/icon-sets-sync/src`, and Go resolves a subdirectory module only through a tag
named after the subdirectory. **A `src/` tag is permanent once the Go proxy has fetched it**: never
move or recreate one, and the same holds for every `vX.Y.Z`. The step leaves an existing alias alone.

**Vendored web assets and the SBOM.** The web UI embeds Tabler (with the Bootstrap and Popper it
bundles) and a Tabler Icons subset as committed files, not as an npm dependency, so Syft's SBOM does
not list them. Their exact versions are in `src/internal/web/assets/vendor/TABLER_VERSION`, their
hashes in `MANIFEST.sha256`, and their licences in `THIRD_PARTY_NOTICES.md`, which every archive and
Linux package carries. Four pinned MIT components are clearer there than in a generated SBOM; move
them with `make tabler-update` before a release, never by hand.

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
