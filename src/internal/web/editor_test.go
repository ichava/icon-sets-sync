package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var baseField = regexp.MustCompile(`name="base" value="([^"]*)"`)

func editorBase(t *testing.T, c *client, name string) string {
	t.Helper()
	body := c.get("/config/" + name).Body.String()
	m := baseField.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no base revision on the %s editor", name)
	}
	return m[1]
}

func TestEditorShowsTheRawFileWithItsNotes(t *testing.T) {
	s, _ := newTestServer(t)
	body := signIn(t, s).get("/config/icon-sets-demo.json").Body.String()
	if !strings.Contains(body, "kept verbatim by the editor") {
		t.Fatal("the editor dropped the _note annotation")
	}
}

func TestSaveKeepsTheExactBytes(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)
	base := editorBase(t, c, "icon-sets-demo.json")
	edited := strings.Replace(demoPackJSON, `"1.0.0"`, `"1.1.0"`, 1)
	crlf := strings.ReplaceAll(edited, "\n", "\r\n") // what a browser's textarea submits
	rec := c.post("/config/icon-sets-demo.json", url.Values{"content": {crlf}, "base": {base}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/config/icon-sets-demo.json?saved=1" {
		t.Fatalf("save: %d %s\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	got, _ := os.ReadFile(filepath.Join(eng.cfg.ConfigDir, "icon-sets-demo.json"))
	if string(got) != edited {
		t.Fatalf("saved bytes differ from the submitted text:\n%s", got)
	}
	if !strings.Contains(c.get("/config/icon-sets-demo.json?saved=1").Body.String(), "Saved.") {
		t.Error("no saved confirmation")
	}
}

func TestAnInvalidSaveWritesNothingAndKeepsTheText(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)
	base := editorBase(t, c, "icon-sets-demo.json")
	bad := strings.Replace(demoPackJSON, `"source": {"type": "npm", "package": "demo"}`, `"source": {"type": "npm", "pakage": "demo"}`, 1)
	rec := c.post("/config/icon-sets-demo.json", url.Values{"content": {bad}, "base": {base}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("save of an invalid document: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "pakage") || !strings.Contains(rec.Body.String(), "/source") {
		t.Error("the editor lost the text or did not say where the problem is")
	}
	got, _ := os.ReadFile(filepath.Join(eng.cfg.ConfigDir, "icon-sets-demo.json"))
	if string(got) != demoPackJSON {
		t.Fatal("an invalid save changed the file")
	}
}

func TestAStaleSaveIsAConflict(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)
	base := editorBase(t, c, "icon-sets-demo.json")
	path := filepath.Join(eng.cfg.ConfigDir, "icon-sets-demo.json")
	theirs := strings.Replace(demoPackJSON, `"1.0.0"`, `"2.0.0"`, 1)
	if err := os.WriteFile(path, []byte(theirs), 0o644); err != nil {
		t.Fatal(err)
	}
	mine := strings.Replace(demoPackJSON, `"1.0.0"`, `"1.5.0"`, 1)
	rec := c.post("/config/icon-sets-demo.json", url.Values{"content": {mine}, "base": {base}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "1.5.0") {
		t.Fatalf("stale save: %d", rec.Code)
	}
	if got, _ := os.ReadFile(path); string(got) != theirs {
		t.Fatal("a stale save overwrote someone else's edit")
	}
}

func TestValidateAnswersFormsAndFetches(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	page := c.post("/config/icon-sets-demo.json/validate", url.Values{"content": {"{"}})
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "1 problem(s)") {
		t.Fatalf("form validate: %d", page.Code)
	}

	r := httptest.NewRequest(http.MethodPost, "http://"+testHost+"/config/icon-sets-demo.json/validate",
		strings.NewReader(url.Values{"content": {demoPackJSON}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Origin", "http://"+testHost)
	r.Header.Set(csrfHeader, c.csrf)
	r.AddCookie(c.cookie)
	rec := do(s, r)
	var res struct {
		OK       bool `json:"ok"`
		Problems int  `json:"problems"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || !res.OK || res.Problems != 0 {
		t.Fatalf("fetch validate: %d %s", rec.Code, rec.Body.String())
	}
}

func TestTheEditorOnlyOpensListedFiles(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	// ServeMux itself redirects a bare ".." away (307) before any handler.
	if rec := c.get("/config/.."); rec.Code == http.StatusOK {
		t.Error("GET /config/.. was served")
	}
	for _, name := range []string{"secret.json", "..%2Fconfig%2Fpacks.json", "icon-sets-demo"} {
		if rec := c.get("/config/" + name); rec.Code != http.StatusNotFound {
			t.Errorf("GET /config/%s: %d", name, rec.Code)
		}
		if rec := c.post("/config/"+name, url.Values{"content": {"{}"}}); rec.Code != http.StatusNotFound {
			t.Errorf("POST /config/%s: %d", name, rec.Code)
		}
	}
}

func TestRegistryValidationNeedsEveryPacksConfig(t *testing.T) {
	s, eng := newTestServer(t)
	got := eng.cfg.ValidateConfig("packs.json", []byte(`{"packs": ["icon-sets-demo", "icon-sets-ghost"]}`))
	if len(got) != 1 || !strings.Contains(got[0].Text, "icon-sets-ghost") {
		t.Fatalf("findings %+v", got)
	}
	_ = s
}

func TestGlobalConfigIsCreatedAndReloaded(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)
	page := c.get("/config/global").Body.String()
	if !strings.Contains(page, "does not exist yet") {
		t.Fatal("a missing global config is not explained")
	}
	rec := c.post("/config/global", url.Values{"content": {`{"git": {"base": "develop"}}`}, "base": {""}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create: %d\n%s", rec.Code, rec.Body.String())
	}
	path := filepath.Join(eng.cfg.GlobalHome, "icon-sets-sync", "config.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	for _, row := range eng.cfg.Settings().Rows {
		if row.Name == "git.base" && (row.Value != "develop" || row.Origin != "global") {
			t.Errorf("the saved global config was not reloaded: %+v", row)
		}
	}
	bad := c.post("/config/global", url.Values{"content": {`{"nope": 1}`}, "base": {editorBase(t, c, "global")}})
	if bad.Code != http.StatusUnprocessableEntity {
		t.Errorf("an unknown global key was saved: %d", bad.Code)
	}
}

func TestReadOnlyEditor(t *testing.T) {
	s, _ := newTestServer(t, func(o *Options) { o.ReadOnly = true })
	c := signIn(t, s)
	body := c.get("/config/icon-sets-demo.json").Body.String()
	if !strings.Contains(body, " readonly>") || strings.Contains(body, `id="editor-save"`) {
		t.Error("a read-only server offers an editable editor")
	}
	if rec := c.post("/config/icon-sets-demo.json", url.Values{"content": {demoPackJSON}}); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("save on a read-only server: %d", rec.Code)
	}
	if rec := c.post("/config/icon-sets-demo.json/validate", url.Values{"content": {demoPackJSON}}); rec.Code != http.StatusOK {
		t.Errorf("validate on a read-only server: %d", rec.Code)
	}
}

// A read-only directory -- the Docker image mounts config/ :ro -- is found by
// trying to write, and the editor says so instead of failing on save.
func TestAReadOnlyDirectoryIsDetected(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root POSIX permission check")
	}
	s, eng := newTestServer(t)
	c := signIn(t, s)
	base := editorBase(t, c, "icon-sets-demo.json")
	if err := os.Chmod(eng.cfg.ConfigDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(eng.cfg.ConfigDir, 0o755) }()
	body := c.get("/config/icon-sets-demo.json").Body.String()
	if !strings.Contains(body, "Read-only:") || !strings.Contains(body, " readonly>") {
		t.Error("the editor did not detect the read-only directory")
	}
	rec := c.post("/config/icon-sets-demo.json", url.Values{"content": {demoPackJSON}, "base": {base}})
	if rec.Code == http.StatusSeeOther {
		t.Error("a save into a read-only directory reported success")
	}
}
