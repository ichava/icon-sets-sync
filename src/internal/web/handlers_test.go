package web

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDashboardChecksOnceAndCaches(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)

	first := c.get("/")
	if first.Code != http.StatusOK {
		t.Fatalf("dashboard %d", first.Code)
	}
	if !s.waitIdle(5 * time.Second) {
		t.Fatal("the background check never finished")
	}
	body := c.get("/").Body.String()
	for _, want := range []string{"icon-sets-tabler", "Update available", "Up to date", "Check failed", "6,202", "uncommitted changes", "3.49.0"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard lacks %q", want)
		}
	}
	c.get("/")
	if n := eng.checkCount(); n != 1 {
		t.Errorf("%d checks for three page loads; the cache should have answered", n)
	}

	if rec := c.post("/check", nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	s.waitIdle(5 * time.Second)
	if n := eng.checkCount(); n != 2 {
		t.Errorf("refresh did not re-check: %d", n)
	}
}

func TestDashboardRechecksAfterTheTTL(t *testing.T) {
	now := fixedNow
	s, eng := newTestServer(t, func(o *Options) { o.Now = func() time.Time { return now } })
	c := signIn(t, s)
	c.get("/")
	s.waitIdle(5 * time.Second)
	now = now.Add(11 * time.Minute)
	c.get("/")
	s.waitIdle(5 * time.Second)
	if n := eng.checkCount(); n != 2 {
		t.Errorf("checks %d, want 2 after the TTL", n)
	}
}

func TestDashboardAsksForAReloadWhileChecking(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	s.status.mu.Lock()
	s.status.running = true
	s.status.mu.Unlock()
	body := c.get("/").Body.String()
	if !strings.Contains(body, `data-reload-after="4"`) || !strings.Contains(body, "Checking upstream registries") {
		t.Error("a running check is not announced")
	}
}

func TestSettingsShowsOrigins(t *testing.T) {
	s, _ := newTestServer(t)
	body := signIn(t, s).get("/settings").Body.String()
	for _, want := range []string{"git.base", "develop", ">global<", ">env<", "/work/config", testHost} {
		if !strings.Contains(body, want) {
			t.Errorf("settings lacks %q", want)
		}
	}
}

func TestLicencesPageListsEveryVendoredPackage(t *testing.T) {
	s, _ := newTestServer(t)
	body := signIn(t, s).get("/about/licenses").Body.String()
	for _, want := range []string{"@tabler/core", "@tabler/icons-sprite", "bootstrap", "@popperjs/core", "Permission is hereby granted"} {
		if !strings.Contains(body, want) {
			t.Errorf("licences page lacks %q", want)
		}
	}
}

func TestSchemaIsServed(t *testing.T) {
	s, _ := newTestServer(t)
	rec := signIn(t, s).get("/schema/pack.schema.json")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"$schema"`) {
		t.Fatalf("%d", rec.Code)
	}
}

func TestAssetsAreFingerprintedAndImmutable(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	u := mustAsset(t, s, "vendor/tabler/tabler.min.css")
	rec := c.get(u)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") ||
		rec.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	stale := "/assets/000000000000/vendor/tabler/tabler.min.css"
	if rec := c.get(stale); rec.Code != http.StatusNotFound {
		t.Errorf("a stale fingerprint served content: %d", rec.Code)
	}
	if rec := c.get("/assets/" + s.assets.hashes["app.css"] + "/../server.go"); rec.Code == http.StatusOK {
		t.Error("an asset path escaped the asset set")
	}
}
