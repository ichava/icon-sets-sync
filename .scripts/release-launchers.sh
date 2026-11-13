#!/usr/bin/env bash
# Build the double-click launchers for a release and attach them to it. Run by the shared
# simtabi/.github go-release.yml as its post-release command, from the repository root, after
# GoReleaser has published the release. GoReleaser does not build these: the macOS app bundle
# around the universal binary it just built, zipped (an .app is a folder), and the Windows GUI
# launchers (src/internal/tools/launchers, as `make app` and `make cross` run it).
#
# Needs TAG, VERSION, DIST (GoReleaser's dist directory), GH_TOKEN, GITHUB_REPOSITORY and
# RUNNER_TEMP, all set in the shared workflow. Writes checksums-launchers.txt beside the assets and
# attaches it too, and copies it to $RUNNER_TEMP/extra-checksums.txt so the launchers are attested.
set -euo pipefail
: "${TAG:?}" "${VERSION:?}" "${DIST:?}" "${GH_TOKEN:?}" "${GITHUB_REPOSITORY:?}" "${RUNNER_TEMP:?}"

out="$RUNNER_TEMP/launchers"
mkdir -p "$out"
# The published icon-sets-sync_macos_universal asset, at the build path GoReleaser recorded for it
# (flatdist reads artifacts.json the same way).
universal=$(jq -r '.[] | select(.name=="icon-sets-sync_macos_universal") | .path' "$DIST/artifacts.json")
[ -f "$universal" ] || { echo "::error::no universal binary in $DIST/artifacts.json"; exit 1; }
go -C src run ./internal/tools/launchers \
  -app "$out/icon-sets-sync.app" \
  -universal "../$universal" \
  -version "$VERSION" \
  -windows "$out"
(cd "$out" && zip -qry icon-sets-sync_macos_universal.app.zip icon-sets-sync.app && rm -rf icon-sets-sync.app)
# Written outside the folder first, so it does not list itself.
(cd "$out" && sha256sum -- * > "$RUNNER_TEMP/checksums-launchers.txt")
mv "$RUNNER_TEMP/checksums-launchers.txt" "$out/"
[ "$(find "$out" -name '*.exe' | wc -l)" -ge 3 ] || { echo "::error::expected at least 3 Windows launchers"; exit 1; }
ls -l "$out"
gh release upload "$TAG" --repo "$GITHUB_REPOSITORY" "$out"/*
cp "$out/checksums-launchers.txt" "$RUNNER_TEMP/extra-checksums.txt"
