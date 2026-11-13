package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/recipes"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// RecipeRequest is one direct recipe run, skipping the upstream check.
type RecipeRequest struct {
	Stack     settings.Stack
	Pack      *config.PackConfig
	ConfigDir string

	// Version is the upstream version to fetch. Empty means the pack's
	// current_version.
	Version string

	Logger   *slog.Logger
	Observer pipeline.Observer
}

// Recipe runs one pack's recipe at a version: the body of the `recipe`
// command, so the web UI runs it through the same checks. A missing version
// is a *UsageError; a pack with no sinks outside a dry run is refused before
// anything is fetched.
func Recipe(ctx context.Context, req RecipeRequest) (pipeline.Result, error) {
	pack := req.Pack
	resolved, err := PackSettings(req.Stack, pack)
	if err != nil {
		return pipeline.Result{}, err
	}
	s := resolved.Settings()

	version := req.Version
	if version == "" {
		version = pack.CurrentVersion
	}
	if version == "" {
		return pipeline.Result{}, usage(fmt.Errorf("%s: no version supplied and no current_version in config", pack.Pack))
	}
	if len(pack.Sinks) == 0 && !s.DryRun {
		return pipeline.Result{}, fmt.Errorf("%s declares no sinks, so the recipe would write nothing; add sinks or pass --dry-run", pack.Pack)
	}

	return RunRecipe(ctx, pack, version, recipes.Options{
		DryRun: s.DryRun, ConfigDir: req.ConfigDir, Settings: &resolved,
		Logger: req.Logger, Observer: req.Observer,
	})
}
