package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/gitx"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/recipes"
	"github.com/ichava/icon-sets-sync/src/internal/core/sinks"
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

// A recipe runs several pipelines (openmoji: colour, then black). Each one's
// dry-run review is carried to the result RunRecipe returns, so the second
// pipeline's clean finish cannot hide the first one's review.
func TestCollectReviewsKeepsEveryPipelinesReview(t *testing.T) {
	a := pipeline.Result{PipelineName: "color", Success: true, Metrics: map[string]any{"review": map[string]any{"message": "a", "target": "ta"}}}
	b := pipeline.Result{PipelineName: "black", Success: true, Metrics: map[string]any{"review": map[string]any{"message": "b", "target": "tb"}}}
	c := pipeline.Result{PipelineName: "commit", Success: true, Metrics: map[string]any{}}
	got := collectReviews([]pipeline.Result{a, b, c})
	if len(got) != 2 || got[0].Message != "a" || got[1].Message != "b" {
		t.Fatalf("reviews = %+v", got)
	}
}

// A person's commit that no longer applies on the sync's branch is a review,
// not a failure, and the review names it.
func TestAKeptCommitThatNoLongerAppliesIsAReview(t *testing.T) {
	err := &sinks.KeptCommitError{Branch: "chore/sync", Commit: gitx.Commit{SHA: "abcdef1234", Author: "P <p@x>", Subject: "test: fix"}}
	if !errors.Is(err, sinks.ErrNeedsReview) {
		t.Fatal("not a review")
	}
	o := Outcome{Result: pipeline.Result{Err: err}}
	rs := o.Reviews()
	if len(rs) != 1 || rs[0].Target != "chore/sync" || !strings.Contains(rs[0].Message, "abcdef1") {
		t.Fatalf("%+v", rs)
	}
}
