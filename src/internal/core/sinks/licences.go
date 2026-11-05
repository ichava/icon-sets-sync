package sinks

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
// A directory holding a source.md is hand-maintained, and the sink never
// deletes or overwrites it. The uppercase SOURCE.md is still accepted but
// deprecated (see LegacyHandSuppliedMarker). source.md is where a maintainer records where a
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
	// (holds a source.md), which satisfies the exception.
	MissingReason string

	// Licence is the SPDX id the pack declares for this upstream. When set,
	// the upstream's licence text must still be that licence, or the run
	// fails before the existing texts are touched: a relicensed upstream is a
	// decision for a person, not something a scheduled sync copies in.
	Licence string

	// SHA256 is the digest one of the upstream's licence files must have.
	// When set, a sync whose upstream licence text changed by a byte fails
	// before the existing texts are touched, as a changed Licence does.
	SHA256 string
}

// ErrLicenceChanged marks a sync refused because the upstream's licence text
// no longer matches the licence the pack declares. Callers test for it with
// errors.Is to report a licence change apart from any other failure.
var ErrLicenceChanged = errors.New("licence changed")

// licenceSignatures maps a declared SPDX id to the opening lines its licence
// text starts with: each entry is one accepted form, and a file matches it
// when its first non-empty lines are exactly those lines, after trimming
// space and a Markdown heading's leading '#'. Adding a licence here is how a
// pack may declare it.
//
// The opening lines, not a hash: a copyright year moving is not a relicensing,
// and a guard that fired on it every January would be switched off. A pack
// that wants the stronger check sets licence_sha256 as well.
var licenceSignatures = map[string][][]string{
	"CC-BY-4.0":    {{"Attribution 4.0 International"}},
	"CC-BY-SA-4.0": {{"Attribution-ShareAlike 4.0 International"}},
	"CC-BY-3.0":    {{"Creative Commons Legal Code", "Attribution 3.0 Unported"}},
	"CC0-1.0": {
		{"CC0 1.0 Universal"},
		{"Creative Commons Legal Code", "CC0 1.0 Universal"},
	},
	"Apache-2.0": {{"Apache License", "Version 2.0, January 2004"}},
	"MIT":        {{"MIT License"}, {"The MIT License (MIT)"}, {"The MIT License"}},
	"ISC":        {{"ISC License"}},
}

// LicenceIDs lists the SPDX ids a pack may declare, sorted.
func LicenceIDs() []string {
	ids := make([]string, 0, len(licenceSignatures))
	for id := range licenceSignatures {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// openingLines returns up to n non-empty lines from the start of a text,
// trimmed of space and a Markdown heading marker.
func openingLines(text string, n int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) == n {
			break
		}
	}
	return out
}

// checkLicence reports an error unless one of the upstream files opens with
// one of the declared licence's signatures.
func checkLicence(spdx string, files []string) error {
	forms, ok := licenceSignatures[spdx]
	if !ok {
		return fmt.Errorf("unknown licence %q: add its opening lines to licenceSignatures before declaring it", spdx)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		for _, form := range forms {
			got := openingLines(string(b), len(form))
			if strings.Join(got, "\n") == strings.Join(form, "\n") {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: the pack declares %s but no upstream licence file opens with %q; review the new terms, then update the pack's declared licence", ErrLicenceChanged, spdx, strings.Join(forms[0], " / "))
}

// checkLicenceDigest reports an error unless one of the upstream files has
// exactly the recorded SHA-256. It is the check for a licence with no
// standard text (Material Design Icons' Pictogrammers licence, Game Icons'
// own statement), and for a pack that pins its licence text to the byte.
func checkLicenceDigest(want string, files []string) error {
	want = strings.ToLower(want)
	var got []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		h := hex.EncodeToString(sum[:])
		if h == want {
			return nil
		}
		got = append(got, filepath.Base(f)+"="+h)
	}
	return fmt.Errorf("%w: no upstream licence file has the recorded SHA-256 %s (found %s); review the new text, then update licence_sha256", ErrLicenceChanged, want, strings.Join(got, ", "))
}

// HandSuppliedMarker is the file whose presence makes a licence directory
// hand-maintained. It records where the hand-supplied text came from. The name
// is a lowercase slug, the estate's rule for file names, which the
// sim-standards pre-commit hook enforces on Markdown.
const HandSuppliedMarker = "source.md"

// LegacyHandSuppliedMarker is the uppercase spelling the sink accepted first.
// It still marks a directory hand-maintained, so a pack that has not renamed
// its markers keeps its texts.
//
// Deprecated: name the marker source.md (HandSuppliedMarker). The uppercase
// spelling is accepted until 0.3.0 at the earliest.
const LegacyHandSuppliedMarker = "SOURCE.md"

// handSuppliedMarkers are the names that mark a directory hand-maintained,
// matched exactly: a case-insensitive filesystem (macOS) must not accept a
// spelling the Linux runners that sync the packs would not.
var handSuppliedMarkers = []string{HandSuppliedMarker, LegacyHandSuppliedMarker}

// The status values LicenceTexts reports in its metric.
const (
	LicenceStatusCopied       = "copied"
	LicenceStatusMissing      = "missing"
	LicenceStatusHandSupplied = "hand-supplied"
)

// isHandSupplied reports whether dir holds a marker (source.md, or the
// deprecated SOURCE.md) as a regular file, and which name it found. Directory
// entries are compared byte for byte rather than probed with Lstat, which a
// case-insensitive filesystem would answer for any spelling.
func isHandSupplied(dir string) (bool, string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	for _, want := range handSuppliedMarkers {
		for _, e := range entries {
			if e.Name() == want && e.Type().IsRegular() {
				return true, want, nil
			}
		}
	}
	return false, "", nil
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

	handSupplied, marker, err := isHandSupplied(target)
	if err != nil {
		return fmt.Errorf("LicenceTexts %s: %w", s.Set, err)
	}
	if marker == LegacyHandSuppliedMarker {
		ctx.Logger().Info("licence texts: "+LegacyHandSuppliedMarker+" is deprecated; rename it "+HandSuppliedMarker,
			"set", s.Set, "target", target)
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
		ctx.Metric("licence_texts", map[string]any{"set": s.Set, "files": 0, "status": LicenceStatusMissing, "missing": s.MissingReason, "dry_run": ctx.DryRun()})
		if ctx.DryRun() {
			return nil
		}
		return os.RemoveAll(target)
	}
	if len(files) == 0 {
		return fmt.Errorf("LicenceTexts %s: upstream %s ships no LICENSE, LICENCE, COPYING, NOTICE or OFL file at its root; set licence_missing with the evidence, and add the text by hand", s.Set, orDefault(src, "(unknown source)"))
	}

	if s.Licence != "" {
		if err := checkLicence(s.Licence, files); err != nil {
			return fmt.Errorf("LicenceTexts %s: %w", s.Set, err)
		}
	}
	if s.SHA256 != "" {
		if err := checkLicenceDigest(s.SHA256, files); err != nil {
			return fmt.Errorf("LicenceTexts %s: %w", s.Set, err)
		}
	}

	if ctx.DryRun() {
		// Checked like a real run, so a relicensed upstream still fails here,
		// but the pack's licence directory is not replaced.
		ctx.Logger().Info("licence texts: dry run, pack left untouched", "set", s.Set, "would_write", baseNames(files), "target", target)
		ctx.Metric("licence_texts", map[string]any{"set": s.Set, "files": 0, "would_write": len(files),
			"status": LicenceStatusCopied, "dry_run": true, "target": target})
		return nil
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
