package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

func TestAuditRunsAndRendersItsResults(t *testing.T) {
	dir := t.TempDir()
	s, eng := newTestServer(t, func(o *Options) { o.HistoryDir = dir })
	c := signIn(t, s)
	if body := c.get("/manifest-pack/audit").Body.String(); !strings.Contains(body, "No audit has run yet") {
		t.Fatal("the empty state is missing")
	}
	path := startRun(t, c, "/manifest-pack/audit", url.Values{"set": {"tabler-outline"}, "package": {"@tabler/icons"}})
	waitJob(t, s, path)
	if calls := eng.runs(); len(calls) != 1 || calls[0].Packs[0] != "tabler-outline" || calls[0].Packs[1] != "@tabler/icons" {
		t.Fatalf("calls %+v", calls)
	}

	body := c.get("/manifest-pack/audit").Body.String()
	for _, want := range []string{"tabler-outline", "@tabler/icons@3.49.0", "icons/outline", "mystery-icons", "no candidate reproduced", "manifest.json"} {
		if !strings.Contains(body, want) {
			t.Errorf("audit page lacks %q", want)
		}
	}
	id := strings.TrimPrefix(path, "/jobs/")
	rec := c.get("/manifest-pack/audit/" + id + "/manifest.json")
	var m manifestpack.Manifest
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest: %d %v", rec.Code, err)
	}
	if m.Pack != "ichava/icon-sets-bundled" || m.Sets["tabler-outline"].Package != "@tabler/icons" {
		t.Fatalf("manifest %+v", m)
	}
	if _, ok := m.Sets["mystery-icons"]; ok {
		t.Error("an unresolved set was written into the manifest")
	}

	// The results are part of the run, so they survive a restart.
	again, _ := newTestServer(t, func(o *Options) { o.HistoryDir = dir })
	if body := signIn(t, again).get("/manifest-pack/audit").Body.String(); !strings.Contains(body, "tabler-outline") {
		t.Error("the audit results did not survive a restart")
	}
}

func TestAuditFormIsChecked(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)
	for _, form := range []url.Values{
		{"set": {"../etc"}},
		{"set": {"a"}, "package": {"not a package; rm -rf"}},
		{"set": {"a"}, "package": {"--registry=http://evil"}},
		{"package": {"@tabler/icons"}},
	} {
		if rec := c.post("/manifest-pack/audit", form); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d", form, rec.Code)
		}
	}
	if n := len(eng.runs()); n != 0 {
		t.Errorf("%d audits started from bad forms", n)
	}
}

func TestAuditIsARunSoReadOnlyHasNone(t *testing.T) {
	s, _ := newTestServer(t, func(o *Options) { o.ReadOnly = true })
	c := signIn(t, s)
	if body := c.get("/manifest-pack/audit").Body.String(); strings.Contains(body, `action="/manifest-pack/audit"`) {
		t.Error("a read-only server offers an audit")
	}
	if rec := c.post("/manifest-pack/audit", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /manifest-pack/audit on a read-only server: %d", rec.Code)
	}
}

// The audit's former address, /bundled/audit, was removed: none of its
// routes answers any more, while the current address still does.
func TestRemovedAuditRouteIsNotFound(t *testing.T) {
	dir := t.TempDir()
	s, eng := newTestServer(t, func(o *Options) { o.HistoryDir = dir })
	c := signIn(t, s)

	if rec := c.get("/manifest-pack/audit"); rec.Code != http.StatusOK {
		t.Fatalf("GET /manifest-pack/audit: %d", rec.Code)
	}
	for _, path := range []string{"/bundled/audit", "/bundled/audit/1/manifest.json"} {
		if rec := c.get(path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", path, rec.Code)
		}
	}
	form := url.Values{"set": {"tabler-outline"}, "package": {"@tabler/icons"}}
	if rec := c.post("/bundled/audit", form); rec.Code != http.StatusNotFound {
		t.Errorf("POST /bundled/audit: %d, want 404", rec.Code)
	}
	if calls := eng.runs(); len(calls) != 0 {
		t.Errorf("the removed route started an audit: %+v", calls)
	}
}

