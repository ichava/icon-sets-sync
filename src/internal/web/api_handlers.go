package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/apitoken"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/events"
	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

// openAPIDoc is the API's OpenAPI 3.1 description. It is documentation, not
// the source of the routes: apiRoutes is, and a test holds the two together.
//
//go:embed openapi.json
var openAPIDoc []byte

func (s *Server) apiNoRoute(w http.ResponseWriter, r *http.Request) {
	if s.opts.ReadOnly && r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, r, http.StatusMethodNotAllowed, "read-only", "Read-only server",
			"This server was started with --read-only, so it cannot run a sync or change a file.")
		return
	}
	writeProblem(w, r, http.StatusNotFound, "no-route", "Not found", "There is no API endpoint at this address and method; see /api/v1/openapi.json.")
}

func (s *Server) apiIndex(w http.ResponseWriter, r *http.Request) {
	tok := tokenFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"name": "icon-sets-sync", "version": s.opts.Version, "api": "v1", "read_only": s.opts.ReadOnly,
		"surfaces": map[string]any{
			"api": true, "events_stream": s.opts.API.EventsStream, "webhooks": s.opts.API.Webhooks != nil, "tls": s.opts.TLS,
		},
		"token":       map[string]any{"name": tok.Name, "scopes": tok.Scopes},
		"event_types": events.Types(),
		"openapi":     "/api/v1/openapi.json",
	})
}

func (s *Server) apiOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(openAPIDoc)
}

