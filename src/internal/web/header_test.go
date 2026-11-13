package web

import (
	"net/http"
	"strings"
	"testing"
)

// The header follows preview.tabler.io's (owner, 2026-11-08): a logo mark and
// the product's name, a search button that opens the command palette, the
// theme toggle, a recent-runs dropdown and a server menu.
func TestHeaderFollowsTheTablerPreview(t *testing.T) {
	s, _ := newTestServer(t)
	body := signIn(t, s).get("/").Body.String()
	for _, want := range []string{
		`<title>Packs · ` + AppName + `</title>`,
		`<span class="navbar-brand-text d-none d-sm-inline">` + AppName + `</span>`,
		`class="navbar-brand-mark"`,
		`data-bs-target="#command-palette"`,
		`aria-label="Recent runs"`,
		`data-role="server-menu"`,
		`href="/about"`,
		`data-bs-target="#shortcuts-modal"`,
		`action="/server/stop"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("header lacks %s", want)
		}
	}
}

// The command palette lists every page, the actions a page offers, and every
// pack, so any of them is a few keystrokes away.
func TestCommandPaletteListsPagesActionsAndPacks(t *testing.T) {
	s, eng := newTestServer(t)
	body := signIn(t, s).get("/jobs").Body.String()
	i := strings.Index(body, `id="command-palette"`)
	if i < 0 {
		t.Fatal("no command palette")
	}
	palette := body[i:]
	palette = palette[:strings.Index(palette, `data-role="palette-empty"`)]
	for _, want := range []string{
		`href="/"`, `href="/jobs"`, `href="/manifest-pack/audit"`, `href="/config"`, `href="/settings"`, `href="/about"`,
		`data-command="check"`, `data-command="sync-all-dry-run"`, `data-command="theme"`, `data-command="shortcuts"`,
		`<input type="search" class="form-control" id="command-palette-input"`,
	} {
		if !strings.Contains(palette, want) {
			t.Errorf("palette lacks %s", want)
		}
	}
	for _, p := range eng.packs {
		if !strings.Contains(palette, `href="/packs/`+p.Name+`"`) {
			t.Errorf("palette lacks pack %s", p.Name)
		}
	}
	ro, _ := newTestServer(t, func(o *Options) { o.ReadOnly = true })
	if strings.Contains(signIn(t, ro).get("/").Body.String(), `data-command="sync-all-dry-run"`) {
		t.Error("a read-only palette offers a sync")
	}
}

// The shortcuts dialog lists every binding app.js handles.
func TestShortcutsDialogListsTheBindings(t *testing.T) {
	s, _ := newTestServer(t)
	body := signIn(t, s).get("/").Body.String()
	js, err := assetFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range shortcuts {
		if !strings.Contains(body, sc.Description) {
			t.Errorf("shortcuts dialog lacks %q", sc.Description)
		}
		if sc.JS != "" && !strings.Contains(string(js), sc.JS) {
			t.Errorf("app.js does not handle %q (%s)", sc.Description, sc.JS)
		}
	}
	if len(shortcuts) < 8 {
		t.Fatalf("%d shortcuts, want at least 8", len(shortcuts))
	}
}

// About names the build, the runtime and where things are.
func TestAboutPage(t *testing.T) {
	s, _ := newTestServer(t)
	res := signIn(t, s).get("/about")
	if res.Code != 200 {
		t.Fatalf("GET /about = %d", res.Code)
	}
	body := res.Body.String()
	for _, want := range []string{"<h1 class=\"page-title\">About " + AppName + "</h1>", "Version", "Commit", "Built", "Go", "Platform", "Listening on", testHost, "Config dir", "https://github.com/ichava/icon-sets-sync", "/about/licenses"} {
		if !strings.Contains(body, want) {
			t.Errorf("about lacks %q", want)
		}
	}
}

// The favicon is the header's logo mark: a primary rounded square holding
// the packages glyph, so the tab and the header read as one product.
func TestFaviconMatchesTheLogoMark(t *testing.T) {
	b, err := assetFS.ReadFile("assets/favicon.svg")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<rect`, `rx=`, `fill="#066fd1"`, `stroke="#fff"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("favicon lacks %s", want)
		}
	}
}

// The owner, 2026-11-08: the server menu was too long and its names too
// wordy. It keeps four short items; licences, documentation and issues moved
// into About's tabs. Its trigger shows a short version, not a snapshot name.
func TestServerMenuIsShort(t *testing.T) {
	s, _ := newTestServer(t, func(o *Options) { o.Version = "0.1.0-SNAPSHOT-907a2a5" })
	body := signIn(t, s).get("/").Body.String()
	i := strings.Index(body, `data-role="server-menu"`)
	if i < 0 {
		t.Fatal("no server menu")
	}
	menu := body[i:]
	menu = menu[:strings.Index(menu, `</form>`)]
	if n := strings.Count(menu, `class="dropdown-item`); n != 4 {
		t.Errorf("server menu has %d items, want 4 (About, Settings, Shortcuts, Stop server)", n)
	}
	for _, want := range []string{">About</a>", ">Settings</a>", ">Shortcuts</button>", ">Stop server</button>", ">Server</div>", ">v0.1.0&#43;907a2a5</div>"} {
		if !strings.Contains(menu, want) {
			t.Errorf("server menu lacks %s", want)
		}
	}
	for _, gone := range []string{"Third-party licences", "Documentation", "Report an issue", "SNAPSHOT"} {
		if strings.Contains(menu, gone) {
			t.Errorf("server menu still carries %q", gone)
		}
	}
}

