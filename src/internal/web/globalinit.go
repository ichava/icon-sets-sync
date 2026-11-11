package web

import (
	"bytes"
	"net/http"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/app"
)

// GlobalInitializer is an Engine that keeps the global config initialised:
// generated from what the machine has until a person edits it, and
// regenerated on request.
type GlobalInitializer interface {
	// GlobalInitInfo says how the global config stands, when it was last
	// generated, and what a generation would find now.
	GlobalInitInfo() GlobalInitInfo
	// InitGlobal writes the generated config unless the file is a person's
	// (force writes it anyway, keeping the old copy in the history).
	InitGlobal(force bool) (GlobalInitResult, error)
}

// GlobalInitInfo is the editor's banner.
type GlobalInitInfo struct {
	State     app.GlobalState
	Generated time.Time
	Notes     []string
}

// GlobalInitResult is what an initialisation did.
type GlobalInitResult struct {
	Path    string
	State   app.GlobalState // before
	Changed bool
	Notes   []string
}

func (e *AppEngine) generateGlobal() app.GlobalInit {
	if e.InitGlobalFrom != nil {
		return e.InitGlobalFrom()
	}
	return app.InitialGlobal(e.Workspace)
}

func (e *AppEngine) initRecord() string {
	if e.InitRecord != "" {
		return e.InitRecord
	}
	return app.InitRecordPath(e.History.Dir)
}

// GlobalInitInfo implements GlobalInitializer.
func (e *AppEngine) GlobalInitInfo() GlobalInitInfo {
	doc, err := e.ReadConfig(GlobalName)
	rec := app.ReadInitRecord(e.initRecord())
	info := GlobalInitInfo{Notes: e.generateGlobal().Notes, Generated: rec.Time}
	if err != nil {
		info.State = app.GlobalMissing
		return info
	}
	info.State = app.ClassifyGlobal(doc.Data, doc.Exists, rec)
	return info
}

// InitGlobal implements GlobalInitializer. It writes through WriteConfig, so
// the file is validated, the previous copy is kept in the history and the
// running settings reload.
func (e *AppEngine) InitGlobal(force bool) (GlobalInitResult, error) {
	doc, err := e.ReadConfig(GlobalName)
	if err != nil {
		return GlobalInitResult{}, err
	}
	gen := e.generateGlobal()
	res := GlobalInitResult{Path: doc.Path, Notes: gen.Notes,
		State: app.ClassifyGlobal(doc.Data, doc.Exists, app.ReadInitRecord(e.initRecord()))}
	if res.State == app.GlobalEdited && !force {
		return res, nil
	}
	if !doc.Exists || !bytes.Equal(doc.Data, gen.Data) {
		if err := e.WriteConfig(GlobalName, gen.Data, doc.Rev); err != nil {
			return res, err
		}
		res.Changed = true
	}
	return res, app.WriteInitRecord(e.initRecord(), gen.Data)
}

// handleGlobalReinit regenerates the global config from this machine,
// whatever it holds now; the previous copy goes to the history.
func (s *Server) handleGlobalReinit(w http.ResponseWriter, r *http.Request) {
	gi, ok := s.opts.Engine.(GlobalInitializer)
	if !ok {
		s.renderError(w, r, http.StatusNotFound, "Not available", "This server cannot initialise the global config.")
		return
	}
	res, err := gi.InitGlobal(true)
	if err != nil {
		s.renderError(w, r, http.StatusUnprocessableEntity, "Could not reinitialise the global config", err.Error())
		return
	}
	q := "reinit=same"
	if res.Changed {
		q = "reinit=1"
	}
	http.Redirect(w, r, "/config/"+GlobalName+"?"+q, http.StatusSeeOther)
}
