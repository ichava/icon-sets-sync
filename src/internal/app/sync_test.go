package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/recipes"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

type fakeChecker map[string]checker.Result

func (f fakeChecker) CheckPack(_ context.Context, p *config.PackConfig) checker.Result {
	return f[p.Name]
}

func pack(name string, sinks ...string) *config.PackConfig {
	p := &config.PackConfig{Name: name, Pack: "ichava/" + name, PackRoot: "/work/" + name}
	for _, s := range sinks {
		p.Sinks = append(p.Sinks, config.SinkConfig{Type: s})
	}
	return p
}

type recorded struct {
	mu   sync.Mutex
	runs map[string]recipes.Options
}

func (r *recorded) run(result pipeline.Result, err error) RunFunc {
	return func(_ context.Context, p *config.PackConfig, version string, opts recipes.Options) (pipeline.Result, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.runs == nil {
			r.runs = map[string]recipes.Options{}
		}
		r.runs[p.Name+"@"+version] = opts
		return result, err
	}
}

func TestSync(t *testing.T) {
	stale := checker.Result{Current: "1.0.0", Latest: "2.0.0", Stale: true}
	fresh := checker.Result{Current: "2.0.0", Latest: "2.0.0"}
	dead := checker.Result{Current: "1.0.0", Reason: "unreachable: 503"}

	tests := []struct {
		name      string
		pack      *config.PackConfig
		check     checker.Result
		flags     settings.Values
		runResult pipeline.Result
		runErr    error
		want      Status
		wantRun   bool
		wantErr   string
	}{
		{"stale pack syncs", pack("a", "filesystem", "git-branch"), stale, nil,
			pipeline.Result{Success: true}, nil, StatusSynced, true, ""},
		{"fresh pack is left alone", pack("a", "filesystem"), fresh, nil,
			pipeline.Result{Success: true}, nil, StatusUpToDate, false, ""},
		{"force refreshes a fresh pack", pack("a", "filesystem"), fresh, settings.Values{"force": true},
			pipeline.Result{Success: true}, nil, StatusSynced, true, ""},
		{"an unresolvable upstream fails, it is not skipped", pack("a", "filesystem"), dead, nil,
			pipeline.Result{}, nil, StatusUnresolved, false, "503"},
		{"no sinks is an error", pack("a"), stale, nil,
			pipeline.Result{}, nil, StatusFailed, false, "declares no sinks"},
		{"no sinks is allowed in a dry run", pack("a"), stale, settings.Values{"dry_run": true},
			pipeline.Result{Success: true}, nil, StatusSynced, true, ""},
		{"a recipe error fails the pack", pack("a", "filesystem"), stale, nil,
			pipeline.Result{}, errors.New("npm pack failed"), StatusFailed, true, "npm pack failed"},
		{"a failed pipeline fails the pack", pack("a", "filesystem"), stale, nil,
			pipeline.Result{Err: errors.New("boom"), FailedStage: "Sanitise"}, nil, StatusFailed, true, "Sanitise: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorded{}
			out := Sync(context.Background(), []*config.PackConfig{tt.pack}, SyncOptions{
				Stack:   settings.Stack{Flags: tt.flags},
				Checker: fakeChecker{"a": tt.check},
				Run:     rec.run(tt.runResult, tt.runErr),
			})
			if len(out) != 1 {
				t.Fatalf("got %d outcomes", len(out))
			}
			o := out[0]
			if o.Status != tt.want {
				t.Errorf("status = %s, want %s (err %v)", o.Status, tt.want, o.Err)
			}
			if ran := len(rec.runs) > 0; ran != tt.wantRun {
				t.Errorf("ran = %v, want %v", ran, tt.wantRun)
			}
			if tt.wantErr != "" && (o.Err == nil || !strings.Contains(o.Err.Error(), tt.wantErr)) {
				t.Errorf("err = %v, want containing %q", o.Err, tt.wantErr)
			}
			if got := AnyFailed(out); got != (tt.want == StatusFailed || tt.want == StatusUnresolved) {
				t.Errorf("AnyFailed = %v", got)
			}
		})
	}
}

