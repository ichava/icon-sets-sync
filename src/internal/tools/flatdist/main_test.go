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

const scratch = ".cache/goreleaser"

// goreleaserTree lays out a scratch dist the way a GoReleaser snapshot does:
// one folder per target, a binary-format "archive" pointing into it, a folder
// sharing a bare binary's name, and the archives and packages beside them.
// The checkout already holds a host build and a stale cross binary.
func goreleaserTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dist := filepath.Join(root, scratch)
	files := map[string]string{
		"icon-sets-sync_linux_amd64_v1/icon-sets-sync":       "linux-amd64",
		"icon-sets-sync_linux_s390x/icon-sets-sync":          "linux-s390x",
		"icon-sets-sync_darwin_all/icon-sets-sync":           "universal",
		"icon-sets-sync_windows_amd64_v1/icon-sets-sync.exe": "windows",
		"icon-sets-sync_freebsd_arm64_v8.0/icon-sets-sync":   "freebsd-arm64",
		"icon-sets-sync_linux_amd64.tar.gz":                  "tgz",
		"icon-sets-sync_linux_amd64.deb":                     "deb",
		"icon-sets-sync_0.1.0-SNAPSHOT_source.tar.gz":        "src",
		"config.yaml":   "cfg",
		"metadata.json": "{}",
		"checksums.txt": "stale",
	}
	for rel, body := range files {
		write(t, filepath.Join(dist, rel), body)
	}
	write(t, filepath.Join(root, "build", "macos", "icon-sets-sync"), "host")
	write(t, filepath.Join(root, "build", "linux", "icon-sets-sync_linux_mips"), "dropped target")
	bin := func(name, goos, path string) map[string]any {
		return map[string]any{"name": name, "path": scratch + "/" + path, "type": "Binary", "goos": goos,
			"extra": map[string]any{"Format": "binary", "ID": "binary"}}
	}
	arts := []map[string]any{
		// The build artifacts themselves, which are not what ships.
		{"name": "icon-sets-sync", "path": scratch + "/icon-sets-sync_linux_amd64_v1/icon-sets-sync", "type": "Binary",
			"goos": "linux", "extra": map[string]any{"ID": "icon-sets-sync"}},
		bin("icon-sets-sync_linux_amd64", "linux", "icon-sets-sync_linux_amd64_v1/icon-sets-sync"),
		bin("icon-sets-sync_linux_s390x", "linux", "icon-sets-sync_linux_s390x/icon-sets-sync"),
		bin("icon-sets-sync_macos_universal", "darwin", "icon-sets-sync_darwin_all/icon-sets-sync"),
		bin("icon-sets-sync_windows_amd64.exe", "windows", "icon-sets-sync_windows_amd64_v1/icon-sets-sync.exe"),
		// No goos recorded: read from the name.
		bin("icon-sets-sync_freebsd_arm64", "", "icon-sets-sync_freebsd_arm64_v8.0/icon-sets-sync"),
		{"name": "icon-sets-sync_linux_amd64.tar.gz", "path": scratch + "/icon-sets-sync_linux_amd64.tar.gz", "type": "Archive"},
		{"name": "icon-sets-sync_linux_amd64.deb", "path": scratch + "/icon-sets-sync_linux_amd64.deb", "type": "Linux Package"},
		{"name": "icon-sets-sync_0.1.0-SNAPSHOT_source.tar.gz", "path": scratch + "/icon-sets-sync_0.1.0-SNAPSHOT_source.tar.gz", "type": "Source"},
		{"name": "checksums.txt", "path": scratch + "/checksums.txt", "type": "Checksum"},
		{"name": "metadata.json", "path": scratch + "/metadata.json", "type": "Metadata"},
	}
	b, _ := json.Marshal(arts)
	write(t, filepath.Join(dist, "artifacts.json"), string(b))
	return root
}

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
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

