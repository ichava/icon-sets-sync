package sinks

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// The openmoji 15.1.0 -> 17.0.0 case (2026-11-09): every icon the removal
// guard refused had a byte-identical file at a new path. A rename is reported
// as a rename, with where it went; only an icon with no copy anywhere in the
// incoming tree is a removal.
func TestRemovalGuardTellsRenamesFromRemovals(t *testing.T) {
	target := writeSVGs(t, filepath.Join(t.TempDir(), "dest"), map[string]string{
		"kept.svg":               "<svg>kept</svg>",
		"uncategorised/e005.svg": "<svg>orca</svg>",
		"uncategorised/gone.svg": "<svg>gone</svg>",
	})
	source := writeSVGs(t, filepath.Join(t.TempDir(), "src"), map[string]string{
		"kept.svg":                "<svg>kept</svg>",
		"animals-nature/orca.svg": "<svg>orca</svg>",
	})

	err := Filesystem{Root: target, RefuseRemovals: true}.Execute(ctxFor(t, source))
	var review *ReviewError
	if !errors.As(err, &review) || !errors.Is(err, ErrNeedsReview) {
		t.Fatalf("err = %v, want a *ReviewError", err)
	}
	want := []Removal{
		{Path: "uncategorised/e005.svg", RenamedTo: "animals-nature/orca.svg"},
		{Path: "uncategorised/gone.svg"},
	}
	if !reflect.DeepEqual(review.Removals, want) {
		t.Fatalf("removals = %+v, want %+v", review.Removals, want)
	}
	for _, s := range []string{"1 renamed", "1 removed", "uncategorised/e005.svg -> animals-nature/orca.svg", "uncategorised/gone.svg"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("message %q does not say %q", err.Error(), s)
		}
	}
}

// The real-run message names every icon, not the first ten.
func TestRemovalGuardNamesEveryIcon(t *testing.T) {
	target := svgTree(t, filepath.Join(t.TempDir(), "dest"), 30)
	source := svgTree(t, filepath.Join(t.TempDir(), "src"), 5)
	err := Filesystem{Root: target, RefuseRemovals: true}.Execute(ctxFor(t, source))
	var review *ReviewError
	if !errors.As(err, &review) {
		t.Fatalf("err = %v, want a *ReviewError", err)
	}
	if len(review.Removals) != 25 {
		t.Fatalf("%d removals, want 25", len(review.Removals))
	}
	last := review.Removals[24].Path
	if !strings.Contains(err.Error(), last) {
		t.Errorf("the message stops before the 25th icon %s", last)
	}
}

// A dry run reports, it does not fail: the pipeline succeeds, the pack is
// untouched, and a "review" metric lists every icon a real run would refuse
// to remove.
func TestDryRunReportsWhatTheGuardWouldRefuse(t *testing.T) {
	target := writeSVGs(t, filepath.Join(t.TempDir(), "dest"), map[string]string{
		"a.svg": "<svg>a</svg>", "b.svg": "<svg>b</svg>",
	})
	source := writeSVGs(t, filepath.Join(t.TempDir(), "src"), map[string]string{
		"renamed-a.svg": "<svg>a</svg>",
	})
	ctx := ctxFor(t, source)
	ctx.Config[pipeline.ConfigDryRun] = true
	if err := (Filesystem{Root: target, RefuseRemovals: true}).Execute(ctx); err != nil {
		t.Fatalf("a dry run must report, not fail: %v", err)
	}
	if n, _ := countSVGs(target); n != 2 {
		t.Errorf("dry run touched the pack: %d icons, want 2", n)
	}
	r, ok := ReviewOf(ctx.Metrics)
	if !ok {
		t.Fatalf("no review metric in %v", ctx.Metrics)
	}
	want := []Removal{{Path: "a.svg", RenamedTo: "renamed-a.svg"}, {Path: "b.svg"}}
	if !reflect.DeepEqual(r.Removals, want) {
		t.Fatalf("review removals = %+v, want %+v", r.Removals, want)
	}
}

// A shrink past MinRetention is a review in a dry run too.
func TestDryRunReportsARetentionShrink(t *testing.T) {
	target := svgTree(t, filepath.Join(t.TempDir(), "dest"), 10)
	source := svgTree(t, filepath.Join(t.TempDir(), "src"), 2)
	ctx := ctxFor(t, source)
	ctx.Config[pipeline.ConfigDryRun] = true
	if err := (Filesystem{Root: target, MinRetention: 0.9}).Execute(ctx); err != nil {
		t.Fatalf("a dry run must report, not fail: %v", err)
	}
	r, ok := ReviewOf(ctx.Metrics)
	if !ok || !strings.Contains(r.Reason, "refusing to replace 10 icons") {
		t.Fatalf("review = %+v, %v", r, ok)
	}
}

// Forced, nothing is refused and nothing needs review.
func TestForcedRunNeedsNoReview(t *testing.T) {
	target := svgTree(t, filepath.Join(t.TempDir(), "dest"), 3)
	source := svgTree(t, filepath.Join(t.TempDir(), "src"), 1)
	ctx := ctxFor(t, source)
	ctx.Config[pipeline.ConfigDryRun] = true
	if err := (Filesystem{Root: target, RefuseRemovals: true, Force: true}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReviewOf(ctx.Metrics); ok {
		t.Error("a forced run recorded a review")
	}
}

// OpenMoji's black flags share one outline: identical bytes at many paths
// are not a rename. A rename needs exactly one new path with the bytes; bytes
// that stay at a path shipped today too make a dropped duplicate.
func TestIdenticalArtworkIsNotARename(t *testing.T) {
	existing := map[string][32]byte{
		"flags/flag-afghanistan.svg": {1}, "flags/flag-albania.svg": {1},
		"uncategorised/2691.svg": {1}, "uncategorised/old.svg": {2}, "uncategorised/twin.svg": {3},
	}
	incoming := map[string][32]byte{
		"flags/flag-afghanistan.svg": {1}, "flags/flag-albania.svg": {1},
		"flags/new-a.svg": {3}, "flags/new-b.svg": {3},
		"animals/orca.svg": {2},
	}
	got := removals(existing, incoming)
	want := []Removal{
		{Path: "uncategorised/2691.svg", SameAs: "flags/flag-afghanistan.svg"},
		{Path: "uncategorised/old.svg", RenamedTo: "animals/orca.svg"},
		{Path: "uncategorised/twin.svg"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("removals = %+v\nwant %+v", got, want)
	}
}