// The recipe runs against the latest version, with this pack's own settings
// layered under the run-wide ones.
func TestSyncPassesThePackSettingsAndLatestVersion(t *testing.T) {
	p := pack("a", "filesystem", "git-branch")
	p.Settings = map[string]any{"git": map[string]any{"base": "from-pack"}, "dry_run": true}

	rec := &recorded{}
	Sync(context.Background(), []*config.PackConfig{p}, SyncOptions{
		Stack:     settings.Stack{Env: settings.Values{"git.branch": "chore/env"}},
		ConfigDir: "/cfg",
		Checker:   fakeChecker{"a": {Latest: "3.0.0", Stale: true}},
		Run:       rec.run(pipeline.Result{Success: true}, nil),
	})

	opts, ok := rec.runs["a@3.0.0"]
	if !ok {
		t.Fatalf("runs = %v", rec.runs)
	}
	if !opts.DryRun || opts.ConfigDir != "/cfg" {
		t.Errorf("opts = %+v", opts)
	}
	s := opts.Settings.Settings()
	if s.Git.Base != "from-pack" || s.Git.Branch != "chore/env" {
		t.Errorf("settings = %+v", s.Git)
	}
}

func TestSyncRejectsABadPackSettingsBlock(t *testing.T) {
	p := pack("a", "filesystem")
	p.Settings = map[string]any{"concurrency": 3.0}
	out := Sync(context.Background(), []*config.PackConfig{p}, SyncOptions{
		Checker: fakeChecker{"a": {Latest: "1", Stale: true}},
		Run:     (&recorded{}).run(pipeline.Result{Success: true}, nil),
	})
	if out[0].Status != StatusFailed || !strings.Contains(out[0].Err.Error(), "run-wide") {
		t.Fatalf("outcome = %+v", out[0])
	}
}

// One failure must not stop the rest, and outcomes come back in input order
// whatever order the workers finish in.
func TestSyncContinuesPastAFailureAndKeepsOrder(t *testing.T) {
	packs := []*config.PackConfig{pack("a", "filesystem"), pack("b", "filesystem"), pack("c", "filesystem")}
	out := Sync(context.Background(), packs, SyncOptions{
		Concurrency: 3,
		Checker: fakeChecker{
			"a": {Latest: "2", Stale: true},
			"b": {Reason: "unreachable"},
			"c": {Latest: "2", Stale: true},
		},
		Run: (&recorded{}).run(pipeline.Result{Success: true}, nil),
	})
	got := []Status{out[0].Status, out[1].Status, out[2].Status}
	want := []Status{StatusSynced, StatusUnresolved, StatusSynced}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("statuses = %v, want %v", got, want)
		}
	}
	if out[1].Pack != "ichava/b" {
		t.Errorf("order lost: %v", out[1].Pack)
	}
}

func TestSyncHonoursConcurrency(t *testing.T) {
	tests := []struct{ limit, packs int }{{1, 4}, {2, 6}, {4, 4}}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			var active, peak int32
			run := func(context.Context, *config.PackConfig, string, recipes.Options) (pipeline.Result, error) {
				n := atomic.AddInt32(&active, 1)
				for {
					p := atomic.LoadInt32(&peak)
					if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&active, -1)
				return pipeline.Result{Success: true}, nil
			}
			var packs []*config.PackConfig
			chk := fakeChecker{}
			for i := 0; i < tt.packs; i++ {
				name := string(rune('a' + i))
				packs = append(packs, pack(name, "filesystem"))
				chk[name] = checker.Result{Latest: "2", Stale: true}
			}
			Sync(context.Background(), packs, SyncOptions{Concurrency: tt.limit, Checker: chk, Run: run})
			if int(peak) > tt.limit {
				t.Errorf("peak concurrency %d exceeds limit %d", peak, tt.limit)
			}
			if tt.limit > 1 && peak < 2 {
				t.Errorf("limit %d but never ran in parallel (peak %d)", tt.limit, peak)
			}
		})
	}
}

func TestApplyWorkspace(t *testing.T) {
	tests := []struct{ root, workspace, want string }{
		{"/work/x", "/ws", "/work/x"},
		{"icon-sets-x", "/ws", filepath.Join("/ws", "icon-sets-x")},
		{"icon-sets-x", "", "icon-sets-x"},
	}
	for _, tt := range tests {
		p := &config.PackConfig{PackRoot: tt.root}
		ApplyWorkspace(p, tt.workspace)
		if p.PackRoot != tt.want {
			t.Errorf("(%q, %q) = %q, want %q", tt.root, tt.workspace, p.PackRoot, tt.want)
		}
	}
}
