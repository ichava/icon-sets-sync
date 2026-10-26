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
	for _, path := range []string{"/manifest-pack/audit", "/bundled/audit"} {
		if rec := c.post(path, nil); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s on a read-only server: %d", path, rec.Code)
		}
	}
}

// The former address serves the same handlers, unchanged, and says it is
// deprecated in its response headers. It goes away no earlier than 0.2.0.
func TestLegacyAuditRouteStillWorksAndIsDeprecated(t *testing.T) {
	dir := t.TempDir()
	s, eng := newTestServer(t, func(o *Options) { o.HistoryDir = dir })
	c := signIn(t, s)

	legacy, current := c.get("/bundled/audit"), c.get("/manifest-pack/audit")
	if legacy.Code != http.StatusOK || legacy.Body.String() != current.Body.String() {
		t.Fatalf("GET /bundled/audit: %d, same page as /manifest-pack/audit: %v", legacy.Code, legacy.Body.String() == current.Body.String())
	}
	if legacy.Header().Get("Deprecation") != "true" || legacy.Header().Get("Link") != `</manifest-pack/audit>; rel="successor-version"` {
		t.Errorf("legacy headers: Deprecation=%q Link=%q", legacy.Header().Get("Deprecation"), legacy.Header().Get("Link"))
	}
	if current.Header().Get("Deprecation") != "" {
		t.Error("the current route says it is deprecated")
	}

	path := startRun(t, c, "/bundled/audit", url.Values{"set": {"tabler-outline"}, "package": {"@tabler/icons"}})
	waitJob(t, s, path)
	if calls := eng.runs(); len(calls) != 1 || calls[0].Packs[0] != "tabler-outline" {
		t.Fatalf("POST /bundled/audit did not start the audit: %+v", calls)
	}
	id := strings.TrimPrefix(path, "/jobs/")
	old, now := c.get("/bundled/audit/"+id+"/manifest.json"), c.get("/manifest-pack/audit/"+id+"/manifest.json")
	if old.Code != http.StatusOK || old.Body.String() != now.Body.String() {
		t.Fatalf("legacy manifest download: %d, identical: %v", old.Code, old.Body.String() == now.Body.String())
	}
	if want := "</manifest-pack/audit/" + id + `/manifest.json>; rel="successor-version"`; old.Header().Get("Link") != want {
		t.Errorf("Link = %q, want %q", old.Header().Get("Link"), want)
	}
	if rec := c.post("/bundled/audit", url.Values{"set": {"../etc"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("the legacy route skipped the form checks: %d", rec.Code)
	}
}
