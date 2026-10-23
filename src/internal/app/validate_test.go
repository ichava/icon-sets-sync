package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

func TestValidateConfigsTheShippedConfigs(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "config")
	findings := ValidateConfigs(dir, settings.Stack{})
	if n := Problems(findings); n != 0 {
		t.Fatalf("%d problems in the shipped configs: %+v", n, findings)
	}
	// Registry line plus at least one line per pack: assert the size of what
	// was inspected, so a registry that matched nothing cannot pass.
	text := ""
	for _, f := range findings {
		text += f.Text + "\n"
	}
	for _, slug := range []string{"icon-sets-tabler", "icon-sets-flag", "icon-sets-bundled", "icon-sets-emoji"} {
		if !strings.Contains(text, slug) {
			t.Errorf("no finding names %s:\n%s", slug, text)
		}
	}
	if len(findings) < 5 {
		t.Errorf("only %d findings", len(findings))
	}
}

func writePack(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "packs.json"), []byte(`{"packs": ["icon-sets-demo"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "icon-sets-demo.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const demoPack = `{
  "name": %q,
  "pack": "ichava/icon-sets-demo",
  "pack_root": "/work/icon-sets-demo",
  "current_version": "1.0.0",
  "version_check_url": "https://registry.npmjs.org/demo/latest",
  "source": {"type": "npm", "package": "demo"},
  "sinks": %s
}`

func TestValidatePackLevels(t *testing.T) {
	cases := map[string]struct {
		name, sinks string
		want        []Level
		contains    string
	}{
		"clean":          {"icon-sets-demo", `[{"type": "filesystem", "root": "x"}]`, []Level{LevelOK}, "(recipe generic)"},
		"no sinks warns": {"icon-sets-demo", `[]`, []Level{LevelWarn, LevelOK}, "declares no sinks"},
		"name mismatch":  {"icon-sets-other", `[]`, []Level{LevelError}, "does not match its registry slug"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writePack(t, dir, sprintf(demoPack, c.name, c.sinks))
			got := ValidatePack(dir, "icon-sets-demo", settings.Stack{})
			if len(got) != len(c.want) {
				t.Fatalf("findings %+v, want levels %v", got, c.want)
			}
			for i, f := range got {
				if f.Level != c.want[i] {
					t.Errorf("finding %d level %s, want %s", i, f.Level, c.want[i])
				}
			}
			if !strings.Contains(got[0].Text, c.contains) {
				t.Errorf("%q lacks %q", got[0].Text, c.contains)
			}
		})
	}
}

func TestValidateConfigsStopsAtAMissingRegistry(t *testing.T) {
	got := ValidateConfigs(t.TempDir(), settings.Stack{})
	if len(got) != 1 || got[0].Level != LevelError {
		t.Fatalf("got %+v", got)
	}
}
