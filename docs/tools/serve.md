# `serve`

`icon-sets-sync serve` (alias `ui`) is a web UI for the same work the CLI does, served from the binary to a browser on this machine.

## What it shows

| Page | Address | What it is for |
|---|---|---|
| Packs | `/` | every registered pack: vendored and upstream versions, status, SVG count, the pack repo's branch and uncommitted changes |
| Pack | `/packs/<slug>` | one pack's config and `_note` annotations, its pipeline, its settings with origins, its recent runs, and the Sync and Recipe forms |
| Run | `/jobs/<id>` | a run's per-pack status, its stages and its log, live while it runs, with Cancel |
| Runs | `/jobs` | the run history, filtered by pack or outcome, each downloadable as JSON Lines |
| Config | `/config` | the registry, the pack configs, the bundled set manifest and the global config, each in a validating editor |
| Settings | `/settings` | the effective settings and the layer each came from, the same as `config print --origins` |
| Licences | `/about/licenses` | the notices of the embedded Tabler, Bootstrap and Popper |

The upstream check behind the Packs page is cached for ten minutes. **Refresh check** polls again.
Every page works without JavaScript; with it, a run's progress streams in and the editor validates
as you type.

## Flags

| Flag | Default | Effect |
|---|---|---|
| `--addr` | `127.0.0.1` | address to listen on, `host` or `host:port`; a port here pins it like `--port` |
| `--port` | `7878` | port to listen on; see below |
| `--allow-remote` | off | allow a non-loopback `--addr`; plain HTTP, so prefer an SSH tunnel |
| `--allow-host` | none | an extra `Host` header to accept behind a proxy (repeatable) |
| `--read-only` | off | serve no route that runs a sync or saves a file |
| `--no-open` | off | do not open a browser (it only opens one when attached to a terminal anyway) |
| `--token-file` | none | also write the sign-in address to this file, mode `0600` |

The global flags (`--config-dir`, `--set`, `--global-config`, `-v`) apply as for every command.

**Port.** With no `--port`, `serve` tries 7878 and, when another process holds it, the next port up,
up to 20 ports, and prints the address it actually bound. `--port 7878` (or `--addr 127.0.0.1:7878`)
means that port: if it is taken, `serve` fails instead of moving. `--port 0` lets the system pick.

## Signing in

`serve` prints an address with a one-time token:

```text
icon-sets-sync web UI: http://127.0.0.1:7878/?token=3q2...
```

Opening it exchanges the token for a session cookie (`HttpOnly`, `SameSite=Strict`) and removes the
token from the address bar and history. Without the cookie every page answers 401. The token changes
every time `serve` starts.

## Safety

- **Dry run by default.** A run from the UI is a dry run unless you choose **Real sync**; a real run
  then asks you to type the pack name (or `all packs`) on a confirmation page, and confirm again in
  the browser. The choice enters the settings as the flag layer, so a pack config or
  `ICHAVA_DRY_RUN` cannot turn a dry run into a commit.
- **One run per pack.** A second run of a pack that is already running is refused with a link to
  the first: two would race on the same repository's branches.
- **Only this server's pages can change anything.** Requests with an unexpected `Host` are refused
  (DNS rebinding), and every change needs a same-origin `Origin` and the session's form token.
- **Nothing loads from the network.** The page's styles, scripts and icons are embedded, and the
  Content-Security-Policy forbids inline script and style.
- **Credentials stay where they are.** Runs use the `git` and `gh` credentials this machine already
  has (`GH_TOKEN`); the UI never asks for, stores or shows a token.

## Where things are kept

Finished runs are kept as JSON Lines in `$XDG_STATE_HOME/icon-sets-sync/runs/` (else
`~/.local/state/icon-sets-sync/runs/`; `%LocalAppData%` on Windows), the newest 50. A global config
saved from the editor goes to `$XDG_CONFIG_HOME/icon-sets-sync/config.json` (else `~/.config`) and
applies to the next run without a restart.

## Updating Tabler

The Tabler version is pinned in `src/internal/web/assets/vendor/TABLER_VERSION`:

```bash
make tabler-update   # move to the newest @tabler/core and @tabler/icons-sprite
make tabler-vendor   # re-vendor exactly what is pinned
```

Both check every download against the npm registry's integrity hash and rewrite the vendored
files, the licence texts, `MANIFEST.sha256` and `THIRD_PARTY_NOTICES.md` together; the test suite
fails if any of them disagree. To use another Tabler icon, add its name to
`src/internal/web/assets/vendor/icons.txt` and run `make tabler-vendor`.

---

[← Docs index](../../README.md#documentation)
