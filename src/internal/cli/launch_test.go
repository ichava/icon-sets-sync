package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeEnv states a machine for the browser decision.
func fakeEnv(goos string, tty bool, env map[string]string) launchEnv {
	return launchEnv{
		goos:      goos,
		getenv:    func(k string) string { return env[k] },
		stdinTTY:  tty,
		stdoutTTY: tty,
	}
}

// withLaunch installs env and a browser fake that records every URL it is
// asked to open, and a listener that binds nothing.
func withLaunch(t *testing.T, env launchEnv) *[]string {
	t.Helper()
	fakeListen(t)
	var opened []string
	serveBrowser = func(url string) error { opened = append(opened, url); return nil }
	prev := currentLaunchEnv
	currentLaunchEnv = func() launchEnv { return env }
	t.Cleanup(func() { currentLaunchEnv = prev })
	return &opened
}

// runRoot runs the tree with exactly args (no args at all when empty) and,
// if it starts serving, stops it the way Ctrl-C would.
func runRoot(t *testing.T, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := NewRootCommand()
	var out strings.Builder
	w := &notifyWriter{b: &out, seen: make(chan struct{}, 1)}
	root.SetOut(w)
	root.SetErr(w)
	// Non-nil, or cobra falls back to os.Args.
	root.SetArgs(append([]string{}, args...))
	done := make(chan int, 1)
	go func() {
		code := ExitOK
		if err := root.ExecuteContext(ctx); err != nil {
			var exit *ExitError
			if asExitError(err, &exit) {
				code = exit.Code
				if exit.Err != nil {
					w.writeString(exit.Err.Error())
				}
			} else {
				code = ExitFailure
			}
		}
		done <- code
	}()
	select {
	case <-w.seen:
		cancel()
	case code := <-done:
		return w.String(), code
	case <-time.After(10 * time.Second):
		t.Fatal("neither served nor exited")
	}
	select {
	case code := <-done:
		return w.String(), code
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop when its context was cancelled")
	}
	return "", 0
}

func TestBrowserAllowed(t *testing.T) {
	cases := []struct {
		name   string
		env    launchEnv
		want   bool
		reason string
	}{
		{"macOS terminal", fakeEnv("darwin", true, nil), true, ""},
		{"Windows console", fakeEnv("windows", true, nil), true, ""},
		{"Linux desktop on X11", fakeEnv("linux", true, map[string]string{"DISPLAY": ":0"}), true, ""},
		{"Linux desktop on Wayland", fakeEnv("linux", true, map[string]string{"WAYLAND_DISPLAY": "wayland-0"}), true, ""},
		{"ICHAVA_NO_BROWSER=0 is not an opt-out", fakeEnv("darwin", true, map[string]string{"ICHAVA_NO_BROWSER": "0"}), true, ""},
		{"Linux over ssh, no display", fakeEnv("linux", true, nil), false, noBrowserDisplay},
		{"FreeBSD, no display", fakeEnv("freebsd", true, nil), false, noBrowserDisplay},
		{"CI", fakeEnv("darwin", true, map[string]string{"CI": "true"}), false, noBrowserCI},
		{"CI beats a display", fakeEnv("linux", true, map[string]string{"CI": "1", "DISPLAY": ":0"}), false, noBrowserCI},
		{"opted out", fakeEnv("darwin", true, map[string]string{"ICHAVA_NO_BROWSER": "1"}), false, noBrowserOptOut},
		{"not a terminal", fakeEnv("darwin", false, nil), false, noBrowserTerminal},
		{"stdin piped", launchEnv{goos: "darwin", getenv: func(string) string { return "" }, stdinTTY: false, stdoutTTY: true}, false, noBrowserTerminal},
		{"stdout piped", launchEnv{goos: "darwin", getenv: func(string) string { return "" }, stdinTTY: true, stdoutTTY: false}, false, noBrowserTerminal},
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

// No arguments at all is a double-click: open the UI where a person can see
// it, print help everywhere else, and exit 0 either way.
func TestNoArgsOpensTheUIOrPrintsHelp(t *testing.T) {
	cases := []struct {
		name   string
		env    launchEnv
		opens  bool
		reason string
	}{
		{"terminal", fakeEnv("darwin", true, nil), true, ""},
		{"not a terminal", fakeEnv("darwin", false, nil), false, noBrowserTerminal},
		{"CI", fakeEnv("linux", true, map[string]string{"CI": "true", "DISPLAY": ":0"}), false, noBrowserCI},
		{"Linux headless", fakeEnv("linux", true, nil), false, noBrowserDisplay},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolate(t)
			t.Setenv("ICHAVA_CONFIG_DIR", filepath.Join("..", "..", "..", "config"))
			opened := withLaunch(t, c.env)
			out, code := runRoot(t)
			if code != ExitOK {
				t.Fatalf("exit %d:\n%s", code, out)
			}
			if !c.opens {
				if len(*opened) != 0 {
					t.Errorf("opened a browser: %v", *opened)
				}
				if !strings.Contains(out, c.reason) || !strings.Contains(out, "Usage:") {
					t.Errorf("want the reason and the help:\n%s", out)
				}
				if strings.Contains(out, "web UI:") {
					t.Errorf("served the UI where it should print help:\n%s", out)
				}
				return
			}
			url := printedURL.FindString(out)
			if url == "" || !strings.Contains(out, "press Ctrl-C to stop") {
				t.Fatalf("did not print the tokenised URL and how to stop:\n%s", out)
			}
			if len(*opened) != 1 || (*opened)[0] != url {
				t.Fatalf("opened %v, want exactly the printed %s", *opened, url)
			}
		})
	}
}

// A subcommand is headless, whatever machine it runs on.
func TestSubcommandsNeverOpenABrowser(t *testing.T) {
	cfg := filepath.Join("..", "..", "..", "config")
	for _, args := range [][]string{
		{"version"},
		{"--config-dir", cfg, "list-packs"},
		{"--config-dir", cfg, "config", "print"},
		{"--config-dir", cfg, "config", "validate"},
		{"menu"},
		{"--config-dir", cfg}, // a flag and no command: the menu path, not the UI
		{"serve", "--no-open"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			isolate(t)
			opened := withLaunch(t, fakeEnv("darwin", true, nil))
			if out, code := runRoot(t, args...); code != ExitOK {
				t.Fatalf("exit %d:\n%s", code, out)
			}
			if len(*opened) != 0 {
				t.Fatalf("%v opened a browser: %v", args, *opened)
			}
		})
	}
}

func TestServeOpensTheBrowserUnlessToldNot(t *testing.T) {
	cfg := filepath.Join("..", "..", "..", "config")
	cases := []struct {
		name  string
		env   map[string]string
		args  []string
		opens bool
	}{
		{"plain serve opens", nil, nil, true},
		{"--no-open", nil, []string{"--no-open"}, false},
		{"ICHAVA_NO_BROWSER=1", map[string]string{"ICHAVA_NO_BROWSER": "1"}, nil, false},
		{"CI", map[string]string{"CI": "true"}, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolate(t)
			opened := withLaunch(t, fakeEnv("darwin", true, c.env))
			out, code := runRoot(t, append([]string{"--config-dir", cfg, "serve"}, c.args...)...)
			if code != ExitOK {
				t.Fatalf("exit %d:\n%s", code, out)
			}
			url := printedURL.FindString(out)
			if c.opens && (len(*opened) != 1 || (*opened)[0] != url) {
				t.Fatalf("opened %v, want exactly the printed %s", *opened, url)
			}
			if !c.opens && len(*opened) != 0 {
				t.Fatalf("opened %v", *opened)
			}
		})
	}
}
