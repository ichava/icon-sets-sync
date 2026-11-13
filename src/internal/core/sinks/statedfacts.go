package sinks

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/gitx"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// FilesDir is where a pack keeps its SVGs, relative to its root: one folder per
// set or variant, or the SVGs themselves for a flat pack.
const FilesDir = "resources/assets/svg/files"

// StatedFacts keeps the figures a pack states about itself in step with what a
// sync has just written: its icon count, in total and per set folder, and the
// version it vendors, wherever README.md, composer.json and the pack's tests
// state them.
//
// The packs' guards pin those figures on purpose, so a silently shrunk or grown
// set fails. Nothing moved the pins with the assets, so every refresh that
// changed a count opened red and was fixed by hand: tabler 3.51.0, carbon
// 11.92.0, simple-icons 16.37.0 and lucide 1.72.0 on 2026-11-11. The licence
// check's version is VersionStamp's (stampLicenceCheck), not this.
//
// It runs before VersionStamp, so the version file still names the previous
// version, and reads the previous counts from git HEAD, the tree the sync
// started from. Only exact, whole occurrences of a previous figure are
// rewritten, and only in those files. A count under ten is left alone, being
// too likely to be some other number, and so is a previous figure two facts
// share. A previous figure no file states is reported, never guessed at.
type StatedFacts struct {
	pipeline.SinkKind

	Pack *config.PackConfig
	Git  *gitx.Git
	Log  *slog.Logger
}

func (StatedFacts) Name() string { return "StatedFacts" }

// fact is one figure that moved: what the pack stated, and what it should.
type fact struct {
	label    string
	old, new string
	version  bool
}

func (s StatedFacts) Execute(ctx *pipeline.Context) error {
	log := s.Log
	if log == nil {
		log = ctx.Logger()
	}
	if ctx.DryRun() || s.Pack == nil || s.Pack.PackRoot == "" {
		return nil
	}
	git := s.Git
	if git == nil {
		git = gitx.New()
	}
	root := s.Pack.PackRoot

	before, err := committedCounts(context.Background(), git, root)
	if err != nil {
		// A pack with no commits has nothing stated yet.
		ctx.Metric("stated_facts", map[string]any{"status": "no-history"})
		return nil //nolint:nilerr // no HEAD is an ordinary state for a new pack
	}
	after, err := diskCounts(filepath.Join(root, FilesDir))
	if err != nil {
		return err
	}

	facts := countFacts(before, after)
	newVersion, _ := ctx.FetchedVersion()
	if oldVersion := config.ReadVendoredVersion(s.Pack); oldVersion != "" && newVersion != "" && oldVersion != newVersion {
		facts = append(facts, fact{label: "version", old: oldVersion, new: newVersion, version: true})
	}
	if len(facts) == 0 {
		ctx.Metric("stated_facts", map[string]any{"status": "unchanged"})
		return nil
	}

	files, err := statingFiles(root)
	if err != nil {
		return err
	}
	stated := map[string]bool{}
	var changed []string
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		for _, f := range facts {
			if f.version && !strings.HasSuffix(path, "README.md") && !strings.HasSuffix(path, "composer.json") {
				continue
			}
			for _, form := range forms(f) {
				next, n := replaceWhole(text, form[0], form[1], f.version)
				if n > 0 {
					text, stated[f.label] = next, true
				}
			}
		}
		if text != string(body) {
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			changed = append(changed, rel)
		}
	}

	var unstated []string
	for _, f := range facts {
		if !stated[f.label] {
			unstated = append(unstated, fmt.Sprintf("%s %s", f.label, display(f.old, f.version)))
		}
	}
	ctx.Metric("stated_facts", map[string]any{"status": "updated", "files": changed, "unstated": unstated})
	log.Info("stated facts", "pack", s.Pack.Pack, "files", changed)
	if len(unstated) > 0 {
		log.Warn("stated facts: a previous figure appears in no README, composer.json or test; check what the pack states by hand",
			"pack", s.Pack.Pack, "figures", unstated)
	}
	return nil
}