func TestShortVersionLabel(t *testing.T) {
	for in, want := range map[string]string{
		"0.1.0-SNAPSHOT-907a2a5": "v0.1.0+907a2a5", "0.1.0-SNAPSHOT": "v0.1.0", "0.1.0": "v0.1.0", "v1.2.3": "v1.2.3", "dev": "dev", "test": "test", "": "dev",
	} {
		if got := versionLabel(in); got != want {
			t.Errorf("versionLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// About is the home of the reference material, as tabs that work as links,
// so each is linkable and works without JavaScript; /about/licenses keeps
// working and opens the Licences tab.
func TestAboutTabs(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	tabHref := map[string]string{"overview": "/about", "shortcuts": "/about?tab=shortcuts", "licences": "/about/licenses", "help": "/about?tab=help"}
	for path, active := range map[string]string{
		"/about": "overview", "/about?tab=shortcuts": "shortcuts", "/about?tab=licences": "licences",
		"/about?tab=help": "help", "/about/licenses": "licences", "/about?tab=nonsense": "overview",
	} {
		res := c.get(path)
		if res.Code == http.StatusSeeOther {
			res = c.get(res.Header().Get("Location"))
		}
		if res.Code != 200 {
			t.Fatalf("GET %s = %d", path, res.Code)
		}
		body := res.Body.String()
		for tab, href := range tabHref {
			if !strings.Contains(body, `href="`+href+`"`) {
				t.Errorf("%s: no %s tab", path, tab)
			}
		}
		if !strings.Contains(body, `class="nav-link active" aria-current="page" href="`+tabHref[active]+`"`) {
			t.Errorf("%s: the %s tab is not the active one", path, active)
		}
	}
	lic := c.get("/about/licenses").Body.String()
	if !strings.Contains(lic, "@tabler/core") || !strings.Contains(lic, "Permission is hereby granted") {
		t.Error("the Licences tab does not show the notices")
	}
	help := c.get("/about?tab=help").Body.String()
	for _, want := range []string{"/tree/main/docs", "/issues", "CHANGELOG.md"} {
		if !strings.Contains(help, want) {
			t.Errorf("the Help tab lacks %s", want)
		}
	}
}

// One label everywhere: the header, the footer, Settings and About show the
// same string for a snapshot build (the owner's screenshot, 2026-11-09).
func TestEveryPageShowsTheSameVersionLabel(t *testing.T) {
	s, _ := newTestServer(t, func(o *Options) {
		o.Version = "0.1.0-SNAPSHOT-9201d04"
		o.Build.Version = "0.1.0-SNAPSHOT-9201d04"
	})
	c := signIn(t, s)
	for _, page := range []string{"/", "/settings", "/about"} {
		body := c.get(page).Body.String()
		if strings.Contains(body, "0.1.0-SNAPSHOT-9201d04") {
			t.Errorf("%s shows the raw version string", page)
		}
		if !strings.Contains(body, "v0.1.0&#43;9201d04") { // html/template writes + as &#43;
			t.Errorf("%s lacks the label v0.1.0+9201d04", page)
		}
	}
}
