package recipes

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// dryRunPacks is one pack per recipe, rooted at packRoot. Every caller of a
// sync -- `sync`, `sync-all`, the menu and the web UI's "Sync stale packs (dry
// run)" -- builds through Build, so covering every recipe here covers them.
func dryRunPacks(t *testing.T, packRoot string) (map[string]*config.PackConfig, string) {
	t.Helper()
	generic := npmPack("icon-sets-flag")
	generic.PackRoot = packRoot

	emoji := &config.PackConfig{
		Name: "icon-sets-twemoji", Pack: "ichava/icon-sets-twemoji", PackRoot: packRoot, Recipe: RecipeTwemojiSets,
		VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
		Source: config.SourceConfig{Type: "url"},
	}
	openmoji := &config.PackConfig{
		Name: "icon-sets-openmoji", Pack: "ichava/icon-sets-openmoji", PackRoot: packRoot, Recipe: RecipeOpenMojiSets,
		VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
		Source: config.SourceConfig{Type: "url"}, Licence: "CC-BY-SA-4.0",
	}

	dir := t.TempDir()
	m := &manifestpack.Manifest{Pack: "ichava/icon-sets-bundled", Sets: map[string]manifestpack.SetSource{
		"demo": {Package: "demo", Path: "package"},
	}}
	if err := m.Save(mustManifestPath(t, dir, "icon-sets-bundled")); err != nil {
		t.Fatal(err)
	}
	bundled := &config.PackConfig{
		Name: "icon-sets-bundled", Pack: "ichava/icon-sets-bundled", Recipe: RecipeManifestPack, PackRoot: packRoot,
		VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
		Source: config.SourceConfig{Type: "url"},
	}
	return map[string]*config.PackConfig{
		RecipeGeneric: generic, RecipeTwemojiSets: emoji, RecipeOpenMojiSets: openmoji, RecipeManifestPack: bundled,
	}, dir
}

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestEveryRecipesDryRunLeavesThePackUntouched runs every sink a dry-run build
// keeps against a fake pack checkout, with a fetched tree that differs from it,
// and requires the checkout byte-identical afterwards.
func TestEveryRecipesDryRunLeavesThePackUntouched(t *testing.T) {
	for _, dry := range []bool{true, false} {
		packs, configDir := dryRunPacks(t, "/unused")
		for recipe, pack := range packs {
			built, err := Build(pack, "17.0.0", Options{DryRun: dry, ConfigDir: configDir})
			if err != nil {
				t.Fatalf("%s: %v", recipe, err)
			}
			for _, p := range built {
				v, ok := p.ConfigValue(pipeline.ConfigDryRun)
				if dry && v != true {
					t.Errorf("%s: dry-run pipeline %s is not marked dry_run (got %v)", recipe, p.Name(), v)
				}
				if !dry && ok {
					t.Errorf("%s: real pipeline %s carries dry_run=%v", recipe, p.Name(), v)
				}
			}
		}
	}

	packs, _ := dryRunPacks(t, "/unused")
	for recipe := range packs {
		t.Run(recipe, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "pack")
			for rel, body := range map[string]string{
				"composer.json": "{}",
				"resources/assets/svg/files/twemoji/old.svg":        "<svg/>",
				"resources/assets/svg/files/openmoji-color/old.svg": "<svg/>",
				"resources/assets/svg/files/demo/old.svg":           "<svg/>",
				"resources/assets/svg/files/old.svg":                "<svg/>",
				"resources/assets/svg/licenses/demo/LICENSE":        "old",
			} {
				p := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			packs, configDir := dryRunPacks(t, root)
			built, err := Build(packs[recipe], "17.0.0", Options{DryRun: true, ConfigDir: configDir})
			if err != nil {
				t.Fatal(err)
			}

			fetched := t.TempDir()
			lic := filepath.Join(fetched, "LICENSE")
			for name, body := range map[string]string{"new.svg": "<svg>new</svg>", "old.svg": "<svg>changed</svg>"} {
				if err := os.WriteFile(filepath.Join(fetched, name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(lic, []byte("Attribution-ShareAlike 4.0 International\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			before := snapshot(t, root)
			ran := 0
			for _, p := range built {
				cfg := map[string]any{}
				for _, k := range []string{pipeline.ConfigPack, pipeline.ConfigPackRoot, pipeline.ConfigDryRun} {
					if v, ok := p.ConfigValue(k); ok {
						cfg[k] = v
					}
				}
				for _, stage := range p.Stages() {
					sink, ok := stage.(pipeline.Sink)
					if !ok {
						continue
					}
					ctx := &pipeline.Context{
						PipelineName: p.Name(), WorkingDir: t.TempDir(),
						Config: cfg, Extras: map[string]any{}, Metrics: map[string]any{},
					}
					ctx.SetFetchedPath(fetched)
					ctx.SetFetchedVersion("17.0.0")
					ctx.SetString(pipeline.KeyLicenceFiles, lic)
					if err := sink.Execute(ctx); err != nil {
						t.Fatalf("%s %s: %v", p.Name(), sink.Name(), err)
					}
					ran++
				}
			}
			if ran == 0 {
				t.Fatal("no sink ran, so this proves nothing")
			}
			if after := snapshot(t, root); !reflect.DeepEqual(before, after) {
				t.Errorf("a %s dry run wrote into the pack:\nbefore %v\nafter  %v", recipe, before, after)
			}
		})
	}
}
