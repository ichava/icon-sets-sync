# `serve`

`icon-sets-sync serve` (alias `ui`) is a web UI for the same work the CLI does, served from the binary to a browser on this machine.

## What it shows

| Page | Address | What it is for |
|---|---|---|
| Packs | `/` | every registered pack: vendored and upstream versions, status, SVG count, the pack repo's branch and uncommitted changes |
| Pack | `/packs/<slug>` | one pack's config and `_note` annotations, its pipeline, its settings with origins, its recent runs, and the Sync and Recipe forms |
| Run | `/jobs/<id>` | a run's per-pack status, its stages and its log, live while it runs, with Cancel. Each pack line carries the diff the run makes (icons added, removed and changed, counted before the wipe, so a dry run shows it too), and says so when an upstream licence change blocked the sync |
| Runs | `/jobs` | the run history, filtered by pack or outcome, each downloadable as JSON Lines |
| Config | `/config` | the registry, the pack configs, the bundled set manifest and the global config, each in a validating editor |
| Manifest pack audit | `/manifest-pack/audit` | `manifest-pack audit` in the browser: which upstream each set of the aggregator pack came from, with the result downloadable as a set manifest. |
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
| `--no-open` | off | do not open a browser; `ICHAVA_NO_BROWSER=1` does the same |
| `--token-file` | none | also write the sign-in address to this file, mode `0600` |

The global flags (`--config-dir`, `--set`, `--global-config`, `-v`) apply as for every command.

**Browser.** `serve` opens the default browser at the printed, tokenised address (`open` on macOS,
`xdg-open` on Linux and the BSDs, `rundll32 url.dll,FileProtocolHandler` on Windows), and so does
running the binary with no arguments at all, which is what a double-click in Finder or Explorer does.
It never opens one:

- when `CI` is set;
- when `ICHAVA_NO_BROWSER=1`, or `--no-open` is passed;
- when stdin or stdout is not a terminal (a pipe, a service, a scheduled job);
- on Linux and the BSDs when neither `DISPLAY` nor `WAYLAND_DISPLAY` is set (an SSH session, a server).

In those cases no arguments prints the help and exits 0, and `serve` just prints the address. If the
browser cannot be started, the address is still printed and the server carries on. Every other
command is headless and never opens a browser.

**Port.** With no `--port`, `serve` tries 7878 and, when another process holds it, the next port up,
up to 20 ports, and prints the address it actually bound. `--port 7878` (or `--addr 127.0.0.1:7878`)
means that port: if it is taken, `serve` fails instead of moving. `--port 0` lets the system pick.
Running the binary with no arguments behaves like plain `serve`, so a busy 7878 moves too.

**One server at a time.** A running server records its address in `serve.json` in the state
directory (`$XDG_STATE_HOME/icon-sets-sync`, else `~/.local/state/icon-sets-sync`;
`%LocalAppData%\icon-sets-sync` on Windows), mode `0600` because the address carries the token. A
second no-argument launch checks that server over the loopback (`GET /healthz` with the token in a
request header) and, if it answers, opens the browser at it, prints one line and exits 0, starting no
second server. A record whose server no longer answers is removed.

**Stopping.** Every page has **Stop server**, a same-origin form post with the page's CSRF token,
kept under `--read-only` because it changes nothing on disk. It, Ctrl-C and closing the Terminal
window all shut the server down gracefully and remove `serve.json`.

**The Terminal window.** A command-line binary opened from Finder or Explorer always gets a terminal
window; that is the operating system, not this tool. The window prints the address, the config
directory it found and "running in your browser; close this window or press Ctrl-C to stop". A
hand-off to a running server prints one line and the window can be closed.

**Which configs.** The startup line `config: <dir> (<source>)` and the Settings page name where the
pack configs came from: `flag` (`--config-dir`), `env` (`ICHAVA_CONFIG_DIR`), `cwd` (found walking up
from the working directory), `executable` (`config/` beside the binary, or found walking up from the
binary's own directory, which is how a double-clicked `build/macos/icon-sets-sync` or
`build/macos/icon-sets-sync_macos_universal` finds its checkout) or `embedded` (the configs built into
the binary, used outside any checkout). Embedded configs are read-only in the Config page; pass
`--config-dir` to edit a checkout's. Without a checkout of a pack, its page says a sync needs a
workspace (`ICHAVA_WORKSPACE`).

## Signing in

`serve` prints an address with a one-time token:

```text
icon-sets-sync web UI: http://127.0.0.1:7878/?token=3q2...
```

The UI runs only while `serve` is running. Open the printed address as it is, token included:
opening `http://127.0.0.1:7878` without the token is refused by design and shows a 401 sign-in
page, not the dashboard.

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
- **Fetches stay on a short list of hosts, over https.** Runs fetch only from the built-in upstream hosts
  and any you add with `allowed_hosts`, and refuse plain `http`; see
  [Upstream hosts and https](../configuration.md#upstream-hosts-and-https). A blocked fetch fails the run
  with the host named. The setting is read when `serve` starts.
- **Credentials stay where they are.** Runs use the `git` and `gh` credentials this machine already
  has (`GH_TOKEN`); the UI never asks for, stores or shows a token.

## Where things are kept

Finished runs are kept as JSON Lines in `$XDG_STATE_HOME/icon-sets-sync/runs/` (else
`~/.local/state/icon-sets-sync/runs/`; `%LocalAppData%` on Windows), the newest 50. A global config
saved from the editor goes to `$XDG_CONFIG_HOME/icon-sets-sync/config.json` (else `~/.config`) and
applies to the next run without a restart.

## From a checkout

```bash
make ui        # builds build/<os>/icon-sets-sync, then serve --port 7878 against config/
make serve     # go run, on the first free port from 7878
```

`make serve` is `go -C src run ./cmd/icon-sets-sync serve`. It runs from `src/`, and the registry
lives in the repository's `config/`, so the config directory is resolved in this order: `--config-dir`,
`ICHAVA_CONFIG_DIR`, `config/` beside the binary, `./config` if it holds `packs.json`, then the
`config/` of the repository around the working directory (up to 5 parents up, stopping at `.git`,
next to `src/go.mod`; the directory chosen is logged at info level). If none exists, the Packs page
says "missing pack registry" and names `ICHAVA_CONFIG_DIR` and `--config-dir` as the fix.

The pack checkouts are found the same way: the shipped configs name `/work/<pack>` (the CI runner's
layout), so locally the `workspace` setting (`ICHAVA_WORKSPACE`) wins when it holds the pack, then
`/work/<pack>`, then the sibling checkout beside this repository. A pack page shows the root and where
it came from; `config print --pack <slug>` prints it as `pack_root_origin`.

`make ui` pins port 7878 and fails if it is taken; `make serve` moves to the next free port. Both
listen on `127.0.0.1` only. Open the address it logs.

## Updating Tabler

The Tabler version is pinned in `src/internal/web/assets/vendor/TABLER_VERSION`:

```bash
make tabler-update   # move to the newest @tabler/core and @tabler/icons-sprite
make tabler-vendor   # re-vendor exactly what is pinned
make tabler-check    # only say whether a newer release is out
```

Both check every download against the npm registry's integrity hash and rewrite the vendored
files, the licence texts, `MANIFEST.sha256` and `THIRD_PARTY_NOTICES.md` together; the test suite
fails if any of them disagree. The Tests workflow's weekly run warns when a newer release is out;
it never opens a pull request. To use another Tabler icon, add its name to
`src/internal/web/assets/vendor/icons.txt` and run `make tabler-vendor`.

---

[← Docs index](../../README.md#documentation)
