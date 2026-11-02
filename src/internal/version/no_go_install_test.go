package version

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// goInstallOfThisModule matches a `go install` of this repository's module.
// The Go module proxy cached src/v0.1.0 to src/v0.1.2 from commits before the
// single-commit publication, and it keeps the first copy of a tag for good, so
// `go install …@v0.1.0` serves that older code and `@latest` the older v0.1.2.
// The docs offer the signed release binaries and the Composer package instead.
var goInstallOfThisModule = regexp.MustCompile(`go\s+install\s+github\.com/ichava/icon-sets-sync`)

// minGoInstallDocFiles is the number of files the guard read on 2026-11-01
// (README.md, CONTRIBUTING.md and the 9 tracked files under docs/). A walk that stops matching
// must fail rather than pass over nothing.
const minGoInstallDocFiles = 11

func TestDocsOfferNoGoInstall(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	files := []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "CONTRIBUTING.md"),
	}
	err := filepath.WalkDir(filepath.Join(root, "docs"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip dotfiles such as a local .DS_Store, so the count matches a clean checkout.
		if !d.IsDir() && !strings.HasPrefix(d.Name(), ".") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking docs/: %v", err)
	}

	read := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		read++
		if m := goInstallOfThisModule.Find(data); m != nil {
			t.Errorf("%s offers %q; go install serves the proxy's cached pre-publication code, offer the release binaries or Composer", f, m)
		}
	}
	if read < minGoInstallDocFiles {
		t.Fatalf("read %d files, want at least %d: the guard is not looking where it should", read, minGoInstallDocFiles)
	}
}
