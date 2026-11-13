# Configuration

One config file per pack in `config/`, a registry listing them, an optional global config, and
settings that resolve across five layers.

## The registry

```json
{ "packs": ["icon-sets-tabler", "icon-sets-flag", "icon-sets-twemoji", "icon-sets-openmoji"] }
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

## Naming packs

A pack name is a **lowercase slug**: letters `a-z` and digits, in words joined by single hyphens
(`^[a-z0-9]+(-[a-z0-9]+)*$`, at most 100 characters). The same string is the config's `name`, its
file name (`config/<name>.json`), its entry in `packs.json` and the value of `--pack`, and it ends
up in URLs (`/packs/<name>`), branch names and run history, so it has no dots, underscores,
capitals or spaces.

| Right | Wrong |
|---|---|
| `icon-sets-tabler` | `Icon-Sets-Tabler`, `icon_sets_tabler` |
| `icon-sets-extras-feather` (one config per set of a shared pack: `<pack>-<set>`) | `icon-sets-extras.feather` |

It is enforced, not advised:

- the loader refuses any other name in `packs.json`, a config's `name` or `--pack`, states the
  rule, and names the slug to use when there is one;
- `config/schema/pack.schema.json` carries the pattern, so an editor that reads `$schema` marks a
  bad name as you type;
- `TestEveryShippedNameIsASlug` fails on any shipped config, file name or registry entry that is
  not a slug.

The dotted `icon-sets-extras.<set>` names were renamed `icon-sets-extras-<set>` (icon-sets-sync#104)
and are no longer accepted.

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
| `name` | pack slug; must match the filename and the registry entry. A lowercase slug: see [Naming packs](#naming-packs) |
| `pack` | Composer package name, e.g. `ichava/icon-sets-tabler` |
| `pack_root` | the pack repo: absolute, `~`-prefixed, or relative to the `workspace` setting |
| `version_check_url` | the URL polled to discover the latest upstream version |
| `source` | where the assets come from; see [Sources](#sources) |
| `sinks` | where they land, in order; see [Sinks](#sinks) |
| `transforms` | what happens in between; see [Transforms](#transforms) |
| `recipe` | `auto` (default), `generic`, `twemoji-sets`, `openmoji-sets` or `manifest-pack`; see [Recipes](#recipes) |
| `licence` | Optional SPDX id the upstream must keep: `Apache-2.0`, `CC-BY-3.0`, `CC-BY-4.0`, `CC-BY-SA-4.0`, `CC0-1.0`, `ISC` or `MIT`. A sync whose upstream licence text no longer opens with that licence's lines fails before the pack is touched; see [Upstream licence texts](architecture.md#upstream-licence-texts-travel-with-the-icons) |
| `licence_sha256` | Optional SHA-256 one upstream licence file must have. A sync whose licence text changed by a byte fails before the pack is touched. For a licence with no standard text (Font Awesome Free, Material Design Icons, Game Icons), or to pin one exactly |
| `licence_set` | the directory under `resources/assets/svg/licenses/` the licence texts go to; default named after the npm package or GitHub repository |
| `licence_from` | the upstream directory, relative to its root, the licence files are read from; default the root. Noto Emoji's SVGs carry their own `2D/svg/LICENSE` |
| `licence_missing` | why the upstream ships no licence file, with the evidence. The sync runs and reports the gap; the pack supplies the text by hand beside a `source.md`, which keeps the sink from removing it |
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

A `github-archive` source reads its version from whatever GitHub endpoint `version_check_url` names: a
tag list (`/repos/<o>/<r>/tags`, newest first), a release (`/releases/latest` or `/releases/tags/<tag>`),
or a branch head (`/commits/<branch>`, whose `sha` is the version). The tag prefix is the text
`archive_url` puts between `refs/tags/` and `{version}` (`v`, `website@`, or nothing), and it is stripped,
so the version is exactly what `archive_url` expects.

`source_path` narrows the fetched tree to a subdirectory before the transforms run.

`max_download_bytes` raises the 1 GiB archive cap for one `github-*` source, up to 4 GiB. Only
Noto Emoji sets it: its tagged archive is 1.2 GB, almost all of it PNGs the pack does not take.

## Transforms

With none declared, a pack gets the default: `sanitise`. Declared transforms replace it and run in
order.

| `type` | Options | Does |
|---|---|---|
| `subset` | `subdir` | narrow the tree to a subdirectory |
| `sanitise` | `also_strip_class`, `strict`, `report_only`, `drop_unparsable` | apply the shared SVG policy in place; `drop_unparsable` removes a file the policy cannot parse instead of leaving it unchecked |
| `slugify` | `dirs`, `hyphenate_dots` | lowercase and hyphenate filenames; `dirs` slugs directory names too, `hyphenate_dots` turns a dot in the stem into a hyphen. Two names slugging to one fail the run |
| `categorise` | `by`: `cldr` or `passthrough` | file emoji by Unicode group; `cldr` adds the taxonomy fetch automatically |
| `indexer` | `targets`: `codepoints`, `names` | write the emoji lookup files |
| `flatten-variants` | `variants` (`path`, `prefix`, `suffix`), `separator`, `slug` | merge several upstream directories into one flat set; `slug` slugs each stem before the affix |
| `split-variants` | `variants` (`path`, `from`, `suffix`, `flatten_prefix`) | sort files into one directory per variant, by the directory they come from and the suffix they carry; see below |
| `exclude` | `names`, `directories`, `metadata` (`file`, `name`, `field`, `allow`) | drop icons a pack must not ship; see below |

### Variants: `split-variants`

Each entry names an output directory (`path`), the upstream directory it reads (`from`, default the
root) and the name suffix it claims (`suffix`, stripped from the name written). A file goes to the
entry with the longest suffix it ends with, else to the entry of its directory with no suffix. A
file no entry claims is dropped and counted, which is how a pack takes a subset. `flatten_prefix`
also reads `from`'s sub-directories and prefixes each name with its sub-directory. An entry that
claims nothing, or two names that reduce to one, fail the run.

| Upstream shape | Example | Entries |
|---|---|---|
| a directory per variant | heroicons `24/outline` | `{ "path": "outline", "from": "24/outline" }` |
| a suffix in one directory | ionicons `bell-outline` | `{ "path": "outline", "suffix": "-outline" }`, `{ "path": "filled" }` |
| both | Phosphor `thin/bell-thin` | `{ "path": "thin", "from": "thin", "suffix": "-thin" }` |
| a subset by suffix | Fluent `_24_regular` | `{ "path": "regular", "suffix": "_24_regular" }` |
| sub-folders kept as a prefix | Carbon `32/watson-health/` | `{ "path": "size-32", "from": "32", "flatten_prefix": true }` |

### Leaving icons out: `exclude`

`names` drops file stems wherever they occur (Octicons' GitHub marks). `directories` drops every
file with that directory name in its path (Game Icons' `badges/`). `metadata` reads a JSON array
the upstream ships, relative to its root, and drops each item whose `field` is present and not in
`allow`, naming its file by `name` (Simple Icons' non-CC0 logos, from `data/simple-icons.json`). An
item that must be dropped but has no name fails the run, as does an empty data file.

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
policy, and match icon-browser's browser (formerly react-icon-browser) and core's; the pack SVGs measure at most 1.0 MB and 6,741 elements.

## Download and extraction limits

Sources fetch from pinned upstreams, so these limits only bite if one is compromised. Each fails the
run with an error that names the limit; none is retried, because a retry would read the same bytes.

| Limit | Value | Where |
|---|---|---|
| Version-check and registry JSON | 32 MiB | `httpx.DefaultMaxJSONBytes` |
| One archive download | 1 GiB, or the source's `max_download_bytes` (at most 4 GiB) | `httpx.DefaultMaxDownloadBytes` |
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
| `api.github.com` | the version checks of openmoji and the `github-archive` packs |
| `github.com` | the OpenMoji tag archive |
| `codeload.github.com` | where `github.com` redirects the archive to |
| `unicode.org` | the CLDR `emoji-test.txt` the emoji categoriser reads |

A test loads every shipped pack config, builds its pipelines and fails if any URL they reach is off this
list, so adding a recipe that fetches from a new host means adding the host here in the same change.
Release-asset hosts (`objects.githubusercontent.com`, `release-assets.githubusercontent.com`) are not on
it, because no shipped recipe downloads a release asset.

### GitHub API token

Anonymous requests to `api.github.com` share a limit of 60 an hour, and ten shipped packs check their
version there. When `GH_TOKEN` (or, failing that, `GITHUB_TOKEN`) is set, version checks send it to
`api.github.com`, over https, and to no other host. `sync-pack.yml` already exports `GH_TOKEN`. A check
that hits a spent limit says so and gives the reset time, rather than a bare `403 Forbidden`.

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
is read when the program starts, so `serve` needs a restart to pick up a change.

### npm packages need no npm

A set whose source is an npm package (`@tabler/icons`, `@twemoji/svg`, the aggregator's sets) is
fetched straight from `registry.npmjs.org` through the client above. The tarball is checked against the
`sha512` integrity the registry publishes, and a mismatch, or a version that publishes no integrity, is
refused. Node.js is not needed.

`npm pack` is the fallback when the registry fetch fails. npm is looked for in this order, and a failure
names every place searched:

1. the `npm_path` setting (`ICHAVA_NPM_PATH`, `--set npm_path=…`)
2. `ICHAVA_NPM`
3. `PATH`
4. the usual install directories: `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin`, Volta, fnm,
   asdf, `~/.local/bin` and nvm's newest node; on Windows `Program Files\nodejs`, `%APPDATA%\npm`,
   Volta and `NVM_SYMLINK`.

That order matters for `serve`. A server started from Finder, launchd or a desktop shortcut gets a PATH
with none of Homebrew, nvm, Volta or fnm on it. `doctor` reports what was found.

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
`illustrations/office`. Only `metadata.data` is read: a key at the top level of the file (`items`,
`kinds`, `categories`) is ignored, as it is by ichava/core. Precedence, first hit wins: item, rule,
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

Scans each pack's checkout and writes `.cache/kind-suggestions/<pack>.json` (change the directory with
`--kind-suggestions-dir`; it was `build/kind-suggestions/` until build/ became binaries only), an array of records. **It only suggests.** Nothing writes a kind into
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
in the commit subject and body. A stated-facts step and a version stamp are inserted, in that order, before every `git-branch` sink (see [architecture](architecture.md#what-a-pack-states-about-itself-moves-with-its-assets)).

**A pack with no sinks is refused** by `sync` and `recipe` unless it is a dry run, because it would
fetch, transform and then write nothing.

The `git-branch` fields are the pack's values for the matching `git.*` settings: they beat the
defaults and the pack's `settings` block, and give way to the global config, the environment and
flags.

## Recipes

A recipe turns a pack config into pipelines. Every recipe but `generic` runs only when the config names it in `recipe`; `auto`, the default, picks `generic` for an npm or `github-*` source and nothing otherwise, and never looks at the pack's name:

| Recipe | Picked for | Builds |
|---|---|---|
| `twemoji-sets` | `"recipe": "twemoji-sets"` (`icon-sets-twemoji`) | Twemoji (npm), categorised by the Unicode taxonomy, then one commit. OpenMoji is not part of this pack |
| `openmoji-sets` | `"recipe": "openmoji-sets"` (`icon-sets-openmoji`) | OpenMoji colour and black from one pinned `hfg-gmuend/openmoji` release tag (no `v` prefix), categorised by the Unicode taxonomy, then one commit. With three protections against losing an icon (below) |
| `manifest-pack` | `"recipe": "manifest-pack"` | one pipeline per set in the pack's `<slug>.sets.json` set manifest, each from its own upstream, then one commit |
| `generic` | any `npm` or `github-*` source | the config's own source, transforms and sinks |

#### `openmoji-sets` never drops an icon

In both recipes, two emoji whose names slug alike each keep a file. The first in `emoji-test.txt` order keeps the base slug and a later one takes a name read from its own, with punctuation spelled out by name and anything else as `u` plus its hex code point: `keycap: #` is `keycap.svg` and `keycap: *` is `keycap-asterisk.svg`. `codepoints.json` and `names.json` use the same slugs.

