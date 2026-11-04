package kinds

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/docfile"
	"github.com/ichava/icon-sets-sync/src/internal/core/svg"
)

// Mode says which resolved sources the heuristic may second-guess.
type Mode string

const (
	// ModeDefault looks only at items that resolved icon by falling all the way
	// through to the default.
	ModeDefault Mode = "default"
	// ModeAll also looks at items whose kind came from an explicit package.kind.
	ModeAll Mode = "all"
)

// ParseMode reads the --suggest-kinds value. The empty string is the default.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case "", ModeDefault:
		return ModeDefault, nil
	case ModeAll:
		return ModeAll, nil
	}
	return "", fmt.Errorf("unknown suggest-kinds mode %q (want default or all)", s)
}

// Current is the kind and source an item resolved to before any suggestion.
type Current struct {
	Kind   Kind   `json:"kind"`
	Source Source `json:"source"`
}

// Record is one suggestion, the same JSON shape core's doctor emits. A human
// accepts it by merging Accept into the pack's config.json; nothing here does.
type Record struct {
	Package   string         `json:"package"`
	Path      string         `json:"path"`
	Current   Current        `json:"current"`
	Suggested Kind           `json:"suggested"`
	Signals   []Signal       `json:"signals"`
	Accept    map[string]any `json:"accept"`
}

// ScanResult is everything a scan found.
type ScanResult struct {
	Records []Record
	// Items is the number of items looked at: every .svg under files/ that is
	// not a theme twin, whether or not it could be measured.
	Items int
	// Skipped lists items that could not be measured, with the reason.
	Skipped []string
	// Issues are problems in the pack's own kind keys.
	Issues []Issue
}

type scanned struct {
	path    string
	current Resolution
	signals []Signal
	suggest bool
}

