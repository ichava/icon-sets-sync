package packcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const goodComposer = `{"name": "ichava/icon-sets-demo", "type": "ichava-icon-set"}`
const goodManifest = `{"schema_version": "1.0", "package": {"name": "ichava/icon-sets-demo"}}`

func goodPack(t *testing.T) string {
	root := filepath.Join(t.TempDir(), "icon-sets-demo")
	put(t, root, map[string]string{
		"composer.json":                    goodComposer,
		"LICENSE":                          "MIT",
		"resources/assets/svg/config.json": goodManifest,
		"resources/assets/svg/licenses/demo/LICENSE":   "MIT",
		"resources/assets/svg/files/outline/a.svg":     `<svg xmlns="http://www.w3.org/2000/svg"/>`,
		"resources/assets/svg/files/outline/cat/b.svg": `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
	})
	return root
}

func TestAGoodPackHasNoProblems(t *testing.T) {
	r := Check(goodPack(t), Options{PackConfigs: map[string]string{"ichava/icon-sets-demo": "icon-sets-demo.json"}, ParseSVGs: true})
	if r.Errors() != 0 || r.Warnings() != 0 {
		t.Fatalf("problems: %+v", r.Findings)
	}
	if r.Name != "ichava/icon-sets-demo" || r.SVGs != 2 || r.Sets != 1 {
		t.Errorf("report: %+v", r)
	}
}

func TestEveryCheckCanFail(t *testing.T) {
	cases := map[string]struct {
		mutate func(root string)
		want   string
	}{
		"composer missing": {func(r string) { _ = os.Remove(filepath.Join(r, "composer.json")) }, "composer.json"},
		"composer wrong type": {func(r string) {
			put(t, r, map[string]string{"composer.json": `{"name":"ichava/icon-sets-demo","type":"library"}`})
		}, "ichava-icon-set"},
		"manifest missing": {func(r string) { _ = os.Remove(filepath.Join(r, "resources/assets/svg/config.json")) }, "manifest"},
		"manifest mismatch": {func(r string) {
			put(t, r, map[string]string{"resources/assets/svg/config.json": `{"schema_version":"1.0","package":{"name":"ichava/other"}}`})
		}, "names ichava/other"},
		"no pack config":  {func(r string) {}, "no pack config"},
		"no licence":      {func(r string) { _ = os.RemoveAll(filepath.Join(r, "resources/assets/svg/licenses")) }, "licence"},
		"no root LICENSE": {func(r string) { _ = os.Remove(filepath.Join(r, "LICENSE")) }, "LICENSE"},
		"empty set":       {func(r string) { _ = os.MkdirAll(filepath.Join(r, "resources/assets/svg/files/empty"), 0o755) }, "empty"},
		"empty category":  {func(r string) { _ = os.MkdirAll(filepath.Join(r, "resources/assets/svg/files/outline/nothing"), 0o755) }, "nothing"},
		"stray file": {func(r string) {
			put(t, r, map[string]string{"resources/assets/svg/files/outline/readme.png": "x"})
		}, "readme.png"},
		"bad svg": {func(r string) {
			put(t, r, map[string]string{"resources/assets/svg/files/outline/broken.svg": "<svg><g></svg>"})
		}, "broken.svg"},
		"not svg root": {func(r string) {
			put(t, r, map[string]string{"resources/assets/svg/files/outline/html.svg": "<html></html>"})
		}, "html.svg"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := goodPack(t)
			tc.mutate(root)
			configs := map[string]string{"ichava/icon-sets-demo": "icon-sets-demo.json"}
			if name == "no pack config" {
				configs = nil
			}
			r := Check(root, Options{PackConfigs: configs, ParseSVGs: true})
			found := false
			for _, f := range r.Findings {
				if strings.Contains(f.Text, tc.want) || strings.Contains(f.Path, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no finding naming %q: %+v", tc.want, r.Findings)
			}
		})
	}
}

// Discover finds the ichava packs under each directory by their composer
// type, and only those.
func TestDiscoverFindsPacksByComposerType(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "icon-sets-a"), map[string]string{"composer.json": `{"name":"ichava/icon-sets-a","type":"ichava-icon-set"}`})
	put(t, filepath.Join(dir, "core"), map[string]string{"composer.json": `{"name":"ichava/core","type":"library"}`})
	put(t, filepath.Join(dir, "js-motion"), map[string]string{"package.json": `{}`})
	got, missing := Discover([]string{dir, filepath.Join(dir, "nope")})
	if len(got) != 1 || filepath.Base(got[0]) != "icon-sets-a" {
		t.Errorf("discovered %v", got)
	}
	if len(missing) != 1 {
		t.Errorf("missing %v", missing)
	}
}

// A single-set pack keeps its SVGs straight in files/; OS junk is ignored.
func TestASingleSetPackAndOSJunkAreFine(t *testing.T) {
	root := filepath.Join(t.TempDir(), "icon-sets-demo")
	put(t, root, map[string]string{
		"composer.json":                    goodComposer,
		"LICENSE":                          "MIT",
		"resources/assets/svg/config.json": goodManifest,
		"resources/assets/svg/licenses/demo/LICENSE": "MIT",
		"resources/assets/svg/files/a.svg":           `<svg xmlns="http://www.w3.org/2000/svg"/>`,
		"resources/assets/svg/files/.DS_Store":       "junk",
		"resources/assets/svg/files/cat/b.svg":       `<?xml version="1.0" encoding="iso-8859-1"?><svg xmlns="http://www.w3.org/2000/svg"/>`,
	})
	r := Check(root, Options{PackConfigs: map[string]string{"ichava/icon-sets-demo": "x"}, ParseSVGs: true})
	if r.Errors() != 0 || r.Sets != 1 || r.SVGs != 2 {
		t.Fatalf("%+v", r)
	}
}
