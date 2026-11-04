package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/kinds"
)

// kindFixture is the demo pack with a scene in its files/ tree.
func kindFixture(t *testing.T) (configDir, outDir string) {
	t.Helper()
	configDir = fixture(t, "", "")
	packRoot := filepath.Join(configDir, "icon-sets-demo")
	scenes := filepath.Join(packRoot, "resources", "assets", "svg", "files", "scenes")
	if err := os.MkdirAll(scenes, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1200 800">`)
	for _, c := range []string{"111111", "222222", "333333", "444444"} {
		b.WriteString(`<rect fill="#` + c + `" width="1" height="1"/>`)
	}
	b.WriteString(`<!--` + strings.Repeat("x", 21_000) + `--></svg>`)
	write(t, filepath.Join(scenes, "hero.svg"), b.String())
	return configDir, filepath.Join(t.TempDir(), "kind-suggestions")
}

func readRecords(t *testing.T, file string) []kinds.Record {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("no suggestions file: %v", err)
	}
	var recs []kinds.Record
	if err := json.Unmarshal(data, &recs); err != nil {
		t.Fatal(err)
	}
	return recs
}

func TestSyncSuggestKinds(t *testing.T) {
	for _, args := range [][]string{
		{"--suggest-kinds"},
		{"--suggest-kinds=default"},
		{"--suggest-kinds=all"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			isolate(t)
			dir, out := kindFixture(t)
			var ran []string
			withSeams(t, fakeChecker{"icon-sets-demo": {Current: "1.0.0", Latest: "2.0.0", Stale: true}}, &ran)

			full := append([]string{"--config-dir", dir, "sync", "--pack", "icon-sets-demo", "--kind-suggestions-dir", out}, args...)
			output, code := run(t, full...)
			if code != ExitOK {
				t.Fatalf("exit %d:\n%s", code, output)
			}
			recs := readRecords(t, filepath.Join(out, "icon-sets-demo.json"))
			if len(recs) != 1 || recs[0].Path != "scenes/hero" || recs[0].Suggested != kinds.Illustration {
				t.Errorf("records = %+v", recs)
			}
			if !strings.Contains(output, "kind suggestion") {
				t.Errorf("no summary in the output:\n%s", output)
			}
		})
	}
}

func TestSuggestKindsIsOffUnlessAsked(t *testing.T) {
	isolate(t)
	dir, out := kindFixture(t)
	var ran []string
	withSeams(t, fakeChecker{"icon-sets-demo": {Current: "1.0.0", Latest: "2.0.0", Stale: true}}, &ran)

	if output, code := run(t, "--config-dir", dir, "sync", "--pack", "icon-sets-demo", "--kind-suggestions-dir", out); code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, output)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("suggestions were written without --suggest-kinds")
	}
}

func TestSuggestKindsRunsEvenWhenTheSyncFails(t *testing.T) {
	isolate(t)
	dir, out := kindFixture(t)
	withSeams(t, fakeChecker{"icon-sets-demo": {Current: "1.0.0", Reason: "unreachable: 404"}}, &[]string{})

	_, code := run(t, "--config-dir", dir, "sync", "--pack", "icon-sets-demo", "--kind-suggestions-dir", out, "--suggest-kinds")
	if code != ExitFailure {
		t.Fatalf("exit %d, want the sync's own failure", code)
	}
	if recs := readRecords(t, filepath.Join(out, "icon-sets-demo.json")); len(recs) != 1 {
		t.Errorf("records = %+v", recs)
	}
}

func TestSuggestKindsBadModeIsAUsageError(t *testing.T) {
	isolate(t)
	dir, out := kindFixture(t)
	withSeams(t, fakeChecker{"icon-sets-demo": {Latest: "2", Stale: true}}, &[]string{})

	for _, cmd := range [][]string{{"sync", "--pack", "icon-sets-demo"}, {"check"}} {
		args := append([]string{"--config-dir", dir}, cmd...)
		args = append(args, "--kind-suggestions-dir", out, "--suggest-kinds=everything")
		if output, code := run(t, args...); code != ExitUsage {
			t.Errorf("%v: exit %d, want %d:\n%s", cmd, code, ExitUsage, output)
		}
	}
}

func TestCheckSuggestKindsKeepsItsOwnExitCode(t *testing.T) {
	isolate(t)
	dir, out := kindFixture(t)
	checkAll = func(_ context.Context, _ []*config.PackConfig) []checker.Result {
		return []checker.Result{{Package: "icon-sets-demo", Current: "1.0.0", Latest: "2.0.0", Stale: true}}
	}
	t.Cleanup(func() { checkAll = app.CheckAll })

	_, code := run(t, "--config-dir", dir, "check", "--kind-suggestions-dir", out, "--suggest-kinds")
	if code != ExitFailure {
		t.Errorf("exit %d, want 1 for a stale pack", code)
	}
	if recs := readRecords(t, filepath.Join(out, "icon-sets-demo.json")); len(recs) != 1 {
		t.Errorf("records = %+v", recs)
	}

	checkAll = func(_ context.Context, _ []*config.PackConfig) []checker.Result {
		return []checker.Result{{Package: "icon-sets-demo", Current: "2.0.0", Latest: "2.0.0"}}
	}
	if _, code := run(t, "--config-dir", dir, "check", "--kind-suggestions-dir", out, "--suggest-kinds"); code != ExitOK {
		t.Errorf("exit %d, want 0 for a current pack", code)
	}
}
