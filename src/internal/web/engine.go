package web

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/docfile"
	"github.com/ichava/icon-sets-sync/src/internal/core/gitx"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/recipes"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// Engine is everything the web layer asks of the sync engine. Handlers call
// it and nothing else, so no handler can re-implement a command.
//
// It is the test seam -- the same idea as the CLI's syncChecker/syncRun --
// and has one production implementation, AppEngine, a thin adapter over
// internal/app.
type Engine interface {
	// Packs loads every registered pack, in registry order.
	Packs() ([]*config.PackConfig, error)

	// Check resolves each pack's upstream status. It polls the registries.
	Check(ctx context.Context, packs []*config.PackConfig) []checker.Result

	// PackStats reads a pack repo's local state: SVG count, branch, dirty.
	PackStats(ctx context.Context, pack *config.PackConfig) PackStats

	// Settings is the effective run-wide settings, with where each came from.
	Settings() SettingsView

	// Pack loads one registered pack.
	Pack(slug string) (*config.PackConfig, error)

	// PackSettings is one pack's effective settings, its own layer merged in.
	PackSettings(pack *config.PackConfig) (SettingsView, error)

	// PackNotes returns the authoring annotations (`_note` and the like) of a
	// pack's config file, which loading strips.
	PackNotes(pack *config.PackConfig) map[string]string

	// Sync refreshes packs that are behind upstream (or all, with Force),
	// exactly as `sync` does, with req applied as the flag layer.
	Sync(ctx context.Context, packs []*config.PackConfig, req RunRequest, hooks RunHooks) []app.Outcome

	// Recipe runs one pack's recipe at a version, as `recipe` does.
	Recipe(ctx context.Context, pack *config.PackConfig, version string, req RunRequest, hooks RunHooks) (pipeline.Result, error)
}

// RunRequest is what a run form may set. It enters the settings as the flag
// layer -- the same way the TUI's dry-run choice does -- so nothing below it,
// not a pack config and not the environment, can turn a dry run into a
// real commit.
type RunRequest struct {
	DryRun      bool
	Force       bool
	Concurrency int // 0 leaves the setting alone
}

// RunHooks route a run's output into a job.
type RunHooks struct {
	Logger   *slog.Logger
	Observer pipeline.Observer
	Starting func(pack *config.PackConfig, result checker.Result, dryRun bool)
	Done     func(app.Outcome)
}

// PackStats is the local state of one pack repository.
type PackStats struct {
	SVGs   int
	Branch string
	Dirty  bool
	// Err explains why the repo could not be read; the other fields are then
	// zero.
	Err string
}

// SettingRow is one line of the settings table.
type SettingRow struct {
	Name, Value, Origin, Help string
	PackScoped                bool
}

// SettingsView is the effective settings and the paths they came from.
type SettingsView struct {
	Rows       []SettingRow
	ConfigDir  string
	GlobalPath string
}

// AppEngine is the production Engine over internal/app.
type AppEngine struct {
	ConfigDir  string
	Workspace  string
	GlobalPath string
	Stack      settings.Stack
	Git        *gitx.Git
}

// Packs implements Engine.
func (e *AppEngine) Packs() ([]*config.PackConfig, error) {
	return app.LoadPacksIn(e.ConfigDir, "", e.Workspace)
}

// Check implements Engine.
func (e *AppEngine) Check(ctx context.Context, packs []*config.PackConfig) []checker.Result {
	return app.CheckAll(ctx, packs)
}

// PackStats implements Engine.
func (e *AppEngine) PackStats(ctx context.Context, pack *config.PackConfig) PackStats {
	return ReadPackStats(ctx, e.Git, pack)
}

// Settings implements Engine.
func (e *AppEngine) Settings() SettingsView {
	return BuildSettingsView(e.Stack.Resolve(nil), e.ConfigDir, e.GlobalPath)
}

// Pack implements Engine.
func (e *AppEngine) Pack(slug string) (*config.PackConfig, error) {
	packs, err := app.LoadPacksIn(e.ConfigDir, slug, e.Workspace)
	if err != nil {
		return nil, err
	}
	return packs[0], nil
}

// PackSettings implements Engine.
func (e *AppEngine) PackSettings(pack *config.PackConfig) (SettingsView, error) {
	r, err := app.PackSettings(e.Stack, pack)
	if err != nil {
		return SettingsView{}, err
	}
	return BuildSettingsView(r, e.ConfigDir, e.GlobalPath), nil
}

// PackNotes implements Engine.
func (e *AppEngine) PackNotes(pack *config.PackConfig) map[string]string {
	return ReadNotes(pack.File)
}

