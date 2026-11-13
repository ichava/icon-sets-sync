package web

import (
	"net/http"

	"github.com/ichava/icon-sets-sync/src/internal/core/packcheck"
)

type validateData struct {
	Ran, Parse       bool
	Reports          []packcheck.Report
	Errors, Warnings int
	Err              string
}

// handleValidate checks the pack checkouts' structure on request. It only
// reads, so a read-only server keeps it. Parsing every SVG is a choice,
// because the largest packs hold over 100,000.
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d := validateData{Ran: q.Get("run") == "1", Parse: q.Get("parse") == "1"}
	if d.Ran {
		reports, err := s.opts.Engine.ValidatePacks(d.Parse)
		if err != nil {
			d.Err = err.Error()
		}
		d.Reports = reports
		for _, rep := range reports {
			d.Errors += rep.Errors()
			d.Warnings += rep.Warnings()
		}
	}
	s.render(w, http.StatusOK, "validate", s.newView(r, "validate", "Validate packs", d))
}
