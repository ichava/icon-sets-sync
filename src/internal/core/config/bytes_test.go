package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ParsePackBytes and LoadPackFile must agree on every shipped config: the
// editor validates with one and the sync reads with the other.
func TestParsePackBytesAgreesWithLoadPackFileOnTheShippedConfigs(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "config")
	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Packs) < 4 {
		t.Fatalf("only %d packs registered", len(registry.Packs))
	}
	for _, slug := range registry.Packs {
		path, err := PackFile(slug, dir)
		if err != nil {
			t.Fatal(err)
		}
		fromFile, err := LoadPackFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		fromBytes, err := ParsePackBytes(path, data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(fromFile, fromBytes) {
			t.Errorf("%s: the two loaders disagree", slug)
		}
	}
}

func TestParsePackBytesReportsEveryViolationWithItsPath(t *testing.T) {
	_, err := ParsePackBytes("config/demo.json", []byte(`{"name": "demo", "nope": 1}`))
	if err == nil {
		t.Fatal("an invalid document parsed")
	}
	if !strings.HasPrefix(err.Error(), "config/demo.json: ") {
		t.Errorf("error does not name the path: %v", err)
	}
}

func TestParsePackBytesReadsYAMLByExtension(t *testing.T) {
	doc := []byte(`name: icon-sets-demo
pack: ichava/icon-sets-demo
pack_root: /work/icon-sets-demo
current_version: "1.0.0"
version_check_url: https://registry.npmjs.org/demo/latest
source: {type: npm, package: demo}
sinks: []
`)
	p, err := ParsePackBytes("demo.yaml", doc)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "icon-sets-demo" || p.File != "demo.yaml" {
		t.Fatalf("%+v", p)
	}
}

// An invalid save never reaches the disk, and a valid one keeps the bytes
// exactly -- annotations included -- rather than a re-marshalled struct.
func TestWritePackFileAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "icon-sets-demo.json")
	good := `{
  "_note": "kept verbatim",
  "name": "icon-sets-demo",
  "pack": "ichava/icon-sets-demo",
  "pack_root": "/work/icon-sets-demo",
  "current_version": "1.0.0",
  "version_check_url": "https://registry.npmjs.org/demo/latest",
  "source": {"type": "npm", "package": "demo"},
  "sinks": []
}
`
	if err := os.WriteFile(path, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WritePackFileAtomic(path, []byte(`{"name": 1}`)); err == nil {
		t.Fatal("an invalid document was written")
	}
	if got, _ := os.ReadFile(path); string(got) != good {
		t.Fatalf("the refused save changed the file:\n%s", got)
	}

	edited := strings.Replace(good, `"1.0.0"`, `"1.1.0"`, 1)
	if err := WritePackFileAtomic(path, []byte(edited)); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != edited {
		t.Fatalf("saved bytes differ from the submitted bytes:\n%s", got)
	}
}
