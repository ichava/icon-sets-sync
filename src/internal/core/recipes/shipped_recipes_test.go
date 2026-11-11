package recipes

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/sinks"
)

// shippedRecipesFloor is how many pack configs config/ ships: the four packs
// that predate v0.1.0 (tabler, flag, emoji, openmoji) and the 34 v0.1.0
// recipes (14 packs and the 20 sets of icon-sets-extras), measured 2026-11-04.
// Raise it when a recipe is added; lowering it is removing a recipe.
const shippedRecipesFloor = 38

// TestEveryShippedRecipeValidates loads every registered pack config through
// the schema and builds its pipelines, and fails below the floor. A registry
// that silently lost entries, or a glob that stopped matching, would
// otherwise pass by validating nothing.
func TestEveryShippedRecipeValidates(t *testing.T) {
	configDir := filepath.Join("..", "..", "..", "..", "config")
	registry, err := config.LoadRegistry(configDir)
	if err != nil {
		t.Fatal(err)
	}

	validated := 0
	for _, slug := range registry.Packs {
		pack, err := config.LoadPack(slug, configDir)
		if err != nil {
			t.Errorf("%s: %v", slug, err)
			continue
		}
		version := pack.CurrentVersion
		if version == "" {
			version = "1.0.0"
		}
		for _, dryRun := range []bool{true, false} {
			if _, err := Build(pack, version, Options{DryRun: dryRun, ConfigDir: configDir}); err != nil {
				t.Errorf("%s (dry run %v): %v", slug, dryRun, err)
			}
		}
		validated++
	}
	if validated < shippedRecipesFloor {
		t.Fatalf("validated %d shipped pack configs, want at least %d", validated, shippedRecipesFloor)
	}

	// The other direction: a config file that is not registered is a recipe
	// nothing runs.
	files, err := filepath.Glob(filepath.Join(configDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, slug := range registry.Packs {
		registered[slug] = true
	}
	var stray []string
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		if name != "packs" && !strings.HasSuffix(name, ".sets") && !registered[name] {
			stray = append(stray, name)
		}
	}
	sort.Strings(stray)
	if len(stray) > 0 {
		t.Errorf("config files not in packs.json: %v", stray)
	}
}

// TestSchemaLicencesMatchTheSignatures: the schema's `licence` enum and the
// sink's signature table are two lists of one fact. A licence the schema
// admits but the sink cannot recognise fails every sync of that pack; one the
// sink knows but the schema refuses can never be declared.
func TestSchemaLicencesMatchTheSignatures(t *testing.T) {
	var doc struct {
		Properties struct {
			Licence struct {
				Enum []string `json:"enum"`
			} `json:"licence"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(config.PackSchema(), &doc); err != nil {
		t.Fatal(err)
	}
	got := append([]string(nil), doc.Properties.Licence.Enum...)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(sinks.LicenceIDs(), ",") {
		t.Errorf("schema licence enum %v, sink signatures %v", got, sinks.LicenceIDs())
	}
}

// TestGenericCarriesTheLicenceGuards: every licence field a pack config sets
// must reach the sink, or the guard is decorative.
func TestGenericCarriesTheLicenceGuards(t *testing.T) {
	pack := npmPack("icon-sets-demo")
	pack.Licence = "Apache-2.0"
	pack.LicenceSHA256 = strings.Repeat("ab", 32)
	pack.LicenceSet = "carbon"
	pack.LicenceMissing = "npm ships none"
	built, err := Build(pack, "1.0.0", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range built[0].Stages() {
		if lt, ok := s.(sinks.LicenceTexts); ok {
			if lt.Licence != "Apache-2.0" || lt.SHA256 != pack.LicenceSHA256 || lt.Set != "carbon" ||
				!strings.HasSuffix(lt.Root, "/licenses/carbon") || lt.MissingReason != "npm ships none" {
				t.Errorf("sink got %+v", lt)
			}
			return
		}
	}
	t.Fatal("no LicenceTexts stage")
}
