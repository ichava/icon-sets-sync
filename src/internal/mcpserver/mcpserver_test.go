package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/jsonlog"
	"github.com/ichava/icon-sets-sync/src/internal/web"
	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

type fakeEngine struct {
	mu     sync.Mutex
	runs   []web.RunRequest
	global []byte
}

var packs = []*config.PackConfig{
	{Name: "icon-sets-flag", Pack: "ichava/icon-sets-flag", CurrentVersion: "7.2.3", Source: config.SourceConfig{Type: "npm", Package: "flag-icons"}},
}

func (f *fakeEngine) Packs() ([]*config.PackConfig, error) { return packs, nil }
func (f *fakeEngine) Pack(slug string) (*config.PackConfig, error) {
	for _, p := range packs {
		if p.Name == slug {
			return p, nil
		}
	}
	return nil, jobs.ErrNotFound
}
func (f *fakeEngine) PackNotes(*config.PackConfig) map[string]string { return nil }
func (f *fakeEngine) Check(_ context.Context, ps []*config.PackConfig) []checker.Result {
	out := make([]checker.Result, len(ps))
	for i := range ps {
		out[i] = checker.Result{Current: "7.2.3", Latest: "7.3.0", Stale: true}
	}
	return out
}
func (f *fakeEngine) ConfigFiles() ([]web.ConfigFile, error) {
	return []web.ConfigFile{{Name: web.GlobalName}}, nil
}
func (f *fakeEngine) ValidateConfig(string, []byte) []app.Finding { return nil }
func (f *fakeEngine) ReadConfig(string) (web.ConfigDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return web.ConfigDoc{ConfigFile: web.ConfigFile{Name: web.GlobalName, Path: "/home/me/.config/icon-sets-sync/config.json"}, Exists: f.global != nil, Data: f.global}, nil
}
func (f *fakeEngine) WriteConfig(_ string, data []byte, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.global = data
	return nil
}
func (f *fakeEngine) Sync(_ context.Context, ps []*config.PackConfig, req web.RunRequest, h web.RunHooks) []app.Outcome {
	f.mu.Lock()
	f.runs = append(f.runs, req)
	f.mu.Unlock()
	out := make([]app.Outcome, len(ps))
	for i, p := range ps {
		out[i] = app.Outcome{Pack: p.Pack, Status: app.StatusSynced, DryRun: req.DryRun}
		h.Done(out[i])
	}
	return out
}

type fixture struct {
	t       *testing.T
	eng     *fakeEngine
	reg     *jobs.Registry
	session *mcp.ClientSession
	audit   *jsonlog.Log
}

func start(t *testing.T, o Options) *fixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f := &fixture{t: t, eng: &fakeEngine{}, audit: &jsonlog.Log{Path: filepath.Join(t.TempDir(), "audit.jsonl")}}
	f.reg = jobs.NewRegistry(ctx, &jobs.Store{Dir: t.TempDir()}, nil)
	o.Engine, o.Jobs, o.Audit = f.eng, f.reg, f.audit
	srvT, cliT := mcp.NewInMemoryTransports()
	if _, err := New(o).Connect(ctx, srvT, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, cliT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	f.session = session
	return f
}

func (f *fixture) tools() []string {
	f.t.Helper()
	res, err := f.session.ListTools(context.Background(), nil)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, tool := range res.Tools {
		out = append(out, tool.Name)
	}
	slices.Sort(out)
	return out
}

func (f *fixture) call(name string, args map[string]any) (string, bool) {
	f.t.Helper()
	res, err := f.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		f.t.Fatalf("%s: %v", name, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), res.IsError
}

func sorted(list []string) []string {
	out := slices.Clone(list)
	slices.Sort(out)
	return out
}

// Guard: write tools are absent under --read-only and without
// mcp.allow_writes. Proven red by registering them unconditionally: the
// first two cases listed sync_pack, cancel_job and set_setting.
func TestWriteToolsExistOnlyWithAllowWritesAndNeverReadOnly(t *testing.T) {
	reads := sorted(ReadTools)
	all := sorted(append(slices.Clone(ReadTools), WriteTools...))
	for name, c := range map[string]struct {
		o    Options
		want []string
	}{
		"default":                  {Options{}, reads},
		"read-only with writes on": {Options{ReadOnly: true, AllowWrites: true}, reads},
		"writes allowed":           {Options{AllowWrites: true}, all},
	} {
		if got := start(t, c.o).tools(); !slices.Equal(got, c.want) {
			t.Errorf("%s: tools %v, want %v", name, got, c.want)
		}
	}
	_, err := start(t, Options{}).session.CallTool(context.Background(),
		&mcp.CallToolParams{Name: "set_setting", Arguments: map[string]any{"name": "page_size", "value": 30}})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("an unregistered write tool answered: %v", err)
	}
}

