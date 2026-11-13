package app

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/kinds"
)

// KindsSVGDir is where a pack keeps config.json and its files/ tree.
const KindsSVGDir = "resources/assets/svg"

// DefaultKindSuggestionsDir is where `check` and `sync --suggest-kinds` write,
// relative to the working directory (the repository root, in practice). The
// repository's .gitignore already covers .cache/, and `make clean` removes it.
// Not build/, which holds the binaries and nothing else (checkbuild).
const DefaultKindSuggestionsDir = ".cache/kind-suggestions"

// KindReport is one pack's row-7 scan.
type KindReport struct {
	Pack    string
	File    string
	Records int
	Items   int
	Skipped []string
	Issues  []kinds.Issue
	Err     error
}

// packSlug is what a pack name may be to become a file name.
var packSlug = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// SuggestKinds scans each pack's checkout for items that look like
// illustrations and writes <outDir>/<pack>.json for each (contract section 3).
//
// It reads the pack and writes only into outDir. It never touches config.json,
// the pack's files or any database: a suggestion becomes a kind only when a
// human copies its accept block into the pack's config. One pack failing does
// not stop the others; its report carries the error.
func SuggestKinds(packs []*config.PackConfig, mode kinds.Mode, outDir string) []KindReport {
	reports := make([]KindReport, 0, len(packs))
	for _, p := range packs {
		r := KindReport{Pack: p.Name}

		if !packSlug.MatchString(p.Name) || p.Name == ".." {
			r.Err = fmt.Errorf("pack name %q cannot be a file name", p.Name)
			reports = append(reports, r)
			continue
		}

		res, err := kinds.Scan(p.Pack, filepath.Join(p.PackRoot, filepath.FromSlash(KindsSVGDir)), mode)
		if err != nil {
			r.Err = err
			reports = append(reports, r)
			continue
		}
		r.Items, r.Records, r.Skipped, r.Issues = res.Items, len(res.Records), res.Skipped, res.Issues

		r.File = filepath.Join(outDir, p.Name+".json")
		if err := kinds.WriteRecords(r.File, res.Records); err != nil {
			r.File, r.Err = "", err
		}
		reports = append(reports, r)
	}
	return reports
}
