package transforms

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// FindLicences records the upstream's own licence files, before anything
// narrows the tree.
//
// Upstreams keep LICENSE, NOTICE and the like at the root of the package or
// archive, while the icons sit in a subdirectory that SubsetTo or
// FlattenVariants then makes the fetched path. Run after either and the
// licence is already out of reach, which is how every pack came to ship its
// icons without the text MIT, ISC, Apache-2.0, BSD and OFL require to travel
// with them. So this runs first, looks only at the root, and hands the paths
// forward for the LicenceTexts sink. It reads nothing and changes nothing.
//
// It also records the untouched root as upstream_root, for the stages that
// read an upstream data file after the tree has been narrowed.
type FindLicences struct {
	pipeline.TransformKind

	// Dir is where the licence files sit, relative to the root, for an
	// upstream whose icons carry a licence of their own in a sub-directory.
	// Noto Emoji is the case: its root LICENSE is the OFL and covers the
	// fonts, while 2D/svg/LICENSE is the Apache-2.0 notice governing the
	// SVGs. Empty means the root.
	Dir string
}

func (FindLicences) Name() string { return "FindLicences" }

func (t FindLicences) Execute(ctx *pipeline.Context) error {
	root, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("FindLicences: fetched_path not set; needs a source upstream")
	}
	dir := root
	if t.Dir != "" {
		rel := filepath.Clean(filepath.FromSlash(t.Dir))
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("FindLicences: %q is outside the upstream", t.Dir)
		}
		dir = filepath.Join(root, rel)
	}
	found, err := LicenceFiles(dir)
	if err != nil {
		return fmt.Errorf("FindLicences: %w", err)
	}
	kept, err := snapshotLicences(ctx.WorkingDir, found)
	if err != nil {
		return fmt.Errorf("FindLicences: %w", err)
	}
	ctx.SetString(pipeline.KeyLicenceFiles, strings.Join(kept, "\n"))
	ctx.SetString(pipeline.KeyUpstreamRoot, root)
	ctx.Metric("find_licences", map[string]any{"files": len(found)})
	return nil
}

// licenceStems are the names an upstream gives its licence and notice files,
// compared case-insensitively against the part of the filename before the
// first '.', '-' or '_': LICENSE, LICENSE.md, LICENSE-GRAPHICS, licence.txt,
// COPYING, NOTICE, OFL.txt.
var licenceStems = map[string]bool{
	"license": true, "licence": true, "copying": true, "notice": true, "ofl": true,
}

// IsLicenceFile reports whether a root-level filename names a licence or notice.
func IsLicenceFile(name string) bool {
	stem := strings.ToLower(name)
	if i := strings.IndexAny(stem, ".-_"); i >= 0 {
		stem = stem[:i]
	}
	return licenceStems[stem]
}

// LicenceFiles lists the licence and notice files directly under root, as
// absolute paths in a stable order. Subdirectories are not searched: a licence
// file deep in an upstream tree usually belongs to a dependency or a fixture,
// not to the icons.
func LicenceFiles(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && IsLicenceFile(e.Name()) {
			abs, err := filepath.Abs(filepath.Join(root, e.Name()))
			if err != nil {
				return nil, err
			}
			out = append(out, abs)
		}
	}
	sort.Strings(out)
	return out, nil
}

// LicenceSnapshotDir is where FindLicences copies the licence files it found,
// inside the pipeline's working directory but outside the fetched tree.
const LicenceSnapshotDir = "licence-files"

// snapshotLicences copies the licence files out of the fetched tree, byte for
// byte under their upstream names, and returns the copies' paths. A later
// transform may rebuild the fetched tree: Categorise wipes its root and
// refills it with SVGs only, and where the root is the whole upstream (the
// Twemoji npm package) that deleted the LICENSE before LicenceTexts read it,
// failing every real emoji-sets sync. The copies cannot be touched by any
// stage that rewrites the tree. With no working directory (a caller running
// the stage outside a pipeline) the originals are recorded as before.
func snapshotLicences(workingDir string, found []string) ([]string, error) {
	if workingDir == "" || len(found) == 0 {
		return found, nil
	}
	dir, err := os.MkdirTemp(workingDir, LicenceSnapshotDir+"-")
	if err != nil {
		return nil, err
	}
	kept := make([]string, 0, len(found))
	for _, f := range found {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		dest := filepath.Join(dir, filepath.Base(f))
		if err := os.WriteFile(dest, b, 0o644); err != nil {
			return nil, err
		}
		kept = append(kept, dest)
	}
	return kept, nil
}
