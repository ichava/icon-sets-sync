package web

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/sinks"
)

// The run summary on the job page is the per-pack diff: added, removed and
// changed icons, beside the copy count it always showed.
func TestSummaryShowsTheDiff(t *testing.T) {
	got := summary(map[string]any{"filesystem": map[string]any{
		"copied": 3, "added": 1, "removed": 2, "changed": 1, "added_names": []string{"new.svg"},
	}})
	for _, want := range []string{"filesystem copied 3", "filesystem added 1", "filesystem removed 2", "filesystem changed 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "new.svg") {
		t.Errorf("the one-line summary must not list names: %q", got)
	}
}

func TestPackDetailNamesALicenceChange(t *testing.T) {
	changed := app.Outcome{Status: app.StatusFailed, Err: errors.New("LicenceTexts: licence changed"),
		Result: pipeline.Result{Err: fmt.Errorf("LicenceTexts x: %w", sinks.ErrLicenceChanged)}}
	if d := packDetail(changed); !strings.Contains(d, "licence changed upstream") {
		t.Errorf("detail %q does not name the licence change", d)
	}
	other := app.Outcome{Status: app.StatusFailed, Err: errors.New("npm pack: 404")}
	if d := packDetail(other); d != "" {
		t.Errorf("an unrelated failure got detail %q", d)
	}
	synced := app.Outcome{Status: app.StatusSynced,
		Result: pipeline.Result{Metrics: map[string]any{"filesystem": map[string]any{"copied": 3, "added": 1}}}}
	if d := packDetail(synced); !strings.Contains(d, "filesystem added 1") {
		t.Errorf("synced detail %q", d)
	}
	if d := packDetail(app.Outcome{Status: app.StatusUpToDate, Current: "1.2.3"}); d != "up to date at 1.2.3" {
		t.Errorf("up-to-date detail %q", d)
	}
}

// U10: a pack stopped by Cancel was reported "failed: SplitVariants:
// context canceled". It is reported cancelled, whichever error carries it.
func TestPackStatusNamesACancelledPack(t *testing.T) {
	cancelled := app.Outcome{Status: app.StatusFailed, Err: fmt.Errorf("%s", "SplitVariants: context canceled"),
		Result: pipeline.Result{Err: fmt.Errorf("stage: %w", context.Canceled)}}
	if got := packStatus(cancelled); got != "cancelled" {
		t.Errorf("packStatus = %q, want cancelled", got)
	}
	if got := packStatus(app.Outcome{Status: app.StatusFailed, Err: errors.New("boom")}); got != "failed" {
		t.Errorf("packStatus = %q, want failed", got)
	}
	if got := packStatus(app.Outcome{Status: app.StatusSynced}); got != "synced" {
		t.Errorf("packStatus = %q, want synced", got)
	}
}
