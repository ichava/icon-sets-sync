package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
	"github.com/ichava/icon-sets-sync/src/internal/core/pathguard"
)

// ManifestPack is the registry slug of the aggregator pack the manifest-pack
// audit surveys.
const ManifestPack = "icon-sets-bundled"

// AuditOptions configures AuditManifestPack.
type AuditOptions struct {
	ConfigDir string
	Workspace string

	// Set restricts the audit to one set. Package verifies one named npm
	// package against it instead of guessing; it needs Set.
	Set     string
	Package string

	// WorkDir keeps the downloads. Empty means a temporary directory that is
	// removed afterwards.
	WorkDir string

	// Parallel bounds the concurrent sets. Below 1 means the CPU count.
	Parallel int

	// Fetcher fetches candidate packages. Nil means npm.
	Fetcher manifestpack.Fetcher

	// Starting is called once the sets are known, before any fetch. May be nil.
	Starting func(sets int, filesRoot string)

	// SetDone is called as each set is resolved, in completion order, one
	// call at a time. May be nil.
	SetDone func(manifestpack.Resolution)

	// Logger receives the resolver's lines. Nil means slog.Default().
	Logger *slog.Logger
}

// AuditManifestPack identifies, by measurement, which upstream each vendored
// set of the manifest pack came from. It is the body of `manifest-pack audit`,
// so the CLI and the web UI cannot survey differently. Results are sorted.
func AuditManifestPack(ctx context.Context, opts AuditOptions) ([]manifestpack.Resolution, error) {
	pack, err := config.LoadPack(ManifestPack, opts.ConfigDir)
	if err != nil {
		return nil, err
	}
	ApplyWorkspace(pack, opts.Workspace)

	filesRoot := filepath.Join(pack.PackRoot, "resources", "assets", "svg", "files")
	sets, err := manifestpack.ListSets(filesRoot)
	if err != nil {
		return nil, err
	}
	if opts.Set != "" {
		if _, err := pathguard.Join(filesRoot, opts.Set); err != nil {
			return nil, usage(errf("--set %q: %w", opts.Set, err))
		}
		if !slices.Contains(sets, opts.Set) {
			return nil, usage(&UnknownSetError{Set: opts.Set, Pack: ManifestPack, Sets: sets, Suggestions: SuggestSets(opts.Set, sets)})
		}
		sets = []string{opts.Set}
	}
	if opts.Package != "" && opts.Set == "" {
		return nil, usage(errf("--package needs --set: a package can only be verified against one set"))
	}

	workDir := opts.WorkDir
	if workDir == "" {
		workDir, err = os.MkdirTemp("", "icon-sets-sync-manifest-pack-audit-")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(workDir) }()
	}

	parallel := opts.Parallel
	if parallel <= 0 {
		parallel = runtime.NumCPU()
	}

	if opts.Starting != nil {
		opts.Starting(len(sets), filesRoot)
	}
	fetcher := opts.Fetcher
	if fetcher == nil {
		fetcher = manifestpack.NpmFetcher{Log: opts.Logger}
	}
	results := resolveSets(ctx, sets, filesRoot, workDir, parallel, opts.Package, fetcher, opts.Logger, opts.SetDone)
	manifestpack.SortResolutions(results)
	return results, nil
}

// ResolveSets audits sets concurrently.
//
// Each set is independent and dominated by a network fetch, so this is
// worthwhile -- dozens of sets fetched one at a time is minutes of waiting. Results
// are written into a fixed slot per set rather than appended, so the output
// order stays deterministic regardless of which finishes first.
func ResolveSets(ctx context.Context, sets []string, filesRoot, workDir string, parallel int, pkg string, fetcher manifestpack.Fetcher) []manifestpack.Resolution {
	return resolveSets(ctx, sets, filesRoot, workDir, parallel, pkg, fetcher, nil, nil)
}

func resolveSets(ctx context.Context, sets []string, filesRoot, workDir string, parallel int, pkg string,
	fetcher manifestpack.Fetcher, log *slog.Logger, done func(manifestpack.Resolution)) []manifestpack.Resolution {
	var explicit []string
	if pkg != "" {
		explicit = []string{pkg}
	}
	if parallel < 1 {
		parallel = 1
	}
	results := make([]manifestpack.Resolution, len(sets))

	var wg sync.WaitGroup
	var mu sync.Mutex
	slots := make(chan struct{}, parallel)

	for i, set := range sets {
		wg.Add(1)
		go func(i int, set string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()

			setDir, err := pathguard.Join(filesRoot, set)
			if err != nil {
				results[i] = manifestpack.Resolution{Set: set, Confidence: manifestpack.ConfidenceNone, Error: err.Error()}
			} else {
				results[i] = manifestpack.ResolveSet(ctx, setDir, fetcher, workDir, log, explicit...)
			}
			if done != nil {
				mu.Lock()
				done(results[i])
				mu.Unlock()
			}
		}(i, set)
	}
	wg.Wait()
	return results
}

// UnknownSetError is an audit asked for a set the pack does not have. It
// carries the near names, so a front-end can offer them.
type UnknownSetError struct {
	Set, Pack   string
	Sets        []string
	Suggestions []string
}

func (e *UnknownSetError) Error() string {
	msg := "there is no set \"" + e.Set + "\" in " + e.Pack
	if len(e.Suggestions) > 0 {
		return msg + "; did you mean " + strings.Join(e.Suggestions, " or ") + "?"
	}
	return msg + "; leave the set empty to audit every one"
}

// AuditSets lists the aggregator pack's sets, for a form's suggestions.
func AuditSets(configDir, workspace string) ([]string, error) {
	pack, err := config.LoadPack(ManifestPack, configDir)
	if err != nil {
		return nil, err
	}
	ApplyWorkspace(pack, workspace)
	return manifestpack.ListSets(filepath.Join(pack.PackRoot, "resources", "assets", "svg", "files"))
}

// SuggestSets returns the sets a mistyped name probably meant: those it is a
// part of ("tabler" for tabler-icons), and those one or two edits away
// ("mdl" for mdi). Sorted; empty when nothing is close.
func SuggestSets(name string, sets []string) []string {
	name = strings.ToLower(strings.TrimSpace(name))
	var out []string
	for _, s := range sets {
		if strings.Contains(s, name) || strings.Contains(name, s) && len(s) > 2 || editDistance(name, s) <= 2 {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
