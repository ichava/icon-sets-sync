package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/kinds"
)

// kindPack makes a pack checkout holding one illustration-like file and one
// glyph, and returns its config.
func kindPack(t *testing.T, name string) *config.PackConfig {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	files := filepath.Join(root, "resources", "assets", "svg", "files", "scenes")
	if err := os.MkdirAll(files, 0o755); err != nil {
		t.Fatal(err)
	}

	var scene strings.Builder
	scene.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1200 800">`)
	for i := 0; i < 9; i++ {
		fmt.Fprintf(&scene, `<rect fill="#%d%d%d%d%d%d" width="1" height="1"/>`, i, i, i, i, i, i)
	}
	scene.WriteString(`<!--` + strings.Repeat("x", 21_000) + `-->`)
	scene.WriteString(`</svg>`)

	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(files, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("hero.svg", scene.String())
	write("dot.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M0 0"/></svg>`)

	return &config.PackConfig{Name: name, Pack: "ichava/" + name, PackRoot: root}
}

func TestSuggestKindsWritesOneFilePerPack(t *testing.T) {
	out := filepath.Join(t.TempDir(), "build", "kind-suggestions")
	packs := []*config.PackConfig{kindPack(t, "icon-sets-one"), kindPack(t, "icon-sets-two")}

	reports := SuggestKinds(packs, kinds.ModeDefault, out)
	if len(reports) != 2 {
		t.Fatalf("reports = %d, want 2", len(reports))
	}
	for i, r := range reports {
		if r.Err != nil {
			t.Fatalf("%s: %v", r.Pack, r.Err)
		}
		if r.Pack != packs[i].Name || r.Items != 2 || r.Records != 1 {
			t.Errorf("report = %+v", r)
		}
		want := filepath.Join(out, packs[i].Name+".json")
		if r.File != want {
			t.Errorf("file = %q, want %q", r.File, want)
		}
		data, err := os.ReadFile(want)
		if err != nil {
			t.Fatal(err)
		}
		var recs []kinds.Record
		if err := json.Unmarshal(data, &recs); err != nil || len(recs) != 1 {
			t.Fatalf("%s: %v %s", want, err, data)
		}
		if recs[0].Package != packs[i].Pack || recs[0].Path != "scenes/hero" {
			t.Errorf("record = %+v", recs[0])
		}
	}
}

func TestSuggestKindsReportsAFailureAndKeepsGoing(t *testing.T) {
	out := t.TempDir()
	missing := &config.PackConfig{Name: "icon-sets-missing", Pack: "ichava/icon-sets-missing", PackRoot: filepath.Join(t.TempDir(), "nope")}
	good := kindPack(t, "icon-sets-good")

	reports := SuggestKinds([]*config.PackConfig{missing, good}, kinds.ModeDefault, out)
	if len(reports) != 2 || reports[0].Err == nil || reports[1].Err != nil {
		t.Fatalf("reports = %+v", reports)
	}
	if _, err := os.Stat(filepath.Join(out, "icon-sets-missing.json")); err == nil {
		t.Error("a failed scan left a file behind")
	}
	if _, err := os.Stat(filepath.Join(out, "icon-sets-good.json")); err != nil {
		t.Error("the good pack was not written after the bad one failed")
	}
}

func TestSuggestKindsRefusesAnUnsafePackName(t *testing.T) {
	out := t.TempDir()
	p := kindPack(t, "icon-sets-x")
	for _, name := range []string{"../escape", "a/b", "", ".", ".."} {
		p.Name = name
		r := SuggestKinds([]*config.PackConfig{p}, kinds.ModeDefault, out)
		if len(r) != 1 || r[0].Err == nil {
			t.Errorf("name %q: report %+v, want an error", name, r)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(out))
	for _, e := range entries {
		if e.Name() == "escape.json" {
			t.Error("a pack name escaped the output directory")
		}
	}
}
