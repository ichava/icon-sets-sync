package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/recipes"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/core/sinks"
)

// PackSettings resolves one pack's settings over the run-wide stack.
//
// The pack layer is the config's `settings` block. It is parsed here rather
// than at load time because the schema has already checked its shape, and
// only the settings package knows which keys are pack-scoped.
func PackSettings(stack settings.Stack, pack *config.PackConfig) (settings.Resolved, error) {
	var packValues settings.Values
	if len(pack.Settings) > 0 {
		v, err := settings.FromTree(pack.Settings, settings.LayerPack)
		if err != nil {
			return settings.Resolved{}, fmt.Errorf("%s: settings: %w", pack.Name, err)
		}
		packValues = v
	}
	return stack.Resolve(packValues), nil
}

// ciWorkRoot is the directory sync-pack.yml links to the runner's checkouts.
// A var so tests can stand in for it.
var ciWorkRoot = "/work"

// ApplyWorkspace resolves where a pack's checkout is.
//
// A relative pack_root is joined to the workspace setting, as before. Every
// shipped config names an absolute /work/<pack>, the layout sync-pack.yml
// makes on a CI runner; for such a root, in order:
//
//  1. the workspace setting (--set workspace=, ICHAVA_WORKSPACE), when
//     <workspace>/<pack> exists;
//  2. /work/<pack>, when it exists (CI);
//  3. the sibling checkout <toolkit root>/../<pack>, when it exists (a
//     developer machine, where the packs sit beside this repository), with
//     the toolkit root found from the working directory or, failing that,
//     from the executable's own directory;
//  4. the configured value, unchanged, with PackRootOrigin naming the
//     setting that fixes it.
//
// pack.PackRootOrigin records which of these applied.
func ApplyWorkspace(pack *config.PackConfig, workspace string) {
	cwd, _ := os.Getwd()
	applyWorkspaceIn(pack, workspace, cwd, config.ExecutableDir(), ciWorkRoot)
}

func applyWorkspaceIn(pack *config.PackConfig, workspace, cwd, exeDir, ciRoot string) {
	pack.PackRootOrigin = "pack config"
	if pack.PackRoot == "" {
		return
	}
	if !filepath.IsAbs(pack.PackRoot) {
		if workspace != "" {
			pack.PackRoot = filepath.Join(workspace, pack.PackRoot)
			pack.PackRootOrigin = "workspace setting"
		}
		return
	}
	if filepath.Dir(pack.PackRoot) != filepath.Clean(ciRoot) {
		return // an absolute root of the config's own choosing
	}
	name := filepath.Base(pack.PackRoot)
	if workspace != "" {
		if c := filepath.Join(workspace, name); isDir(c) {
			pack.PackRoot, pack.PackRootOrigin = c, "workspace setting"
			return
		}
	}
	if isDir(pack.PackRoot) {
		return
	}
	// From the working directory, then from the binary's own directory: a
	// double-clicked binary runs with cwd=$HOME.
	for _, start := range []string{cwd, exeDir} {
		if start == "" {
			continue
		}
		if root := config.FindToolkitRoot(start); root != "" {
			if c := filepath.Join(filepath.Dir(root), name); isDir(c) {
				pack.PackRoot, pack.PackRootOrigin = c, "sibling checkout of this repository"
				return
			}
		}
	}
	pack.PackRootOrigin = "pack config (" + pack.PackRoot + " does not exist; sync needs a workspace: set ICHAVA_WORKSPACE or --set workspace=<directory holding the " + name + " checkout>)"
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Status is how one pack's sync ended.
type Status string

// The outcomes a sync can have.
const (
	StatusUpToDate   Status = "up-to-date"
	StatusSynced     Status = "synced"
	StatusFailed     Status = "failed"
	StatusUnresolved Status = "unresolved"
)

// Outcome is one pack's result.
type Outcome struct {
	Pack    string
	Status  Status
	Current string
	Latest  string
	DryRun  bool
	Result  pipeline.Result
	Err     error
}

// Failed reports whether the outcome should make the run exit non-zero.
func (o Outcome) Failed() bool { return o.Status == StatusFailed || o.Status == StatusUnresolved }

// LicenceChanged reports whether the outcome failed because the upstream's
// licence no longer matches the licence the pack declares. The outcome's own
// error is flattened to text, so the pipeline result's error is checked too.
func (o Outcome) LicenceChanged() bool {
	return errors.Is(o.Err, sinks.ErrLicenceChanged) || errors.Is(o.Result.Err, sinks.ErrLicenceChanged)
}

// AnyFailed reports whether any outcome failed.
func AnyFailed(outcomes []Outcome) bool {
	for _, o := range outcomes {
		if o.Failed() {
			return true
		}
	}
	return false
}

// Checker is the version check a sync runs first.
type Checker interface {
	CheckPack(ctx context.Context, pack *config.PackConfig) checker.Result
}

// RunFunc builds and runs one pack's recipe.
type RunFunc func(ctx context.Context, pack *config.PackConfig, version string, opts recipes.Options) (pipeline.Result, error)

// SyncOptions configures Sync.
type SyncOptions struct {
	// Stack is the run-wide settings; each pack's own layer is merged in.
	Stack settings.Stack

	// ConfigDir is handed to the recipes (a manifest-pack's set manifest lives there).
	ConfigDir string

	// Concurrency bounds how many packs run at once. Below 1 means 1.
	Concurrency int

	Checker Checker
	Run     RunFunc

	// Logger and Observer are handed to every pack's recipe; see
	// recipes.Options. Nil means slog.Default() and no observer.
	Logger   *slog.Logger
	Observer pipeline.Observer

	// Starting and Done are called around each pack, from that pack's
	// goroutine, for progress output. Either may be nil. Calls are
	// serialised, so a reporter need not lock.
	Starting func(pack *config.PackConfig, result checker.Result, dryRun bool)
	Done     func(Outcome)
}

// Sync refreshes every pack that is behind upstream (or every pack, with
// force), and returns one outcome per pack in input order.
//
// Two things fail loudly here that the Python let pass:
//
//   - An upstream whose latest version cannot be resolved is a failure, not a
//     skip. The Python printed a yellow line and exited 0, so a dead registry
//     or a renamed package looked exactly like an up-to-date pack, every
//     Monday, indefinitely.
//   - A pack that declares no sinks is a failure unless this is a dry run: it
//     would fetch, transform, and then write nothing, and report success.
//
// Neither stops the other packs. One dead registry must not hold back the
// rest of the run; it makes the run exit non-zero once the rest are done.
func Sync(ctx context.Context, packs []*config.PackConfig, opts SyncOptions) []Outcome {
	workers := opts.Concurrency
	if workers < 1 {
		workers = 1
	}
	run := opts.Run
	if run == nil {
		run = RunRecipe
	}
	chk := opts.Checker
	if chk == nil {
		chk = checker.New()
	}

	outcomes := make([]Outcome, len(packs))
	var mu sync.Mutex
	report := func(f func()) {
		mu.Lock()
		defer mu.Unlock()
		f()
	}

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, pack := range packs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, pack *config.PackConfig) {
			defer wg.Done()
			defer func() { <-sem }()

			o := syncOne(ctx, pack, opts, chk, run, report)
			outcomes[i] = o
			if opts.Done != nil {
				report(func() { opts.Done(o) })
			}
		}(i, pack)
	}
	wg.Wait()
	return outcomes
}

