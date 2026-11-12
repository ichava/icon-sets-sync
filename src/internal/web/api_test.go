package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/apitoken"
	"github.com/ichava/icon-sets-sync/src/internal/core/jsonlog"
	"github.com/ichava/icon-sets-sync/src/internal/events"
)

// apiFixture is a server with the API on, a token store in a temporary
// directory, and helpers to make tokens and call the API through the
// handler, no socket involved.
type apiFixture struct {
	t      *testing.T
	s      *Server
	eng    *fakeEngine
	tokens *apitoken.Store
	audit  *jsonlog.Log
	clock  *testClock
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newAPIFixture(t *testing.T, mutate ...func(*Options)) *apiFixture {
	t.Helper()
	dir := t.TempDir()
	clock := &testClock{now: fixedNow}
	f := &apiFixture{t: t, clock: clock,
		tokens: &apitoken.Store{Path: filepath.Join(dir, apitoken.FileName), Now: clock.Now},
		audit:  &jsonlog.Log{Path: filepath.Join(dir, "api-audit.jsonl")}}
	f.s, f.eng = newTestServer(t, append([]func(*Options){func(o *Options) {
		o.API = APIOptions{Enabled: true, Tokens: f.tokens, RateLimit: 1000, EventsStream: true, Audit: f.audit}
		o.Now = clock.Now
	}}, mutate...)...)
	return f
}

func (f *apiFixture) token(scopes ...apitoken.Scope) string {
	f.t.Helper()
	_, secret, err := f.tokens.Create("t"+strings.ReplaceAll(string(scopes[0]), ":", "-")+randomSuffix(), scopes, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	return secret
}

var suffixN struct {
	sync.Mutex
	n int
}

func randomSuffix() string {
	suffixN.Lock()
	defer suffixN.Unlock()
	suffixN.n++
	return "-" + strings.Repeat("x", suffixN.n%5+1) + string(rune('a'+suffixN.n%26))
}

func (f *apiFixture) call(token, method, path string, body any) *httptest.ResponseRecorder {
	f.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			f.t.Fatal(err)
		}
		rd = bytes.NewReader(data)
	}
	r := httptest.NewRequest(method, "http://"+testHost+path, rd)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if rd != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	return do(f.s, r)
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON (%d): %s", rec.Code, rec.Body.String())
	}
	return out
}

// The API answers nothing on a server built without it: a bearer token
// opens no door there, because there is no API.
func TestAPIIsOffUnlessEnabled(t *testing.T) {
	s, _ := newTestServer(t)
	store := &apitoken.Store{Path: filepath.Join(t.TempDir(), apitoken.FileName)}
	_, secret, err := store.Create("ci", []apitoken.Scope{apitoken.ScopeAdmin}, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "http://"+testHost+"/api/v1", nil)
	r.Header.Set("Authorization", "Bearer "+secret)
	if rec := do(s, r); rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), `"api"`) {
		t.Fatalf("API answered while off: %d %s", rec.Code, rec.Body.String())
	}
	if s.apiMux != nil {
		t.Fatal("API routes registered while off")
	}
}

func TestAPIIndexWithAReadToken(t *testing.T) {
	f := newAPIFixture(t)
	rec := f.call(f.token(apitoken.ScopeRead), http.MethodGet, "/api/v1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	doc := decodeJSON(t, rec)
	if doc["api"] != "v1" || doc["read_only"] != false {
		t.Fatalf("index %v", doc)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("the API must send no CORS header")
	}
}

// Guard: a token without the scope a route needs is refused. Proven red by
// making scoped() call h without the check: every 403 below became 2xx.
func TestAPITokenWithoutTheScopeIsRefused(t *testing.T) {
	f := newAPIFixture(t)
	read := f.token(apitoken.ScopeRead)
	sync := f.token(apitoken.ScopeSync)
	write := f.token(apitoken.ScopeConfigWrite)
	admin := f.token(apitoken.ScopeAdmin)
	for _, c := range []struct {
		token, method, path string
		body                any
		want                int
	}{
		{read, http.MethodPost, "/api/v1/packs/icon-sets-flag/sync", nil, http.StatusForbidden},
		{read, http.MethodPost, "/api/v1/sync", nil, http.StatusForbidden},
		{read, http.MethodPost, "/api/v1/check", nil, http.StatusForbidden},
		{read, http.MethodPut, "/api/v1/settings/page_size", map[string]any{"value": 30}, http.StatusForbidden},
		{read, http.MethodGet, "/api/v1/tokens", nil, http.StatusForbidden},
		{sync, http.MethodGet, "/api/v1/packs", nil, http.StatusForbidden},
		{sync, http.MethodPut, "/api/v1/settings/page_size", map[string]any{"value": 30}, http.StatusForbidden},
		{write, http.MethodPost, "/api/v1/sync", nil, http.StatusForbidden},
		{write, http.MethodGet, "/api/v1/webhooks/deliveries", nil, http.StatusForbidden},
		{sync, http.MethodPost, "/api/v1/server/stop", nil, http.StatusForbidden},
		// The controls: the right scope, and admin, which carries every scope.
		{read, http.MethodGet, "/api/v1/packs", nil, http.StatusOK},
		{sync, http.MethodPost, "/api/v1/check", nil, http.StatusAccepted},
		{admin, http.MethodGet, "/api/v1/tokens", nil, http.StatusOK},
		{admin, http.MethodGet, "/api/v1/packs", nil, http.StatusOK},
	} {
		rec := f.call(c.token, c.method, c.path, c.body)
		if rec.Code != c.want {
			t.Errorf("%s %s: %d, want %d: %s", c.method, c.path, rec.Code, c.want, rec.Body.String())
		}
		if c.want == http.StatusForbidden && rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s %s: a refusal must be problem+json, got %q", c.method, c.path, rec.Header().Get("Content-Type"))
		}
	}
	f.s.waitIdle(5 * time.Second)
}

