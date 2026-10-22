package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/recipes"
	"github.com/ichava/icon-sets-sync/src/internal/ui"
)

// ---------------------------------------------------------------- list-packs

func newListPacksCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "list-packs",
		Aliases: []string{"list"},
		Short:   "List the registered packs",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			packs, err := app.LoadPacksIn(configDir(), "", workspace())
			if err != nil {
				return failure(err)
			}

			if ui.JSONMode() {
				rows := make([]map[string]string, 0, len(packs))
				for _, p := range packs {
					rows = append(rows, map[string]string{
						"name": p.Name, "pack": p.Pack,
						"current": config.ResolvedCurrentVersion(p),
						"source":  p.Source.Type,
						"recipe":  recipes.Select(p),
					})
				}
				return ui.JSON(rows)
			}

			ui.Printf("Registered packs (%d):\n", len(packs))
			for _, p := range packs {
				// The version shown is the resolved one -- what the pack repo
				// records, not this repository's fallback. The Python printed
				// the fallback, so a synced pack still displayed a stale
				// number here while `check` correctly said it was up to date.
				current := config.ResolvedCurrentVersion(p)
				if current == "" {
					current = "?"
				}
				ui.Printf("  %s %s -> %s (current: %s)\n", ui.Sym.Bullet, p.Name, p.Pack, current)
			}
			return nil
		},
	}
}

// --------------------------------------------------------------------- check

func newCheckCommand() *cobra.Command {
	var pack string

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check every pack against its upstream registry",
		Long: `Check every pack against its upstream registry.

Exits 1 when any pack is behind upstream, so it can gate a scheduled job.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			packs, err := app.LoadPacksIn(configDir(), pack, workspace())
			if err != nil {
				return failure(err)
			}

			results := app.CheckAll(cmd.Context(), packs)

			if ui.JSONMode() {
				if err := ui.JSON(results); err != nil {
					return failure(err)
				}
			} else {
				ui.Printf("%s", renderCheck(results))
				ui.Summarise(results)
			}

			// Exit 1 on any stale pack. Load-bearing for CI.
			if checker.AnyStale(results) {
				return &ExitError{Code: ExitFailure}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&pack, "pack", "", "restrict to one pack slug")
	return cmd
}

// renderCheck maps results onto the shared table renderer in internal/ui.
func renderCheck(results []checker.Result) string {
	return ui.RenderCheckTable(ui.CheckRows(results))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ---------------------------------------------------------------------- sync

func newSyncCommand() *cobra.Command {
	var (
		pack     string
		allPacks bool
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Refresh a pack's assets from upstream and open a pull request",
		Long: `Refresh a pack's assets from upstream and open a pull request.

Fails (exit 1) when a pack's latest upstream version cannot be resolved, when
a pack declares no sinks outside a dry run, or when any stage fails. The other
packs still run first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if pack == "" && !allPacks {
				return usageErr("pass --pack=<slug> or --all")
			}
			if pack != "" && allPacks {
				return usageErr("pass --pack=<slug> or --all, not both")
			}
			packs, err := app.LoadPacksIn(configDir(), pack, workspace())
			if err != nil {
				return failure(err)
			}
			return runSync(cmd.Context(), packs)
		},
	}
	f := cmd.Flags()
	f.StringVar(&pack, "pack", "", "sync one pack slug")
	f.BoolVar(&allPacks, "all", false, "sync every pack (same as sync-all)")
	f.IntVar(&g.concurrency, "concurrency", 1, "packs synced at once with --all (setting concurrency)")
	addRunFlags(f, true)
	return cmd
}

func newSyncAllCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync-all",
		Short: "Refresh every registered pack, up to --concurrency at a time",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			packs, err := app.LoadPacksIn(configDir(), "", workspace())
			if err != nil {
				return failure(err)
			}
			return runSync(cmd.Context(), packs)
		},
	}
	f := cmd.Flags()
	f.IntVar(&g.concurrency, "concurrency", 1, "packs synced at once (setting concurrency)")
	addRunFlags(f, true)
	return cmd
}

