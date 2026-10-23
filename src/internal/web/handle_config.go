package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/app"
)

// maxConfigBytes bounds an editor submission. The largest shipped document,
// the bundled set manifest, is well under this.
const maxConfigBytes = 1 << 20

type configListData struct {
	Files []ConfigFile
	Err   string
}

func (s *Server) handleConfigList(w http.ResponseWriter, r *http.Request) {
	files, err := s.opts.Engine.ConfigFiles()
	d := configListData{Files: files}
	if err != nil {
		d.Err = err.Error()
	}
	s.render(w, http.StatusOK, "config", s.newView(r, "config", "Config", d))
}

type editorData struct {
	Doc      ConfigDoc
	Text     string
	Base     string
	Lines    []struct{}
	Findings []app.Finding
	Problems int
	Checked  bool
	Saved    bool
	Conflict bool
	Editable bool
}

func (s *Server) editorView(r *http.Request, doc ConfigDoc, text, base string) editorData {
	n := strings.Count(text, "\n") + 1
	if strings.HasSuffix(text, "\n") {
		n--
	}
	if n < 1 {
		n = 1
	}
	return editorData{
		Doc: doc, Text: text, Base: base, Lines: make([]struct{}, n),
		Saved:    r.URL.Query().Get("saved") == "1",
		Editable: !s.opts.ReadOnly && !doc.ReadOnly,
	}
}

func (s *Server) readDoc(w http.ResponseWriter, r *http.Request) (ConfigDoc, bool) {
	doc, err := s.opts.Engine.ReadConfig(r.PathValue("name"))
	if errors.Is(err, os.ErrNotExist) {
		s.renderError(w, r, http.StatusNotFound, "No such config file", "The editor only opens the files listed on the Config page.")
		return doc, false
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Config unreadable", err.Error())
		return doc, false
	}
	return doc, true
}

func (s *Server) handleConfigEdit(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.readDoc(w, r)
	if !ok {
		return
	}
	text := string(doc.Data)
	if !doc.Exists && doc.Kind == KindGlobal {
		text = "{\n}\n"
	}
	s.render(w, http.StatusOK, "editor", s.newView(r, "config", doc.Name, s.editorView(r, doc, text, doc.Rev)))
}

// submitted reads the editor's text, normalising the CRLF a browser's
// textarea submits back to the LF the files use.
func submitted(w http.ResponseWriter, r *http.Request) ([]byte, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxConfigBytes+4096)
	if err := r.ParseForm(); err != nil {
		return nil, "", err
	}
	text := strings.ReplaceAll(r.PostFormValue("content"), "\r\n", "\n")
	if len(text) > maxConfigBytes {
		return nil, "", errors.New("the document is too large")
	}
	return []byte(text), r.PostFormValue("base"), nil
}

// handleConfigValidate checks the submitted text. A fetch from app.js asks
// for JSON; a plain form post gets the editor back with the findings.
func (s *Server) handleConfigValidate(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.readDoc(w, r)
	if !ok {
		return
	}
	data, base, err := submitted(w, r)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	findings := s.opts.Engine.ValidateConfig(doc.Name, data)
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": app.Problems(findings) == 0, "problems": app.Problems(findings), "findings": findings,
		})
		return
	}
	d := s.editorView(r, doc, string(data), base)
	d.Findings, d.Problems, d.Checked = findings, app.Problems(findings), true
	s.render(w, http.StatusOK, "editor", s.newView(r, "config", doc.Name, d))
}

func (s *Server) handleConfigSave(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.readDoc(w, r)
	if !ok {
		return
	}
	data, base, err := submitted(w, r)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	err = s.opts.Engine.WriteConfig(doc.Name, data, base)
	if err == nil {
		http.Redirect(w, r, "/config/"+doc.Name+"?saved=1", http.StatusSeeOther)
		return
	}

	// Refused: give the person their text back, never lose an edit.
	d := s.editorView(r, doc, string(data), base)
	status := http.StatusInternalServerError
	var invalid *ValidationError
	switch {
	case errors.As(err, &invalid):
		status = http.StatusUnprocessableEntity
		d.Findings, d.Problems, d.Checked = invalid.Findings, app.Problems(invalid.Findings), true
	case errors.Is(err, ErrConflict):
		status, d.Conflict = http.StatusConflict, true
	default:
		d.Findings = []app.Finding{{Level: app.LevelError, Text: err.Error()}}
		d.Problems, d.Checked = 1, true
	}
	s.render(w, status, "editor", s.newView(r, "config", doc.Name, d))
}