func syncOne(ctx context.Context, pack *config.PackConfig, opts SyncOptions, chk Checker, run RunFunc, report func(func())) Outcome {
	o := Outcome{Pack: pack.Pack}

	resolved, err := PackSettings(opts.Stack, pack)
	if err != nil {
		o.Status, o.Err = StatusFailed, err
		return o
	}
	s := resolved.Settings()
	o.DryRun = s.DryRun

	// Checked before any network call: this is a configuration mistake, and
	// it is the same mistake whatever upstream says.
	if len(pack.Sinks) == 0 && !s.DryRun {
		o.Status = StatusFailed
		o.Err = fmt.Errorf("declares no sinks, so a sync would write nothing; add sinks to %s or pass --dry-run", configName(pack))
		return o
	}

	result := chk.CheckPack(ctx, pack)
	o.Current, o.Latest = result.Current, result.Latest

	if result.Latest == "" {
		o.Status = StatusUnresolved
		o.Err = fmt.Errorf("cannot resolve the latest upstream version: %s", result.Reason)
		return o
	}
	if !result.Stale && !s.Force {
		o.Status = StatusUpToDate
		return o
	}

	if opts.Starting != nil {
		report(func() { opts.Starting(pack, result, s.DryRun) })
	}

	res, err := run(ctx, pack, result.Latest, recipes.Options{
		DryRun:    s.DryRun,
		ConfigDir: opts.ConfigDir,
		Settings:  &resolved,
		Logger:    opts.Logger,
		Observer:  opts.Observer,
	})
	o.Result = res
	switch {
	case err != nil:
		o.Status, o.Err = StatusFailed, err
	case !res.Success:
		o.Status, o.Err = StatusFailed, fmt.Errorf("%s", res.Error())
	default:
		o.Status = StatusSynced
	}
	return o
}

func configName(pack *config.PackConfig) string {
	if pack.File != "" {
		return pack.File
	}
	return pack.Name + ".json"
}

// RunRecipe builds a pack's pipelines and runs them in order.
//
// Aborts on the first failure and returns that result, rather than running the
// rest: the emoji recipe's trailing pipeline commits whatever the three asset
// pipelines left on disk, so continuing past a failed fetch would commit a
// half-refreshed pack.
//
// ctx reaches every stage, so cancelling it stops the pipeline in flight and
// skips the pipelines after it.
func RunRecipe(ctx context.Context, pack *config.PackConfig, version string, opts recipes.Options) (pipeline.Result, error) {
	built, err := recipes.Build(pack, version, opts)
	if err != nil {
		return pipeline.Result{}, err
	}

	var last pipeline.Result
	for _, p := range built {
		result, err := p.RunContext(ctx)
		if err != nil {
			return result, err
		}
		last = result
		if !result.Success {
			return result, nil
		}
	}
	return last, nil
}
