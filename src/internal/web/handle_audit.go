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
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

type auditData struct {
	Job      *jobs.Info
	Live     bool
	Results  []manifestpack.Resolution
	Summary  map[manifestpack.Confidence]int
	Order    []manifestpack.Confidence
	Set, Pkg string
	// Unavailable is set when no audit can start because the manifest
	// pack's private config did not load (U12). The error is not shown: it
	// names a private pack, which a public page must not.
	Unavailable bool
	// PrivateDir is the private config directory this server has, when it
	// has one that still does not load the pack; Candidates are the pack
	// checkouts' private config directories found on disk, each a one-click
	// fix.
	PrivateDir string
	Candidates []string
	// Sets are the pack's sets, offered as the set field's suggestions.
	Sets []string
	// Unknown is a refused start for a set the pack does not have.
	Unknown *app.UnknownSetError
	// Errors counts the sets the latest audit could not read; Problems
	// explain each one.
	Errors   int
	Problems map[string]auditProblem
	// Enabled is set on the reload after the private config was loaded;
	// SaveErr is why loading it failed.
	Enabled bool
	SaveErr string
}

// auditProblem is why a set could not be audited, and what fixes it.
type auditProblem struct {
	Cause, Fix  string
	Suggestions []string
}

// legacyReadError matches the note an audit recorded for a set it could not
// read, before Resolution had an Error field (until 2026-11-09).
var legacyReadError = regexp.MustCompile(`^(lstat|stat|open) .+: no such file or directory$`)

// explainAuditErrors moves a legacy read error from Note to Error, and
// writes each errored set's cause and fix.
func explainAuditErrors(rs []manifestpack.Resolution, sets []string) map[string]auditProblem {
	out := map[string]auditProblem{}
	for i := range rs {
		if rs[i].Error == "" && legacyReadError.MatchString(rs[i].Note) {
			rs[i].Error, rs[i].Note = rs[i].Note, ""
		}
		if rs[i].Error == "" {
			continue
		}
		p := auditProblem{Cause: rs[i].Error, Fix: "Check the pack checkout: Settings > Where the ichava packs live."}
		if strings.Contains(rs[i].Error, "no such file or directory") {
			p.Cause = "The pack has no set directory named " + rs[i].Set + ": it was renamed, or never existed."
			p.Suggestions = app.SuggestSets(rs[i].Set, sets)
			switch {
			case len(p.Suggestions) > 0:
				p.Fix = "Audit the set by its directory name:"
			case len(sets) == 0:
				p.Fix = "Load the aggregator pack's config (above); the set field then lists the pack's real set names."
			default:
				p.Fix = "Leave the set empty to audit every set, or pick one from the set field's list."
			}
		}
		out[rs[i].Set] = p
	}
	return out
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

func auditResults(events []jobs.Event) []manifestpack.Resolution {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == jobs.EventResult {
			var rs []manifestpack.Resolution
			if json.Unmarshal(events[i].Data, &rs) == nil {
				return rs
			}
		}
	}
	return nil
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	d := auditData{
		Order: []manifestpack.Confidence{manifestpack.ConfidenceExact, manifestpack.ConfidenceStrong, manifestpack.ConfidencePackage,
			manifestpack.ConfidenceAmbiguous, manifestpack.ConfidencePartial, manifestpack.ConfidenceNone},
	}
	d.Enabled = r.URL.Query().Get("enabled") == "1"
	s.renderAudit(w, r, http.StatusOK, d)
}

// renderAudit fills the page's state and renders it.
func (s *Server) renderAudit(w http.ResponseWriter, r *http.Request, status int, d auditData) {
	d.Order = []manifestpack.Confidence{manifestpack.ConfidenceExact, manifestpack.ConfidenceStrong, manifestpack.ConfidencePackage,
		manifestpack.ConfidenceAmbiguous, manifestpack.ConfidencePartial, manifestpack.ConfidenceNone}
	if _, err := s.opts.Engine.Pack(app.ManifestPack); err != nil {
		d.Unavailable = true
		d.PrivateDir = config.PrivateConfigDir()
		if !s.opts.ReadOnly {
			d.Candidates = s.opts.Engine.PrivateConfigCandidates()
		}
	} else if sets, err := s.opts.Engine.AuditSets(); err == nil {
		d.Sets = sets
	}
	if info, events, ok := s.latestAudit(); ok {
		d.Job, d.Live = &info, info.Status == jobs.StatusRunning
		d.Results = auditResults(events)
		d.Problems = explainAuditErrors(d.Results, d.Sets)
		d.Errors = manifestpack.CountErrors(d.Results)
		// An unreadable set has no confidence to count; it is counted once,
		// as could not read.
		var surveyed []manifestpack.Resolution
		for _, res := range d.Results {
			if res.Error == "" {
				surveyed = append(surveyed, res)
			}
		}
		d.Summary = manifestpack.Summary(surveyed)
	}
	v := s.newView(r, "audit", "Manifest pack audit", d)
	v.Reload = d.Live
	s.render(w, status, "audit", v)
}

