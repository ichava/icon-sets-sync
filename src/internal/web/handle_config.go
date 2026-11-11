package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/confighistory"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// maxConfigBytes bounds an editor submission. The largest shipped document,
// a manifest-pack's set manifest, is well under this.
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
	// ErrorLines marks the 1-based lines a finding points at, for the gutter.
	ErrorLines map[int]bool
	// Fixed lists what Fix changed, when the text came from Fix; FixDiff is
	// the change as a unified diff.
	Fixed   []string
	FixDiff string
	// FixNone is set when Fix ran and found nothing to change.
	FixNone bool
	// Keys is the setting registry, for the global config's key picker.
	Keys     []keyHint
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
	// Init is the global config's generation state, for its banner and the
	// Reinitialise action; Reinit is "1" after one changed the file and
	// "same" after one found nothing to change.
	Init   *GlobalInitInfo
	Reinit string
}

func (s *Server) editorView(r *http.Request, doc ConfigDoc, text, base string) editorData {
	n := strings.Count(text, "\n") + 1
	if strings.HasSuffix(text, "\n") {
		n--
	}
	if n < 1 {
		n = 1
	}
	d := editorData{
		Doc: doc, Text: text, Base: base, Lines: make([]struct{}, n),
		Saved:    r.URL.Query().Get("saved") == "1",
		Editable: !s.opts.ReadOnly && !doc.ReadOnly,
	}
	if doc.Kind == KindGlobal {
		d.Keys = keyHints()
		if gi, ok := s.opts.Engine.(GlobalInitializer); ok {
			info := gi.GlobalInitInfo()
			d.Init = &info
			d.Reinit = r.URL.Query().Get("reinit")
		}
	}
	return d
}

// withFindings records validation findings and the lines they mark.
func (d *editorData) withFindings(fs []app.Finding) {
	d.Findings, d.Problems, d.Checked = fs, app.Problems(fs), true
	d.ErrorLines = map[int]bool{}
	for _, f := range fs {
		if f.Level == app.LevelError && f.Line > 0 {
			d.ErrorLines[f.Line] = true
		}
	}
}

// keyHint is one known setting, offered by the global config's key picker.
type keyHint struct {
	Name, Type, Help, Insert string
}

func keyHints() []keyHint {
	var out []keyHint
	for _, k := range settings.Keys {
		def, _ := json.Marshal(k.Default)
		out = append(out, keyHint{Name: k.Name, Type: k.Kind.String(), Help: k.Help,
			Insert: fmt.Sprintf("%q: %s", k.Name, def)})
	}
	return out
}

// GlobalSeed is what the editor opens on when no global config exists: the
// $schema line, so an editor that reads it completes every key.
func GlobalSeed() string {
	return "{\n  \"$schema\": \"" + settings.GlobalSchemaURL + "\"\n}\n"
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
		text = GlobalSeed()
	}
	d := s.editorView(r, doc, text, doc.Rev)
	// Checked on open, so the page says at once whether the file is valid.
	d.withFindings(s.opts.Engine.ValidateConfig(doc.Name, []byte(text)))
	s.render(w, http.StatusOK, "editor", s.newView(r, "config", doc.Name, d))
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
	d.withFindings(findings)
	s.render(w, http.StatusOK, "editor", s.newView(r, "config", doc.Name, d))
}

// handleConfigFix shows the editor with the safe repairs applied and the
// diff they make. Nothing is written: the person reviews, then saves.
func (s *Server) handleConfigFix(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.readDoc(w, r)
	if !ok {
		return
	}
	data, base, err := submitted(w, r)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	fixed, err := s.opts.Engine.FixConfig(doc.Name, data)
	if err != nil {
		d := s.editorView(r, doc, string(data), base)
		d.withFindings([]app.Finding{{Level: app.LevelError, Text: err.Error()}})
		s.render(w, http.StatusUnprocessableEntity, "editor", s.newView(r, "config", doc.Name, d))
		return
	}
	findings := s.opts.Engine.ValidateConfig(doc.Name, fixed.Data)
	diff := ""
	if len(fixed.Changes) > 0 {
		diff = confighistory.Unified("as you had it", "fixed", string(data), string(fixed.Data))
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		changes := fixed.Changes
		if changes == nil {
			changes = []string{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text": string(fixed.Data), "changes": changes, "diff": diff,
			"ok": app.Problems(findings) == 0, "problems": app.Problems(findings), "findings": findings,
		})
		return
	}
	d := s.editorView(r, doc, string(fixed.Data), base)
	d.Fixed = fixed.Changes
	d.FixNone = len(fixed.Changes) == 0
	d.FixDiff = diff
	d.withFindings(findings)
	s.render(w, http.StatusOK, "editor", s.newView(r, "config", doc.Name, d))
}

type historyData struct {
	Doc     ConfigDoc
	Entries []confighistory.Entry
	// One copy, when one is open: its text, its diff against the file now,
	// and the file's revision for a restore.
	Entry    *confighistory.Entry
	Diff     string
	Base     string
	Editable bool
}

// handleConfigHistory lists a document's saved copies, or with an id shows
// one copy's diff against the file as it is now, with a restore button.
func (s *Server) handleConfigHistory(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.readDoc(w, r)
	if !ok {
		return
	}
	entries, err := s.opts.Engine.ConfigHistory(doc.Name)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "History unreadable", err.Error())
		return
	}
	d := historyData{Doc: doc, Entries: entries, Base: doc.Rev, Editable: !s.opts.ReadOnly && !doc.ReadOnly}
	if id := r.PathValue("id"); id != "" {
		data, err := s.opts.Engine.ConfigHistoryRead(doc.Name, id)
		if err != nil {
			s.renderError(w, r, http.StatusNotFound, "No such copy", err.Error())
			return
		}
		for i := range entries {
			if entries[i].ID == id {
				d.Entry = &entries[i]
			}
		}
		d.Diff = confighistory.Unified("now", "copy of "+id, string(doc.Data), string(data))
	}
	s.render(w, http.StatusOK, "history", s.newView(r, "config", doc.Name+" history", d))
}

// handleConfigRestore writes a saved copy back, through WriteConfig: it is
// validated, refused on a concurrent change, and the current text is kept in
// the history first, so a restore can itself be undone.
func (s *Server) handleConfigRestore(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.readDoc(w, r)
	if !ok {
		return
	}
	id := r.PostFormValue("id")
	data, err := s.opts.Engine.ConfigHistoryRead(doc.Name, id)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such copy", err.Error())
		return
	}
	switch err := s.opts.Engine.WriteConfig(doc.Name, data, r.PostFormValue("base")); {
	case err == nil:
		http.Redirect(w, r, "/config/"+doc.Name+"?saved=1", http.StatusSeeOther)
	case errors.Is(err, ErrConflict):
		s.renderErrorLink(w, r, http.StatusConflict, "Not restored", "The file changed since the history page was opened.", "/config/"+doc.Name+"/history/"+id, "Look again")
	default:
		s.renderError(w, r, http.StatusUnprocessableEntity, "Not restored", err.Error())
	}
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
		d.withFindings(invalid.Findings)
	case errors.Is(err, ErrConflict):
		status, d.Conflict = http.StatusConflict, true
	default:
		d.Findings = []app.Finding{{Level: app.LevelError, Text: err.Error()}}
		d.Problems, d.Checked = 1, true
	}
	s.render(w, status, "editor", s.newView(r, "config", doc.Name, d))
}
