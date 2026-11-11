package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/recipes"
	"github.com/ichava/icon-sets-sync/src/internal/core/sinks"
)

func stubCheck(t *testing.T, results ...checker.Result) {
	t.Helper()
	checkAll = func(_ context.Context, _ []*config.PackConfig) []checker.Result { return results }
	t.Cleanup(func() { checkAll = app.CheckAll })
}

var (
	ok      = checker.Result{Package: "ichava/icon-sets-demo", Name: "icon-sets-demo", Current: "1", Latest: "1"}
	behind  = checker.Result{Package: "ichava/icon-sets-demo", Name: "icon-sets-demo", Current: "1", Latest: "2", Stale: true}
	errored = checker.Result{Package: "ichava/icon-sets-demo", Name: "icon-sets-demo", Current: "1", Reason: "unreachable: 403"}
)

// C1: check exited 0 when no upstream could be reached, so a scheduled CI
// check that looked at nothing went green -- the false green CountErrored's
// own comment warns about. --fail-on chooses what fails the run; the
// default stays "stale", as before.
func TestCheckFailOn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result checker.Result
		args   []string
		want   int
	}{
		{"default: stale fails", behind, nil, ExitFailure},
		{"default: an error still passes, as before", errored, nil, ExitOK},
		{"error: an error fails", errored, []string{"--fail-on", "error"}, ExitFailure},
		{"error: stale passes", behind, []string{"--fail-on", "error"}, ExitOK},
		{"any: an error fails", errored, []string{"--fail-on", "any"}, ExitFailure},
		{"any: stale fails", behind, []string{"--fail-on", "any"}, ExitFailure},
		{"any: up to date passes", ok, []string{"--fail-on", "any"}, ExitOK},
		{"never: stale passes", behind, []string{"--fail-on", "never"}, ExitOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			dir := fixture(t, "", "")
			stubCheck(t, tc.result)
			out, code := run(t, append([]string{"--config-dir", dir, "check"}, tc.args...)...)
			if code != tc.want {
				t.Errorf("exit %d, want %d:\n%s", code, tc.want, out)
			}
		})
	}
	isolate(t)
	if out, code := run(t, "--config-dir", fixture(t, "", ""), "check", "--fail-on", "sometimes"); code != ExitUsage {
		t.Errorf("a bad --fail-on exited %d, want %d:\n%s", code, ExitUsage, out)
	}
}

// C2: --github writes what a GitHub Actions job reads: a Markdown table to
// $GITHUB_STEP_SUMMARY, counts and the stale pack list to $GITHUB_OUTPUT,
// and a warning or error annotation per pack that needs attention.
func TestCheckGitHubReport(t *testing.T) {
	isolate(t)
	dir := fixture(t, "", "")
	tmp := t.TempDir()
	summary, output := filepath.Join(tmp, "summary.md"), filepath.Join(tmp, "output")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	t.Setenv("GITHUB_OUTPUT", output)
	stubCheck(t, behind, errored)

	out, code := run(t, "--config-dir", dir, "check", "--github", "--fail-on", "never")
	if code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{"::warning title=icon-sets-demo is behind upstream::", "::error title=icon-sets-demo could not be checked::unreachable: 403"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks annotation %q:\n%s", want, out)
		}
	}
	sum := read(t, summary)
	for _, want := range []string{"| Pack | Current | Latest | Status |", "| icon-sets-demo | 1 | 2 | update available |", "1 behind upstream, 1 could not be checked"} {
		if !strings.Contains(sum, want) {
			t.Errorf("step summary lacks %q:\n%s", want, sum)
		}
	}
	outp := read(t, output)
	for _, want := range []string{"stale=1\n", "errored=1\n", "stale-packs=icon-sets-demo\n"} {
		if !strings.Contains(outp, want) {
			t.Errorf("$GITHUB_OUTPUT lacks %q:\n%s", want, outp)
		}
	}
}

// --github outside Actions is a usage error rather than a silent no-op.
func TestGitHubReportNeedsTheActionsFiles(t *testing.T) {
	isolate(t)
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	t.Setenv("GITHUB_OUTPUT", "")
	stubCheck(t, ok)
	if out, code := run(t, "--config-dir", fixture(t, "", ""), "check", "--github"); code != ExitUsage {
		t.Errorf("exit %d, want %d:\n%s", code, ExitUsage, out)
	}
}