// committedCounts counts the SVGs under FilesDir at HEAD: the total under "",
// and each top-level folder under its name.
func committedCounts(ctx context.Context, git *gitx.Git, root string) (map[string]int, error) {
	paths, err := git.ListTree(ctx, root, "HEAD", FilesDir)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, p := range paths {
		if rel, ok := strings.CutPrefix(p, FilesDir+"/"); ok && strings.HasSuffix(rel, ".svg") {
			tally(counts, rel)
		}
	}
	return counts, nil
}

// diskCounts counts the SVGs the sync has written, the same way.
func diskCounts(dir string) (map[string]int, error) {
	counts := map[string]int{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".svg") {
			rel, _ := filepath.Rel(dir, path)
			tally(counts, filepath.ToSlash(rel))
		}
		return nil
	})
	return counts, err
}

func tally(counts map[string]int, rel string) {
	counts[""]++
	if top, _, nested := strings.Cut(rel, "/"); nested {
		counts[top]++
	}
}

// countFacts is every count that moved: the total, then each folder, in name order.
// A count under ten, or a previous count two facts share, is not a fact.
func countFacts(before, after map[string]int) []fact {
	keys := make([]string, 0, len(after))
	for k := range after {
		if _, ok := before[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	seen := map[int]int{}
	for _, k := range keys {
		seen[before[k]]++
	}
	var facts []fact
	for _, k := range keys {
		o, n := before[k], after[k]
		if o == n || o < 10 || seen[o] > 1 {
			continue
		}
		label := "total"
		if k != "" {
			label = k
		}
		facts = append(facts, fact{label: label, old: strconv.Itoa(o), new: strconv.Itoa(n)})
	}
	return facts
}

// forms are the spellings a fact is stated in: a version as itself; a count raw
// (2773) and, from a thousand up, grouped (2,773).
func forms(f fact) [][2]string {
	if f.version {
		return [][2]string{{f.old, f.new}}
	}
	out := [][2]string{{f.old, f.new}}
	if g := grouped(f.old); g != f.old {
		out = append(out, [2]string{g, grouped(f.new)})
	}
	return out
}

func display(v string, version bool) string {
	if version {
		return v
	}
	return grouped(v)
}

// grouped writes a whole number with thousands separators: 2773 is "2,773".
func grouped(n string) string {
	if len(n) <= 3 {
		return n
	}
	var b strings.Builder
	head := len(n) % 3
	if head > 0 {
		b.WriteString(n[:head])
	}
	for i := head; i < len(n); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(n[i : i+3])
	}
	return b.String()
}

// replaceWhole replaces every occurrence of old that stands alone: not inside a
// longer number (12,773 or 2773.5). A version may follow a letter (v16.36.0),
// a count may not.
func replaceWhole(text, old, repl string, version bool) (string, int) {
	var b strings.Builder
	n := 0
	for {
		i := strings.Index(text, old)
		if i < 0 {
			b.WriteString(text)
			return b.String(), n
		}
		end := i + len(old)
		if wholeAt(text, i, end, version) {
			b.WriteString(text[:i])
			b.WriteString(repl)
			n++
		} else {
			b.WriteString(text[:end])
		}
		text = text[end:]
	}
}

func wholeAt(text string, start, end int, version bool) bool {
	if start > 0 {
		c := text[start-1]
		if isDigit(c) || c == ',' || c == '.' || (!version && isLetter(c)) {
			return false
		}
	}
	if end < len(text) {
		c := text[end]
		if isDigit(c) || (c == ',' || c == '.') && end+1 < len(text) && isDigit(text[end+1]) {
			return false
		}
	}
	return true
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// statingFiles are where a pack states its figures: README.md, composer.json and
// every PHP file under tests/.
func statingFiles(root string) ([]string, error) {
	var files []string
	for _, name := range []string{"README.md", "composer.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			files = append(files, filepath.Join(root, name))
		}
	}
	err := filepath.WalkDir(filepath.Join(root, "tests"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".php") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
