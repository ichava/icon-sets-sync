package recipes

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/core/sinks"
	"github.com/ichava/icon-sets-sync/src/internal/core/sources"
	"github.com/ichava/icon-sets-sync/src/internal/core/transforms"
)

var shippedConfigDir = filepath.Join("..", "..", "..", "..", "config")

// TestOpenMojiPackResolvesAndPinsItsLicence reads the config this repository
// ships, because that is the file a scheduled run reads: a recipe that only
// works for a hand-built PackConfig proves nothing about the pack.
func TestOpenMojiPackResolvesAndPinsItsLicence(t *testing.T) {
	pack, err := config.LoadPack("icon-sets-openmoji", shippedConfigDir)
	if err != nil {
		t.Fatalf("the shipped config does not load: %v", err)
	}
	if pack.Pack != "ichava/icon-sets-openmoji" {
		t.Errorf("targets pack %q, want ichava/icon-sets-openmoji", pack.Pack)
	}
	if got := Select(pack); got != RecipeOpenMojiSets {
		t.Fatalf("Select = %q, want %q", got, RecipeOpenMojiSets)
	}
	if pack.Licence != "CC-BY-SA-4.0" {
		t.Errorf("licence = %q, want CC-BY-SA-4.0", pack.Licence)
	}
	if pack.Source.Owner != "hfg-gmuend" || pack.Source.Repo != "openmoji" {
		t.Errorf("upstream = %s/%s, want hfg-gmuend/openmoji", pack.Source.Owner, pack.Source.Repo)
	}

	reg, err := config.LoadRegistry(shippedConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, name := range reg.Packs {
		listed = listed || name == "icon-sets-openmoji"
	}
	if !listed {
		t.Errorf("config/packs.json does not list icon-sets-openmoji, so no scheduled run reaches it: %v", reg.Packs)
	}

	built, err := buildFor(pack, "17.0.0", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(built) != 3 {
		t.Fatalf("got %d pipelines, want 3 (colour, black, commit)", len(built))
	}
	for i, c := range []struct{ name, subdir string }{{"openmoji-color", "color/svg"}, {"openmoji-black", "black/svg"}} {
		p := built[i]
		assertLicenceWiring(t, p, "openmoji")
		if !strings.Contains(p.Name(), c.name+"@17.0.0") {
			t.Errorf("pipeline %q does not name its set and pinned tag %s@17.0.0", p.Name(), c.name)
		}
		stages := p.Stages()
		lt := stages[indexOf(stageNames(p), "LicenceTexts")].(sinks.LicenceTexts)
		if lt.Licence != "CC-BY-SA-4.0" {
			t.Errorf("%s: LicenceTexts guard declares %q, want CC-BY-SA-4.0", p.Name(), lt.Licence)
		}
		fs := stages[indexOf(stageNames(p), "Filesystem")].(sinks.Filesystem)
		if !strings.HasSuffix(filepath.ToSlash(fs.Root), "/resources/assets/svg/files/"+c.name) {
			t.Errorf("%s: writes to %q", p.Name(), fs.Root)
		}
		arch := stages[0].(sources.Compound).Second.(sources.GithubArchive)
		if arch.Version != "17.0.0" || !strings.Contains(arch.ArchiveURL, "/refs/tags/{version}.zip") {
			t.Errorf("%s: source is not pinned to a tag: %+v", p.Name(), arch)
		}
	}
}

func TestOpenMojiRecipeNeedsAVersion(t *testing.T) {
	om, err := config.LoadPack("icon-sets-openmoji", shippedConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildFor(om, "", true); err == nil {
		t.Fatal("an empty version must be refused, not fetched as an empty tag")
	}
}

func openmojiPack(t *testing.T) *config.PackConfig {
	t.Helper()
	om, err := config.LoadPack("icon-sets-openmoji", shippedConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	om.PackRoot = "/work/x"
	return om
}

func TestUnicodeVersionIsDerivedFromTheOpenMojiRelease(t *testing.T) {
	for _, tc := range []struct{ release, want string }{
		{"17.0.0", "17.0"}, {"15.1.0", "15.1"}, {"16.0.0", "16.0"}, {"nightly", ""},
	} {
		if got := unicodeVersionFor(tc.release); got != tc.want {
			t.Errorf("unicodeVersionFor(%q) = %q, want %q", tc.release, got, tc.want)
		}
	}

	built, err := buildFor(openmojiPack(t), "17.0.0", true)
	if err != nil {
		t.Fatal(err)
	}
	cldr := built[0].Stages()[0].(sources.Compound).First.(sources.UnicodeCldr)
	if cldr.UnicodeVersion != "17.0" {
		t.Errorf("OpenMoji 17.0.0 is categorised against Unicode %q, want 17.0", cldr.UnicodeVersion)
	}
}

// The derived taxonomy never falls below the default. OpenMoji 15.1.0 against
// the 15.1 taxonomy put crab, lobster, oyster, shrimp and squid back in
// food-drink, from the animals-nature that Unicode 16.0 moved them to and the
// pack ships, and the removal guard refused every run.
func TestTheDerivedTaxonomyNeverFallsBelowTheDefault(t *testing.T) {
	for _, tc := range []struct{ release, floor, want string }{
		{"15.1.0", "16.0", "16.0"}, {"16.0.0", "16.0", "16.0"}, {"17.0.0", "16.0", "17.0"},
		{"9.0.0", "16.0", "16.0"}, {"nightly", "16.0", "16.0"}, {"16.10.0", "16.9", "16.10"},
	} {
		if got := taxonomyFor(tc.release, tc.floor); got != tc.want {
			t.Errorf("taxonomyFor(%q, %q) = %q, want %q", tc.release, tc.floor, got, tc.want)
		}
	}

	built, err := buildFor(openmojiPack(t), "15.1.0", true)
	if err != nil {
		t.Fatal(err)
	}
	cldr := built[0].Stages()[0].(sources.Compound).First.(sources.UnicodeCldr)
	if cldr.UnicodeVersion != settings.DefaultUnicodeVersion {
		t.Errorf("OpenMoji 15.1.0 is categorised against Unicode %q, want the default %s", cldr.UnicodeVersion, settings.DefaultUnicodeVersion)
	}
}

// An explicit emoji.unicode_version always wins over the derived one.
func TestExplicitUnicodeVersionBeatsTheDerivedOne(t *testing.T) {
	r := settings.Resolve(settings.LayerValues{Layer: settings.LayerEnv, Values: settings.Values{"emoji.unicode_version": "16.0"}})
	built, err := Build(openmojiPack(t), "17.0.0", Options{DryRun: true, Settings: &r})
	if err != nil {
		t.Fatal(err)
	}
	cldr := built[0].Stages()[0].(sources.Compound).First.(sources.UnicodeCldr)
	if cldr.UnicodeVersion != "16.0" {
		t.Errorf("got %q, want the explicit 16.0", cldr.UnicodeVersion)
	}
}

// TestOpenMojiPackNeverDropsAnIconButEmojiIsUnchanged is the split: the
// openmoji pack opts into preserving, guarded removals and a forced override;
// the emoji pack (Twemoji) keeps the exact-match, drop-and-count behaviour.
func TestOpenMojiPackNeverDropsAnIconButEmojiIsUnchanged(t *testing.T) {
	emoji := &config.PackConfig{
		Name: "icon-sets-twemoji", Pack: "ichava/icon-sets-twemoji", PackRoot: "/work/x", Recipe: RecipeTwemojiSets,
		VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
		Source: config.SourceConfig{Type: "url"},
	}
	eb, err := buildFor(emoji, "15.0.0", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range eb {
		for _, st := range p.Stages() {
			switch v := st.(type) {
			case transforms.Categorise:
				if v.Preserve || v.MaxUncategorised != 0 || v.Force {
					t.Errorf("%s: emoji Categorise gained opt-in behaviour: %+v", p.Name(), v)
				}
			case sinks.Filesystem:
				if v.RefuseRemovals || v.Force {
					t.Errorf("%s: emoji Filesystem gained opt-in behaviour: %+v", p.Name(), v)
				}
			}
		}
	}

	r := settings.Resolve(settings.LayerValues{Layer: settings.LayerFlag, Values: settings.Values{"force": true}})
	ob, err := Build(openmojiPack(t), "17.0.0", Options{DryRun: true, Settings: &r})
	if err != nil {
		t.Fatal(err)
	}
	var cat, fs, san int
	for _, p := range ob {
		for _, st := range p.Stages() {
			switch v := st.(type) {
			case transforms.Sanitise:
				san++
				if !v.Force {
					t.Errorf("%s: Sanitise = %+v, want forced, so --force lifts the stray-text limit", p.Name(), v)
				}
			case transforms.Categorise:
				cat++
				if !v.Preserve || v.MaxUncategorised <= 0 || !v.Force {
					t.Errorf("%s: Categorise = %+v, want preserving, guarded and forced", p.Name(), v)
				}
			case sinks.Filesystem:
				fs++
				if !v.RefuseRemovals || !v.Force {
					t.Errorf("%s: Filesystem = %+v, want removals refused and forced", p.Name(), v)
				}
			}
		}
	}
	if cat != 2 || fs != 2 || san != 2 {
		t.Fatalf("inspected %d Categorise, %d Filesystem and %d Sanitise stages, want 2 of each", cat, fs, san)
	}
}
