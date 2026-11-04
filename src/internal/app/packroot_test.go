package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
)

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPackRootResolution(t *testing.T) {
	// A toolkit checkout at <base>/icon-sets-sync, in this repo's own layout.
	newBase := func(t *testing.T) (base, toolkit string) {
		base = t.TempDir()
		toolkit = mkdir(t, base, "icon-sets-sync")
		mkdir(t, toolkit, ".git")
		mkdir(t, toolkit, "config")
		mkdir(t, toolkit, "src")
		must(t, os.WriteFile(filepath.Join(toolkit, "config", "packs.json"), []byte(`{}`), 0o644))
		must(t, os.WriteFile(filepath.Join(toolkit, "src", "go.mod"), []byte("module x\n"), 0o644))
		return base, toolkit
	}

	cases := []struct {
		name       string
		setup      func(t *testing.T, base, toolkit, ci string) (workspace string)
		wantRoot   func(base, ci, workspace string) string
		wantOrigin string
	}{
		{"CI layout: /work/<pack> exists",
			func(t *testing.T, base, toolkit, ci string) string { mkdir(t, ci, "icon-sets-tabler"); return "" },
			func(base, ci, _ string) string { return filepath.Join(ci, "icon-sets-tabler") }, "pack config"},
		{"sibling layout: only ../<pack> exists",
			func(t *testing.T, base, toolkit, ci string) string { mkdir(t, base, "icon-sets-tabler"); return "" },
			func(base, ci, _ string) string { return filepath.Join(base, "icon-sets-tabler") }, "sibling checkout"},
		{"explicit workspace beats both",
			func(t *testing.T, base, toolkit, ci string) string {
				mkdir(t, ci, "icon-sets-tabler")
				mkdir(t, base, "icon-sets-tabler")
				return mkdir(t, base, "elsewhere", "x")
			},
			func(base, ci, ws string) string { return filepath.Join(ws, "icon-sets-tabler") }, "workspace"},
		{"neither exists: today's value, with the fix named",
			func(t *testing.T, base, toolkit, ci string) string { return "" },
			func(base, ci, _ string) string { return filepath.Join(ci, "icon-sets-tabler") }, "ICHAVA_WORKSPACE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base, toolkit := newBase(t)
			ci := filepath.Join(t.TempDir(), "work")
			mkdir(t, ci)
			ws := tc.setup(t, base, toolkit, ci)
			if ws != "" {
				mkdir(t, ws, "icon-sets-tabler")
			}
			p := &config.PackConfig{Name: "icon-sets-tabler", PackRoot: filepath.Join(ci, "icon-sets-tabler")}
			applyWorkspaceIn(p, ws, filepath.Join(toolkit, "src"), "", ci)
			if want := tc.wantRoot(base, ci, ws); p.PackRoot != want {
				t.Errorf("PackRoot = %q, want %q", p.PackRoot, want)
			}
			if !strings.Contains(p.PackRootOrigin, tc.wantOrigin) {
				t.Errorf("origin = %q, want it to contain %q", p.PackRootOrigin, tc.wantOrigin)
			}
		})
	}
}

func TestRelativePackRootStillUsesWorkspace(t *testing.T) {
	p := &config.PackConfig{Name: "x", PackRoot: "x"}
	applyWorkspaceIn(p, "/ws", "", "", "/work")
	if p.PackRoot != filepath.Join("/ws", "x") {
		t.Errorf("PackRoot = %q", p.PackRoot)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
