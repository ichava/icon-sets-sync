// Package buildlayout holds the local build directory to the estate layout:
// build/ with one folder per OS and nothing else.
//
//	build/
//	  targets.txt, nfpm.yaml   tracked inputs, when a repository has them
//	  checksums.txt            SHA-256 over every cross binary, as macos/{binary}_macos_arm64
//	  macos/ linux/ windows/ freebsd/ …   the binaries, flat
//	  archives/                only after `make dist`, flat
//	  icon-sets-sync           the tracked Composer launcher (PHP), not a build output
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

// ComposerLauncher is the PHP script composer.json registers as the package's
// command, so Composer installs it as vendor/bin/icon-sets-sync. It is tracked
// in build/ rather than in a bin/ folder (owner, 2026-11-13: one folder, and it
// is build/). It is allowed at the root only while it is that script, so a
// compiled binary written there by mistake still fails.
const ComposerLauncher = Binary

// composerShebang is the first line the Composer launcher starts with.
const composerShebang = "#!/usr/bin/env php\n"

// isComposerLauncher reports whether path is the PHP launcher: it starts with
// the php shebang, which no compiled binary does.
func isComposerLauncher(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, len(composerShebang))
	if _, err := io.ReadFull(f, head); err != nil {
		return false
	}
	return string(head) == composerShebang
}

// rootFiles are the only other plain files build/ may hold.
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

// AppBundle is the macOS app `make app` writes into build/macos/.
const AppBundle = Binary + ".app"

// WindowsLauncherArchs are the Windows targets the launcher is built for:
// the same as the binary's Windows builds in src/.goreleaser.yaml.
var WindowsLauncherArchs = []string{"amd64", "arm64", "386"}

// WindowsLauncher is the launcher's file name for a Windows arch, beside
// {binary}_windows_{arch}.exe.
func WindowsLauncher(arch string) string { return Binary + "-launcher_windows_" + arch + ".exe" }

// universalBinary is the cross binary the bundle carries.
const universalBinary = "macos/" + Binary + "_macos_universal"

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
	var bundleSum string
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
			if name == "macos" {
				n, sum, sub, err := checkBundle(filepath.Join(dir, name, AppBundle))
				if err != nil {
					return seen, nil, err
				}
				seen += n
				bundleSum = sum
				problems = append(problems, sub...)
			}
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
		case !e.IsDir() && name == ComposerLauncher && isComposerLauncher(filepath.Join(dir, name)):
		default:
			problems = append(problems, fmt.Sprintf("build/%s: only targets.txt, nfpm.yaml, checksums.txt and the Composer launcher (a PHP script) may sit at the root of build/", name))
		}
	}
	sums, listed, err := checkChecksums(dir, bins)
	if err != nil {
		return seen, nil, err
	}
	problems = append(problems, sums...)
	if bundleSum != "" {
		switch uni, ok := listed[universalBinary]; {
		case !ok:
			problems = append(problems, fmt.Sprintf("build/macos/%s: no %s in build/checksums.txt to vouch for its binary; run make cross, then make app", AppBundle, universalBinary))
		case uni != bundleSum:
			problems = append(problems, fmt.Sprintf("build/macos/%s: its binary is not %s; run make app again", AppBundle, universalBinary))
		}
	}
	return seen, problems, nil
}

