package sinks

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

func writeSVGs(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestFilesystemReportsTheDiff is the per-pack diff the web UI and the CLI
// show: which icons a refresh adds, removes and changes, counted before the
// wipe so a dry run reports it too.
func TestFilesystemReportsTheDiff(t *testing.T) {
	target := writeSVGs(t, filepath.Join(t.TempDir(), "dest"), map[string]string{
		"kept.svg": "<svg/>", "edited.svg": "<svg>old</svg>", "gone.svg": "<svg/>", "sub/also-gone.svg": "<svg/>",
	})
	source := writeSVGs(t, filepath.Join(t.TempDir(), "src"), map[string]string{
		"kept.svg": "<svg/>", "edited.svg": "<svg>new</svg>", "new.svg": "<svg/>",
	})
	ctx := ctxFor(t, source)
	if err := (Filesystem{Root: target}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	m, _ := ctx.Metrics["filesystem"].(map[string]any)
	got := map[string]any{"added": m["added"], "removed": m["removed"], "changed": m["changed"]}
	want := map[string]any{"added": 1, "removed": 2, "changed": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diff counts = %v, want %v", got, want)
	}
	if m["copied"] != 3 {
		t.Errorf("copied = %v, want 3 (the existing metric is unchanged)", m["copied"])
	}
	if !reflect.DeepEqual(m["added_names"], []string{"new.svg"}) ||
		!reflect.DeepEqual(m["removed_names"], []string{"gone.svg", "sub/also-gone.svg"}) ||
		!reflect.DeepEqual(m["changed_names"], []string{"edited.svg"}) {
		t.Errorf("names: added %v removed %v changed %v", m["added_names"], m["removed_names"], m["changed_names"])
	}
}

// An incremental sink deletes nothing, so it must not claim removals.
func TestFilesystemIncrementalReportsNoRemovals(t *testing.T) {
	target := writeSVGs(t, filepath.Join(t.TempDir(), "dest"), map[string]string{"old.svg": "<svg/>"})
	source := writeSVGs(t, filepath.Join(t.TempDir(), "src"), map[string]string{"new.svg": "<svg/>"})
	ctx := ctxFor(t, source)
	if err := (Filesystem{Root: target, Incremental: true}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	m, _ := ctx.Metrics["filesystem"].(map[string]any)
	if m["added"] != 1 || m["removed"] != 0 {
		t.Fatalf("added %v removed %v, want 1 and 0", m["added"], m["removed"])
	}
}

func TestFilesystemDiffNamesAreCapped(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < maxDiffNames+5; i++ {
		files[filepath.Join("d", string(rune('a'+i/26))+string(rune('a'+i%26))+".svg")] = "<svg/>"
	}
	source := writeSVGs(t, filepath.Join(t.TempDir(), "src"), files)
	ctx := ctxFor(t, source)
	if err := (Filesystem{Root: filepath.Join(t.TempDir(), "dest")}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	m, _ := ctx.Metrics["filesystem"].(map[string]any)
	if m["added"] != maxDiffNames+5 {
		t.Errorf("added = %v, want the full count", m["added"])
	}
	names, _ := m["added_names"].([]string)
	if n := len(names); n != maxDiffNames {
		t.Errorf("%d names kept, want the cap %d", n, maxDiffNames)
	}
}

// The CLI and the web UI tell a relicensed upstream apart from any other
// failure through this sentinel, so CI can open an issue instead of a PR.
func TestALicenceChangeIsRecognisable(t *testing.T) {
	src := t.TempDir()
	p := filepath.Join(src, "LICENSE")
	if err := os.WriteFile(p, []byte("Attribution 4.0 International\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := ctxFor(t, src)
	ctx.SetString(pipeline.KeyLicenceFiles, p)
	err := LicenceTexts{Root: filepath.Join(t.TempDir(), "lic"), Set: "x", Licence: "CC-BY-SA-4.0"}.Execute(ctx)
	if !errors.Is(err, ErrLicenceChanged) {
		t.Fatalf("err = %v, want it to wrap ErrLicenceChanged", err)
	}
	// An unknown SPDX id is a config mistake, not a licence change.
	err = LicenceTexts{Root: filepath.Join(t.TempDir(), "lic"), Set: "x", Licence: "WTFPL-9"}.Execute(ctx)
	if err == nil || errors.Is(err, ErrLicenceChanged) {
		t.Fatalf("an unknown id must fail without claiming a licence change: %v", err)
	}
}
