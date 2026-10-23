// Package cli is the cobra command tree. It stays thin: every command parses
// flags and calls into internal/app and internal/core, so the TUI can call the
// same functions and the two front-ends cannot drift into two implementations.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/ui"
	"github.com/ichava/icon-sets-sync/src/internal/version"
)

// Exit codes. These are a contract, not an implementation detail: `check`
// exiting 1 on a stale pack is what makes it usable as a CI gate, and Click
// used 2 for a usage error, which anything wrapping the Python binary may
// already depend on.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

// ExitError carries an exit code out of a command.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit %d", e.Code)
	}
	return e.Err.Error()
}
func (e *ExitError) Unwrap() error { return e.Err }

func usageErr(format string, a ...any) error {
	return &ExitError{Code: ExitUsage, Err: fmt.Errorf(format, a...)}
}

func failure(err error) error { return &ExitError{Code: ExitFailure, Err: err} }

// globals holds the flags every command shares.
type globals struct {
	configDir    string
	globalConfig string
	sets         []string
	verbose      bool
	quiet        bool
	noColor      bool
	noInput      bool
	assumeYes    bool
	jsonOut      bool

	// Bound by the sync, sync-all and recipe commands. They are read through
	// the settings flag layer rather than directly, so a flag outranks the
	// environment and the config files in exactly one place.
	dryRun      bool
	commit      bool
	force       bool
	concurrency int
}

var g globals

// state is what PersistentPreRunE resolved, for the commands to read.
type runState struct {
	stack      settings.Stack
	run        settings.Resolved
	globalPath string
}

var state runState

// Execute builds the command tree and runs it, returning the process exit code.
func Execute() int {
	if filepath.Base(os.Args[0]) == version.LegacyName {
		fmt.Fprintf(os.Stderr, "warning: %s is deprecated and may be removed in 0.3.0; run %s instead.\n",
			version.LegacyName, version.Name)
	}

	ctx, stop := InterruptContext(context.Background())
	defer stop()

	root := NewRootCommand()
	if err := root.ExecuteContext(ctx); err != nil {
		var exit *ExitError
		if ok := asExitError(err, &exit); ok {
			if exit.Err != nil {
				ui.Failure("%s", exit.Err)
			}
			return exit.Code
		}
		ui.Failure("%s", err)
		return ExitFailure
	}
	return ExitOK
}

// InterruptContext returns a context cancelled by the first SIGINT or
// SIGTERM, so Ctrl-C stops a sync cleanly: the stage in flight sees the
// cancel, its subprocess or download is stopped, and no later stage (the
// commit, the push) runs. After the first signal the default handling is
// restored, so a second Ctrl-C still kills the process outright.
func InterruptContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx, stop
}

