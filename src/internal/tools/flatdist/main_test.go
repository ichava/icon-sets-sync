package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// goreleaserTree lays out a dist the way a GoReleaser snapshot does: one
// folder per target, a binary-format "archive" pointing into it, a folder
// sharing a bare binary's name, and the archives and packages beside them.
func goreleaserTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dist := filepath.Join(root, "build", "dist")
	files := map[string]string{
		"icon-sets-sync_linux_amd64_v1/icon-sets-sync":       "linux-amd64",
		"icon-sets-sync_linux_s390x/icon-sets-sync":          "linux-s390x",
		"icon-sets-sync_darwin_all/icon-sets-sync":           "universal",
		"icon-sets-sync_windows_amd64_v1/icon-sets-sync.exe": "windows",
		"icon-sets-sync_linux_amd64.tar.gz":                  "tgz",
		"icon-sets-sync_linux_amd64.deb":                     "deb",
		"icon-sets-sync_0.1.0-SNAPSHOT_source.tar.gz":        "src",
		"config.yaml":   "cfg",
		"metadata.json": "{}",
		"checksums.txt": "stale",
	}
	for rel, body := range files {
		p := filepath.Join(dist, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bin := func(name, path string) map[string]any {
		return map[string]any{"name": name, "path": "build/dist/" + path, "type": "Binary",
			"extra": map[string]any{"Format": "binary", "ID": "binary"}}
	}
	arts := []map[string]any{
		// The build artifacts themselves, which are not what ships.
		{"name": "icon-sets-sync", "path": "build/dist/icon-sets-sync_linux_amd64_v1/icon-sets-sync", "type": "Binary",
			"extra": map[string]any{"ID": "icon-sets-sync"}},
		bin("icon-sets-sync_linux_amd64", "icon-sets-sync_linux_amd64_v1/icon-sets-sync"),
		bin("icon-sets-sync_linux_s390x", "icon-sets-sync_linux_s390x/icon-sets-sync"),
		bin("icon-sets-sync_macos_universal", "icon-sets-sync_darwin_all/icon-sets-sync"),
		bin("icon-sets-sync_windows_amd64.exe", "icon-sets-sync_windows_amd64_v1/icon-sets-sync.exe"),
		{"name": "icon-sets-sync_linux_amd64.tar.gz", "path": "build/dist/icon-sets-sync_linux_amd64.tar.gz", "type": "Archive"},
		{"name": "icon-sets-sync_linux_amd64.deb", "path": "build/dist/icon-sets-sync_linux_amd64.deb", "type": "Linux Package"},
		{"name": "icon-sets-sync_0.1.0-SNAPSHOT_source.tar.gz", "path": "build/dist/icon-sets-sync_0.1.0-SNAPSHOT_source.tar.gz", "type": "Source"},
		{"name": "checksums.txt", "path": "build/dist/checksums.txt", "type": "Checksum"},
		{"name": "metadata.json", "path": "build/dist/metadata.json", "type": "Metadata"},
	}
	b, _ := json.Marshal(arts)
	if err := os.WriteFile(filepath.Join(dist, "artifacts.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func checkSums(t *testing.T, dir string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for _, line := range lines {
		sum, name, ok := strings.Cut(line, "  ")
		if !ok || strings.ContainsAny(name, "/\\") {
			t.Fatalf("not a bare-name checksum line: %q", line)
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != sum {
			t.Errorf("%s: checksum does not verify", name)
		}
	}
	if len(lines) != len(listDir(t, dir))-1-boolInt(dirHas(t, dir, "archives/")) {
		t.Errorf("checksums.txt covers %d files, the directory holds %v", len(lines), listDir(t, dir))
	}
}

func dirHas(t *testing.T, dir, name string) bool {
	for _, n := range listDir(t, dir) {
		if n == name {
			return true
		}
	}
	return false
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

var wantBins = []string{
	"checksums.txt",
	"icon-sets-sync_linux_amd64", "icon-sets-sync_linux_s390x",
	"icon-sets-sync_macos_universal", "icon-sets-sync_windows_amd64.exe",
}

// make cross: the bare binaries and one checksums.txt, no folders, no metadata.
func TestCrossKeepsOnlyTheBareBinaries(t *testing.T) {
	root := goreleaserTree(t)
	n, a, err := flatten(root, "build/dist", false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 || a != 0 {
		t.Fatalf("kept %d binaries and %d archives, want 4 and 0", n, a)
	}
	dist := filepath.Join(root, "build", "dist")
	if got := listDir(t, dist); !reflect.DeepEqual(got, wantBins) {
		t.Fatalf("build/dist = %v\nwant %v", got, wantBins)
	}
	if got := listDir(t, filepath.Join(root, "build")); !reflect.DeepEqual(got, []string{"dist/"}) {
		t.Errorf("left something beside dist: %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dist, "icon-sets-sync_linux_s390x")); string(b) != "linux-s390x" {
		t.Errorf("the binary sharing its folder's name was not kept: %q", b)
	}
	if fi, _ := os.Stat(filepath.Join(dist, "icon-sets-sync_linux_amd64")); fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("binary lost its executable bit: %v", fi.Mode())
	}
	checkSums(t, dist)
}

// make dist: the same root, plus archives/ with its own checksums.
func TestDistGroupsTheArchives(t *testing.T) {
	root := goreleaserTree(t)
	n, a, err := flatten(root, "build/dist", true)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 || a != 3 {
		t.Fatalf("kept %d binaries and %d archives, want 4 and 3", n, a)
	}
	dist := filepath.Join(root, "build", "dist")
	if got := listDir(t, dist); !reflect.DeepEqual(got, append(append([]string{}, "archives/"), wantBins...)) {
		t.Fatalf("build/dist = %v", got)
	}
	want := []string{"checksums.txt", "icon-sets-sync_0.1.0-SNAPSHOT_source.tar.gz", "icon-sets-sync_linux_amd64.deb", "icon-sets-sync_linux_amd64.tar.gz"}
	if got := listDir(t, filepath.Join(dist, "archives")); !reflect.DeepEqual(got, want) {
		t.Fatalf("archives = %v\nwant %v", got, want)
	}
	checkSums(t, dist)
	checkSums(t, filepath.Join(dist, "archives"))
}

// No binary-format archive means the config changed under this tool: fail
// rather than leave an empty dist that looks like a clean build.
func TestFlattenRefusesAnEmptyResult(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "build", "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "artifacts.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := flatten(root, "build/dist", false); err == nil {
		t.Fatal("flattened nothing and reported success")
	}
}
