package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

type stageItem struct{ Role, Type, Detail string }

type packData struct {
	Pack     *config.PackConfig
	Recipe   string
	Current  string
	Notes    []note
	Stages   []stageItem
	Settings SettingsView
	Result   checker.Result
	Status   checker.Status
	Checked  bool
	Stats    PackStats
	Runs     []jobs.Info
	Running  string
}

type note struct{ Key, Text string }

func (s *Server) handlePack(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPack(w, r)
	if !ok {
		return
	}
	d := packData{Pack: p, Recipe: RecipeName(p), Current: config.ResolvedCurrentVersion(p), Running: s.jobs.Running(p.Name)}
	for k, v := range s.opts.Engine.PackNotes(p) {
		d.Notes = append(d.Notes, note{k, v})
	}
	sort.Slice(d.Notes, func(a, b int) bool { return d.Notes[a].Key < d.Notes[b].Key })

	d.Stages = append(d.Stages, stageItem{"source", p.Source.Type, firstNonEmpty(p.Source.Package, p.Source.Repo)})
	for _, t := range p.Transforms {
		typ, _ := t["type"].(string)
		d.Stages = append(d.Stages, stageItem{"transform", typ, ""})
	}
	for _, sk := range p.Sinks {
		d.Stages = append(d.Stages, stageItem{"sink", sk.Type, firstNonEmpty(sk.Root, sk.Branch)})
	}

	var err error
	if d.Settings, err = s.opts.Engine.PackSettings(p); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Pack settings do not resolve", err.Error())
		return
	}
	snap := s.status.snapshot()
	for i, sp := range snap.Packs {
		if sp.Name == p.Name && i < len(snap.Results) {
			d.Result, d.Checked, d.Status = snap.Results[i], true, snap.Results[i].Status()
			if i < len(snap.Stats) {
				d.Stats = snap.Stats[i]
			}
		}
	}
	for _, info := range s.jobs.List() {
		if len(d.Runs) == 5 {
			break
		}
		if contains(info.Packs, p.Name) {
			d.Runs = append(d.Runs, info)
		}
	}
	s.render(w, http.StatusOK, "pack", s.newView(r, "dashboard", p.Name, d))
}

func (s *Server) loadPack(w http.ResponseWriter, r *http.Request) (*config.PackConfig, bool) {
	slug := r.PathValue("slug")
	if !validSlug(slug) {
		s.renderError(w, r, http.StatusNotFound, "No such pack", "There is no registered pack by that name.")
		return nil, false
	}
	p, err := s.opts.Engine.Pack(slug)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such pack", err.Error())
		return nil, false
	}
	return p, true
}

