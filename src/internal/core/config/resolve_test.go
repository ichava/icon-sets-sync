package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoLayout builds this repository's own shape under a temp dir: a .git
// directory, src/go.mod and config/packs.json. It returns the repo root.
func repoLayout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{".git", "src/cmd", "config"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{
		"src/go.mod":        "module example.invalid/src\n",
		"config/packs.json": `{"packs": []}`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestResolveConfigDir(t *testing.T) {
	root := repoLayout(t)
	empty := t.TempDir()
	noEnv := func(string) string { return "" }
	envOf := func(k, v string) func(string) string {
		return func(name string) string {
			if name == k {
				return v
			}
			return ""
		}
	}
	nested := filepath.Join(root, "src", "cmd")

	cases := []struct {
		name     string
		explicit string
		getenv   func(string) string
		cwd      string
		want     string
	}{
		{"explicit flag wins over env and layout", "/flag/dir", envOf("ICHAVA_CONFIG_DIR", "/env/dir"), root, "/flag/dir"},
		{"env wins over the layout", "", envOf("ICHAVA_CONFIG_DIR", "/env/dir"), root, "/env/dir"},
		{"older env name still honoured", "", envOf("ICHAVA_DEV_CONFIG_DIR", "/old/dir"), root, "/old/dir"},
		{"cwd is the repo root", "", noEnv, root, "config"},
		{"cwd is src/", "", noEnv, filepath.Join(root, "src"), filepath.Join(root, "config")},
		{"cwd is src/cmd (two levels)", "", noEnv, nested, filepath.Join(root, "config")},
		{"not found keeps the relative default", "", noEnv, empty, "config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveConfigDir(tc.explicit, tc.getenv, tc.cwd, "")
			if got != tc.want {
				t.Errorf("ResolveConfigDir = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveConfigDirStopsAtGitAndDepth(t *testing.T) {
	// A config/packs.json above a .git boundary must not be found.
	outer := t.TempDir()
	for _, d := range []string{"config", "src", "inner/.git", "inner/sub"} {
		if err := os.MkdirAll(filepath.Join(outer, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	must(t, os.WriteFile(filepath.Join(outer, "config", "packs.json"), []byte(`{}`), 0o644))
	must(t, os.WriteFile(filepath.Join(outer, "src", "go.mod"), []byte("module x\n"), 0o644))
	got := ResolveConfigDir("", func(string) string { return "" }, filepath.Join(outer, "inner", "sub"), "")
	if got != "config" {
		t.Errorf("walked past .git: got %q", got)
	}

	// More than 5 levels up is not searched.
	root := repoLayout(t)
	deep := filepath.Join(root, "a", "b", "c", "d", "e", "f")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ResolveConfigDir("", func(string) string { return "" }, deep, ""); got != "config" {
		t.Errorf("walked more than 5 levels: got %q", got)
	}
}

func TestMissingRegistryNamesTheFix(t *testing.T) {
	_, err := LoadRegistry(t.TempDir())
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"missing pack registry", "ICHAVA_CONFIG_DIR", "--config-dir"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
