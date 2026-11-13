// Package web is the `serve` command's localhost web UI.
//
// Server-rendered html/template pages over the same internal/app functions
// the CLI and the TUI call, with a little vanilla JavaScript and Tabler's CSS,
// all embedded in the binary: the page makes no network request beyond this
// server. It never imports internal/cli, internal/ui or internal/tui, which
// hold process-wide state a long-lived server cannot share.
package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/events"
	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

// DefaultHistoryDir is where finished runs are kept: XDG state, on every
// platform but Windows, per the house layout ($XDG_STATE_HOME, else
// ~/.local/state; %LocalAppData% on Windows).
func DefaultHistoryDir(lookup func(string) (string, bool)) string {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	base, _ := lookup("XDG_STATE_HOME")
	if base == "" && runtime.GOOS == "windows" {
		base, _ = lookup("LOCALAPPDATA")
	}
	if base == "" {
		home, _ := lookup("HOME")
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		if home == "" {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "icon-sets-sync", "runs")
}

// Options configures a Server.
type Options struct {
	// Engine is the sync engine. Required.
	Engine Engine

	// Tools reports the external programs a run needs; nil is app.Tools.
	// Tests pin it so a page does not depend on what the machine has.
	Tools func() []app.Tool

	// Token is the launch token printed in the URL. Required.
	Token string

	// AllowedHosts are the Host header values answered (see AllowedHosts).
	AllowedHosts []string

	// AllowRemote records that the bind is not loopback; it lets a TLS
	// reverse proxy's X-Forwarded-Proto mark the session cookie Secure.
	AllowRemote bool

	// ReadOnly registers no route that runs a sync or writes a file.
	ReadOnly bool

	// Version, BindAddr and ConfigDir are shown on the settings page.
	Version  string
	BindAddr string
	// Build is what the About page reports about this binary. Zero means
	// the running binary's own (BuildInfoOf); tests pin it.
	Build BuildInfo

	// HistoryDir keeps finished jobs as JSON Lines. Empty keeps them in
	// memory only. See DefaultHistoryDir.
	HistoryDir string

	// CheckTTL is how old a cached upstream check may be before a page load
	// starts a new one. Zero means ten minutes.
	CheckTTL time.Duration

	// Now is the clock. Nil means time.Now.
	Now func() time.Time

	// Bus receives what the server and its runs do (see internal/events).
	// Nil makes one; serve passes its own so webhooks can subscribe.
	Bus *events.Bus

	// TLS records that the listener terminates TLS, for server.started.
	TLS bool

	// API configures the REST API. Its zero value leaves it off, and then
	// nothing under /api/ answers but the UI's sign-in refusal.
	API APIOptions
}

// Server is the web UI.
type Server struct {
	opts     Options
	pages    *pageSet
	assets   *assetSet
	sessions sessions
	handler  http.Handler
	status   statusCache
	jobs     *jobs.Registry
	history  *jobs.Store

	// stop is closed by the "Stop server" action.
	stop stopper

	// base is cancelled on shutdown, stopping background work.
	base   context.Context
	cancel context.CancelFunc

	// bus is where the server publishes; apiMux holds the API's routes,
	// nil when the API is off.
	bus    *events.Bus
	apiMux *http.ServeMux
	api    apiState
}

// New builds a Server. It parses every template once, so a template error is
// a startup error rather than a 500 on first visit.
func New(opts Options) (*Server, error) {
	if opts.Engine == nil {
		return nil, errors.New("web: an Engine is required")
	}
	if opts.Token == "" {
		return nil, errors.New("web: a Token is required")
	}
	if opts.CheckTTL == 0 {
		opts.CheckTTL = 10 * time.Minute
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Build == (BuildInfo{}) {
		opts.Build = BuildInfoOf(opts.Version)
	}
	assets, err := loadAssets()
	if err != nil {
		return nil, err
	}
	pages, err := parsePages(assets)
	if err != nil {
		return nil, err
	}
	s := &Server{opts: opts, pages: pages, assets: assets, stop: stopper{ch: make(chan struct{})}}
	s.base, s.cancel = context.WithCancel(context.Background())
	if opts.HistoryDir != "" {
		s.history = &jobs.Store{Dir: opts.HistoryDir}
	}
	s.bus = opts.Bus
	if s.bus == nil {
		s.bus = events.NewBus(events.Source(), opts.Now)
	}
	s.jobs = jobs.NewRegistry(s.base, s.history, opts.Now)
	s.jobs.SetHooks(jobs.Hooks{Started: s.publishJobStarted, Event: s.publishJobEvent})
	ui := securityHeaders(s.hostCheck(s.auth(s.sameOrigin(s.routes()))))
	s.handler = ui
	if opts.API.Enabled {
		if opts.API.Tokens == nil {
			return nil, errors.New("web: the API needs a token store")
		}
		s.api = newAPIState(opts.API)
		s.apiMux = s.apiRoutes()
		api := apiHeaders(s.apiHostCheck(s.apiAuth(s.apiMux)))
		// The API and the UI are two doors with two locks: a bearer token
		// opens /api/ and nothing else, a session cookie everything else and
		// never /api/.
		s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, apiPrefix) {
				api.ServeHTTP(w, r)
				return
			}
			ui.ServeHTTP(w, r)
		})
	}
	return s, nil
}

