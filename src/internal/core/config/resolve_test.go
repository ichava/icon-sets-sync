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

// TestResolveConfigSourceLayers covers the flag, env and cwd layers. A miss
// on all of them falls through to the executable and embedded layers, which
// source_test.go covers; here it must only not claim the cwd.
func TestResolveConfigSourceLayers(t *testing.T) {
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
		{"the removed ICHAVA_DEV_CONFIG_DIR is not read", "", envOf("ICHAVA_DEV_CONFIG_DIR", "/old/dir"), root, "config"},
		{"cwd is the repo root", "", noEnv, root, "config"},
		{"cwd is src/", "", noEnv, filepath.Join(root, "src"), filepath.Join(root, "config")},
		{"cwd is src/cmd (two levels)", "", noEnv, nested, filepath.Join(root, "config")},
		{"not found is not the cwd", "", noEnv, empty, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, src := ResolveConfigSource(tc.explicit, tc.getenv, tc.cwd, "")
			if tc.want == "" {
				if src == SourceCwd || src == SourceEnv || src == SourceFlag {
					t.Errorf("ResolveConfigSource = %q from %s, want no flag, env or cwd answer", got, src)
				}
				return
			}
			if got != tc.want {
				t.Errorf("ResolveConfigSource = %q (%s), want %q", got, src, tc.want)
			}
		})
	}
}

func TestResolveConfigSourceStopsAtGitAndDepth(t *testing.T) {
	// A config/packs.json above a .git boundary must not be found.
	outer := t.TempDir()
	for _, d := range []string{"config", "src", "inner/.git", "inner/sub"} {
		if err := os.MkdirAll(filepath.Join(outer, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	must(t, os.WriteFile(filepath.Join(outer, "config", "packs.json"), []byte(`{}`), 0o644))
	must(t, os.WriteFile(filepath.Join(outer, "src", "go.mod"), []byte("module x\n"), 0o644))
	if got, src := ResolveConfigSource("", func(string) string { return "" }, filepath.Join(outer, "inner", "sub"), ""); src == SourceCwd {
		t.Errorf("walked past .git: got %q", got)
	}

	// More than 5 levels up is not searched.
	root := repoLayout(t)
	deep := filepath.Join(root, "a", "b", "c", "d", "e", "f")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, src := ResolveConfigSource("", func(string) string { return "" }, deep, ""); src == SourceCwd {
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
