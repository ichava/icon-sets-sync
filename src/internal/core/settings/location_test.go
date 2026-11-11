package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func envOf(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The owner's rule (2026-11-09): ~/.config/icon-sets-sync/config.json on every
// OS, Windows included, and $XDG_CONFIG_HOME overrides it.
func TestTheDefaultGlobalConfigIsDotConfigOnEveryOS(t *testing.T) {
	home := t.TempDir()
	loc, err := FindGlobalConfig(envOf(map[string]string{"HOME": home}), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", AppDir, "config.json")
	if loc.Default != want || loc.Path != "" || loc.Legacy {
		t.Fatalf("loc = %+v, want default %s and nothing found", loc, want)
	}
	xdg := t.TempDir()
	loc, _ = FindGlobalConfig(envOf(map[string]string{"HOME": home, "XDG_CONFIG_HOME": xdg}), nil)
	if loc.Default != filepath.Join(xdg, AppDir, "config.json") {
		t.Errorf("XDG_CONFIG_HOME not honoured: %+v", loc)
	}
}

// A config at an old location (the OS config dir: %AppData% on Windows,
// ~/Library/Application Support on macOS) is read, flagged as legacy, and
// never moved.
func TestALegacyGlobalConfigIsReadAndFlagged(t *testing.T) {
	home, old := t.TempDir(), t.TempDir()
	legacy := filepath.Join(old, AppDir, "config.json")
	write(t, legacy)
	loc, err := FindGlobalConfig(envOf(map[string]string{"HOME": home}), []string{old})
	if err != nil {
		t.Fatal(err)
	}
	if loc.Path != legacy || !loc.Legacy || loc.Layer != "legacy location" {
		t.Fatalf("loc = %+v", loc)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("the legacy file moved: %v", err)
	}
	// Once the default exists, it wins.
	write(t, loc.Default)
	loc, _ = FindGlobalConfig(envOf(map[string]string{"HOME": home}), []string{old})
	if loc.Path != loc.Default || loc.Legacy {
		t.Errorf("the default does not win: %+v", loc)
	}
}

func TestAnExplicitGlobalConfigWins(t *testing.T) {
	loc, _ := FindGlobalConfig(envOf(map[string]string{"HOME": t.TempDir(), GlobalConfigEnv: "/x/y.json"}), nil)
	if loc.Path != "/x/y.json" || loc.Layer != GlobalConfigEnv {
		t.Errorf("loc = %+v", loc)
	}
}
