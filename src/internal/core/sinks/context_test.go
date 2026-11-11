package sinks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/gitx"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

type runKey struct{}

// seenRunner records the context every git/gh call received.
type seenRunner struct{ seen []any }

func (r *seenRunner) Run(ctx context.Context, _ string, _ []string, name string, args ...string) (string, error) {
	r.seen = append(r.seen, ctx.Value(runKey{}))
	if name == "git" && len(args) > 0 && args[0] == "status" {
		return "", nil // clean tree: the sink stops after this call
	}
	return "", nil
}

type versionSource struct{ pipeline.SourceKind }

func (versionSource) Name() string { return "VersionSource" }
func (versionSource) Execute(ctx *pipeline.Context) error {
	ctx.SetFetchedVersion("2.0.0")
	return nil
}

// The git sink runs git and gh with the run's context, so cancelling a run
// kills an in-flight push rather than waiting for it.
func TestGitBranchUsesTheRunContext(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &seenRunner{}
	ctx := context.WithValue(context.Background(), runKey{}, "run-7")
	result, err := pipeline.Named("demo").
		Source(versionSource{}).
		Sink(GitBranch{RepoRoot: repo, Git: &gitx.Git{Run: runner}}).
		RunContext(ctx)
	if err != nil || !result.Success {
		t.Fatalf("result %+v, err %v", result, err)
	}
	if len(runner.seen) == 0 {
		t.Fatal("the sink made no git call")
	}
	for i, v := range runner.seen {
		if v != "run-7" {
			t.Errorf("git call %d ran with %v, not the run's context", i, v)
		}
	}
}
