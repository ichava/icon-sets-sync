package cli

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/ui"
	"github.com/ichava/icon-sets-sync/src/internal/version"
)

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check this machine can run a sync: tools, PATH, configs and pack directories",
		Long: `Report what a sync needs and whether it is here: git and gh (npm is only a
fallback), the PATH this process sees, where the pack configs and the global
config are, and whether each packages_dirs entry exists.

Exits 1 when a required tool or a packages_dirs entry is missing, so CI can
run it first. --json prints the same as a document.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tools := app.Tools()
			dirs := state.run.Strings("packages_dirs")
			missingDirs := app.MissingPackagesDirs(dirs)
			cfgDir, cfgSource := configSource()
			report := map[string]any{
				"version":       version.Version(),
				"platform":      runtime.GOOS + "/" + runtime.GOARCH,
				"path":          os.Getenv("PATH"),
				"config_dir":    cfgDir,
				"config_source": cfgSource,
				"global_config": state.global,
				"packages_dirs": dirs,
				"missing_dirs":  missingDirs,
				"tools":         tools,
				"integrations":  integrationReport(state.run),
			}
			bad := len(app.MissingTools(tools)) + len(missingDirs)
			if ui.JSONMode() {
				report["ok"] = bad == 0
				if err := ui.JSON(report); err != nil {
					return failure(err)
				}
			} else {
				ui.Heading("icon-sets-sync " + version.Version())
				ui.Info("platform %s/%s", runtime.GOOS, runtime.GOARCH)
				ui.Info("PATH %s", os.Getenv("PATH"))
				ui.Info("pack configs %s (chosen by %s)", cfgDir, cfgSource)
				g := state.global
				if g.Path != "" {
					ui.Info("global config %s (chosen by %s)", g.Path, g.Layer)
				} else {
					ui.Info("global config: none (it belongs at %s)", g.Default)
				}
				if len(dirs) == 0 {
					ui.Info("packages_dirs: not set (packs are found through workspace, /work, or beside this checkout)")
				}
				for _, d := range dirs {
					if contains(missingDirs, d) {
						ui.Failure("packages_dirs %s: missing", d)
					} else {
						ui.Success("packages_dirs %s", d)
					}
				}
				printIntegrations(integrationReport(state.run))
				for _, t := range tools {
					switch {
					case t.Found && t.OnPath:
						ui.Success("%s %s", t.Name, t.Path)
					case t.Found:
						ui.Warning("%s %s -- found outside PATH; add its directory to PATH for other programs", t.Name, t.Path)
					case t.Optional:
						ui.Info("%s not found -- %s", t.Name, t.Why)
					default:
						ui.Failure("%s not found -- needed because it %s", t.Name, t.Why)
					}
				}
			}
			if bad > 0 {
				return &ExitError{Code: ExitFailure, Err: fmt.Errorf("%d problem(s) found", bad)}
			}
			return nil
		},
	}
}

// integrationStatus is which integration surfaces are on, and the risky
// combinations among them, for doctor.
type integrationStatus struct {
	API          bool     `json:"api"`
	EventsStream bool     `json:"events_stream"`
	Webhooks     bool     `json:"webhooks"`
	Endpoints    int      `json:"webhook_endpoints"`
	MCP          bool     `json:"mcp"`
	MCPWrites    bool     `json:"mcp_writes"`
	Warnings     []string `json:"warnings"`
}

func integrationReport(run settings.Resolved) integrationStatus {
	st := integrationStatus{
		API: run.Bool("api.enabled"), EventsStream: run.Bool("events.stream"),
		Webhooks: run.Bool("webhooks.enabled"), Endpoints: len(run.Webhooks("webhooks.endpoints")),
		MCP: run.Bool("mcp.enabled"), MCPWrites: run.Bool("mcp.allow_writes"), Warnings: []string{},
	}
	warn := func(s string) { st.Warnings = append(st.Warnings, s) }
	if run.String("api.tls") == settings.TLSOffBehindProxy {
		warn("api.tls=off-behind-proxy: serve --allow-remote answers in plain HTTP; only safe behind a TLS proxy")
	}
	if st.API && run.Bool("api.allow_remote") {
		warn("api.allow_remote: the API answers beyond loopback with --allow-remote; keep tokens scoped and expiring")
	}
	if st.EventsStream && !st.API {
		warn("events.stream is on but api.enabled is off, so there is no stream to serve")
	}
	if run.Bool("webhooks.allow_private") {
		warn("webhooks.allow_private: webhooks may reach private, loopback and link-local addresses (cloud metadata included)")
	}
	if run.Bool("webhooks.allow_insecure_localhost") {
		warn("webhooks.allow_insecure_localhost: plain-http deliveries to localhost are allowed")
	}
	if st.Webhooks && st.Endpoints == 0 {
		warn("webhooks.enabled is on with no endpoint; `webhooks add` makes one")
	}
	if st.MCP && st.MCPWrites {
		warn("mcp.allow_writes: an agent can start real syncs (with confirm) and change settings")
	}
	return st
}

func printIntegrations(st integrationStatus) {
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	ui.Info("integrations: api %s, event stream %s, webhooks %s (%d endpoint(s)), mcp %s (writes %s)",
		onOff(st.API), onOff(st.EventsStream), onOff(st.Webhooks), st.Endpoints, onOff(st.MCP), onOff(st.MCPWrites))
	for _, w := range st.Warnings {
		ui.Warning("%s", w)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
