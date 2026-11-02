package manifestpack

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseManifestBytesAgreesWithLoadManifest(t *testing.T) {
	// A private pack's manifest does not ship here; this reads the synthetic
	// one the config package's private-dir tests use.
	dir := filepath.Join("..", "config", "testdata", "private-config")
	fromFile, err := LoadManifest(dir, "icon-sets-example")
	if err != nil {
		t.Fatal(err)
	}
	path := ManifestPath(dir, "icon-sets-example")
	data, _ := os.ReadFile(path)
	fromBytes, err := ParseManifestBytes(path, data)
	if err != nil || !reflect.DeepEqual(fromFile, fromBytes) {
		t.Fatalf("the two loaders disagree: %v", err)
	}
	if len(fromBytes.Sets) != 2 {
		t.Fatalf("only %d sets", len(fromBytes.Sets))
	}
	if _, err := ParseManifestBytes("m.json", []byte(`{"pack": "x", "sets": {}}`)); err == nil {
		t.Error("a manifest with no sets parsed")
	}
}
