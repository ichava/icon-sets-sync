package web

import (
	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/events"
	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

// What the server publishes on its event bus. The per-job stream the run page
// reads is the job's own ring, kept for Last-Event-ID resumption and the
// history; these hooks feed the same events to the bus, so the API stream and
// the webhooks see what the run page sees.

// publishJobStarted is the registry's Started hook.
func (s *Server) publishJobStarted(info jobs.Info) {
	s.bus.Emit(events.JobStarted, map[string]any{
		"job_id": info.ID, "kind": string(info.Kind), "packs": nonNil(info.Packs),
		"dry_run": info.DryRun, "label": info.Label,
	})
}

// publishJobEvent is the registry's Event hook.
func (s *Server) publishJobEvent(info jobs.Info, e jobs.Event) {
	switch e.Type {
	case jobs.EventStage:
		data := map[string]any{"job_id": info.ID, "pipeline": e.Pipeline, "stage": e.Stage, "state": e.State}
		if e.Err != "" {
			data["error"] = e.Err
		}
		s.bus.Emit(events.JobProgress, data)
	case jobs.EventPackDone:
		if e.Status == string(app.StatusSynced) {
			s.bus.Emit(events.PackSynced, map[string]any{
				"job_id": info.ID, "pack": e.Pack, "dry_run": info.DryRun, "detail": e.Msg,
			})
		}
	case jobs.EventDone:
		data := map[string]any{
			"job_id": info.ID, "kind": string(info.Kind), "packs": nonNil(info.Packs), "dry_run": info.DryRun,
			"status": string(info.Status), "duration_ms": info.Ended.Sub(info.Started).Milliseconds(),
		}
		if info.Err != "" {
			data["error"] = info.Err
		}
		s.bus.Emit(events.JobFinished, data)
	}
}

// publishCheck reports each pack an upstream check found behind, then the
// finished check.
func (s *Server) publishCheck(packs []*config.PackConfig, results []checker.Result, err error) {
	for i, r := range results {
		if i >= len(packs) || r.Status() != checker.StatusUpdateAvailable {
			continue
		}
		s.bus.Emit(events.PackBehindUpstream, map[string]any{
			"pack": packs[i].Name, "current": r.Current, "latest": r.Latest,
		})
	}
	data := map[string]any{
		"packs": len(packs), "stale": checker.CountStale(results), "errored": checker.CountErrored(results),
	}
	if err != nil {
		data["error"] = err.Error()
	}
	s.bus.Emit(events.UpstreamChecked, data)
}

// publishConfigChanged reports a saved file or setting: its name and the
// key, never the value.
func (s *Server) publishConfigChanged(file, key, action string) {
	data := map[string]any{"file": file, "action": action}
	if key != "" {
		data["key"] = key
	}
	s.bus.Emit(events.ConfigChanged, data)
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