func asExitError(err error, target **ExitError) bool {
	for err != nil {
		if e, ok := err.(*ExitError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// NewRootCommand builds the tree.
func NewRootCommand() *cobra.Command {
	g = globals{}
	state = runState{run: settings.Resolve()}

	root := &cobra.Command{
		Use:   version.Name,
		Short: "Sync the Ichava icon packs from their upstream registries",
		Long: `Sync the Ichava icon packs from their upstream registries.

Polls each pack's upstream, refreshes the vendored SVGs into the pack repo,
stamps the version and opens a pull request.

Settings resolve in this order, highest first:
  flags > ICHAVA_* environment > global config > pack config > defaults
The global config is $XDG_CONFIG_HOME/icon-sets-sync/config.{json,yaml}.
Run "config print --origins" to see where every value came from.

Run with no arguments in a terminal for the interactive menu.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMenu(cmd, args)
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&g.configDir, "config-dir", "", "where pack configs live (setting config_dir; default: config/ beside the binary, else ./config)")
	pf.StringVar(&g.globalConfig, "global-config", "", "global config file (default: $XDG_CONFIG_HOME/icon-sets-sync/config.{json,yaml})")
	pf.StringArrayVar(&g.sets, "set", nil, "set any setting, as key=value (repeatable; see `config print`)")
	pf.BoolVarP(&g.verbose, "verbose", "v", false, "debug logging (log_level=debug)")
	pf.BoolVarP(&g.quiet, "quiet", "q", false, "only errors (log_level=error) and no status output")
	pf.BoolVar(&g.noColor, "no-color", false, "disable colour (also honours NO_COLOR)")
	pf.BoolVar(&g.noInput, "no-input", false, "never prompt; fail instead of asking")
	pf.BoolVar(&g.assumeYes, "yes", false, "assume yes for confirmations")
	pf.BoolVar(&g.jsonOut, "json", false, "machine-readable output")

	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if err := resolveState(cmd); err != nil {
			return err
		}
		ui.Init(ui.Config{
			AssumeYes: g.assumeYes,
			NoInput:   g.noInput,
			NoColor:   g.noColor,
			JSON:      g.jsonOut,
			Quiet:     g.quiet,
			Out:       cmd.OutOrStdout(),
			Err:       cmd.ErrOrStderr(),
		})
		setupLogging(state.run.String("log_level"))
		return nil
	}

	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageErr("%s", err)
	})

	root.AddCommand(
		newListPacksCommand(),
		newCheckCommand(),
		newSyncCommand(),
		newSyncAllCommand(),
		newRecipeCommand(),
		newConfigCommand(),
		newBundledCommand(),
		newMenuCommand(),
		newVersionCommand(),
	)
	return root
}

// resolveState builds the settings stack: the global config file, the
// environment, and the flags actually given on this command line.
//
// Only flags the user set count. A flag's zero default is not a statement --
// treating `--dry-run`'s false as one would override ICHAVA_DRY_RUN=1 with a
// value nobody typed.
func resolveState(cmd *cobra.Command) error {
	if g.verbose && g.quiet {
		return usageErr("-v and -q contradict each other; pass one")
	}
	if g.dryRun && g.commit {
		return usageErr("--dry-run and --commit contradict each other; pass one")
	}

	changed := func(name string) bool {
		f := cmd.Flags().Lookup(name)
		return f != nil && f.Changed
	}
	overlay := func(flagValues settings.Values) error {
		setFlag := func(name string, key string, value any) {
			if changed(name) {
				flagValues[key] = value
			}
		}
		setFlag("config-dir", "config_dir", g.configDir)
		if g.verbose {
			flagValues["log_level"] = "debug"
		}
		if g.quiet {
			flagValues["log_level"] = "error"
		}
		setFlag("dry-run", "dry_run", g.dryRun)
		if changed("commit") && g.commit {
			flagValues["dry_run"] = false
		}
		setFlag("force", "force", g.force)
		if changed("concurrency") {
			if err := flagValues.Set("concurrency", g.concurrency); err != nil {
				return fmt.Errorf("--concurrency: %s", err)
			}
		}
		return nil
	}

	// The layering itself lives in app.BuildStack, so the web UI resolves
	// precedence through the same code. This function keeps the CLI-only
	// parts: the contradiction checks and the mapping onto exit codes.
	stack, path, err := app.BuildStack(app.StackInput{
		GlobalPath:  g.globalConfig,
		Lookup:      os.LookupEnv,
		Assignments: g.sets,
		Overlay:     overlay,
	})
	if err != nil {
		if app.IsUsage(err) {
			return usageErr("%s", err)
		}
		return failure(err)
	}

	state.globalPath = path
	state.stack = stack
	state.run = state.stack.Resolve(nil)
	return nil
}

// addRunFlags binds the flags the refreshing commands share. --force only
// means something where there is an upstream check to override.
func addRunFlags(f *pflag.FlagSet, withForce bool) {
	f.BoolVar(&g.dryRun, "dry-run", false, "refresh files but skip the version stamp, commit and pull request (setting dry_run)")
	f.BoolVar(&g.commit, "commit", false, "commit even if a config or ICHAVA_DRY_RUN says dry run (the Python's --commit)")
	if withForce {
		f.BoolVar(&g.force, "force", false, "refresh even when not stale (setting force)")
	}
}

// setupLogging sends structured logs to stderr, so they never contaminate the
// stdout a caller may be piping into jq.
func setupLogging(level string) {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})))
}

// configDir resolves the directory to read pack configs from.
func configDir() string {
	if dir := state.run.String("config_dir"); dir != "" {
		return dir
	}
	return config.DefaultConfigDir()
}

func workspace() string { return state.run.String("workspace") }

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and exit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The Docker smoke test calls this; keep the "<name> <version>"
			// shape on the first line.
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s %s\n", version.Name, version.Version())
			if g.verbose {
				if c := version.Commit(); c != "" {
					fmt.Fprintf(out, "commit %s\n", c)
				}
				if d := version.Date(); d != "" {
					fmt.Fprintf(out, "built  %s\n", d)
				}
			}
			return nil
		},
	}
}
