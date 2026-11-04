package cli

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const bundledExe = "/Applications/icon-sets-sync.app/Contents/MacOS/icon-sets-sync"

func TestInAppBundle(t *testing.T) {
	for path, want := range map[string]bool{
		bundledExe: true,
		"/Users/me/build/macos/icon-sets-sync.app/Contents/MacOS/icon-sets-sync": true,
		"/usr/local/bin/icon-sets-sync":                                          false,
		"/Users/me/build/macos/icon-sets-sync":                                   false,
		"/x/icon-sets-sync.app/Contents/Resources/icon-sets-sync":                false,
		"/x/Contents/MacOS/icon-sets-sync":                                       false,
		"/x/.app/Contents/MacOS/icon-sets-sync":                                  false,
		"":                                                                       false,
	} {
		if got := inAppBundle(path); got != want {
			t.Errorf("inAppBundle(%q) = %v, want %v", path, got, want)
		}
	}
}

// Explorer gives a double-clicked console program a console of its own; a
// shell shares its console with what it runs.
func TestExplorerLaunch(t *testing.T) {
	cases := []struct {
		name   string
		procs  int
		parent string
		want   bool
	}{
		{"double-click in Explorer", 1, "explorer.exe", true},
		{"Explorer, case as Windows reports it", 1, "Explorer.EXE", true},
		{"parent unknown, console its own", 1, "", true},
		{"cmd.exe", 2, "cmd.exe", false},
		{"PowerShell", 2, "powershell.exe", false},
		{"pwsh in Windows Terminal", 2, "pwsh.exe", false},
		{"a shell that left the console list", 1, "cmd.exe", false},
		{"Git Bash", 1, "bash.exe", false},
		{"no console at all (a detached child)", 0, "icon-sets-sync.exe", false},
		{"the call failed", 0, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := explorerLaunch(c.procs, c.parent); got != c.want {
				t.Fatalf("explorerLaunch(%d, %q) = %v, want %v", c.procs, c.parent, got, c.want)
			}
		})
	}
}

func TestDesktopLaunchOf(t *testing.T) {
	cases := []struct {
		name            string
		env             launchEnv
		desktop, detach bool
	}{
		{"a terminal", fakeEnv("darwin", true, nil), false, false},
		{"the .app from Finder", bundleEnv(nil), true, true},
		{"a bundle path is not a bundle on Linux", launchEnv{goos: "linux", exe: bundledExe, getenv: noEnv}, false, false},
		{"Explorer double-click", explorerEnv(nil), true, true},
		{"the console flag means nothing off Windows", launchEnv{goos: "linux", consoleAlone: true, getenv: noEnv}, false, false},
		{"the .desktop entry", fakeEnv("linux", false, map[string]string{desktopLaunchEnv: "1", "DISPLAY": ":0"}), true, false},
		{"the relaunched child", fakeEnv("windows", false, map[string]string{desktopLaunchEnv: "1"}), true, false},
		{"ICHAVA_DESKTOP_LAUNCH=0", fakeEnv("linux", false, map[string]string{desktopLaunchEnv: "0"}), false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			desktop, detach := desktopLaunchOf(c.env)
			if desktop != c.desktop || detach != c.detach {
				t.Fatalf("desktopLaunchOf = %v, %v; want %v, %v", desktop, detach, c.desktop, c.detach)
			}
		})
	}
}

// A desktop launch has no terminal, and still opens the browser; CI and the
// opt-out still win.
func TestBrowserAllowedFromADesktop(t *testing.T) {
	cases := []struct {
		name   string
		env    launchEnv
		want   bool
		reason string
	}{
		{"the .app from Finder", bundleEnv(nil), true, ""},
		{"the .desktop entry", fakeEnv("linux", false, map[string]string{desktopLaunchEnv: "1", "DISPLAY": ":0"}), true, ""},
		{"the .desktop entry over ssh", fakeEnv("linux", false, map[string]string{desktopLaunchEnv: "1"}), false, noBrowserDisplay},
		{"CI beats the bundle", bundleEnv(map[string]string{"CI": "true"}), false, noBrowserCI},
		{"the opt-out beats the bundle", bundleEnv(map[string]string{"ICHAVA_NO_BROWSER": "1"}), false, noBrowserOptOut},
		{"a bare binary from Finder's Terminal is still a terminal", fakeEnv("darwin", true, nil), true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := browserAllowed(c.env)
			if got != c.want || reason != c.reason {
				t.Fatalf("browserAllowed = %v, %q; want %v, %q", got, reason, c.want, c.reason)
			}
		})
	}
}

type relaunch struct {
	exe  string
	args []string
	env  []string
	log  string
}

// withRelaunch records detached starts instead of making them.
func withRelaunch(t *testing.T, err error) *[]relaunch {
	t.Helper()
	var got []relaunch
	prev := startDetached
	startDetached = func(exe string, args, env []string, log string) error {
		got = append(got, relaunch{exe, args, env, log})
		return err
	}
	t.Cleanup(func() { startDetached = prev })
	return &got
}

