package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/core/gitx"
	"github.com/ichava/icon-sets-sync/src/internal/core/jsonlog"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/mcpserver"
	"github.com/ichava/icon-sets-sync/src/internal/version"
	"github.com/ichava/icon-sets-sync/src/internal/web"
	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

// mcpTransport is the seam a test replaces with an in-memory transport.
var mcpTransport = func() mcp.Transport { return &mcp.StdioTransport{} }

func newMCPCommand() *cobra.Command {
	var enable, readOnly bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the Model Context Protocol on stdio, for an AI agent's host",
		Long: `Serve the Model Context Protocol on stdin and stdout, for an AI agent's host to
spawn. No network port is opened.

Off unless mcp.enabled is set or --enable is passed. The read tools
(list_packs, get_pack, check_upstream, list_jobs, get_job, get_job_log,
validate_configs, dry_run_sync) are always offered. The write tools
(sync_pack, cancel_job, set_setting) exist only with mcp.allow_writes, and
never with --read-only. Every sync is a dry run unless dry_run is false, and a
real one needs confirm set to the pack's slug. See docs/tools/mcp.md.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !enable && !state.run.Bool("mcp.enabled") {
				return usageErr("the MCP server is off: set mcp.enabled (in the global config, on the Settings page, or ICHAVA_MCP_ENABLED=1), or pass --enable for this run")
			}
			return runMCP(cmd.Context(), readOnly, state.run)
		},
	}
	cmd.Flags().BoolVar(&enable, "enable", false, "serve even though mcp.enabled is off, for this run")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "offer no write tool, whatever mcp.allow_writes says")
	return cmd
}

// runMCP serves until the host closes stdin or ctx ends. Nothing may write to
// stdout but the protocol, so every message goes to stderr.
func runMCP(ctx context.Context, readOnly bool, run settings.Resolved) error {
	eng := configEngine()
	eng.ConfigDir, eng.ConfigSource = configSource()
	eng.Git = gitx.New()
	stateDir := web.DefaultStateDir(os.LookupEnv)
	var store *jobs.Store
	var audit *jsonlog.Log
	if stateDir != "" {
		store = &jobs.Store{Dir: web.DefaultHistoryDir(os.LookupEnv)}
		audit = &jsonlog.Log{Path: filepath.Join(stateDir, "api-audit.jsonl"), Keep: 3}
	}
	reg := jobs.NewRegistry(ctx, store, nil)
	srv := mcpserver.New(mcpserver.Options{
		Engine:        eng,
		Jobs:          reg,
		ReadOnly:      readOnly,
		AllowWrites:   run.Bool("mcp.allow_writes"),
		ServerRunning: serverRunning,
		Audit:         audit,
		Version:       version.Version(),
	})
	err := srv.Run(ctx, mcpTransport())
	reg.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		return failure(err)
	}
	return nil
}

// serverRunning reports a serve recorded and answering on this machine.
func serverRunning() bool {
	st, ok := readInstanceState()
	return ok && probeInstance(st.URL)
}
