package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// fix, history, diff and restore on the command line, the editor's cycle.
func TestConfigFixHistoryDiffRestore(t *testing.T) {
	isolate(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := fixture(t, "", "")
	global := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), settings.AppDir, "config.json")
	if err := os.MkdirAll(filepath.Dir(global), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte(`{"packages_dir":"/p"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	base := []string{"--config-dir", dir}

	out, code := run(t, append(base, "config", "show")...)
	if code != ExitOK || !strings.Contains(out, "global") || !strings.Contains(out, "chosen by:") {
		t.Fatalf("show (%d):\n%s", code, out)
	}
	if out, code := run(t, append(base, "--no-input", "config", "fix", "global")...); code != ExitUsage || !strings.Contains(out, "+  \"packages_dirs\"") {
		t.Fatalf("fix without --yes (%d):\n%s", code, out)
	}
	if out, code := run(t, append(base, "--yes", "config", "fix", "global")...); code != ExitOK {
		t.Fatalf("fix (%d):\n%s", code, out)
	}
	fixed, _ := os.ReadFile(global)
	if !strings.Contains(string(fixed), `"packages_dirs": "/p"`) || !strings.Contains(string(fixed), settings.GlobalSchemaURL) {
		t.Fatalf("fixed file:\n%s", fixed)
	}
	out, _ = run(t, append(base, "config", "history", "global")...)
	id := regexp.MustCompile(`\d{8}T[0-9.]+Z`).FindString(out)
	if id == "" {
		t.Fatalf("history:\n%s", out)
	}
	if out, _ := run(t, append(base, "config", "diff", "global")...); !strings.Contains(out, `+{"packages_dir":"/p"}`) {
		t.Errorf("diff:\n%s", out)
	}
	if out, code := run(t, append(base, "--yes", "config", "restore", "global", id)...); code != ExitOK {
		t.Fatalf("restore (%d):\n%s", code, out)
	}
	if got, _ := os.ReadFile(global); string(got) != `{"packages_dir":"/p"}` {
		t.Errorf("restored:\n%s", got)
	}

	if err := os.WriteFile(global, []byte("{\n  \"dry_rnu\": true\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The pre-run loads the global config and refuses the bad key first;
	// --global-config pointing elsewhere lets validate read the named file.
	other := filepath.Join(t.TempDir(), "other.json")
	_ = os.WriteFile(other, []byte("{}"), 0o644)
	out, code = run(t, append(base, "--global-config", other, "config", "validate", "icon-sets-demo.json")...)
	if code != ExitOK {
		t.Errorf("validate by name (%d):\n%s", code, out)
	}
}
