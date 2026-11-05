// Package transforms mutates the in-flight asset tree between a source and a
// sink. Every transform reads and writes Context.FetchedPath.
package transforms

import (
	"bytes"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/svg"
)

// walkSVGs returns every .svg under root, in a stable order.
//
// Sorted because the order reaches the progress output and the metrics, and an
// unordered walk makes two runs over the same tree produce logs that cannot be
// diffed.
func walkSVGs(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(path), ".svg") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// SubsetTo re-points FetchedPath at a subdirectory of the fetched tree.
//
// This is how a source's `source_path` is honoured: npm ships @tabler/icons
// with the SVGs under icons/, and the rest of the tarball is not wanted.
type SubsetTo struct {
	pipeline.TransformKind
	Subdir string
}

func (SubsetTo) Name() string { return "SubsetTo" }

func (t SubsetTo) Execute(ctx *pipeline.Context) error {
	subdir := strings.Trim(t.Subdir, "/")
	if subdir == "" {
		return nil
	}

	root, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("SubsetTo: fetched_path not set; needs a source upstream")
	}

	target := filepath.Join(root, subdir)
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("SubsetTo: subdir %q not found under %s", t.Subdir, root)
	}

	ctx.SetFetchedPath(target)
	return nil
}

// Sanitise applies the SVG policy to every file in the tree, in place.
type Sanitise struct {
	pipeline.TransformKind

	// AlsoStripClass removes the class attribute as well. Off by default.
	AlsoStripClass bool

	// Strict turns policy violations into a failure. Off by default, because
	// an upstream that ships one odd icon should not block the other 6,000 --
	// the violation is reported either way.
	Strict bool

	// ReportOnly counts what the policy would remove without writing anything.
	//
	// For the aggregator pack, which has never been sanitised: its committed
	// icons keep comments the policy strips, including fontawesome's licence
	// attribution, so enforcing the policy during a refresh would rewrite tens
	// of thousands of files and discard that attribution. Measured, all 50
	// fontawesome icons sampled change under the policy. Reporting keeps the
	// security picture visible without turning a refresh into a rewrite; the
	// decision to enforce is then someone's to take deliberately.
	ReportOnly bool

	// MaxStrayTextFiles is the most files one run may rewrite to drop stray
	// text (see svg.StrayText); zero means DefaultMaxStrayTextFiles. Force
	// lets a run past it.
	MaxStrayTextFiles int
	Force             bool

	// DropUnparsable removes a file the sanitiser cannot parse instead of
	// leaving it as upstream shipped it. Off by default. It is the stricter
	// choice, not a looser one: a file that cannot be parsed cannot be
	// checked, so with it on such a file never reaches the pack. Noto Emoji
	// needs it, for 170 SVGs that declare Adobe Illustrator entities in an
	// internal DTD. The count is in the run's metrics and logged per file.
	DropUnparsable bool

	Log *slog.Logger
}

// DefaultMaxStrayTextFiles bounds how many files one Sanitise may rewrite to
// drop stray text. OpenMoji 15.1.0 had one such file in 4,000-odd; a release
// with dozens is malformed, and rewriting it quietly would hide that.
const DefaultMaxStrayTextFiles = 25

func (Sanitise) Name() string { return "Sanitise" }

func (t Sanitise) Execute(ctx *pipeline.Context) error {
	root, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("Sanitise: fetched_path not set; needs a source upstream")
	}

	files, err := walkSVGs(root)
	if err != nil {
		return err
	}

	log := t.Log
	if log == nil {
		log = ctx.Logger()
	}

	var cleaned, unparsable, strayFiles, strayNodes int
	var violations, drops []string
	type pendingWrite struct {
		path string
		data []byte
	}
	var writes []pendingWrite

	for _, path := range files {
		original, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		filtered, removed, err := svg.SanitiseBytes(original, t.AlsoStripClass)
		if err != nil {
			// An unparseable file is counted and left exactly as it was.
			// Dropping it would silently shrink the pack; rewriting a guess at
			// it would be worse.
			unparsable++
			log.Warn("sanitise: cannot parse", "file", path, "error", err, "dropped", t.DropUnparsable && !t.ReportOnly)
			if t.DropUnparsable && !t.ReportOnly {
				drops = append(drops, path)
			}
			continue
		}

		if len(removed) > 0 {
			violations = append(violations,
				filepath.Base(path)+": "+svg.SummariseRemoved(removed))
		}

		if n := svg.StrayTextCount(removed); n > 0 {
			strayFiles++
			strayNodes += n
			rel, _ := filepath.Rel(root, path)
			log.Warn("sanitise: dropped stray text", "file", rel, "count", n)
		}

		if !bytes.Equal(filtered, original) {
			if !t.ReportOnly {
				writes = append(writes, pendingWrite{path, filtered})
			}
			cleaned++
		}
	}

	// Checked before anything is written, so a refused run leaves the tree as
	// it was fetched. Report-only writes nothing, so it is never refused.
	maxStray := t.MaxStrayTextFiles
	if maxStray <= 0 {
		maxStray = DefaultMaxStrayTextFiles
	}
	if !t.ReportOnly && strayFiles > maxStray {
		msg := fmt.Sprintf("Sanitise: %d files carry stray text under <svg> or <g>, over the %d-file limit; "+
			"the upstream release is probably malformed", strayFiles, maxStray)
		if !t.Force {
			return fmt.Errorf("%s -- check it, or pass --force", msg)
		}
		log.Warn(msg + "; continuing because the run is forced")
	}

	for _, w := range writes {
		if err := os.WriteFile(w.path, w.data, 0o644); err != nil {
			return err
		}
	}
	for _, path := range drops {
		if err := os.Remove(path); err != nil {
			return err
		}
	}

	// `cleaned` counts files the policy would change; in report-only mode none
	// of them were written, which the metric records so a reader of the run
	// summary cannot mistake one mode for the other.
	ctx.Metric("sanitise", map[string]any{
		"scanned": len(files), "cleaned": cleaned,
		"unparsable": unparsable, "dropped_unparsable": len(drops), "violations": len(violations),
		"report_only":      t.ReportOnly,
		"stray_text_files": strayFiles, "stray_text_nodes": strayNodes,
	})
	log.Info("sanitise", "scanned", len(files), "would_change", cleaned,
		"written", !t.ReportOnly, "unparsable", unparsable, "violations", len(violations),
		"stray_text_files", strayFiles)

	if t.Strict && len(violations) > 0 {
		return &svg.PolicyViolationError{Violations: violations}
	}
	return nil
}

