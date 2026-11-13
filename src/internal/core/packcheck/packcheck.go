// Package packcheck validates the structure of an ichava icon pack checkout:
// its composer.json, its manifest, that a pack config names it, its sets,
// licences and categories, and that every file under the sets is an SVG that
// parses. The `validate` command and the web UI's Validate page share it.
package packcheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/svg"
)

// ComposerType is how a pack is recognised, and the only way (ichava's own
// rule): no folder or package name is matched.
const ComposerType = "ichava-icon-set"

// Where a pack keeps things, relative to its root.
const (
	ManifestPath = "resources/assets/svg/config.json"
	FilesDir     = "resources/assets/svg/files"
	LicencesDir  = "resources/assets/svg/licenses"
)

// Level is how bad a finding is.
type Level string

// The levels.
const (
	Error Level = "error"
	Warn  Level = "warning"
)

// Finding is one problem. Path is relative to the pack root, for an
// annotation's file= and for a person; it may be empty.
type Finding struct {
	Level Level  `json:"level"`
	Check string `json:"check"`
	Path  string `json:"path,omitempty"`
	Text  string `json:"text"`
}

// Report is one pack's result.
type Report struct {
	Root     string    `json:"root"`
	Name     string    `json:"name,omitempty"`
	Config   string    `json:"pack_config,omitempty"`
	Sets     int       `json:"sets"`
	SVGs     int       `json:"svgs"`
	Findings []Finding `json:"findings"`
}

// Errors and Warnings count the findings by level.
func (r Report) Errors() int   { return r.count(Error) }
func (r Report) Warnings() int { return r.count(Warn) }

func (r Report) count(l Level) int {
	n := 0
	for _, f := range r.Findings {
		if f.Level == l {
			n++
		}
	}
	return n
}

func (r *Report) add(l Level, check, path, format string, a ...any) {
	r.Findings = append(r.Findings, Finding{Level: l, Check: check, Path: path, Text: fmt.Sprintf(format, a...)})
}

// Options tunes a check.
type Options struct {
	// PackConfigs maps a composer name to the pack config that names it, so
	// a pack no config syncs is reported.
	PackConfigs map[string]string
	// ParseSVGs parses every SVG. Slow on the large packs (bundled has over
	// 120,000), so the web page makes it a choice.
	ParseSVGs bool
	// MaxListed caps the files a finding of one kind names one by one; the
	// rest are counted. Zero means 50.
	MaxListed int
}

// Discover lists the ichava packs directly under each directory, by composer
// type, and the directories that do not exist.
func Discover(dirs []string) (packs, missing []string) {
	seen := map[string]bool{}
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			missing = append(missing, d)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			root := filepath.Join(d, e.Name())
			if seen[root] {
				continue
			}
			if c, err := readComposer(root); err == nil && c.Type == ComposerType {
				seen[root] = true
				packs = append(packs, root)
			}
		}
	}
	sort.Strings(packs)
	return packs, missing
}

type composer struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func readComposer(root string) (composer, error) {
	var c composer
	data, err := os.ReadFile(filepath.Join(root, "composer.json"))
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(data, &c)
}

// Check validates one pack checkout.
func Check(root string, o Options) Report {
	if o.MaxListed == 0 {
		o.MaxListed = 50
	}
	r := Report{Root: root, Findings: []Finding{}}

	c, err := readComposer(root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		r.add(Error, "composer", "composer.json", "composer.json is missing")
	case err != nil:
		r.add(Error, "composer", "composer.json", "composer.json does not parse: %v", err)
	default:
		r.Name = c.Name
		if c.Name == "" {
			r.add(Error, "composer", "composer.json", "composer.json has no name")
		}
		if c.Type != ComposerType {
			r.add(Error, "composer", "composer.json", "composer.json type is %q; an ichava pack is %q", c.Type, ComposerType)
		}
	}

	checkManifest(root, &r)

	if r.Name != "" {
		if cfg, ok := o.PackConfigs[r.Name]; ok {
			r.Config = cfg
		} else {
			r.add(Warn, "pack-config", "", "no pack config in the config directory names %s, so nothing syncs it", r.Name)
		}
	}

	if _, err := os.Stat(filepath.Join(root, "LICENSE")); err != nil {
		r.add(Error, "licence", "LICENSE", "the pack has no LICENSE at its root")
	}
	if n := countFiles(filepath.Join(root, LicencesDir)); n == 0 {
		r.add(Error, "licence", LicencesDir, "no upstream licence text under %s", LicencesDir)
	}

	checkSets(root, o, &r)
	return r
}

