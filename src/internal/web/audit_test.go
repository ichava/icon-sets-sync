package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
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
