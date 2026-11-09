package settings

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateSchema = flag.Bool("update-schema", false, "rewrite config/schema/global.schema.json")

// The shipped schema is GlobalSchema's output. Run
// `go test ./internal/core/settings -update-schema` after changing a key.
func TestGlobalSchemaFileIsCurrent(t *testing.T) {
	want, err := GlobalSchema()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "..", "..", "..", "config", "schema", "global.schema.json")
	if *updateSchema {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run go test ./internal/core/settings -update-schema", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("config/schema/global.schema.json is stale; run go test ./internal/core/settings -update-schema")
	}
	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	props := doc["properties"].(map[string]any)
	n := 0
	for range Keys {
		n++
	}
	if len(props) < 10 || n < 15 {
		t.Fatalf("schema has %d top-level properties for %d keys: the generator lost keys", len(props), n)
	}
	git := props["git"].(map[string]any)["properties"].(map[string]any)
	if _, ok := git["base"]; !ok {
		t.Error("nested key git.base missing")
	}
}

// A global config may carry $schema; the loader ignores it.
func TestGlobalConfigAcceptsSchemaKey(t *testing.T) {
	if _, err := ParseGlobal("/c.json", []byte(`{"$schema": "`+GlobalSchemaURL+`", "concurrency": 2}`)); err != nil {
		t.Fatal(err)
	}
}
