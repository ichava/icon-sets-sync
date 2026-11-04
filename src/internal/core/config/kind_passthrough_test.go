package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/kinds"
)

// TestSyncPreservesEveryKindKey is the golden test for kind passthrough
// (contract section 10): a sync that stamps a new upstream version into a
// pack's config.json must leave package.kind, categories.*.kind, items and
// kinds.rules exactly as they were, bytes and order included.
//
// The writer edits the one version leaf in place rather than round-tripping the
// document, so this holds by construction today. The test is what keeps it
// true if someone swaps in a parse-and-reserialise.
func TestSyncPreservesEveryKindKey(t *testing.T) {
	before, err := os.ReadFile(filepath.Join("testdata", "kind-keys.before.json"))
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "kind-keys.golden.json"))
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	dir := filepath.Join(root, "resources", "assets", "svg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "config.json")
	if err := os.WriteFile(target, before, 0o644); err != nil {
		t.Fatal(err)
	}
	pack := &PackConfig{PackRoot: root, VersionFile: DefaultVersionFile, VersionKeys: DefaultVersionKeys()}

	if _, err := WriteVendoredVersion(pack, "3.47.0"); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(golden) {
		t.Fatalf("config.json after the sync differs from the golden file\n--- got\n%s\n--- want\n%s", after, golden)
	}

	// The same answer from the resolver, for every row the file can set.
	paths := []string{
		"glyphs/hero-banner", "glyphs/home", "spot/tiny-cat", "spot/big/park",
		"illustrations/office", "legacy/one", "legacy/other", "root",
	}
	b, a := parseKinds(t, before), parseKinds(t, after)
	if len(b.Issues) != 0 || len(a.Issues) != 0 {
		t.Fatalf("kind issues before %+v after %+v", b.Issues, a.Issues)
	}
	for _, p := range paths {
		if b.Resolve(p) != a.Resolve(p) {
			t.Errorf("%s resolved %+v before and %+v after", p, b.Resolve(p), a.Resolve(p))
		}
	}
	// And the resolution is not vacuous: the file does set kinds.
	if got := a.Resolve("glyphs/hero-banner"); got.Kind != kinds.Illustration || got.Source != kinds.SourceItem {
		t.Errorf("fixture sets no item kind: %+v", got)
	}
}

func parseKinds(t *testing.T, data []byte) *kinds.Config {
	t.Helper()
	cfg, err := kinds.ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
