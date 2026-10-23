package web

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/gitx"
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
