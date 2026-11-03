package version

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// personalContact matches a personal maintainer name or mailbox. Nothing this
// repository ships may carry one (decided 2026-10-28): the maintainer is
// "Simtabi LLC <ichava@simtabi.com>", and the sync bot commits as the
// github-actions bot's noreply address, not a personal GitHub login.
var personalContact = regexp.MustCompile(`(?i)imani@|manyara|imanimanyara`)

// minPersonalContactFiles is the number of files the guard read on
// 2026-10-30 (.goreleaser.yaml, README.md, Dockerfile and 9 under docs/).
// A walk that stops matching must fail rather than pass over nothing.
const minPersonalContactFiles = 12

func TestNoPersonalContactShips(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	files := []string{
		filepath.Join(root, "src", ".goreleaser.yaml"),
		filepath.Join(root, "README.md"),
		filepath.Join(root, "Dockerfile"),
	}
	// The Go sources too: the bot's default commit author lives in code
	// (gitx, settings), and that is where a personal login sat until 2026-10-30.
	err := filepath.WalkDir(filepath.Join(root, "src", "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".go" && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking src/internal: %v", err)
	}
	err = filepath.WalkDir(filepath.Join(root, "docs"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
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
		if m := personalContact.Find(data); m != nil {
			t.Errorf("%s carries a personal name or address (%q); use Simtabi LLC <ichava@simtabi.com>", f, m)
		}
	}
	if read < minPersonalContactFiles {
		t.Fatalf("read %d files, want at least %d: the guard is not looking where it should", read, minPersonalContactFiles)
	}
}
