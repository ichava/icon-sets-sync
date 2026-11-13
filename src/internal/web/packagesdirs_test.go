package web

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// The Settings list editor writes packages_dirs into the global config,
// keeping the keys already there, and the change takes effect at once.
func TestSettingsSavesPackagesDirsIntoTheGlobalConfig(t *testing.T) {
	s, eng := newTestServer(t)
	t.Cleanup(func() { app.SetPackagesDirs(nil) })
	global := filepath.Join(eng.cfg.GlobalHome, settings.AppDir, "config.json")
	if err := os.MkdirAll(filepath.Dir(global), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte(`{"concurrency": 2, "packages_dir": "/old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := t.TempDir()
	c := signIn(t, s)
	resp := c.post("/settings/packages-dirs", url.Values{"dir": {a, "  ", "/does/not/exist"}})
	if resp.Code != 303 {
		t.Fatalf("POST = %d:\n%s", resp.Code, resp.Body.String())
	}
	var tree map[string]any
	data, _ := os.ReadFile(global)
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tree["packages_dirs"], []any{a, "/does/not/exist"}) || tree["concurrency"] != float64(2) {
		t.Errorf("global config = %s", data)
	}
	if _, alias := tree["packages_dir"]; alias {
		t.Error("the singular alias was left beside the list")
	}
	if got := app.PackagesDirs(); !reflect.DeepEqual(got, []string{a, "/does/not/exist"}) {
		t.Errorf("not applied: %v", got)
	}
}

// Each entry says whether it exists.
func TestSettingsShowsWhetherEachPacksDirExists(t *testing.T) {
	a := t.TempDir()
	v := BuildSettingsView(settings.Stack{Global: settings.Values{"packages_dirs": []string{a, filepath.Join(a, "gone")}}}.Resolve(nil), "/c", "")
	if len(v.PackagesDirs) != 2 || !v.PackagesDirs[0].Exists || v.PackagesDirs[1].Exists {
		t.Fatalf("%+v", v.PackagesDirs)
	}
	s, _ := newTestServer(t)
	body := signIn(t, s).get("/settings").Body.String()
	for _, want := range []string{`action="/settings/packages-dirs"`, "data-list-add", "<template data-list-template>"} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page lacks %q", want)
		}
	}
}
