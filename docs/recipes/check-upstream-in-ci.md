# Check upstream in CI

Run the upstream check, or a sync, from a GitHub Actions job and read the result from the job page.

```yaml
name: Upstream check

on:
  schedule:
    - cron: '45 6 * * 1'
  workflow_dispatch:

permissions:
  contents: read

jobs:
  check:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - uses: actions/checkout@v7
        with:
          repository: ichava/icon-sets-sync
          ref: <sha>
      - uses: actions/setup-go@v7
        with:
          go-version-file: src/go.mod
      - run: go -C src build -o "$RUNNER_TEMP/icon-sets-sync" ./cmd/icon-sets-sync
      - id: check
        run: "$RUNNER_TEMP/icon-sets-sync" --non-interactive check --github --fail-on any
        env:
          GH_TOKEN: ${{ github.token }}
      - if: always()
        run: echo "behind upstream: ${{ steps.check.outputs.stale-packs }}"
```

| Flag | What it does |
|---|---|
| `--fail-on stale` | exit 1 when a pack is behind upstream (the default) |
| `--fail-on error` | exit 1 when a pack's upstream could not be checked |
| `--fail-on any` | either of the two |
| `--fail-on never` | report only |
| `--github` | write a table to `$GITHUB_STEP_SUMMARY`, `stale`, `errored` and `stale-packs` to `$GITHUB_OUTPUT`, and a warning or error annotation per pack |

`sync --github` and `sync-all --github` write the same kind of summary, with `synced`, `failed`,
`synced-packs` and `failed-packs` as outputs. `--github` outside Actions is a usage error. With
`--json`, stdout stays the JSON document and no annotations are printed.

`GH_TOKEN` (or `GITHUB_TOKEN`) is sent to `api.github.com` only, and lifts the anonymous limit of 60
requests an hour that ten packs' version checks share. `--workspace <dir>` points the shipped
configs' `/work/<pack>` at the directory holding the pack checkouts, instead of linking `/work`.

To refresh a pack and open its pull request, call the reusable `sync-pack.yml` instead; see
[Maintainer sync](../maintainer-sync.md).

---

[← Docs index](../../README.md#documentation)