// slugPattern and slugTrim implement the shared filename slug rule.
var (
	slugPattern = regexp.MustCompile(`[^a-z0-9.]+`)
	nameSlug    = regexp.MustCompile(`[^a-z0-9]+`)
)

// Slugify lowercases and hyphenates SVG filenames in place.
//
// The default rule keeps dots, so `lucene.net.svg` stays as it is. Two options
// widen it for upstreams whose names need more: HyphenateDots slugs the stem
// with SlugifyName, the rule the indexer uses (`lucene.net` becomes
// `lucene-net`), and Dirs slugs every directory name by the same rule, for an
// upstream that keeps its categories in folders such as `Game & Sports`.
//
// Two names that slug to one fail the run, naming both. Renaming over an
// existing file would drop one of them at an unchanged count, which nothing
// later in the run would notice.
type Slugify struct {
	pipeline.TransformKind

	// Dirs slugs directory names as well as file names.
	Dirs bool

	// HyphenateDots turns a dot inside the stem into a hyphen too.
	HyphenateDots bool
}

func (Slugify) Name() string { return "Slugify" }

func (t Slugify) Execute(ctx *pipeline.Context) error {
	root, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("Slugify: fetched_path not set; needs a source upstream")
	}

	files, err := walkSVGs(root)
	if err != nil {
		return err
	}

	renamed := 0
	for _, path := range files {
		dir, base := filepath.Split(path)
		slug, err := t.fileSlug(base)
		if err != nil {
			return fmt.Errorf("Slugify: %s: %w", path, err)
		}
		if slug == base {
			continue
		}
		if err := renameNoClobber(path, filepath.Join(dir, slug)); err != nil {
			return fmt.Errorf("Slugify: %w", err)
		}
		renamed++
	}

	dirsRenamed := 0
	if t.Dirs {
		if dirsRenamed, err = t.slugDirs(root); err != nil {
			return fmt.Errorf("Slugify: %w", err)
		}
	}

	ctx.Metric("slugify", map[string]any{"renamed": renamed, "dirs_renamed": dirsRenamed})
	return nil
}

// fileSlug is the slugged form of one filename.
func (t Slugify) fileSlug(base string) (string, error) {
	lowered := strings.ToLower(base)
	if !t.HyphenateDots {
		slug := strings.Trim(slugPattern.ReplaceAllString(lowered, "-"), "-.")
		if !strings.HasSuffix(lowered, ".svg") {
			slug += ".svg"
		}
		return slug, nil
	}
	stem := strings.TrimSuffix(lowered, ".svg")
	slug := SlugifyName(stem)
	if slug == "" {
		return "", fmt.Errorf("%q has no letters or digits to keep", base)
	}
	return slug + ".svg", nil
}

// dirSlug is the slugged form of one directory name.
func (t Slugify) dirSlug(name string) string {
	if t.HyphenateDots {
		return SlugifyName(name)
	}
	return strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(name), "-"), "-.")
}

// slugDirs renames every directory under root, deepest first, so a parent's
// rename never invalidates a child path still to be visited.
func (t Slugify) slugDirs(root string) (int, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	sort.Slice(dirs, func(i, j int) bool {
		di, dj := strings.Count(dirs[i], string(filepath.Separator)), strings.Count(dirs[j], string(filepath.Separator))
		if di != dj {
			return di > dj
		}
		return dirs[i] < dirs[j]
	})

	renamed := 0
	for _, path := range dirs {
		parent, name := filepath.Split(path)
		slug := t.dirSlug(name)
		if slug == "" {
			return renamed, fmt.Errorf("directory %q has no letters or digits to keep", path)
		}
		if slug == name {
			continue
		}
		if err := renameNoClobber(path, filepath.Join(parent, slug)); err != nil {
			return renamed, err
		}
		renamed++
	}
	return renamed, nil
}

// renameNoClobber renames from to to, refusing when to is another file.
//
// A case-only rename on a case-insensitive filesystem finds to already there
// -- it is the same file -- so that one is allowed.
func renameNoClobber(from, to string) error {
	if info, err := os.Stat(to); err == nil {
		src, serr := os.Stat(from)
		if serr != nil || !os.SameFile(src, info) {
			return fmt.Errorf("%s and %s slug to the same name", from, to)
		}
	}
	return os.Rename(from, to)
}

// SlugifyName is the shared name-to-slug rule, exported because the CLDR
// categoriser and the indexer both need it and must agree.
func SlugifyName(name string) string {
	return strings.Trim(nameSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// SlugifyGroup slugs a CLDR group name.
//
// Python has two near-copies of this -- categorise's replaces "&" with a space
// and then spaces with hyphens, indexer's only does the first. They produce
// identical output for every CLDR group because SlugifyName already collapses
// any run of non-alphanumerics to a single hyphen, so the second replace is
// redundant. One function here, rather than two that are equal by luck.
func SlugifyGroup(group string) string {
	return SlugifyName(strings.ReplaceAll(group, "&", " "))
}