// validSlug keeps a path value to the shape pack slugs have, so it can never
// name a file outside the config directory: lowercase letters, digits and
// hyphens, in segments joined by single dots ("icon-sets-extras.game-icons",
// a set config of a shared pack). No segment is empty, so ".", ".." and a
// leading or trailing dot are refused. Until R4 the dot was refused outright,
// and every set config's page and actions answered 404.
func validSlug(s string) bool {
	if s == "" || len(s) > 100 {
		return false
	}
	for _, seg := range strings.Split(s, ".") {
		if seg == "" {
			return false
		}
		for _, r := range seg {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// runForm is a run's choices as submitted.
type runForm struct {
	Real        bool // anything but mode=real is a dry run
	Force       bool
	Concurrency int
	Version     string
	Confirm     string
}

func parseRunForm(r *http.Request) (runForm, error) {
	f := runForm{
		Real:    r.PostFormValue("mode") == "real",
		Force:   r.PostFormValue("force") == "1",
		Version: strings.TrimSpace(r.PostFormValue("version")),
		Confirm: strings.TrimSpace(r.PostFormValue("confirm")),
	}
	if c := r.PostFormValue("concurrency"); c != "" {
		n, err := strconv.Atoi(c)
		if err != nil || n < 1 || n > 16 {
			return f, fmt.Errorf("concurrency %q: want 1 to 16", c)
		}
		f.Concurrency = n
	}
	if len(f.Version) > 64 {
		return f, errors.New("version is too long")
	}
	return f, nil
}

// confirmData is the second confirmation a real run needs.
type confirmData struct {
	Action, Target, Phrase string
	Form                   runForm
	Packs                  []string
}

// needsConfirmation renders the confirmation page and reports true when a
// real run has not yet been confirmed by typing its phrase. A dry run never
// does: it changes nothing a person cannot discard.
func (s *Server) needsConfirmation(w http.ResponseWriter, r *http.Request, f runForm, action, phrase string, packs []string) bool {
	if !f.Real || f.Confirm == phrase {
		return false
	}
	s.render(w, http.StatusOK, "confirm", s.newView(r, "dashboard", "Confirm a real sync",
		confirmData{Action: action, Target: phrase, Phrase: phrase, Form: f, Packs: packs}))
	return true
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPack(w, r)
	if !ok {
		return
	}
	f, err := parseRunForm(r)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	if s.needsConfirmation(w, r, f, "/packs/"+p.Name+"/sync", p.Name, []string{p.Name}) {
		return
	}
	s.startSync(w, r, jobs.KindSync, []*config.PackConfig{p}, f)
}

func (s *Server) handleSyncAll(w http.ResponseWriter, r *http.Request) {
	f, err := parseRunForm(r)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	packs, err := s.opts.Engine.Packs()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Packs unavailable", err.Error())
		return
	}
	names := make([]string, len(packs))
	for i, p := range packs {
		names[i] = p.Name
	}
	if s.needsConfirmation(w, r, f, "/sync-all", "all packs", names) {
		return
	}
	s.startSync(w, r, jobs.KindSyncAll, packs, f)
}

func (s *Server) startSync(w http.ResponseWriter, r *http.Request, kind jobs.Kind, packs []*config.PackConfig, f runForm) {
	names := make([]string, len(packs))
	for i, p := range packs {
		names[i] = p.Name
	}
	req := RunRequest{DryRun: !f.Real, Force: f.Force, Concurrency: f.Concurrency}
	label := "dry run"
	if f.Real {
		label = "real run"
	}
	if f.Force {
		label += ", forced"
	}
	j, err := s.jobs.Start(jobs.Spec{Kind: kind, Packs: names, DryRun: req.DryRun, Label: label},
		func(ctx context.Context, j *jobs.Job) (jobs.Status, error) {
			outcomes := s.opts.Engine.Sync(ctx, packs, req, s.hooks(j))
			failed, review := 0, 0
			for _, o := range outcomes {
				switch {
				case o.Failed():
					failed++
				case o.NeedsReview():
					review++
				}
			}
			if failed > 0 {
				return jobs.StatusFailed, fmt.Errorf("%d of %d packs failed", failed, len(outcomes))
			}
			if review > 0 {
				return jobs.StatusNeedsReview, fmt.Errorf("%d of %d packs need review", review, len(outcomes))
			}
			return jobs.StatusSucceeded, nil
		})
	s.started(w, r, j, err)
}

func (s *Server) handleRecipe(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPack(w, r)
	if !ok {
		return
	}
	f, err := parseRunForm(r)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	if s.needsConfirmation(w, r, f, "/packs/"+p.Name+"/recipe", p.Name, []string{p.Name}) {
		return
	}
	req := RunRequest{DryRun: !f.Real}
	version := f.Version
	label := "recipe " + firstNonEmpty(version, p.CurrentVersion)
	if req.DryRun {
		label += ", dry run"
	}
	j, err := s.jobs.Start(jobs.Spec{Kind: jobs.KindRecipe, Packs: []string{p.Name}, DryRun: req.DryRun, Label: label},
		func(ctx context.Context, j *jobs.Job) (jobs.Status, error) {
			h := s.hooks(j)
			j.PackStarted(p.Pack, label)
			res, err := s.opts.Engine.Recipe(ctx, p, version, req, h)
			switch {
			case err != nil:
				j.PackDone(p.Pack, packStatus(app.Outcome{Status: app.StatusFailed, Err: err}), err.Error(), "")
				return jobs.StatusFailed, err
			case !res.Success:
				j.PackDone(p.Pack, packStatus(app.Outcome{Status: app.StatusFailed, Result: res}), res.Error(), "")
				return jobs.StatusFailed, errors.New(res.Error())
			}
			j.PackDone(p.Pack, string(app.StatusSynced), "", summary(res.Metrics))
			return jobs.StatusSucceeded, nil
		})
	s.started(w, r, j, err)
}

// hooks routes a run's logger, stage events and pack boundaries into a job.
func (s *Server) hooks(j *jobs.Job) RunHooks {
	return RunHooks{
		Logger:   slog.New(jobs.Handler(j, slog.LevelInfo)),
		Observer: jobs.Observer(j),
		Starting: func(p *config.PackConfig, r checker.Result, dryRun bool) {
			mode := ""
			if dryRun {
				mode = " (dry run)"
			}
			j.PackStarted(p.Pack, fmt.Sprintf("refreshing from %s to %s%s", firstNonEmpty(r.Current, "-"), r.Latest, mode))
		},
		Done: func(o app.Outcome) {
			errText := ""
			if o.Err != nil {
				errText = o.Err.Error()
			}
			for _, rv := range o.Reviews() {
				if err := j.Record(reviewRecord{Kind: "review", Pack: o.Pack, Review: rv}); err != nil {
					j.Logf("error", "recording the review: %v", err)
				}
			}
			j.PackDone(o.Pack, packStatus(o), errText, packDetail(o))
		},
	}
}

func (s *Server) started(w http.ResponseWriter, r *http.Request, j *jobs.Job, err error) {
	var busy *jobs.ErrBusy
	if errors.As(err, &busy) {
		s.renderErrorLink(w, r, http.StatusConflict, "Already running",
			fmt.Sprintf("%s is being synced by another run. One run per pack at a time, because two would race on the same repository's branches.", busy.Pack),
			"/jobs/"+busy.JobID, "Watch that run")
		return
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not start the run", err.Error())
		return
	}
	http.Redirect(w, r, "/jobs/"+j.Info().ID, http.StatusSeeOther)
}

// packDetail is the line under a pack on the job page.
func packDetail(o app.Outcome) string {
	switch {
	case o.LicenceChanged():
		return "licence changed upstream: the sync is blocked until a person reads the new terms and updates the pack's declared licence"
	case o.Status == app.StatusUpToDate:
		return "up to date at " + o.Current
	case o.Status == app.StatusSynced:
		return summary(o.Result.Metrics)
	case o.NeedsReview():
		renamed, removed := 0, 0
		for _, rv := range o.Reviews() {
			for _, rm := range rv.Removals {
				if rm.RenamedTo != "" {
					renamed++
				} else {
					removed++
				}
			}
		}
		return fmt.Sprintf("a real run would stop here: %d renamed, %d removed -- see the list below", renamed, removed)
	}
	return ""
}

// packStatus is the status a run page shows for one pack: its outcome, or
// "cancelled" when Cancel stopped it. Until U10 a cancelled pack read
// "failed: <stage>: context canceled". Sync re-wraps the pipeline's error as
// text, so the pipeline result's own error is checked too.
func packStatus(o app.Outcome) string {
	if errors.Is(o.Err, context.Canceled) || errors.Is(o.Result.Err, context.Canceled) {
		return string(jobs.StatusCancelled)
	}
	return string(o.Status)
}

// summary renders a run's metrics as one short line.
func summary(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		if inner, ok := m[k].(map[string]any); ok {
			for _, field := range []string{"copied", "added", "removed", "changed", "status", "would_write"} {
				if v, ok := inner[field]; ok {
					parts = append(parts, fmt.Sprintf("%s %s %v", k, strings.ReplaceAll(field, "_", " "), v))
				}
			}
		}
	}
	return strings.Join(parts, ", ")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
