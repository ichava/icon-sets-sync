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
// A directory holding a SOURCE.md is hand-maintained, and the sink never
// deletes or overwrites it. SOURCE.md is where a maintainer records where a
// hand-supplied text came from, for a set whose upstream ships none; replacing
// that directory on a scheduled run would destroy the only copy of the text in
// the pack. Such a set is reported as hand-supplied rather than missing, and a
// licence_missing exception it satisfies stops warning. If upstream later
// starts shipping licence files for that set, the folder is still kept and the
// run warns that upstream now ships its own, so a human reconciles the two:
// the hand-supplied text may be the better one (a fuller copyright line), and
// only a person can tell, so the sink neither switches silently nor fails a
// scheduled run over it.
//
// An upstream with no licence file at its root fails the run, naming the set.
// Shipping icons whose licence text cannot be found is the defect this sink
// exists to end, so it is not reduced to a warning on a scheduled job.
type LicenceTexts struct {
	pipeline.SinkKind

	Root string
	Set  string

	// MissingReason lets a set whose upstream ships no licence file refresh
	// anyway, with the reason recorded where it is configured (a manifest-pack set's
	// licence_missing). The set's licence directory is then removed rather than
	// left holding a text that no longer matches, and the gap is logged and
	// reported as a metric on every run, unless the directory is hand-supplied
	// (holds a SOURCE.md), which satisfies the exception.
	MissingReason string
}

// HandSuppliedMarker is the file whose presence makes a licence directory
// hand-maintained. It records where the hand-supplied text came from.
const HandSuppliedMarker = "SOURCE.md"

// The status values LicenceTexts reports in its metric.
const (
	LicenceStatusCopied       = "copied"
	LicenceStatusMissing      = "missing"
	LicenceStatusHandSupplied = "hand-supplied"
)

// isHandSupplied reports whether dir holds a SOURCE.md as a regular file.
func isHandSupplied(dir string) (bool, error) {
	fi, err := os.Lstat(filepath.Join(dir, HandSuppliedMarker))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return fi.Mode().IsRegular(), nil
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

	handSupplied, err := isHandSupplied(target)
	if err != nil {
		return fmt.Errorf("LicenceTexts %s: %w", s.Set, err)
	}
	src, _ := ctx.String(pipeline.KeyFetchedSource)

	if handSupplied {
		// Never delete or overwrite a hand-maintained directory, whatever
		// upstream does.
		if len(files) > 0 {
			ctx.Logger().Warn("licence texts: upstream now ships licence files; the hand-supplied folder is kept, reconcile it by hand",
				"set", s.Set, "source", orDefault(src, "(unknown source)"), "upstream_files", baseNames(files), "target", target,
				"hint", "compare the texts, then either keep the folder or delete its "+HandSuppliedMarker+" (and any licence_missing) so the sync copies upstream's")
			ctx.Metric("licence_texts", map[string]any{"set": s.Set, "files": 0, "status": LicenceStatusHandSupplied,
				"upstream_files": len(files), "reconcile": true, "target": target})
			return nil
		}
		if s.MissingReason == "" {
			return fmt.Errorf("LicenceTexts %s: upstream %s ships no LICENSE, LICENCE, COPYING, NOTICE or OFL file at its root; %s holds a hand-supplied text (it is kept), but set licence_missing with the evidence too", s.Set, orDefault(src, "(unknown source)"), target)
		}
		ctx.Logger().Info("licence texts: upstream ships none; hand-supplied", "set", s.Set, "target", target)
		ctx.Metric("licence_texts", map[string]any{"set": s.Set, "files": 0, "status": LicenceStatusHandSupplied, "target": target})
		return nil
	}

	if len(files) == 0 && s.MissingReason != "" {
		ctx.Logger().Warn("licence texts: upstream ships none; recorded as missing", "set", s.Set, "reason", s.MissingReason,
			"hint", "add the text by hand to "+target+" with a "+HandSuppliedMarker+" saying where it came from")
		ctx.Metric("licence_texts", map[string]any{"set": s.Set, "files": 0, "status": LicenceStatusMissing, "missing": s.MissingReason})
		return os.RemoveAll(target)
	}
	if len(files) == 0 {
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
	ctx.Metric("licence_texts", map[string]any{"set": s.Set, "files": len(files), "status": LicenceStatusCopied, "target": target})
	return nil
}

func baseNames(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}
