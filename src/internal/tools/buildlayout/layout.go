// Package buildlayout holds the local build directory to the estate layout:
// build/ with one folder per OS and nothing else.
//
//	build/
//	  targets.txt, nfpm.yaml   tracked inputs, when a repository has them
//	  checksums.txt            SHA-256 over every cross binary, as macos/{binary}_macos_arm64
//	  macos/ linux/ windows/ freebsd/ …   the binaries, flat
//	  archives/                only after `make dist`, flat
//
// No build/dist/, no binary at the root of build/, and no folder inside an OS
// folder or inside archives/. `make build-local`, `make cross` and `make dist`
// run Check after they write, through ./internal/tools/checkbuild.
package buildlayout

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Binary is the one program this repository builds.
const Binary = "icon-sets-sync"

// rootFiles are the only plain files build/ may hold.
var rootFiles = map[string]bool{"targets.txt": true, "nfpm.yaml": true, "checksums.txt": true}

// osMetadata is what a file browser writes into any folder it opens: Finder's
// .DS_Store, Explorer's Thumbs.db and desktop.ini. Not build output, and
// already ignored by git, so it is passed over rather than reported.
var osMetadata = map[string]bool{".DS_Store": true, "Thumbs.db": true, "desktop.ini": true}

// osDirs are the OS folders: Go's GOOS names, with darwin written as macos.
// A folder named darwin is refused, because the folder is the artifact's OS
// token and the artifacts say macos.
var osDirs = map[string]bool{
	"macos": true, "linux": true, "windows": true, "freebsd": true, "openbsd": true,
	"netbsd": true, "dragonfly": true, "solaris": true, "illumos": true, "aix": true,
	"plan9": true, "android": true, "ios": true, "js": true, "wasip1": true,
}

// ArchivesDir is the folder `make dist` adds.
const ArchivesDir = "archives"

// OSToken is the folder and artifact name for a GOOS: darwin becomes macos.
func OSToken(goos string) string {
	if goos == "darwin" {
		return "macos"
	}
	return goos
}

// IsOSDir reports whether name is one of the OS folders.
func IsOSDir(name string) bool { return osDirs[name] }

// Check returns every way dir breaks the layout, and the number of entries it
// inspected, so a caller can tell an empty build/ from a clean one. A missing
// dir is an error: there is nothing to vouch for. It also holds
// build/checksums.txt to the cross binaries: one line per {binary}_* file in
// the OS folders, named by its path inside build/ (macos/icon-sets-sync_macos_arm64),
// each hash matching, so `cd build && shasum -a 256 -c checksums.txt` passes.
func Check(dir string) (int, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, nil, err
	}
	var problems, bins []string
	seen := 0
	for _, e := range entries {
		seen++
		name := e.Name()
		switch {
		case e.IsDir() && osDirs[name]:
			n, sub, err := checkFlat(filepath.Join(dir, name), name)
			if err != nil {
				return seen, nil, err
			}
			seen += n
			problems = append(problems, sub...)
			found, err := crossBinaries(dir, name)
			if err != nil {
				return seen, nil, err
			}
			bins = append(bins, found...)
		case e.IsDir() && name == ArchivesDir:
			n, sub, err := checkFlat(filepath.Join(dir, name), name)
			if err != nil {
				return seen, nil, err
			}
			seen += n
			problems = append(problems, sub...)
		case e.IsDir():
			problems = append(problems, fmt.Sprintf("build/%s/: not an OS folder or archives/ (allowed: %s)", name, allowedDirs()))
		case rootFiles[name], !e.IsDir() && osMetadata[name]:
		default:
			problems = append(problems, fmt.Sprintf("build/%s: only targets.txt, nfpm.yaml and checksums.txt may sit at the root of build/", name))
		}
	}
	sums, err := checkChecksums(dir, bins)
	if err != nil {
		return seen, nil, err
	}
	return seen, append(problems, sums...), nil
}

// crossBinaries lists the {binary}_* files in one OS folder, as paths inside
// build/. The host build, named plain {binary}, is not a cross binary.
func crossBinaries(dir, osDir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(dir, osDir))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), Binary+"_") {
			out = append(out, osDir+"/"+e.Name())
		}
	}
	return out, nil
}

// checkChecksums holds build/checksums.txt to exactly the cross binaries
// present, each hash verifying. No cross binaries and no checksums.txt (a
// build-local only) is fine; cross binaries without one is not.
func checkChecksums(dir string, bins []string) ([]string, error) {
	f, err := os.Open(filepath.Join(dir, "checksums.txt"))
	if os.IsNotExist(err) {
		if len(bins) > 0 {
			return []string{fmt.Sprintf("build/checksums.txt is missing, and %d cross binaries need it", len(bins))}, nil
		}
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var problems []string
	want := map[string]bool{}
	for _, b := range bins {
		want[b] = true
	}
	listed := map[string]bool{}
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		sum, name, ok := strings.Cut(sc.Text(), "  ")
		if !ok || len(sum) != 64 || strings.Count(name, "/") != 1 {
			problems = append(problems, fmt.Sprintf("build/checksums.txt:%d: want `<sha256>  <os>/<file>`, got %q", line, sc.Text()))
			continue
		}
		listed[name] = true
		if !want[name] {
			problems = append(problems, fmt.Sprintf("build/checksums.txt:%d: %s is not in build/", line, name))
			continue
		}
		got, err := sha256File(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		if got != sum {
			problems = append(problems, fmt.Sprintf("build/checksums.txt:%d: %s does not verify", line, name))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Strings(bins)
	for _, b := range bins {
		if !listed[b] {
			problems = append(problems, fmt.Sprintf("build/%s: not listed in build/checksums.txt", b))
		}
	}
	return problems, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checkFlat refuses any directory inside an OS folder or archives/.
func checkFlat(dir, label string) (int, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, nil, err
	}
	var problems []string
	for _, e := range entries {
		if e.IsDir() {
			problems = append(problems, fmt.Sprintf("build/%s/%s/: %s/ holds files only, never a folder", label, e.Name(), label))
		}
	}
	return len(entries), problems, nil
}

func allowedDirs() string {
	names := make([]string, 0, len(osDirs)+1)
	for n := range osDirs {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(append(names, ArchivesDir), ", ")
}