// Sync implements Engine.
func (e *AppEngine) Sync(ctx context.Context, packs []*config.PackConfig, req RunRequest, hooks RunHooks) []app.Outcome {
	stack, err := e.withFlags(req)
	if err != nil {
		out := make([]app.Outcome, len(packs))
		for i, p := range packs {
			out[i] = app.Outcome{Pack: p.Pack, Status: app.StatusFailed, Err: err}
		}
		return out
	}
	r := stack.Resolve(nil)
	return app.Sync(ctx, packs, app.SyncOptions{
		Stack: stack, ConfigDir: e.configDir(), Concurrency: r.Int("concurrency"),
		Logger: hooks.Logger, Observer: hooks.Observer, Starting: hooks.Starting, Done: hooks.Done,
	})
}

// Recipe implements Engine.
func (e *AppEngine) Recipe(ctx context.Context, pack *config.PackConfig, version string, req RunRequest, hooks RunHooks) (pipeline.Result, error) {
	stack, err := e.withFlags(req)
	if err != nil {
		return pipeline.Result{}, err
	}
	return app.Recipe(ctx, app.RecipeRequest{
		Stack: stack, Pack: pack, ConfigDir: e.configDir(), Version: version,
		Logger: hooks.Logger, Observer: hooks.Observer,
	})
}

func (e *AppEngine) configDir() string {
	if e.ConfigDir == "" {
		return config.DefaultConfigDir()
	}
	return e.ConfigDir
}

// withFlags returns the stack with a run form's choices as flag-layer values,
// over whatever the command line already put there.
func (e *AppEngine) withFlags(req RunRequest) (settings.Stack, error) {
	return WithRunFlags(e.Stack, req)
}

// WithRunFlags copies stack's flag layer and sets dry_run and force on it --
// always, so the form's choice is the effective one -- and concurrency when
// given.
func WithRunFlags(stack settings.Stack, req RunRequest) (settings.Stack, error) {
	flags := settings.Values{}
	for k, v := range stack.Flags {
		flags[k] = v
	}
	if err := flags.Set("dry_run", req.DryRun); err != nil {
		return stack, err
	}
	if err := flags.Set("force", req.Force); err != nil {
		return stack, err
	}
	if req.Concurrency != 0 {
		if err := flags.Set("concurrency", req.Concurrency); err != nil {
			return stack, err
		}
	}
	stack.Flags = flags
	return stack, nil
}

// ReadNotes returns a config file's top-level annotation keys: those starting
// with "_". Unreadable or non-JSON files yield none.
func ReadNotes(path string) map[string]string {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	canonical, err := docfile.ToJSON(path, data)
	if err != nil {
		return nil
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(canonical, &raw) != nil {
		return nil
	}
	out := map[string]string{}
	for k, v := range raw {
		if !strings.HasPrefix(k, "_") {
			continue
		}
		var s string
		if json.Unmarshal(v, &s) == nil {
			out[k] = s
		} else {
			out[k] = string(v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ReadPackStats counts a pack's vendored SVGs and reads its git state. A
// missing checkout is reported, not fatal: the dashboard still renders.
func ReadPackStats(ctx context.Context, git *gitx.Git, pack *config.PackConfig) PackStats {
	var st PackStats
	root := filepath.Join(pack.PackRoot, "resources", "assets")
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(d.Name()), ".svg") {
			st.SVGs++
		}
		return ctx.Err()
	})
	if err != nil {
		return PackStats{Err: err.Error()}
	}
	if git == nil {
		git = gitx.New()
	}
	if st.Branch, err = git.CurrentBranch(ctx, pack.PackRoot); err != nil {
		st.Err = err.Error()
		return st
	}
	if st.Dirty, err = git.HasUncommittedChanges(ctx, pack.PackRoot); err != nil {
		st.Err = err.Error()
	}
	return st
}

// BuildSettingsView flattens resolved settings into display rows, in the
// registry's order, with each key's origin and help.
func BuildSettingsView(r settings.Resolved, configDir, globalPath string) SettingsView {
	v := SettingsView{ConfigDir: configDir, GlobalPath: globalPath}
	if v.ConfigDir == "" {
		v.ConfigDir = config.DefaultConfigDir()
	}
	for _, k := range settings.Keys {
		var value string
		switch k.Kind {
		case settings.Bool:
			value = boolString(r.Bool(k.Name))
		case settings.Int:
			value = itoa(r.Int(k.Name))
		default:
			value = r.String(k.Name)
		}
		v.Rows = append(v.Rows, SettingRow{
			Name: k.Name, Value: value, Origin: string(r.Origin(k.Name)), Help: k.Help, PackScoped: k.PackScoped,
		})
	}
	return v
}

// RecipeName names the recipe a pack runs, for display.
func RecipeName(p *config.PackConfig) string { return recipes.Select(p) }

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
