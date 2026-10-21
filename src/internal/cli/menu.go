package cli

import (
	"github.com/spf13/cobra"

	"github.com/ichava/maintainer-toolkit/src/internal/app"
	"github.com/ichava/maintainer-toolkit/src/internal/tui"
	"github.com/ichava/maintainer-toolkit/src/internal/ui"
)

func newMenuCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "menu",
		Short: "Interactive menu (the default action in a terminal)",
		Args:  cobra.NoArgs,
		RunE:  runMenu,
	}
}

// runMenu launches the TUI, or prints help when there is nobody to drive it.
//
// This is the Docker image's default CMD, and the image is run by a scheduled
// GitHub Actions job as often as by a person. The Python version called
// questionary.select() unconditionally, which on a runner blocks on a stdin
// that never answers -- a job that hangs until the 30-minute timeout rather
// than one that says what it wanted.
func runMenu(cmd *cobra.Command, _ []string) error {
	if !ui.Interactive() {
		ui.Info("not a terminal, so there is nothing to drive the menu; printing help instead.")
		return cmd.Help()
	}
	action, err := tui.Run(configDir())
	if err != nil {
		return failure(err)
	}
	if action == nil {
		return nil
	}
	return runMenuAction(cmd, action)
}

// runMenuAction runs the sync the menu chose, through the same path as the
// `sync` command. A menu dry run is a flag-layer value, so nothing below it
// -- a pack's settings, the environment -- can turn it into a real commit.
func runMenuAction(cmd *cobra.Command, action *tui.Action) error {
	if action.DryRun {
		state.stack.Flags["dry_run"] = true
		state.run = state.stack.Resolve(nil)
	}
	only := action.Pack
	if action.All {
		only = ""
	}
	packs, err := app.LoadPacksIn(configDir(), only, workspace())
	if err != nil {
		return failure(err)
	}
	return runSync(cmd.Context(), packs)
}
