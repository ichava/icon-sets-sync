package app

import (
	"context"
	"errors"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/recipes"
)

// A cancelled context stops RunRecipe before the first stage, so nothing is
// fetched and nothing is committed. Before the context was threaded through,
// RunRecipe discarded it and Ctrl-C could only kill the process.
func TestRunRecipeHonoursACancelledContext(t *testing.T) {
	p := &config.PackConfig{
		Name: "icon-sets-demo", Pack: "ichava/icon-sets-demo", PackRoot: t.TempDir(),
		Source: config.SourceConfig{Type: "npm", Package: "demo-package-that-must-not-be-fetched"},
		Sinks:  []config.SinkConfig{{Type: "filesystem", Root: t.TempDir()}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := RunRecipe(ctx, p, "1.0.0", recipes.Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("result %+v", result)
	}
}
