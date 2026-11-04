# Configuration

One config file per pack in `config/`, a registry listing them, an optional global config, and
settings that resolve across five layers.

## The registry

```json
{ "packs": ["icon-sets-tabler", "icon-sets-flag", "icon-sets-emoji", "icon-sets-openmoji"] }
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
| `recipe` | `auto` (default), `generic`, `emoji-sets`, `openmoji-sets` or `manifest-pack`; see [Recipes](#recipes) |
| `licence` | Optional SPDX id the upstream must keep (`CC-BY-4.0` or `CC-BY-SA-4.0`). A sync whose upstream licence text no longer opens with that licence's title fails before the pack is touched; see [Upstream licence texts](architecture.md#upstream-licence-texts-travel-with-the-icons) |
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
| `script` | nothing | declared by the earlier implementation's schema and never implemented; kept so old configs load |

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

### What `sanitise` enforces on CSS values

A value in `style`, or in `fill`, `stroke`, `filter`, `clip-path`, `mask` or `marker-*`, is read as CSS
after its comments are stripped, its escapes decoded and its case folded, so `\75rl(` and `u/**/rl(`
are seen as `url(`. Then:

- `url(` must name a same-document fragment, `url(#grad)`, with its quotes matched.
- Any other function must be a colour, `var`, math, transform or easing function (`rgb`, `oklch`,
  `color-mix`, `var`, `calc`, `clamp`, `translate`, `cubic-bezier`, `steps` and their siblings).
  `image-set()`, `src()`, `image()`, `cross-fade()`, `element()` and `expression()` are not.
- Any `@` rule fails the value.

A value that fails is dropped whole, like any other disallowed attribute.

Three presentation attributes have a narrower rule of their own, on top of that guard:

- `display` survives only as bare keywords: `none`, `inline`, `block`, `inline flow-root`. No
  parens, no `url()`, no escape. OpenMoji hides construction shapes in its subdivision flags with
  `display="none"`, and stripping it makes them render.
- `color` takes the CSS guard above.
- `marker-start`, `marker-mid` and `marker-end` survive only as `none` or `url(#fragment)`.

Inkscape's `sodipodi:docname` is metadata and is stripped.

### Stray text

Text or CDATA that sits directly under `<svg>` or `<g>` is an upstream defect, not content: it
renders as a stray glyph. OpenMoji 15.1.0's `1F684.svg` (high-speed train) carries a `d` line there.
`sanitise` drops it, keeps the indentation around it, and logs each file with the number of runs
removed (`sanitise: dropped stray text file=… count=…`). Text inside `<title>`, `<desc>`, `<text>` and
`<tspan>` is left alone.

If more than 25 files in one `sanitise` would be rewritten for stray text, the run fails before
anything is written, so a malformed upstream cannot quietly rewrite a pack. `--force` lets it
through with a warning. `report_only` writes nothing and is never refused.

A document over 4,500,000 bytes or 30,000 elements is refused before it builds a tree. It is treated like an
unparseable file: counted and left as it was. The caps are the `MaxBytes` and `MaxNodes` fields on the
policy, and match react-icon-browser's and core's; the pack SVGs measure at most 1.0 MB and 6,741 elements.

## Download and extraction limits

Sources fetch from pinned upstreams, so these limits only bite if one is compromised. Each fails the
run with an error that names the limit; none is retried, because a retry would read the same bytes.

| Limit | Value | Where |
|---|---|---|
| Version-check and registry JSON | 32 MiB | `httpx.DefaultMaxJSONBytes` |
| One archive download | 1 GiB | `httpx.DefaultMaxDownloadBytes` |
| One extracted file | 256 MiB | `sources.DefaultExtractLimits.MaxMemberBytes` |
| Everything extracted from one archive | 2 GiB | `MaxTotalBytes` |
| Entries in one archive | 200,000 | `MaxEntries` |

The extraction caps are enforced on the bytes actually written, not on a size an archive header
declares, because a decompression bomb is exactly the archive whose header lies. Path escapes, symlinks,
hard links and device members were already refused.

**Redirects.** The HTTP client follows at most 10, and refuses one that leaves `https` for `http`,
leaves `http(s)` altogether, lands on a host that is not on the allowlist, or lands on a literal
loopback, private or link-local address (the cloud metadata service is `169.254.169.254`).

## Upstream hosts and https

Every fetch the HTTP client makes, and every redirect hop, must be `https` and must go to an allowed host.
It is checked before any request is sent, and a refusal is never retried.

| Default host | Used for |
|---|---|
| `registry.npmjs.org` | version checks of the npm-backed packs (tabler, flag, emoji) |
| `api.github.com` | the openmoji version check |
| `github.com` | the OpenMoji tag archive |
| `codeload.github.com` | where `github.com` redirects the archive to |
| `unicode.org` | the CLDR `emoji-test.txt` the emoji categoriser reads |

A test loads every shipped pack config, builds its pipelines and fails if any URL they reach is off this
list, so adding a recipe that fetches from a new host means adding the host here in the same change.
Release-asset hosts (`objects.githubusercontent.com`, `release-assets.githubusercontent.com`) are not on
it, because no shipped recipe downloads a release asset.

Plain `http` is refused in a pack config when it loads and again when it is fetched. The exception is a
test's own server: `http://127.0.0.1`, `http://localhost` and `http://[::1]`, on the first request only.

To fetch from another host, for a mirror or a private pack, add it to `allowed_hosts`. It is run-wide
and cannot be set in a pack config, so editing a pack from the web UI cannot widen where the toolkit
fetches from:

```bash
icon-sets-sync --set allowed_hosts=mirror.example,cdn.mirror.example sync --pack=<slug>
ICHAVA_ALLOWED_HOSTS=mirror.example icon-sets-sync sync --pack=<slug>
```

```json
{ "allowed_hosts": "mirror.example" }
```

(that last one in the global config). Names only: no scheme, port, path or wildcard, and a host does not
cover its subdomains. A blocked fetch fails with the URL, the host and this setting's name. The setting
is read when the program starts, so `serve` needs a restart to pick up a change. `npm pack` talks to the
registry through npm and its own configuration, not through this client.

**`npm pack` runs with `--ignore-scripts`**, so a git or local spec in a config cannot run a package's
`prepare` script on the runner.

## Kinds

A pack declares what its items are (`icon` or `illustration`) in its own
`resources/assets/svg/config.json`, and `ichava/core` reads it. `icon-sets-sync` does not decide a kind;
it keeps those keys intact and can suggest where one may be wrong.

| Key | Meaning |
|---|---|
| `package.kind` | the pack's default |
| `metadata.data.categories.<slug>.kind` | a category (the first folder under `files/`) |
| `metadata.data.kinds.rules[]` | `{ "match": <glob>, "kind": <kind> }`; the **first** matching rule wins |
| `metadata.data.items.<item path>.kind` | one item |

An item path is the file's path under `files/` with `.svg` removed, for example
`illustrations/office`. A key at the top level of the file (`items`, `kinds`, `categories`) is read as a
fallback when the `metadata.data` one is absent or empty. Precedence, first hit wins: item, rule,
category, pack, then `icon`. A pack with none of these keys reads as all `icon`.

**Globs** match the whole item path, case-sensitively. `*` is any run of characters except `/`, `?` is
one character except `/`, and `**` is zero or more whole path segments (`scenes/**`, `**/hero-*`,
`a/**/b`). A pattern with `[ ] { } \`, an empty or `..` segment, or a `**` that is not a whole segment
is invalid and the rule is ignored. The implementation is `internal/core/kinds/glob.go`, and core's
`Support\KindGlob` carries the same test vectors.

**A sync never touches these keys.** The version stamp edits one value in place and leaves every other
byte of `config.json` alone; a golden test (`kind_passthrough_test.go`) holds that.

### Suggesting kinds

```bash
icon-sets-sync check --suggest-kinds            # or: sync --pack=<slug> --suggest-kinds
icon-sets-sync check --suggest-kinds=all
```

Scans each pack's checkout and writes `build/kind-suggestions/<pack>.json` (change the directory with
`--kind-suggestions-dir`), an array of records. **It only suggests.** Nothing writes a kind into
`config.json`; a person copies a record's `accept` block in, which is the acceptance.

A file is measured on its viewBox, size, element count and literal colours. These signals fire:

| Signal | Fires when |
|---|---|
| `aspect` | viewBox width/height is outside 0.8 to 1.25 |
| `palette` | 4 or more distinct literal colours (`fill`, `stroke`, `stop-color`) |
| `bytes` | more than 20,000 bytes |
| `elements` | more than 150 elements |
| `canvas` | the larger viewBox side is more than 256 |

A suggestion needs 3 or more signals, at least one of them `bytes` or `elements` (which keeps small
multicolour 4:3 flags out), and an item that resolved `icon` **by default**. Items whose kind came from
an item entry, a rule or a category are a person's decision and are never second-guessed. With
`--suggest-kinds=all`, items that are `icon` only through `package.kind` are included too.

When a folder holds at least 5 items and at least 80% are suggested, they collapse into one record for
the folder with an `accept` of `{ "kinds": { "rules": [{ "match": "<folder>/**", "kind": "illustration" }] } }`.
Theme twins (a category's `theme_variants`, such as `scene-dark.svg`) are not items and are not scanned.
A file that cannot be parsed is listed as skipped. The scan warns about, and does not fail on, an
unreadable pack; `check` keeps its exit 1 for a stale pack only.

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
| `openmoji-sets` | `icon-sets-openmoji` | OpenMoji colour and black from one pinned `hfg-gmuend/openmoji` release tag (no `v` prefix), categorised by the Unicode taxonomy, then one commit. Runs the same pipelines as the OpenMoji sets inside `emoji-sets`, plus three protections they lack (below) |
| `manifest-pack` | `"recipe": "manifest-pack"` | one pipeline per set in the pack's `<slug>.sets.json` set manifest, each from its own upstream, then one commit |
| `generic` | any `npm` or `github-*` source | the config's own source, transforms and sinks |

#### `openmoji-sets` never drops an icon

The deprecated `emoji-sets` recipe files an icon only when its filename matches a `emoji-test.txt` entry exactly, and drops the rest (its log line says `missing=N`). `openmoji-sets` opts out of that:

- **Selector-insensitive match.** OpenMoji names about 200 emoji without the emoji-presentation selector (`2600.svg`, the sun; `emoji-test.txt` has `2600 FE0F`). They match their taxonomy entry anyway.
- **An explicit bucket.** An icon still unmatched, such as an OpenMoji extra (`e380.svg`), an emoji newer than the taxonomy, or a component glyph, is kept as `uncategorised/<upstream-name>.svg`, with a warning and an `uncategorised` count. Two emoji that slug to one name (`keycap: #` and `keycap: *`) keep both: the later takes the name, the earlier goes to the bucket.
- **The taxonomy follows the release.** `emoji.unicode_version` defaults to the OpenMoji release's major and minor, unless something above the built-in default sets it.
- **Two guards, both lifted by `--force`.** The run fails if more than 20% of the icons are uncategorised (a taxonomy of the wrong release), and the file sink refuses, before it wipes anything, to remove an icon the pack ships today. Removing one on purpose (upstream retired it) needs `--force`; the removals are logged.


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
| `config_dir` | `config/` beside the binary, else the checkout found walking up from the working directory, then from the binary, else the configs embedded in the binary | `ICHAVA_CONFIG_DIR` (older: `ICHAVA_DEV_CONFIG_DIR`) | no | where the registry and pack configs live |
| `private_config_dir` | none | `ICHAVA_PRIVATE_CONFIG_DIR` | no | private pack configs merged over `config_dir` at run time; see [Private packs](#private-packs) |
| `log_level` | `info` | `ICHAVA_LOG_LEVEL` (older: `ICHAVA_DEV_LOG_LEVEL`) | no | `debug`, `info`, `warn`, `error` |
| `concurrency` | `1` | `ICHAVA_CONCURRENCY` | no | packs synced at once by `sync-all` |
| `workspace` | none | `ICHAVA_WORKSPACE` | no | directory a relative `pack_root` resolves against |
| `allowed_hosts` | none | `ICHAVA_ALLOWED_HOSTS` | no | extra upstream hosts the client may fetch from, comma separated; see [Upstream hosts and https](#upstream-hosts-and-https) |
| `dry_run` | `false` | `ICHAVA_DRY_RUN` | yes | fetch and diff, write nothing into the pack, skip stamp, commit and pull request |
| `force` | `false` | `ICHAVA_FORCE` | yes | refresh even when current |
| `git.author` | `ichava-sync-bot <41898282+github-actions[bot]@users.noreply.github.com>` | `ICHAVA_GIT_AUTHOR` | yes | author and committer, `Name <email>` |
| `git.base` | `main` | `ICHAVA_GIT_BASE` | yes | the pull request's base branch |
| `git.branch` | `chore/sync-upstream-{version}` | `ICHAVA_GIT_BRANCH` | yes | the sync branch |
| `git.commit_subject` | `chore(upstream): bump {pack} to v{new}` | `ICHAVA_GIT_COMMIT_SUBJECT` | yes | commit and pull request title |
| `git.commit_body` | `Automated sync from upstream {pack} {old} -> {new}. ...` | `ICHAVA_GIT_COMMIT_BODY` | yes | commit and pull request body |
| `git.open_pr` | `true` | `ICHAVA_GIT_OPEN_PR` | yes | open a pull request after pushing |
| `emoji.unicode_version` | `16.0` | `ICHAVA_EMOJI_UNICODE_VERSION` | yes | Unicode release whose `emoji-test.txt` drives categorisation; must exist at unicode.org (up to 16.0 under `Public/emoji/<v>/`, from 17.0 under `Public/<v>.0/emoji/`). `openmoji-sets` follows its OpenMoji release instead (17.0.0 uses 17.0) unless this is set |
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
this repository's `config/`, the runner's throwaway checkout, and never committed, so every scheduled
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
