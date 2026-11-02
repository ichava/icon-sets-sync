# Configuration

One config file per pack in `config/`, a registry listing them, an optional global config, and
settings that resolve across five layers.

## The registry

```json
{ "packs": ["icon-sets-tabler", "icon-sets-flag", "icon-sets-emoji"] }
```

`config/packs.json` (or `packs.yaml`). Each slug maps to `config/<slug>.json`, `.yaml` or `.yml`.
Two of those for one slug is an error rather than a preference: it means a file was converted and
the original left behind. A slug listed twice is an error too.

## Private packs

A pack whose configuration may not be published -- a private or client pack -- keeps its configs in
its own repository and hands them to the toolkit at run time:

```bash
icon-sets-sync --private-config-dir /path/to/private sync --pack=<slug>
ICHAVA_PRIVATE_CONFIG_DIR=/path/to/private icon-sets-sync sync --pack=<slug>
```

The private directory has the same layout as `config/`: an optional `packs.json`, one `<slug>.json`
per pack, and any file a recipe reads beside its pack config, such as a `<slug>.sets.json` set
manifest. It is merged with `config/` rather than replacing it:

- the registry is the public one followed by every private slug it does not already list;
- a file present in both is read from the private directory, so a pack can be overridden without
  forking the toolkit;
- a private directory that is set but missing fails the run, so a mount that did not happen never
  reads as "no private packs".

Recipes stay data-driven, so a private pack needs no Go code here: it names an existing recipe in its
config (`"recipe": "manifest-pack"` for one pipeline per set in its set manifest) and supplies the data.
Nothing describing a private pack is committed to this repository; a test fails on any tracked file
that names a private pack's paths or config files.

## A pack config

```json
{
  "name": "icon-sets-tabler",
  "pack": "ichava/icon-sets-tabler",
  "pack_root": "/work/icon-sets-tabler",
  "current_version": "3.46.0",
  "version_check_url": "https://registry.npmjs.org/@tabler/icons/latest",
  "source": { "type": "npm", "package": "@tabler/icons", "source_path": "icons" },
  "sinks": [
    { "type": "filesystem", "root": "{pack_root}/resources/assets/svg/files" },
    { "type": "git-branch", "repo_root": "{pack_root}", "branch": "chore/sync-upstream-{version}" }
  ]
}
```

The same file in YAML is equally valid, and decodes to the same document:

```yaml
name: icon-sets-tabler
pack: ichava/icon-sets-tabler
pack_root: /work/icon-sets-tabler
version_check_url: https://registry.npmjs.org/@tabler/icons/latest
source: { type: npm, package: "@tabler/icons", source_path: icons }
sinks:
  - { type: filesystem, root: "{pack_root}/resources/assets/svg/files" }
  - { type: git-branch, repo_root: "{pack_root}" }
```

Quote versions in YAML. An unquoted `16.0` is a number, and the schema refuses it rather than
turning it into `"16"`.

