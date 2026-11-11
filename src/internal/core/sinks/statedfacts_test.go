package sinks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/gitx"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// statedPack builds a committed pack shaped like icon-sets-carbon on 2026-11-11: two set
// folders (size-32 with 2,773 SVGs, size-16 with 68), a README and composer.json that state
// those figures and the vendored version, and a test that pins them.
func statedPack(t *testing.T) string {
	t.Helper()
	pack := t.TempDir()
	svgs(t, filepath.Join(pack, "resources/assets/svg/files/size-32"), 2773)
	svgs(t, filepath.Join(pack, "resources/assets/svg/files/size-16"), 68)
	write(t, filepath.Join(pack, "README.md"),
		"> Carbon Icons: 2,841 SVGs across `size-32` (2,773), `size-16` (68), vendored from `@carbon/icons` 11.91.0.\n")
	write(t, filepath.Join(pack, "composer.json"),
		`{"description": "2,841 Carbon SVG icons (2,773 at 32 px, 68 at 16 px) from @carbon/icons 11.91.0, for Laravel 13. Released 2026."}`+"\n")
	write(t, filepath.Join(pack, "tests/Feature/IconsTest.php"),
		"<?php\n$expected = ['size-32' => 2773, 'size-16' => 68];\nexpect($total)->toBe(2841);\n")
	write(t, filepath.Join(pack, "resources/assets/svg/config.json"),
		`{"upstream": {"current_version": "11.91.0"}}`+"\n")
	gitIn(t, pack, "init", "-q", "-b", "main")
	gitIn(t, pack, "add", "-A")
	gitIn(t, pack, "commit", "-q", "-m", "Initial release")
	return pack
}

func svgs(t *testing.T, dir string, n int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		write(t, filepath.Join(dir, "i"+itoa(i)+".svg"), "<svg/>")
	}
}

func itoa(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return digits[i : i+1]
	}
	return itoa(i/10) + digits[i%10:i%10+1]
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func runStatedFacts(t *testing.T, pack, version string) *pipeline.Context {
	t.Helper()
	ctx := &pipeline.Context{PipelineName: "carbon:commit", WorkingDir: t.TempDir(),
		Config: map[string]any{}, Extras: map[string]any{}, Metrics: map[string]any{}}
	ctx.SetFetchedVersion(version)
	g := gitx.New()
	g.Env = []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	p := &config.PackConfig{Pack: "icon-sets-carbon", PackRoot: pack,
		VersionFile: "resources/assets/svg/config.json", VersionKeys: []string{"upstream.current_version"}}
	if err := (StatedFacts{Pack: p, Git: g}).Execute(ctx); err != nil {
		t.Fatalf("StatedFacts: %v", err)
	}
	return ctx
}

// The carbon 11.92.0 refresh added six 32 px icons. The pack's guard pins each count on
// purpose, so the bump opened red until the test, README and composer.json were edited by
// hand (icon-sets-carbon#7). The sync now moves every stated figure with the assets.
func TestStatedFactsMoveTheCountsAndVersionWithTheAssets(t *testing.T) {
	pack := statedPack(t)
	svgs(t, filepath.Join(pack, "resources/assets/svg/files/size-32"), 2779) // six more

	runStatedFacts(t, pack, "11.92.0")

	readme := read(t, filepath.Join(pack, "README.md"))
	for _, want := range []string{"2,847 SVGs", "`size-32` (2,779)", "`size-16` (68)", "`@carbon/icons` 11.92.0"} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md does not say %q:\n%s", want, readme)
		}
	}
	composer := read(t, filepath.Join(pack, "composer.json"))
	for _, want := range []string{"2,847 Carbon", "(2,779 at 32 px, 68 at 16 px)", "@carbon/icons 11.92.0", "Released 2026."} {
		if !strings.Contains(composer, want) {
			t.Errorf("composer.json does not say %q:\n%s", want, composer)
		}
	}
	tests := read(t, filepath.Join(pack, "tests/Feature/IconsTest.php"))
	for _, want := range []string{"'size-32' => 2779", "'size-16' => 68", "toBe(2847)"} {
		if !strings.Contains(tests, want) {
			t.Errorf("IconsTest.php does not say %q:\n%s", want, tests)
		}
	}
}

// Only whole figures move: 2,773 inside 12,773 or 2773.5, and a count under ten, are
// some other number.
func TestStatedFactsOnlyRewriteWholeFigures(t *testing.T) {
	pack := statedPack(t)
	write(t, filepath.Join(pack, "README.md"), "2,841 SVGs. Not 12,841, not 2841.5, not 2,8410.\n")
	gitIn(t, pack, "commit", "-qam", "readme")
	svgs(t, filepath.Join(pack, "resources/assets/svg/files/size-32"), 2779)

	runStatedFacts(t, pack, "11.91.0")

	if got, want := read(t, filepath.Join(pack, "README.md")), "2,847 SVGs. Not 12,841, not 2841.5, not 2,8410.\n"; got != want {
		t.Errorf("README.md = %q, want %q", got, want)
	}
}

// Nothing changed: no file is touched, and the metric says so.
func TestStatedFactsLeaveAnUnchangedPackAlone(t *testing.T) {
	pack := statedPack(t)
	before := read(t, filepath.Join(pack, "README.md"))

	ctx := runStatedFacts(t, pack, "11.91.0")

	if got := read(t, filepath.Join(pack, "README.md")); got != before {
		t.Errorf("README.md changed:\n%s", got)
	}
	if m, _ := ctx.Metrics["stated_facts"].(map[string]any); m["status"] != "unchanged" {
		t.Errorf("stated_facts metric = %v, want status unchanged", ctx.Metrics["stated_facts"])
	}
}

// A previous figure that no file states is reported, never guessed at (lucide's README
// still said 2,313 when its test pinned 2,322).
func TestStatedFactsReportAFigureNoFileStates(t *testing.T) {
	pack := statedPack(t)
	write(t, filepath.Join(pack, "README.md"), "Carbon icons, many of them.\n")
	write(t, filepath.Join(pack, "composer.json"), `{"description": "Carbon icons."}`+"\n")
	write(t, filepath.Join(pack, "tests/Feature/IconsTest.php"), "<?php\n")
	gitIn(t, pack, "commit", "-qam", "no figures")
	svgs(t, filepath.Join(pack, "resources/assets/svg/files/size-32"), 2779)

	ctx := runStatedFacts(t, pack, "11.91.0")

	m, _ := ctx.Metrics["stated_facts"].(map[string]any)
	unstated, _ := m["unstated"].([]string)
	if !strings.Contains(strings.Join(unstated, " "), "2,841") {
		t.Errorf("unstated = %v, want the old total 2,841 reported", unstated)
	}
}
