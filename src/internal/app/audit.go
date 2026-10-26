package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
)

// ManifestPack is the registry slug of the aggregator pack the manifest-pack
// audit surveys.
const ManifestPack = "icon-sets-bundled"

// BundledPack is ManifestPack's former name, and the same value.
//
// Deprecated: use ManifestPack. Earliest removal: 0.2.0.
const BundledPack = ManifestPack

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
		sets = []string{opts.Set}
	}
	if opts.Package != "" && opts.Set == "" {
		return nil, usage(errf("--package needs --set: a package can only be verified against one set"))
	}

	workDir := opts.WorkDir
	if workDir == "" {
		workDir, err = os.MkdirTemp("", "imt-manifest-pack-audit-")
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

// AuditBundled is AuditManifestPack's former name, and forwards to it.
//
// Deprecated: use AuditManifestPack. Earliest removal: 0.2.0.
func AuditBundled(ctx context.Context, opts AuditOptions) ([]manifestpack.Resolution, error) {
	return AuditManifestPack(ctx, opts)
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

			results[i] = manifestpack.ResolveSet(
				ctx, filepath.Join(filesRoot, set), fetcher, workDir, log, explicit...)
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