func waitRuns(t *testing.T, f *fixture, n int) []web.RunRequest {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.eng.mu.Lock()
		got := slices.Clone(f.eng.runs)
		f.eng.mu.Unlock()
		if len(got) >= n {
			f.reg.Wait()
			return got
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("fewer than %d runs", n)
	return nil
}

func TestARealSyncNeedsConfirmEqualToTheSlug(t *testing.T) {
	f := start(t, Options{AllowWrites: true})
	if out, isErr := f.call("sync_pack", map[string]any{"slug": "icon-sets-flag"}); isErr {
		t.Fatalf("a default sync: %s", out)
	}
	if out, isErr := f.call("sync_pack", map[string]any{"slug": "icon-sets-flag", "dry_run": false}); !isErr || !strings.Contains(out, "confirm") {
		t.Fatalf("a real sync without confirm: %s", out)
	}
	if out, isErr := f.call("sync_pack", map[string]any{"slug": "icon-sets-flag", "dry_run": false, "confirm": "icon-sets-tabler"}); !isErr {
		t.Fatalf("a real sync confirming another pack: %s", out)
	}
	if out, isErr := f.call("sync_pack", map[string]any{"slug": "icon-sets-flag", "dry_run": false, "confirm": "icon-sets-flag"}); isErr {
		t.Fatalf("a confirmed real sync: %s", out)
	}
	runs := waitRuns(t, f, 2)
	if len(runs) != 2 || !runs[0].DryRun || runs[1].DryRun {
		t.Fatalf("runs %+v: want a dry run, then the confirmed real one", runs)
	}
	recs, _ := f.audit.Tail(0)
	if len(recs) != 2 || !strings.Contains(string(recs[1]), "dry_run=false") {
		t.Fatalf("audit %s", recs)
	}
	busy := start(t, Options{AllowWrites: true, ServerRunning: func() bool { return true }})
	if out, isErr := busy.call("sync_pack", map[string]any{"slug": "icon-sets-flag", "dry_run": false, "confirm": "icon-sets-flag"}); !isErr || !strings.Contains(out, "serve is running") {
		t.Fatalf("a real sync beside a running serve: %s", out)
	}
}

func TestReadToolsAnswer(t *testing.T) {
	f := start(t, Options{})
	out, isErr := f.call("list_packs", nil)
	if isErr || !strings.Contains(out, `"slug": "icon-sets-flag"`) {
		t.Fatalf("list_packs: %s", out)
	}
	out, isErr = f.call("check_upstream", map[string]any{})
	if isErr || !strings.Contains(out, `"stale": 1`) {
		t.Fatalf("check_upstream: %s", out)
	}
	out, isErr = f.call("dry_run_sync", map[string]any{"slug": "icon-sets-flag"})
	if isErr {
		t.Fatalf("dry_run_sync: %s", out)
	}
	var started struct{ Job jobs.Info }
	if err := json.Unmarshal([]byte(out), &started); err != nil || !started.Job.DryRun {
		t.Fatalf("dry_run_sync started %s", out)
	}
	if runs := waitRuns(t, f, 1); !runs[0].DryRun {
		t.Fatal("dry_run_sync ran for real")
	}
	out, isErr = f.call("get_job", map[string]any{"id": started.Job.ID})
	if isErr || !strings.Contains(out, `"status": "synced"`) {
		t.Fatalf("get_job: %s", out)
	}
	if out, isErr := f.call("get_pack", map[string]any{"slug": "../etc"}); !isErr {
		t.Fatalf("a non-slug: %s", out)
	}
}

func TestSetSettingIsChecked(t *testing.T) {
	f := start(t, Options{AllowWrites: true})
	if out, isErr := f.call("set_setting", map[string]any{"name": "page_size", "value": 3}); !isErr || !strings.Contains(out, "at least 5") {
		t.Fatalf("out of range: %s", out)
	}
	if out, isErr := f.call("set_setting", map[string]any{"name": "page_size", "value": 40}); isErr {
		t.Fatalf("a good value: %s", out)
	}
	if !strings.Contains(string(f.eng.global), `"page_size": 40`) {
		t.Fatalf("global config %s", f.eng.global)
	}
}
