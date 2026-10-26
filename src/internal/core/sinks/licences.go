package sinks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// LicenceTexts writes the upstream's own licence and notice files, verbatim,
// into the pack beside the icons they cover.
//
// The files are the ones FindLicences recorded at the root of the fetched
// tree, copied byte for byte under their upstream names. Never a template:
// the text that satisfies MIT's "this permission notice shall be included"
// is the upstream's own, copyright line and all.
//
// Root is the destination for one set, with {pack_root} and {version}
// interpolated; the recipes use resources/assets/svg/licenses/<set>, which is
// not export-ignored, so the texts ship in every dist archive with the icons.
// The directory is replaced on each run, so a NOTICE upstream drops is dropped
// here too.
//
// An upstream with no licence file at its root fails the run, naming the set.
// Shipping icons whose licence text cannot be found is the defect this sink
// exists to end, so it is not reduced to a warning on a scheduled job.
type LicenceTexts struct {
	pipeline.SinkKind

	Root string
	Set  string

	// MissingReason lets a set whose upstream ships no licence file refresh
	// anyway, with the reason recorded where it is configured (a bundled set's
	// licence_missing). The set's licence directory is then removed rather than
	// left holding a text that no longer matches, and the gap is logged and
	// reported as a metric on every run.
	MissingReason string
}

func (LicenceTexts) Name() string { return "LicenceTexts" }

func (s LicenceTexts) Execute(ctx *pipeline.Context) error {
	raw, ok := ctx.String(pipeline.KeyLicenceFiles)
	if !ok {
		return fmt.Errorf("LicenceTexts %s: licence_files not set; FindLicences must run before anything narrows the tree", s.Set)
	}
	var files []string
	for _, f := range strings.Split(raw, "\n") {
		if f != "" {
			files = append(files, f)
		}
	}
	version, _ := ctx.FetchedVersion()
	target, err := filepath.Abs(config.Interpolate(s.Root, map[string]string{
		"version":   version,
		"pack_root": ctx.ConfigString(pipeline.ConfigPackRoot),
	}))
	if err != nil {
		return err
	}

	if len(files) == 0 && s.MissingReason != "" {
		ctx.Logger().Warn("licence texts: upstream ships none; recorded as missing", "set", s.Set, "reason", s.MissingReason)
		ctx.Metric("licence_texts", map[string]any{"set": s.Set, "files": 0, "missing": s.MissingReason})
		return os.RemoveAll(target)
	}
	if len(files) == 0 {
		src, _ := ctx.String(pipeline.KeyFetchedSource)
		return fmt.Errorf("LicenceTexts %s: upstream %s ships no LICENSE, LICENCE, COPYING, NOTICE or OFL file at its root; set licence_missing with the evidence, and add the text by hand", s.Set, orDefault(src, "(unknown source)"))
	}

	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}

	for _, src := range files {
		if err := copyFile(src, filepath.Join(target, filepath.Base(src))); err != nil {
			return fmt.Errorf("LicenceTexts %s: %w", s.Set, err)
		}
	}
	ctx.Metric("licence_texts", map[string]any{"set": s.Set, "files": len(files), "target": target})
	return nil
}
