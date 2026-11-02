# Getting started

The tool answers one question, *has upstream moved, and can we take it?*, and then does the taking.

Every command below is `icon-sets-sync <command>`. Under Docker that is
`docker compose run --rm ichava-maintainer-toolkit <command>`, and most have a `make` target.

## Validate the configs

```bash
icon-sets-sync config validate
```

Checks the registry, every pack config against `config/schema/pack.schema.json`, each pack's
`settings` block, that a recipe can run each pack, any set manifest a recipe reads, and the global config
if there is one. Exits 1 on any problem, naming the file and the JSON pointer of the bad value.

## See what it knows about

```bash
icon-sets-sync list-packs
```

Every pack in `config/packs.json`, with the version each pack repository records.

## Check without writing anything

```bash
icon-sets-sync check
```

Resolves each pack's `version_check_url`, compares against what the pack repo records, and reports
what is stale. Writes nothing. Exits 1 when any pack is behind, so it can gate a scheduled job.

## Refresh one pack

```bash
icon-sets-sync sync --pack=icon-sets-tabler
```

Fetches the new upstream, runs the pack's recipe, writes the assets, records the new version in the
pack's own `config.json`, commits to `chore/sync-upstream-<version>` and opens a pull request. A
human reviews the diff and merges; the tool never merges.

- `--dry-run` writes the assets into the pack's working tree and stops there: no version stamp, no
  commit, no pull request. `--commit` does the opposite, overriding a `dry_run` set anywhere else.
- `--force` refreshes even when the pack is already current.

**`sync` fails loudly.** It exits 1 when a pack's latest upstream version cannot be resolved (a dead
registry, a renamed package), and when a pack declares no sinks outside a dry run. The Python
printed a warning and exited 0 in both cases, so a broken upstream looked exactly like an up-to-date
pack. Other packs in the same run still go first.

## Refresh every pack

```bash
icon-sets-sync sync-all --dry-run --concurrency=2
```

`sync-all` is `sync --all`. `--concurrency` bounds how many packs run at once; the default is one,
which keeps a CI log in registry order.

## See the effective settings

```bash
icon-sets-sync config print --pack=icon-sets-emoji --origins
```

The merged settings for that pack and the layer each value came from. See
[Configuration](configuration.md#settings) for the layers.

## Try it interactively

```bash
icon-sets-sync menu
```

The default Docker command in a terminal: check all packs or one, sync one pack (it asks first), sync
every pack as a dry run, list the packs. A sync chosen in the menu runs after the menu closes, through
the same code as `sync`. Without a terminal, `menu` prints help instead of waiting for input that will
never come.

## What happens next

The pull request lands in the pack repository. **It arrives with no CI checks**: a pull request
opened with `GITHUB_TOKEN` does not trigger workflows, by design, so the pack's `sync-upstream.yml`
runs the suite itself and posts a `Sync verification` commit status. Merging to `main` runs the
pack's suite normally.

---

[← Docs index](../README.md#documentation)
