# Installation

Download a release binary from GitHub, or build `icon-sets-sync` from source.

## Download a release

Each release on the [releases page](https://github.com/ichava/icon-sets-sync/releases) carries:

| Asset | What it is |
|---|---|
| `icon-sets-sync_<os>_<arch>[.exe]` | the bare binary, ready to run (`macos`, not `darwin`; `macos_universal` runs on both Mac architectures) |
| `icon-sets-sync_<os>_<arch>.tar.gz` / `.zip` | the same binary with `LICENSE`, `README.md`, `CHANGELOG.md` and the third-party notices |
| `.deb`, `.rpm`, `.apk` | Linux packages |
| `icon-sets-sync_macos_universal.app.zip` | the macOS app: unzip and open it (unsigned; see below) |
| `icon-sets-sync-launcher_windows_<arch>.exe` | the Windows launcher, which opens the web UI without a console |
| `checksums.txt`, `checksums-launchers.txt` | SHA-256 of every asset above |

```bash
shasum -a 256 -c checksums.txt --ignore-missing   # sha256sum on Linux
```

The GitHub release is the only download channel: there is no `go install` path, Homebrew tap, Scoop
bucket or image. [Release](release.md#verifying-a-release) shows how to check the signature and the
build provenance.

## Build from source

```bash
git clone https://github.com/ichava/icon-sets-sync.git
cd icon-sets-sync
make build-local                  # writes build/<os>/icon-sets-sync; make build does the same
build/macos/icon-sets-sync --config-dir config list-packs   # build/linux/, build/windows/….exe elsewhere
```

Run with no arguments, or double-clicked, it opens the web UI in your browser; with a command it runs
headless, as on CI and servers (see [`serve`](tools/serve.md#flags)).

Double-clicked from inside the checkout, every binary under `build/<os>/` finds
the checkout's `config/` from their own location; a binary copied anywhere else uses the pack configs
built into it, read-only. After editing `config/`, run `make embed-config` so the built-in copy
matches; a test fails until it does.

### Build outputs

`build/` holds one folder per OS and nothing else, and is gitignored; `make clean` removes it.

```text
build/
  checksums.txt   SHA-256 over every cross binary below, as macos/icon-sets-sync_macos_arm64 …
  macos/          icon-sets-sync (the host build)  icon-sets-sync_macos_amd64  _arm64  _universal
                  icon-sets-sync.app (after `make app`)
  linux/          icon-sets-sync_linux_<arch> …  icon-sets-sync.desktop  icon-sets-sync.png
  windows/        icon-sets-sync_windows_<arch>.exe …  icon-sets-sync-launcher_windows_<arch>.exe …
  freebsd/        icon-sets-sync_freebsd_<arch> …
  archives/       only after `make dist`
```

| Command | Writes |
|---|---|
| `make build-local` | `build/<os>/icon-sets-sync` for this machine (`build/macos/` on a Mac, `.exe` on Windows) |
| `make cross` | `build/{os}/{binary}_{os}_{arch}` (`.exe` on Windows) for every release target of `icon-sets-sync`, with macOS as `macos` plus a `macos_universal` binary, and `build/checksums.txt` over them. Nothing else. |
| `make dist` | the same, plus `build/archives/`: the `.tar.gz`/`.zip` archives, the `.deb`/`.rpm`/`.apk` packages, the source tarball and their own `checksums.txt` |
| `make app` | `build/macos/icon-sets-sync.app`, from the universal binary `make cross` wrote (see [Double-click launchers](#double-click-launchers)) |
| `make desktop` | `build/linux/icon-sets-sync.desktop` and `icon-sets-sync.png`; `make cross` runs it too |

`cross` and `dist` need GoReleaser v2 and read the target list from `src/.goreleaser.yaml`; they are
snapshots with publishing, signing and SBOMs skipped, so nothing is released. GoReleaser writes into a
scratch directory, `.cache/goreleaser/`, and only the bare binaries are copied out of it into
`build/{os}/`; the scratch directory is then removed. `make check-build` (run by all three targets)
fails if `build/` holds anything else or an OS folder holds a folder. Measured 2026-11-04,
`make cross` leaves 19 binaries and `checksums.txt`; it left 38 (568 MB) while it also built a short
alias, and 86 items (1.0 GB) before it was trimmed, most of them GoReleaser's per-target folders. `build/icon-sets-sync` is the tracked Composer
launcher (PHP), not a build output: `make clean` keeps it and `make check-build` allows it.

`build/checksums.txt` names each cross binary by its path inside `build/`
(`macos/icon-sets-sync_macos_arm64`), so it verifies as-is (`sha256sum -c` on Linux):

verify: cd build && shasum -a 256 -c checksums.txt

The release's `checksums.txt`, which CI uploads beside the assets, keeps bare names, because install
scripts and self-update look assets up by name.

`build/macos/icon-sets-sync.app` is the one folder an OS folder may hold. It is not listed in
`checksums.txt` itself: the guard requires its `Contents/MacOS/icon-sets-sync` to be byte for byte the
`macos/icon-sets-sync_macos_universal` that `checksums.txt` lists and verifies, with `Info.plist`
present and the binary executable. `make cross` removes the bundle, because it replaces the binary
the bundle carries; run `make app` again after it.

### Double-click launchers

Each OS allows a different amount, measured 2026-11-04:

| OS | Single binary, no console? | What ships |
|---|---|---|
| Windows | **Nearly.** Explorer gives a double-clicked console program a console of its own (`GetConsoleProcessList` returns 1, and the parent is not a shell). The binary detects that, starts `serve` again with `DETACHED_PROCESS`, and exits, but Windows opens that console before any code runs, so a window flashes for under a second. From `cmd` or PowerShell the console is shared, the count is 2 or more, and it is the ordinary CLI. | `build/windows/icon-sets-sync-launcher_windows_<arch>.exe`, which opens no window at all |
| macOS | **No.** Finder hands a bare executable to Terminal, which opens a window and runs it there; the window belongs to Terminal, so nothing the process does (detaching, exiting) removes it. Only an app bundle is started by LaunchServices without Terminal. | `build/macos/icon-sets-sync.app` |
| Linux | **No, but a launcher needs no wrapper.** A file manager runs a bare executable in a terminal or not at all, depending on the desktop; a `.desktop` entry with `Terminal=false` runs it with none. | `build/linux/icon-sets-sync.desktop` |

**Windows launcher.** `make cross` also builds `icon-sets-sync-launcher_windows_<arch>.exe` for
amd64, arm64 and 386, beside `icon-sets-sync_windows_<arch>.exe`. It is a separate, tiny program
(`src/cmd/icon-sets-sync-launcher`) linked for the GUI subsystem (`-H=windowsgui`), so Windows opens
no console for it. It finds the CLI beside itself (the matching `icon-sets-sync_windows_<arch>.exe`,
else `icon-sets-sync.exe`), starts its `serve` with no console, which opens your browser, and exits.
If the CLI is missing it shows an error box naming the paths it tried.

- **Web UI:** double-click the launcher. Keep it in the same folder as the CLI.
- **CLI:** run `icon-sets-sync.exe` (or `icon-sets-sync_windows_<arch>.exe`) in `cmd` or PowerShell.
  The CLI stays a console program, so its output, Ctrl-C and stdin behave normally there.

`TestWindowsLauncherIsAGUIProgramAndTheCLIIsNot` reads both executables' PE headers and fails if the
launcher loses the GUI subsystem or the CLI gains it. Like the macOS app and the Linux entry, the
launcher is built by `make cross` and is not a GoReleaser build or a release asset.

**macOS app.** `make cross && make app` writes:

```text
build/macos/icon-sets-sync.app/Contents/
  Info.plist                          LSUIElement: no Dock icon and no menu bar
  MacOS/icon-sets-sync                the universal binary itself, unchanged
  Resources/icon-sets-sync.icns
```

Double-clicked (or `open build/macos/icon-sets-sync.app`), the binary sees it runs from
`Contents/MacOS/`, starts `serve` detached, which opens your browser, and exits at once. There is no
Dock icon; stop the server with **Stop server** in the UI. Because the app has already quit, a second
double-click starts it again, finds the running server through `serve.json` and opens the browser
there instead of starting another. The server's output goes to `serve.log` beside `serve.json`
(mode `0600`; it holds the sign-in address).

The binary in the bundle is still the full CLI. Put it on `PATH` with a symlink:

```bash
ln -s "$PWD/build/macos/icon-sets-sync.app/Contents/MacOS/icon-sets-sync" /usr/local/bin/icon-sets-sync
icon-sets-sync check
```

Run through the symlink from a terminal, it behaves as any terminal run: no arguments opens the UI in
the foreground.

**Unsigned.** The app is neither signed nor notarised, which needs the owner's Apple Developer ID.
Gatekeeper warns on first launch of a downloaded copy: right-click → **Open**, then **Open** (or allow
it in System Settings → Privacy & Security). With a Developer ID the commands would be:

```bash
codesign --force --options runtime --timestamp \
  --sign "Developer ID Application: Simtabi LLC (TEAMID)" build/macos/icon-sets-sync.app
ditto -c -k --keepParent build/macos/icon-sets-sync.app icon-sets-sync.zip
xcrun notarytool submit icon-sets-sync.zip --keychain-profile <profile> --wait
xcrun stapler staple build/macos/icon-sets-sync.app
```

Signing changes the bundle's binary, so the guard's byte-for-byte check against the universal binary
would then fail by design; sign a copy, outside `build/`.

**Linux entry.** Install the entry and its icon for your user, with `icon-sets-sync` on `PATH`:

```bash
install -Dm644 build/linux/icon-sets-sync.desktop ~/.local/share/applications/icon-sets-sync.desktop
install -Dm644 build/linux/icon-sets-sync.png ~/.local/share/icons/hicolor/256x256/apps/icon-sets-sync.png
```

It runs `env ICHAVA_DESKTOP_LAUNCH=1 icon-sets-sync`. `ICHAVA_DESKTOP_LAUNCH=1` says a person at a
desktop started it, so it opens the browser though it has no terminal; edit `Exec=` to an absolute
path if the binary is not on `PATH`.

Put `build/<os>/` on your `PATH` (`make shell` does it for one shell) to call it from anywhere. To pin a version, check out its
tag (`git checkout vX.Y.Z`) before building. To run it without keeping a binary:

```bash
go -C src run ./cmd/icon-sets-sync version
```

The module lives in `src/`, so every `go` command names it with `-C src`; a bare `go build ./...` at
the repository root finds no packages.

> The `imt` alias binary was retired on 2026-11-04: use `icon-sets-sync`, which takes the same commands.

`go install` is not an install path: the Go module proxy holds copies of the early `src/v0.1.x` tags
taken from commits before the single-commit publication, so it would serve older code. See
[Release](release.md#where-this-package-is).

Release binaries come from `release.yml`, which runs on a pushed version tag or on request; see
[Release](release.md#binaries-are-published-on-the-github-release).

## What a sync needs

| Tool | Why |
|---|---|
| `git` | branch, commit and push in the pack repositories |
| `gh` | open the pull request |
| `npm` | `npm pack <package>@<version>`, the npm source strategy |

A `GH_TOKEN` with permission to push a branch and open a pull request in the pack repositories, for
any sync that commits. `check`, `list-packs` and `config validate` need only the binary.

## The `/work` directory

Every shipped config names its pack as `/work/<pack>` (`pack_root`), so `/work` must be the
directory that **contains** the pack checkouts, not a pack itself. That is the layout
`sync-pack.yml` makes on the runner, with the same symlink. On Linux:

```bash
sudo ln -sfn ~/src/ichava /work   # the directory holding icon-sets-tabler, icon-sets-flag, ...
```

On macOS the root volume is read-only, so the link is declared in `/etc/synthetic.conf` and created
at the next boot. The two columns are separated by a tab, and the target has no leading slash:

```text
work	Users/you/src/ichava
```

Without `/work`, keep your own pack configs with a relative `pack_root` and point the `workspace`
setting (`ICHAVA_WORKSPACE`) at the directory holding the packs; see
[Configuration](configuration.md#settings).

## Requirements

Go 1.26 or later to build. `git`, `gh` and `npm` on `PATH` to sync.

---

[← Docs index](../README.md#documentation)
