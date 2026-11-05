package manifestpack

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadManifestRefusesAPackNameThatLeavesTheConfigDir is go/path-injection:
// a pack name from an edited document reaches LoadManifest.
func TestLoadManifestRefusesAPackNameThatLeavesTheConfigDir(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	valid, err := os.ReadFile(filepath.Join("..", "config", "testdata", "private-config", "icon-sets-example.sets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "escape.sets.json"), valid, 0o644); err != nil {
		t.Fatal(err)
	}
	if m, err := LoadManifest(configDir, "../escape"); err == nil {
		t.Fatalf("LoadManifest read a manifest outside the config directory: %d sets", len(m.Sets))
	}
}

// TestReadIconStaysUnderItsRoot: a fingerprint key is a relative path, and one
// that climbs out of the set directory must not be read.
func TestReadIconStaysUnderItsRoot(t *testing.T) {
	root := t.TempDir()
	set := filepath.Join(root, "set")
	if err := os.MkdirAll(set, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := readIcon(set, "../secret"); err == nil {
		t.Fatalf("readIcon read %q from outside the set", b)
	}
}
