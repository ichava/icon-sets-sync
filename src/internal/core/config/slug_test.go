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

func TestCanonicalSlug(t *testing.T) {
	for in, want := range map[string]string{
		"icon-sets-extras.ant-design": "icon-sets-extras-ant-design",
		"Icon-Sets-Extras.Feather":    "icon-sets-extras-feather",
		"icon_sets.x":                 "icon-sets-x",
	} {
		got, deprecated := CanonicalSlug(in)
		if got != want || !deprecated {
			t.Errorf("CanonicalSlug(%q) = %q, %v; want %q, true", in, got, deprecated, want)
		}
	}
	if got, deprecated := CanonicalSlug("icon-sets-flag"); got != "icon-sets-flag" || deprecated {
		t.Errorf("a slug is its own canonical form: %q %v", got, deprecated)
	}
	if _, deprecated := CanonicalSlug("../etc"); deprecated {
		t.Error("a path is not a deprecated spelling of a slug")
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

// The dotted names are deprecated, not removed: a registry entry, a pack's
// "name" and a --pack argument in the old spelling still resolve, to the
// canonical slug, with a warning.
func TestDeprecatedDottedNamesStillResolve(t *testing.T) {
	var warned []string
	old := Deprecated
	Deprecated = func(msg string) { warned = append(warned, msg) }
	t.Cleanup(func() { Deprecated = old })

	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("packs.json", `{"packs":["icon-sets-extras.feather"]}`)
	// A user's own config still in the old spelling, file name and all.
	write("icon-sets-extras.feather.json", `{"name":"icon-sets-extras.feather","pack":"ichava/icon-sets-extras",
		"pack_root":"/tmp/x","version_check_url":"https://registry.npmjs.org/feather-icons/latest",
		"source":{"type":"npm","package":"feather-icons"}}`)

	reg, err := ParseRegistryBytes(filepath.Join(dir, "packs.json"), mustRead(t, filepath.Join(dir, "packs.json")))
	if err != nil || reg.Packs[0] != "icon-sets-extras-feather" {
		t.Fatalf("registry: %+v %v", reg, err)
	}
	p, err := LoadPack("icon-sets-extras.feather", dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "icon-sets-extras-feather" {
		t.Errorf("name %q, want the canonical slug", p.Name)
	}
	if p2, err := LoadPack("icon-sets-extras-feather", dir); err != nil || p2.Name != p.Name {
		t.Errorf("the canonical slug does not find the old-spelled file: %v", err)
	}
	if len(warned) == 0 || !strings.Contains(strings.Join(warned, "\n"), "icon-sets-extras-feather") {
		t.Errorf("no deprecation warning naming the new slug: %q", warned)
	}
}

// A name that is not a slug and has no slug spelling is refused, naming the rule.
func TestANameThatIsNoSlugIsRefused(t *testing.T) {
	_, err := ParseRegistryBytes("packs.json", []byte(`{"packs":["icon sets/../x"]}`))
	if err == nil || !strings.Contains(err.Error(), "lowercase") {
		t.Fatalf("err = %v", err)
	}
}
