// Command icon-sets-sync-launcher starts the icon-sets-sync web UI from a
// Windows double-click with no console window on screen.
//
// icon-sets-sync.exe is a console program, so Windows gives it a console
// window before any Go code runs; it notices an Explorer double-click and
// hands off to a detached server, but the window still flashes. This program
// is built for the GUI subsystem (-H=windowsgui), so Windows opens no console
// for it. It finds icon-sets-sync.exe beside itself, starts its `serve` the
// way that hand-off does (ICHAVA_DESKTOP_LAUNCH=1, no console), and exits.
// It holds no logic of its own: everything else is the CLI binary's.
//
// It is built by `make cross` (src/internal/tools/launchers -windows) for the
// Windows targets, as icon-sets-sync-launcher_windows_<arch>.exe beside
// icon-sets-sync_windows_<arch>.exe, like the macOS .app and the Linux
// .desktop entry. On any other OS it only says so.
package main

import (
	"path/filepath"
	"strings"
)

const (
	// cliName is the program this one starts.
	cliName = "icon-sets-sync"
	// launcherName is this program's own base name.
	launcherName = cliName + "-launcher"
)

// candidates lists, in order, the paths where the CLI may sit beside this
// launcher: first the name matching the launcher's own
// (icon-sets-sync-launcher_windows_amd64.exe looks for
// icon-sets-sync_windows_amd64.exe, as the build and a release lay them
// out), then plain icon-sets-sync.exe, the name an installed copy has.
func candidates(self string) []string {
	dir, base := filepath.Dir(self), filepath.Base(self)
	var out []string
	if rest, ok := strings.CutPrefix(strings.ToLower(base), launcherName); ok && rest != ".exe" {
		out = append(out, filepath.Join(dir, cliName+base[len(launcherName):]))
	}
	return append(out, filepath.Join(dir, cliName+".exe"))
}
