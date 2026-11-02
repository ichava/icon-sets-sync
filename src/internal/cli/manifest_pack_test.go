package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/app"
)

// manifestPackConfig writes a config dir holding the manifest pack, with one
// vendored set, so the audit gets as far as its own argument checks.
func manifestPackConfig(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	packRoot := filepath.Join(root, "pack")
	if err := os.MkdirAll(filepath.Join(packRoot, "resources", "assets", "svg", "files", "demo-icons"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pack := `{"name": "` + app.ManifestPack + `", "pack": "ichava/` + app.ManifestPack + `", "pack_root": "` + packRoot + `",
  "current_version": "1.0.0", "version_check_url": "https://example.invalid/latest",
  "recipe": "manifest-pack", "source": {"type": "url"}, "sinks": []}`
	if err := os.WriteFile(filepath.Join(dir, app.ManifestPack+".json"), []byte(pack), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// `manifest-pack audit` runs, and its former name, `bundled audit`, was
// removed: it is an unknown command now, not an alias.
func TestManifestPackAuditRunsAndBundledIsGone(t *testing.T) {
	isolate(t)
	dir := manifestPackConfig(t)

	// --package without --set is refused before anything is fetched, which
	// exercises the whole command without the network.
	now, nowCode := run(t, "--config-dir", dir, "manifest-pack", "audit", "--package", "demo")
	if nowCode != ExitUsage || !strings.Contains(now, "--package needs --set") {
		t.Fatalf("manifest-pack audit: exit %d, did not reach its own checks:\n%s", nowCode, now)
	}

	help, code := run(t, "manifest-pack", "audit", "--help")
	if code != ExitOK || !strings.Contains(help, "imt manifest-pack audit --set") {
		t.Errorf("manifest-pack audit --help: exit %d\n%s", code, help)
	}

	old, oldCode := run(t, "--config-dir", dir, "bundled", "audit", "--package", "demo")
	if oldCode == ExitOK || strings.Contains(old, "--package needs --set") || !strings.Contains(old, `unknown command "bundled"`) {
		t.Errorf("bundled audit still runs: exit %d\n%s", oldCode, old)
	}
	top, _ := run(t, "--help")
	if !strings.Contains(top, "manifest-pack") || strings.Contains(top, "\n  bundled ") {
		t.Errorf("top-level help should list manifest-pack and not bundled:\n%s", top)
	}
}
