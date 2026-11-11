package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/app"
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
	// Check is the upstream check's state, for the alert bar.
	Check checkStatus
	// JustChecked is set on the reload after a check finished, for a toast.
	JustChecked bool
	// MissingTools are the programs a real run needs and cannot find: the
	// preflight a person sees before starting one.
	MissingTools []app.Tool
	Rows         []packRow
	CheckedAt    time.Time
	Running      bool
	Err          string
	Totals       struct{ Packs, OK, Stale, Failed, SVGs int }
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	s.refreshIfStale()
	snap := s.status.snapshot()

	d := dashboardData{CheckedAt: snap.At, Running: snap.Running, Err: snap.Err, MissingTools: s.missingTools(),
		Check: s.checkStatus(), JustChecked: r.URL.Query().Get("checked") == "1"}
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

func (s *Server) missingTools() []app.Tool {
	tools := s.opts.Tools
	if tools == nil {
		tools = app.Tools
	}
	return app.MissingTools(tools())
}

// checkStatus is the upstream check's state, for the dashboard's alert bar
// and progress, polled while a check runs.
type checkStatus struct {
	Running   bool   `json:"running"`
	Done      int    `json:"done"`
	Total     int    `json:"total"`
	CheckedAt string `json:"checked_at,omitempty"`
	// CheckedAgo is CheckedAt relative to now ("4 minutes ago").
	CheckedAgo string `json:"checked_ago,omitempty"`
	Stale      int    `json:"stale"`
	Errored    int    `json:"errored"`
	Err        string `json:"error,omitempty"`
}

func (s *Server) checkStatus() checkStatus {
	snap := s.status.snapshot()
	st := checkStatus{Running: snap.Running, Done: snap.Done, Total: snap.Total, Err: snap.Err,
		Stale: checker.CountStale(snap.Results), Errored: checker.CountErrored(snap.Results)}
	if !snap.At.IsZero() {
		st.CheckedAt = snap.At.UTC().Format(time.RFC3339)
		st.CheckedAgo = agoFor(s.now().Sub(snap.At))
	}
	return st
}

// agoFor is a duration as a person says it, against the server's clock.
func agoFor(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < 2*time.Minute:
		return "a minute ago"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 2*time.Hour:
		return "an hour ago"
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s.checkStatus())
}
