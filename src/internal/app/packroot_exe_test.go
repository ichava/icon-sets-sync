package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
)

// A double-clicked binary runs with cwd=$HOME; the sibling checkouts are found
// from the executable's own directory instead.
func TestPackRootFromTheExecutable(t *testing.T) {
	base := t.TempDir()
	toolkit := mkdir(t, base, "icon-sets-sync")
	mkdir(t, toolkit, ".git")
	mkdir(t, toolkit, "src")
	mkdir(t, toolkit, "config")
	must(t, os.WriteFile(filepath.Join(toolkit, "config", "packs.json"), []byte(`{}`), 0o644))
	must(t, os.WriteFile(filepath.Join(toolkit, "src", "go.mod"), []byte("module x\n"), 0o644))
	mkdir(t, base, "icon-sets-tabler")
	home := t.TempDir()
	ci := filepath.Join(t.TempDir(), "work")

	for _, exe := range []string{filepath.Join(toolkit, "build"), filepath.Join(toolkit, "build", "dist"), filepath.Join(toolkit, "build", "macos"), filepath.Join(toolkit, "build", "windows")} {
		p := &config.PackConfig{Name: "icon-sets-tabler", PackRoot: filepath.Join(ci, "icon-sets-tabler")}
		applyWorkspaceIn(p, "", home, exe, ci)
		if want := filepath.Join(base, "icon-sets-tabler"); p.PackRoot != want {
			t.Errorf("exe in %s: PackRoot = %q, want %q", exe, p.PackRoot, want)
		}
	}

	// No checkout anywhere: say a sync needs a workspace.
	p := &config.PackConfig{Name: "icon-sets-flag", PackRoot: filepath.Join(ci, "icon-sets-flag")}
	applyWorkspaceIn(p, "", home, t.TempDir(), ci)
	if !strings.Contains(p.PackRootOrigin, "sync needs a workspace") || !strings.Contains(p.PackRootOrigin, "ICHAVA_WORKSPACE") {
		t.Errorf("origin = %q", p.PackRootOrigin)
	}
}
