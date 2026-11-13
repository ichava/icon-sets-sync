package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// fix formats, renames a deprecated key and adds $schema, keeping the order
// of the keys a person wrote.
func TestFixConfigRepairsAGlobalConfig(t *testing.T) {
	in := []byte(`{"concurrency":2,"packages_dir":"~/p","git":{"base":"main"}}`)
	got, err := FixConfig(KindGlobalConfig, "/c/config.json", in)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"$schema\": \"" + settings.GlobalSchemaURL + "\",\n  \"concurrency\": 2,\n  \"packages_dirs\": \"~/p\",\n  \"git\": {\n    \"base\": \"main\"\n  }\n}\n"
	if string(got.Data) != want {
		t.Errorf("fixed:\n%s\nwant:\n%s", got.Data, want)
	}
	if len(got.Changes) != 3 {
		t.Errorf("changes %v, want formatting, rename and $schema", got.Changes)
	}
	again, _ := FixConfig(KindGlobalConfig, "/c/config.json", got.Data)
	if len(again.Changes) != 0 || string(again.Data) != string(got.Data) {
		t.Errorf("fix is not idempotent: %v", again.Changes)
	}
	if !json.Valid(got.Data) {
		t.Error("invalid JSON out")
	}
}

func TestFixConfigLeavesBrokenJSONAndYAMLAlone(t *testing.T) {
	if _, err := FixConfig(KindGlobalConfig, "/c.json", []byte(`{"a":`)); err == nil {
		t.Error("broken JSON was 'fixed'")
	}
	got, err := FixConfig(KindGlobalConfig, "/c.yaml", []byte("concurrency: 2\n"))
	if err != nil || len(got.Changes) != 0 || string(got.Data) != "concurrency: 2\n" {
		t.Errorf("YAML changed: %+v %v", got, err)
	}
}

func TestFixConfigAddsThePackSchema(t *testing.T) {
	got, err := FixConfig(KindPackConfig, "/c/icon-sets-x.json", []byte("{\n  \"name\": \"icon-sets-x\"\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got.Data), "{\n  \"$schema\": \"./schema/pack.schema.json\",") {
		t.Errorf("%s", got.Data)
	}
}

// Errors carry a line and a column, so the editor can point at them.
func TestLocateFindsTheLine(t *testing.T) {
	data := []byte("{\n  \"concurrency\": 2,\n  \"dry_rnu\": true\n}\n")
	line, col := Locate(data, errors.New(`unknown setting "dry_rnu"`))
	if line != 3 || col != 3 {
		t.Errorf("unknown key at %d:%d, want 3:3", line, col)
	}
	bad := []byte("{\n  \"a\": 1,\n  oops\n}")
	var v any
	err := json.Unmarshal(bad, &v)
	if line, _ := Locate(bad, err); line != 3 {
		t.Errorf("syntax error at line %d, want 3", line)
	}
	if line, _ := Locate(data, errors.New("no position here")); line != 0 {
		t.Errorf("invented a line: %d", line)
	}
}
