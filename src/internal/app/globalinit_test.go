package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func mkPack(t *testing.T, root, name string, private bool) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"name":"ichava/`+name+`","type":"ichava-icon-set"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if private {
		p := filepath.Join(dir, ".dev", "icon-sets-sync")
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "packs.json"), []byte(`{"packs":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The generated config names what this machine has: the directory holding the
// pack checkouts, and the private config directory when there is exactly one.
func TestInitialGlobalNamesWhatTheMachineHas(t *testing.T) {
	root := t.TempDir()
	mkPack(t, root, "icon-sets-tabler", false)
	mkPack(t, root, "icon-sets-bundled", true)
	g := initialGlobalFrom([]string{root, filepath.Join(root, "missing")}, nil)
	var doc map[string]any
	if err := json.Unmarshal(g.Data, &doc); err != nil {
		t.Fatalf("%s: %v", g.Data, err)
	}
	if doc["$schema"] == nil {
		t.Error("no $schema")
	}
	dirs, _ := doc["packages_dirs"].([]any)
	if len(dirs) != 1 || dirs[0] != root {
		t.Errorf("packages_dirs = %v, want [%s]", dirs, root)
	}
	want := filepath.Join(root, "icon-sets-bundled", ".dev", "icon-sets-sync")
	if doc["private_config_dir"] != want {
		t.Errorf("private_config_dir = %v, want %s", doc["private_config_dir"], want)
	}
	if len(g.Notes) == 0 {
		t.Error("no notes saying what was found")
	}
}

// Two private config directories: neither is chosen, and the note says so.
func TestInitialGlobalLeavesAnAmbiguousPrivateDirUnset(t *testing.T) {
	root := t.TempDir()
	mkPack(t, root, "a", true)
	mkPack(t, root, "b", true)
	g := initialGlobalFrom([]string{root}, nil)
	var doc map[string]any
	_ = json.Unmarshal(g.Data, &doc)
	if _, ok := doc["private_config_dir"]; ok {
		t.Errorf("chose one of two private dirs: %s", g.Data)
	}
}

func TestClassifyGlobal(t *testing.T) {
	gen := []byte("{\n  \"$schema\": \"x\",\n  \"packages_dirs\": [\"/p\"]\n}\n")
	rec := InitRecord{SHA256: sha(gen)}
	for _, c := range []struct {
		name   string
		data   []byte
		exists bool
		rec    InitRecord
		want   GlobalState
	}{
		{"missing", nil, false, InitRecord{}, GlobalMissing},
		{"schema only", []byte(`{"$schema": "x"}`), true, InitRecord{}, GlobalEmpty},
		{"blank", []byte("  \n"), true, InitRecord{}, GlobalEmpty},
		{"generated", gen, true, rec, GlobalGenerated},
		{"edited after generating", append([]byte(nil), []byte("{\"$schema\":\"x\",\"concurrency\":2}")...), true, rec, GlobalEdited},
		{"hand-written, never generated", []byte(`{"concurrency":2}`), true, InitRecord{}, GlobalEdited},
		{"not JSON", []byte("concurrency: 2\n"), true, InitRecord{}, GlobalEdited},
	} {
		if got := ClassifyGlobal(c.data, c.exists, c.rec); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestInitRecordRoundTrips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state", "global-init.json")
	if r := ReadInitRecord(p); r.SHA256 != "" {
		t.Fatalf("a missing record reads as %+v", r)
	}
	if err := WriteInitRecord(p, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	if r := ReadInitRecord(p); r.SHA256 != sha([]byte("abc")) || r.Time.IsZero() {
		t.Fatalf("%+v", r)
	}
}