// checkBundle holds build/macos/icon-sets-sync.app to the shape `make app`
// writes: Contents/Info.plist, Contents/Resources/, and Contents/MacOS/
// holding exactly the executable. It returns the entries it inspected and
// the binary's SHA-256, or "" when there is no bundle or no binary.
func checkBundle(app string) (int, string, []string, error) {
	if _, err := os.Stat(app); os.IsNotExist(err) {
		return 0, "", nil, nil
	}
	label := "build/macos/" + AppBundle
	seen := 0
	var problems []string
	err := filepath.WalkDir(app, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != app {
			seen++
		}
		return nil
	})
	if err != nil {
		return seen, "", nil, err
	}
	if fi, err := os.Stat(filepath.Join(app, "Contents", "Info.plist")); err != nil || !fi.Mode().IsRegular() {
		problems = append(problems, label+"/Contents/Info.plist is missing")
	}
	macos := filepath.Join(app, "Contents", "MacOS")
	entries, err := os.ReadDir(macos)
	if err != nil && !os.IsNotExist(err) {
		return seen, "", nil, err
	}
	for _, e := range entries {
		if e.Name() != Binary {
			problems = append(problems, fmt.Sprintf("%s/Contents/MacOS/%s: only %s belongs here", label, e.Name(), Binary))
		}
	}
	exe := filepath.Join(macos, Binary)
	fi, err := os.Stat(exe)
	switch {
	case err != nil:
		return seen, "", append(problems, label+"/Contents/MacOS/"+Binary+" is missing"), nil
	case !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0:
		return seen, "", append(problems, label+"/Contents/MacOS/"+Binary+" is not an executable file"), nil
	}
	sum, err := sha256File(exe)
	if err != nil {
		return seen, "", nil, err
	}
	return seen, sum, problems, nil
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
//
// It also returns the hash of every line that verified, by name.
func checkChecksums(dir string, bins []string) ([]string, map[string]string, error) {
	verified := map[string]string{}
	f, err := os.Open(filepath.Join(dir, "checksums.txt"))
	if os.IsNotExist(err) {
		if len(bins) > 0 {
			return []string{fmt.Sprintf("build/checksums.txt is missing, and %d cross binaries need it", len(bins))}, verified, nil
		}
		return nil, verified, nil
	} else if err != nil {
		return nil, nil, err
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
			return nil, nil, err
		}
		if got != sum {
			problems = append(problems, fmt.Sprintf("build/checksums.txt:%d: %s does not verify", line, name))
			continue
		}
		verified[name] = sum
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	sort.Strings(bins)
	for _, b := range bins {
		if !listed[b] {
			problems = append(problems, fmt.Sprintf("build/%s: not listed in build/checksums.txt", b))
		}
	}
	return problems, verified, nil
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

// launcherFiles are the files beside the binaries that are not binaries: the
// double-click launchers `make cross` and `make app` write. Each is a named
// exception with its reason, because the estate rule says an OS folder holds
// binaries only, and no recorded owner decision covers these yet (measured
// 2026-11-05: one .app bundle in macos/, one .desktop and one .png in linux/,
// in this repository and no other). They are listed for the owner. The Windows
// launchers joined on 2026-11-12, by the owner's choice of option A.
var launcherFiles = map[string]map[string]string{
	"macos": {AppBundle: "macOS opens a bare executable inside Terminal; the app bundle is the double-click launcher"},
	"linux": {
		Binary + ".desktop": "the desktop entry a Linux file manager double-clicks",
		Binary + ".png":     "the icon that desktop entry names",
	},
	"windows": windowsLauncherFiles(),
}

func windowsLauncherFiles() map[string]string {
	files := map[string]string{}
	for _, arch := range WindowsLauncherArchs {
		files[WindowsLauncher(arch)] = "the GUI-subsystem launcher Explorer double-clicks with no console window (owner, option A, 2026-11-11)"
	}
	return files
}

// checkFlat holds an OS folder to its binaries: {binary}_{os}_{arch}[.exe],
// the host build {binary}[.exe], and the named launcherFiles. archives/ may
// hold any file. Neither may hold a folder, except the app bundle in macos/,
// which checkBundle inspects.
func checkFlat(dir, label string) (int, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, nil, err
	}
	var problems []string
	for _, e := range entries {
		name := e.Name()
		if _, ok := launcherFiles[label][name]; ok && e.IsDir() == (name == AppBundle) {
			continue
		}
		if e.IsDir() {
			problems = append(problems, fmt.Sprintf("build/%s/%s/: %s/ holds files only, never a folder", label, name, label))
			continue
		}
		if label == ArchivesDir || osMetadata[name] || isBinaryFor(label, name) {
			continue
		}
		problems = append(problems, fmt.Sprintf("build/%s/%s: an OS folder holds %s_%s_<arch> binaries, the host %s, and the named launchers only", label, name, Binary, label, Binary))
	}
	return len(entries), problems, nil
}

// isBinaryFor reports whether name is this program's binary for the OS folder label.
func isBinaryFor(label, name string) bool {
	base := strings.TrimSuffix(name, ".exe")
	if base == Binary {
		return true
	}
	arch, ok := strings.CutPrefix(base, Binary+"_"+label+"_")
	return ok && arch != "" && !strings.ContainsAny(arch, "_/.")
}

func allowedDirs() string {
	names := make([]string, 0, len(osDirs)+1)
	for n := range osDirs {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(append(names, ArchivesDir), ", ")
}
