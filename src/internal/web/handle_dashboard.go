package web

import (
	"net/http"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
)

// packRow is one pack on the dashboard.
type packRow struct {
	Slug, Pack, Recipe string
	Current            string
	Result             checker.Result
	Status             checker.Status
	Checked            bool
	Stats              PackStats
}

type dashboardData struct {
	Rows      []packRow
	CheckedAt time.Time
	Running   bool
	Err       string
	Totals    struct{ Packs, OK, Stale, Failed, SVGs int }
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	s.refreshIfStale()
	snap := s.status.snapshot()

	d := dashboardData{CheckedAt: snap.At, Running: snap.Running, Err: snap.Err}
	packs := snap.Packs
	if packs == nil {
		// No check has finished yet: still list the packs, from disk.
		var err error
		if packs, err = s.opts.Engine.Packs(); err != nil {
			d.Err = err.Error()
		}
	}
	for i, p := range packs {
		row := packRow{Slug: p.Name, Pack: p.Pack, Recipe: RecipeName(p), Current: config.ResolvedCurrentVersion(p)}
		if i < len(snap.Results) && snap.Packs != nil {
			row.Result, row.Checked = snap.Results[i], true
			row.Status = row.Result.Status()
			switch row.Status {
			case checker.StatusOK:
				d.Totals.OK++
			case checker.StatusUpdateAvailable:
				d.Totals.Stale++
			default:
				d.Totals.Failed++
			}
		}
		if i < len(snap.Stats) && snap.Packs != nil {
			row.Stats = snap.Stats[i]
			d.Totals.SVGs += row.Stats.SVGs
		}
		d.Rows = append(d.Rows, row)
	}
	d.Totals.Packs = len(d.Rows)

	v := s.newView(r, "dashboard", "Packs", d)
	v.Reload = snap.Running
	s.render(w, http.StatusOK, "dashboard", v)
}

// handleRefresh starts a new upstream check and returns to the dashboard.
// It polls the registries but writes nothing, so it stays available on a
// read-only server.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	s.refresh()
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
