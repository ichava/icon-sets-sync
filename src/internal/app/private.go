package app

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
)

// PrivateConfigSubdir is where a pack keeps its private sync config.
const PrivateConfigSubdir = ".dev/icon-sets-sync"

// PrivateConfigCandidates finds pack checkouts carrying a private config
// directory (<pack>/.dev/icon-sets-sync/ with a packs.json): under each
// packages_dirs entry, under workspace, and beside this checkout. The web UI
// offers them when the aggregator pack's config is not loaded, so the fix is
// a click rather than a restart with a flag. Nothing is enabled by finding
// one: a private config stays opt-in.
func PrivateConfigCandidates(workspace string) []string {
	roots := append([]string(nil), PackagesDirs()...)
	if workspace != "" {
		roots = append(roots, workspace)
	}
	cwd, _ := os.Getwd()
	for _, start := range []string{cwd, config.ExecutableDir()} {
		if start == "" {
			continue
		}
		if root := config.FindToolkitRoot(start); root != "" {
			roots = append(roots, filepath.Dir(root))
		}
	}
	return privateConfigCandidatesIn(roots)
}

func privateConfigCandidatesIn(roots []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, root := range roots {
		matches, _ := filepath.Glob(filepath.Join(root, "*", filepath.FromSlash(PrivateConfigSubdir), "packs.json"))
		for _, m := range matches {
			dir := filepath.Dir(m)
			if abs, err := filepath.Abs(dir); err == nil {
				dir = abs
			}
			if !seen[dir] {
				seen[dir] = true
				out = append(out, dir)
			}
		}
	}
	sort.Strings(out)
	return out
}
