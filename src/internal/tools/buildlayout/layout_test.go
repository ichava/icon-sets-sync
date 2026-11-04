package buildlayout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tree(t *testing.T, files ...string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "build")
	for _, f := range files {
		p := filepath.Join(root, f)
		if strings.HasSuffix(f, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The layout `make build-local && make cross && make dist` writes.
var good = []string{
	"checksums.txt", ".DS_Store", "macos/.DS_Store",
	"macos/icon-sets-sync", "macos/icon-sets-sync_macos_arm64", "macos/icon-sets-sync_macos_universal",
	"linux/icon-sets-sync_linux_amd64", "linux/icon-sets-sync_linux_armv7",
	"windows/icon-sets-sync_windows_amd64.exe",
	"freebsd/icon-sets-sync_freebsd_amd64",
	"archives/checksums.txt", "archives/icon-sets-sync_linux_amd64.tar.gz",
}

func TestTheLayoutPasses(t *testing.T) {
	n, problems, err := Check(tree(t, good...))
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("a good layout failed: %v", problems)
	}
	// Five folders and two files at the root, plus the eleven files inside them.
	if n != len(good)+5 {
		t.Fatalf("inspected %d entries, want %d", n, len(good)+5)
	}
}

// Every shape the old layouts left behind, and the ones the rule names.
func TestEveryOtherShapeFails(t *testing.T) {
	cases := map[string]string{
		"build/dist/ (the 2026-08 layout)":        "dist/icon-sets-sync_linux_amd64",
		"a binary at the root of build/":          "icon-sets-sync",
		"a folder inside an OS folder":            "macos/icon-sets-sync_darwin_all/icon-sets-sync",
		"a folder inside archives/":               "archives/sboms/x.json",
		"a darwin folder":                         "darwin/icon-sets-sync_macos_arm64",
		"GoReleaser metadata at the root":         "artifacts.json",
		"another tool's output under build/":      "kind-suggestions/icon-sets-tabler.json",
		"an empty folder nobody named":            "tmp/",
		"a binary-named per-target folder in OS/": "linux/icon-sets-sync_linux_s390x/",
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			_, problems, err := Check(tree(t, append([]string{extra}, good...)...))
			if err != nil {
				t.Fatal(err)
			}
			if len(problems) != 1 {
				t.Fatalf("problems = %v, want exactly one for %s", problems, extra)
			}
		})
	}
}

func TestAMissingBuildDirIsAnError(t *testing.T) {
	if _, _, err := Check(filepath.Join(t.TempDir(), "build")); err == nil {
		t.Fatal("a missing build/ passed")
	}
}

func TestOSToken(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "macos", "linux": "linux", "windows": "windows"} {
		if got := OSToken(goos); got != want || !IsOSDir(got) {
			t.Errorf("OSToken(%s) = %s (IsOSDir %v), want %s", goos, got, IsOSDir(got), want)
		}
	}
}
