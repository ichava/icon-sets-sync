package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsSlug(t *testing.T) {
	for s, want := range map[string]bool{
		"icon-sets-flag": true, "icon-sets-extras-ant-design": true, "a1": true,
		"icon-sets-extras.ant-design": false, "Icon-Sets": false, "a_b": false,
		"": false, "-a": false, "a-": false, "a--b": false, "a b": false, "../x": false,
	} {
		if IsSlug(s) != want {
			t.Errorf("IsSlug(%q) = %v, want %v", s, !want, want)
		}
	}
}

// Every name shipped in config/ -- each pack config's file name, its "name"
// and each registry entry -- is a lowercase slug. Counted, so an empty glob
// fails rather than passes.
func TestEveryShippedNameIsASlug(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "config")
	// Read the raw documents: the loaders accept a deprecated spelling and
	// hand back the slug, which would hide one shipped by mistake.
	var reg PacksRegistry
	if err := json.Unmarshal(mustRead(t, filepath.Join(dir, "packs.json")), &reg); err != nil {
		t.Fatal(err)
	}
	for _, slug := range reg.Packs {
		if !IsSlug(slug) {
			t.Errorf("packs.json lists %q, which is not a slug", slug)
		}
	}
	if len(reg.Packs) < 30 {
		t.Fatalf("read %d registry entries, want at least 30", len(reg.Packs))
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	n := 0
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".json")
		if base == "packs" {
			continue
		}
		n++
		if !IsSlug(base) {
			t.Errorf("config file %s is not named by a slug", filepath.Base(f))
		}
		var p struct{ Name string }
		if err := json.Unmarshal(mustRead(t, f), &p); err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if !IsSlug(p.Name) || p.Name != base {
			t.Errorf("%s: name %q is not the slug %q", filepath.Base(f), p.Name, base)
		}
	}
	if n != len(reg.Packs) {
		t.Errorf("%d pack configs, %d registry entries", n, len(reg.Packs))
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Old spellings are refused, not translated: the error states the rule and
// names the slug to use, wherever the name comes from.
func TestNonSlugNamesAreRefusedWithTheSlugToUse(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("icon-sets-extras-feather.json", `{"name":"icon-sets-extras.feather","pack":"ichava/icon-sets-extras",
		"pack_root":"/tmp/x","version_check_url":"https://registry.npmjs.org/feather-icons/latest",
		"source":{"type":"npm","package":"feather-icons"}}`)
	write("icon-sets-extras.feather.json", `{}`)

	check := func(what string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: an old spelling was accepted", what)
		}
		for _, want := range []string{"lowercase slug", `"icon-sets-extras-feather"`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: %q lacks %q", what, err, want)
			}
		}
	}
	_, err := ParseRegistryBytes("packs.json", []byte(`{"packs":["icon-sets-extras.feather"]}`))
	check("registry", err)
	_, err = LoadPack("icon-sets-extras.feather", dir)
	check("--pack", err)
	_, err = LoadPack("Icon_Sets_Extras_Feather", dir)
	check("--pack, capitals", err)
	_, err = LoadPack("icon-sets-extras-feather", dir)
	check("name inside the file", err)
}

// A name with no slug spelling is refused with the rule and no suggestion.
func TestANameThatIsNoSlugIsRefused(t *testing.T) {
	_, err := ParseRegistryBytes("packs.json", []byte(`{"packs":["icon sets/../x"]}`))
	if err == nil || !strings.Contains(err.Error(), "lowercase slug") || strings.Contains(err.Error(), "use ") {
		t.Fatalf("err = %v", err)
	}
}

// The schema editors validate against and the loader use one pattern.
func TestSchemaSlugPatternMatchesIsSlug(t *testing.T) {
	var schema struct {
		Properties struct {
			Name struct {
				Pattern string `json:"pattern"`
			} `json:"name"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(mustRead(t, filepath.Join("..", "..", "..", "..", "config", "schema", "pack.schema.json")), &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Properties.Name.Pattern != SlugPattern {
		t.Errorf("schema name pattern %q, IsSlug uses %q", schema.Properties.Name.Pattern, SlugPattern)
	}
}
