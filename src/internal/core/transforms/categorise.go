package transforms

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// CLDRRecord is one fully-qualified emoji from Unicode's emoji-test.txt.
type CLDRRecord struct {
	Codepoints []string
	Group      string
	Name       string
}

// Key is the codepoint sequence as it appears in an upstream filename.
func (r CLDRRecord) Key() string {
	return strings.ToLower(strings.Join(r.Codepoints, "-"))
}

var (
	cldrGroupRe = regexp.MustCompile(`^# group:\s*(.+)$`)
	// Only fully-qualified rows. The minimally-qualified and unqualified
	// variants are the same emoji written with fewer selectors, and taking
	// them too would map several codepoint sequences onto one output name.
	cldrLineRe = regexp.MustCompile(
		`^([0-9A-Fa-f][0-9A-Fa-f ]+);\s*fully-qualified\s*#\s*\S+\s+E\d+\.\d+\s+(.+)$`)
)

// ParseCLDR reads emoji-test.txt into records, carrying the group heading
// forward across the rows beneath it.
func ParseCLDR(text string) []CLDRRecord {
	var records []CLDRRecord
	group := ""

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if m := cldrGroupRe.FindStringSubmatch(line); m != nil {
				group = strings.TrimSpace(m[1])
			}
			continue
		}
		m := cldrLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		records = append(records, CLDRRecord{
			Codepoints: strings.Fields(m[1]),
			Group:      group,
			Name:       strings.TrimSpace(m[2]),
		})
	}
	return records
}

// Categorise regroups a flat emoji tree into Unicode CLDR category
// directories, renaming each file to the emoji's slug.
//
// Upstream ships one flat directory named by codepoint; the packs file icons
// under a category so a picker can group them. Requires a UnicodeCldr source
// to have populated the taxonomy first.
type Categorise struct {
	pipeline.TransformKind

	// By is "cldr" or "passthrough".
	By string

	// Preserve opts into never dropping an icon. Off by default, which is the
	// historical behaviour the deprecated emoji pack relies on: exact match
	// only, and an icon with no taxonomy entry is dropped and counted missing.
	//
	// On, two things change. A file whose name differs from a taxonomy entry
	// only by the emoji-presentation selector (FE0F) is matched to it --
	// OpenMoji ships 2600.svg where emoji-test.txt has 2600 FE0F, which left
	// the sun, the wastebasket and ~200 more without a category. And an icon
	// still unmatched, such as an OpenMoji extra or an emoji newer than the
	// taxonomy, is kept in UncategorisedDir under its upstream name and counted
	// as "uncategorised".
	Preserve bool

	// MaxUncategorised fails the run when more than this fraction of the icons
	// ended up uncategorised, which is what a taxonomy of the wrong release
	// looks like. Zero disables it. Only read with Preserve.
	MaxUncategorised float64

	// Force lets a run past MaxUncategorised. Nothing is dropped either way.
	Force bool

	Log *slog.Logger
}

// UncategorisedDir is where Preserve keeps icons the taxonomy does not name.
const UncategorisedDir = "uncategorised"

// withoutFE0F drops the emoji-presentation selector from a hyphenated key.
func withoutFE0F(key string) string {
	parts := strings.Split(key, "-")
	kept := parts[:0]
	for _, p := range parts {
		if p != "fe0f" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "-")
}

func (Categorise) Name() string { return "Categorise" }

