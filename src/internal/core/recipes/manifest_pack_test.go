package recipes

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
)

// The manifest-pack recipe is reached one way: by naming it in config. It
// builds the same pipelines whichever pack names it, and logs nothing about
// how it was reached.
func TestManifestPackDispatchIsTheSameForEveryPack(t *testing.T) {
	tests := []struct {
		label, name, recipe string
	}{
		{"recipe named in config", "icon-sets-bundled", RecipeManifestPack},
		{"another pack, recipe named in config", "icon-sets-example", RecipeManifestPack},
	}

	var want string
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			dir := t.TempDir()
			m := &manifestpack.Manifest{
				Pack: "ichava/" + tt.name,
				Sets: map[string]manifestpack.SetSource{
					"bootstrap-icons": {Package: "bootstrap-icons", Path: "package/icons"},
					"feather-icons":   {Package: "feather-icons", Path: "package/dist/icons", Version: "4.29.2"},
				},
			}
			if err := m.Save(manifestpack.ManifestPath(dir, tt.name)); err != nil {
				t.Fatal(err)
			}
			pack := &config.PackConfig{
				Name: tt.name, Pack: "ichava/" + tt.name, PackRoot: "/work/p", Recipe: tt.recipe,
				VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
				Source: config.SourceConfig{Type: "url"},
			}
			var logs bytes.Buffer
			built, err := Build(pack, "2.2.0", Options{ConfigDir: dir, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
			if err != nil {
				t.Fatal(err)
			}

			// The shape, with the pack name taken out, must not depend on how
			// the recipe was reached.
			var shape []string
			for _, p := range built {
				shape = append(shape, strings.TrimPrefix(p.Name(), tt.name)+"="+strings.Join(stageNames(p), ","))
			}
			got := strings.Join(shape, "\n")
			if len(built) != 3 {
				t.Fatalf("got %d pipelines, want 2 sets and the commit:\n%s", len(built), got)
			}
			if want == "" {
				want = got
			} else if got != want {
				t.Errorf("pipelines differ from the config-named dispatch:\n%s\nwant:\n%s", got, want)
			}

			if strings.Contains(logs.String(), "deprecated") {
				t.Errorf("logged a deprecation notice:\n%s", logs.String())
			}
		})
	}
}

// The recipe's former name and the pack-name dispatch were removed: neither
// reaches manifest-pack any more, so a config has to name it.
func TestManifestPackIsNotReachedByItsFormerNameOrByPackName(t *testing.T) {
	for _, pack := range []config.PackConfig{
		{Name: "icon-sets-example", Recipe: "bundled", Source: config.SourceConfig{Type: "url"}},
		{Name: "icon-sets-bundled", Source: config.SourceConfig{Type: "url"}},
		{Name: "icon-sets-bundled", Recipe: RecipeAuto, Source: config.SourceConfig{Type: "url"}},
	} {
		if got := Select(&pack); got == RecipeManifestPack {
			t.Errorf("Select(%s, recipe %q) = %q; only \"recipe\": %q may select it", pack.Name, pack.Recipe, got, RecipeManifestPack)
		}
	}
}
