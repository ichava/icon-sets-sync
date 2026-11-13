package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
)

// The CI surface: --output json, check-upstream --fail-on-outdated, GitHub
// mode detected from the environment, and one ::group:: per synced pack.
func TestCIReadySurface(t *testing.T) {
	isolate(t)
	dir := fixture(t, "", "")
	var ran []string
	withSeams(t, fakeChecker{"icon-sets-demo": {Current: "1.0.0", Latest: "2.0.0", Stale: true}}, &ran)
	stubCheck(t, checker.Result{Package: "ichava/icon-sets-demo", Name: "icon-sets-demo", Current: "1", Latest: "2", Stale: true})

	out, code := run(t, "--config-dir", dir, "--output", "json", "check-upstream", "--fail-on-outdated")
	if code != ExitFailure {
		t.Fatalf("check-upstream --fail-on-outdated on a stale pack exited %d:\n%s", code, out)
	}
	var rows []map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&rows); err != nil {
		t.Fatalf("--output json is not JSON: %v\n%s", err, out)
	}
	if _, code := run(t, "--config-dir", dir, "--output", "yaml", "check"); code != ExitUsage {
		t.Errorf("--output yaml exited %d, want %d", code, ExitUsage)
	}
	if _, code := run(t, "--config-dir", dir, "check", "--fail-on-outdated", "--fail-on", "never"); code != ExitUsage {
		t.Errorf("contradicting flags exited %d", code)
	}

	tmp := t.TempDir()
	summary, output := filepath.Join(tmp, "summary.md"), filepath.Join(tmp, "out")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	t.Setenv("GITHUB_OUTPUT", output)
	out, _ = run(t, "--config-dir", dir, "sync", "--pack", "icon-sets-demo", "--dry-run")
	if !strings.Contains(out, "::group::ichava/icon-sets-demo") || !strings.Contains(out, "::endgroup::") {
		t.Errorf("no group:\n%s", out)
	}
	if b, _ := os.ReadFile(summary); !strings.Contains(string(b), "### Sync") {
		t.Errorf("GitHub mode was not detected; summary:\n%s", b)
	}
	before, _ := os.ReadFile(summary)
	run(t, "--config-dir", dir, "sync", "--pack", "icon-sets-demo", "--dry-run", "--github=false")
	if after, _ := os.ReadFile(summary); len(after) != len(before) {
		t.Error("--github=false still wrote the summary")
	}
}

// doctor reports the tools and fails when a packages_dirs entry is missing.
func TestDoctor(t *testing.T) {
	isolate(t)
	dir := fixture(t, "", "")
	out, code := run(t, "--config-dir", dir, "--json", "--packages-dir", filepath.Join(t.TempDir(), "gone"), "doctor")
	if code != ExitFailure {
		t.Fatalf("doctor with a missing packages dir exited %d:\n%s", code, out)
	}
	var report struct {
		OK      bool     `json:"ok"`
		Missing []string `json:"missing_dirs"`
		Tools   []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	// run returns stdout then stderr; the report is the first JSON value.
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&report); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if report.OK || len(report.Missing) != 1 || len(report.Tools) != 3 {
		t.Errorf("%+v", report)
	}
}
