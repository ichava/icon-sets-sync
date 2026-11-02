package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

const testHost = "127.0.0.1:7878"

var fixedNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// fakeEngine answers from fixtures and counts the checks it was asked for.
type fakeEngine struct {
	mu     sync.Mutex
	packs  []*config.PackConfig
	checks int
	calls  []runCall
	block  chan struct{}
	// cfg answers the config editor's calls: a real AppEngine over a fixture
	// directory, because the editor's whole point is the real loaders.
	cfg *AppEngine
}

func (f *fakeEngine) ConfigFiles() ([]ConfigFile, error)     { return f.cfg.ConfigFiles() }
func (f *fakeEngine) ReadConfig(n string) (ConfigDoc, error) { return f.cfg.ReadConfig(n) }
func (f *fakeEngine) ValidateConfig(n string, d []byte) []app.Finding {
	return f.cfg.ValidateConfig(n, d)
}
func (f *fakeEngine) WriteConfig(n string, d []byte, base string) error {
	return f.cfg.WriteConfig(n, d, base)
}

// demoPackJSON is a valid pack config with an authoring note.
const demoPackJSON = `{
  "_note": "kept verbatim by the editor",
  "name": "icon-sets-demo",
  "pack": "ichava/icon-sets-demo",
  "pack_root": "/work/icon-sets-demo",
  "current_version": "1.0.0",
  "version_check_url": "https://registry.npmjs.org/demo/latest",
  "source": {"type": "npm", "package": "demo"},
  "sinks": [{"type": "filesystem", "root": "{pack_root}/files"}]
}
`

// configFixture writes a config directory and an XDG config home, and
// returns them.
func configFixture(t *testing.T) (dir, home string) {
	t.Helper()
	root := t.TempDir()
	dir, home = filepath.Join(root, "config"), filepath.Join(root, "xdg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"packs.json":          `{"packs": ["icon-sets-demo"]}` + "\n",
		"icon-sets-demo.json": demoPackJSON,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, home
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

func (f *fakeEngine) Pack(slug string) (*config.PackConfig, error) {
	for _, p := range f.packs {
		if p.Name == slug {
			return p, nil
		}
	}
	return nil, fmt.Errorf("missing pack config: %s.json", slug)
}

func (f *fakeEngine) PackSettings(p *config.PackConfig) (SettingsView, error) {
	r, err := app.PackSettings(settings.Stack{}, p)
	if err != nil {
		return SettingsView{}, err
	}
	return BuildSettingsView(r, "/work/config", ""), nil
}

func (f *fakeEngine) PackNotes(p *config.PackConfig) map[string]string {
	if p.Name == "icon-sets-tabler" {
		return map[string]string{"_note": "Tabler ships outline and filled variants."}
	}
	return nil
}

// runCall is one Sync or Recipe the fake was asked for.
type runCall struct {
	Kind    string
	Packs   []string
	Req     RunRequest
	Version string
}

func (f *fakeEngine) record(c runCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
}

func (f *fakeEngine) runs() []runCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]runCall(nil), f.calls...)
}

// Sync emits what a real run would through the hooks: a start, one stage, a
// log line, a done. With block set it waits for its context, so a test can
// cancel it mid-run.
func (f *fakeEngine) Sync(ctx context.Context, packs []*config.PackConfig, req RunRequest, h RunHooks) []app.Outcome {
	names := make([]string, len(packs))
	for i, p := range packs {
		names[i] = p.Name
	}
	f.record(runCall{Kind: "sync", Packs: names, Req: req})
	out := make([]app.Outcome, len(packs))
	for i, p := range packs {
		h.Starting(p, checker.Result{Current: p.CurrentVersion, Latest: "9.9.9", Stale: true}, req.DryRun)
		h.Observer.StageStarted(p.Name+"@9.9.9", "NpmTarball")
		h.Logger.Info("npm pack", "spec", p.Name+"@9.9.9")
		if f.block != nil {
			select {
			case <-ctx.Done():
				h.Observer.StageFinished(p.Name+"@9.9.9", "NpmTarball", nil, ctx.Err())
				out[i] = app.Outcome{Pack: p.Pack, Status: app.StatusFailed, Err: ctx.Err()}
				h.Done(out[i])
				continue
			case <-f.block:
			}
		}
		h.Observer.StageFinished(p.Name+"@9.9.9", "NpmTarball", map[string]any{"npm": map[string]any{"files": 3}}, nil)
		out[i] = app.Outcome{Pack: p.Pack, Status: app.StatusSynced, DryRun: req.DryRun,
			Result: pipeline.Result{Success: true, Metrics: map[string]any{"filesystem": map[string]any{"copied": 3}}}}
		h.Done(out[i])
	}
	return out
}

func (f *fakeEngine) Recipe(_ context.Context, p *config.PackConfig, version string, req RunRequest, h RunHooks) (pipeline.Result, error) {
	f.record(runCall{Kind: "recipe", Packs: []string{p.Name}, Req: req, Version: version})
	h.Logger.Info("recipe", "version", version)
	return pipeline.Result{Success: true}, nil
}

func (f *fakeEngine) AuditManifestPack(_ context.Context, req AuditRequest, h AuditHooks) ([]manifestpack.Resolution, error) {
	f.record(runCall{Kind: "audit", Packs: []string{req.Set, req.Package}})
	h.Starting(2, "/work/aggregate/resources/assets/svg/files")
	rs := []manifestpack.Resolution{
		{Set: "tabler-outline", Icons: 4900, Package: "@tabler/icons", Version: "3.49.0", Path: "icons/outline", Matched: 4900, Identical: 4900, Confidence: manifestpack.ConfidenceExact},
		{Set: "mystery-icons", Icons: 120, Confidence: manifestpack.ConfidenceNone, Note: "no candidate reproduced any icon"},
	}
	for _, r := range rs {
		h.SetDone(r)
	}
	return rs, nil
}

const testToken = "test-launch-token"

func newTestServer(t *testing.T, mutate ...func(*Options)) (*Server, *fakeEngine) {
	t.Helper()
	eng := newFakeEngine()
	dir, home := configFixture(t)
	eng.cfg = &AppEngine{ConfigDir: dir, GlobalHome: home}
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

// waitJob waits for the job behind a /jobs/{id} path to finish.
func waitJob(t *testing.T, s *Server, path string) {
	t.Helper()
	j := s.jobs.Get(strings.TrimPrefix(path, "/jobs/"))
	if j == nil {
		t.Fatalf("no job at %s", path)
	}
	select {
	case <-j.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("job did not finish")
	}
}
