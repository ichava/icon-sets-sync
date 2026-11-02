package web

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

func startRun(t *testing.T, c *client, path string, form url.Values) string {
	t.Helper()
	rec := c.post(path, form)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/jobs/") {
		t.Fatalf("POST %s: %d %s\n%s", path, rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	return rec.Header().Get("Location")
}

func TestARunIsADryRunUnlessTheFormSaysReal(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)
	for _, form := range []url.Values{nil, {"mode": {"dry-run"}}, {"mode": {"REAL"}}, {"mode": {""}}} {
		waitJob(t, s, startRun(t, c, "/packs/icon-sets-flag/sync", form))
	}
	for i, call := range eng.runs() {
		if !call.Req.DryRun {
			t.Errorf("run %d was not a dry run: %+v", i, call)
		}
	}
}

func TestARealRunNeedsTheTypedConfirmation(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)

	first := c.post("/packs/icon-sets-flag/sync", url.Values{"mode": {"real"}, "force": {"1"}})
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "This is not a dry run") ||
		!strings.Contains(first.Body.String(), `name="force" value="1"`) {
		t.Fatalf("no confirmation page: %d", first.Code)
	}
	wrong := c.post("/packs/icon-sets-flag/sync", url.Values{"mode": {"real"}, "confirm": {"icon-sets-tabler"}})
	if wrong.Code != http.StatusOK || !strings.Contains(wrong.Body.String(), "did not match") {
		t.Fatalf("a wrong confirmation was not refused: %d", wrong.Code)
	}
	if n := len(eng.runs()); n != 0 {
		t.Fatalf("%d runs started before confirmation", n)
	}

	waitJob(t, s, startRun(t, c, "/packs/icon-sets-flag/sync", url.Values{"mode": {"real"}, "force": {"1"}, "confirm": {"icon-sets-flag"}}))
	calls := eng.runs()
	if len(calls) != 1 || calls[0].Req.DryRun || !calls[0].Req.Force {
		t.Fatalf("calls %+v", calls)
	}

	all := c.post("/sync-all", url.Values{"mode": {"real"}})
	if all.Code != http.StatusOK || !strings.Contains(all.Body.String(), "<code>all packs</code>") {
		t.Fatalf("sync-all did not ask: %d", all.Code)
	}
	waitJob(t, s, startRun(t, c, "/sync-all", url.Values{"mode": {"real"}, "confirm": {"all packs"}, "concurrency": {"2"}}))
	last := eng.runs()[1]
	if last.Req.DryRun || last.Req.Concurrency != 2 || len(last.Packs) != 3 {
		t.Fatalf("sync-all call %+v", last)
	}
}

func TestBadRunFormsAreRefused(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)
	for _, v := range []string{"0", "17", "x"} {
		if rec := c.post("/sync-all", url.Values{"concurrency": {v}}); rec.Code != http.StatusBadRequest {
			t.Errorf("concurrency %s: %d", v, rec.Code)
		}
	}
	for _, slug := range []string{"..%2F..%2Fetc", "Icon-Sets", "nope"} {
		if rec := c.post("/packs/"+slug+"/sync", nil); rec.Code != http.StatusNotFound {
			t.Errorf("slug %s: %d", slug, rec.Code)
		}
	}
	if n := len(eng.runs()); n != 0 {
		t.Errorf("%d runs started from bad forms", n)
	}
}

func TestOneRunPerPackAndCancel(t *testing.T) {
	s, eng := newTestServer(t)
	eng.block = make(chan struct{})
	c := signIn(t, s)

	path := startRun(t, c, "/packs/icon-sets-flag/sync", nil)
	busy := c.post("/sync-all", nil)
	if busy.Code != http.StatusConflict || !strings.Contains(busy.Body.String(), `href="`+path+`"`) {
		t.Fatalf("a second run on a busy pack: %d\n%s", busy.Code, busy.Body.String())
	}
	if page := c.get("/packs/icon-sets-flag").Body.String(); !strings.Contains(page, "A run is in progress") {
		t.Error("the pack page does not say a run is in progress")
	}
	live := c.get(path).Body.String()
	if !strings.Contains(live, `data-live="1"`) || !strings.Contains(live, "/cancel") {
		t.Error("a running job's page is not live or has no Cancel")
	}

	if rec := c.post(path+"/cancel", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("cancel: %d", rec.Code)
	}
	waitJob(t, s, path)
	j := s.jobs.Get(strings.TrimPrefix(path, "/jobs/"))
	if j.Info().Status != jobs.StatusCancelled {
		t.Fatalf("status %s", j.Info().Status)
	}
	if body := c.get(path).Body.String(); !strings.Contains(body, "Cancelled") || strings.Contains(body, `data-live="1"`) {
		t.Error("the cancelled job's page does not say so")
	}
	if rec := c.post("/jobs/20990101T000000Z-abcdef/cancel", nil); rec.Code != http.StatusNotFound {
		t.Errorf("cancelling an unknown job: %d", rec.Code)
	}
}

func TestRecipeRunsAtTheVersionAsked(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)
	waitJob(t, s, startRun(t, c, "/packs/icon-sets-tabler/recipe", url.Values{"version": {"3.48.0"}}))
	calls := eng.runs()
	if len(calls) != 1 || calls[0].Kind != "recipe" || calls[0].Version != "3.48.0" || !calls[0].Req.DryRun {
		t.Fatalf("calls %+v", calls)
	}
}

// sseEvents parses a text/event-stream body into (id, event) pairs.
func sseEvents(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	var id, typ string
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			typ = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var e jobs.Event
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e); err != nil {
				t.Fatalf("data is not an event: %v", err)
			}
			if e.Type != typ {
				t.Fatalf("event %s carries type %s", typ, e.Type)
			}
		case line == "":
			if id != "" {
				out = append(out, id+":"+typ)
			}
			id, typ = "", ""
		}
	}
	return out
}

