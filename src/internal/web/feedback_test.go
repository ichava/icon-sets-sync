package web

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The dashboard's alert bar: info with a spinner and n of N while checking,
// then success or warning with a relative and an absolute time, dismissible,
// and announced.
func TestDashboardAlertBar(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	_ = c.get("/")
	if !s.waitIdle(5 * time.Second) {
		t.Fatal("check did not finish")
	}
	body := c.get("/").Body.String()
	for _, want := range []string{`id="check-status"`, `role="status" aria-live="polite"`, `data-bs-dismiss="alert"`, "Upstream checked", `data-role="checked-at"`} {
		if !strings.Contains(body, want) {
			t.Errorf("alert bar lacks %q", want)
		}
	}
	if !strings.Contains(body, "alert-success") && !strings.Contains(body, "alert-warning") {
		t.Error("a finished check is neither success nor warning")
	}

	// While a check runs: the info state, a spinner and a progress bar.
	s.status.mu.Lock()
	s.status.running, s.status.done, s.status.total = true, 2, 5
	s.status.mu.Unlock()
	body = c.get("/").Body.String()
	for _, want := range []string{"alert-info", "spinner-border", "2 of 5 packs", `<progress class="progress progress-sm mt-2 w-100" data-role="check-progress" value="2" max="5"`} {
		if !strings.Contains(body, want) {
			t.Errorf("running alert bar lacks %q", want)
		}
	}
	var st checkStatus
	if err := json.Unmarshal(c.get("/status.json").Body.Bytes(), &st); err != nil || !st.Running || st.Done != 2 || st.Total != 5 {
		t.Errorf("status.json = %+v %v", st, err)
	}
	s.status.mu.Lock()
	s.status.running = false
	s.status.mu.Unlock()

	if !strings.Contains(c.get("/?checked=1").Body.String(), `data-toast-title="Upstream checked"`) {
		t.Error("no toast after a check finished")
	}
}

// Every page carries the toast container and its template, and a save asks
// for a toast.
func TestToastsAndProgressMarkup(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	body := c.get("/settings").Body.String()
	for _, want := range []string{`id="toasts"`, `<template id="toast-template">`, `data-progress="Saving packages_dirs"`} {
		if !strings.Contains(body, want) {
			t.Errorf("settings lacks %q", want)
		}
	}
	if !strings.Contains(c.get("/config/icon-sets-demo.json?saved=1").Body.String(), `data-toast="success"`) {
		t.Error("a save renders no toast")
	}
}

// A real sync is confirmed in a modal on the page that starts it: every form
// that can start one names what to type, and the layout carries the modal.
// The server still checks the phrase, and the confirmation page stays as the
// fallback without JavaScript, now without a second dialog stacked on it.
func TestRealSyncConfirmsInAModal(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	pack := c.get("/packs/icon-sets-flag").Body.String()
	for _, want := range []string{
		`action="/packs/icon-sets-flag/sync" data-real-phrase="icon-sets-flag"`,
		`action="/packs/icon-sets-flag/recipe" data-real-phrase="icon-sets-flag"`,
		`id="real-run-modal"`, `id="real-run-confirm"`, `id="real-run-start" disabled`,
	} {
		if !strings.Contains(pack, want) {
			t.Errorf("pack page lacks %q", want)
		}
	}
	dash := c.get("/").Body.String()
	if !strings.Contains(dash, `action="/sync-all" data-real-phrase="all packs"`) || !strings.Contains(dash, `data-role="pack-sync-form" data-real-phrase=""`) {
		t.Error("the dashboard's run forms do not name a phrase")
	}
	confirm := c.post("/packs/icon-sets-flag/sync", url.Values{"mode": {"real"}}).Body.String()
	if !strings.Contains(confirm, `name="confirm"`) || strings.Contains(confirm, `data-confirm-title="Start a real sync?"`) {
		t.Error("the fallback page is missing its field, or still stacks a second confirm")
	}
}
