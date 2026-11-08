package web

import (
	"strings"
	"testing"
)

// U1: the navbar was navbar-expand with no toggler, so at phone width every
// nav link, the Stop server button and the theme links sat in one row and the
// page overflowed to 721px on a 390px screen. Tabler's documented navbar
// collapses below md behind a navbar-toggler that targets navbar-collapse.
func TestNavbarCollapsesBehindATogglerAsTablerDocuments(t *testing.T) {
	s, _ := newTestServer(t)
	body := signIn(t, s).get("/").Body.String()
	for _, want := range []string{
		`class="navbar navbar-expand-md d-print-none"`,
		`class="navbar-toggler collapsed" type="button" data-bs-toggle="collapse" data-bs-target="#navbar-menu" aria-controls="navbar-menu" aria-expanded="false" aria-label="Toggle navigation"`,
		`<span class="navbar-toggler-icon"></span>`,
		`<div class="collapse navbar-collapse" id="navbar-menu">`,
		`<span class="nav-link-icon">`,
		`<span class="nav-link-title">Packs</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("layout lacks %s", want)
		}
	}
	// The nav links live inside the collapse, not beside it.
	menu := body[strings.Index(body, `id="navbar-menu"`):]
	if !strings.Contains(menu[:strings.Index(menu, "</ul>")], `href="/jobs"`) {
		t.Error("the Runs link is not inside navbar-menu")
	}
}

// U3: the light, dark and system links all showed at once with nothing to say
// which theme was on. Tabler shows the one that switches away, with
// hide-theme-dark and hide-theme-light.
func TestThemeLinksShowOnlyTheSwitchAway(t *testing.T) {
	s, _ := newTestServer(t)
	body := signIn(t, s).get("/").Body.String()
	for _, want := range []string{
		`href="?theme=dark" class="nav-link px-2 hide-theme-dark"`,
		`href="?theme=light" class="nav-link px-2 hide-theme-light"`,
		`data-bs-title="Dark theme" aria-label="Dark theme"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("layout lacks %s", want)
		}
	}
}

// U4: no page named an icon, so every load asked for /favicon.ico and got a
// 404 in the console.
func TestEveryPageNamesAnEmbeddedFavicon(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	body := c.get("/").Body.String()
	i := strings.Index(body, `<link rel="icon" type="image/svg+xml" href="`)
	if i < 0 {
		t.Fatal("no favicon link")
	}
	href := body[i+len(`<link rel="icon" type="image/svg+xml" href="`):]
	href = href[:strings.Index(href, `"`)]
	res := c.get(href)
	if res.Code != 200 || !strings.HasPrefix(res.Header().Get("Content-Type"), "image/svg+xml") {
		t.Errorf("GET %s = %d %q", href, res.Code, res.Header().Get("Content-Type"))
	}
}

// U2: a commit SHA used as a version (svg-logos, game-icons, fluent-emoji)
// was printed in full, twice per row, which pushed the packs table past the
// card at 1366px. It shows 12 characters and keeps the whole value in title.
func TestShortVersion(t *testing.T) {
	sha := "82d948812bfe3f269ef8f731dcdb07b08160edc4"
	if got := string(shortVersion(sha)); got != `82d948812bfe` {
		t.Errorf("shortVersion(sha) = %s", got)
	}
	for _, v := range []string{"3.50.0", "2026-09-24-unicode18_0", "", "abc"} {
		if got := string(shortVersion(v)); got != v {
			t.Errorf("shortVersion(%q) = %s, want it unchanged", v, got)
		}
	}
	if got := string(shortVersion(`<b>`)); got != "&lt;b&gt;" {
		t.Errorf("shortVersion escapes: %s", got)
	}
}

// Page-header actions are a Tabler btn-list, not hand-spaced flex.
func TestPageHeaderActionsUseBtnList(t *testing.T) {
	s, _ := newTestServer(t)
	body := signIn(t, s).get("/").Body.String()
	if !strings.Contains(body, `<div class="btn-list">`) {
		t.Error("the dashboard header actions are not a btn-list")
	}
}

// U8: before the first check finished the cards read "Up to date 0",
// "Behind upstream 0" and "Vendored SVGs 0" -- a result nobody had measured.
// Until a check has completed they show a dash.
func TestDashboardTotalsWaitForTheFirstCheck(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	s.status.mu.Lock()
	s.status.running = true
	s.status.mu.Unlock()
	body := c.get("/").Body.String()
	for _, label := range []string{"Up to date", "Behind upstream", "Vendored SVGs"} {
		want := `<div class="text-secondary">` + label + `</div><div class="h2 mb-0">–</div>`
		if !strings.Contains(body, want) {
			t.Errorf("unchecked %q card does not show a dash", label)
		}
	}
}

// U9: a check failure's reason is one long URL with no break opportunity, so
// it set the Status column's width and pushed the packs table past its card.
// Every error text inside a table cell may break anywhere, or is truncated
// with the whole text in its tooltip.
func TestTableErrorTextMayBreak(t *testing.T) {
	for _, name := range []string{"templates/dashboard.html", "templates/jobs.html"} {
		b, err := templateFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, field := range []string{"{{.Result.Reason}}", "{{.Stats.Err}}", "{{.Err}}"} {
			for i := strings.Index(s, field); i >= 0; {
				open := strings.LastIndex(s[:i], "<")
				if !strings.Contains(s[open:i], "text-break") && !strings.Contains(s[open:i], "text-truncate") {
					t.Errorf("%s: %s sits in %q without text-break or text-truncate", name, field, s[open:i])
				}
				next := strings.Index(s[i+1:], field)
				if next < 0 {
					break
				}
				i += 1 + next
			}
		}
	}
}

// U12: without the bundled pack's private config the audit page offered
// Start audit, and every run failed at once with "missing pack config".
// The page now says what is missing and how to supply it, and the button
// is disabled.
func TestAuditPageSaysWhenTheManifestPackConfigIsMissing(t *testing.T) {
	s, _ := newTestServer(t)
	body := signIn(t, s).get("/manifest-pack/audit").Body.String()
	for _, want := range []string{"--private-config-dir", `class="alert alert-warning"`, "Start audit</button>"} {
		if !strings.Contains(body, want) {
			t.Errorf("audit page lacks %q", want)
		}
	}
	if !strings.Contains(body, `<button type="submit" class="btn btn-primary w-100" disabled>`) {
		t.Error("Start audit is not disabled")
	}
}