`twemoji-sets` files a Twemoji icon only when its filename matches a `emoji-test.txt` entry exactly, and drops the rest (its log line says `missing=N`). `openmoji-sets` opts out of that:

- **Selector-insensitive match.** OpenMoji names about 200 emoji without the emoji-presentation selector (`2600.svg`, the sun; `emoji-test.txt` has `2600 FE0F`). They match their taxonomy entry anyway.
- **An explicit bucket.** An icon still unmatched, such as an OpenMoji extra (`e380.svg`), an emoji newer than the taxonomy, or a component glyph, is kept as `uncategorised/<upstream-name>.svg`, with a warning and an `uncategorised` count. One emoji shipped twice (with and without its FE0F) keeps the fully-qualified file under the name and puts the other in the bucket.
- **The taxonomy follows the release, never below the default.** `emoji.unicode_version` defaults to the OpenMoji release's major and minor, or to the built-in default (`16.0`) when that is newer, unless something above the built-in default sets it. OpenMoji 15.1.0 therefore uses 16.0, the taxonomy the pack shipped with, and 17.0.0 uses 17.0.
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
| `config_dir` | `config/` beside the binary, else the checkout found walking up from the working directory, then from the binary, else the configs embedded in the binary | `ICHAVA_CONFIG_DIR` | no | where the registry and pack configs live |
| `private_config_dir` | none | `ICHAVA_PRIVATE_CONFIG_DIR` | no | private pack configs merged over `config_dir` at run time; see [Private packs](#private-packs) |
| `log_level` | `info` | `ICHAVA_LOG_LEVEL` | no | `debug`, `info`, `warn`, `error` |
| `concurrency` | `1` | `ICHAVA_CONCURRENCY` | no | packs synced at once by `sync-all` |
| `packages_dirs` (alias `packages_dir`) | none | `ICHAVA_PACKAGES_DIRS` (OS list separator) | no | directories holding the pack checkouts, searched in order; see [Where the packs live](#where-the-packs-live) |
| `workspace` | none | `ICHAVA_WORKSPACE` | no | directory a relative `pack_root` resolves against |
| `history_keep` | `50` | `ICHAVA_HISTORY_KEEP` | no | copies of each config file kept before a save; `0` keeps none. See [Managing the config files](#managing-the-config-files) |
| `page_size` | `25` | `ICHAVA_PAGE_SIZE` | no | rows per page in the web UI's lists (packs, runs, audit results, a run's log), 5 to 500; a list's own page-size menu overrides it for that view |
| `allowed_hosts` | none | `ICHAVA_ALLOWED_HOSTS` | no | extra upstream hosts the client may fetch from, comma separated; see [Upstream hosts and https](#upstream-hosts-and-https) |
| `npm_path` | none | `ICHAVA_NPM_PATH` (and `ICHAVA_NPM`) | no | the npm for the `npm pack` fallback; see [npm packages need no npm](#npm-packages-need-no-npm) |
| `dry_run` | `false` | `ICHAVA_DRY_RUN` | yes | fetch and diff, write nothing into the pack, skip stamp, commit and pull request |
| `force` | `false` | `ICHAVA_FORCE` | yes | refresh even when current |
| `git.author` | `ichava-sync-bot <41898282+github-actions[bot]@users.noreply.github.com>` | `ICHAVA_GIT_AUTHOR` | yes | author and committer, `Name <email>` |
| `git.base` | `main` | `ICHAVA_GIT_BASE` | yes | the pull request's base branch |
| `git.branch` | `chore/sync-upstream-{version}` | `ICHAVA_GIT_BRANCH` | yes | the sync branch |
| `git.commit_subject` | `chore(upstream): bump {pack} to v{new}` | `ICHAVA_GIT_COMMIT_SUBJECT` | yes | commit and pull request title |
| `git.commit_body` | `Automated sync from upstream {pack} {old} -> {new}. ...` | `ICHAVA_GIT_COMMIT_BODY` | yes | commit and pull request body |
| `git.open_pr` | `true` | `ICHAVA_GIT_OPEN_PR` | yes | open a pull request after pushing |
| `emoji.unicode_version` | `16.0` | `ICHAVA_EMOJI_UNICODE_VERSION` | yes | Unicode release whose `emoji-test.txt` drives categorisation; must exist at unicode.org (up to 16.0 under `Public/emoji/<v>/`, from 17.0 under `Public/<v>.0/emoji/`). `openmoji-sets` follows its OpenMoji release instead (17.0.0 uses 17.0; never older than this default, so 15.1.0 uses 16.0) unless this is set |
| `api.enabled` | `false` | `ICHAVA_API_ENABLED` | no | serve the REST API under `/api/v1`; see [Integrations](#integrations) |
| `api.allow_remote` | `false` | `ICHAVA_API_ALLOW_REMOTE` | no | also serve the API on a bind beyond loopback |
| `api.rate_limit` | `60` | `ICHAVA_API_RATE_LIMIT` | no | requests per minute per API token, 1 to 10,000 |
| `api.tls` | `auto` | `ICHAVA_API_TLS` | no | `auto` terminates TLS beyond loopback; `off-behind-proxy` serves plain HTTP there |
| `api.tls_cert` | none | `ICHAVA_API_TLS_CERT` | no | PEM certificate (chain) presented beyond loopback; set with `api.tls_key` |
| `api.tls_key` | none | `ICHAVA_API_TLS_KEY` | no | PEM private key for `api.tls_cert` |
| `events.stream` | `false` | `ICHAVA_EVENTS_STREAM` | no | serve `GET /api/v1/events`, the event stream |
| `webhooks.enabled` | `false` | `ICHAVA_WEBHOOKS_ENABLED` | no | deliver events to `webhooks.endpoints` while `serve` runs |
| `webhooks.endpoints` | none | `ICHAVA_WEBHOOKS_ENDPOINTS` (JSON) | no | the receivers: an array of `{name, url, events, secret_env}` |
| `webhooks.allow_private` | `false` | `ICHAVA_WEBHOOKS_ALLOW_PRIVATE` | no | let webhooks reach private, loopback and link-local addresses |
| `webhooks.allow_insecure_localhost` | `false` | `ICHAVA_WEBHOOKS_ALLOW_INSECURE_LOCALHOST` | no | allow `http://` to localhost only |
| `mcp.enabled` | `false` | `ICHAVA_MCP_ENABLED` | no | let `icon-sets-sync mcp` serve |
| `mcp.allow_writes` | `false` | `ICHAVA_MCP_ALLOW_WRITES` | no | offer the MCP write tools |

The run-wide settings (those marked "no") cannot appear in a pack config's `settings`.

### Flags

`--set key=value` sets any of them, and is repeatable. The common ones have their own flags:
`--config-dir`, `-v` (`log_level=debug`), `-q` (`log_level=error`), `--dry-run`, `--commit`
(`dry_run=false`), `--force`, `--concurrency`. A flag counts only when it is given: an unset
`--dry-run` does not override `ICHAVA_DRY_RUN=1`.

### Managing the config files

The web UI's Config page and the `config` commands manage the same files the same way: the registry,
each pack config, the manifest-pack's set manifest and the global config. `config show` lists them by
name. `global` is the global config.

| Command | Web UI | Does |
|---|---|---|
| `config show [name]` | Config page | lists the files and where the global config was found, or prints one |
| `config edit <name>` | the editor | edits in `$VISUAL`/`$EDITOR` (needs a terminal), then validates and saves |
| `config validate [name…]` | Validate | checks with the loader the sync uses; each problem gives `name:line:col` |
| `config fix <name>` | Fix | formats, renames deprecated keys (`packages_dir` to `packages_dirs`) and adds `$schema`; shows the diff and asks first |
| `config history <name>` | History | the copies kept before each save, newest first |
| `config diff <name> [id]` | Diff | how the file differs from a copy (the newest by default) |
| `config restore <name> <id>` | Restore | writes a copy back; shows the diff and asks first |
| `config migrate` | (Settings says when) | copies a global config from an old location to `~/.config` |

The web editor highlights JSON and YAML as you type (keys, strings, numbers, literals, comments).
A JSON file is checked in the browser on every change: a syntax error is underlined where it is, its
line is marked in the gutter and the problem is listed with a link to it, and Save stays off. Once the
text parses, the server checks it against the schema and the loaders after a short pause, the same
check Validate runs. Without JavaScript the editor is a plain text area and Validate posts the form.

Every save is validated first, refused if the file changed on disk since it was opened, and written
atomically (a temporary file and a rename). A copy of the previous text is kept in
`$XDG_STATE_HOME/icon-sets-sync/history/` (else `~/.local/state/icon-sets-sync/history/`). A restore
keeps a copy too, so it can be undone. `history_keep` sets how many copies of each file are kept (50 by
default, `0` for none).

The global config validates against `config/schema/global.schema.json`, which is generated from the
settings registry. A new one opens on its `$schema` line, so an editor that reads JSON Schema completes
and checks every key, and the web editor lists the known settings with an Insert button. Fix leaves
YAML alone, because re-encoding it would drop its comments, and it reports broken JSON rather than
guessing what was meant. `--yes` answers the questions. Without a terminal and without `--yes` they are
a usage error (exit 2), never a hang.

### Checking the pack checkouts

`icon-sets-sync validate` (and the web UI's Validate page) checks each ichava pack checkout under
`packages_dirs`, or each registered pack's checkout when that is unset:

- `composer.json` exists, parses and has type `ichava-icon-set`
- the manifest `resources/assets/svg/config.json` parses and names the same package
- a pack config in the config directory syncs it (a warning when none does)
- `resources/assets/svg/files/` holds sets, either one directory per set or the SVGs directly for a
  single-set pack, and no set is empty
- a root `LICENSE` and the upstream licence texts under `resources/assets/svg/licenses/`
- no empty category directories (a warning)
- no file under the sets that is not an SVG
- every SVG parses with the toolkit's own parser (`--skip-svg-parse` skips this on the CLI; the web page
  makes it a checkbox, because the largest packs hold over 100,000)

It exits 1 on an error, or on a warning with `--strict`. `--json` prints a report. In GitHub Actions
(`GITHUB_ACTIONS=true`, or `--github`) each problem is also an annotation on its file, and a table goes
to the step summary.

### Where the packs live

`packages_dirs` lists the directories holding the ichava pack checkouts. For each pack the toolkit
looks for `<dir>/<pack>/` in each entry, in order, before `workspace`, `/work` and the checkouts beside
this repository. That is how a shipped config's `/work/icon-sets-openmoji` becomes a path on this
machine.

```json
{ "packages_dirs": ["~/projects/ichava/packages", "/srv/ichava"] }
```

A single string works too, and so does the singular `packages_dir`. `~` is the home directory, and a
relative entry resolves against the config file that names it (or the working directory, for
`--packages-dir` and the environment). `--packages-dir` repeats, and `ICHAVA_PACKAGES_DIRS` uses the OS
list separator (`:`, or `;` on Windows). An entry that is not a directory is skipped with a warning,
never a crash.

The Settings page edits it as a list, with a found or missing mark beside each entry, and saves it into
the global config.

### The global config

Optional. **`~/.config/icon-sets-sync/config.json` on every OS, Windows included** (`.yaml` and
`.yml` work too). `$XDG_CONFIG_HOME` overrides the `~/.config` part, and `--global-config` or
`ICHAVA_GLOBAL_CONFIG` names another file.

This deliberately overrides the organisation's default of `%AppData%` on Windows (and the OS's
`~/Library/Application Support` on macOS): one documented path, the same on every machine, that a
person can find without looking it up.

A config left at one of those old locations is still read. Every command then warns once and names
`config migrate`, which copies the file to `~/.config/icon-sets-sync/` after asking (`--yes`
answers). The old file is never moved or deleted for you. The Settings page says which layer chose the
file and shows the same warning.

It holds settings, the values `config print` lists, and nothing about packs. Pack configs live in the
config directory (`--config-dir`), a different thing: the registry and one file per pack. It nests the
dotted names:

```yaml
concurrency: 2
git:
  base: main
  open_pr: true
emoji:
  unicode_version: "16.0"
```

#### It starts initialised

`serve` (and so a double-click) writes the global config from what this machine has before it
listens, and `icon-sets-sync config init` does the same from a terminal:

```json
{
  "$schema": "https://raw.githubusercontent.com/ichava/icon-sets-sync/main/config/schema/global.schema.json",
  "packages_dirs": ["/Users/me/projects/ichava/packages"],
  "private_config_dir": "/Users/me/projects/ichava/packages/aggregate/.dev/icon-sets-sync"
}
```

- `packages_dirs` is the directory this checkout sits in, when it holds ichava packs.
- `private_config_dir` is set only when exactly one pack checkout carries a private config; with
  several, none is chosen and the note says so.
- Nothing else is written, so every other setting keeps following its default.

**A file you edited is never overwritten by a start.** A missing file, one holding nothing but
`$schema`, and one that is byte for byte what was last generated are (re)written; the generator
records what it wrote in `$XDG_STATE_HOME/icon-sets-sync/global-init.json` to tell the two apart.
To regenerate an edited file, press **Reinitialise** on the global config's page or run
`config init --force`; the previous copy goes to the config history, where **History** restores it.
`--read-only` servers and `CI` runs write nothing.

### A pack's settings

```json
{
  "name": "icon-sets-twemoji",
  "settings": { "emoji": { "unicode_version": "16.0" }, "git": { "base": "main" } }
}
```

### Seeing the result

`icon-sets-sync config print --origins [--pack <slug>] [--format yaml]` prints the merged settings
and the layer each came from; with `--pack`, the loaded pack config and the recipe too.

The web UI's Settings page shows the same list, and each value can be edited in place: a menu for a
setting with fixed values (`log_level`), a switch for a boolean, a number field with its bounds, a
multi-line field for a list or the commit body, and a path field for a directory or the npm
executable. **A save writes the global config** (nested by its dots, so `git.base` lands under
`"git": {"base": ...}`), keeping every other key, through the same validated, atomic, history-kept
write as the config editor; **Reset** removes the key from there, so the next layer down applies. The
environment and flags belong to the process that started the server, so the page cannot change them,
and when one of them sets a key the page says so beside it: a saved value applies only where that
variable or flag is unset.

Each value is checked as it is typed, by the same code that checks the save (`POST
/settings/value/<name>/check`, which writes nothing and works on a read-only server), so the page
refuses exactly what the server would. On top of the loader's own rules, the form refuses a directory
that is not an absolute path to an existing directory, an `npm_path` that is not an executable file,
an `allowed_hosts` entry that is not a bare host name, a `git.base` or `git.branch` git would not
accept as a branch, an `emoji.unicode_version` that is not a release such as `16.0`, a list that
names an entry twice, a multi-line value where one line is expected, and anything over 4,096
characters.

### Lists and pages

The web UI's lists (packs, runs, a manifest-pack audit's results and a run's log) show `page_size`
rows at a time (25 by default, 5 to 500). Each list's own **Per page** menu offers 10, 25, 50, 100
and 200 for that view, kept in the address (`?per_page=50&page=3`) with any filter, so a page can be
bookmarked. A page number past the end shows the last page, and a size not in the menu falls back
to `page_size`. A run's log is paged in the browser over the lines its level filter shows, and
following the log keeps its last page open.

### Integrations

The REST API, its event stream, webhooks and the MCP server are **all off** until a setting turns
each on, and a fresh install behaves exactly as before. They are edited like any other setting, and
the Settings page shows them in their own **Integrations** card. `serve` reads them when it starts,
so turning one on takes effect on the next start. `doctor` says which are on, and warns about each
risky combination: a remote API, `api.tls=off-behind-proxy`, private webhooks, plain-http webhooks
and MCP writes.

| Surface | Turn it on | Reference |
|---|---|---|
| REST API | `api.enabled`, and a token from `icon-sets-sync api token create` | [API](tools/api.md) |
| Event stream | `events.stream` (with the API) | [Events](tools/events.md) |
| Webhooks | `webhooks.enabled`, and an endpoint from `icon-sets-sync webhooks add` | [Webhooks](tools/webhooks.md) |
| MCP | `mcp.enabled`, or `icon-sets-sync mcp --enable` | [MCP](tools/mcp.md) |

Secrets never sit in the global config, which is meant to be shareable: API tokens (as SHA-256
hashes) and webhook secrets live in the state directory, `$XDG_STATE_HOME/icon-sets-sync/`
(`~/.local/state/icon-sets-sync/`), each file mode `0600`:

| File | Holds |
|---|---|
| `api-tokens.json` | each API token's name, scopes, expiry, last use and SHA-256 |
| `api-audit.jsonl` | one line per API write and MCP write tool call (token name, route, status) |
| `webhook-secrets.json` | each endpoint's generated secret, and the previous one during a rotation |
| `webhook-deliveries.jsonl` | one line per delivery attempt, rotated at 5 MiB |
| `webhook-state.json` | each endpoint's consecutive failures, and whether it was disabled |
| `tls-cert.pem`, `tls-key.pem` | the self-signed certificate `serve` generates beyond loopback |

`webhooks.endpoints` is a JSON array in the global config. `webhooks add` writes it, and the
Settings page edits it as JSON:

```json
{
  "webhooks": {
    "enabled": true,
    "endpoints": [
      { "name": "ci", "url": "https://ci.example.com/hooks/icons", "events": ["job.finished", "pack.behind_upstream"] }
    ]
  }
}
```

Each endpoint names the event types it receives, and there is no "all": an endpoint gets only what
it lists. `secret_env`, when set, names an environment variable whose value signs instead of the
generated secret, for a container or a CI runner.

#### TLS beyond loopback

On loopback `serve` speaks plain HTTP, as it always did: nothing leaves the machine. Bound beyond
loopback (`--addr 0.0.0.0 --allow-remote`), it terminates TLS itself, with `api.tls_cert` and
`api.tls_key` when both are set, and otherwise with a self-signed certificate it generates once,
keeps as `tls-cert.pem` in the state directory, and prints the SHA-256 fingerprint and public-key
pin of on every start. Plain HTTP beyond loopback needs `api.tls=off-behind-proxy`, for a TLS proxy
in front, and both `serve` and `doctor` warn about it. The API itself also needs `api.allow_remote`
on such a bind.

## Where the vendored version actually lives

**In the pack repo, not here.** `version_file` and `version_keys` point at the pack's own
`resources/assets/svg/config.json`, which travels with the assets it describes.

`current_version` in this repository is a fallback for a pack that has no vendored record yet. It
used to be the source of truth, and that made the sync non-convergent: the bump was written through
this repository's `config/`, the runner's throwaway checkout, and never committed, so every scheduled
run re-detected the same release and re-opened the same pull request. The version stamp now writes
into the pack repo, before the `git-branch` sink commits.

## Adding a pack

1. Pick the slug: lowercase, words joined by hyphens, `<pack>-<set>` for one set of a shared pack
   ([Naming packs](#naming-packs)). Write `config/<slug>.json` (or `.yaml`) with that slug as its
   `name`, describing the upstream and the sinks.
2. Add the slug to `config/packs.json`.
3. `icon-sets-sync config validate`, then `icon-sets-sync check --pack=<slug>` to confirm the
   upstream poll resolves.

Most packs need only config: an npm or GitHub source, a `source_path`, perhaps a transform list.
If the upstream's shape genuinely differs (several sources, a taxonomy), it needs a recipe; see
[Architecture](architecture.md#recipes).

---

[← Docs index](../README.md#documentation)