// checkSums verifies a checksums.txt whose bare names live in the given
// folders, and that it covers exactly the {binary}_* files in them.
func checkSums(t *testing.T, file string, dirs ...string) {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	where := map[string]string{}
	for _, d := range dirs {
		for _, n := range listDir(t, d) {
			if strings.HasPrefix(n, "icon-sets-sync_") {
				where[n] = filepath.Join(d, n)
			}
		}
	}
	for _, line := range lines {
		sum, name, ok := strings.Cut(line, "  ")
		if !ok || strings.ContainsAny(name, "/\\") {
			t.Fatalf("not a bare-name checksum line: %q", line)
		}
		b, err := os.ReadFile(where[name])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != sum {
			t.Errorf("%s: checksum does not verify", name)
		}
	}
	if len(lines) != len(where) {
		t.Errorf("%s covers %d files, the folders hold %v", file, len(lines), where)
	}
}

// make cross: the bare binaries in their OS folders, one checksums.txt, and
// nothing of GoReleaser's left anywhere.
func TestCrossWritesOneFolderPerOS(t *testing.T) {
	root := goreleaserTree(t)
	n, a, err := flatten(root, scratch, "build", false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 || a != 0 {
		t.Fatalf("kept %d binaries and %d archives, want 5 and 0", n, a)
	}
	build := filepath.Join(root, "build")
	if got, want := listDir(t, build), []string{"checksums.txt", "freebsd/", "linux/", "macos/", "windows/"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("build = %v\nwant %v", got, want)
	}
	want := map[string][]string{
		"macos":   {"icon-sets-sync", "icon-sets-sync_macos_universal"},
		"linux":   {"icon-sets-sync_linux_amd64", "icon-sets-sync_linux_s390x"},
		"windows": {"icon-sets-sync_windows_amd64.exe"},
		"freebsd": {"icon-sets-sync_freebsd_arm64"},
	}
	for dir, files := range want {
		if got := listDir(t, filepath.Join(build, dir)); !reflect.DeepEqual(got, files) {
			t.Errorf("build/%s = %v, want %v", dir, got, files)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(build, "macos", "icon-sets-sync")); string(b) != "host" {
		t.Errorf("the host build was not left alone: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(build, "linux", "icon-sets-sync_linux_s390x")); string(b) != "linux-s390x" {
		t.Errorf("the binary sharing its folder's name was not kept: %q", b)
	}
	if fi, _ := os.Stat(filepath.Join(build, "linux", "icon-sets-sync_linux_amd64")); fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("binary lost its executable bit: %v", fi.Mode())
	}
	if _, err := os.Stat(filepath.Join(root, scratch)); !os.IsNotExist(err) {
		t.Errorf("the scratch dist was left behind: %v", err)
	}
	checkSums(t, filepath.Join(build, "checksums.txt"),
		filepath.Join(build, "macos"), filepath.Join(build, "linux"),
		filepath.Join(build, "windows"), filepath.Join(build, "freebsd"))
}

// make dist: the same, plus archives/ with its own checksums.
func TestDistGroupsTheArchives(t *testing.T) {
	root := goreleaserTree(t)
	n, a, err := flatten(root, scratch, "build", true)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 || a != 3 {
		t.Fatalf("kept %d binaries and %d archives, want 5 and 3", n, a)
	}
	build := filepath.Join(root, "build")
	if got, want := listDir(t, build), []string{"archives/", "checksums.txt", "freebsd/", "linux/", "macos/", "windows/"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("build = %v\nwant %v", got, want)
	}
	want := []string{"checksums.txt", "icon-sets-sync_0.1.0-SNAPSHOT_source.tar.gz", "icon-sets-sync_linux_amd64.deb", "icon-sets-sync_linux_amd64.tar.gz"}
	if got := listDir(t, filepath.Join(build, "archives")); !reflect.DeepEqual(got, want) {
		t.Fatalf("archives = %v\nwant %v", got, want)
	}
	checkSums(t, filepath.Join(build, "archives", "checksums.txt"), filepath.Join(build, "archives"))
}

// No binary-format archive means the config changed under this tool: fail
// rather than leave an empty build that looks like a clean one.
func TestFlattenRefusesAnEmptyResult(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, scratch, "artifacts.json"), "[]")
	if _, _, err := flatten(root, scratch, "build", false); err == nil {
		t.Fatal("flattened nothing and reported success")
	}
}
