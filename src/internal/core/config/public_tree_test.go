package config

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// privatePathIn matches what may not appear in any tracked file of this
// public repository: a file path inside, or a config file of, a pack that is
// private. Their recipes live in that pack's own repository and are loaded at
// run time through a private config directory (see private.go).
//
// Why these and not a plain word match:
//
//   - The private aggregator pack's slug is also an ordinary English word plus
//     a prefix, and "bundled" is the name of a generic recipe and Go package
//     here, and plain prose ("bootstrap is bundled inside tabler.min.js"). A
//     word match would forbid the language, not the leak. So the slug is only
//     a finding where it is a path -- followed by a separator or a config
//     extension, or preceded by a separator -- which is the shape every leaked
//     path list and config filename has. A bare slug names the pack; it does
//     not describe what is in it.
//   - The other private pack's name has no generic meaning at all, so any
//     occurrence is a finding.
//   - Both packs' pre-rename slugs are covered, because dated measurements
//     recorded paths under the old names.
//
// One exception to "preceded by a separator": the composer name
// (ichava/<slug>, also the GitHub repository) is the pack's identity, not a
// path into it, so a slug preceded by exactly `ichava/` and not followed by a
// separator is allowed.
//
// The terms are assembled at run time so this file does not match itself.
var (
	privateSlug  = `(?:icon-sets-` + `bundled|bundled` + `-icons)`
	privateWord  = regexp.MustCompile(`(?i)metro` + `nic`)
	privateAfter = regexp.MustCompile(`(?i)` + privateSlug + `(?:[/\\]|\.(?:sets\.)?(?:json|ya?ml)\b)`)
	privateUnder = regexp.MustCompile(`(?i)([A-Za-z0-9_.-]*)[/\\]` + privateSlug + `\b`)
)

// privatePathIn returns the first private path in line, or "".
func privatePathIn(line string) string {
	if m := privateWord.FindString(line); m != "" {
		return m
	}
	if m := privateAfter.FindString(line); m != "" {
		return m
	}
	for _, loc := range privateUnder.FindAllStringSubmatchIndex(line, -1) {
		if strings.EqualFold(line[loc[2]:loc[3]], "ichava") {
			continue // the composer name; privateAfter already caught ichava/<slug>/...
		}
		return line[loc[0]:loc[1]]
	}
	return ""
}

// privatePathExemptions are files allowed to keep matching lines, each with a
// ceiling and the reason, so an entry is a limit rather than a licence: a new
// match pushes the file over its ceiling and fails, and a ceiling above what
// the file holds fails too, so a fixed file cannot keep an exemption it no
// longer needs.
var privatePathExemptions = map[string]struct {
	ceiling int
	reason  string
}{
	// None. The vendored SVG policy held 3 matching `_comment` lines until
	// ichava/core reworded them; its copy here now has 0 and needs no entry.
}

// repoRoot is the repository root, four levels above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// trackedFiles lists every file git tracks, plus any untracked file that is
// not ignored, so a new file is caught before it is committed.
func trackedFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var files []string
	for _, f := range bytes.Split(out, []byte{0}) {
		if len(f) > 0 {
			files = append(files, string(f))
		}
	}
	return files
}

// findPrivatePaths reports every line in files under root that matches a
// private-path pattern, and every file whose own path does.
func findPrivatePaths(t *testing.T, root string, files []string) []string {
	t.Helper()
	var findings []string
	for _, rel := range files {
		if privatePathIn("/"+rel) != "" {
			findings = append(findings, rel+": the file's own path names a private pack")
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			if os.IsNotExist(err) {
				continue // deleted in the working tree, not yet staged
			}
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if m := privatePathIn(line); m != "" {
				findings = append(findings, rel+":"+strconv.Itoa(i+1)+": "+m)
			}
		}
	}
	return findings
}