// Bus is the server's event bus.
func (s *Server) Bus() *events.Bus { return s.bus }

// Handler returns the full middleware chain and routes.
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) now() time.Time { return s.opts.Now() }

// Serve answers on ln until ctx is cancelled or Stop is called, then shuts down with a
// ten-second grace for requests in flight and cancels background work.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	s.bus.Emit(events.ServerStarted, map[string]any{
		"version": s.opts.Version, "address": s.opts.BindAddr, "tls": s.opts.TLS,
		"read_only": s.opts.ReadOnly, "api": s.apiMux != nil,
	})
	select {
	case err := <-errc:
		s.bus.Emit(events.ServerStopping, map[string]any{"reason": "error"})
		s.cancel()
		return err
	case <-ctx.Done():
		s.bus.Emit(events.ServerStopping, map[string]any{"reason": "signal"})
	case <-s.stop.ch:
		s.bus.Emit(events.ServerStopping, map[string]any{"reason": "stop-action"})
	}
	// Cancel the runs first: an open event stream only ends when its job
	// does, so shutting the server down before them would wait out the grace.
	s.cancel()
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	if err := srv.Shutdown(shutdown); err != nil {
		return err
	}
	s.jobs.Wait()
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Close stops background work without serving; for tests.
func (s *Server) Close() { s.cancel() }

// statusCache holds the last upstream check and pack stats, so a page load
// does not poll four registries.
type statusCache struct {
	mu      sync.Mutex
	at      time.Time
	packs   []*config.PackConfig
	results []checker.Result
	stats   []PackStats
	err     string
	running bool
	// done and total count the packs a running check has finished, for the
	// dashboard's progress bar.
	done, total int
}

type statusSnapshot struct {
	At      time.Time
	Packs   []*config.PackConfig
	Results []checker.Result
	Stats   []PackStats
	Err     string
	Running bool
	Done    int
	Total   int
}

func (c *statusCache) snapshot() statusSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return statusSnapshot{At: c.at, Packs: c.packs, Results: c.results, Stats: c.stats, Err: c.err, Running: c.running, Done: c.done, Total: c.total}
}

// refresh starts a background check unless one is running. It returns
// whether it started one.
func (s *Server) refresh() bool {
	c := &s.status
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return false
	}
	c.running = true
	c.done, c.total = 0, 0
	c.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(s.base, 3*time.Minute)
		defer cancel()
		packs, err := s.opts.Engine.Packs()
		var results []checker.Result
		var stats []PackStats
		if err == nil {
			c.mu.Lock()
			c.total = len(packs)
			c.mu.Unlock()
			if pc, ok := s.opts.Engine.(ProgressChecker); ok {
				results = pc.CheckProgress(ctx, packs, func(done int) {
					c.mu.Lock()
					c.done = done
					c.mu.Unlock()
				})
			} else {
				results = s.opts.Engine.Check(ctx, packs)
			}
			stats = make([]PackStats, len(packs))
			for i, p := range packs {
				stats[i] = s.opts.Engine.PackStats(ctx, p)
			}
		}
		c.mu.Lock()
		c.running = false
		c.at = s.now()
		c.packs, c.results, c.stats = packs, results, stats
		c.err = ""
		if err != nil {
			c.err = err.Error()
		}
		c.mu.Unlock()
		s.publishCheck(packs, results, err)
	}()
	return true
}

// DefaultStateDir is the tool's XDG state directory, the parent of the run
// history: $XDG_STATE_HOME/icon-sets-sync, else ~/.local/state/icon-sets-sync
// (%LocalAppData%\icon-sets-sync on Windows). It holds the API tokens, the
// webhook secrets and the logs beside the runs.
func DefaultStateDir(lookup func(string) (string, bool)) string {
	runs := DefaultHistoryDir(lookup)
	if runs == "" {
		return ""
	}
	return filepath.Dir(runs)
}

// refreshIfStale starts a check when there is none or it is older than the TTL.
func (s *Server) refreshIfStale() {
	snap := s.status.snapshot()
	if snap.Running {
		return
	}
	if snap.At.IsZero() || s.now().Sub(snap.At) > s.opts.CheckTTL {
		s.refresh()
	}
}

// waitIdle blocks until no background check is running; for tests.
func (s *Server) waitIdle(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !s.status.snapshot().Running {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

type ctxKey int

const sessionKey ctxKey = 0

func withSession(ctx context.Context, s *session) context.Context {
	return context.WithValue(ctx, sessionKey, s)
}

func sessionFrom(ctx context.Context) *session {
	s, _ := ctx.Value(sessionKey).(*session)
	return s
}
