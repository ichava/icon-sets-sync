package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/bundled"
	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

type auditData struct {
	Job      *jobs.Info
	Live     bool
	Results  []bundled.Resolution
	Summary  map[bundled.Confidence]int
	Order    []bundled.Confidence
	Set, Pkg string
}

// latestAudit finds the newest audit run, in memory or in the history.
func (s *Server) latestAudit() (jobs.Info, []jobs.Event, bool) {
	for _, info := range s.jobs.List() {
		if info.Kind == jobs.KindAudit {
			i, events, _, err := s.lookupJob(info.ID)
			if err == nil {
				return i, events, true
			}
		}
	}
	return jobs.Info{}, nil, false
}

func auditResults(events []jobs.Event) []bundled.Resolution {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == jobs.EventResult {
			var rs []bundled.Resolution
			if json.Unmarshal(events[i].Data, &rs) == nil {
				return rs
			}
		}
	}
	return nil
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	d := auditData{
		Order: []bundled.Confidence{bundled.ConfidenceExact, bundled.ConfidenceStrong, bundled.ConfidencePackage,
			bundled.ConfidenceAmbiguous, bundled.ConfidencePartial, bundled.ConfidenceNone},
	}
	if info, events, ok := s.latestAudit(); ok {
		d.Job, d.Live = &info, info.Status == jobs.StatusRunning
		d.Results = auditResults(events)
		d.Summary = bundled.Summary(d.Results)
	}
	v := s.newView(r, "audit", "Bundled audit", d)
	v.Reload = d.Live
	s.render(w, http.StatusOK, "audit", v)
}

// npmName is the shape of an npm package name; the audit hands it to npm.
var npmName = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9][a-z0-9._~-]*$`)

func (s *Server) handleAuditStart(w http.ResponseWriter, r *http.Request) {
	req := AuditRequest{Set: strings.TrimSpace(r.PostFormValue("set")), Package: strings.TrimSpace(r.PostFormValue("package"))}
	if req.Set != "" && !validSlug(req.Set) {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", "A set name is lowercase letters, digits and hyphens.")
		return
	}
	if req.Package != "" && (!npmName.MatchString(req.Package) || len(req.Package) > 214) {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", "That is not an npm package name.")
		return
	}
	if req.Package != "" && req.Set == "" {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", "A package can only be verified against one set; name the set too.")
		return
	}
	label := "every set"
	if req.Set != "" {
		label = "set " + req.Set
		if req.Package != "" {
			label += " against " + req.Package
		}
	}
	// The audit reads the bundled pack's tree; holding its lock keeps a sync
	// from rewriting that tree mid-survey.
	j, err := s.jobs.Start(jobs.Spec{Kind: jobs.KindAudit, Packs: []string{app.BundledPack}, DryRun: true, Label: label},
		func(ctx context.Context, j *jobs.Job) (jobs.Status, error) {
			done := 0
			results, err := s.opts.Engine.AuditBundled(ctx, req, AuditHooks{
				Logger: slog.New(jobs.Handler(j, slog.LevelInfo)),
				Starting: func(n int, root string) {
					j.Logf("info", "auditing %d set(s) from %s", n, root)
				},
				SetDone: func(res bundled.Resolution) {
					done++
					j.PackDone(res.Set, string(res.Confidence), "", fmt.Sprintf("%d/%d identical", res.Identical, res.Icons))
				},
			})
			if err != nil {
				return jobs.StatusFailed, err
			}
			if err := j.Record(results); err != nil {
				return jobs.StatusFailed, err
			}
			sum := bundled.Summary(results)
			j.Logf("info", "%d exact, %d strong, %d upstream-identified, %d partial, %d unresolved",
				sum[bundled.ConfidenceExact], sum[bundled.ConfidenceStrong], sum[bundled.ConfidencePackage],
				sum[bundled.ConfidencePartial], sum[bundled.ConfidenceNone])
			// A survey, not a gate: unresolved sets are work for a person, not a
			// failed audit -- the same reason `bundled audit` exits 0.
			return jobs.StatusSucceeded, nil
		})
	s.started(w, r, j, err)
}

// handleAuditManifest downloads an audit's resolutions as a set manifest,
// the input a per-set refresh needs, like `bundled audit --json`.
func (s *Server) handleAuditManifest(w http.ResponseWriter, r *http.Request) {
	_, events, _, err := s.lookupJob(r.PathValue("id"))
	results := auditResults(events)
	if err != nil || results == nil {
		s.renderError(w, r, http.StatusNotFound, "No audit results", "That run is not a finished audit.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="icon-sets-bundled.sets.json"`)
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(bundled.ManifestFromResolutions("ichava/"+app.BundledPack, results))
}
