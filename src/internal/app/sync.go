package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// packagesDirs is the run-wide packages_dirs setting, applied at startup the
// way allowed_hosts and private_config_dir are.
var (
	packagesMu   sync.RWMutex
	packagesDirs []string
)

// SetPackagesDirs sets the packages_dirs setting: directories searched, in
// order, for a pack's checkout before the workspace setting.
func SetPackagesDirs(dirs []string) {
	packagesMu.Lock()
	defer packagesMu.Unlock()
	packagesDirs = append([]string(nil), dirs...)
}

// PackagesDirs returns the packages_dirs setting.
func PackagesDirs() []string {
	packagesMu.RLock()
	defer packagesMu.RUnlock()
	return append([]string(nil), packagesDirs...)
}

// MissingPackagesDirs lists the packages_dirs entries that are not
// directories, for a warning: a missing entry is skipped, not fatal.
func MissingPackagesDirs(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		if !isDir(d) {
			out = append(out, d)
		}
	}
	return out
}

// ciWorkRoot is the directory sync-pack.yml links to the runner's checkouts.
// A var so tests can stand in for it.
var ciWorkRoot = "/work"

// ApplyWorkspace resolves where a pack's checkout is.
//
// A relative pack_root is joined to the first packages_dirs entry that holds
// it, else to the workspace setting, as before. Every shipped config names an
// absolute /work/<pack>, the layout sync-pack.yml makes on a CI runner; for
// such a root, in order:
//
//  0. each packages_dirs entry (--packages-dir, ICHAVA_PACKAGES_DIRS), when
//     <dir>/<pack> exists;
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
	applyPackagesIn(pack, PackagesDirs(), workspace, cwd, config.ExecutableDir(), ciWorkRoot)
}

func applyWorkspaceIn(pack *config.PackConfig, workspace, cwd, exeDir, ciRoot string) {
	applyPackagesIn(pack, nil, workspace, cwd, exeDir, ciRoot)
}

func applyPackagesIn(pack *config.PackConfig, dirs []string, workspace, cwd, exeDir, ciRoot string) {
	pack.PackRootOrigin = "pack config"
	if pack.PackRoot == "" {
		return
	}
	if !filepath.IsAbs(pack.PackRoot) {
		for _, d := range dirs {
			if c := filepath.Join(d, pack.PackRoot); isDir(c) {
				pack.PackRoot, pack.PackRootOrigin = c, "packages_dirs ("+d+")"
				return
			}
		}
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
	for _, d := range dirs {
		if c := filepath.Join(d, name); isDir(c) {
			pack.PackRoot, pack.PackRootOrigin = c, "packages_dirs ("+d+")"
			return
		}
	}
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
	pack.PackRootOrigin = "pack config (" + pack.PackRoot + " does not exist; sync needs a workspace: set packages_dirs (--packages-dir, ICHAVA_PACKAGES_DIRS) or workspace (ICHAVA_WORKSPACE) to the directory holding the " + name + " checkout)"
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
	// StatusNeedsReview is a run a guard stopped for a person to decide: a
	// refresh would rename or remove shipped icons. A dry run that would hit
	// the guard ends here too, having reported every icon, rather than failing.
	StatusNeedsReview Status = "needs-review"
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

// NeedsReview reports whether a guard stopped the run for a person to decide.
func (o Outcome) NeedsReview() bool { return o.Status == StatusNeedsReview }

// Reviews is what a person has to look at: the dry run's recorded reviews, or
// the real run's refusal.
func (o Outcome) Reviews() []sinks.Review {
	if r, ok := o.Result.Metrics["reviews"].([]sinks.Review); ok && len(r) > 0 {
		return r
	}
	var re *sinks.ReviewError
	if errors.As(o.Result.Err, &re) {
		return []sinks.Review{{Target: re.Target, Reason: re.Reason, Message: re.Error(), Removals: re.Removals}}
	}
	var kc *sinks.KeptCommitError
	if errors.As(o.Result.Err, &kc) {
		return []sinks.Review{{Target: kc.Branch, Reason: kc.Error(), Message: kc.Error()}}
	}
	return nil
}

// AnyNeedsReview reports whether any outcome needs review.
func AnyNeedsReview(outcomes []Outcome) bool {
	for _, o := range outcomes {
		if o.NeedsReview() {
			return true
		}
	}
	return false
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
	case !res.Success && errors.Is(res.Err, sinks.ErrNeedsReview):
		o.Status, o.Err = StatusNeedsReview, fmt.Errorf("%s", res.Error())
	case !res.Success:
		o.Status, o.Err = StatusFailed, fmt.Errorf("%s", res.Error())
	case len(o.Reviews()) > 0:
		o.Status = StatusNeedsReview
		msgs := make([]string, 0)
		for _, r := range o.Reviews() {
			msgs = append(msgs, r.Message)
		}
		o.Err = fmt.Errorf("a real run would stop for review: %s", strings.Join(msgs, "; "))
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
	var all []pipeline.Result
	for _, p := range built {
		result, err := p.RunContext(ctx)
		if err != nil {
			return result, err
		}
		last = result
		all = append(all, result)
		if !result.Success {
			return result, nil
		}
	}
	if reviews := collectReviews(all); len(reviews) > 0 {
		if last.Metrics == nil {
			last.Metrics = map[string]any{}
		}
		last.Metrics["reviews"] = reviews
	}
	return last, nil
}

// collectReviews gathers the dry-run review each pipeline recorded, in order.
func collectReviews(results []pipeline.Result) []sinks.Review {
	var out []sinks.Review
	for _, r := range results {
		if rv, ok := sinks.ReviewOf(r.Metrics); ok {
			out = append(out, rv)
		}
	}
	return out
}
