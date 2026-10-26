package recipes

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
)

// The manifest-pack recipe is reached three ways until 0.2.0: by naming it in
// config, by its deprecated name "bundled", and, for the one pack that had it
// first, by pack name with no `recipe` at all. All three must build the same
// pipelines; only the deprecated two may log a notice.
func TestManifestPackDispatchIsTheSameWithOrWithoutTheField(t *testing.T) {
	const name = "icon-sets-bundled" // the pack that is dispatched by name
	tests := []struct {
		label, name, recipe string
		notice              string // "" means no notice may be logged
	}{
		{"recipe named in config", name, RecipeManifestPack, ""},
		{"no recipe field, dispatched by name", name, "", "by pack name is deprecated"},
		{"recipe auto, dispatched by name", name, RecipeAuto, "by pack name is deprecated"},
		{"deprecated recipe name", name, RecipeBundled, "is deprecated and will be removed in " + RemovalRelease + "; write "},
		{"another pack, recipe named in config", "icon-sets-example", RecipeManifestPack, ""},
		{"another pack, deprecated recipe name", "icon-sets-example", RecipeBundled, "is deprecated and will be removed in " + RemovalRelease + "; write "},
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

			if tt.notice == "" {
				if strings.Contains(logs.String(), "deprecated") {
					t.Errorf("logged a deprecation notice it should not have:\n%s", logs.String())
				}
				return
			}
			if !strings.Contains(logs.String(), tt.notice) || !strings.Contains(logs.String(), RemovalRelease) {
				t.Errorf("want a notice containing %q and %s, got:\n%s", tt.notice, RemovalRelease, logs.String())
			}
		})
	}
}

func TestDeprecationNamesTheReplacementAndTheRelease(t *testing.T) {
	tests := []struct {
		label string
		pack  config.PackConfig
		want  string // "" means no notice
	}{
		{"manifest-pack", config.PackConfig{Name: "icon-sets-example", Recipe: RecipeManifestPack}, ""},
		{"manifest-pack on the old name", config.PackConfig{Name: "icon-sets-bundled", Recipe: RecipeManifestPack}, ""},
		{"bundled", config.PackConfig{Name: "icon-sets-example", Recipe: RecipeBundled}, `write "recipe": "manifest-pack"`},
		{"by name", config.PackConfig{Name: "icon-sets-bundled"}, `set "recipe": "manifest-pack" in its config`},
		{"an unrelated pack", config.PackConfig{Name: "icon-sets-tabler"}, ""},
		{"emoji by name is not deprecated", config.PackConfig{Name: "icon-sets-emoji"}, ""},
		{"an explicit other recipe on the old name", config.PackConfig{Name: "icon-sets-bundled", Recipe: RecipeGeneric}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			got := Deprecation(&tt.pack)
			if tt.want == "" {
				if got != "" {
					t.Errorf("Deprecation = %q, want none", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) || !strings.Contains(got, "removed in "+RemovalRelease) {
				t.Errorf("Deprecation = %q, want it to contain %q and the removal release", got, tt.want)
			}
		})
	}
}
