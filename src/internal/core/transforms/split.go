package transforms

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// SplitVariant is one output directory of SplitVariants, and the rule that
// decides which upstream files land in it.
type SplitVariant struct {
	// Path is the output directory the matching files are written to.
	Path string

	// From is the upstream directory the files are read from, relative to the
	// fetched tree. Empty means the tree's root.
	From string

	// Suffix selects the files whose stem ends with it, and is stripped from
	// the name written. An empty suffix takes every file in From that no
	// other variant reading From claimed.
	Suffix string

	// FlattenPrefix reads From's sub-directories too, prefixing each file's
	// name with its sub-directory path (watson-health/view.svg is written as
	// watson-health-view.svg). Without it only From's own files are read.
	// Every variant reading the same From must agree on it.
	FlattenPrefix bool
}

// SplitVariants sorts an upstream's files into one directory per variant,
// by the directory they come from and the suffix their name carries.
//
// It is FlattenVariants in the other direction. Upstreams spell a variant
// three ways -- as a directory (heroicons' 24/outline), as a name suffix in a
// shared directory (ionicons' bell-outline, Fluent's access_time_24_regular),
// or both at once (Phosphor's thin/bell-thin, Material Symbols'
// outlined/home-fill) -- and the packs keep every variant as a directory of
// bare names. One rule covers all of them: read From, claim each file for the
// variant with the longest matching suffix, strip it, write it under Path.
//
// A file no variant claims is dropped, and that is how a pack takes a subset:
// Fluent ships ten sizes and the pack keeps 24 by naming only the _24_
// suffixes. The drop is counted per source directory and logged, so a subset
// is visible in the run summary rather than silent.
//
// A variant that claims nothing fails the run. That is what an upstream
// renaming its suffixes looks like, and carrying on would empty a directory
// of the pack at the next sync.
type SplitVariants struct {
	pipeline.TransformKind
	Variants []SplitVariant
}

func (SplitVariants) Name() string { return "SplitVariants" }

// splitGroup is every variant reading one source directory.
type splitGroup struct {
	from     string
	flatten  bool
	variants []int
}

func (t SplitVariants) Execute(ctx *pipeline.Context) error {
	if len(t.Variants) == 0 {
		return fmt.Errorf("SplitVariants: no variants declared")
	}
	root, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("SplitVariants: fetched_path not set; needs a source upstream")
	}

	groups, err := t.groups()
	if err != nil {
		return err
	}

	type planned struct{ src, rel string }
	plan := map[string]planned{}
	claimed := make([]int, len(t.Variants))
	dropped := map[string]int{}

	for _, g := range groups {
		dir := filepath.Join(root, filepath.FromSlash(g.from))
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("SplitVariants: source directory %q not found under %s", orDefault(g.from, "."), root)
		}

		files, err := splitSources(dir, g.flatten)
		if err != nil {
			return fmt.Errorf("SplitVariants: %w", err)
		}
		for _, rel := range files {
			subdir, base := filepath.Split(filepath.FromSlash(rel))
			stem := strings.TrimSuffix(base, filepath.Ext(base))

			v, name := t.claim(g, stem)
			if v < 0 {
				dropped[orDefault(g.from, ".")]++
				continue
			}
			if subdir != "" {
				name = strings.ReplaceAll(filepath.ToSlash(filepath.Clean(subdir)), "/", "-") + "-" + name
			}
			out := strings.Trim(t.Variants[v].Path, "/") + "/" + name + ".svg"
			from := strings.TrimPrefix(g.from+"/"+rel, "/")
			if prev, clash := plan[out]; clash {
				return fmt.Errorf("SplitVariants: %q would be written twice, from %s and %s -- "+
					"two upstream names reduce to one once the suffix is stripped", out, prev.rel, from)
			}
			plan[out] = planned{src: filepath.Join(dir, filepath.FromSlash(rel)), rel: from}
			claimed[v]++
		}
	}

	for i, n := range claimed {
		if n == 0 {
			v := t.Variants[i]
			return fmt.Errorf("SplitVariants: variant %q (from %q, suffix %q) matched no files; "+
				"upstream has probably renamed its layout", v.Path, orDefault(v.From, "."), v.Suffix)
		}
	}

	out, err := os.MkdirTemp(ctx.WorkingDir, "split-")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(plan))
	for name := range plan {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		dest := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := copyInto(plan[name].src, dest); err != nil {
			return err
		}
	}

	perVariant := map[string]any{}
	for i, v := range t.Variants {
		perVariant[strings.Trim(v.Path, "/")] = claimed[i]
	}
	droppedTotal := 0
	for from, n := range dropped {
		droppedTotal += n
		ctx.Logger().Info("split-variants: files no variant claims are not kept", "from", from, "dropped", n)
	}
	ctx.Metric("split_variants", map[string]any{
		"variants": perVariant, "icons": len(plan), "dropped": droppedTotal, "target": out,
	})
	ctx.SetFetchedPath(out)
	return nil
}

// groups collects the variants by source directory and checks they agree.
func (t SplitVariants) groups() ([]splitGroup, error) {
	byFrom := map[string]*splitGroup{}
	var order []string
	seenPath := map[string]bool{}

	for i, v := range t.Variants {
		path := strings.Trim(v.Path, "/")
		if path == "" || strings.Contains(path, "..") {
			return nil, fmt.Errorf("SplitVariants: variant %d needs a path inside the pack", i)
		}
		from := strings.Trim(filepath.ToSlash(v.From), "/")
		if from == "." {
			from = ""
		}
		if strings.Contains(from, "..") {
			return nil, fmt.Errorf("SplitVariants: variant %q reads from %q, outside the fetched tree", v.Path, v.From)
		}
		key := from + "\x00" + v.Suffix
		if seenPath[key] {
			return nil, fmt.Errorf("SplitVariants: two variants read %q with suffix %q; a file can only go one way", orDefault(from, "."), v.Suffix)
		}
		seenPath[key] = true

		g, ok := byFrom[from]
		if !ok {
			g = &splitGroup{from: from, flatten: v.FlattenPrefix}
			byFrom[from] = g
			order = append(order, from)
		} else if g.flatten != v.FlattenPrefix {
			return nil, fmt.Errorf("SplitVariants: the variants reading %q disagree on flatten_prefix", orDefault(from, "."))
		}
		g.variants = append(g.variants, i)
	}

	out := make([]splitGroup, 0, len(order))
	for _, from := range order {
		out = append(out, *byFrom[from])
	}
	return out, nil
}

// claim picks the variant a stem belongs to: the longest suffix it ends with,
// else the group's empty-suffix variant. It returns -1 when none claims it.
func (t SplitVariants) claim(g splitGroup, stem string) (int, string) {
	best, bare := -1, -1
	for _, i := range g.variants {
		s := t.Variants[i].Suffix
		if s == "" {
			bare = i
			continue
		}
		// The stem must keep a name of its own: a file called "-fill.svg" is
		// not the fill variant of nothing.
		if len(stem) > len(s) && strings.HasSuffix(stem, s) &&
			(best < 0 || len(s) > len(t.Variants[best].Suffix)) {
			best = i
		}
	}
	if best >= 0 {
		return best, strings.TrimSuffix(stem, t.Variants[best].Suffix)
	}
	if bare >= 0 {
		return bare, stem
	}
	return -1, ""
}

// splitSources lists the .svg files under dir, slash-separated and relative
// to it: its own files only, or every level when recursive.
func splitSources(dir string, recursive bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir && !recursive {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.EqualFold(filepath.Ext(path), ".svg") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return files, err
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
