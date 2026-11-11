package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/app"
)

// A start initialises a missing or $schema-only global config, refreshes one
// it generated, and leaves a person's edit alone.
func TestInitGlobalRespectsAnEdit(t *testing.T) {
	_, eng := newTestServer(t)
	e := eng.cfg
	gen := testGlobalInit().Data

	res, err := e.InitGlobal(false)
	if err != nil || !res.Changed || res.State != app.GlobalMissing {
		t.Fatalf("missing: %+v %v", res, err)
	}
	doc, _ := e.ReadConfig(GlobalName)
	if string(doc.Data) != string(gen) {
		t.Fatalf("wrote %s", doc.Data)
	}
	if got := e.GlobalInitInfo().State; got != app.GlobalGenerated {
		t.Errorf("after generating: %s", got)
	}
	// Generated, unchanged: nothing to write.
	if res, _ := e.InitGlobal(false); res.Changed || res.State != app.GlobalGenerated {
		t.Errorf("regenerating an unchanged file: %+v", res)
	}

	// A person's edit survives a start.
	edited := []byte(`{"concurrency": 3}` + "\n")
	if err := e.WriteConfig(GlobalName, edited, doc.Rev); err != nil {
		t.Fatal(err)
	}
	if res, _ := e.InitGlobal(false); res.Changed || res.State != app.GlobalEdited {
		t.Errorf("a start touched an edited file: %+v", res)
	}
	if doc, _ := e.ReadConfig(GlobalName); string(doc.Data) != string(edited) {
		t.Errorf("edited file now %s", doc.Data)
	}

	// Reinitialise replaces it, and the edit is in the history.
	if res, err := e.InitGlobal(true); err != nil || !res.Changed {
		t.Fatalf("force: %+v %v", res, err)
	}
	h, _ := e.ConfigHistory(GlobalName)
	found := false
	for _, entry := range h {
		if b, _ := e.ConfigHistoryRead(GlobalName, entry.ID); string(b) == string(edited) {
			found = true
		}
	}
	if !found {
		t.Error("the edited copy is not in the history")
	}
}

// A $schema-only file, like the one an earlier version left, is filled in.
func TestInitGlobalFillsASchemaOnlyFile(t *testing.T) {
	_, eng := newTestServer(t)
	e := eng.cfg
	doc, _ := e.ReadConfig(GlobalName)
	if err := e.WriteConfig(GlobalName, []byte(GlobalSeed()), doc.Rev); err != nil {
		t.Fatal(err)
	}
	res, err := e.InitGlobal(false)
	if err != nil || !res.Changed || res.State != app.GlobalEmpty {
		t.Fatalf("%+v %v", res, err)
	}
}

// The editor says how the file stands and offers Reinitialise; the action
// regenerates it and the page toasts the result.
func TestGlobalEditorReinitialises(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)
	doc, _ := eng.cfg.ReadConfig(GlobalName)
	if err := eng.cfg.WriteConfig(GlobalName, []byte(`{"concurrency": 3}`+"\n"), doc.Rev); err != nil {
		t.Fatal(err)
	}
	page := c.get("/config/global").Body.String()
	for _, want := range []string{"edited", "/config/global/reinit", "Reinitialise", "/work/packages holds 3"} {
		if !strings.Contains(page, want) {
			t.Errorf("editor lacks %q", want)
		}
	}
	rec := c.post("/config/global/reinit", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/config/global?reinit=1" {
		t.Fatalf("reinit: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	page = c.get("/config/global?reinit=1").Body.String()
	for _, want := range []string{"Global config reinitialised", "generated", "/work/packages"} {
		if !strings.Contains(page, want) {
			t.Errorf("after reinit the page lacks %q", want)
		}
	}
	if rec := c.post("/config/global/reinit", nil); rec.Header().Get("Location") != "/config/global?reinit=same" {
		t.Errorf("an unchanged reinit: %s", rec.Header().Get("Location"))
	}
	// Read-only servers do not offer it.
	ro, _ := newTestServer(t, func(o *Options) { o.ReadOnly = true })
	if strings.Contains(signIn(t, ro).get("/config/global").Body.String(), "/reinit") {
		t.Error("a read-only server offers Reinitialise")
	}
}

// Fix answers JSON for the page's script: the fixed text, what changed, the
// diff and the findings, so the editor updates in place.
func TestFixAnswersJSON(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	form := url.Values{"content": {`{"packages_dir": ["/a"]}`}}
	r := c.postJSON("/config/global/fix", form)
	if r.Code != http.StatusOK {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	var res struct {
		Text    string
		Changes []string
		Diff    string
		OK      bool
	}
	if err := json.Unmarshal(r.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "packages_dirs") || len(res.Changes) == 0 || res.Diff == "" || !res.OK {
		t.Fatalf("%+v", res)
	}
	// Nothing to fix: no changes and no diff, still JSON.
	form.Set("content", res.Text)
	r = c.postJSON("/config/global/fix", form)
	_ = json.Unmarshal(r.Body.Bytes(), &res)
	if len(res.Changes) != 0 || res.Diff != "" {
		t.Errorf("nothing to fix: %+v", res)
	}
}

// The editor checks the file as it opens, so its status is known at once.
func TestEditorValidatesOnOpen(t *testing.T) {
	s, eng := newTestServer(t)
	doc, _ := eng.cfg.ReadConfig(GlobalName)
	if err := os.MkdirAll(strings.TrimSuffix(doc.Path, "config.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc.Path, []byte(`{"concurrency": "many"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	page := signIn(t, s).get("/config/global").Body.String()
	if !strings.Contains(page, "problem(s)") || strings.Contains(page, "Not checked") {
		t.Error("the editor did not check the file on open")
	}
}

// The editor carries the highlighting layer and the document's language,
// which app.js reads for its highlighting and its in-browser syntax check.
func TestEditorCarriesTheHighlightLayerAndLanguage(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)
	page := c.get("/config/global").Body.String()
	for _, want := range []string{`id="editor-hl"`, `aria-hidden="true"`, `data-lang="json"`} {
		if !strings.Contains(page, want) {
			t.Errorf("editor lacks %s", want)
		}
	}
	// A YAML global config is highlighted as YAML.
	doc, _ := eng.cfg.ReadConfig(GlobalName)
	yml := strings.TrimSuffix(doc.Path, ".json") + ".yaml"
	if err := os.MkdirAll(strings.TrimSuffix(yml, "config.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yml, []byte("concurrency: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng.cfg.GlobalPath = yml
	if page := c.get("/config/global").Body.String(); !strings.Contains(page, `data-lang="yaml"`) {
		t.Error("a YAML config is not marked yaml")
	}
}
