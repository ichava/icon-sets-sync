package cli

import (
	"context"
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/recipes"
	"github.com/ichava/icon-sets-sync/src/internal/core/text"
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
	var (
		pack   string
		kf     kindFlags
		failOn string
		github bool
	)

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check every pack against its upstream registry",
		Long: `Check every pack against its upstream registry.

Exits 1 when any pack is behind upstream, so it can gate a scheduled job.
--fail-on chooses what fails the run: stale (the default), error (a pack
whose upstream could not be checked), any (either) or never. In a GitHub
Actions step, --github also writes a summary table to $GITHUB_STEP_SUMMARY,
stale=, errored= and stale-packs= to $GITHUB_OUTPUT, and an annotation per
pack that is behind or could not be checked.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := kf.validate(); err != nil {
				return err
			}
			if !validFailOn(failOn) {
				return usageErr("--fail-on must be one of %s, not %q", strings.Join(failOnModes, ", "), failOn)
			}
			if github {
				if _, _, err := githubFiles(); err != nil {
					return usageErr("%s", err)
				}
			}
			packs, err := app.LoadPacksIn(configDir(), pack, workspace())
			if err != nil {
				return failure(err)
			}

			results := checkAll(cmd.Context(), packs)

			if ui.JSONMode() {
				if err := ui.JSON(results); err != nil {
					return failure(err)
				}
			} else {
				ui.Printf("%s", renderCheck(results))
				ui.Summarise(results)
			}

			kf.report(packs)

			if github {
				if err := reportCheckToGitHub(cmd.OutOrStdout(), results, !ui.JSONMode()); err != nil {
					return failure(err)
				}
			}

			// Exit 1 on a stale pack by default. Load-bearing for CI.
			if checkFails(failOn, results) {
				return &ExitError{Code: ExitFailure}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&failOn, "fail-on", "stale", "what exits 1: stale, error (could not be checked), any, or never")
	cmd.Flags().BoolVar(&github, "github", false, "in GitHub Actions: write a step summary, step outputs and annotations")
	cmd.Flags().StringVar(&pack, "pack", "", "restrict to one pack slug")
	addKindFlags(cmd.Flags(), &kf)
	return cmd
}

// renderCheck maps results onto the shared table renderer in internal/ui.
func renderCheck(results []checker.Result) string {
	return ui.RenderCheckTable(ui.CheckRows(results))
}

// ---------------------------------------------------------------------- sync

func newSyncCommand() *cobra.Command {
	var (
		pack     string
		allPacks bool
		kf       kindFlags
		github   bool
	)

	cmd := &cobra.Command{
		Use: "sync",
		// run is the CI-mode name (`run --pack <slug> --non-interactive`).
		Aliases: []string{"run"},
		Short:   "Refresh a pack's assets from upstream and open a pull request",
		Long: `Refresh a pack's assets from upstream and open a pull request.

Fails (exit 1) when a pack's latest upstream version cannot be resolved, when
a pack declares no sinks outside a dry run, or when any stage fails. The other
packs still run first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := kf.validate(); err != nil {
				return err
			}
			if github {
				if _, _, err := githubFiles(); err != nil {
					return usageErr("%s", err)
				}
			}
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
			// The scan reads the pack checkout, so it runs after the sync has
			// refreshed it, and whether or not the sync succeeded.
			syncErr := runSync(cmd.Context(), cmd, packs, github)
			kf.report(packs)
			return syncErr
		},
	}
	f := cmd.Flags()
	f.StringVar(&pack, "pack", "", "sync one pack slug")
	addKindFlags(f, &kf)
	f.BoolVar(&allPacks, "all", false, "sync every pack (same as sync-all)")
	f.BoolVar(&github, "github", false, "in GitHub Actions: write a step summary, step outputs and annotations")
	f.IntVar(&g.concurrency, "concurrency", 1, "packs synced at once with --all (setting concurrency)")
	addRunFlags(f, true)
	return cmd
}

func newSyncAllCommand() *cobra.Command {
	var github bool
	cmd := &cobra.Command{
		Use:   "sync-all",
		Short: "Refresh every registered pack, up to --concurrency at a time",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if github {
				if _, _, err := githubFiles(); err != nil {
					return usageErr("%s", err)
				}
			}
			packs, err := app.LoadPacksIn(configDir(), "", workspace())
			if err != nil {
				return failure(err)
			}
			return runSync(cmd.Context(), cmd, packs, github)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&github, "github", false, "in GitHub Actions: write a step summary, step outputs and annotations")
	f.IntVar(&g.concurrency, "concurrency", 1, "packs synced at once (setting concurrency)")
	addRunFlags(f, true)
	return cmd
}

// checkAll is app.CheckAll, swappable so a test can run `check` without the
// network.
var checkAll = app.CheckAll

// Seams for tests. Nil means the real checker and the real recipe runner,
// which need the network and npm.
var (
	syncChecker app.Checker
	syncRun     app.RunFunc
)

func runSync(ctx context.Context, cmd *cobra.Command, packs []*config.PackConfig, github bool) error {
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
			ui.Info("%s: refreshing from %s %s %s%s", p.Pack, text.OrDash(r.Current), ui.Sym.Arrow, r.Latest, mode)
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
			// Present only when true: CI opens an issue for a person instead
			// of a pull request.
			if o.LicenceChanged() {
				row["licence_changed"] = true
			}
			if reviews := o.Reviews(); len(reviews) > 0 {
				row["reviews"] = reviews
			}
			rows = append(rows, row)
		}
		if err := ui.JSON(rows); err != nil {
			return failure(err)
		}
	}

	if github {
		if err := reportSyncToGitHub(cmd.OutOrStdout(), outcomes, !ui.JSONMode()); err != nil {
			return failure(err)
		}
	}

	if app.AnyFailed(outcomes) {
		return &ExitError{Code: ExitFailure}
	}
	if app.AnyNeedsReview(outcomes) {
		return &ExitError{Code: ExitNeedsReview}
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
		ui.Success("%s: done -- %s", o.Pack, o.Result.Headline())
	case app.StatusNeedsReview:
		ui.Warning("%s: needs review", o.Pack)
		for _, r := range o.Reviews() {
			if r.Reason != "" {
				ui.Warning("  %s", r.Reason)
			}
			for _, rm := range r.Removals {
				if rm.RenamedTo != "" {
					ui.Warning("  renamed  %s -> %s", rm.Path, rm.RenamedTo)
				} else {
					ui.Warning("  removed  %s", rm.Path)
				}
			}
		}
		ui.Warning("%s: check these, then re-run with --force to let them through", o.Pack)
	default:
		// A pack with no recipe yet is reported as a warning, not a failure.
		// No recipe returns ErrRecipePending today, and since
		// icon-sets-bundled#95 no workflow greps for its wording.
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
			// The checks and the run are app.Recipe, which the web UI calls
			// too; this maps its errors and renders the summary.
			outcome, err := app.Recipe(cmd.Context(), app.RecipeRequest{
				Stack: state.stack, Pack: packs[0], ConfigDir: configDir(), Version: versionFlag,
			})
			if err != nil {
				if app.IsUsage(err) {
					return usageErr("%s", err)
				}
				return failure(err)
			}
			if ui.JSONMode() {
				if err := ui.JSON(outcome.Summary()); err != nil {
					return failure(err)
				}
			} else {
				ui.Printf("%s\n", outcome.Headline())
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
