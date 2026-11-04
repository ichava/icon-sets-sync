// Package buildlayout holds the local build directory to the estate layout:
// build/ with one folder per OS and nothing else.
//
//	build/
//	  targets.txt, nfpm.yaml   tracked inputs, when a repository has them
//	  checksums.txt            bare-name SHA-256 over every cross binary
//	  macos/ linux/ windows/ freebsd/ …   the binaries, flat
//	  archives/                only after `make dist`, flat
//
// No build/dist/, no binary at the root of build/, and no folder inside an OS
// folder or inside archives/. `make build-local`, `make cross` and `make dist`
// run Check after they write, through ./internal/tools/checkbuild.
package buildlayout

import (
	"fmt"
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
// dir is an error: there is nothing to vouch for.
func Check(dir string) (int, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, nil, err
	}
	var problems []string
	seen := 0
	for _, e := range entries {
		seen++
		name := e.Name()
		switch {
		case e.IsDir() && (osDirs[name] || name == ArchivesDir):
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
	return seen, problems, nil
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
