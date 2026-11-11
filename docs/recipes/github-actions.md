# Run icon-sets-sync in GitHub Actions

Build the binary from source in the job, then run `doctor`, `validate`, the upstream check and a dry-run
sync. Every step fails the job on a real problem and reports to the job page.

This builds from a pinned commit. icon-sets-sync is git-only: there is no published binary to download.

```yaml
name: Packs

on:
  schedule:
    - cron: '45 6 * * 1'
  workflow_dispatch:

permissions:
  contents: read

jobs:
  packs:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    env:
      GOPRIVATE: github.com/ichava/*
      GONOSUMDB: github.com/ichava/*
      NO_COLOR: '1'
    steps:
      - uses: actions/checkout@v7
        with:
          repository: ichava/icon-sets-sync
          ref: <full commit sha>
          path: toolkit
      - uses: actions/checkout@v7
        with:
          repository: ichava/icon-sets-tabler
          path: packs/icon-sets-tabler
      - uses: actions/setup-go@v7
        with:
          go-version-file: toolkit/src/go.mod
          cache-dependency-path: toolkit/src/go.sum
      - name: Build
        run: go -C toolkit/src build -o "$RUNNER_TEMP/icon-sets-sync" ./cmd/icon-sets-sync

      - name: Doctor
        run: '"$RUNNER_TEMP/icon-sets-sync" --config-dir toolkit/config --packages-dir packs doctor'

      - name: Validate the pack checkouts
        run: '"$RUNNER_TEMP/icon-sets-sync" --config-dir toolkit/config --packages-dir packs validate'

      - id: check
        name: Upstream check
        run: '"$RUNNER_TEMP/icon-sets-sync" --config-dir toolkit/config --packages-dir packs check-upstream --fail-on-outdated --pack icon-sets-tabler'
        continue-on-error: true
        env:
          GH_TOKEN: ${{ github.token }}

      - name: Dry run
        if: steps.check.outcome == 'failure'
        run: '"$RUNNER_TEMP/icon-sets-sync" --config-dir toolkit/config --packages-dir packs --output json sync --pack icon-sets-tabler --dry-run > sync.json'
        env:
          GH_TOKEN: ${{ github.token }}
```

## What the binary does in a job

| | |
|---|---|
| No prompts | without a terminal, a question is a usage error (exit 2) unless `--yes` answers it; `--non-interactive` makes that explicit |
| No terminal UI | `menu` and the no-argument launch need a terminal; in CI, no arguments prints the help |
| No colour | `--no-color` or `NO_COLOR` |
| Machine output | `--output json` (or `--json`): stdout is one JSON document, and status lines go to stderr |
| Annotations | detected from `GITHUB_ACTIONS=true` with `GITHUB_STEP_SUMMARY` and `GITHUB_OUTPUT` set, or forced with `--github` (`--github=false` turns it off): `::error file=…::` and `::warning::` annotations, a table in the step summary, outputs in `$GITHUB_OUTPUT`, and one `::group::` per synced pack |
| Notifications | silent in CI: desktop notifications are off when `CI` is set |
| Configs | from `--config-dir`, `--packages-dir` and `ICHAVA_*`, never only from where the binary sits |

## Exit codes

| Code | Meaning |
|---|---|
| `0` | done, nothing wrong |
| `1` | a failure: a pack failed or could not be checked, a stale pack under `--fail-on`, an invalid config or pack, a missing tool in `doctor` |
| `2` | a usage error: a bad flag, contradicting flags, or a question with no terminal and no `--yes` |
| `3` | needs review: a sync (or its dry run) would rename or remove shipped icons; every icon is listed. A failure in the same run wins: `1` |

## Outputs

| Command | `$GITHUB_OUTPUT` |
|---|---|
| `check` | `stale`, `errored`, `stale-packs` |
| `sync`, `sync-all` | `synced`, `failed`, `synced-packs`, `failed-packs`, `review`, `review-packs` |

`validate` writes a table to the step summary and an annotation on each file with a problem.

---

[← Docs index](../../README.md#documentation)