func (t Categorise) Execute(ctx *pipeline.Context) error {
	switch t.By {
	case "", "passthrough":
		return nil
	case "cldr":
	default:
		return fmt.Errorf("Categorise: unknown taxonomy %q", t.By)
	}

	cldrText, ok := ctx.String(pipeline.KeyCLDRText)
	if !ok {
		return fmt.Errorf("Categorise(by=cldr): UnicodeCldr source must run first to populate cldr_text")
	}
	root, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("Categorise: fetched_path not set; needs a source upstream")
	}

	records := ParseCLDR(cldrText)
	if len(records) == 0 {
		// A taxonomy that parsed to nothing would move every icon into the
		// "missing" bucket and silently empty the pack.
		return fmt.Errorf("Categorise(by=cldr): emoji-test.txt parsed to zero records")
	}

	index := make(map[string][2]string, len(records))
	for _, r := range records {
		index[r.Key()] = [2]string{SlugifyGroup(r.Group), SlugifyName(r.Name)}
	}

	// Selector-insensitive lookup, used only by Preserve and only where it is
	// unambiguous: two taxonomy entries sharing a stripped key map to neither.
	loose := map[string][2]string{}
	if t.Preserve {
		clash := map[string]bool{}
		for k, v := range index {
			sk := withoutFE0F(k)
			if _, seen := loose[sk]; seen {
				clash[sk] = true
			}
			loose[sk] = v
		}
		for sk := range clash {
			delete(loose, sk)
		}
	}

	files, err := walkSVGs(root)
	if err != nil {
		return err
	}

	// Stage, wipe, rebuild. The move has to go through a staging directory
	// because the destination tree is rooted at the same path as the source:
	// writing category directories into it while still reading from it would
	// have the walk pick up files it had already placed.
	staging := filepath.Join(filepath.Dir(root), filepath.Base(root)+"__staging__")
	if err := os.RemoveAll(staging); err != nil {
		return err
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	for _, f := range files {
		if err := os.Rename(f, filepath.Join(staging, filepath.Base(f))); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}

	entries, err := os.ReadDir(staging)
	if err != nil {
		return err
	}

	var copied, missing, uncategorised int
	// target path -> what holds it, to catch slug collisions
	type placement struct {
		stem  string
		exact bool
	}
	placed := map[string]placement{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))

		entry, found := index[stem]
		exact := found
		if !found && t.Preserve {
			entry, found = loose[withoutFE0F(stem)]
		}
		if !found && t.Preserve {
			// Kept, in a bucket that says what it is, under the upstream name.
			uncategorised++
			target := filepath.Join(root, UncategorisedDir, stem+".svg")
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Rename(filepath.Join(staging, name), target); err != nil {
				return err
			}
			continue
		}
		if !found {
			// A codepoint the taxonomy does not name. The file is dropped,
			// which is what Python does -- an icon with no category has
			// nowhere to go in the pack's directory layout.
			missing++
			continue
		}

		target := filepath.Join(root, entry[0], entry[1]+".svg")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if prev, clash := placed[target]; clash && t.Preserve {
			// Two files for one slug, and a rename would overwrite the first
			// without a word. Either two emoji share a slug ("keycap: #" and
			// "keycap: *" are both "keycap"), or OpenMoji ships the emoji both
			// with and without its FE0F (1F3F3-FE0F.svg and 1F3F3.svg are both
			// "white flag"). An exact match beats an FE0F-less one, so the
			// fully-qualified artwork keeps the name; between equals the later
			// keeps it, as it always has. The loser goes to the uncategorised
			// bucket under its upstream name.
			loser := prev.stem
			if prev.exact && !exact {
				loser = stem
			}
			moved := filepath.Join(root, UncategorisedDir, loser+".svg")
			if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
				return err
			}
			if loser == stem {
				if err := os.Rename(filepath.Join(staging, name), moved); err != nil {
					return err
				}
				uncategorised++
				continue
			}
			if err := os.Rename(target, moved); err != nil {
				return err
			}
			copied--
			uncategorised++
		}
		placed[target] = placement{stem: stem, exact: exact}
		if err := os.Rename(filepath.Join(staging, name), target); err != nil {
			return err
		}
		copied++
	}
	_ = os.RemoveAll(staging)

	metric := map[string]any{"copied": copied, "missing": missing}
	if t.Preserve {
		metric["uncategorised"] = uncategorised
	}
	ctx.Metric("categorise", metric)
	ctx.Extras[pipeline.KeyCLDRRecords] = records

	log := t.Log
	if log == nil {
		log = ctx.Logger()
	}
	if !t.Preserve {
		log.Info("categorise", "taxonomy", "cldr", "copied", copied, "missing", missing)
		return nil
	}

	log.Info("categorise", "taxonomy", "cldr", "copied", copied, "missing", missing, "uncategorised", uncategorised)
	total := copied + uncategorised
	if uncategorised > 0 {
		log.Warn("categorise: icons with no taxonomy entry were kept uncategorised",
			"bucket", UncategorisedDir, "count", uncategorised, "of", total, "taxonomy_version", cldrVersion(ctx))
	}
	if t.MaxUncategorised > 0 && total > 0 && float64(uncategorised)/float64(total) > t.MaxUncategorised {
		share := float64(uncategorised) / float64(total)
		msg := fmt.Sprintf("Categorise: %d of %d icons (%.0f%%) matched no taxonomy entry, over the %.0f%% limit; "+
			"the emoji-test.txt release (%s) is probably older than the icons -- set emoji.unicode_version",
			uncategorised, total, share*100, t.MaxUncategorised*100, cldrVersion(ctx))
		if !t.Force {
			return fmt.Errorf("%s, or pass --force", msg)
		}
		log.Warn(msg + "; continuing because the run is forced")
	}
	return nil
}

