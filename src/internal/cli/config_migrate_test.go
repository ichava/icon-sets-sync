package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// A global config at the OS config directory is read and warned about, and
// `config migrate --yes` copies it to ~/.config without moving the original.
func TestConfigMigrateCopiesALegacyGlobalConfig(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("on Linux the OS config directory is ~/.config itself, so there is no legacy location")
	}
	isolate(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	legacyDirs := settings.LegacyConfigDirs()
	if len(legacyDirs) == 0 {
		t.Skip("no OS config dir")
	}
	legacy := filepath.Join(legacyDirs[0], settings.AppDir, "config.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"log_level":"warn"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := fixture(t, "", "")

	out, code := run(t, "--config-dir", dir, "config", "print")
	if code != ExitOK || !strings.Contains(out, "old location") {
		t.Fatalf("no warning about the old location (exit %d):\n%s", code, out)
	}
	if out, code := run(t, "--config-dir", dir, "--no-input", "config", "migrate"); code != ExitUsage {
		t.Fatalf("migrate without --yes and no terminal exited %d, want %d:\n%s", code, ExitUsage, out)
	}
	if out, code := run(t, "--config-dir", dir, "--yes", "config", "migrate"); code != ExitOK {
		t.Fatalf("migrate exited %d:\n%s", code, out)
	}
	home, _ := os.UserHomeDir()
	moved, err := os.ReadFile(filepath.Join(home, ".config", settings.AppDir, "config.json"))
	if err != nil || string(moved) != `{"log_level":"warn"}` {
		t.Fatalf("copy: %q %v", moved, err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("the old file is gone: %v", err)
	}
	if out, _ := run(t, "--config-dir", dir, "config", "print"); strings.Contains(out, "old location") {
		t.Errorf("still warns after the migration:\n%s", out)
	}
}
