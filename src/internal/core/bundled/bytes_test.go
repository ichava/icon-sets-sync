package bundled

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseManifestBytesAgreesWithLoadManifest(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "config")
	fromFile, err := LoadManifest(dir, "icon-sets-bundled")
	if err != nil {
		t.Fatal(err)
	}
	path := ManifestPath(dir, "icon-sets-bundled")
	data, _ := os.ReadFile(path)
	fromBytes, err := ParseManifestBytes(path, data)
	if err != nil || !reflect.DeepEqual(fromFile, fromBytes) {
		t.Fatalf("the two loaders disagree: %v", err)
	}
	if len(fromBytes.Sets) < 10 {
		t.Fatalf("only %d sets", len(fromBytes.Sets))
	}
	if _, err := ParseManifestBytes("m.json", []byte(`{"pack": "x", "sets": {}}`)); err == nil {
		t.Error("a manifest with no sets parsed")
	}
}
