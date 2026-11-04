// Command flatdist copies GoReleaser's local output into the house layout.
//
// It is run by `make cross` and `make dist`, after a snapshot, never by a
// release: the release workflow uploads from GoReleaser's own dist.
//
//	make cross   # build/{os}/{binary}_{os}_{arch}[.exe] + build/checksums.txt
//	make dist    # the same, plus build/archives/ (archives, Linux packages,
//	             # the source tarball) with its own checksums.txt
//
// GoReleaser writes into its scratch dist (src/.goreleaser.yaml `dist:`,
// .cache/goreleaser), one folder per build target, and points its
// binary-format "archives" at the executables inside them. This reads
// artifacts.json, copies each bare binary into build/{os}/ (the OS token, so
// darwin is macos), writes build/checksums.txt with bare names, and removes
// the scratch dist. The names are GoReleaser's own (src/.goreleaser.yaml
// `archives`), so the naming scheme is kept in one place. Nothing of
// GoReleaser's own (per-target folders, artifacts.json, metadata.json,
// config.yaml) reaches build/.
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

	"github.com/ichava/icon-sets-sync/src/internal/tools/buildlayout"
)

func main() {
	var (
		root     string
		dist     string
		out      string
		archives bool
	)
	flag.StringVar(&root, "root", ".", "repository root, which artifacts.json paths are relative to")
	flag.StringVar(&dist, "dist", ".cache/goreleaser", "GoReleaser's scratch dist directory, relative to -root; removed afterwards")
	flag.StringVar(&out, "out", "build", "the build directory, relative to -root")
	flag.BoolVar(&archives, "archives", false, "also keep the archives and Linux packages, under <out>/archives")
	flag.Parse()

	n, a, err := flatten(root, dist, out, archives)
	if err != nil {
		fmt.Fprintln(os.Stderr, "flatdist:", err)
		os.Exit(1)
	}
	fmt.Printf("flatdist: %d binaries in %s/{os}/", n, out)
	if archives {
		fmt.Printf(", %d archives and packages in %s", a, filepath.Join(out, buildlayout.ArchivesDir))
	}
	fmt.Println()
}

// artifact is the part of an artifacts.json entry this reads.
type artifact struct {
	Name  string         `json:"name"`
	Path  string         `json:"path"`
	Type  string         `json:"type"`
	Goos  string         `json:"goos"`
	Extra map[string]any `json:"extra"`
}

// keptAsArchive is every artifact type `make dist` keeps under archives/.
var keptAsArchive = map[string]bool{"Archive": true, "Linux Package": true, "Source": true}

// flatten copies what GoReleaser left in dist into out, and returns how many
// binaries and archives it kept. The host build (build/{os}/icon-sets-sync)
// is left where it is; earlier cross binaries are replaced.
func flatten(root, dist, out string, withArchives bool) (int, int, error) {
	distAbs := filepath.Join(root, dist)
	outAbs := filepath.Join(root, out)
	raw, err := os.ReadFile(filepath.Join(distAbs, "artifacts.json"))
	if err != nil {
		return 0, 0, fmt.Errorf("%w (run goreleaser first)", err)
	}
	var all []artifact
	if err := json.Unmarshal(raw, &all); err != nil {
		return 0, 0, fmt.Errorf("artifacts.json: %w", err)
	}

	type kept struct{ name, path string }
	var bins, arcs []kept
	seen := map[string]bool{}
	for _, a := range all {
		isBin := a.Type == "Binary" && a.Extra["Format"] == "binary"
		isArc := withArchives && keptAsArchive[a.Type]
		if !isBin && !isArc {
			continue
		}
		if seen[a.Name] {
			return 0, 0, fmt.Errorf("%s: two artifacts share this name", a.Name)
		}
		seen[a.Name] = true
		if isBin {
			osDir := buildlayout.OSToken(goosOf(a))
			if !buildlayout.IsOSDir(osDir) {
				return 0, 0, fmt.Errorf("%s: cannot tell its OS (goos %q)", a.Name, a.Goos)
			}
			bins = append(bins, kept{a.Name, filepath.Join(osDir, a.Name)})
		} else {
			arcs = append(arcs, kept{a.Name, filepath.Join(buildlayout.ArchivesDir, a.Name)})
		}
	}
	if len(bins) == 0 {
		return 0, 0, fmt.Errorf("artifacts.json names no binary-format archive; check src/.goreleaser.yaml's `archives`")
	}

	if err := removeCrossBinaries(outAbs); err != nil {
		return 0, 0, err
	}
	if withArchives {
		if err := os.RemoveAll(filepath.Join(outAbs, buildlayout.ArchivesDir)); err != nil {
			return 0, 0, err
		}
	}
	src := map[string]string{}
	for _, a := range all {
		src[a.Name] = filepath.Join(root, a.Path)
	}
	for _, k := range append(append([]kept{}, bins...), arcs...) {
		dest := filepath.Join(outAbs, k.path)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return 0, 0, err
		}
		if err := copyFile(src[k.name], dest); err != nil {
			return 0, 0, err
		}
	}

	paths := func(ks []kept) map[string]string {
		m := map[string]string{}
		for _, k := range ks {
			m[k.name] = filepath.Join(outAbs, k.path)
		}
		return m
	}
	if err := writeChecksums(filepath.Join(outAbs, "checksums.txt"), paths(bins)); err != nil {
		return 0, 0, err
	}
	if len(arcs) > 0 {
		if err := writeChecksums(filepath.Join(outAbs, buildlayout.ArchivesDir, "checksums.txt"), paths(arcs)); err != nil {
			return 0, 0, err
		}
	}
	if err := os.RemoveAll(distAbs); err != nil {
		return 0, 0, err
	}
	if _, problems, err := buildlayout.Check(outAbs); err != nil {
		return 0, 0, err
	} else if len(problems) > 0 {
		return 0, 0, fmt.Errorf("%s breaks the layout: %s", out, strings.Join(problems, "; "))
	}
	return len(bins), len(arcs), nil
}

// goosOf is the artifact's GOOS, read from its name when artifacts.json
// leaves it out: {binary}_{os}_{arch}, where the OS is already the token.
func goosOf(a artifact) string {
	if a.Goos != "" {
		return a.Goos
	}
	rest := strings.TrimPrefix(a.Name, buildlayout.Binary+"_")
	goos, _, _ := strings.Cut(rest, "_")
	return goos
}

// removeCrossBinaries clears the {binary}_* files a previous `make cross`
// left in the OS folders, so a target dropped from the config does not linger.
// The host build, named plain {binary}, stays.
func removeCrossBinaries(out string) error {
	entries, err := os.ReadDir(out)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || !buildlayout.IsOSDir(e.Name()) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(out, e.Name()))
		if err != nil {
			return err
		}
		for _, f := range files {
			if !f.IsDir() && strings.HasPrefix(f.Name(), buildlayout.Binary+"_") {
				if err := os.Remove(filepath.Join(out, e.Name(), f.Name())); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// copyFile copies src to dest, replacing dest. The mode travels, so a binary
// stays executable.
func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return err
	}
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// writeChecksums writes file over the named files: bare filenames, sorted, in
// the `sha256sum -c` format. A name is unique across OS folders because it
// carries its OS, so the bare name is enough to find it.
func writeChecksums(file string, files map[string]string) error {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		f, err := os.Open(files[n])
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
	return os.WriteFile(file, []byte(b.String()), 0o644)
}
