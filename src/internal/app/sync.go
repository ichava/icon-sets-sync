package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/ichava/maintainer-toolkit/src/internal/core/checker"
	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
	"github.com/ichava/maintainer-toolkit/src/internal/core/recipes"
	"github.com/ichava/maintainer-toolkit/src/internal/core/settings"
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

// ApplyWorkspace resolves a relative pack_root against the workspace setting.
//
// Every shipped config names an absolute /work/<pack>, which is where the
// Docker image sees the mounted checkouts. A relative root is for running the
// binary on a laptop against sibling checkouts without editing four configs.
func ApplyWorkspace(pack *config.PackConfig, workspace string) {
	if workspace == "" || pack.PackRoot == "" || filepath.IsAbs(pack.PackRoot) {
		return
	}
	pack.PackRoot = filepath.Join(workspace, pack.PackRoot)
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

	// ConfigDir is handed to the recipes (the bundled manifest lives there).
	ConfigDir string

	// Concurrency bounds how many packs run at once. Below 1 means 1.
	Concurrency int

	Checker Checker
	Run     RunFunc

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
func RunRecipe(_ context.Context, pack *config.PackConfig, version string, opts recipes.Options) (pipeline.Result, error) {
	built, err := recipes.Build(pack, version, opts)
	if err != nil {
		return pipeline.Result{}, err
	}

	var last pipeline.Result
	for _, p := range built {
		result, err := p.Run()
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
