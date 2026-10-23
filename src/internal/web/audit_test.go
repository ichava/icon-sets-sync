package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/bundled"
)

func TestAuditRunsAndRendersItsResults(t *testing.T) {
	dir := t.TempDir()
	s, eng := newTestServer(t, func(o *Options) { o.HistoryDir = dir })
	c := signIn(t, s)
	if body := c.get("/bundled/audit").Body.String(); !strings.Contains(body, "No audit has run yet") {
		t.Fatal("the empty state is missing")
	}
	path := startRun(t, c, "/bundled/audit", url.Values{"set": {"tabler-outline"}, "package": {"@tabler/icons"}})
	waitJob(t, s, path)
	if calls := eng.runs(); len(calls) != 1 || calls[0].Packs[0] != "tabler-outline" || calls[0].Packs[1] != "@tabler/icons" {
		t.Fatalf("calls %+v", calls)
	}

	body := c.get("/bundled/audit").Body.String()
	for _, want := range []string{"tabler-outline", "@tabler/icons@3.49.0", "icons/outline", "mystery-icons", "no candidate reproduced", "manifest.json"} {
		if !strings.Contains(body, want) {
			t.Errorf("audit page lacks %q", want)
		}
	}
	id := strings.TrimPrefix(path, "/jobs/")
	rec := c.get("/bundled/audit/" + id + "/manifest.json")
	var m bundled.Manifest
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
	if body := signIn(t, again).get("/bundled/audit").Body.String(); !strings.Contains(body, "tabler-outline") {
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
		if rec := c.post("/bundled/audit", form); rec.Code != http.StatusBadRequest {
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
	if body := c.get("/bundled/audit").Body.String(); strings.Contains(body, `action="/bundled/audit"`) {
		t.Error("a read-only server offers an audit")
	}
	if rec := c.post("/bundled/audit", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST on a read-only server: %d", rec.Code)
	}
}
