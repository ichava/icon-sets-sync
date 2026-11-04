package recipes

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/sinks"
	"github.com/ichava/icon-sets-sync/src/internal/core/sources"
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

// TestOpenMojiAndEmojiBuildTheSameOpenMojiSets keeps the deprecated emoji sets
// and the new pack byte-identical: both must run the same source, the same
// transforms and write to the same relative paths for the same release.
func TestOpenMojiAndEmojiBuildTheSameOpenMojiSets(t *testing.T) {
	emoji := &config.PackConfig{
		Name: "icon-sets-emoji", Pack: "ichava/icon-sets-emoji", PackRoot: "/work/x",
		VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
		Source: config.SourceConfig{Type: "url"},
	}
	om, err := config.LoadPack("icon-sets-openmoji", shippedConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	om.PackRoot = "/work/x"

	eb, err := buildFor(emoji, "15.0.0", true) // twemoji, colour, black
	if err != nil {
		t.Fatal(err)
	}
	ob, err := buildFor(om, DefaultOpenMojiVersion, true) // colour, black
	if err != nil {
		t.Fatal(err)
	}
	if len(ob) != 2 {
		t.Fatalf("got %d openmoji pipelines, want 2", len(ob))
	}
	for i := range ob {
		e, o := eb[i+1].Stages(), ob[i].Stages()
		fsE, fsO := indexOf(stageNames(eb[i+1]), "Filesystem"), indexOf(stageNames(ob[i]), "Filesystem")
		if fsE != fsO || !reflect.DeepEqual(e[:fsE+1], o[:fsO+1]) {
			t.Errorf("%s: source, transforms or file sink differ from emoji's", ob[i].Name())
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