func cldrVersion(ctx *pipeline.Context) string {
	v, _ := ctx.String(pipeline.KeyCLDRVersion)
	return v
}

// Indexer writes the lookup files a picker needs beside the icons.
type Indexer struct {
	pipeline.TransformKind

	// Targets is any of "codepoints" and "names".
	Targets []string

	// OutDirKey names an Extras key holding the output directory. When empty
	// or absent, the files land beside the fetched tree.
	OutDirKey string
}

func (Indexer) Name() string { return "Indexer" }

func (t Indexer) Execute(ctx *pipeline.Context) error {
	recordsAny, ok := ctx.Extras[pipeline.KeyCLDRRecords]
	if !ok {
		return fmt.Errorf("Indexer: cldr_records not set; needs Categorise(by=cldr) upstream")
	}
	records, ok := recordsAny.([]CLDRRecord)
	if !ok {
		return fmt.Errorf("Indexer: cldr_records is %T, want []CLDRRecord", recordsAny)
	}

	outDir := ""
	if t.OutDirKey != "" {
		outDir, _ = ctx.String(t.OutDirKey)
	}
	if outDir == "" {
		root, ok := ctx.FetchedPath()
		if !ok {
			return fmt.Errorf("Indexer: no output directory and no fetched_path")
		}
		outDir = filepath.Dir(root)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	written := map[string]any{}

	for _, target := range t.Targets {
		switch target {
		case "codepoints":
			out := make(map[string]string, len(records))
			for _, r := range records {
				out[strings.Join(r.Codepoints, "-")] = SlugifyGroup(r.Group) + "/" + SlugifyName(r.Name)
			}
			if err := writeJSON(filepath.Join(outDir, "codepoints.json"), out); err != nil {
				return err
			}
			written["codepoints"] = len(out)

		case "names":
			type entry struct {
				Codepoints []string `json:"codepoints"`
				Category   string   `json:"category"`
				Name       string   `json:"name"`
			}
			out := make(map[string]entry, len(records))
			for _, r := range records {
				out[SlugifyName(r.Name)] = entry{
					Codepoints: r.Codepoints,
					Category:   SlugifyGroup(r.Group),
					Name:       r.Name,
				}
			}
			if err := writeJSON(filepath.Join(outDir, "names.json"), out); err != nil {
				return err
			}
			written["names"] = len(out)

		default:
			return fmt.Errorf("Indexer: unknown target %q", target)
		}
	}

	ctx.Metric("indexer", written)
	return nil
}

// writeJSON writes an index with two-space indent and a trailing newline,
// matching Python's json.dumps(..., indent=2, sort_keys=True) so that a sync PR
// shows only the entries that actually changed.
//
// encoding/json already sorts map keys. HTML escaping is switched off, because
// on by default it would render the ampersand in an emoji name as \u0026 and
// make every file differ from the Python's for no reason anyone could see.
func writeJSON(path string, value any) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return err
	}
	// Encode already appends the newline.
	return os.WriteFile(path, b.Bytes(), 0o644)
}