func checkManifest(root string, r *Report) {
	data, err := os.ReadFile(filepath.Join(root, ManifestPath))
	if err != nil {
		r.add(Error, "manifest", ManifestPath, "the manifest %s is missing", ManifestPath)
		return
	}
	var m struct {
		SchemaVersion string `json:"schema_version"`
		Package       struct {
			Name string `json:"name"`
		} `json:"package"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		r.add(Error, "manifest", ManifestPath, "the manifest does not parse: %v", err)
		return
	}
	if m.SchemaVersion == "" {
		r.add(Warn, "manifest", ManifestPath, "the manifest has no schema_version")
	}
	switch {
	case m.Package.Name == "":
		r.add(Error, "manifest", ManifestPath, "the manifest has no package.name")
	case r.Name != "" && m.Package.Name != r.Name:
		r.add(Error, "manifest", ManifestPath, "the manifest names %s, composer.json %s", m.Package.Name, r.Name)
	}
}

func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func checkSets(root string, o Options, r *Report) {
	files := filepath.Join(root, FilesDir)
	entries, err := os.ReadDir(files)
	if err != nil {
		r.add(Error, "sets", FilesDir, "no sets: %s is missing", FilesDir)
		return
	}
	// A single-set pack keeps its SVGs straight in files/ (the manifest's
	// "type": "single"); a multi-set pack keeps one directory per set. Both
	// are valid. Anything else directly in files/ is stray.
	var setDirs []string
	direct := false
	for _, e := range entries {
		switch {
		case e.IsDir():
			setDirs = append(setDirs, filepath.Join(files, e.Name()))
		case junk(e.Name()):
		case strings.EqualFold(filepath.Ext(e.Name()), ".svg"):
			direct = true
		}
	}
	s := &setWalk{root: root, o: o, r: r}
	if direct {
		r.Sets++
		n := s.walk(files)
		if n == 0 {
			r.add(Error, "sets", FilesDir, "the set is empty")
		}
		r.SVGs += n
	}
	for _, d := range setDirs {
		if direct {
			// files/ is the set; its directories are categories, walked above.
			break
		}
		r.Sets++
		n := s.walk(d)
		if n == 0 {
			r.add(Error, "sets", s.rel(d), "the set %s is empty", filepath.Base(d))
		}
		r.SVGs += n
	}
	if r.Sets == 0 {
		r.add(Error, "sets", FilesDir, "%s holds no set directory and no SVG", FilesDir)
	}
	if s.stray > o.MaxListed {
		r.add(Error, "stray", "", "and %d more files that are not SVGs", s.stray-o.MaxListed)
	}
	if s.broken > o.MaxListed {
		r.add(Error, "svg", "", "and %d more SVGs that do not parse", s.broken-o.MaxListed)
	}
}

type setWalk struct {
	root          string
	o             Options
	r             *Report
	stray, broken int
}

func (s *setWalk) rel(path string) string {
	rel, _ := filepath.Rel(s.root, path)
	return filepath.ToSlash(rel)
}

// walk counts a set's SVGs, reporting stray files, empty categories and,
// with ParseSVGs, SVGs that do not parse.
func (s *setWalk) walk(setDir string) int {
	svgs := 0
	_ = filepath.WalkDir(setDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry is reported by what it lacks
		}
		name := d.Name()
		if d.IsDir() {
			if path != setDir && countSVGs(path) == 0 {
				s.r.add(Warn, "category", s.rel(path), "the category %s holds no SVG", name)
			}
			return nil
		}
		if junk(name) {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(name), ".svg") {
			s.stray++
			if s.stray <= s.o.MaxListed {
				s.r.add(Error, "stray", s.rel(path), "%s is not an SVG; the ecosystem ships SVG only", name)
			}
			return nil
		}
		svgs++
		if s.o.ParseSVGs {
			if err := parseSVG(path); err != nil {
				s.broken++
				if s.broken <= s.o.MaxListed {
					s.r.add(Error, "svg", s.rel(path), "%s does not parse: %v", name, err)
				}
			}
		}
		return nil
	})
	return svgs
}

func countSVGs(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(d.Name()), ".svg") {
			n++
		}
		return nil
	})
	return n
}

// parseSVG checks a file parses with the toolkit's own SVG parser -- the one
// the sanitiser uses before an icon is committed -- and that its root element
// is svg.
func parseSVG(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // a file inside the pack being checked
	if err != nil {
		return err
	}
	root, err := svg.Parse(data, false)
	if err != nil {
		return err
	}
	if root == nil {
		return errors.New("no root element")
	}
	if root.Local != "svg" {
		return fmt.Errorf("the root element is <%s>, not <svg>", root.Local)
	}
	return nil
}

// junk is what an OS leaves in a directory. It is never committed (the packs
// ignore it), so it is skipped rather than reported.
func junk(name string) bool {
	switch name {
	case ".DS_Store", "Thumbs.db", "desktop.ini", ".gitkeep":
		return true
	}
	return false
}
