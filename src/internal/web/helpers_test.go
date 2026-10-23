package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

const testHost = "127.0.0.1:7878"

var fixedNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// fakeEngine answers from fixtures and counts the checks it was asked for.
type fakeEngine struct {
	mu     sync.Mutex
	packs  []*config.PackConfig
	checks int
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{packs: []*config.PackConfig{
		{Name: "icon-sets-tabler", Pack: "ichava/icon-sets-tabler", CurrentVersion: "3.40.0",
			Source: config.SourceConfig{Type: "npm", Package: "@tabler/icons"}},
		{Name: "icon-sets-flag", Pack: "ichava/icon-sets-flag", CurrentVersion: "7.2.3",
			Source: config.SourceConfig{Type: "npm", Package: "flag-icons"}},
		{Name: "icon-sets-emoji", Pack: "ichava/icon-sets-emoji", CurrentVersion: "16.0.1",
			Source: config.SourceConfig{Type: "url"}},
	}}
}

func (f *fakeEngine) Packs() ([]*config.PackConfig, error) { return f.packs, nil }

func (f *fakeEngine) Check(_ context.Context, packs []*config.PackConfig) []checker.Result {
	f.mu.Lock()
	f.checks++
	f.mu.Unlock()
	out := make([]checker.Result, len(packs))
	for i, p := range packs {
		switch p.Name {
		case "icon-sets-tabler":
			out[i] = checker.Result{Package: p.Pack, Current: "3.40.0", Latest: "3.49.0", Stale: true,
				ReleaseURL: "https://github.com/tabler/tabler-icons/releases/tag/v3.49.0"}
		case "icon-sets-flag":
			out[i] = checker.Result{Package: p.Pack, Current: "7.2.3", Latest: "7.2.3"}
		default:
			out[i] = checker.Result{Package: p.Pack, Current: "16.0.1", Reason: "GET https://example.invalid: 503"}
		}
	}
	return out
}

func (f *fakeEngine) checkCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checks
}

func (f *fakeEngine) PackStats(_ context.Context, p *config.PackConfig) PackStats {
	switch p.Name {
	case "icon-sets-tabler":
		return PackStats{SVGs: 6202, Branch: "main"}
	case "icon-sets-flag":
		return PackStats{SVGs: 542, Branch: "chore/sync-upstream-7.2.3", Dirty: true}
	}
	return PackStats{Err: "open /work/icon-sets-emoji: no such file or directory"}
}

func (f *fakeEngine) Settings() SettingsView {
	stack := settings.Stack{
		Global: settings.Values{"git.base": "develop"},
		Env:    settings.Values{"concurrency": 2},
	}
	return BuildSettingsView(stack.Resolve(nil), "/work/config", "/home/me/.config/icon-sets-sync/config.json")
}

const testToken = "test-launch-token"

func newTestServer(t *testing.T, mutate ...func(*Options)) (*Server, *fakeEngine) {
	t.Helper()
	eng := newFakeEngine()
	opts := Options{
		Engine:       eng,
		Token:        testToken,
		AllowedHosts: AllowedHosts("127.0.0.1", 7878, nil),
		Version:      "test",
		BindAddr:     testHost,
		Now:          func() time.Time { return fixedNow },
	}
	for _, m := range mutate {
		m(&opts)
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, eng
}

// client carries a signed-in session through the handler, no socket involved.
type client struct {
	t      *testing.T
	s      *Server
	cookie *http.Cookie
	csrf   string
}

var csrfMeta = regexp.MustCompile(`<meta name="csrf-token" content="([^"]*)">`)

func signIn(t *testing.T, s *Server) *client {
	t.Helper()
	rec := do(s, httptest.NewRequest(http.MethodGet, "http://"+testHost+"/?token="+testToken, nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("token exchange: %d", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies %v", cookies)
	}
	c := &client{t: t, s: s, cookie: cookies[0]}
	page := c.get("/settings")
	m := csrfMeta.FindStringSubmatch(page.Body.String())
	if m == nil || m[1] == "" {
		t.Fatal("no CSRF token on a signed-in page")
	}
	c.csrf = m[1]
	return c
}

func do(s *Server, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	return rec
}

func (c *client) get(path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "http://"+testHost+path, nil)
	r.AddCookie(c.cookie)
	return do(c.s, r)
}

// post sends a same-origin form post with the session's CSRF token.
func (c *client) post(path string, form url.Values) *httptest.ResponseRecorder {
	if form == nil {
		form = url.Values{}
	}
	form.Set(csrfField, c.csrf)
	r := httptest.NewRequest(http.MethodPost, "http://"+testHost+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://"+testHost)
	r.AddCookie(c.cookie)
	return do(c.s, r)
}
