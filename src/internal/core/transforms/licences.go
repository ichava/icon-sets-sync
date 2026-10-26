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
type FindLicences struct {
	pipeline.TransformKind
}

func (FindLicences) Name() string { return "FindLicences" }

func (FindLicences) Execute(ctx *pipeline.Context) error {
	root, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("FindLicences: fetched_path not set; needs a source upstream")
	}
	found, err := LicenceFiles(root)
	if err != nil {
		return fmt.Errorf("FindLicences: %w", err)
	}
	ctx.SetString(pipeline.KeyLicenceFiles, strings.Join(found, "\n"))
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