// Seams for tests. Nil means the real checker and the real recipe runner,
// which need the network and npm.
var (
	syncChecker app.Checker
	syncRun     app.RunFunc
)

func runSync(ctx context.Context, packs []*config.PackConfig) error {
	outcomes := app.Sync(ctx, packs, app.SyncOptions{
		Checker:     syncChecker,
		Run:         syncRun,
		Stack:       state.stack,
		ConfigDir:   configDir(),
		Concurrency: state.run.Int("concurrency"),
		Starting: func(p *config.PackConfig, r checker.Result, dryRun bool) {
			mode := ""
			if dryRun {
				mode = " (dry run)"
			}
			ui.Info("%s: refreshing from %s %s %s%s", p.Pack, orDash(r.Current), ui.Sym.Arrow, r.Latest, mode)
		},
		Done: reportOutcome,
	})

	if ui.JSONMode() {
		rows := make([]map[string]any, 0, len(outcomes))
		for _, o := range outcomes {
			row := map[string]any{
				"pack": o.Pack, "status": o.Status, "current": o.Current,
				"latest": o.Latest, "dry_run": o.DryRun,
			}
			if o.Err != nil {
				row["error"] = o.Err.Error()
			}
			if o.Status == app.StatusSynced {
				row["summary"] = o.Result.Summary()
			}
			rows = append(rows, row)
		}
		if err := ui.JSON(rows); err != nil {
			return failure(err)
		}
	}

	if app.AnyFailed(outcomes) {
		return &ExitError{Code: ExitFailure}
	}
	return nil
}

func reportOutcome(o app.Outcome) {
	if ui.JSONMode() {
		return
	}
	switch o.Status {
	case app.StatusUpToDate:
		ui.Success("%s: up-to-date (%s)", o.Pack, o.Current)
	case app.StatusSynced:
		ui.Success("%s: done -- %v", o.Pack, o.Result.Summary())
	default:
		// The literal "recipe pending" is a cross-repository contract:
		// icon-sets-bundled's sync-upstream.yml greps for it. No recipe
		// returns it any more, but the wording is kept for any that might.
		if errors.Is(o.Err, pipeline.ErrRecipePending) {
			ui.Warning("%s: %s", o.Pack, o.Err)
			return
		}
		ui.Failure("%s: %s", o.Pack, o.Err)
	}
}

// --------------------------------------------------------------------- recipe

func newRecipeCommand() *cobra.Command {
	var versionFlag string

	cmd := &cobra.Command{
		Use:   "recipe <name>",
		Short: "Run one pack's recipe directly, skipping the upstream check",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			packs, err := app.LoadPacksIn(configDir(), args[0], workspace())
			if err != nil {
				return failure(err)
			}
			pack := packs[0]

			resolved, err := app.PackSettings(state.stack, pack)
			if err != nil {
				return failure(err)
			}
			s := resolved.Settings()

			version := versionFlag
			if version == "" {
				version = pack.CurrentVersion
			}
			if version == "" {
				return usageErr("%s: no version supplied and no current_version in config", pack.Pack)
			}
			if len(pack.Sinks) == 0 && !s.DryRun {
				return failure(fmt.Errorf("%s declares no sinks, so the recipe would write nothing; add sinks or pass --dry-run", pack.Pack))
			}

			outcome, err := app.RunRecipe(cmd.Context(), pack, version, recipes.Options{
				DryRun: s.DryRun, ConfigDir: configDir(), Settings: &resolved,
			})
			if err != nil {
				return failure(err)
			}
			if ui.JSONMode() {
				if err := ui.JSON(outcome.Summary()); err != nil {
					return failure(err)
				}
			} else {
				ui.Printf("%v\n", outcome.Summary())
			}
			if !outcome.Success {
				return &ExitError{Code: ExitFailure}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&versionFlag, "version", "", "version override (default: the config's current_version)")
	addRunFlags(f, false)
	return cmd
}