// Guard: an expired or a revoked token is refused. Proven red by making
// Authenticate skip the State check: both answered 200.
func TestAPIExpiredOrRevokedTokenIsRefused(t *testing.T) {
	f := newAPIFixture(t)
	_, expiring, err := f.tokens.Create("expiring", []apitoken.Scope{apitoken.ScopeRead}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if rec := f.call(expiring, http.MethodGet, "/api/v1", nil); rec.Code != http.StatusOK {
		t.Fatalf("a fresh token: %d", rec.Code)
	}
	f.clock.Add(2 * time.Hour)
	rec := f.call(expiring, http.MethodGet, "/api/v1", nil)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "expired") {
		t.Fatalf("expired: %d %s", rec.Code, rec.Body.String())
	}

	_, revoked, err := f.tokens.Create("revoked", []apitoken.Scope{apitoken.ScopeRead}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rec := f.call(revoked, http.MethodGet, "/api/v1", nil); rec.Code != http.StatusOK {
		t.Fatalf("before revoking: %d", rec.Code)
	}
	if _, err := f.tokens.Revoke("revoked"); err != nil {
		t.Fatal(err)
	}
	rec = f.call(revoked, http.MethodGet, "/api/v1", nil)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "revoked") {
		t.Fatalf("revoked: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.call("iss_not-a-real-token", http.MethodGet, "/api/v1", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown: %d", rec.Code)
	}
}

// The two doors have two locks: the session cookie does not open /api/, and
// a bearer token does not open a page.
func TestSessionAndBearerDoNotCross(t *testing.T) {
	f := newAPIFixture(t)
	c := signIn(t, f.s)
	r := httptest.NewRequest(http.MethodGet, "http://"+testHost+"/api/v1", nil)
	r.AddCookie(c.cookie)
	if rec := do(f.s, r); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a session cookie on /api/v1: %d", rec.Code)
	}
	r = httptest.NewRequest(http.MethodGet, "http://"+testHost+"/api/v1?token="+testToken, nil)
	if rec := do(f.s, r); rec.Code != http.StatusBadRequest {
		t.Fatalf("the launch token in an API URL: %d", rec.Code)
	}
	admin := f.token(apitoken.ScopeAdmin)
	for _, path := range []string{"/", "/settings", "/jobs", "/status.json"} {
		r := httptest.NewRequest(http.MethodGet, "http://"+testHost+path, nil)
		r.Header.Set("Authorization", "Bearer "+admin)
		if rec := do(f.s, r); rec.Code != http.StatusUnauthorized {
			t.Errorf("a bearer token on %s: %d", path, rec.Code)
		}
	}
}

