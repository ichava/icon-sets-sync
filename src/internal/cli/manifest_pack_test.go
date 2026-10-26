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

// `bundled audit` is the deprecated name of `manifest-pack audit`: the same
// command, the same exit code and output, plus one notice naming the
// replacement and the release that removes it.
func TestBundledAuditIsADeprecatedAliasOfManifestPackAudit(t *testing.T) {
	isolate(t)
	dir := manifestPackConfig(t)

	// --package without --set is refused before anything is fetched, which
	// exercises the whole command without the network.
	now, nowCode := run(t, "--config-dir", dir, "manifest-pack", "audit", "--package", "demo")
	old, oldCode := run(t, "--config-dir", dir, "bundled", "audit", "--package", "demo")
	if nowCode != ExitUsage || oldCode != nowCode {
		t.Fatalf("exit codes: manifest-pack %d, bundled %d, want both %d\n%s\n%s", nowCode, oldCode, ExitUsage, now, old)
	}
	if !strings.Contains(now, "--package needs --set") {
		t.Fatalf("manifest-pack audit did not reach its own checks:\n%s", now)
	}
	notice := "`bundled audit` is deprecated and will be removed in 0.2.0; use `manifest-pack audit`"
	if strings.Contains(now, "deprecated") {
		t.Errorf("manifest-pack audit printed a deprecation notice:\n%s", now)
	}
	if !strings.Contains(old, notice) {
		t.Errorf("bundled audit printed no deprecation notice:\n%s", old)
	}
	if rest := strings.Replace(old, notice, "", 1); !strings.Contains(rest, "--package needs --set") {
		t.Errorf("bundled audit's own output differs:\n%s", old)
	}

	help, code := run(t, "manifest-pack", "audit", "--help")
	if code != ExitOK || !strings.Contains(help, "imt manifest-pack audit --set") {
		t.Errorf("manifest-pack audit --help: exit %d\n%s", code, help)
	}
	if help, code := run(t, "bundled", "audit", "--help"); code != ExitOK || !strings.Contains(help, "Identify which upstream") {
		t.Errorf("bundled audit --help: exit %d\n%s", code, help)
	}

	// The old name is kept working, not advertised.
	top, _ := run(t, "--help")
	if !strings.Contains(top, "manifest-pack") || strings.Contains(top, "\n  bundled ") {
		t.Errorf("top-level help should list manifest-pack and hide bundled:\n%s", top)
	}
}
