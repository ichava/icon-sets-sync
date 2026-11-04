package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// launchWithState is withLaunch with the state directory in a temp dir and a
// probe fake that answers alive and records what it was asked.
func launchWithState(t *testing.T, alive bool) (opened, probed *[]string, tried *[]string) {
	t.Helper()
	isolate(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("ICHAVA_CONFIG_DIR", filepath.Join("..", "..", "..", "config"))
	opened = withLaunch(t, fakeEnv("darwin", true, nil))
	tried = fakeListen(t)
	serveBrowser = func(url string) error { *opened = append(*opened, url); return nil }
	var p []string
	prev := probeInstance
	probeInstance = func(url string) bool { p = append(p, url); return alive }
	t.Cleanup(func() { probeInstance = prev })
	return opened, &p, tried
}

func writeState(t *testing.T, url string) string {
	t.Helper()
	path := stateFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(instanceState{URL: url, PID: 1})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const runningURL = "http://127.0.0.1:7900/?token=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// The running server records where it is, readable by its owner only, and
// removes the record when it stops.
func TestServerRecordsItselfPrivatelyAndCleansUp(t *testing.T) {
	opened, _, _ := launchWithState(t, false)
	path := stateFilePath()
	var during instanceState
	var mode os.FileMode
	serveBrowser = func(url string) error {
		*opened = append(*opened, url)
		fi, err := os.Stat(path)
		if err != nil {
			t.Errorf("no state file while serving: %v", err)
			return nil
		}
		mode = fi.Mode().Perm()
		b, _ := os.ReadFile(path)
		_ = json.Unmarshal(b, &during)
		return nil
	}
	out, code := runRoot(t)
	if code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if want := filepath.Join(os.Getenv("XDG_STATE_HOME"), "icon-sets-sync", "serve.json"); path != want {
		t.Errorf("state file at %s, want %s", path, want)
	}
	if mode != 0o600 {
		t.Errorf("state file mode %v, want 0600", mode)
	}
	if during.URL == "" || during.URL != printedURL.FindString(out) {
		t.Errorf("state names %q, the server printed %q", during.URL, printedURL.FindString(out))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("state file left behind after the server stopped: %v", err)
	}
	if !strings.Contains(out, "running in your browser; close this window or press Ctrl-C to stop") {
		t.Errorf("the window does not say how to stop:\n%s", out)
	}
}

// A second double-click finds the first and opens it, starting nothing.
func TestSecondLaunchHandsOffToTheRunningOne(t *testing.T) {
	opened, probed, tried := launchWithState(t, true)
	path := writeState(t, runningURL)
	out, code := runRoot(t)
	if code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if len(*tried) != 0 {
		t.Errorf("started a second server: listened on %v", *tried)
	}
	if len(*probed) != 1 || (*probed)[0] != runningURL {
		t.Errorf("probed %v", *probed)
	}
	if len(*opened) != 1 || (*opened)[0] != runningURL {
		t.Errorf("opened %v, want the running instance", *opened)
	}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 1 || !strings.Contains(out, "already running") {
		t.Errorf("want one line saying it is already running:\n%s", out)
	}
	if strings.Contains(out, "token=") {
		t.Errorf("the hand-off printed the token:\n%s", out)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the hand-off removed the live instance's state: %v", err)
	}
}

// A record left by a server that died is removed, and a new server starts.
func TestStaleStateIsRemoved(t *testing.T) {
	opened, probed, tried := launchWithState(t, false)
	writeState(t, runningURL)
	out, code := runRoot(t)
	if code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if len(*probed) != 1 || len(*tried) == 0 {
		t.Fatalf("probed %v, listened %v", *probed, *tried)
	}
	if len(*opened) != 1 || (*opened)[0] == runningURL {
		t.Errorf("opened %v, want the new server", *opened)
	}
	if _, err := os.Stat(stateFilePath()); !os.IsNotExist(err) {
		t.Errorf("state file still there: %v", err)
	}
}