// TestPublicTreeNamesNoPrivatePack fails when any tracked file names a path in,
// or a config file of, a private pack.
func TestPublicTreeNamesNoPrivatePack(t *testing.T) {
	root := repoRoot(t)
	files := trackedFiles(t, root)
	// Assert the size of what was inspected: a git call that listed nothing
	// would otherwise report a clean tree.
	const floor = 200
	if len(files) < floor {
		t.Fatalf("inspected %d files, below the floor of %d", len(files), floor)
	}
	perFile := map[string][]string{}
	for _, f := range findPrivatePaths(t, root, files) {
		file, _, _ := strings.Cut(f, ":")
		perFile[file] = append(perFile[file], f)
	}
	for file, ex := range privatePathExemptions {
		if n := len(perFile[file]); n > ex.ceiling {
			t.Errorf("%s: %d matching lines, above its exemption ceiling of %d (%s)", file, n, ex.ceiling, ex.reason)
		} else if n < ex.ceiling {
			t.Errorf("%s: %d matching lines, below its exemption ceiling of %d -- lower the ceiling", file, n, ex.ceiling)
		}
		delete(perFile, file)
	}
	for _, findings := range perFile {
		for _, f := range findings {
			t.Error(f)
		}
	}
	t.Logf("inspected %d files", len(files))
}

// TestPrivatePathGuardCanFail proves the matcher reports each shape it exists
// for, and passes the generic words it must allow.
func TestPrivatePathGuardCanFail(t *testing.T) {
	dir := t.TempDir()
	bad := map[string]string{
		"a.md": "see resources/" + "metro" + "nic-icons/flags",
		"b.go": `PackRoot: "/work/icon-sets-` + `bundled"`,
		"c.md": "read config/icon-sets-" + "bundled.sets.json",
		"d.js": `"bundled` + `-icons/resources/assets/svg/files/x.svg"`,
		"h.md": "git clone ichava/icon-sets-" + "bundled/resources",
	}
	good := map[string]string{
		"e.md": "bootstrap is bundled inside tabler.min.js; the bundled recipe runs one pipeline per set",
		"f.go": `case pack.Name == "icon-sets-` + `bundled":`,
		"g.go": `Pack: "ichava/icon-sets-` + `bundled", // composer name`,
	}
	var names []string
	for _, set := range []map[string]string{bad, good} {
		for name, body := range set {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
	}
	got := strings.Join(findPrivatePaths(t, dir, names), "\n")
	for name := range bad {
		if !strings.Contains(got, name+":") {
			t.Errorf("%s was not reported:\n%s", name, got)
		}
	}
	for name := range good {
		if strings.Contains(got, name+":") {
			t.Errorf("%s was reported, but it names no private path:\n%s", name, got)
		}
	}
}

// privateWords are the names of the packs that are private. Written split so
// this file does not itself carry what TestPublicTreeNamesNoPrivatePack
// forbids.
var privateWords = []string{"metro" + "nic", "icon-sets-" + "bundled", "bundled" + "-icons"}

// TestPublicConfigShipsNoPrivatePack pins what a public build carries: the
// config/ directory is copied into the image verbatim, so a private pack's
// config or set manifest there would be published with every release.
func TestPublicConfigShipsNoPrivatePack(t *testing.T) {
	defer SetPrivateConfigDir("")()
	entries, err := os.ReadDir(publicConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	inspected := 0
	for _, e := range entries {
		inspected++
		for _, w := range privateWords {
			if strings.Contains(strings.ToLower(e.Name()), w) {
				t.Errorf("config/%s ships with the public build", e.Name())
			}
		}
	}
	packs, err := LoadAll(publicConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range packs {
		for _, w := range privateWords {
			if strings.Contains(strings.ToLower(p.Name+" "+p.Pack), w) {
				t.Errorf("the public registry lists %s (%s)", p.Name, p.Pack)
			}
		}
	}
	// Assert the size of what was inspected: a config dir that moved would
	// otherwise pass by reading nothing.
	if inspected < 4 || len(packs) < 3 {
		t.Fatalf("inspected %d entries and %d packs; the public config dir did not load", inspected, len(packs))
	}
}
