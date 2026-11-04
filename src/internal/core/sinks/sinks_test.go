package sinks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

func svgTree(t *testing.T, dir string, n int) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		name := filepath.Join(dir, "icon"+string(rune('a'+i%26))+string(rune('a'+i/26))+".svg")
		if err := os.WriteFile(name, []byte("<svg/>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func ctxFor(t *testing.T, source string) *pipeline.Context {
	t.Helper()
	c := &pipeline.Context{
		PipelineName: "test", WorkingDir: t.TempDir(),
		Config: map[string]any{}, Extras: map[string]any{}, Metrics: map[string]any{},
	}
	c.SetFetchedPath(source)
	return c
}

// TestRetentionGuardRefusesALargeShrink is the iconoir case: upstream
// reorganised into per-weight directories, so a manifest path that used to
// cover the whole set now covers a fraction of it. The sink wipes before it
// copies, so without this the refresh deletes the difference and reports
// success.
func TestRetentionGuardRefusesALargeShrink(t *testing.T) {
	target := svgTree(t, filepath.Join(t.TempDir(), "dest"), 100)
	source := svgTree(t, filepath.Join(t.TempDir(), "src"), 60)

	err := Filesystem{Root: target, MinRetention: 0.90}.Execute(ctxFor(t, source))
	if err == nil {
		t.Fatal("expected a refusal")
	}

	// And the destination is untouched, which is the whole point of counting
	// before removing.
	n, _ := countSVGs(target)
	if n != 100 {
		t.Errorf("destination now has %d icons, want 100 left exactly as they were", n)
	}
}

func TestRetentionGuardAllowsAnOrdinaryShrink(t *testing.T) {
	target := svgTree(t, filepath.Join(t.TempDir(), "dest"), 100)
	source := svgTree(t, filepath.Join(t.TempDir(), "src"), 95)

	if err := (Filesystem{Root: target, MinRetention: 0.90}).Execute(ctxFor(t, source)); err != nil {
		t.Fatalf("an upstream retiring five icons must not fail a refresh: %v", err)
	}
	if n, _ := countSVGs(target); n != 95 {
		t.Errorf("destination has %d icons, want 95", n)
	}
}

func TestRetentionGuardIsOffByDefault(t *testing.T) {
	target := svgTree(t, filepath.Join(t.TempDir(), "dest"), 100)
	source := svgTree(t, filepath.Join(t.TempDir(), "src"), 1)

	// The single-upstream packs replace a whole tree from one source and have
	// no use for the guard; it must not change their behaviour.
	if err := (Filesystem{Root: target}).Execute(ctxFor(t, source)); err != nil {
		t.Fatalf("the guard must be opt-in: %v", err)
	}
}

func TestRetentionGuardIgnoresAnEmptyDestination(t *testing.T) {
	target := filepath.Join(t.TempDir(), "dest")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	source := svgTree(t, filepath.Join(t.TempDir(), "src"), 5)

	if err := (Filesystem{Root: target, MinRetention: 0.90}).Execute(ctxFor(t, source)); err != nil {
		t.Fatalf("a first sync has nothing to protect: %v", err)
	}
}

// TestRefuseRemovalsKeepsShippedIcons: a refresh that would drop an icon the
// pack ships today fails before the wipe, and leaves the pack as it was.
func TestRefuseRemovalsKeepsShippedIcons(t *testing.T) {
	target := svgTree(t, filepath.Join(t.TempDir(), "dest"), 10)
	source := svgTree(t, filepath.Join(t.TempDir(), "src"), 9) // iconaa..: drops the last one

	err := Filesystem{Root: target, RefuseRemovals: true}.Execute(ctxFor(t, source))
	if err == nil {
		t.Fatal("expected a refusal naming the icon that would go")
	}
	if n, _ := countSVGs(target); n != 10 {
		t.Errorf("destination has %d icons, want all 10 untouched", n)
	}

	if err := (Filesystem{Root: target, RefuseRemovals: true, Force: true}).Execute(ctxFor(t, source)); err != nil {
		t.Fatalf("forced, the refresh must go through: %v", err)
	}
	if n, _ := countSVGs(target); n != 9 {
		t.Errorf("destination has %d icons after a forced refresh, want 9", n)
	}
}

func TestRefuseRemovalsAllowsAdditionsAndIsOffByDefault(t *testing.T) {
	target := svgTree(t, filepath.Join(t.TempDir(), "dest"), 5)
	grown := svgTree(t, filepath.Join(t.TempDir(), "src"), 8)
	if err := (Filesystem{Root: target, RefuseRemovals: true}).Execute(ctxFor(t, grown)); err != nil {
		t.Fatalf("adding icons is not a removal: %v", err)
	}

	smaller := svgTree(t, filepath.Join(t.TempDir(), "src2"), 4)
	if err := (Filesystem{Root: target}).Execute(ctxFor(t, smaller)); err != nil {
		t.Fatalf("without the option the sink is unchanged: %v", err)
	}
}
