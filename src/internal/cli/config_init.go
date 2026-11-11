package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/app"
)

// ------------------------------------------------------------ config init

func newConfigInitCommand() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write the global config from what this machine has, unless you have edited it",
		Long: `Write the global config from what this machine has: its $schema line,
packages_dirs (the directory of ichava pack checkouts this tool sits in) and
private_config_dir (when exactly one pack checkout carries a private config).

A missing file, a file with nothing in it but $schema, and a file this command
wrote and nobody changed since are (re)written; a file you edited is left
alone unless --force is given. The web UI does the same at every start, and
its global config page has a Reinitialise button. The previous copy is kept in
the config history (config history global).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := configEngine().InitGlobal(force)
			if err != nil {
				return failure(err)
			}
			out := cmd.OutOrStdout()
			switch {
			case res.Changed:
				_, _ = fmt.Fprintf(out, "wrote %s (was %s)\n", res.Path, res.State)
			case res.State == app.GlobalEdited:
				_, _ = fmt.Fprintf(out, "left %s alone: you have edited it (pass --force to replace it; the old copy goes to the history)\n", res.Path)
			default:
				_, _ = fmt.Fprintf(out, "%s is already what this machine generates\n", res.Path)
			}
			for _, n := range res.Notes {
				_, _ = fmt.Fprintf(out, "  %s\n", n)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace the global config even if you edited it")
	return cmd
}