func TestEventsReplayAFinishedJob(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	path := startRun(t, c, "/packs/icon-sets-flag/sync", nil)
	waitJob(t, s, path)
	id := strings.TrimPrefix(path, "/jobs/")

	rec := c.get("/events/jobs/" + id)
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	got := strings.Join(sseEvents(t, rec.Body.String()), " ")
	want := "1:pack-start 2:stage 3:log 4:stage 5:pack-done 6:done"
	if got != want {
		t.Fatalf("events\n got %s\nwant %s", got, want)
	}

	r := httptest.NewRequest(http.MethodGet, "http://"+testHost+"/events/jobs/"+id, nil)
	r.Header.Set("Last-Event-ID", "4")
	r.AddCookie(c.cookie)
	if got := strings.Join(sseEvents(t, do(s, r).Body.String()), " "); got != "5:pack-done 6:done" {
		t.Fatalf("resume after 4: %s", got)
	}
}

// syncWriter is a goroutine-safe ResponseWriter that can flush, so a test can
// read a live event stream while the handler is still writing it.
type syncWriter struct {
	mu      sync.Mutex
	header  http.Header
	body    strings.Builder
	flushes int
}

func (w *syncWriter) Header() http.Header { return w.header }
func (w *syncWriter) WriteHeader(int)     {}
func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(p)
}
func (w *syncWriter) Flush() {
	w.mu.Lock()
	w.flushes++
	w.mu.Unlock()
}
func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

func TestEventsTailALiveJob(t *testing.T) {
	s, eng := newTestServer(t)
	eng.block = make(chan struct{})
	c := signIn(t, s)
	path := startRun(t, c, "/packs/icon-sets-flag/sync", nil)
	id := strings.TrimPrefix(path, "/jobs/")

	w := &syncWriter{header: http.Header{}}
	r := httptest.NewRequest(http.MethodGet, "http://"+testHost+"/events/jobs/"+id, nil)
	r.AddCookie(c.cookie)
	done := make(chan struct{})
	go func() {
		s.Handler().ServeHTTP(w, r)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(w.String(), "event: log") {
		if time.Now().After(deadline) {
			t.Fatalf("the stream did not deliver the live events:\n%s", w.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if strings.Contains(w.String(), "event: done") {
		t.Fatal("done arrived before the job finished")
	}
	close(eng.block)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end with the job")
	}
	if got := strings.Join(sseEvents(t, w.String()), " "); !strings.HasSuffix(got, "5:pack-done 6:done") {
		t.Fatalf("tail %s", got)
	}
}

func TestHistorySurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	s, _ := newTestServer(t, func(o *Options) { o.HistoryDir = dir })
	c := signIn(t, s)
	path := startRun(t, c, "/packs/icon-sets-flag/sync", nil)
	waitJob(t, s, path)

	again, _ := newTestServer(t, func(o *Options) { o.HistoryDir = dir })
	c2 := signIn(t, again)
	list := c2.get("/jobs").Body.String()
	if !strings.Contains(list, `href="`+path+`"`) {
		t.Fatalf("the restarted server's history lacks the run:\n%s", list)
	}
	page := c2.get(path)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "filesystem copied 3") {
		t.Fatalf("a stored run does not render: %d", page.Code)
	}
	dl := c2.get(path + "/log.jsonl")
	lines := strings.Split(strings.TrimSpace(dl.Body.String()), "\n")
	if dl.Code != http.StatusOK || len(lines) != 7 || !strings.Contains(dl.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("download: %d, %d lines", dl.Code, len(lines))
	}
	if got := sseEvents(t, c2.get("/events/jobs/"+strings.TrimPrefix(path, "/jobs/")).Body.String()); len(got) != 6 {
		t.Fatalf("a stored run replays %d events", len(got))
	}
}

func TestRunsFilter(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	waitJob(t, s, startRun(t, c, "/packs/icon-sets-flag/sync", nil))
	waitJob(t, s, startRun(t, c, "/packs/icon-sets-tabler/recipe", nil))
	count := func(q string) int { return strings.Count(c.get("/jobs"+q).Body.String(), "/log.jsonl\">") }
	if n := count(""); n != 2 {
		t.Errorf("all: %d", n)
	}
	if n := count("?pack=icon-sets-tabler"); n != 1 {
		t.Errorf("by pack: %d", n)
	}
	if n := count("?outcome=failed"); n != 0 {
		t.Errorf("by outcome: %d", n)
	}
}

func TestPackPage(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	body := c.get("/packs/icon-sets-tabler").Body.String()
	for _, want := range []string{"Tabler ships outline and filled variants.", "git.base", `action="/packs/icon-sets-tabler/sync"`, `value="dry-run" checked`} {
		if !strings.Contains(body, want) {
			t.Errorf("pack page lacks %q", want)
		}
	}
	ro, _ := newTestServer(t, func(o *Options) { o.ReadOnly = true })
	if body := signIn(t, ro).get("/packs/icon-sets-tabler").Body.String(); strings.Contains(body, "/sync\"") {
		t.Error("a read-only pack page offers a sync")
	}
}

func TestValidSlug(t *testing.T) {
	for slug, want := range map[string]bool{
		"icon-sets-flag": true, "a1": true,
		"": false, "../etc": false, "Icon": false, "a/b": false, "a.b": false, strings.Repeat("a", 101): false,
	} {
		if validSlug(slug) != want {
			t.Errorf("validSlug(%q) = %v", slug, !want)
		}
	}
}