func (s *Server) apiSchema(w http.ResponseWriter, r *http.Request) {
	data, ok := events.Schema(r.PathValue("type"))
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "no-such-event-type", "No such event type",
			"Known types: "+strings.Join(events.Types(), ", "))
		return
	}
	w.Header().Set("Content-Type", "application/schema+json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// apiPage reads ?page= and ?per_page= (default page_size, at most 500).
func (s *Server) apiPage(r *http.Request) (page, per int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	per, _ = strconv.Atoi(r.URL.Query().Get("per_page"))
	if page < 1 {
		page = 1
	}
	if per < 1 {
		per = s.pageSize()
	}
	if per > 500 {
		per = 500
	}
	return page, per
}

func pageOf[T any](items []T, page, per int) []T {
	start := (page - 1) * per
	if start >= len(items) {
		return []T{}
	}
	end := min(start+per, len(items))
	return items[start:end]
}

// apiPackJSON is one pack as the API shows it.
type apiPackJSON struct {
	Slug     string            `json:"slug"`
	Pack     string            `json:"pack"`
	Recipe   string            `json:"recipe"`
	Current  string            `json:"current_version"`
	Upstream *apiUpstreamJSON  `json:"upstream,omitempty"`
	Stats    *PackStats        `json:"stats,omitempty"`
	Running  string            `json:"running_job,omitempty"`
	Notes    map[string]string `json:"notes,omitempty"`
}

type apiUpstreamJSON struct {
	Status  string `json:"status"`
	Latest  string `json:"latest,omitempty"`
	Release string `json:"release_url,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

func (s *Server) packJSON(p *config.PackConfig, snap statusSnapshot, i int) apiPackJSON {
	out := apiPackJSON{Slug: p.Name, Pack: p.Pack, Recipe: RecipeName(p), Current: config.ResolvedCurrentVersion(p), Running: s.jobs.Running(p.Name)}
	if snap.Packs != nil && i >= 0 && i < len(snap.Results) {
		res := snap.Results[i]
		out.Upstream = &apiUpstreamJSON{Status: string(res.Status()), Latest: res.Latest, Release: res.ReleaseURL, Reason: res.Reason}
	}
	if snap.Packs != nil && i >= 0 && i < len(snap.Stats) {
		st := snap.Stats[i]
		out.Stats = &st
	}
	return out
}

func (s *Server) apiPacks(w http.ResponseWriter, r *http.Request) {
	snap := s.status.snapshot()
	packs := snap.Packs
	if packs == nil {
		var err error
		if packs, err = s.opts.Engine.Packs(); err != nil {
			writeProblem(w, r, http.StatusInternalServerError, "packs-unavailable", "Packs unavailable", err.Error())
			return
		}
	}
	items := make([]apiPackJSON, len(packs))
	for i, p := range packs {
		items[i] = s.packJSON(p, snap, i)
	}
	page, per := s.apiPage(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"items": pageOf(items, page, per), "page": page, "per_page": per, "total": len(items),
		"checked_at": s.checkStatus().CheckedAt,
	})
}

func (s *Server) apiPack(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !validSlug(slug) {
		writeProblem(w, r, http.StatusNotFound, "no-such-pack", "No such pack", "Pack names are lowercase slugs.")
		return
	}
	p, err := s.opts.Engine.Pack(slug)
	if err != nil {
		writeProblem(w, r, http.StatusNotFound, "no-such-pack", "No such pack", err.Error())
		return
	}
	snap := s.status.snapshot()
	i := -1
	for j, q := range snap.Packs {
		if q.Name == slug {
			i = j
		}
	}
	out := s.packJSON(p, snap, i)
	out.Notes = s.opts.Engine.PackNotes(p)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) apiCheck(w http.ResponseWriter, _ *http.Request) {
	started := s.refresh()
	writeJSON(w, http.StatusAccepted, map[string]any{"started": started, "status": s.checkStatus()})
}

// apiSyncRequest is the body of the sync endpoints. dry_run is a pointer so
// that leaving it out means true: a real run is always asked for by name.
type apiSyncRequest struct {
	DryRun      *bool    `json:"dry_run"`
	Force       bool     `json:"force"`
	Concurrency int      `json:"concurrency"`
	Packs       []string `json:"packs"`
}

func (s *Server) apiSyncPack(w http.ResponseWriter, r *http.Request) {
	var req apiSyncRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if len(req.Packs) > 0 {
		writeProblem(w, r, http.StatusBadRequest, "bad-body", "Bad request body", "packs belongs to POST /api/v1/sync; this endpoint syncs the pack in its path.")
		return
	}
	slug := r.PathValue("slug")
	p, err := s.opts.Engine.Pack(slug)
	if !validSlug(slug) || err != nil {
		writeProblem(w, r, http.StatusNotFound, "no-such-pack", "No such pack", "There is no pack named "+strconv.Quote(slug)+".")
		return
	}
	s.apiStartSync(w, r, jobs.KindSync, []*config.PackConfig{p}, req)
}

func (s *Server) apiSyncAll(w http.ResponseWriter, r *http.Request) {
	var req apiSyncRequest
	if !decodeBody(w, r, &req) {
		return
	}
	all, err := s.opts.Engine.Packs()
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "packs-unavailable", "Packs unavailable", err.Error())
		return
	}
	packs := all
	if len(req.Packs) > 0 {
		packs = nil
		for _, name := range req.Packs {
			found := false
			for _, p := range all {
				if p.Name == name {
					packs, found = append(packs, p), true
				}
			}
			if !found {
				writeProblem(w, r, http.StatusNotFound, "no-such-pack", "No such pack", "There is no pack named "+strconv.Quote(name)+".")
				return
			}
		}
	}
	s.apiStartSync(w, r, jobs.KindSyncAll, packs, req)
}

func (s *Server) apiStartSync(w http.ResponseWriter, r *http.Request, kind jobs.Kind, packs []*config.PackConfig, req apiSyncRequest) {
	if req.Concurrency < 0 || req.Concurrency > app.MaxConcurrency {
		writeProblem(w, r, http.StatusBadRequest, "bad-body", "Bad request body",
			fmt.Sprintf("concurrency must be 0 (the setting) to %d.", app.MaxConcurrency))
		return
	}
	run := RunRequest{DryRun: true, Force: req.Force, Concurrency: req.Concurrency}
	if req.DryRun != nil {
		run.DryRun = *req.DryRun
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) > 255 {
		writeProblem(w, r, http.StatusBadRequest, "bad-idempotency-key", "Bad Idempotency-Key", "Keep it to 255 characters.")
		return
	}
	idem := ""
	if key != "" {
		idem = tokenFrom(r.Context()).ID + "\x00" + key
		if id, ok := s.api.idemGet(idem, s.now()); ok {
			if info, _, _, err := s.lookupJob(id); err == nil {
				w.Header().Set("Location", "/api/v1/jobs/"+id)
				writeJSON(w, http.StatusOK, map[string]any{"job": info, "replayed": true})
				return
			}
		}
	}
	j, err := s.launchSync(kind, packs, run)
	var busy *jobs.ErrBusy
	switch {
	case errors.As(err, &busy):
		writeProblemDoc(w, Problem{Type: problemType("busy"), Title: "Already running", Status: http.StatusConflict,
			Detail: busy.Error() + "; one run per pack at a time", Instance: r.URL.Path, JobID: busy.JobID})
		return
	case err != nil:
		writeProblem(w, r, http.StatusInternalServerError, "not-started", "Could not start the run", err.Error())
		return
	}
	info := j.Info()
	if idem != "" {
		s.api.idemPut(idem, info.ID, s.now())
	}
	w.Header().Set("Location", "/api/v1/jobs/"+info.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"job": info})
}

func (s *Server) apiJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var items []jobs.Info
	for _, info := range s.jobs.List() {
		if p := q.Get("pack"); p != "" && !contains(info.Packs, p) {
			continue
		}
		if st := q.Get("status"); st != "" && string(info.Status) != st {
			continue
		}
		items = append(items, info)
	}
	if items == nil {
		items = []jobs.Info{}
	}
	page, per := s.apiPage(r)
	writeJSON(w, http.StatusOK, map[string]any{"items": pageOf(items, page, per), "page": page, "per_page": per, "total": len(items)})
}

// apiPackOutcome is one pack's line of a run.
type apiPackOutcome struct {
	Pack   string `json:"pack"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (s *Server) apiJob(w http.ResponseWriter, r *http.Request) {
	info, evs, _, err := s.lookupJob(r.PathValue("id"))
	if err != nil {
		writeProblem(w, r, http.StatusNotFound, "no-such-job", "No such run", "That run is not in this server's memory or its history.")
		return
	}
	var packs []apiPackOutcome
	for _, e := range evs {
		if e.Type == jobs.EventPackDone {
			packs = append(packs, apiPackOutcome{Pack: e.Pack, Status: e.Status, Detail: e.Msg, Error: e.Err})
		}
	}
	if packs == nil {
		packs = []apiPackOutcome{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": info, "packs": packs, "events": len(evs),
		"log": "/api/v1/jobs/" + info.ID + "/log"})
}

func (s *Server) apiCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.jobs.Cancel(id) {
		writeProblem(w, r, http.StatusNotFound, "no-such-job", "No such run", "Only a run started by this server can be cancelled.")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job": s.jobs.Get(id).Info(), "cancelling": true})
}

// apiSettingJSON is one setting as the API shows it. No setting holds a
// secret (tokens and webhook secrets live in the state directory), so none
// is redacted; a key that ever does must be blanked here.
type apiSettingJSON struct {
	Name    string `json:"name"`
	Value   any    `json:"value"`
	Origin  string `json:"origin"`
	Default string `json:"default"`
	Env     string `json:"env"`
	Group   string `json:"group,omitempty"`
	Help    string `json:"help"`
}

func (s *Server) apiSettings(w http.ResponseWriter, _ *http.Request) {
	view := s.opts.Engine.Settings()
	out := make([]apiSettingJSON, 0, len(view.Rows))
	for _, row := range view.Rows {
		out = append(out, apiSettingJSON{Name: row.Name, Value: row.Typed, Origin: row.Origin, Default: row.Default, Env: row.Env, Group: row.Group, Help: row.Help})
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": out, "global_config": view.GlobalPath})
}

// settingRaw turns a JSON value into the strings a form would post, so the
// API checks a value with the Settings page's own parseSetting.
func settingRaw(k settings.Key, v any) ([]string, error) {
	switch k.Kind {
	case settings.Webhooks:
		data, err := json.Marshal(v)
		return []string{string(data)}, err
	case settings.List:
		switch x := v.(type) {
		case string:
			return []string{x}, nil
		case []any:
			out := make([]string, 0, len(x))
			for _, it := range x {
				s, ok := it.(string)
				if !ok {
					return nil, fmt.Errorf("want strings, got %T", it)
				}
				out = append(out, s)
			}
			return []string{strings.Join(out, "\n")}, nil
		}
		return nil, fmt.Errorf("want a string or an array of strings, got %T", v)
	case settings.Bool:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("want a boolean, got %T", v)
		}
		return []string{strconv.FormatBool(b)}, nil
	case settings.Int:
		n, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("want an integer, got %T", v)
		}
		return []string{strconv.FormatFloat(n, 'f', -1, 64)}, nil
	}
	str, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("want a string, got %T", v)
	}
	return []string{str}, nil
}