func TestAPISyncIsADryRunUnlessAskedOtherwise(t *testing.T) {
	f := newAPIFixture(t)
	tok := f.token(apitoken.ScopeSync, apitoken.ScopeRead)
	rec := f.call(tok, http.MethodPost, "/api/v1/packs/icon-sets-flag/sync", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	waitJob(t, f.s, strings.TrimPrefix(rec.Header().Get("Location"), "/api/v1"))
	rec = f.call(tok, http.MethodPost, "/api/v1/packs/icon-sets-flag/sync", map[string]any{"dry_run": false})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	waitJob(t, f.s, strings.TrimPrefix(rec.Header().Get("Location"), "/api/v1"))
	runs := f.eng.runs()
	if len(runs) != 2 || !runs[0].Req.DryRun || runs[1].Req.DryRun {
		t.Fatalf("runs %+v: want a dry run, then a real one", runs)
	}
	job := f.call(tok, http.MethodGet, rec.Header().Get("Location"), nil)
	if job.Code != http.StatusOK || !strings.Contains(job.Body.String(), `"status": "synced"`) {
		t.Fatalf("job: %d %s", job.Code, job.Body.String())
	}
	if rec := f.call(tok, http.MethodPost, "/api/v1/sync", map[string]any{"concurrency": 1 << 40}); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unbounded concurrency must be refused: %d", rec.Code)
	}
	if rec := f.call(tok, http.MethodPost, "/api/v1/packs/icon-sets-flag/sync", `{"dry_rnu": false}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a misspelt field must be refused, not ignored: %d", rec.Code)
	}
}

func TestAPIIdempotencyKeyReplaysTheRun(t *testing.T) {
	f := newAPIFixture(t)
	tok := f.token(apitoken.ScopeSync)
	first := httptest.NewRequest(http.MethodPost, "http://"+testHost+"/api/v1/sync", nil)
	first.Header.Set("Authorization", "Bearer "+tok)
	first.Header.Set("Idempotency-Key", "nightly-2026-11-12")
	rec := do(f.s, first)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	waitJob(t, f.s, strings.TrimPrefix(rec.Header().Get("Location"), "/api/v1"))
	again := httptest.NewRequest(http.MethodPost, "http://"+testHost+"/api/v1/sync", nil)
	again.Header.Set("Authorization", "Bearer "+tok)
	again.Header.Set("Idempotency-Key", "nightly-2026-11-12")
	rec2 := do(f.s, again)
	if rec2.Code != http.StatusOK || rec2.Header().Get("Location") != rec.Header().Get("Location") {
		t.Fatalf("replay: %d %s at %s", rec2.Code, rec2.Body.String(), rec2.Header().Get("Location"))
	}
	if n := len(f.eng.runs()); n != 1 {
		t.Fatalf("%d runs; the key must not start a second", n)
	}
}

func TestAPIReadOnlyServesNoWriteRoute(t *testing.T) {
	f := newAPIFixture(t, func(o *Options) { o.ReadOnly = true })
	admin := f.token(apitoken.ScopeAdmin)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/sync"},
		{http.MethodPost, "/api/v1/packs/icon-sets-flag/sync"},
		{http.MethodPost, "/api/v1/jobs/20261023T120000Z-abcdef/cancel"},
		{http.MethodPut, "/api/v1/settings/page_size"},
		{http.MethodDelete, "/api/v1/settings/page_size"},
		{http.MethodPost, "/api/v1/webhooks/ci/test"},
	} {
		req := httptest.NewRequest(c.method, "http://"+testHost+c.path, strings.NewReader(`{"value": 30}`))
		if _, pattern := f.s.apiMux.Handler(req); pattern != apiPrefix {
			t.Errorf("%s %s is registered on a read-only server (%s)", c.method, c.path, pattern)
		}
		if rec := f.call(admin, c.method, c.path, `{"value": 30}`); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: %d", c.method, c.path, rec.Code)
		}
	}
	if len(f.eng.runs()) != 0 {
		t.Fatal("a read-only server ran a sync")
	}
	if rec := f.call(admin, http.MethodGet, "/api/v1/packs", nil); rec.Code != http.StatusOK {
		t.Fatalf("reads stay: %d", rec.Code)
	}
}

func TestAPIBodyIsCapped(t *testing.T) {
	f := newAPIFixture(t)
	tok := f.token(apitoken.ScopeConfigWrite)
	big := `{"value": "` + strings.Repeat("a", MaxAPIBody) + `"}`
	if rec := f.call(tok, http.MethodPut, "/api/v1/settings/git.base", big); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestAPIRateLimitPerToken(t *testing.T) {
	f := newAPIFixture(t, func(o *Options) { o.API.RateLimit = 2 })
	tok := f.token(apitoken.ScopeRead)
	other := f.token(apitoken.ScopeRead)
	for i := 0; i < 2; i++ {
		if rec := f.call(tok, http.MethodGet, "/api/v1", nil); rec.Code != http.StatusOK {
			t.Fatalf("request %d: %d", i, rec.Code)
		}
	}
	rec := f.call(tok, http.MethodGet, "/api/v1", nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("third: %d", rec.Code)
	}
	if rec := f.call(other, http.MethodGet, "/api/v1", nil); rec.Code != http.StatusOK {
		t.Fatalf("another token has its own bucket: %d", rec.Code)
	}
	f.clock.Add(time.Minute)
	if rec := f.call(tok, http.MethodGet, "/api/v1", nil); rec.Code != http.StatusOK {
		t.Fatalf("after a minute: %d", rec.Code)
	}
}

func TestAPISettingIsCheckedSavedAndAudited(t *testing.T) {
	f := newAPIFixture(t)
	sub := f.s.Bus().Subscribe(10, events.ConfigChanged)
	defer sub.Close()
	tok := f.token(apitoken.ScopeConfigWrite)
	rec := f.call(tok, http.MethodPut, "/api/v1/settings/page_size", map[string]any{"value": 3})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "at least 5") {
		t.Fatalf("an out-of-range value: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.call(tok, http.MethodPut, "/api/v1/settings/page_size", map[string]any{"value": 30})
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	doc, err := f.eng.ReadConfig(GlobalName)
	if err != nil || !strings.Contains(string(doc.Data), `"page_size": 30`) {
		t.Fatalf("global config %q, %v", doc.Data, err)
	}
	select {
	case e := <-sub.C():
		if err := events.Validate(e); err != nil || strings.Contains(string(e.Data), "30") {
			t.Fatalf("config.changed %s: %v (it must name the key, never the value)", e.Data, err)
		}
	case <-time.After(time.Second):
		t.Fatal("no config.changed event")
	}
	if rec := f.call(tok, http.MethodDelete, "/api/v1/settings/page_size", nil); rec.Code != http.StatusOK {
		t.Fatalf("reset: %d", rec.Code)
	}
	recs, err := f.audit.Tail(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("%d audit records for three writes", len(recs))
	}
	var last AuditRecord
	if err := json.Unmarshal(recs[2], &last); err != nil {
		t.Fatal(err)
	}
	if last.Method != http.MethodDelete || last.Status != http.StatusOK || !strings.HasPrefix(last.Token, "tconfig-write") ||
		last.Route != "DELETE /api/v1/settings/{name}" {
		t.Fatalf("audit %+v", last)
	}
	if strings.Contains(string(recs[0]), "iss_") {
		t.Fatal("the audit log must never hold a token")
	}
}

// syncRecorder is a ResponseWriter a streaming handler and a test can share.
type syncRecorder struct {
	mu     sync.Mutex
	header http.Header
	buf    bytes.Buffer
}

func (r *syncRecorder) Header() http.Header { return r.header }
func (r *syncRecorder) WriteHeader(int)     {}
func (r *syncRecorder) Flush()              {}
func (r *syncRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}
func (r *syncRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

func TestAPIEventStreamSendsCloudEvents(t *testing.T) {
	f := newAPIFixture(t)
	tok := f.token(apitoken.ScopeRead)
	req := httptest.NewRequest(http.MethodGet, "http://"+testHost+"/api/v1/events?type=webhook.test", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	ctx, cancel := context.WithCancel(req.Context())
	rec := &syncRecorder{header: http.Header{}}
	done := make(chan struct{})
	go func() {
		f.s.Handler().ServeHTTP(rec, req.WithContext(ctx))
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(rec.String(), "icon-sets-sync events") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	f.s.Bus().Emit(events.ServerStopping, map[string]any{"reason": "signal"})
	f.s.Bus().Emit(events.WebhookTest, map[string]any{"name": "ci", "message": "hello"})
	for !strings.Contains(rec.String(), "event: webhook.test") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	body := rec.String()
	if !strings.Contains(body, `"specversion":"1.0"`) || strings.Contains(body, "server.stopping") {
		t.Fatalf("stream:\n%s", body)
	}
}

// Every event a run publishes is a valid CloudEvents envelope whose data
// matches its type's schema.
func TestRunEventsMatchTheirSchemas(t *testing.T) {
	f := newAPIFixture(t)
	sub := f.s.Bus().Subscribe(100)
	defer sub.Close()
	tok := f.token(apitoken.ScopeSync)
	rec := f.call(tok, http.MethodPost, "/api/v1/sync", map[string]any{"packs": []string{"icon-sets-flag"}})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	waitJob(t, f.s, strings.TrimPrefix(rec.Header().Get("Location"), "/api/v1"))
	f.call(tok, http.MethodPost, "/api/v1/check", nil)
	f.s.waitIdle(5 * time.Second)
	seen := map[string]int{}
	timeout := time.After(2 * time.Second)
	for seen[events.UpstreamChecked] == 0 {
		select {
		case e := <-sub.C():
			if err := events.Validate(e); err != nil {
				t.Errorf("%s: %v (%s)", e.Type, err, e.Data)
			}
			seen[e.Type]++
		case <-timeout:
			t.Fatalf("events seen %v", seen)
		}
	}
	for _, typ := range []string{events.JobStarted, events.JobProgress, events.PackSynced, events.JobFinished, events.PackBehindUpstream} {
		if seen[typ] == 0 {
			t.Errorf("no %s among %v", typ, seen)
		}
	}
}

func TestAPIAnswersProblemsForUnknownRoutes(t *testing.T) {
	f := newAPIFixture(t)
	rec := f.call(f.token(apitoken.ScopeRead), http.MethodGet, "/api/v1/nope", nil)
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Status != http.StatusNotFound || p.Type == "" || p.Title == "" {
		t.Fatalf("problem %+v, %v", p, err)
	}
}
