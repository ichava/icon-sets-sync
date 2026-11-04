// Command flatdist trims GoReleaser's local output to the house layout.
//
// It is run by `make cross` and `make dist`, after a snapshot, never by a
// release: the release workflow uploads from GoReleaser's own dist.
//
//	make cross   # build/dist/{binary}_{os}_{arch}[.exe] + checksums.txt, nothing else
//	make dist    # the same, plus build/dist/archives/ (archives, Linux packages,
//	             # the source tarball) with its own checksums.txt
//
// GoReleaser keeps one folder per build target and points its binary-format
// "archives" at the executables inside them, so a snapshot leaves dozens of
// per-target folders and metadata files in build/dist and no bare binary at
// its root. This reads artifacts.json, moves each named artifact to where the
// layout wants it, writes the checksum files with bare names, and removes
// everything else. The names are GoReleaser's own (src/.goreleaser.yaml
// `archives`), so the naming scheme is kept in one place.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	var (
		root     string
		dist     string
		archives bool
	)
	flag.StringVar(&root, "root", ".", "repository root, which artifacts.json paths are relative to")
	flag.StringVar(&dist, "dist", "build/dist", "GoReleaser's dist directory, relative to -root")
	flag.BoolVar(&archives, "archives", false, "also keep the archives and Linux packages, under <dist>/archives")
	flag.Parse()

	n, a, err := flatten(root, dist, archives)
	if err != nil {
		fmt.Fprintln(os.Stderr, "flatdist:", err)
		os.Exit(1)
	}
	fmt.Printf("flatdist: %d binaries in %s", n, filepath.Join(dist))
	if archives {
		fmt.Printf(", %d archives and packages in %s", a, filepath.Join(dist, "archives"))
	}
	fmt.Println()
}

// artifact is the part of an artifacts.json entry this reads.
type artifact struct {
	Name  string         `json:"name"`
	Path  string         `json:"path"`
	Type  string         `json:"type"`
	Extra map[string]any `json:"extra"`
}

// keptAsArchive is every artifact type `make dist` keeps under archives/.
var keptAsArchive = map[string]bool{"Archive": true, "Linux Package": true, "Source": true}

// flatten rebuilds dist from what GoReleaser left in it, and returns how many
// binaries and archives it kept. The old tree is moved aside first, because a
// bare binary's name (icon-sets-sync_linux_s390x) can be the name of the
// per-target folder it is read from.
func flatten(root, dist string, withArchives bool) (int, int, error) {
	distAbs := filepath.Join(root, dist)
	raw, err := os.ReadFile(filepath.Join(distAbs, "artifacts.json"))
	if err != nil {
		return 0, 0, fmt.Errorf("%w (run goreleaser first)", err)
	}
	var all []artifact
	if err := json.Unmarshal(raw, &all); err != nil {
		return 0, 0, fmt.Errorf("artifacts.json: %w", err)
	}

	work := distAbs + ".flatdist"
	if err := os.RemoveAll(work); err != nil {
		return 0, 0, err
	}
	if err := os.Rename(distAbs, work); err != nil {
		return 0, 0, err
	}
	defer func() { _ = os.RemoveAll(work) }()
	if err := os.MkdirAll(distAbs, 0o755); err != nil {
		return 0, 0, err
	}
	// artifacts.json paths name the dist directory as it was before the move.
	from := func(p string) string {
		rel, err := filepath.Rel(filepath.Clean(dist), filepath.Clean(p))
		if err != nil || strings.HasPrefix(rel, "..") {
			return filepath.Join(root, p)
		}
		return filepath.Join(work, rel)
	}

	var bins, arcs []string
	archDir := filepath.Join(distAbs, "archives")
	for _, a := range all {
		switch {
		case a.Type == "Binary" && a.Extra["Format"] == "binary":
			if err := moveFile(from(a.Path), filepath.Join(distAbs, a.Name)); err != nil {
				return 0, 0, err
			}
			bins = append(bins, a.Name)
		case withArchives && keptAsArchive[a.Type]:
			if err := os.MkdirAll(archDir, 0o755); err != nil {
				return 0, 0, err
			}
			if err := moveFile(from(a.Path), filepath.Join(archDir, a.Name)); err != nil {
				return 0, 0, err
			}
			arcs = append(arcs, a.Name)
		}
	}
	if len(bins) == 0 {
		return 0, 0, fmt.Errorf("artifacts.json names no binary-format archive; check src/.goreleaser.yaml's `archives`")
	}
	if err := writeChecksums(distAbs, bins); err != nil {
		return 0, 0, err
	}
	if len(arcs) > 0 {
		if err := writeChecksums(archDir, arcs); err != nil {
			return 0, 0, err
		}
	}
	return len(bins), len(arcs), nil
}

// moveFile renames, and copies when a rename cannot (another volume). The
// mode travels, so a binary stays executable.
func moveFile(src, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s: two artifacts share this name", filepath.Base(dest))
	}
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// writeChecksums writes dir/checksums.txt over names: bare filenames, sorted,
// in the `sha256sum -c` format, so it verifies from inside dir.
func writeChecksums(dir string, names []string) error {
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		f, err := os.Open(filepath.Join(dir, n))
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		_ = f.Close()
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(h.Sum(nil)), n)
	}
	return os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(b.String()), 0o644)
}
