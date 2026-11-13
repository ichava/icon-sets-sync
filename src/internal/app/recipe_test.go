package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

func recipePack(t *testing.T, sinks ...config.SinkConfig) *config.PackConfig {
	return &config.PackConfig{
		Name: "icon-sets-demo", Pack: "ichava/icon-sets-demo", PackRoot: t.TempDir(),
		Source: config.SourceConfig{Type: "npm", Package: "demo-package-that-must-not-be-fetched"},
		Sinks:  sinks,
	}
}

func TestRecipeNeedsAVersion(t *testing.T) {
	_, err := Recipe(context.Background(), RecipeRequest{Pack: recipePack(t)})
	if !IsUsage(err) || !strings.Contains(err.Error(), "no version supplied") {
		t.Fatalf("err %v", err)
	}
}

func TestRecipeRefusesNoSinksOutsideADryRun(t *testing.T) {
	_, err := Recipe(context.Background(), RecipeRequest{Pack: recipePack(t), Version: "1.0.0"})
	if err == nil || IsUsage(err) || !strings.Contains(err.Error(), "declares no sinks") {
		t.Fatalf("err %v", err)
	}
}

// The flag layer decides the dry run, and a dry run with no sinks is allowed:
// the run starts and stops at the cancelled context instead of fetching.
func TestRecipeRunsUnderTheStacksDryRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stack := settings.Stack{Flags: settings.Values{"dry_run": true}}
	p := recipePack(t)
	p.CurrentVersion = "1.0.0"
	result, err := Recipe(ctx, RecipeRequest{Stack: stack, Pack: p})
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("result %+v", result)
	}
}