// The page names a private config directory it found, and loading it writes
// private_config_dir into the global config. Only a found directory is taken.
func TestAuditOffersTheFoundPrivateConfigDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "aggregate", ".dev", "icon-sets-sync")
	s, eng := newTestServer(t)
	eng.privateCandidates = []string{dir}
	t.Cleanup(func() { config.SetPrivateConfigDir("") })
	c := signIn(t, s)
	body := c.get("/manifest-pack/audit").Body.String()
	if !strings.Contains(body, dir) || !strings.Contains(body, "Use this directory") {
		t.Fatal("the found directory is not offered")
	}
	if rec := c.post("/settings/private-config-dir", url.Values{"dir": {"/etc"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("a directory the server did not find was accepted: %d", rec.Code)
	}
	if rec := c.post("/settings/private-config-dir", url.Values{"dir": {dir}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("load = %d:\n%s", rec.Code, rec.Body.String())
	}
	global, _ := os.ReadFile(filepath.Join(eng.cfg.GlobalHome, settings.AppDir, "config.json"))
	if !strings.Contains(string(global), `"private_config_dir": "`+dir+`"`) {
		t.Errorf("global config:\n%s", global)
	}
	if config.PrivateConfigDir() != dir {
		t.Errorf("not applied: %q", config.PrivateConfigDir())
	}
}

// A set the pack does not have starts nothing and offers the near names.
func TestAuditRefusesAnUnknownSet(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)
	rec := c.post("/manifest-pack/audit", url.Values{"set": {"tabler"}})
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `data-role="unknown-set"`) ||
		!strings.Contains(body, `name="set" value="tabler-icons"`) || !strings.Contains(body, `name="set" value="tabler-outline"`) {
		t.Fatalf("%d:\n%s", rec.Code, body)
	}
	if n := len(eng.runs()); n != 0 {
		t.Errorf("%d audits started", n)
	}
}

// A run recorded before Resolution.Error existed (the owner's screenshot,
// 2026-11-09): its lstat note is shown as a cause and a fix, counted as could
// not read, and its "succeeded" is qualified.
func TestAnOldAuditsReadErrorIsExplained(t *testing.T) {
	rs := []manifestpack.Resolution{{Set: "tabler", Confidence: manifestpack.ConfidenceNone,
		Note: "lstat /p/aggregate/resources/assets/svg/files/tabler: no such file or directory"}}
	got := explainAuditErrors(rs, []string{"mdi", "tabler-icons"})
	p, ok := got["tabler"]
	if !ok || rs[0].Error == "" || rs[0].Note != "" || len(p.Suggestions) != 1 || p.Suggestions[0] != "tabler-icons" ||
		!strings.Contains(p.Cause, "no set directory named tabler") {
		t.Fatalf("%+v %+v", rs, got)
	}
	// With the config not loaded there is no set list to suggest from.
	unloaded := explainAuditErrors([]manifestpack.Resolution{{Set: "tabler", Note: rs[0].Error}}, nil)["tabler"]
	if !strings.Contains(unloaded.Fix, "Load the aggregator pack's config") {
		t.Errorf("fix without a set list: %q", unloaded.Fix)
	}
}

// A survey that could not read a set did not survey it: the run fails.
func TestAnAuditWithAnUnreadableSetFails(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	path := startRun(t, c, "/manifest-pack/audit", url.Values{"set": {"broken-set"}})
	waitJob(t, s, path)
	info := s.jobs.Get(strings.TrimPrefix(path, "/jobs/")).Info()
	if info.Status != jobs.StatusFailed || !strings.Contains(info.Err, "1 of 1 sets could not be read") {
		t.Fatalf("%+v", info)
	}
	body := c.get("/manifest-pack/audit").Body.String()
	for _, want := range []string{"could not read", "permission denied", `data-role="set-problem"`,
		`<div class="text-secondary">none</div><div class="h2 mb-0">0</div>`} {
		if !strings.Contains(body, want) {
			t.Errorf("audit page lacks %q", want)
		}
	}
}