| Key | Meaning |
|---|---|
| `name` | pack slug; must match the filename and the registry entry |
| `pack` | Composer package name, e.g. `ichava/icon-sets-tabler` |
| `pack_root` | the pack repo: absolute, `~`-prefixed, or relative to the `workspace` setting |
| `version_check_url` | the URL polled to discover the latest upstream version |
| `source` | where the assets come from; see [Sources](#sources) |
| `sinks` | where they land, in order; see [Sinks](#sinks) |
| `transforms` | what happens in between; see [Transforms](#transforms) |
| `recipe` | `auto` (default), `generic`, `emoji-sets` or `manifest-pack`; see [Recipes](#recipes) |
| `settings` | this pack's layer of the [settings](#settings) |
| `version_file` | pack-repo-relative file recording the vendored version; default `resources/assets/svg/config.json` |
| `version_keys` | dot-paths inside that file; default `upstream.current_version`, `package.upstream_version` |
| `current_version` | **fallback only**; see [where the version lives](#where-the-vendored-version-actually-lives) |

Top-level keys beginning with `_` are authoring notes and are ignored. A `$schema` key is accepted
as an editor hint and ignored.

## The schema

[`config/schema/pack.schema.json`](../config/schema/pack.schema.json) (JSON Schema 2020-12) is the
contract. Every pack config is validated against it on load, before anything reaches a registry, and
every violation is reported at once with its JSON pointer:

```text
config/icon-sets-x.json: does not match config/schema/pack.schema.json:
  /source: missing property 'package'
  /sinks/0: missing property 'root'
```

The binary embeds a copy, so a config is validated identically wherever it runs; a test fails if
the copy and the repository file differ. Point an editor at the repository file for completion.

`icon-sets-sync config validate` runs every check without syncing anything.

## Sources

| `type` | Fetches | Notes |
|---|---|---|
| `npm` | `npm pack <package>@<version>` | needs `package` |
| `github-archive`, `github-release`, `github-tag` | the tagged archive | `archive_url` (with `{version}`), or `owner` and `repo`, which builds `.../archive/refs/tags/v{version}.zip` |
| `url` | nothing | version discovery only: `version_field` is a dot-path into the `version_check_url` response, default `version` |
| `script` | nothing | declared by the Python schema and never implemented; kept so old configs load |

`source_path` narrows the fetched tree to a subdirectory before the transforms run.

## Transforms

With none declared, a pack gets the default: `sanitise`. Declared transforms replace it and run in
order.

| `type` | Options | Does |
|---|---|---|
| `subset` | `subdir` | narrow the tree to a subdirectory |
| `sanitise` | `also_strip_class`, `strict`, `report_only` | apply the shared SVG policy in place |
| `slugify` | | lowercase and hyphenate filenames |
| `categorise` | `by`: `cldr` or `passthrough` | file emoji by Unicode group; `cldr` adds the taxonomy fetch automatically |
| `indexer` | `targets`: `codepoints`, `names` | write the emoji lookup files |
| `flatten-variants` | `variants` (`path`, `prefix`, `suffix`), `separator` | merge several upstream directories into one flat set |

## Sinks

| `type` | Options | Does |
|---|---|---|
| `filesystem` | `root` (required), `incremental`, `min_retention` | wipe `root`, then copy every SVG in; `incremental` skips the wipe, `min_retention` (0 to 1) refuses to replace a populated directory with far fewer files |
| `git-branch` | `repo_root`, `branch`, `commit_subject`, `commit_body`, `base`, `open_pr` | commit everything uncommitted in the pack repo, push the branch, open a pull request |

`{pack_root}` and `{version}` are interpolated in paths and the branch; `{pack}`, `{old}` and `{new}`
in the commit subject and body. A version stamp is inserted before every `git-branch` sink.

**A pack with no sinks is refused** by `sync` and `recipe` unless it is a dry run, because it would
fetch, transform and then write nothing.

The `git-branch` fields are the pack's values for the matching `git.*` settings: they beat the
defaults and the pack's `settings` block, and give way to the global config, the environment and
flags.

## Recipes

A recipe turns a pack config into pipelines. `auto` picks by pack name, then by source type:

| Recipe | Picked for | Builds |
|---|---|---|
| `emoji-sets` | `icon-sets-emoji` | Twemoji (npm) + OpenMoji colour and black (GitHub archive), categorised by the Unicode taxonomy, then one commit |
| `manifest-pack` | `"recipe": "manifest-pack"` | one pipeline per set in the pack's `<slug>.sets.json` set manifest, each from its own upstream, then one commit |
| `generic` | any `npm` or `github-*` source | the config's own source, transforms and sinks |

`manifest-pack` is never picked by `auto`: a pack config has to say `"recipe": "manifest-pack"`.

## Settings

Settings are the knobs that are not about one pack's upstream: dry runs, the git identity, the base
branch, how many packs run at once. Each resolves from five layers, highest first:

```text
flags  >  ICHAVA_* environment  >  global config  >  pack config `settings`  >  defaults
```

A layer replaces only the keys it sets. An unknown key in any layer is an error, not a no-op: a
misspelt `dry_rnu: true` that silently did nothing would commit.

| Setting | Default | Environment | Per pack | Meaning |
|---|---|---|---|---|
| `config_dir` | `config/` beside the binary, else `./config` | `ICHAVA_CONFIG_DIR` (older: `ICHAVA_DEV_CONFIG_DIR`) | no | where the registry and pack configs live |
| `private_config_dir` | none | `ICHAVA_PRIVATE_CONFIG_DIR` | no | private pack configs merged over `config_dir` at run time; see [Private packs](#private-packs) |
| `log_level` | `info` | `ICHAVA_LOG_LEVEL` (older: `ICHAVA_DEV_LOG_LEVEL`) | no | `debug`, `info`, `warn`, `error` |
| `concurrency` | `1` | `ICHAVA_CONCURRENCY` | no | packs synced at once by `sync-all` |
| `workspace` | none | `ICHAVA_WORKSPACE` | no | directory a relative `pack_root` resolves against |
| `dry_run` | `false` | `ICHAVA_DRY_RUN` | yes | write files, skip stamp, commit and pull request |
| `force` | `false` | `ICHAVA_FORCE` | yes | refresh even when current |
| `git.author` | `ichava-sync-bot <41898282+github-actions[bot]@users.noreply.github.com>` | `ICHAVA_GIT_AUTHOR` | yes | author and committer, `Name <email>` |
| `git.base` | `main` | `ICHAVA_GIT_BASE` | yes | the pull request's base branch |
| `git.branch` | `chore/sync-upstream-{version}` | `ICHAVA_GIT_BRANCH` | yes | the sync branch |
| `git.commit_subject` | `chore(upstream): bump {pack} to v{new}` | `ICHAVA_GIT_COMMIT_SUBJECT` | yes | commit and pull request title |
| `git.commit_body` | `Automated sync from upstream {pack} {old} -> {new}. ...` | `ICHAVA_GIT_COMMIT_BODY` | yes | commit and pull request body |
| `git.open_pr` | `true` | `ICHAVA_GIT_OPEN_PR` | yes | open a pull request after pushing |
| `emoji.unicode_version` | `16.0` | `ICHAVA_EMOJI_UNICODE_VERSION` | yes | Unicode release whose `emoji-test.txt` drives categorisation; must exist at unicode.org/Public/emoji/ |
| `emoji.openmoji_version` | `15.1.0` | `ICHAVA_EMOJI_OPENMOJI_VERSION` | yes | OpenMoji release tag |

The run-wide settings (the four marked "no") cannot appear in a pack config's `settings`.

### Flags

`--set key=value` sets any of them, and is repeatable. The common ones have their own flags:
`--config-dir`, `-v` (`log_level=debug`), `-q` (`log_level=error`), `--dry-run`, `--commit`
(`dry_run=false`), `--force`, `--concurrency`. A flag counts only when it is given: an unset
`--dry-run` does not override `ICHAVA_DRY_RUN=1`.

### The global config

Optional. `$XDG_CONFIG_HOME/icon-sets-sync/config.json`, `.yaml` or `.yml`, falling back to
`~/.config/icon-sets-sync/` when `XDG_CONFIG_HOME` is unset (macOS included). `--global-config` or
`ICHAVA_GLOBAL_CONFIG` names another file. It nests the dotted names:

```yaml
concurrency: 2
git:
  base: main
  open_pr: true
emoji:
  unicode_version: "16.0"
```

### A pack's settings

```json
{
  "name": "icon-sets-emoji",
  "settings": { "emoji": { "unicode_version": "16.0" }, "git": { "base": "main" } }
}
```

### Seeing the result

`icon-sets-sync config print --origins [--pack <slug>] [--format yaml]` prints the merged settings
and the layer each came from; with `--pack`, the loaded pack config and the recipe too.

## Where the vendored version actually lives

**In the pack repo, not here.** `version_file` and `version_keys` point at the pack's own
`resources/assets/svg/config.json`, which travels with the assets it describes.

`current_version` in this repository is a fallback for a pack that has no vendored record yet. It
used to be the source of truth, and that made the sync non-convergent: the bump was written through
the `/app/config` mount, the runner's throwaway checkout, and never committed, so every scheduled
run re-detected the same release and re-opened the same pull request. The version stamp now writes
into the pack repo, before the `git-branch` sink commits.

## Adding a pack

1. Write `config/<slug>.json` (or `.yaml`) describing the upstream and the sinks.
2. Add the slug to `config/packs.json`.
3. `icon-sets-sync config validate`, then `icon-sets-sync check --pack=<slug>` to confirm the
   upstream poll resolves.

Most packs need only config: an npm or GitHub source, a `source_path`, perhaps a transform list.
If the upstream's shape genuinely differs (several sources, a taxonomy), it needs a recipe; see
[Architecture](architecture.md#recipes).

---

[← Docs index](../README.md#documentation)