// handlePrivateConfigDir loads a private config directory found on disk:
// it writes private_config_dir into the global config, which applies it at
// once. Only a directory the server itself found is accepted, so the form
// cannot point the toolkit anywhere else.
func (s *Server) handlePrivateConfigDir(w http.ResponseWriter, r *http.Request) {
	dir := r.PostFormValue("dir")
	if !contains(s.opts.Engine.PrivateConfigCandidates(), dir) {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", "That is not a private config directory this server found.")
		return
	}
	if err := s.saveGlobalKey("private_config_dir", dir); err != nil {
		s.renderAudit(w, r, http.StatusUnprocessableEntity, auditData{SaveErr: err.Error()})
		return
	}
	http.Redirect(w, r, "/manifest-pack/audit?enabled=1", http.StatusSeeOther)
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
	// A set the pack does not have is refused here, with the near names,
	// rather than run and recorded as an unreadable directory.
	if req.Set != "" {
		if sets, err := s.opts.Engine.AuditSets(); err == nil && !contains(sets, req.Set) {
			s.renderAudit(w, r, http.StatusUnprocessableEntity, auditData{Set: req.Set, Pkg: req.Package,
				Unknown: &app.UnknownSetError{Set: req.Set, Pack: app.ManifestPack, Sets: sets, Suggestions: app.SuggestSets(req.Set, sets)}})
			return
		}
	}
	label := "every set"
	if req.Set != "" {
		label = "set " + req.Set
		if req.Package != "" {
			label += " against " + req.Package
		}
	}
	// The audit reads the manifest pack's tree; holding its lock keeps a sync
	// from rewriting that tree mid-survey.
	j, err := s.jobs.Start(jobs.Spec{Kind: jobs.KindAudit, Packs: []string{app.ManifestPack}, DryRun: true, Label: label},
		func(ctx context.Context, j *jobs.Job) (jobs.Status, error) {
			done := 0
			results, err := s.opts.Engine.AuditManifestPack(ctx, req, AuditHooks{
				Logger: slog.New(jobs.Handler(j, slog.LevelInfo)),
				Starting: func(n int, root string) {
					j.Logf("info", "auditing %d set(s) from %s", n, root)
				},
				SetDone: func(res manifestpack.Resolution) {
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
			sum := manifestpack.Summary(results)
			j.Logf("info", "%d exact, %d strong, %d upstream-identified, %d partial, %d unresolved",
				sum[manifestpack.ConfidenceExact], sum[manifestpack.ConfidenceStrong], sum[manifestpack.ConfidencePackage],
				sum[manifestpack.ConfidencePartial], sum[manifestpack.ConfidenceNone])
			// A survey, not a gate: unresolved sets are work for a person, not a
			// failed audit -- the same reason `manifest-pack audit` exits 0.
			// A set it could not read at all is a failure: nothing was surveyed.
			if n := manifestpack.CountErrors(results); n > 0 {
				return jobs.StatusFailed, fmt.Errorf("%d of %d sets could not be read", n, len(results))
			}
			return jobs.StatusSucceeded, nil
		})
	s.started(w, r, j, err)
}

// handleAuditManifest downloads an audit's resolutions as a set manifest,
// the input a per-set refresh needs, like `manifest-pack audit --json`.
func (s *Server) handleAuditManifest(w http.ResponseWriter, r *http.Request) {
	_, events, _, err := s.lookupJob(r.PathValue("id"))
	results := auditResults(events)
	if err != nil || results == nil {
		s.renderError(w, r, http.StatusNotFound, "No audit results", "That run is not a finished audit.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+app.ManifestPack+`.sets.json"`)
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(manifestpack.ManifestFromResolutions("ichava/"+app.ManifestPack, results))
}
