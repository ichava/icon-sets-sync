package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/toolpath"
)

// Tools reports what is missing, finds a tool outside PATH in the usual
// directories, and treats npm as optional.
func TestToolsReportsWhatIsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bits")
	}
	usual := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ICHAVA_NPM", "")
	old := toolpath.Dirs
	toolpath.Dirs = func() []string { return []string{usual} }
	t.Cleanup(func() { toolpath.Dirs = old })
	if err := os.WriteFile(filepath.Join(usual, "git"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tools := Tools()
	byName := map[string]Tool{}
	for _, tl := range tools {
		byName[tl.Name] = tl
	}
	if g := byName["git"]; !g.Found || g.OnPath {
		t.Errorf("git: %+v", g)
	}
	if g := byName["gh"]; g.Found || g.Detail == "" {
		t.Errorf("gh: %+v", g)
	}
	if n := byName["npm"]; n.Found || !n.Optional {
		t.Errorf("npm: %+v", n)
	}
	if m := MissingTools(tools); len(m) != 1 || m[0].Name != "gh" {
		t.Errorf("missing: %+v", m)
	}
}
