package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// publicConfigDir is the repository's config/, the directory the Docker image
// ships as /app/config.
var publicConfigDir = filepath.Join("..", "..", "..", "..", "config")

// privateFixture is a synthetic private config directory: a registry, one
// pack config and its set manifest, none of which describes a real pack.
var privateFixture = filepath.Join("testdata", "private-config")

func TestPrivateConfigDirIsMergedIntoTheRegistry(t *testing.T) {
	defer SetPrivateConfigDir("")()

	public, err := LoadRegistry(publicConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(public.Packs, "icon-sets-example") {
		t.Fatal("the public registry already lists the private fixture's pack")
	}

	SetPrivateConfigDir(privateFixture)
	merged, err := LoadRegistry(publicConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	// Public packs first and in their order, then the private ones.
	want := append(slices.Clone(public.Packs), "icon-sets-example")
	if !slices.Equal(merged.Packs, want) {
		t.Fatalf("merged registry = %v, want %v", merged.Packs, want)
	}

	packs, err := LoadAll(publicConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != len(want) {
		t.Fatalf("loaded %d packs, want %d", len(packs), len(want))
	}
	last := packs[len(packs)-1]
	if last.Name != "icon-sets-example" || last.Recipe != "bundled" {
		t.Fatalf("private pack loaded as %+v", last)
	}
	if filepath.Dir(last.File) != privateFixture {
		t.Errorf("private pack read from %s, want the private dir", last.File)
	}
}

func TestPrivateConfigDirWinsForAFileInBoth(t *testing.T) {
	defer SetPrivateConfigDir("")()
	private := t.TempDir()
	body, err := os.ReadFile(filepath.Join(publicConfigDir, "icon-sets-flag.json"))
	if err != nil {
		t.Fatal(err)
	}
	overridden := strings.Replace(string(body), `"current_version": "`, `"current_version": "0.0.0-private`, 1)
	if overridden == string(body) {
		t.Fatal("fixture edit did not apply")
	}
	if err := os.WriteFile(filepath.Join(private, "icon-sets-flag.json"), []byte(overridden), 0o644); err != nil {
		t.Fatal(err)
	}

	SetPrivateConfigDir(private)
	pack, err := LoadPack("icon-sets-flag", publicConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pack.CurrentVersion, "0.0.0-private") {
		t.Errorf("current_version = %q: the public copy was read", pack.CurrentVersion)
	}
	// A private directory without a registry adds no packs.
	reg, err := LoadRegistry(publicConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.Join(reg.Packs, ","), "icon-sets-flag"); n != 1 {
		t.Errorf("icon-sets-flag listed %d times", n)
	}
}

func TestAMissingPrivateConfigDirIsAnError(t *testing.T) {
	defer SetPrivateConfigDir("")()
	SetPrivateConfigDir(filepath.Join(t.TempDir(), "not-mounted"))
	if _, err := LoadRegistry(publicConfigDir); err == nil || !strings.Contains(err.Error(), "private config dir") {
		t.Fatalf("LoadRegistry = %v, want a private config dir error", err)
	}
	if _, err := LoadPack("icon-sets-flag", publicConfigDir); err == nil {
		t.Fatal("LoadPack succeeded with the private dir missing")
	}
}

func TestResolveFilePrefersThePrivateCopy(t *testing.T) {
	defer SetPrivateConfigDir("")()
	public := filepath.Join(publicConfigDir, "icon-sets-example.sets.json")
	if got := ResolveFile(publicConfigDir, "icon-sets-example.sets.json"); got != public {
		t.Errorf("no private dir: %s, want %s", got, public)
	}
	SetPrivateConfigDir(privateFixture)
	// A name that could leave the directory is never looked up in it.
	escaping := filepath.Join("..", "private-config", "icon-sets-example.sets.json")
	if got := ResolveFile(filepath.Join(privateFixture, "nested"), escaping); strings.HasPrefix(got, privateFixture+string(filepath.Separator)+"icon-sets") {
		t.Errorf("an escaping name resolved into the private dir: %s", got)
	}
	want := filepath.Join(privateFixture, "icon-sets-example.sets.json")
	if got := ResolveFile(publicConfigDir, "icon-sets-example.sets.json"); got != want {
		t.Errorf("private dir: %s, want %s", got, want)
	}
}