// In --json mode stdout is the JSON document alone: no annotations in it.
func TestGitHubReportKeepsJSONClean(t *testing.T) {
	isolate(t)
	tmp := t.TempDir()
	t.Setenv("GITHUB_STEP_SUMMARY", filepath.Join(tmp, "s"))
	t.Setenv("GITHUB_OUTPUT", filepath.Join(tmp, "o"))
	stubCheck(t, behind)
	out, _ := run(t, "--config-dir", fixture(t, "", ""), "--json", "check", "--github")
	if strings.Contains(out, "::warning") {
		t.Errorf("annotations leaked into --json output:\n%s", out)
	}
}

// C3: --workspace is a flag of its own, the same setting as --set workspace=.
func TestWorkspaceFlag(t *testing.T) {
	isolate(t)
	p := printConfig(t, "--workspace", "/srv/packs")
	if p.Settings["workspace"] != "/srv/packs" || p.Origins["workspace"] != "flag" {
		t.Errorf("workspace = %v from %v", p.Settings["workspace"], p.Origins["workspace"])
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// C2 for sync: the summary names each pack's outcome, and the outputs list
// what synced and what failed, so a later step can act on them.
func TestSyncGitHubReport(t *testing.T) {
	isolate(t)
	tmp := t.TempDir()
	summary, output := filepath.Join(tmp, "summary.md"), filepath.Join(tmp, "output")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	t.Setenv("GITHUB_OUTPUT", output)
	dir := fixture(t, "", "")
	var ran []string
	withSeams(t, fakeChecker{"icon-sets-demo": {Current: "1.0.0", Latest: "2.0.0", Stale: true}}, &ran)

	for _, args := range [][]string{{"sync", "--pack", "icon-sets-demo", "--github"}, {"sync-all", "--github"}} {
		if out, code := run(t, append([]string{"--config-dir", dir}, args...)...); code != ExitOK {
			t.Fatalf("%v exited %d:\n%s", args, code, out)
		}
	}
	sum := read(t, summary)
	if strings.Count(sum, "| ichava/icon-sets-demo | 1.0.0 | 2.0.0 | synced") != 2 {
		t.Errorf("step summary lacks the synced row twice:\n%s", sum)
	}
	if !strings.Contains(read(t, output), "synced=1\nfailed=0\nsynced-packs=ichava/icon-sets-demo\nfailed-packs=\n") {
		t.Errorf("$GITHUB_OUTPUT:\n%s", read(t, output))
	}
}

// A dry run a guard would stop exits 3, lists every icon, and in Actions
// writes a warning annotation and a table of every icon to the summary.
func TestSyncNeedsReviewExitsThreeAndListsEveryIcon(t *testing.T) {
	isolate(t)
	tmp := t.TempDir()
	summary, output := filepath.Join(tmp, "summary.md"), filepath.Join(tmp, "output")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	t.Setenv("GITHUB_OUTPUT", output)
	dir := fixture(t, "", "")
	syncChecker = fakeChecker{"icon-sets-demo": {Current: "1.0.0", Latest: "2.0.0", Stale: true}}
	syncRun = func(_ context.Context, p *config.PackConfig, v string, o recipes.Options) (pipeline.Result, error) {
		return pipeline.Result{PipelineName: p.Name, Success: true, Metrics: map[string]any{"reviews": []sinks.Review{{
			Target: "/pack/files", Message: "refusing to change 2 icon(s)",
			Removals: []sinks.Removal{{Path: "uncategorised/e005.svg", RenamedTo: "animals-nature/orca.svg"}, {Path: "gone.svg"}},
		}}}}, nil
	}
	t.Cleanup(func() { syncChecker, syncRun = nil, nil })

	out, code := run(t, "--config-dir", dir, "sync", "--pack", "icon-sets-demo", "--dry-run", "--github")
	if code != ExitNeedsReview {
		t.Fatalf("exit %d, want %d:\n%s", code, ExitNeedsReview, out)
	}
	for _, want := range []string{"renamed  uncategorised/e005.svg -> animals-nature/orca.svg", "removed  gone.svg", "::warning title=ichava/icon-sets-demo needs review::"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	sum := read(t, summary)
	if !strings.Contains(sum, "| `uncategorised/e005.svg` | `animals-nature/orca.svg` |") || !strings.Contains(sum, "| `gone.svg` | removed |") {
		t.Errorf("summary lacks the icon table:\n%s", sum)
	}
	if !strings.Contains(read(t, output), "review=1\nreview-packs=ichava/icon-sets-demo\n") {
		t.Errorf("$GITHUB_OUTPUT:\n%s", read(t, output))
	}
}
