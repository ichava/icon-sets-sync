package web

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// A form's dry run is the flag layer, so neither the environment nor a pack's
// own settings block can turn it into a real commit.
func TestWithRunFlagsOutranksEveryOtherLayer(t *testing.T) {
	stack := settings.Stack{
		Env:   settings.Values{"dry_run": false, "concurrency": 4},
		Flags: settings.Values{"git.base": "develop"},
	}
	got, err := WithRunFlags(stack, RunRequest{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	packLayer := settings.Values{"dry_run": false}
	r := got.Resolve(packLayer)
	if !r.Bool("dry_run") || r.Origin("dry_run") != settings.LayerFlag {
		t.Fatalf("dry_run %v from %s", r.Bool("dry_run"), r.Origin("dry_run"))
	}
	if r.String("git.base") != "develop" || r.Int("concurrency") != 4 {
		t.Error("the existing layers were lost")
	}
	if _, set := stack.Flags["dry_run"]; set {
		t.Error("WithRunFlags mutated the server's own stack")
	}
	if _, err := WithRunFlags(stack, RunRequest{Concurrency: -1}); err == nil {
		t.Error("a negative concurrency was accepted")
	}
}

func TestAppEngineReadsPacksAndNotes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("packs.json", `{"packs": ["icon-sets-demo"]}`)
	write("icon-sets-demo.json", `{
  "_note": "read me",
  "_status": {"since": "2026"},
  "name": "icon-sets-demo", "pack": "ichava/icon-sets-demo", "pack_root": "/work/icon-sets-demo",
  "current_version": "1.0.0", "version_check_url": "https://registry.npmjs.org/demo/latest",
  "source": {"type": "npm", "package": "demo"}, "sinks": [],
  "settings": {"git": {"base": "release"}}
}`)
	e := &AppEngine{ConfigDir: dir}
	p, err := e.Pack("icon-sets-demo")
	if err != nil {
		t.Fatal(err)
	}
	notes := e.PackNotes(p)
	if notes["_note"] != "read me" || notes["_status"] != `{"since": "2026"}` || len(notes) != 2 {
		t.Errorf("notes %v", notes)
	}
	v, err := e.PackSettings(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range v.Rows {
		if row.Name == "git.base" && (row.Value != "release" || row.Origin != "pack") {
			t.Errorf("git.base row %+v", row)
		}
	}
	if _, err := e.Pack("icon-sets-missing"); err == nil {
		t.Error("an unregistered pack loaded")
	}
}
