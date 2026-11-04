package buildlayout

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// tree writes the files (each body is its own path, so every hash differs)
// and a checksums.txt over the cross binaries, as flatdist does. A listed
// "checksums.txt" is replaced by that generated one.
func tree(t *testing.T, files ...string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "build")
	var sums []string
	for _, f := range files {
		if f == "checksums.txt" {
			continue
		}
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
		if err := os.WriteFile(p, []byte(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if dir, name, ok := strings.Cut(f, "/"); ok && IsOSDir(dir) && !strings.Contains(name, "/") && strings.HasPrefix(name, Binary+"_") {
			h := sha256.Sum256([]byte(f))
			sums = append(sums, hex.EncodeToString(h[:])+"  "+f+"\n")
		}
	}
	sort.Strings(sums)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "checksums.txt"), []byte(strings.Join(sums, "")), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// The checksum file lists exactly the cross binaries present, and verifies.
func TestChecksumsMustMatchTheBinaries(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		"one stale line": func(t *testing.T, root string) {
			p := filepath.Join(root, "linux", "icon-sets-sync_linux_amd64")
			if err := os.WriteFile(p, []byte("rebuilt"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"a binary nobody listed": func(t *testing.T, root string) {
			p := filepath.Join(root, "linux", "icon-sets-sync_linux_riscv64")
			if err := os.WriteFile(p, []byte("new"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"a line for a binary that is gone": func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "freebsd", "icon-sets-sync_freebsd_amd64")); err != nil {
				t.Fatal(err)
			}
		},
		"a bare name, as the release file has": func(t *testing.T, root string) {
			p := filepath.Join(root, "checksums.txt")
			b, _ := os.ReadFile(p)
			if err := os.WriteFile(p, []byte(strings.Replace(string(b), "  macos/", "  ", 1)), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"no checksums.txt beside cross binaries": func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "checksums.txt")); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			root := tree(t, good...)
			breakIt(t, root)
			_, problems, err := Check(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(problems) == 0 {
				t.Fatal("a broken checksums.txt passed")
			}
		})
	}
}

// build-local alone: the host build, no cross binaries, no checksums.txt.
func TestHostBuildNeedsNoChecksums(t *testing.T) {
	root := tree(t, "macos/icon-sets-sync")
	if err := os.Remove(filepath.Join(root, "checksums.txt")); err != nil {
		t.Fatal(err)
	}
	if _, problems, err := Check(root); err != nil || len(problems) != 0 {
		t.Fatalf("problems = %v, err = %v", problems, err)
	}
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