// The .app and an Explorer double-click start `serve` detached and exit at
// once, so no window stays behind (Windows) and LaunchServices sees the app
// quit, so the next double-click runs it again and hands off (macOS).
func TestADesktopLaunchRelaunchesDetached(t *testing.T) {
	for name, env := range map[string]launchEnv{
		"the .app from Finder":  bundleEnv(nil),
		"Explorer double-click": explorerEnv(nil),
	} {
		t.Run(name, func(t *testing.T) {
			opened, _, tried := launchWithState(t, true)
			prev := currentLaunchEnv
			currentLaunchEnv = func() launchEnv { return env }
			t.Cleanup(func() { currentLaunchEnv = prev })
			starts := withRelaunch(t, nil)

			out, code := runRoot(t)
			if code != ExitOK {
				t.Fatalf("exit %d:\n%s", code, out)
			}
			if len(*starts) != 1 {
				t.Fatalf("started %d detached processes, want 1:\n%s", len(*starts), out)
			}
			s := (*starts)[0]
			if !slices.Equal(s.args, []string{"serve"}) {
				t.Errorf("relaunched with %v, want [serve]", s.args)
			}
			if !slices.Contains(s.env, desktopLaunchEnv+"=1") {
				t.Errorf("the child is not told it is a desktop launch: %v", s.env)
			}
			if s.log == "" || filepath.Dir(s.log) != filepath.Dir(stateFilePath()) {
				t.Errorf("the child logs to %q, want a file beside %s", s.log, stateFilePath())
			}
			if len(*opened) != 0 || len(*tried) != 0 || strings.Contains(out, "web UI:") {
				t.Errorf("the launcher served or opened a browser itself: opened %v, listened %v\n%s", *opened, *tried, out)
			}
		})
	}
}

// A second double-click finds the running server and opens it; nothing new
// starts.
func TestADesktopLaunchHandsOffToARunningServer(t *testing.T) {
	opened, probed, _ := launchWithState(t, true)
	prev := currentLaunchEnv
	currentLaunchEnv = func() launchEnv { return bundleEnv(nil) }
	t.Cleanup(func() { currentLaunchEnv = prev })
	starts := withRelaunch(t, nil)
	writeState(t, runningURL)

	if out, code := runRoot(t); code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if len(*probed) != 1 || len(*opened) != 1 || (*opened)[0] != runningURL {
		t.Fatalf("probed %v, opened %v; want the running server opened", *probed, *opened)
	}
	if len(*starts) != 0 {
		t.Fatalf("started a second server: %v", *starts)
	}
}

// If the detached start fails, serve here rather than show nothing.
func TestAFailedRelaunchServesInProcess(t *testing.T) {
	opened, _, _ := launchWithState(t, true)
	prev := currentLaunchEnv
	currentLaunchEnv = func() launchEnv { return bundleEnv(nil) }
	t.Cleanup(func() { currentLaunchEnv = prev })
	withRelaunch(t, errors.New("fork refused"))

	out, code := runRoot(t)
	if code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if url := printedURL.FindString(out); url == "" || len(*opened) != 1 || (*opened)[0] != url {
		t.Fatalf("did not serve and open in-process: opened %v\n%s", *opened, out)
	}
}

// The binary inside the bundle is still the CLI, and the relaunched child
// and the .desktop entry serve in-process.
func TestDesktopLaunchesThatDoNotRelaunch(t *testing.T) {
	cfg := filepath.Join("..", "..", "..", "config")
	cases := []struct {
		name  string
		env   launchEnv
		args  []string
		opens bool
	}{
		{"the bundled binary with a command", bundleEnv(nil), []string{"--config-dir", cfg, "list-packs"}, false},
		{"the bundled binary with a flag only", bundleEnv(nil), []string{"--config-dir", cfg}, false},
		{"the relaunched child", bundleEnv(map[string]string{desktopLaunchEnv: "1"}), []string{"--config-dir", cfg, "serve"}, true},
		{"the .desktop entry", fakeEnv("linux", false, map[string]string{desktopLaunchEnv: "1", "DISPLAY": ":0"}), nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opened, _, _ := launchWithState(t, false)
			prev := currentLaunchEnv
			currentLaunchEnv = func() launchEnv { return c.env }
			t.Cleanup(func() { currentLaunchEnv = prev })
			starts := withRelaunch(t, nil)

			out, code := runRoot(t, c.args...)
			if code != ExitOK {
				t.Fatalf("exit %d:\n%s", code, out)
			}
			if len(*starts) != 0 {
				t.Fatalf("relaunched: %v", *starts)
			}
			if got := len(*opened) == 1; got != c.opens {
				t.Fatalf("opened %v, want a browser: %v\n%s", *opened, c.opens, out)
			}
		})
	}
}

func noEnv(string) string { return "" }

// bundleEnv is the .app started by Finder: no terminal at all.
func bundleEnv(env map[string]string) launchEnv {
	e := fakeEnv("darwin", false, env)
	e.exe = bundledExe
	return e
}

// explorerEnv is a double-click in Explorer: a console, but its own.
func explorerEnv(env map[string]string) launchEnv {
	e := fakeEnv("windows", true, env)
	e.exe = `C:\Users\me\Downloads\icon-sets-sync_windows_amd64.exe`
	e.consoleAlone = true
	return e
}
