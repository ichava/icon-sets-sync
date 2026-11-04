// Package defaultconfig is the copy of the repository's config/ built into
// the binary, used when it runs outside any checkout (a standalone binary,
// double-clicked from Downloads). It is read-only: the web UI refuses to save
// into it and points at --config-dir instead.
//
// The copy is generated, never edited: change config/, then run
// `make embed-config`. TestEmbeddedConfigMatchesTheRepository fails while the
// two differ.
package defaultconfig

//go:generate go run ../tools/embedconfig -root ../../..

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed config
var files embed.FS

// FS is the embedded config tree, rooted at the config directory.
func FS() fs.FS {
	sub, err := fs.Sub(files, "config")
	if err != nil {
		panic(err) // the directory is embedded at build time
	}
	return sub
}

// Files lists the embedded files, slash-separated and sorted.
func Files() []string {
	var out []string
	_ = fs.WalkDir(FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return err
	})
	sort.Strings(out)
	return out
}

// Dir writes the embedded configs to a per-user cache directory and returns
// it, so every reader that takes a directory works unchanged. The directory is
// named for a hash of the contents and its files are mode 0444; an existing
// copy is checked byte for byte and rewritten where it differs. base is the
// cache root; "" means $XDG_CACHE_HOME, else ~/.cache.
func Dir(base string) (string, error) {
	if base == "" {
		base = os.Getenv("XDG_CACHE_HOME")
	}
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	names := Files()
	h := sha256.New()
	for _, n := range names {
		b, _ := fs.ReadFile(FS(), n)
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", n, len(b))
		h.Write(b)
	}
	dir := filepath.Join(base, "icon-sets-sync", "embedded-config-"+hex.EncodeToString(h.Sum(nil))[:12])
	for _, n := range names {
		want, _ := fs.ReadFile(FS(), n)
		dest := filepath.Join(dir, filepath.FromSlash(n))
		if got, err := os.ReadFile(dest); err == nil && bytes.Equal(got, want) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return "", err
		}
		_ = os.Chmod(dest, 0o600) // a stale read-only copy
		if err := os.WriteFile(dest, want, 0o400); err != nil {
			return "", err
		}
		if err := os.Chmod(dest, 0o444); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// repoFiles lists the files Sync copies from a config directory: everything
// but dotfiles (.DS_Store and the like), slash-separated and sorted.
func repoFiles(src string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && p != src {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}

// Sync makes dest an exact copy of src's files, removing any it no longer
// has, and returns how many it holds.
func Sync(src, dest string) (int, error) {
	names, err := repoFiles(src)
	if err != nil {
		return 0, err
	}
	if len(names) == 0 {
		return 0, fmt.Errorf("%s holds no config files", src)
	}
	if err := os.RemoveAll(dest); err != nil {
		return 0, err
	}
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(n)))
		if err != nil {
			return 0, err
		}
		out := filepath.Join(dest, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return 0, err
		}
		if err := os.WriteFile(out, b, 0o644); err != nil {
			return 0, err
		}
	}
	return len(names), nil
}

// Diff lists how src differs from the embedded copy: files missing from
// either side, and files whose bytes differ. Empty means in step.
func Diff(src string) ([]string, error) {
	names, err := repoFiles(src)
	if err != nil {
		return nil, err
	}
	embedded := map[string]bool{}
	for _, n := range Files() {
		embedded[n] = true
	}
	var out []string
	for _, n := range names {
		if !embedded[n] {
			out = append(out, "not embedded: "+n)
			continue
		}
		delete(embedded, n)
		want, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(n)))
		if err != nil {
			return nil, err
		}
		got, _ := fs.ReadFile(FS(), n)
		if !bytes.Equal(got, want) {
			out = append(out, "differs: "+n)
		}
	}
	for n := range embedded {
		out = append(out, "embedded but gone from config/: "+n)
	}
	sort.Strings(out)
	return out, nil
}
