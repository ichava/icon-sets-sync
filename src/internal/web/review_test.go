package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/sinks"
	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

// The openmoji dry run (2026-11-09): a run a guard would stop reads "needs
// review", not "failed"; its page lists every icon with where it went; and it
// offers a forced re-run behind a confirm dialog that names the icons.
func TestADryRunThatNeedsReviewListsEveryIconAndOffersForce(t *testing.T) {
	s, eng := newTestServer(t)
	var removals []sinks.Removal
	for i := 0; i < 15; i++ {
		removals = append(removals, sinks.Removal{Path: "uncategorised/e0" + string(rune('a'+i)) + ".svg", RenamedTo: "flags/f" + string(rune('a'+i)) + ".svg"})
	}
	removals = append(removals, sinks.Removal{Path: "gone.svg"})
	eng.outcome = func(p *config.PackConfig, req RunRequest) *app.Outcome {
		if req.Force {
			return nil
		}
		return &app.Outcome{Pack: p.Pack, Status: app.StatusNeedsReview, DryRun: true,
			Result: pipeline.Result{Success: true, Metrics: map[string]any{"reviews": []sinks.Review{{Target: "/pack/files", Message: "refusing", Removals: removals}}}}}
	}
	c := signIn(t, s)
	path := startRun(t, c, "/packs/icon-sets-flag/sync", url.Values{"mode": {"dry-run"}})
	waitJob(t, s, path)

	if got := s.jobs.Get(strings.TrimPrefix(path, "/jobs/")).Info().Status; got != jobs.StatusNeedsReview {
		t.Fatalf("job status = %s, want %s", got, jobs.StatusNeedsReview)
	}
	body := c.get(path).Body.String()
	for _, want := range []string{
		"Needs review", "uncategorised/e0o.svg", "flags/fo.svg", "gone.svg", "15 renamed, 1 removed",
		`name="force" value="1"`, `name="mode" value="dry-run"`, `data-confirm-title="Re-run with --force?"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("run page lacks %q", want)
		}
	}
	if strings.Contains(body, ">Failed<") {
		t.Error("a run that needs review reads Failed")
	}

	// The forced re-run goes through.
	forced := startRun(t, c, "/packs/icon-sets-flag/sync", url.Values{"mode": {"dry-run"}, "force": {"1"}})
	waitJob(t, s, forced)
	if got := s.jobs.Get(strings.TrimPrefix(forced, "/jobs/")).Info().Status; got != jobs.StatusSucceeded {
		t.Errorf("forced re-run = %s", got)
	}
}

// The Validate page runs on request and lists each pack's findings.
func TestValidatePage(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	if body := c.get("/validate").Body.String(); strings.Contains(body, "pack-report") {
		t.Error("the page checked before it was asked")
	}
	body := c.get("/validate?run=1&parse=1").Body.String()
	for _, want := range []string{`data-role="pack-report"`, "ichava/icon-sets-demo", "bad.svg does not parse", "1 error(s)"} {
		if !strings.Contains(body, want) {
			t.Errorf("validate page lacks %q", want)
		}
	}
}

// The dashboard warns before a run when a tool a real run needs is missing.
func TestDashboardPreflightNamesMissingTools(t *testing.T) {
	s, _ := newTestServer(t, func(o *Options) {
		o.Tools = func() []app.Tool { return []app.Tool{{Name: "gh", Why: "opens the pull request"}} }
	})
	body := signIn(t, s).get("/").Body.String()
	if !strings.Contains(body, `data-role="preflight"`) || !strings.Contains(body, "<code>gh</code> opens the pull request") {
		t.Error("no preflight warning naming gh")
	}
	s2, _ := newTestServer(t)
	if strings.Contains(signIn(t, s2).get("/").Body.String(), `data-role="preflight"`) {
		t.Error("a preflight warning with nothing missing")
	}
}
