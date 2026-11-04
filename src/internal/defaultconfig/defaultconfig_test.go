package defaultconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var repoConfig = filepath.Join("..", "..", "..", "config")

// TestEmbeddedConfigMatchesTheRepository is the drift guard: the copy built
// into the binary must be config/, file for file and byte for byte. Fix with
// `make embed-config`.
func TestEmbeddedConfigMatchesTheRepository(t *testing.T) {
	// A floor, so a glob that stopped matching cannot pass on an empty tree.
	if n := len(Files()); n < 6 {
		t.Fatalf("only %d embedded files; config/ ships packs.json, four packs and the schema", n)
	}
	diff, err := Diff(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) > 0 {
		t.Fatalf("the embedded configs differ from config/; run make embed-config:\n%s", strings.Join(diff, "\n"))
	}
}

// The guard can fail: a config/ with one byte changed is reported.
func TestDiffReportsAChange(t *testing.T) {
	src := t.TempDir()
	if _, err := Sync(repoConfig, src); err != nil {
		t.Fatal(err)
	}
	if d, _ := Diff(src); len(d) != 0 {
		t.Fatalf("a fresh copy differs: %v", d)
	}
	if err := os.WriteFile(filepath.Join(src, "packs.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "icon-sets-new.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, _ := Diff(src)
	if strings.Join(d, "|") != "differs: packs.json|not embedded: icon-sets-new.json" {
		t.Fatalf("diff = %v", d)
	}
}

func TestDirWritesAReadOnlyCopy(t *testing.T) {
	base := t.TempDir()
	dir, err := Dir(base)
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := Diff(dir); len(d) != 0 {
		t.Fatalf("the extracted copy differs: %v", d)
	}
	fi, err := os.Stat(filepath.Join(dir, "packs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o222 != 0 {
		t.Errorf("packs.json is writable: %v", fi.Mode())
	}
	// A tampered copy is put back.
	p := filepath.Join(dir, "packs.json")
	_ = os.Chmod(p, 0o600)
	if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if again, err := Dir(base); err != nil || again != dir {
		t.Fatalf("Dir again = %s, %v", again, err)
	}
	if d, _ := Diff(dir); len(d) != 0 {
		t.Fatalf("a tampered copy was not repaired: %v", d)
	}
}
