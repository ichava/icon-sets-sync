package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/defaultconfig"
)

// Finder starts a double-clicked binary with the working directory at $HOME,
// so the walk from the working directory never reaches the checkout. These
// are the layers ResolveConfigSource adds, and the ones it keeps.
func TestResolveConfigSource(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := repoLayout(t)
	home := t.TempDir()
	outside := t.TempDir()
	besideExe := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(besideExe, "config"), 0o755))
	for _, d := range []string{"build/dist"} {
		must(t, os.MkdirAll(filepath.Join(root, d), 0o755))
	}
	noEnv := func(string) string { return "" }
	env := func(k string) string {
		if k == "ICHAVA_CONFIG_DIR" {
			return "/env/dir"
		}
		return ""
	}

	cases := []struct {
		name               string
		explicit           string
		getenv             func(string) string
		cwd, exeDir        string
		wantDir, wantSrc   string
		wantEmbeddedConfig bool
	}{
		{"flag", "/flag/dir", env, root, root, "/flag/dir", SourceFlag, false},
		{"env", "", env, root, root, "/env/dir", SourceEnv, false},
		{"cwd inside the checkout", "", noEnv, filepath.Join(root, "src"), outside, filepath.Join(root, "config"), SourceCwd, false},
		{"config/ beside the executable", "", noEnv, home, besideExe, filepath.Join(besideExe, "config"), SourceExecutable, false},
		{"cwd=$HOME, build/icon-sets-sync", "", noEnv, home, filepath.Join(root, "build"), filepath.Join(root, "config"), SourceExecutable, false},
		{"cwd=$HOME, build/dist/icon-sets-sync_macos_universal", "", noEnv, home, filepath.Join(root, "build", "dist"), filepath.Join(root, "config"), SourceExecutable, false},
		{"cwd=$HOME, a standalone binary", "", noEnv, home, outside, "", SourceEmbedded, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, src := ResolveConfigSource(c.explicit, c.getenv, c.cwd, c.exeDir)
			if src != c.wantSrc {
				t.Fatalf("source = %q (dir %s), want %q", src, dir, c.wantSrc)
			}
			if c.wantEmbeddedConfig {
				if d, err := defaultconfig.Diff(dir); err != nil || len(d) != 0 {
					t.Fatalf("%s is not the embedded configs: %v %v", dir, d, err)
				}
				if _, err := LoadRegistry(dir); err != nil {
					t.Fatalf("the embedded registry does not load: %v", err)
				}
				return
			}
			if dir != c.wantDir {
				t.Fatalf("dir = %s, want %s", dir, c.wantDir)
			}
		})
	}
}