func (s *Server) apiSettingKey(w http.ResponseWriter, r *http.Request) (settings.Key, bool) {
	k, ok := settings.Lookup(r.PathValue("name"))
	if !ok || k.Name != r.PathValue("name") {
		writeProblem(w, r, http.StatusNotFound, "no-such-setting", "No such setting", "Known: "+strings.Join(settings.Names(), ", "))
		return settings.Key{}, false
	}
	return k, true
}

func (s *Server) apiSettingPut(w http.ResponseWriter, r *http.Request) {
	k, ok := s.apiSettingKey(w, r)
	if !ok {
		return
	}
	var body struct {
		Value any `json:"value"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	value, note, err := ParseSettingValue(k, body.Value)
	if err != nil {
		writeProblem(w, r, http.StatusUnprocessableEntity, "invalid-setting", "Not saved", k.Name+" "+err.Error())
		return
	}
	if err := s.saveGlobalKey(k.Name, value); err != nil {
		s.apiSaveError(w, r, err)
		return
	}
	out := map[string]any{"name": k.Name, "value": value, "saved": true}
	if note != "" {
		out["note"] = note
	}
	writeJSON(w, http.StatusOK, out)
}

// ParseSettingValue checks a JSON value for a setting exactly as the
// Settings page checks a typed one, and returns what the global config
// stores. note is a warning that does not refuse.
func ParseSettingValue(k settings.Key, v any) (value any, note string, err error) {
	raw, err := settingRaw(k, v)
	if err != nil {
		return nil, "", err
	}
	return parseSetting(k, raw)
}

func (s *Server) apiSettingDelete(w http.ResponseWriter, r *http.Request) {
	k, ok := s.apiSettingKey(w, r)
	if !ok {
		return
	}
	if err := s.saveGlobalKey(k.Name, nil); err != nil {
		s.apiSaveError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": k.Name, "reset": true})
}

func (s *Server) apiSaveError(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *ValidationError
	switch {
	case errors.As(err, &invalid):
		writeProblem(w, r, http.StatusUnprocessableEntity, "invalid-config", "Not saved", err.Error())
	case errors.Is(err, ErrConflict):
		writeProblem(w, r, http.StatusConflict, "conflict", "Not saved", "The global config changed while saving; try again.")
	default:
		writeProblem(w, r, http.StatusInternalServerError, "not-saved", "Not saved", err.Error())
	}
}

func (s *Server) apiValidate(w http.ResponseWriter, r *http.Request) {
	reports, err := s.opts.Engine.ValidatePacks(r.URL.Query().Get("parse") == "1")
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "validate-failed", "Validation failed", err.Error())
		return
	}
	errs, warns := 0, 0
	for _, rep := range reports {
		errs += rep.Errors()
		warns += rep.Warnings()
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports, "errors": errs, "warnings": warns, "ok": errs == 0})
}

func (s *Server) apiAudit(w http.ResponseWriter, r *http.Request) {
	_, evs, _, err := s.lookupJob(r.PathValue("id"))
	results := auditResults(evs)
	if err != nil || results == nil {
		writeProblem(w, r, http.StatusNotFound, "no-audit", "No audit results", "That run is not a finished audit.")
		return
	}
	writeJSON(w, http.StatusOK, manifestpack.ManifestFromResolutions("ichava/"+app.ManifestPack, results))
}

// apiEvents streams the bus as Server-Sent Events: each event's id is its
// CloudEvents id and its data the whole envelope. ?type= (repeatable) keeps
// only those types. There is no replay: an event happens once.
func (s *Server) apiEvents(w http.ResponseWriter, r *http.Request) {
	types := r.URL.Query()["type"]
	for _, t := range types {
		if !events.Known(t) {
			writeProblem(w, r, http.StatusBadRequest, "no-such-event-type", "No such event type", "Known types: "+strings.Join(events.Types(), ", "))
			return
		}
	}
	sub := s.bus.Subscribe(events.DefaultBuffer, types...)
	defer sub.Close()
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": icon-sets-sync events\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	tick := time.NewTicker(sseHeartbeat)
	defer tick.Stop()
	for {
		select {
		case e, ok := <-sub.C():
			if !ok {
				return
			}
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", e.ID, e.Type, data)
		case <-tick.C:
			_, _ = io.WriteString(w, ": ping\n\n")
		case <-r.Context().Done():
			return
		case <-s.base.Done():
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func (s *Server) apiWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	d := s.opts.API.Webhooks
	if d == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "items": []any{}})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	recs, err := d.Log.Tail(limit)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "log-unreadable", "Delivery log unreadable", err.Error())
		return
	}
	if recs == nil {
		recs = []json.RawMessage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "items": recs, "states": d.States()})
}

func (s *Server) apiWebhookTest(w http.ResponseWriter, r *http.Request) {
	d := s.opts.API.Webhooks
	if d == nil {
		writeProblem(w, r, http.StatusConflict, "webhooks-off", "Webhooks are off", "Set webhooks.enabled and restart the server.")
		return
	}
	ep, err := settings.FindWebhook(s.opts.API.WebhookEndpoints, r.PathValue("name"))
	if err != nil {
		writeProblem(w, r, http.StatusNotFound, "no-such-webhook", "No such webhook", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	e, res := d.Test(ctx, ep)
	out := map[string]any{"event_id": e.ID, "status": res.Status, "delivered": res.Err == nil, "duration_ms": res.Duration.Milliseconds()}
	if res.Err != nil {
		out["error"] = res.Err.Error()
		writeJSON(w, http.StatusBadGateway, out)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// apiTokenJSON is a token without its hash.
type apiTokenJSON struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Scopes   []apitoken.Scope `json:"scopes"`
	State    string           `json:"state"`
	Created  time.Time        `json:"created"`
	Expires  time.Time        `json:"expires,omitzero"`
	LastUsed time.Time        `json:"last_used,omitzero"`
	Revoked  time.Time        `json:"revoked,omitzero"`
}

func (s *Server) apiTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.opts.API.Tokens.List()
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "tokens-unreadable", "Tokens unreadable", err.Error())
		return
	}
	out := make([]apiTokenJSON, 0, len(list))
	for _, t := range list {
		out = append(out, apiTokenJSON{ID: t.ID, Name: t.Name, Scopes: t.Scopes, State: t.State(s.now()),
			Created: t.Created, Expires: t.Expires, LastUsed: t.LastUsed, Revoked: t.Revoked})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) apiStop(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]any{"stopping": true})
	s.Stop()
}