// Scan measures every item in a pack and returns the suggestions.
//
// svgDir is the pack's resources/assets/svg directory: config.json beside a
// files/ tree. It only reads. A missing config.json is a pack with no kind
// keys, and every item then reads icon/default.
func Scan(pkg, svgDir string, mode Mode) (*ScanResult, error) {
	cfg := &Config{}
	data, err := os.ReadFile(filepath.Join(svgDir, "config.json"))
	switch {
	case err == nil:
		if cfg, err = ParseConfig(data); err != nil {
			return nil, err
		}
	case os.IsNotExist(err):
		cfg, _ = ParseConfig([]byte("{}"))
	default:
		return nil, err
	}

	filesDir := filepath.Join(svgDir, "files")
	if info, err := os.Stat(filesDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s: no files/ directory to scan", svgDir)
	}

	// Every .svg, as an item path. Only regular files: a symlink could point
	// anywhere, and WalkDir does not follow directory links.
	var all []string
	err = filepath.WalkDir(filesDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() || !strings.EqualFold(filepath.Ext(p), ".svg") {
			return nil
		}
		rel, err := filepath.Rel(filesDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		all = append(all, rel[:len(rel)-len(".svg")])
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(all)
	exists := make(map[string]bool, len(all))
	for _, p := range all {
		exists[p] = true
	}

	res := &ScanResult{Issues: cfg.Issues}
	var items []scanned
	for _, p := range all {
		if isTwin(cfg, exists, p) {
			continue
		}
		res.Items++

		it := scanned{path: p, current: cfg.Resolve(p)}
		if eligible(it.current, mode) {
			sigs, err := measureFile(filepath.Join(filesDir, filepath.FromSlash(p)+".svg"))
			if err != nil {
				res.Skipped = append(res.Skipped, fmt.Sprintf("%s: %v", p, err))
			} else {
				it.signals, it.suggest = sigs, Qualifies(sigs)
			}
		}
		items = append(items, it)
	}

	res.Records = records(pkg, items)
	return res, nil
}

// eligible reports whether the heuristic may look at an item: it resolved icon,
// from the default (or, in ModeAll, from package.kind). Item, rule and category
// are explicit human decisions and are never second-guessed.
func eligible(r Resolution, mode Mode) bool {
	if r.Kind != Icon {
		return false
	}
	return r.Source == SourceDefault || (mode == ModeAll && r.Source == SourcePack)
}

// isTwin reports whether a file is a theme twin of a light file that exists.
// A suffixed file with no light sibling stays an item, so no asset is dropped.
func isTwin(cfg *Config, exists map[string]bool, itemPath string) bool {
	dir, stem := path.Split(itemPath)
	for _, suffix := range cfg.ThemeSuffixes(itemPath) {
		if stem == suffix || !strings.HasSuffix(stem, suffix) {
			continue
		}
		if exists[dir+strings.TrimSuffix(stem, suffix)] {
			return true
		}
	}
	return false
}

func measureFile(p string) ([]Signal, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	// A file over the SVG policy's byte cap is read only that far. Its size is
	// the stat size either way, so the bytes signal is right; a truncated read
	// then fails to parse, and the item is reported as skipped.
	raw, err := io.ReadAll(io.LimitReader(f, svg.DefaultMaxBytes))
	if err != nil {
		return nil, err
	}
	m, err := Measure(raw, info.Size())
	if err != nil {
		return nil, err
	}
	return m.Signals(), nil
}

// records turns scanned items into suggestions, collapsing a folder into one
// rule when it holds at least HeuristicGroupMin items and at least 80% of them
// are suggested.
func records(pkg string, items []scanned) []Record {
	byFolder := map[string][]scanned{}
	for _, it := range items {
		byFolder[path.Dir(it.path)] = append(byFolder[path.Dir(it.path)], it)
	}

	var out []Record
	for folder, members := range byFolder {
		var picked []scanned
		for _, m := range members {
			if m.suggest {
				picked = append(picked, m)
			}
		}
		if len(picked) == 0 {
			continue
		}

		// Integer form of picked/total >= 0.8.
		if folder != "." && len(members) >= HeuristicGroupMin && len(picked)*5 >= len(members)*4 {
			out = append(out, Record{
				Package:   pkg,
				Path:      folder,
				Current:   Current{picked[0].current.Kind, picked[0].current.Source},
				Suggested: Illustration,
				Signals:   groupSignals(picked),
				Accept: map[string]any{"kinds": map[string]any{"rules": []any{
					map[string]any{"match": folder + "/**", "kind": string(Illustration)},
				}}},
			})
			continue
		}

		for _, m := range picked {
			out = append(out, Record{
				Package:   pkg,
				Path:      m.path,
				Current:   Current{m.current.Kind, m.current.Source},
				Suggested: Illustration,
				Signals:   m.signals,
				Accept: map[string]any{"items": map[string]any{
					m.path: map[string]any{"kind": string(Illustration)},
				}},
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// groupSignals summarises a folder: each signal that fired on any suggested
// member, in the contract's order, with the most extreme value seen (for the
// aspect ratio, the one furthest from 1).
func groupSignals(members []scanned) []Signal {
	best := map[string]Signal{}
	for _, m := range members {
		for _, s := range m.signals {
			cur, ok := best[s.ID]
			if !ok || extremity(s) > extremity(cur) {
				best[s.ID] = s
			}
		}
	}
	var out []Signal
	for _, id := range []string{SignalAspect, SignalPalette, SignalBytes, SignalElements, SignalCanvas} {
		if s, ok := best[id]; ok {
			out = append(out, s)
		}
	}
	return out
}

func extremity(s Signal) float64 {
	if s.ID == SignalAspect && s.Value > 0 {
		return math.Abs(math.Log(s.Value))
	}
	return s.Value
}

// WriteRecords writes a pack's suggestions as a JSON array, replacing the file
// atomically. An empty list is written as [] so a reader can tell "ran, found
// nothing" from "never ran".
func WriteRecords(file string, recs []Record) error {
	if recs == nil {
		recs = []Record{}
	}
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return docfile.WriteAtomic(file, append(data, '\n'))
}
