package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The embedded schema is a copy; the repository copy is what editors and
// humans read. They must not drift.
func TestEmbeddedSchemaMatchesTheRepoCopy(t *testing.T) {
	repoCopy := filepath.Join("..", "..", "..", "..", "config", "schema", "pack.schema.json")
	want, err := os.ReadFile(repoCopy)
	if err != nil {
		t.Fatalf("reading %s: %v", repoCopy, err)
	}
	if !bytes.Equal(want, PackSchema()) {
		t.Fatalf("src/internal/core/config/schema/pack.schema.json differs from %s; copy the repo file over the embedded one", repoCopy)
	}
}

const validPack = `{
  "name": "icon-sets-demo",
  "pack": "ichava/icon-sets-demo",
  "pack_root": "/work/icon-sets-demo",
  "version_check_url": "https://registry.npmjs.org/demo/latest",
  "source": {"type": "npm", "package": "demo", "source_path": "icons"},
  "sinks": [
    {"type": "filesystem", "root": "{pack_root}/files"},
    {"type": "git-branch", "repo_root": "{pack_root}", "branch": "chore/sync-upstream-{version}"}
  ]
}`

func withField(t *testing.T, base string, mutate func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(base), &m); err != nil {
		t.Fatal(err)
	}
	mutate(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestValidatePackDocument(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(m map[string]any)
		wantErr string // a JSON pointer or message fragment; "" means valid
	}{
		{"the valid baseline", func(map[string]any) {}, ""},
		{"comment keys are allowed", func(m map[string]any) { m["_note"] = "x" }, ""},
		{"$schema hint is allowed", func(m map[string]any) { m["$schema"] = "./schema/pack.schema.json" }, ""},
		{"unknown top-level key", func(m map[string]any) { m["typo"] = 1 }, "typo"},
		{"missing required name", func(m map[string]any) { delete(m, "name") }, "name"},
		{"npm without a package", func(m map[string]any) {
			m["source"] = map[string]any{"type": "npm"}
		}, "package"},
		{"unknown source type", func(m map[string]any) {
			m["source"] = map[string]any{"type": "ftp"}
		}, "/source/type"},
		{"filesystem sink without a root", func(m map[string]any) {
			m["sinks"] = []any{map[string]any{"type": "filesystem"}}
		}, "/sinks/0"},
		{"git-branch sink with a typo", func(m map[string]any) {
			m["sinks"] = []any{map[string]any{"type": "git-branch", "brnach": "x"}}
		}, "/sinks/0"},
		{"min_retention above 1", func(m map[string]any) {
			m["sinks"] = []any{map[string]any{"type": "filesystem", "root": "x", "min_retention": 2}}
		}, "/sinks/0"},
		{"a known transform", func(m map[string]any) {
			m["transforms"] = []any{
				map[string]any{"type": "sanitise", "also_strip_class": true},
				map[string]any{"type": "slugify"},
			}
		}, ""},
		{"an unknown transform", func(m map[string]any) {
			m["transforms"] = []any{map[string]any{"type": "compress"}}
		}, "/transforms/0"},
		{"subset without a subdir", func(m map[string]any) {
			m["transforms"] = []any{map[string]any{"type": "subset"}}
		}, "/transforms/0"},
		{"pack settings", func(m map[string]any) {
			m["settings"] = map[string]any{"dry_run": true, "git": map[string]any{"base": "trunk"}}
		}, ""},
		{"a run-wide setting in a pack", func(m map[string]any) {
			m["settings"] = map[string]any{"concurrency": 4}
		}, "/settings"},
		{"an unquoted unicode version", func(m map[string]any) {
			m["settings"] = map[string]any{"emoji": map[string]any{"unicode_version": 16.0}}
		}, "/settings/emoji/unicode_version"},
		{"a malformed author", func(m map[string]any) {
			m["settings"] = map[string]any{"git": map[string]any{"author": "nobody"}}
		}, "/settings/git/author"},
		{"an unknown recipe", func(m map[string]any) { m["recipe"] = "magic" }, "/recipe"},
		{"a non-http check url", func(m map[string]any) { m["version_check_url"] = "ftp://x" }, "/version_check_url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePackDocument(withField(t, validPack, tt.mutate))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want an error mentioning %q, got none", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error does not mention %q:\n%v", tt.wantErr, err)
			}
		})
	}
}

func TestLoadPackReadsYAML(t *testing.T) {
	dir := t.TempDir()
	body := `name: icon-sets-demo
pack: ichava/icon-sets-demo
pack_root: /work/icon-sets-demo
version_check_url: https://registry.npmjs.org/demo/latest
source:
  type: npm
  package: demo
sinks:
  - type: filesystem
    root: "{pack_root}/files"
settings:
  git:
    base: trunk
`
	if err := os.WriteFile(filepath.Join(dir, "icon-sets-demo.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pack, err := LoadPack("icon-sets-demo", dir)
	if err != nil {
		t.Fatal(err)
	}
	if pack.Source.Package != "demo" || len(pack.Sinks) != 1 || pack.Sinks[0].Root != "{pack_root}/files" {
		t.Fatalf("decoded %+v", pack)
	}
	if git, _ := pack.Settings["git"].(map[string]any); git["base"] != "trunk" {
		t.Fatalf("settings = %v", pack.Settings)
	}
	if !strings.HasSuffix(pack.File, "icon-sets-demo.yaml") {
		t.Fatalf("File = %q", pack.File)
	}
}

func TestLoadPackRefusesTwoFormatsAtOnce(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"icon-sets-demo.json", "icon-sets-demo.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(validPack), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadPack("icon-sets-demo", dir); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err = %v, want ambiguous", err)
	}
}

func TestLoadRegistryRefusesADuplicate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "packs.json"), []byte(`{"packs":["a","a"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRegistry(dir); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("err = %v", err)
	}
}

func TestSinkAndSourceMarshalKeepExtras(t *testing.T) {
	var src SourceConfig
	if err := json.Unmarshal([]byte(`{"type":"url","version_field":"v"}`), &src); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"version_field":"v"`) {
		t.Fatalf("extras lost on marshal: %s", out)
	}
}
