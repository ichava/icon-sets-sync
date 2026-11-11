package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// desktopLaunchEnv tells the binary a person at a desktop started it, so it
// opens the browser though it has no terminal. The Linux .desktop entry sets
// it, and so does the detached relaunch below, for its child.
const desktopLaunchEnv = "ICHAVA_DESKTOP_LAUNCH"

// desktopLaunchOf reports whether a desktop, not a terminal, started this
// run (desktop), and whether the run should hand itself to a detached `serve`
// and exit (detach):
//
//   - macOS, the binary inside icon-sets-sync.app, as Finder starts it: both.
//     Finder runs a bare executable inside Terminal and nothing the process
//     does can stop that, which is why there is a bundle at all. Detaching
//     makes the app quit at once, so LaunchServices starts it again on the
//     next double-click instead of only bringing the running copy forward,
//     and that second run hands off to the server.
//   - Windows, a console that is this process's alone (an Explorer
//     double-click): both. The console window closes when this process
//     exits, under a second after it appeared.
//   - ICHAVA_DESKTOP_LAUNCH=1 (the .desktop entry, and the detached child):
//     desktop only; it is already where it should run.
func desktopLaunchOf(e launchEnv) (desktop, detach bool) {
	switch {
	case e.goos == "darwin" && inAppBundle(e.exe):
		return true, true
	case e.goos == "windows" && e.consoleAlone:
		return true, true
	case e.getenv != nil && truthy(e.getenv(desktopLaunchEnv)):
		return true, false
	}
	return false, false
}

// inAppBundle reports whether exe is a bundle's main executable:
// <name>.app/Contents/MacOS/<binary>. The path is the one the process was
// started by, unresolved, so a symlink on PATH into the bundle is a terminal
// run, which is what it is.
func inAppBundle(exe string) bool {
	if exe == "" {
		return false
	}
	macos := filepath.Dir(exe)
	contents := filepath.Dir(macos)
	app := filepath.Base(filepath.Dir(contents))
	return filepath.Base(macos) == "MacOS" && filepath.Base(contents) == "Contents" &&
		strings.HasSuffix(app, ".app") && len(app) > len(".app")
}

// shells are the programs that run a console program in their own console.
// A console of one whose parent is one of these is a shell that has gone,
// not Explorer.
var shells = map[string]bool{
	"cmd.exe": true, "powershell.exe": true, "pwsh.exe": true, "bash.exe": true,
	"sh.exe": true, "zsh.exe": true, "fish.exe": true, "nu.exe": true, "wsl.exe": true,
	"mintty.exe": true, "conemu64.exe": true, "conemuc64.exe": true, "far.exe": true,
}

// explorerLaunch decides, from GetConsoleProcessList's count and the parent's
// image name, whether Explorer started this console program. Explorer gives
// a double-clicked console program a console of its own, so the count is 1;
// a shell shares its console, so it is 2 or more. 0 is no console at all.
func explorerLaunch(consoleProcs int, parent string) bool {
	return consoleProcs == 1 && !shells[strings.ToLower(parent)]
}

// relaunchServe starts `serve` detached from this process, told it is a
// desktop launch, with its output in serve.log beside serve.json.
func relaunchServe() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	log := ""
	if path := stateFilePath(); path != "" {
		log = filepath.Join(filepath.Dir(path), "serve.log")
	}
	return startDetached(exe, []string{"serve"}, append(os.Environ(), desktopLaunchEnv+"=1"), log)
}

// startDetached starts exe in its own session (Unix) or with no console
// (Windows) and does not wait. A seam: tests record instead.
var startDetached = func(exe string, args, env []string, log string) error {
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	cmd.SysProcAttr = detachedAttr()
	if log != "" {
		if err := os.MkdirAll(filepath.Dir(log), 0o700); err == nil {
			// The log holds the sign-in address, so it is the owner's only.
			if f, err := os.OpenFile(log, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600); err == nil {
				defer func() { _ = f.Close() }()
				cmd.Stdout, cmd.Stderr = f, f
			}
		}
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
