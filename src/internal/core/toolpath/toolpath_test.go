package toolpath

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A tool missing from PATH is found in the usual directories.
func TestLookPathFallsBackToTheUsualDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bits")
	}
	usual := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	old := Dirs
	Dirs = func() []string { return []string{usual} }
	t.Cleanup(func() { Dirs = old })
	if _, _, err := LookPath("gh"); err == nil {
		t.Fatal("found gh in an empty world")
	}
	p := filepath.Join(usual, "gh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, onPath, err := LookPath("gh")
	if err != nil || got != p || onPath {
		t.Fatalf("%s %v %v", got, onPath, err)
	}
}

func TestNewestFirstOrdersNumerically(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"v9.1.0", "v22.3.0", "v18.20.1"} {
		if err := os.MkdirAll(filepath.Join(root, v, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := NewestFirst(filepath.Join(root, "*", "bin"))
	if len(got) != 3 || !strings.Contains(got[0], "v22.3.0") || !strings.Contains(got[2], "v9.1.0") {
		t.Fatalf("order %v", got)
	}
}
