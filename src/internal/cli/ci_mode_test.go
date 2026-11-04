package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/recipes"
	"github.com/ichava/icon-sets-sync/src/internal/core/sinks"
)

// CI mode names the spec uses: `run --pack <slug> --non-interactive` is the
// same command as `sync --pack <slug> --no-input`.
func TestRunAndNonInteractiveAreTheSyncNames(t *testing.T) {
	isolate(t)
	dir := fixture(t, "", "")
	var ran []string
	withSeams(t, fakeChecker{"icon-sets-demo": {Current: "1.0.0", Latest: "2.0.0", Stale: true}}, &ran)

	out, code := run(t, "--config-dir", dir, "run", "--pack", "icon-sets-demo", "--non-interactive", "--dry-run")
	if code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if got := strings.Join(ran, ","); got != "icon-sets-demo@2.0.0:dry-run" {
		t.Errorf("ran %q", got)
	}
	if !g.noInput {
		t.Error("--non-interactive did not set no-input")
	}
}

// A relicensed upstream is reported as such in --json, so a workflow can open
// an issue for a person instead of a pull request.
func TestSyncJSONFlagsALicenceChange(t *testing.T) {
	isolate(t)
	dir := fixture(t, "", "")
	syncChecker = fakeChecker{"icon-sets-demo": {Current: "1.0.0", Latest: "2.0.0", Stale: true}}
	syncRun = func(_ context.Context, p *config.PackConfig, _ string, _ recipes.Options) (pipeline.Result, error) {
		return pipeline.Result{PipelineName: p.Name, FailedStage: "LicenceTexts",
			Err: fmt.Errorf("LicenceTexts x: %w: the pack declares CC-BY-4.0", sinks.ErrLicenceChanged)}, nil
	}
	t.Cleanup(func() { syncChecker, syncRun = nil, nil })

	out, code := run(t, "--config-dir", dir, "--json", "sync", "--pack", "icon-sets-demo")
	if code != ExitFailure {
		t.Fatalf("exit %d, want %d:\n%s", code, ExitFailure, out)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out[:strings.LastIndex(out, "]")+1]), &rows); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(rows) != 1 || rows[0]["licence_changed"] != true {
		t.Fatalf("rows = %v, want licence_changed true", rows)
	}
}

// Any other failure must not claim a licence change.
func TestSyncJSONLeavesOtherFailuresAlone(t *testing.T) {
	isolate(t)
	dir := fixture(t, "", "")
	var ran []string
	withSeams(t, fakeChecker{"icon-sets-demo": checker.Result{Current: "1.0.0", Reason: "unreachable: 404"}}, &ran)

	out, _ := run(t, "--config-dir", dir, "--json", "sync", "--pack", "icon-sets-demo")
	if strings.Contains(out, "licence_changed") {
		t.Fatalf("an unrelated failure reported a licence change:\n%s", out)
	}
}
