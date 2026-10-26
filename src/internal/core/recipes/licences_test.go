package recipes

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/bundled"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/sinks"
	"github.com/ichava/icon-sets-sync/src/internal/core/sources"
	"github.com/ichava/icon-sets-sync/src/internal/core/transforms"
)

// fakeDownloader serves one archive from memory, so the test needs no network.
type fakeDownloader struct{ body []byte }

func (f fakeDownloader) Download(_ context.Context, _, dest string, _ bool) (string, error) {
	return dest, os.WriteFile(dest, f.body, 0o644)
}

// archive builds the shape GitHub serves: everything under one top directory.
func archive(t *testing.T, kind string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	switch kind {
	case "zip":
		w := zip.NewWriter(&buf)
		for name, body := range files {
			f, err := w.Create("demo-1.0.0/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	case "tar.gz":
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for name, body := range files {
			hdr := &tar.Header{Name: "demo-1.0.0/" + name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

// TestUpstreamLicenceReachesThePack runs the real stages end to end over a
// fixture archive: the licence at the archive root survives the subset that
// narrows the tree to icons/, and lands verbatim beside them.
func TestUpstreamLicenceReachesThePack(t *testing.T) {
	const mit = "MIT License\r\n\r\nCopyright (c) Demo Authors\r\n"
	const notice = "Demo\nCopyright 2026 Example Org\n"

	cases := []struct {
		name    string
		kind    string
		files   map[string]string
		want    map[string]string
		wantErr string
	}{
		{
			name:  "zip, licence only",
			kind:  "zip",
			files: map[string]string{"LICENSE": mit, "icons/a.svg": "<svg/>", "README.md": "r"},
			want:  map[string]string{"LICENSE": mit},
		},
		{
			name:  "tar.gz, licence and NOTICE",
			kind:  "tar.gz",
			files: map[string]string{"LICENSE.txt": mit, "NOTICE": notice, "icons/a.svg": "<svg/>"},
			want:  map[string]string{"LICENSE.txt": mit, "NOTICE": notice},
		},
		{
			name:    "no licence at the root fails",
			kind:    "zip",
			files:   map[string]string{"icons/a.svg": "<svg/>", "icons/LICENSE": "nested, not the upstream's"},
			wantErr: "ships no LICENSE",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			packRoot := t.TempDir()
			p := pipeline.Named("demo@1.0.0").
				WorkingDir(t.TempDir()).
				Config(map[string]any{pipeline.ConfigPackRoot: packRoot}).
				Source(sources.GithubArchive{Owner: "o", Repo: "demo", Version: "1.0.0", HTTP: fakeDownloader{archive(t, c.kind, c.files)}}).
				Transform(transforms.FindLicences{}).
				Transform(transforms.SubsetTo{Subdir: "icons"}).
				Sink(sinks.Filesystem{Root: "{pack_root}/resources/assets/svg/files"}).
				Sink(sinks.LicenceTexts{Root: LicencesRoot + "/demo", Set: "demo"})

			res, err := p.Run()
			if err == nil {
				err = res.Err // a stage failure is reported on the result
			}
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			dir := filepath.Join(packRoot, "resources", "assets", "svg", "licenses", "demo")
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != len(c.want) {
				t.Fatalf("licences dir holds %d files, want %d", len(entries), len(c.want))
			}
			for name, body := range c.want {
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != body {
					t.Errorf("%s is not verbatim:\n got %q\nwant %q", name, got, body)
				}
			}
			if _, err := os.Stat(filepath.Join(packRoot, "resources", "assets", "svg", "files", "a.svg")); err != nil {
				t.Errorf("the icons did not land: %v", err)
			}
		})
	}
}

// assertLicenceWiring checks one asset pipeline: FindLicences straight after
// the source (before anything narrows the tree) and LicenceTexts straight
// after the Filesystem sink, naming the set.
func assertLicenceWiring(t *testing.T, p *pipeline.Pipeline, set string) {
	t.Helper()
	stages := p.Stages()
	names := stageNames(p)
	if len(names) < 2 || names[1] != "FindLicences" {
		t.Errorf("%s: FindLicences is not the first transform: %v", p.Name(), names)
	}
	fs := indexOf(names, "Filesystem")
	if fs < 0 || fs+1 >= len(names) || names[fs+1] != "LicenceTexts" {
		t.Errorf("%s: LicenceTexts does not follow Filesystem: %v", p.Name(), names)
		return
	}
	lt := stages[fs+1].(sinks.LicenceTexts)
	if lt.Set != set || !strings.HasSuffix(filepath.ToSlash(lt.Root), "/resources/assets/svg/licenses/"+set) {
		t.Errorf("%s: LicenceTexts{Root: %q, Set: %q}, want set %q", p.Name(), lt.Root, lt.Set, set)
	}
}

// TestEveryBundledSetShipsItsLicence builds the per-set recipe the way a
// private pack runs it: the pack config and its set manifest come from a
// private config directory merged over the public one, since a private pack's
// recipe may not ship in this repository. The fixture is synthetic. It asserts
// how many sets it inspected: a manifest that stopped loading, or a recipe that
// built nothing, must fail here rather than pass over zero pipelines.
func TestEveryBundledSetShipsItsLicence(t *testing.T) {
	publicDir := filepath.Join("..", "..", "..", "..", "config")
	privateDir := filepath.Join("..", "config", "testdata", "private-config")
	defer config.SetPrivateConfigDir(privateDir)()

	pack, err := config.LoadPack("icon-sets-example", publicDir)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := bundled.LoadManifest(publicDir, pack.Name)
	if err != nil {
		t.Fatal(err)
	}
	built, err := Build(pack, "1.0.0", Options{DryRun: true, ConfigDir: publicDir})
	if err != nil {
		t.Fatal(err)
	}

	sets := manifest.SetNames()
	if len(sets) != 2 {
		t.Fatalf("the fixture manifest lists %d sets, want 2", len(sets))
	}
	if len(built) != len(sets) {
		t.Fatalf("built %d pipelines for %d sets", len(built), len(sets))
	}
	missing := 0
	for i, set := range sets {
		assertLicenceWiring(t, built[i], set)
		lt := built[i].Stages()[indexOf(stageNames(built[i]), "LicenceTexts")].(sinks.LicenceTexts)
		if lt.MissingReason != manifest.Sets[set].LicenceMissing {
			t.Errorf("%s: MissingReason %q does not carry the manifest's licence_missing", set, lt.MissingReason)
		}
		if lt.MissingReason != "" {
			missing++
		}
	}
	if missing != 1 {
		t.Errorf("%d sets carry a licence_missing reason, want the fixture's 1", missing)
	}
	t.Logf("inspected %d sets", len(sets))
}

func TestEmojiAndSingleUpstreamPacksShipTheirLicences(t *testing.T) {
	emoji := &config.PackConfig{
		Name: "icon-sets-emoji", Pack: "ichava/icon-sets-emoji", PackRoot: "/work/icon-sets-emoji",
		VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
		Source: config.SourceConfig{Type: "url"},
	}
	built, err := buildFor(emoji, "15.0.0", true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"twemoji", "openmoji-color", "openmoji-black"}
	if len(built) != len(want) {
		t.Fatalf("got %d emoji pipelines, want %d", len(built), len(want))
	}
	for i, set := range want {
		assertLicenceWiring(t, built[i], set)
	}

	for pack, set := range map[string]string{"@tabler/icons": "tabler-icons", "flag-icons": "flag-icons"} {
		p := npmPack("x")
		p.Source.Package = pack
		built, err := buildFor(p, "1.0.0", false)
		if err != nil {
			t.Fatal(err)
		}
		assertLicenceWiring(t, built[0], set)
	}
}
