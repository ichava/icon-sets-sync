package transforms

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// ExcludeMetadata names icons to drop from a data file the upstream ships,
// so the exclusion follows upstream instead of a list copied out of it once.
//
// Simple Icons is the case: data/simple-icons.json records a licence for the
// handful of logos that are not CC0, and those must stay out of a CC0 pack.
// A hand-kept list of 212 slugs would be wrong the first release a logo is
// added or relicensed; the data file is right by construction.
type ExcludeMetadata struct {
	// File is the data file, relative to the upstream's root (before any
	// source_path narrowed the tree). It must hold a JSON array of objects.
	File string

	// Name is the dot-path, inside each object, to the icon's file stem.
	Name string

	// Field is the dot-path to the value tested, and Allow the values kept.
	// An object without Field is kept: the upstream's own licence covers it.
	Field string
	Allow []string
}

// Exclude drops icons from the tree by name, by directory or by upstream
// metadata.
//
// It exists for the icons a pack must not ship although upstream does:
// trademarks (Octicons' GitHub marks), artwork under another licence (Simple
// Icons' non-CC0 logos), and directories whose licence is not stated (Game
// Icons' badges/). Every reason is in the pack config beside the rule, and
// every drop is counted, so a pack never shrinks without the run saying so.
type Exclude struct {
	pipeline.TransformKind

	// Names are file stems (no .svg) dropped wherever they occur.
	Names []string

	// Dirs are directory names: a file with one of them anywhere in its
	// path, relative to the fetched tree, is dropped.
	Dirs []string

	Metadata *ExcludeMetadata
}

func (Exclude) Name() string { return "Exclude" }

func (t Exclude) Execute(ctx *pipeline.Context) error {
	root, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("Exclude: fetched_path not set; needs a source upstream")
	}
	if len(t.Names) == 0 && len(t.Dirs) == 0 && t.Metadata == nil {
		return fmt.Errorf("Exclude: declares no names, dirs or metadata, so it would drop nothing")
	}

	names := map[string]string{}
	for _, n := range t.Names {
		names[n] = "names"
	}
	if t.Metadata != nil {
		fromData, err := t.Metadata.names(ctx)
		if err != nil {
			return fmt.Errorf("Exclude: %w", err)
		}
		for _, n := range fromData {
			if _, listed := names[n]; !listed {
				names[n] = "metadata"
			}
		}
	}
	dirs := map[string]bool{}
	for _, d := range t.Dirs {
		dirs[d] = true
	}

	files, err := walkSVGs(root)
	if err != nil {
		return err
	}

	matched := map[string]int{}
	removed := 0
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		drop := false
		for _, dir := range parts[:len(parts)-1] {
			if dirs[dir] {
				matched["dir:"+dir]++
				drop = true
			}
		}
		base := parts[len(parts)-1]
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if _, ok := names[stem]; ok {
			matched[stem]++
			drop = true
		}
		if !drop {
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		removed++
	}

	// A listed name or directory upstream no longer has is not an error --
	// upstream retiring a trademarked icon is the outcome the rule wants --
	// but it is said, so a list that has gone stale is noticed.
	var unmatched []string
	for _, n := range t.Names {
		if matched[n] == 0 {
			unmatched = append(unmatched, n)
		}
	}
	for _, d := range t.Dirs {
		if matched["dir:"+d] == 0 {
			unmatched = append(unmatched, d+"/")
		}
	}
	sort.Strings(unmatched)
	if len(unmatched) > 0 {
		ctx.Logger().Warn("exclude: listed but not in this release", "entries", unmatched)
	}

	byMetadata := 0
	for n, why := range names {
		if why == "metadata" && matched[n] > 0 {
			byMetadata++
		}
	}
	ctx.Metric("exclude", map[string]any{
		"scanned": len(files), "removed": removed, "by_metadata": byMetadata, "unmatched": unmatched,
	})
	ctx.Logger().Info("exclude", "scanned", len(files), "removed", removed, "by_metadata", byMetadata)
	return nil
}

// names reads the data file and returns the stems it excludes.
func (m ExcludeMetadata) names(ctx *pipeline.Context) ([]string, error) {
	if m.File == "" || m.Name == "" || m.Field == "" {
		return nil, fmt.Errorf("metadata needs file, name and field")
	}
	upstream, ok := ctx.String(pipeline.KeyUpstreamRoot)
	if !ok {
		return nil, fmt.Errorf("metadata: upstream_root not set; FindLicences records it before anything narrows the tree")
	}
	rel := filepath.Clean(filepath.FromSlash(m.File))
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("metadata file %q is outside the upstream", m.File)
	}
	data, err := os.ReadFile(filepath.Join(upstream, rel))
	if err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}

	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("metadata %s: want a JSON array of objects: %w", m.File, err)
	}
	// An empty file would exclude nothing and look exactly like a release in
	// which every icon is allowed.
	if len(items) == 0 {
		return nil, fmt.Errorf("metadata %s lists no items", m.File)
	}

	allow := map[string]bool{}
	for _, a := range m.Allow {
		allow[a] = true
	}
	var out []string
	for i, item := range items {
		value, present := dotString(item, m.Field)
		if !present || allow[value] {
			continue
		}
		name, ok := dotString(item, m.Name)
		if !ok || name == "" {
			// Fail closed: an item that must be excluded but cannot be named
			// would otherwise ship.
			return nil, fmt.Errorf("metadata %s: item %d has %s=%q but no %s to name its file", m.File, i, m.Field, value, m.Name)
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// dotString reads a string at a dot-path inside a decoded JSON object.
func dotString(obj map[string]any, path string) (string, bool) {
	var cur any = obj
	for _, key := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		if cur, ok = m[key]; !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok
}
