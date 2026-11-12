// Package mcpserver is `icon-sets-sync mcp`: a Model Context Protocol server
// on stdio, built on the official Go SDK, so an agent's host spawns it and no
// network port is opened.
//
// It offers the read tools once it is enabled. The write tools (sync_pack,
// cancel_job, set_setting) are registered only with mcp.allow_writes, and
// never under --read-only: a tool that is not registered cannot be called
// by a confused or hostile prompt. A real sync also needs confirm set to the
// pack's slug.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/jsonlog"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/events"
	"github.com/ichava/icon-sets-sync/src/internal/web"
	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

// Engine is what the tools ask of the sync engine; web.AppEngine is one.
type Engine interface {
	web.Syncer
	web.ConfigStore
	Packs() ([]*config.PackConfig, error)
	Pack(slug string) (*config.PackConfig, error)
	PackNotes(pack *config.PackConfig) map[string]string
	Check(ctx context.Context, packs []*config.PackConfig) []checker.Result
	ConfigFiles() ([]web.ConfigFile, error)
	ValidateConfig(name string, data []byte) []app.Finding
}

// Options configures the server.
type Options struct {
	Engine Engine
	// Jobs runs and remembers this process's syncs; its store is the same
	// history serve keeps, so runs started here show on the Runs page.
	Jobs *jobs.Registry
	// ReadOnly (--read-only) and AllowWrites (mcp.allow_writes) decide
	// whether the write tools exist at all.
	ReadOnly, AllowWrites bool
	// ServerRunning reports a serve on this machine; a real sync is then
	// refused here, so two processes never race on one pack repository.
	ServerRunning func() bool
	// Audit receives one record per write tool call. Nil keeps none.
	Audit *jsonlog.Log
	// Version is the binary's.
	Version string
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// ReadTools and WriteTools name the tools, for docs and tests.
var (
	ReadTools  = []string{"list_packs", "get_pack", "check_upstream", "list_jobs", "get_job", "get_job_log", "validate_configs", "dry_run_sync"}
	WriteTools = []string{"sync_pack", "cancel_job", "set_setting"}
)

type server struct{ o Options }

// New builds the MCP server.
func New(o Options) *mcp.Server {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.ServerRunning == nil {
		o.ServerRunning = func() bool { return false }
	}
	s := &server{o: o}
	srv := mcp.NewServer(&mcp.Implementation{Name: "icon-sets-sync", Version: o.Version}, &mcp.ServerOptions{
		Instructions: "Inspect and sync the ichava icon packs. Read tools are always offered; sync_pack, cancel_job and " +
			"set_setting only when the operator set mcp.allow_writes. Every sync is a dry run unless dry_run is false, " +
			"and a real one needs confirm set to the pack's slug.",
	})
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	mcp.AddTool(srv, &mcp.Tool{Name: "list_packs", Description: "List every registered icon pack: slug, repository, recipe and vendored version.", Annotations: readOnly}, s.listPacks)
	mcp.AddTool(srv, &mcp.Tool{Name: "get_pack", Description: "One pack's details and its authoring notes.", Annotations: readOnly}, s.getPack)
	mcp.AddTool(srv, &mcp.Tool{Name: "check_upstream", Description: "Poll the upstream registries for the given packs (all when none are named) and report which are behind. Writes nothing.", Annotations: readOnly}, s.checkUpstream)
	mcp.AddTool(srv, &mcp.Tool{Name: "list_jobs", Description: "List runs, newest first: this process's and the history serve keeps.", Annotations: readOnly}, s.listJobs)
	mcp.AddTool(srv, &mcp.Tool{Name: "get_job", Description: "One run's status and each pack's outcome.", Annotations: readOnly}, s.getJob)
	mcp.AddTool(srv, &mcp.Tool{Name: "get_job_log", Description: "A run's newest events (log lines, stages, pack outcomes).", Annotations: readOnly}, s.getJobLog)
	mcp.AddTool(srv, &mcp.Tool{Name: "validate_configs", Description: "Validate every config file (registry, pack configs, global config) as the loader would.", Annotations: readOnly}, s.validateConfigs)
	mcp.AddTool(srv, &mcp.Tool{Name: "dry_run_sync", Description: "Start a dry run of one pack: fetch and diff, write nothing into the pack, commit and push nothing. Returns the run's id; poll get_job.", Annotations: readOnly}, s.dryRunSync)
	if o.AllowWrites && !o.ReadOnly {
		mcp.AddTool(srv, &mcp.Tool{Name: "sync_pack", Description: "Sync one pack. A dry run unless dry_run is false; a real run writes, commits, pushes and opens a pull request, and needs confirm set to the pack's slug.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true)}}, s.syncPack)
		mcp.AddTool(srv, &mcp.Tool{Name: "cancel_job", Description: "Cancel a run this process started."}, s.cancelJob)
		mcp.AddTool(srv, &mcp.Tool{Name: "set_setting", Description: "Save one setting to the global config, checked as the Settings page checks it. value is a JSON value of the setting's type.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true)}}, s.setSetting)
	}
	for _, t := range events.Types() {
		data, _ := events.Schema(t)
		uri := "icon-sets-sync://schemas/events/" + t
		srv.AddResource(&mcp.Resource{URI: uri, Name: t + " event schema", MIMEType: "application/schema+json"}, staticResource(uri, data))
	}
	srv.AddResource(&mcp.Resource{URI: "icon-sets-sync://schemas/pack", Name: "pack config schema", MIMEType: "application/schema+json"},
		staticResource("icon-sets-sync://schemas/pack", config.PackSchema()))
	if global, err := settings.GlobalSchema(); err == nil {
		srv.AddResource(&mcp.Resource{URI: "icon-sets-sync://schemas/global", Name: "global config schema", MIMEType: "application/schema+json"},
			staticResource("icon-sets-sync://schemas/global", global))
	}
	return srv
}

func ptr[T any](v T) *T { return &v }

func staticResource(uri string, data []byte) mcp.ResourceHandler {
	return func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/schema+json", Text: string(data)}}}, nil
	}
}

// result is a tool's JSON answer, as text the model reads.
func result(v any) (*mcp.CallToolResult, any, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
}

type noInput struct{}

type packJSON struct {
	Slug    string            `json:"slug"`
	Pack    string            `json:"pack"`
	Recipe  string            `json:"recipe"`
	Current string            `json:"current_version"`
	Notes   map[string]string `json:"notes,omitempty"`
}

func describe(p *config.PackConfig) packJSON {
	return packJSON{Slug: p.Name, Pack: p.Pack, Recipe: web.RecipeName(p), Current: config.ResolvedCurrentVersion(p)}
}

func (s *server) listPacks(context.Context, *mcp.CallToolRequest, noInput) (*mcp.CallToolResult, any, error) {
	packs, err := s.o.Engine.Packs()
	if err != nil {
		return nil, nil, err
	}
	out := make([]packJSON, len(packs))
	for i, p := range packs {
		out[i] = describe(p)
	}
	return result(out)
}

type slugInput struct {
	Slug string `json:"slug" jsonschema:"the pack's slug, as list_packs gives it"`
}

func (s *server) pack(slug string) (*config.PackConfig, error) {
	if !config.IsSlug(slug) {
		return nil, fmt.Errorf("%q is not a pack slug", slug)
	}
	return s.o.Engine.Pack(slug)
}

func (s *server) getPack(_ context.Context, _ *mcp.CallToolRequest, in slugInput) (*mcp.CallToolResult, any, error) {
	p, err := s.pack(in.Slug)
	if err != nil {
		return nil, nil, err
	}
	out := describe(p)
	out.Notes = s.o.Engine.PackNotes(p)
	return result(out)
}

type checkInput struct {
	Slugs []string `json:"slugs,omitempty" jsonschema:"the packs to check; all when empty"`
}

func (s *server) checkUpstream(ctx context.Context, _ *mcp.CallToolRequest, in checkInput) (*mcp.CallToolResult, any, error) {
	all, err := s.o.Engine.Packs()
	if err != nil {
		return nil, nil, err
	}
	packs := all
	if len(in.Slugs) > 0 {
		packs = nil
		for _, p := range all {
			if slices.Contains(in.Slugs, p.Name) {
				packs = append(packs, p)
			}
		}
		if len(packs) != len(in.Slugs) {
			return nil, nil, fmt.Errorf("unknown pack among %v", in.Slugs)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	results := s.o.Engine.Check(ctx, packs)
	type row struct {
		Pack    string `json:"pack"`
		Current string `json:"current"`
		Latest  string `json:"latest,omitempty"`
		Status  string `json:"status"`
		Reason  string `json:"reason,omitempty"`
	}
	out := make([]row, 0, len(results))
	for i, r := range results {
		out = append(out, row{Pack: packs[i].Name, Current: r.Current, Latest: r.Latest, Status: string(r.Status()), Reason: r.Reason})
	}
	return result(map[string]any{"results": out, "stale": checker.CountStale(results), "errored": checker.CountErrored(results)})
}

type listJobsInput struct {
	Pack   string `json:"pack,omitempty" jsonschema:"only runs touching this pack"`
	Status string `json:"status,omitempty" jsonschema:"only runs with this status: running, succeeded, failed, cancelled, needs-review"`
	Limit  int    `json:"limit,omitempty" jsonschema:"at most this many (default 20)"`
}

func (s *server) listJobs(_ context.Context, _ *mcp.CallToolRequest, in listJobsInput) (*mcp.CallToolResult, any, error) {
	limit := in.Limit
	if limit < 1 || limit > 500 {
		limit = 20
	}
	out := []jobs.Info{}
	for _, info := range s.o.Jobs.List() {
		if in.Pack != "" && !slices.Contains(info.Packs, in.Pack) {
			continue
		}
		if in.Status != "" && string(info.Status) != in.Status {
			continue
		}
		out = append(out, info)
		if len(out) == limit {
			break
		}
	}
	return result(out)
}

type jobInput struct {
	ID string `json:"id" jsonschema:"the run's id, as list_jobs gives it"`
}

func (s *server) lookup(id string) (jobs.Info, []jobs.Event, error) {
	if j := s.o.Jobs.Get(id); j != nil {
		return j.Info(), j.Events(), nil
	}
	if s.o.Jobs.Store() == nil || !jobs.ValidID(id) {
		return jobs.Info{}, nil, jobs.ErrNotFound
	}
	return s.o.Jobs.Store().Load(id)
}

func (s *server) getJob(_ context.Context, _ *mcp.CallToolRequest, in jobInput) (*mcp.CallToolResult, any, error) {
	info, evs, err := s.lookup(in.ID)
	if err != nil {
		return nil, nil, err
	}
	type outcome struct {
		Pack   string `json:"pack"`
		Status string `json:"status"`
		Detail string `json:"detail,omitempty"`
		Error  string `json:"error,omitempty"`
	}
	packs := []outcome{}
	for _, e := range evs {
		if e.Type == jobs.EventPackDone {
			packs = append(packs, outcome{e.Pack, e.Status, e.Msg, e.Err})
		}
	}
	return result(map[string]any{"job": info, "packs": packs})
}

type jobLogInput struct {
	ID    string `json:"id" jsonschema:"the run's id"`
	Limit int    `json:"limit,omitempty" jsonschema:"the newest this many events (default 200)"`
}

func (s *server) getJobLog(_ context.Context, _ *mcp.CallToolRequest, in jobLogInput) (*mcp.CallToolResult, any, error) {
	_, evs, err := s.lookup(in.ID)
	if err != nil {
		return nil, nil, err
	}
	limit := in.Limit
	if limit < 1 || limit > 5000 {
		limit = 200
	}
	if len(evs) > limit {
		evs = evs[len(evs)-limit:]
	}
	return result(evs)
}

func (s *server) validateConfigs(context.Context, *mcp.CallToolRequest, noInput) (*mcp.CallToolResult, any, error) {
	files, err := s.o.Engine.ConfigFiles()
	if err != nil {
		return nil, nil, err
	}
	type report struct {
		Name     string        `json:"name"`
		Findings []app.Finding `json:"findings"`
	}
	out := []report{}
	problems := 0
	for _, f := range files {
		doc, err := s.o.Engine.ReadConfig(f.Name)
		if err != nil || !doc.Exists {
			continue
		}
		fs := s.o.Engine.ValidateConfig(f.Name, doc.Data)
		if fs == nil {
			fs = []app.Finding{}
		}
		problems += len(fs)
		out = append(out, report{Name: f.Name, Findings: fs})
	}
	return result(map[string]any{"files": out, "problems": problems, "ok": problems == 0})
}

type dryRunInput struct {
	Slug  string `json:"slug" jsonschema:"the pack's slug"`
	Force bool   `json:"force,omitempty" jsonschema:"run even when the pack is not behind upstream"`
}

func (s *server) dryRunSync(_ context.Context, _ *mcp.CallToolRequest, in dryRunInput) (*mcp.CallToolResult, any, error) {
	return s.startSync(in.Slug, web.RunRequest{DryRun: true, Force: in.Force})
}

func (s *server) startSync(slug string, req web.RunRequest) (*mcp.CallToolResult, any, error) {
	p, err := s.pack(slug)
	if err != nil {
		return nil, nil, err
	}
	j, err := web.StartSync(s.o.Jobs, s.o.Engine, jobs.KindSync, []*config.PackConfig{p}, req)
	if err != nil {
		return nil, nil, err
	}
	return result(map[string]any{"job": j.Info(), "poll": "get_job with this id"})
}

type syncInput struct {
	Slug    string `json:"slug" jsonschema:"the pack's slug"`
	DryRun  *bool  `json:"dry_run,omitempty" jsonschema:"false makes a real run; anything else is a dry run"`
	Force   bool   `json:"force,omitempty" jsonschema:"run even when the pack is not behind upstream"`
	Confirm string `json:"confirm,omitempty" jsonschema:"for a real run, the pack's slug again"`
}

// ErrNeedsConfirm refuses a real sync whose confirm is not the slug.
var ErrNeedsConfirm = errors.New("a real sync needs confirm set to the pack's slug")

func (s *server) syncPack(ctx context.Context, req *mcp.CallToolRequest, in syncInput) (*mcp.CallToolResult, any, error) {
	dry := in.DryRun == nil || *in.DryRun
	if !dry {
		if in.Confirm != in.Slug || in.Slug == "" {
			return nil, nil, ErrNeedsConfirm
		}
		if s.o.ServerRunning() {
			return nil, nil, errors.New("a serve is running on this machine; start real runs through it (its UI or its API), so two processes never work one pack at once")
		}
	}
	s.audit("sync_pack", in.Slug, fmt.Sprintf("dry_run=%t", dry))
	return s.startSync(in.Slug, web.RunRequest{DryRun: dry, Force: in.Force})
}

func (s *server) cancelJob(_ context.Context, _ *mcp.CallToolRequest, in jobInput) (*mcp.CallToolResult, any, error) {
	if !s.o.Jobs.Cancel(in.ID) {
		return nil, nil, errors.New("only a run this process started can be cancelled here")
	}
	s.audit("cancel_job", in.ID, "")
	return result(map[string]any{"cancelling": in.ID})
}

type settingInput struct {
	Name  string `json:"name" jsonschema:"the setting's name, such as page_size"`
	Value any    `json:"value" jsonschema:"the value, of the setting's own type"`
}

func (s *server) setSetting(_ context.Context, _ *mcp.CallToolRequest, in settingInput) (*mcp.CallToolResult, any, error) {
	k, ok := settings.Lookup(in.Name)
	if !ok || k.Name != in.Name {
		return nil, nil, fmt.Errorf("unknown setting %q", in.Name)
	}
	value, note, err := web.ParseSettingValue(k, in.Value)
	if err != nil {
		return nil, nil, fmt.Errorf("%s %w", k.Name, err)
	}
	if err := web.SaveGlobalSetting(s.o.Engine, k.Name, value); err != nil {
		return nil, nil, err
	}
	s.audit("set_setting", k.Name, "")
	return result(map[string]any{"name": k.Name, "saved": true, "note": note})
}

// auditRecord matches the API's audit lines, with the tool in place of the
// route and "mcp" in place of a token's name.
type auditRecord struct {
	Time   time.Time `json:"time"`
	Token  string    `json:"token"`
	Tool   string    `json:"tool"`
	Target string    `json:"target"`
	Detail string    `json:"detail,omitempty"`
}

func (s *server) audit(tool, target, detail string) {
	_ = s.o.Audit.Append(auditRecord{Time: s.o.Now().UTC(), Token: "mcp", Tool: tool, Target: target, Detail: detail})
}
